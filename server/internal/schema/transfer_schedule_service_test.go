package schema_test

import (
	"context"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scheduleTestTransfer(t *testing.T, ctx context.Context, svc *schema.Service) *schema.TransferDefinition {
	t.Helper()
	def, err := svc.CreateTransfer(ctx, validTransfer(uniqueTransferName(t)), "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransfer(context.Background(), def.ID) })
	return def
}

func TestTransferSchedules_CreateRefusesWhatCouldNeverRun(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}
	def := scheduleTestTransfer(t, ctx, svc)

	for why, in := range map[string]schema.TransferScheduleInput{
		"no name":                {Transfer: def.ID, Cron: "0 2 * * *", SourceAccount: "a", DestinationAccount: "b"},
		"a cron it can't read":   {Name: "x", Transfer: def.ID, Cron: "every night", SourceAccount: "a", DestinationAccount: "b"},
		"no source account":      {Name: "x", Transfer: def.ID, Cron: "0 2 * * *", DestinationAccount: "b"},
		"no destination account": {Name: "x", Transfer: def.ID, Cron: "0 2 * * *", SourceAccount: "a"},
	} {
		_, err := svc.CreateTransferSchedule(ctx, in, "dispatch_db", "")
		assert.ErrorIs(t, err, schema.ErrInvalidTransferSchedule, why)
	}

	_, err := svc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name: "x", Transfer: "no such transfer", Cron: "0 2 * * *", SourceAccount: "a", DestinationAccount: "b",
	}, "dispatch_db", "")
	assert.ErrorIs(t, err, schema.ErrTransferNotFound)
}

func TestTransferSchedules_StoreWhatWasAuthorizedAndNoSecret(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}
	def := scheduleTestTransfer(t, ctx, svc) // surveys here → results_db.surveys

	name := uniqueTransferName(t)
	sched, err := svc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name: name, Transfer: def.Name, Cron: "  0   2 * * * ",
		SourceAccount: " Sender ", DestinationAccount: "receiver",
	}, "dispatch_db", "owner-1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransferSchedule(context.Background(), sched.ID) })

	assert.Equal(t, def.ID, sched.TransferID)
	assert.Equal(t, def.Name, sched.TransferName)
	assert.Equal(t, "0 2 * * *", sched.Cron)
	assert.Equal(t, "sender", sched.SourceAccount, "usernames are compared without case")
	assert.Equal(t, "dispatch_db", sched.SourceDatabase, "the local end is stored by name")
	assert.Equal(t, "results_db", sched.DestinationDatabase)
	assert.True(t, sched.Enabled)
	require.NotNil(t, sched.NextRunAt)
	assert.Equal(t, 2, sched.NextRunAt.Hour())

	_, err = svc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name: name, Transfer: def.ID, Cron: "0 3 * * *", SourceAccount: "a", DestinationAccount: "b",
	}, "dispatch_db", "")
	assert.ErrorIs(t, err, schema.ErrTransferScheduleExists)

	// Turning it off clears its next run; a new time brings one back.
	off := false
	paused, err := svc.UpdateTransferSchedule(ctx, sched.ID, schema.TransferSchedulePatch{Enabled: &off})
	require.NoError(t, err)
	assert.Nil(t, paused.NextRunAt)
	on, every := true, "*/30 * * * *"
	resumed, err := svc.UpdateTransferSchedule(ctx, sched.ID, schema.TransferSchedulePatch{Enabled: &on, Cron: &every})
	require.NoError(t, err)
	require.NotNil(t, resumed.NextRunAt)
	assert.Contains(t, []int{0, 30}, resumed.NextRunAt.Minute())
}

// The claim is what keeps two server processes from running the same
// occurrence: only the first of two claims made from the same reading wins.
func TestTransferSchedules_AnOccurrenceIsClaimedOnce(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}
	def := scheduleTestTransfer(t, ctx, svc)
	sched, err := svc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name: uniqueTransferName(t), Transfer: def.ID, Cron: "* * * * *",
		SourceAccount: "a", DestinationAccount: "b",
	}, "dispatch_db", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransferSchedule(context.Background(), sched.ID) })

	now := sched.NextRunAt.Add(10 * time.Second)
	due, err := svc.DueTransferSchedules(ctx, now)
	require.NoError(t, err)
	var reading *schema.TransferSchedule
	for i := range due {
		if due[i].ID == sched.ID {
			reading = &due[i]
		}
	}
	require.NotNil(t, reading, "the schedule is due")

	first, err := svc.ClaimTransferScheduleRun(ctx, reading, now)
	require.NoError(t, err)
	second, err := svc.ClaimTransferScheduleRun(ctx, reading, now)
	require.NoError(t, err)
	assert.True(t, first)
	assert.False(t, second, "the same reading cannot claim the same occurrence twice")

	due, err = svc.DueTransferSchedules(ctx, now)
	require.NoError(t, err)
	for _, d := range due {
		assert.NotEqual(t, sched.ID, d.ID, "a claimed occurrence is no longer due")
	}
}

func TestTransferSchedules_KeepTheMostRecentRuns(t *testing.T) {
	svc, ctx := transferTestService(t)
	if svc == nil {
		return
	}
	def := scheduleTestTransfer(t, ctx, svc)
	sched, err := svc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name: uniqueTransferName(t), Transfer: def.ID, Cron: "0 2 * * *",
		SourceAccount: "a", DestinationAccount: "b",
	}, "dispatch_db", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.DeleteTransferSchedule(context.Background(), sched.ID) })

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 55; i++ {
		at := start.Add(time.Duration(i) * time.Hour)
		require.NoError(t, svc.RecordTransferScheduleRun(ctx, &schema.TransferScheduleRun{
			ScheduleID: sched.ID, Trigger: schema.ScheduleTriggerSchedule,
			StartedAt: at, FinishedAt: at.Add(time.Second), Status: schema.ScheduleRunCommitted,
		}))
	}

	runs, err := svc.ListTransferScheduleRuns(ctx, sched.ID)
	require.NoError(t, err)
	require.Len(t, runs, 50, "the history is bounded")
	assert.Equal(t, start.Add(54*time.Hour), runs[0].StartedAt.UTC(), "newest first")
	assert.Equal(t, start.Add(5*time.Hour), runs[49].StartedAt.UTC(), "the oldest five went")

	after, err := svc.GetTransferSchedule(ctx, sched.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.ScheduleRunCommitted, after.LastStatus)

	// Deleting the transfer deletes its schedules and their history.
	require.NoError(t, svc.DeleteTransfer(ctx, def.ID))
	_, err = svc.GetTransferSchedule(ctx, sched.ID)
	assert.ErrorIs(t, err, schema.ErrTransferScheduleNotFound)
}
