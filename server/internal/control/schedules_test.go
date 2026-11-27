package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Server schedules are kept in the control store, so they and their history
// outlive the server process (#188).
func TestSchedules_AreKeptAcrossARestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "control.db")
	store, err := NewStore(path)
	require.NoError(t, err)

	next := time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC)
	sc := ServerSchedule{ID: "s1", Name: "Nightly", Type: ScheduleBackup, Databases: []string{"crm"},
		Cron: "0 2 * * *", Keep: 7, Enabled: true, NextRunAt: &next, CreatedBy: "root"}
	require.NoError(t, store.CreateSchedule(ctx, sc))
	assert.ErrorIs(t, store.CreateSchedule(ctx, ServerSchedule{ID: "s2", Name: " nightly ", Type: ScheduleBackup}),
		ErrScheduleNameTaken, "names are unique whatever their case")
	_, err = store.StartScheduleRun(ctx, ScheduleRun{ScheduleID: "s1", Trigger: "schedule", StartedAt: next})
	require.NoError(t, err)
	require.NoError(t, store.Close())

	store, err = NewStore(path)
	require.NoError(t, err)
	defer store.Close()
	got, err := store.GetSchedule(ctx, "s1")
	require.NoError(t, err)
	assert.Equal(t, "Nightly", got.Name)
	assert.Equal(t, []string{"crm"}, got.Databases)
	assert.Equal(t, 7, got.Keep)
	require.NotNil(t, got.NextRunAt)
	assert.True(t, got.NextRunAt.Equal(next))

	// The run the server stopped during is recorded as failed, not left
	// running for ever.
	n, err := store.InterruptScheduleRuns(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	runs, err := store.ListScheduleRuns(ctx, "s1", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, RunFailed, runs[0].Status)
	assert.Contains(t, runs[0].Error, "stopped")
}

// An occurrence is claimed once: a second claim of it, by a late tick or
// another process, is refused.
func TestSchedules_AnOccurrenceIsClaimedOnce(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(filepath.Join(t.TempDir(), "control.db"))
	require.NoError(t, err)
	defer store.Close()

	due := time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC)
	after := due.Add(24 * time.Hour)
	require.NoError(t, store.CreateSchedule(ctx, ServerSchedule{ID: "s1", Name: "Nightly", Type: ScheduleBackup,
		Cron: "0 2 * * *", Keep: 7, Enabled: true, NextRunAt: &due}))

	run := ScheduleRun{ScheduleID: "s1", Trigger: "schedule", StartedAt: due}
	sc, runID, err := store.ClaimScheduleRun(ctx, "s1", due, &after, run)
	require.NoError(t, err)
	require.NotNil(t, sc, "claimed")
	assert.True(t, sc.NextRunAt.Equal(after), "the schedule is given as it is now")
	assert.Positive(t, runID)
	sc, _, err = store.ClaimScheduleRun(ctx, "s1", due, &after, run)
	require.NoError(t, err)
	assert.Nil(t, sc, "the same occurrence is not claimed twice")

	// The claimed occurrence has its run recorded with it, so a stop right
	// after the claim is found at the next start.
	runs, err := store.ListScheduleRuns(ctx, "s1", 0)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, RunRunning, runs[0].Status)

	// A change of its settings leaves its next run alone; a change of its
	// timing applies only over the next run it was read with.
	got, err := store.GetSchedule(ctx, "s1")
	require.NoError(t, err)
	got.Keep = 30
	require.NoError(t, store.UpdateSchedule(ctx, *got, false, nil))
	stale := due
	moved := due.Add(time.Hour)
	got.NextRunAt = &moved
	assert.ErrorIs(t, store.UpdateSchedule(ctx, *got, true, &stale), ErrScheduleChanged,
		"a run claimed meanwhile is not moved back")
	got, err = store.GetSchedule(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, got.NextRunAt.Equal(after))
	assert.Equal(t, 30, got.Keep)

	// A disabled schedule is not claimed.
	got.Enabled = false
	require.NoError(t, store.UpdateSchedule(ctx, *got, false, nil))
	sc, _, err = store.ClaimScheduleRun(ctx, "s1", after, nil, run)
	require.NoError(t, err)
	assert.Nil(t, sc)
}

// The history of a schedule keeps its latest runs, and goes with it.
func TestSchedules_KeepTheirLatestRuns(t *testing.T) {
	ctx := context.Background()
	store, err := NewStore(filepath.Join(t.TempDir(), "control.db"))
	require.NoError(t, err)
	defer store.Close()
	require.NoError(t, store.CreateSchedule(ctx, ServerSchedule{ID: "s1", Name: "Hourly", Type: ScheduleMaintenance,
		Cron: "0 * * * *", Operation: "analyze"}))

	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < keptRuns+5; i++ {
		at := start.Add(time.Duration(i) * time.Hour)
		require.NoError(t, store.FinishScheduleRun(ctx, ScheduleRun{ScheduleID: "s1", Trigger: "schedule",
			Status: RunSuccess, StartedAt: at, FinishedAt: &at, SizeBytes: int64(i)}))
	}
	runs, err := store.ListScheduleRuns(ctx, "s1", 0)
	require.NoError(t, err)
	require.Len(t, runs, keptRuns)
	assert.EqualValues(t, keptRuns+4, runs[0].SizeBytes, "the latest run comes first")
	last, err := store.LastScheduleRuns(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, keptRuns+4, last["s1"].SizeBytes)

	require.NoError(t, store.DeleteSchedule(ctx, "s1"))
	runs, err = store.ListScheduleRuns(ctx, "s1", 0)
	require.NoError(t, err)
	assert.Empty(t, runs)
	assert.ErrorIs(t, store.DeleteSchedule(ctx, "s1"), ErrScheduleNotFound)
}
