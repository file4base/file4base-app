package transfer_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/transfer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scheduleFixture is the Dispatch/Results scene of #96 with an account in
// each solution for a schedule to run as (#108).
type scheduleFixture struct {
	*fixture
	mgr      *dbal.MultiDatabaseManager
	surveys  *schema.TransferDefinition
	sender   *schema.UserMetadata // in Dispatch
	receiver *schema.UserMetadata // in Results
}

func newScheduleFixture(t *testing.T, ctx context.Context, mgr *dbal.MultiDatabaseManager) *scheduleFixture {
	t.Helper()
	f := newFixture(t, ctx, mgr)
	sender, err := f.dispatch.schemaSvc.CreateUser(ctx, "sender", "not-used-by-a-schedule", "admin")
	require.NoError(t, err)
	receiver, err := f.results.schemaSvc.CreateUser(ctx, "receiver", "not-used-by-a-schedule", "admin")
	require.NoError(t, err)
	return &scheduleFixture{
		fixture:  f,
		mgr:      mgr,
		surveys:  f.surveyTransfer(t, ctx),
		sender:   sender,
		receiver: receiver,
	}
}

func (f *scheduleFixture) schedule(t *testing.T, ctx context.Context, cron string) *schema.TransferSchedule {
	t.Helper()
	sched, err := f.dispatch.schemaSvc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name:               "Nightly surveys",
		Transfer:           f.surveys.Name,
		Cron:               cron,
		SourceAccount:      "sender",
		DestinationAccount: "receiver",
	}, f.dispatch.name, "dispatch-owner")
	require.NoError(t, err)
	return sched
}

func (f *scheduleFixture) runNow(t *testing.T, ctx context.Context, sched *schema.TransferSchedule) *schema.TransferScheduleRun {
	t.Helper()
	run, err := transfer.RunSchedule(ctx, f.mgr, f.dispatch.name, sched, schema.ScheduleTriggerManual)
	require.NoError(t, err)
	return run
}

func TestRunSchedule_RunsAsItsAccountsAndRecordsTheRun(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newScheduleFixture(t, ctx, mgr)
	sched := f.schedule(t, ctx, "0 2 * * *")

	// What was authorized is kept with the schedule; no secret is.
	assert.Equal(t, f.dispatch.name, sched.SourceDatabase)
	assert.Equal(t, f.results.name, sched.DestinationDatabase)
	require.NotNil(t, sched.NextRunAt)

	run := f.runNow(t, ctx, sched)
	assert.Equal(t, schema.ScheduleRunCommitted, run.Status, run.Error)
	var report transfer.Report
	require.NoError(t, json.Unmarshal([]byte(run.Report), &report))
	assert.Equal(t, 2, report.Added)
	assert.True(t, report.Committed)

	assert.Len(t, rows(t, ctx, f.results.dataSvc, "project_surveys"), 2)

	history, err := f.dispatch.schemaSvc.ListTransferScheduleRuns(ctx, sched.ID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, schema.ScheduleTriggerManual, history[0].Trigger)
	assert.Equal(t, schema.ScheduleRunCommitted, history[0].Status)

	after, err := f.dispatch.schemaSvc.GetTransferSchedule(ctx, sched.ID)
	require.NoError(t, err)
	assert.Equal(t, schema.ScheduleRunCommitted, after.LastStatus)
	require.NotNil(t, after.LastRunAt)

	// A replay of the schedule is a replay of the transfer: it updates.
	run = f.runNow(t, ctx, sched)
	require.NoError(t, json.Unmarshal([]byte(run.Report), &report))
	assert.Equal(t, 0, report.Added)
	assert.Equal(t, 2, report.Updated)
	assert.Len(t, rows(t, ctx, f.results.dataSvc, "project_surveys"), 2)
}

// Every run checks its authority again, because nobody is signed in to vouch
// for it. Losing any part of it refuses the run and writes nothing (#108).
func TestRunSchedule_RefusesWhenAnAccountLostItsAuthority(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}

	cases := []struct {
		why    string
		revoke func(t *testing.T, f *scheduleFixture)
		says   string
	}{
		{
			why: "the destination account was disabled",
			revoke: func(t *testing.T, f *scheduleFixture) {
				inactive := false
				require.NoError(t, f.results.schemaSvc.UpdateUser(ctx, f.receiver.ID, "", "", &inactive))
			},
			says: "is disabled",
		},
		{
			why: "the source account was deleted",
			revoke: func(t *testing.T, f *scheduleFixture) {
				require.NoError(t, f.dispatch.schemaSvc.DeleteUser(ctx, f.sender.ID))
			},
			says: "has no account sender",
		},
		{
			why: "the destination role lost bulk_import",
			revoke: func(t *testing.T, f *scheduleFixture) {
				require.NoError(t, f.results.schemaSvc.SetRolePrivileges(ctx, []schema.RolePrivileges{
					{Role: "admin", BulkExport: true, BulkImport: false},
				}))
			},
			says: "does not hold the bulk_import privilege",
		},
		{
			why: "the source role lost bulk_export",
			revoke: func(t *testing.T, f *scheduleFixture) {
				require.NoError(t, f.dispatch.schemaSvc.SetRolePrivileges(ctx, []schema.RolePrivileges{
					{Role: "admin", BulkExport: false, BulkImport: true},
				}))
			},
			says: "does not hold the bulk_export privilege",
		},
	}

	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			f := newScheduleFixture(t, ctx, mgr)
			sched := f.schedule(t, ctx, "0 2 * * *")
			tc.revoke(t, f)

			run := f.runNow(t, ctx, sched)
			assert.Equal(t, schema.ScheduleRunRefused, run.Status)
			assert.Contains(t, run.Error, tc.says)
			assert.Empty(t, run.Report, "a refused run never reached the transfer")
			assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_surveys"), "nothing was written")

			history, err := f.dispatch.schemaSvc.ListTransferScheduleRuns(ctx, sched.ID)
			require.NoError(t, err)
			require.Len(t, history, 1, "a refused run is still recorded")
			assert.Equal(t, schema.ScheduleRunRefused, history[0].Status)
		})
	}
}

// A schedule authorized for Results must not start writing to another
// solution because someone edited the transfer it runs.
func TestRunSchedule_RefusesADefinitionRepointedAtAnotherSolution(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newScheduleFixture(t, ctx, mgr)
	sched := f.schedule(t, ctx, "0 2 * * *")

	elsewhere := newSolution(t, ctx, mgr, fmt.Sprintf("f4b_elsewhere_%d", uniqueTestStamp()))
	elsewhere.createTable(t, ctx, "project_surveys",
		column{"code", "Code", dbal.FieldTypeText},
		column{"label", "Label", dbal.FieldTypeText},
		column{"issued_on", "Issued On", dbal.FieldTypeDate},
		column{"quota", "Quota", dbal.FieldTypeNumber},
	)
	_, err := elsewhere.schemaSvc.CreateUser(ctx, "receiver", "not-used", "admin")
	require.NoError(t, err)

	in := schema.TransferInput{
		Name:        f.surveys.Name,
		Source:      f.surveys.Source,
		Destination: schema.TransferEnd{Database: elsewhere.name, Table: "project_surveys"},
		Mappings:    f.surveys.Mappings,
		MatchFields: f.surveys.MatchFields,
		Mode:        f.surveys.Mode,
	}
	_, err = f.dispatch.schemaSvc.UpdateTransfer(ctx, f.surveys.ID, in)
	require.NoError(t, err)

	run := f.runNow(t, ctx, sched)
	assert.Equal(t, schema.ScheduleRunRefused, run.Status)
	assert.Contains(t, run.Error, "was authorized for "+f.results.name)
	assert.Empty(t, rows(t, ctx, elsewhere.dataSvc, "project_surveys"), "the new destination got nothing")
	assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_surveys"))
}

func TestRunSchedule_RecordsATransferThatRefusedItself(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newScheduleFixture(t, ctx, mgr)
	strict := f.answerTransfer(t, ctx, "Send answers (strict)", schema.TransferRejectedAbort)

	sched, err := f.dispatch.schemaSvc.CreateTransferSchedule(ctx, schema.TransferScheduleInput{
		Name: "Nightly answers", Transfer: strict.ID, Cron: "0 3 * * *",
		SourceAccount: "sender", DestinationAccount: "receiver",
	}, f.dispatch.name, "")
	require.NoError(t, err)

	run := f.runNow(t, ctx, sched)
	assert.Equal(t, schema.ScheduleRunFailed, run.Status)
	assert.Contains(t, run.Error, "rejected")
	var report transfer.Report
	require.NoError(t, json.Unmarshal([]byte(run.Report), &report), "the report says which row it was")
	require.Len(t, report.Rejected, 1)
	assert.Equal(t, "non renseigné", report.Rejected[0].Value)
	assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_answers"))
}

func TestScheduler_RunsADueScheduleOnceAndOnlyOnce(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newScheduleFixture(t, ctx, mgr)
	sched := f.schedule(t, ctx, "* * * * *")
	require.NotNil(t, sched.NextRunAt)

	// Two schedulers — two server processes over the same databases — wake up
	// together just after the schedule became due.
	due := sched.NextRunAt.Add(30 * time.Second)
	newScheduler := func() *transfer.Scheduler {
		s := transfer.NewScheduler(mgr)
		s.Now = func() time.Time { return due }
		s.Databases = func(context.Context) ([]string, error) { return []string{f.dispatch.name}, nil }
		s.Logf = t.Logf
		return s
	}
	a, b := newScheduler(), newScheduler()

	var wg sync.WaitGroup
	started := make([]int, 2)
	for i, s := range []*transfer.Scheduler{a, b} {
		wg.Add(1)
		go func(i int, s *transfer.Scheduler) {
			defer wg.Done()
			started[i] = s.Tick(ctx)
		}(i, s)
	}
	wg.Wait()
	a.Wait()
	b.Wait()

	assert.Equal(t, 1, started[0]+started[1], "exactly one scheduler claimed the occurrence")
	assert.Len(t, rows(t, ctx, f.results.dataSvc, "project_surveys"), 2)

	history, err := f.dispatch.schemaSvc.ListTransferScheduleRuns(ctx, sched.ID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, schema.ScheduleTriggerSchedule, history[0].Trigger)
	assert.Equal(t, schema.ScheduleRunCommitted, history[0].Status)

	// The occurrence has been taken: the same moment holds nothing more.
	assert.Equal(t, 0, a.Tick(ctx))
	a.Wait()

	after, err := f.dispatch.schemaSvc.GetTransferSchedule(ctx, sched.ID)
	require.NoError(t, err)
	require.NotNil(t, after.NextRunAt)
	assert.True(t, after.NextRunAt.After(due), "the next run moved past the one that ran")
}

func TestScheduler_LeavesDisabledAndNotYetDueSchedulesAlone(t *testing.T) {
	mgr, ctx := transferTestManager(t)
	if mgr == nil {
		return
	}
	f := newScheduleFixture(t, ctx, mgr)
	sched := f.schedule(t, ctx, "0 2 * * *")

	s := transfer.NewScheduler(mgr)
	s.Databases = func(context.Context) ([]string, error) { return []string{f.dispatch.name}, nil }
	s.Logf = t.Logf

	// Before it is due.
	s.Now = func() time.Time { return sched.NextRunAt.Add(-time.Minute) }
	assert.Equal(t, 0, s.Tick(ctx))

	// Due, but switched off.
	off := false
	_, err := f.dispatch.schemaSvc.UpdateTransferSchedule(ctx, sched.ID, schema.TransferSchedulePatch{Enabled: &off})
	require.NoError(t, err)
	s.Now = func() time.Time { return sched.NextRunAt.Add(time.Hour) }
	assert.Equal(t, 0, s.Tick(ctx))
	s.Wait()

	assert.Empty(t, rows(t, ctx, f.results.dataSvc, "project_surveys"))
}
