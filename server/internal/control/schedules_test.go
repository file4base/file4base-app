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

	claimed, err := store.ClaimScheduleRun(ctx, "s1", due, &after)
	require.NoError(t, err)
	assert.True(t, claimed)
	claimed, err = store.ClaimScheduleRun(ctx, "s1", due, &after)
	require.NoError(t, err)
	assert.False(t, claimed, "the same occurrence is not claimed twice")
	got, err := store.GetSchedule(ctx, "s1")
	require.NoError(t, err)
	assert.True(t, got.NextRunAt.Equal(after))
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
