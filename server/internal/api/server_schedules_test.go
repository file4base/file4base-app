package api_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/control"
)

// Server schedules (#188, #204): backups and maintenance, run by the server
// on their schedule, recorded, and never run twice or late.

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.t = t
	c.mu.Unlock()
}

type scheduleJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	Databases []string   `json:"databases"`
	Cron      string     `json:"cron"`
	Keep      int        `json:"keep"`
	Operation string     `json:"operation"`
	Enabled   bool       `json:"enabled"`
	NextRunAt *time.Time `json:"next_run_at"`
	Running   bool       `json:"running"`
	Queued    bool       `json:"queued"`
	LastRun   *struct {
		Status     string `json:"status"`
		Trigger    string `json:"trigger"`
		Error      string `json:"error"`
		DurationMs int64  `json:"duration_ms"`
		SizeBytes  int64  `json:"size_bytes"`
	} `json:"last_run"`
}

type runJSON struct {
	Trigger   string `json:"trigger"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	SizeBytes int64  `json:"size_bytes"`
}

var paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return loc
}()

// scheduleHarness is a server whose schedules run on clock, over store.
type scheduleHarness struct {
	*serverToolsHarness
	clock *fakeClock
	store *control.Store
}

func newScheduleHarness(t *testing.T, store *control.Store, start time.Time) *scheduleHarness {
	t.Helper()
	if store == nil {
		var err error
		store, err = control.NewStore(":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = store.Close() })
	}
	clock := &fakeClock{t: start}
	ctx, cancel := context.WithCancel(context.Background())
	s := newServerToolsHarnessWith(t, true, func(o *api.Options) {
		o.ControlStore = store
		o.Schedules = &api.ScheduleOptions{Context: ctx, Interval: 10 * time.Millisecond, Location: paris, Now: clock.Now}
	})
	h := &scheduleHarness{serverToolsHarness: s, clock: clock, store: store}
	// The scheduler stops, and its run ends, before the databases go.
	t.Cleanup(func() {
		cancel()
		assert.Eventually(t, func() bool {
			for _, sc := range h.list(t) {
				if sc.Running {
					return false
				}
			}
			return true
		}, 30*time.Second, 20*time.Millisecond)
	})
	return h
}

func (h *scheduleHarness) list(t *testing.T) []scheduleJSON {
	t.Helper()
	var res struct {
		Schedules []scheduleJSON `json:"schedules"`
	}
	rec := h.do(http.MethodGet, "/api/v1/server/schedules", h.adminToken, nil, &res)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return res.Schedules
}

func (h *scheduleHarness) get(t *testing.T, id string) scheduleJSON {
	t.Helper()
	for _, sc := range h.list(t) {
		if sc.ID == id {
			return sc
		}
	}
	t.Fatalf("no schedule %s", id)
	return scheduleJSON{}
}

func (h *scheduleHarness) runs(t *testing.T, id string) []runJSON {
	t.Helper()
	var res struct {
		Runs []runJSON `json:"runs"`
	}
	rec := h.do(http.MethodGet, "/api/v1/server/schedules/"+id+"/runs", h.adminToken, nil, &res)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return res.Runs
}

// waitRuns waits until id has n finished runs, and gives them.
func (h *scheduleHarness) waitRuns(t *testing.T, id string, n int) []runJSON {
	t.Helper()
	var runs []runJSON
	require.Eventually(t, func() bool {
		runs = h.runs(t, id)
		if len(runs) < n {
			return false
		}
		for _, r := range runs {
			if r.Status == control.RunRunning {
				return false
			}
		}
		return true
	}, 2*time.Minute, 20*time.Millisecond, "waiting for %d runs", n)
	return runs
}

func (h *scheduleHarness) create(t *testing.T, body map[string]interface{}) scheduleJSON {
	t.Helper()
	var sc scheduleJSON
	rec := h.do(http.MethodPost, "/api/v1/server/schedules", h.adminToken, body, &sc)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return sc
}

func (h *scheduleHarness) backupsOf(t *testing.T, db string) map[string]string {
	t.Helper()
	var res struct {
		Backups []struct {
			ID         string `json:"id"`
			Database   string `json:"database"`
			Type       string `json:"type"`
			ScheduleID string `json:"schedule_id"`
		} `json:"backups"`
	}
	rec := h.do(http.MethodGet, "/api/v1/server/backups", h.adminToken, nil, &res)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	out := map[string]string{}
	for _, b := range res.Backups {
		if b.Database == db {
			out[b.ID] = b.Type + ":" + b.ScheduleID
		}
	}
	return out
}

// A backup schedule backs up its solutions at its time, in the server's
// time zone, records what it did, and keeps the copies it is told to keep,
// never touching the other backups.
func TestSchedules_BackUpOnTimeAndKeepTheirCopies(t *testing.T) {
	// 01:59 in Paris, in winter.
	h := newScheduleHarness(t, nil, time.Date(2026, 11, 27, 0, 59, 0, 0, time.UTC))
	code, manual := h.backup(h.adminToken, nil)
	require.Equal(t, http.StatusCreated, code)

	sc := h.create(t, map[string]interface{}{
		"name": "Nightly", "type": "backup", "databases": []string{h.adminDB}, "cron": "0 2 * * *", "keep": 2,
	})
	require.NotNil(t, sc.NextRunAt)
	assert.True(t, sc.NextRunAt.Equal(time.Date(2026, 11, 27, 1, 0, 0, 0, time.UTC)), "02:00 in Paris is %v", sc.NextRunAt)
	assert.Empty(t, h.runs(t, sc.ID), "nothing runs before its time")

	for day := 0; day < 3; day++ {
		h.clock.Set(time.Date(2026, 11, 27+day, 1, 0, 5, 0, time.UTC))
		runs := h.waitRuns(t, sc.ID, day+1)
		assert.Equal(t, control.RunSuccess, runs[0].Status, runs[0].Error)
		assert.Equal(t, "schedule", runs[0].Trigger)
		assert.Positive(t, runs[0].SizeBytes)
	}
	got := h.get(t, sc.ID)
	require.NotNil(t, got.LastRun)
	assert.Equal(t, control.RunSuccess, got.LastRun.Status)
	assert.Positive(t, got.LastRun.SizeBytes)
	assert.GreaterOrEqual(t, got.LastRun.DurationMs, int64(0))
	assert.True(t, got.NextRunAt.Equal(time.Date(2026, 11, 30, 1, 0, 0, 0, time.UTC)), "%v", got.NextRunAt)

	backups := h.backupsOf(t, h.adminDB)
	scheduled := 0
	for _, kind := range backups {
		if kind == "scheduled:"+sc.ID {
			scheduled++
		}
	}
	assert.Equal(t, 2, scheduled, "the schedule keeps its two latest copies: %v", backups)
	assert.Contains(t, backups, manual, "a manual backup is never removed by a schedule")
	assert.Len(t, h.runs(t, sc.ID), 3, "one run a day, each once")
}

// The occurrence a server was down for is recorded as skipped, not run when
// it starts again; the run it stopped during is recorded as failed.
func TestSchedules_WhatTheServerWasDownForIsSkipped(t *testing.T) {
	store, err := control.NewStore(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	now := time.Date(2026, 11, 30, 8, 0, 0, 0, time.UTC)
	missed := time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC) // two days ago
	require.NoError(t, store.CreateSchedule(ctx, control.ServerSchedule{
		ID: "weekend", Name: "Nightly", Type: control.ScheduleBackup, Databases: []string{"f4b_not_there"},
		Cron: "0 2 * * *", Keep: 7, Enabled: true, NextRunAt: &missed,
	}))
	_, err = store.StartScheduleRun(ctx, control.ScheduleRun{ScheduleID: "weekend", Trigger: "schedule", StartedAt: missed.Add(-24 * time.Hour)})
	require.NoError(t, err)

	h := newScheduleHarness(t, store, now)
	runs := h.waitRuns(t, "weekend", 2)
	require.Len(t, runs, 2)
	assert.Equal(t, control.RunSkipped, runs[0].Status)
	assert.Contains(t, runs[0].Error, "not running")
	assert.Equal(t, control.RunFailed, runs[1].Status)
	assert.Contains(t, runs[1].Error, "stopped")

	got := h.get(t, "weekend")
	require.NotNil(t, got.NextRunAt)
	assert.True(t, got.NextRunAt.Equal(time.Date(2026, 12, 1, 1, 0, 0, 0, time.UTC)), "%v", got.NextRunAt)
	time.Sleep(100 * time.Millisecond)
	assert.Len(t, h.runs(t, "weekend"), 2, "nothing was caught up")
}

// One schedule never runs twice at once: run now is refused while it runs,
// and its own occurrence that comes meanwhile is skipped, not run late.
func TestSchedules_NeverRunTwiceAtOnce(t *testing.T) {
	var block atomic.Bool
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	defer api.SetBackupHookForTest(func() {
		if block.Load() {
			entered <- struct{}{}
			<-release
		}
	})()

	h := newScheduleHarness(t, nil, time.Date(2026, 11, 27, 9, 59, 0, 0, time.UTC))
	sc := h.create(t, map[string]interface{}{
		"name": "Hourly", "type": "backup", "databases": []string{h.aliceDB}, "cron": "0 * * * *", "keep": 5,
	})
	block.Store(true)
	h.clock.Set(time.Date(2026, 11, 27, 10, 0, 1, 0, time.UTC))
	select {
	case <-entered:
	case <-time.After(time.Minute):
		t.Fatal("the run did not start")
	}
	assert.True(t, h.get(t, sc.ID).Running)

	rec := h.do(http.MethodPost, "/api/v1/server/schedules/"+sc.ID+"/run", h.adminToken, nil, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	rec = h.do(http.MethodDelete, "/api/v1/server/schedules/"+sc.ID, h.adminToken, nil, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "a running schedule is not deleted")

	// The next hour comes while the run is still going.
	h.clock.Set(time.Date(2026, 11, 27, 11, 0, 30, 0, time.UTC))
	block.Store(false)
	close(release)

	runs := h.waitRuns(t, sc.ID, 2)
	assert.Equal(t, control.RunSkipped, runs[0].Status)
	assert.Contains(t, runs[0].Error, "still running")
	assert.Equal(t, control.RunSuccess, runs[1].Status, runs[1].Error)
	got := h.get(t, sc.ID)
	assert.True(t, got.NextRunAt.Equal(time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)), "%v", got.NextRunAt)

	// Run now runs it once more, whenever asked, and says so.
	rec = h.do(http.MethodPost, "/api/v1/server/schedules/"+sc.ID+"/run", h.adminToken, nil, nil)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	runs = h.waitRuns(t, sc.ID, 3)
	assert.Equal(t, "manual", runs[0].Trigger)
	assert.Equal(t, control.RunSuccess, runs[0].Status, runs[0].Error)
}

// Maintenance refreshes the planner statistics on either engine.
func TestSchedules_MaintenanceRuns(t *testing.T) {
	h := newScheduleHarness(t, nil, time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC))
	for _, op := range []string{"vacuum_analyze", "analyze"} {
		sc := h.create(t, map[string]interface{}{
			"name": "Statistics " + op, "type": "maintenance", "databases": []string{h.aliceDB, h.bobDB},
			"cron": "30 4 * * 0", "operation": op, "enabled": false,
		})
		assert.Nil(t, sc.NextRunAt, "a disabled schedule has no next run")
		rec := h.do(http.MethodPost, "/api/v1/server/schedules/"+sc.ID+"/run", h.adminToken, nil, nil)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		runs := h.waitRuns(t, sc.ID, 1)
		assert.Equal(t, control.RunSuccess, runs[0].Status, runs[0].Error)
	}
}

// What a schedule is, is checked before it is stored.
func TestSchedules_AreCheckedWhenSaved(t *testing.T) {
	h := newScheduleHarness(t, nil, time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC))
	bad := []struct {
		body map[string]interface{}
		want string
	}{
		{map[string]interface{}{"name": "Script", "type": "script", "cron": "0 2 * * *"}, "#204"},
		{map[string]interface{}{"name": "Nobody", "type": "backup", "cron": "0 2 * * *", "databases": []string{"f4b_none"}}, "no solution"},
		{map[string]interface{}{"name": "Bad cron", "type": "backup", "cron": "every night"}, ""},
		{map[string]interface{}{"name": "Too many", "type": "backup", "cron": "0 2 * * *", "keep": 5000}, "1000"},
		{map[string]interface{}{"name": "Rebuild", "type": "maintenance", "cron": "0 2 * * *", "operation": "reindex"}, "reindex"},
		{map[string]interface{}{"type": "backup", "cron": "0 2 * * *"}, "name"},
	}
	for _, c := range bad {
		rec := h.do(http.MethodPost, "/api/v1/server/schedules", h.adminToken, c.body, nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%v: %s", c.body, rec.Body.String())
		assert.Contains(t, rec.Body.String(), c.want)
	}

	var res struct {
		TimeZone         string `json:"time_zone"`
		SchedulerRunning bool   `json:"scheduler_running"`
	}
	rec := h.do(http.MethodGet, "/api/v1/server/schedules", h.adminToken, nil, &res)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Europe/Paris", res.TimeZone)
	assert.True(t, res.SchedulerRunning)

	sc := h.create(t, map[string]interface{}{"name": "Nightly", "type": "backup", "cron": "0 2 * * *"})
	assert.Equal(t, 7, sc.Keep, "seven copies by default")
	assert.Empty(t, sc.Databases, "all solutions")
	rec = h.do(http.MethodPost, "/api/v1/server/schedules", h.adminToken,
		map[string]interface{}{"name": "NIGHTLY", "type": "backup", "cron": "0 3 * * *"}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)

	var changed scheduleJSON
	rec = h.do(http.MethodPut, "/api/v1/server/schedules/"+sc.ID, h.adminToken, map[string]interface{}{"enabled": false}, &changed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, changed.Enabled)
	assert.Nil(t, changed.NextRunAt)
	assert.Equal(t, "0 2 * * *", changed.Cron, "a change keeps what it leaves out")

	rec = h.do(http.MethodDelete, "/api/v1/server/schedules/"+sc.ID, h.adminToken, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = h.do(http.MethodGet, "/api/v1/server/schedules/"+sc.ID+"/runs", h.adminToken, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
