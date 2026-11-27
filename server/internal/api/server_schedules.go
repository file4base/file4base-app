package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/cronexpr"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// Server schedules (#188, #204), FileMaker Server's Schedules: backups of
// some or all of the server's solutions, kept to a number of copies, and
// their maintenance, run by one goroutine of the server, one job at a time.
//
// A schedule's next run is stored with it, in the control store, and is
// moved to the following occurrence before the run starts, so a run is
// never repeated, not even when the server stops during it. An occurrence
// the server was not running at, or that came while the schedule's previous
// run was still going, is recorded as skipped and not run late: a server
// started after a weekend off runs nothing it missed.

// ScheduleOptions starts the server schedules.
type ScheduleOptions struct {
	// Context runs them until it ends.
	Context context.Context
	// Interval is how often due schedules are looked for (default 20 s).
	Interval time.Duration
	// Location is the time zone their times are read in (default the
	// server's, TZ).
	Location *time.Location
	// Now is the clock. Tests set it.
	Now func() time.Time
}

// missedAfter is how late a run can start after the server starts: an
// occurrence older than that was missed while the server was down.
const missedAfter = 5 * time.Minute

// The maintenance operations.
const (
	maintenanceVacuumAnalyze = "vacuum_analyze"
	maintenanceAnalyze       = "analyze"
)

type serverScheduler struct {
	h        *ServerAdminHandler
	interval time.Duration
	loc      *time.Location
	now      func() time.Time
	wake     chan struct{}

	mu      sync.Mutex
	started bool
	running string   // the schedule running now
	queued  []string // run-now requests, in order
}

func newServerScheduler(h *ServerAdminHandler) *serverScheduler {
	return &serverScheduler{
		h: h, interval: 20 * time.Second, loc: time.Local,
		now:  func() time.Time { return time.Now().UTC() },
		wake: make(chan struct{}, 1),
	}
}

// StartSchedules runs the server schedules until opts.Context ends.
func (h *ServerAdminHandler) StartSchedules(opts ScheduleOptions) {
	s := h.scheduler
	if opts.Interval > 0 {
		s.interval = opts.Interval
	}
	if opts.Location != nil {
		s.loc = opts.Location
	}
	if opts.Now != nil {
		s.now = opts.Now
	}
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	go s.run(opts.Context)
}

func (s *serverScheduler) logf(format string, args ...interface{}) {
	log.Printf("server schedules: "+format, args...)
}

func (s *serverScheduler) run(ctx context.Context) {
	store := s.h.controlStore
	if n, err := store.InterruptScheduleRuns(ctx); err != nil {
		s.logf("the runs the server stopped during could not be recorded: %v", err)
	} else if n > 0 {
		s.logf("%d run(s) were stopped by the last shutdown", n)
	}
	s.skipMissed(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		s.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}

// skipMissed records the occurrences that passed while the server was not
// running and moves their schedules on, without running them.
func (s *serverScheduler) skipMissed(ctx context.Context) {
	list, err := s.h.controlStore.ListSchedules(ctx)
	if err != nil {
		s.logf("the schedules could not be read: %v", err)
		return
	}
	now := s.now()
	for _, sc := range list {
		if !sc.Enabled || sc.NextRunAt == nil || now.Sub(*sc.NextRunAt) <= missedAfter {
			continue
		}
		s.skip(ctx, sc, *sc.NextRunAt, now, fmt.Sprintf("the server was not running at %s",
			sc.NextRunAt.In(s.loc).Format("2006-01-02 15:04 MST")))
	}
}

// skip records that sc's occurrence due was not run, and moves sc to its
// next one after now.
func (s *serverScheduler) skip(ctx context.Context, sc control.ServerSchedule, due, now time.Time, why string) {
	claimed, err := s.h.controlStore.ClaimScheduleRun(ctx, sc.ID, due, s.nextAfter(sc.Cron, now))
	if err != nil || !claimed {
		return
	}
	_ = s.h.controlStore.FinishScheduleRun(ctx, control.ScheduleRun{
		ScheduleID: sc.ID, Trigger: scheduleTriggerSchedule, Status: control.RunSkipped,
		StartedAt: due, FinishedAt: &due, Error: why,
	})
	s.h.recordAudit("schedule", "Skip Scheduled Job", "scheduler", "", sc.Name, "warning", why)
}

func (s *serverScheduler) nextAfter(expr string, t time.Time) *time.Time {
	cron, err := cronexpr.Parse(expr)
	if err != nil {
		return nil
	}
	n := cron.NextIn(t, s.loc)
	if n.IsZero() {
		return nil
	}
	return &n
}

const (
	scheduleTriggerSchedule = "schedule"
	scheduleTriggerManual   = "manual"
)

// tick runs the run-now requests, then every schedule that is due, one
// after the other.
func (s *serverScheduler) tick(ctx context.Context) {
	for ctx.Err() == nil {
		id, ok := s.popQueued()
		if !ok {
			break
		}
		if sc, err := s.h.controlStore.GetSchedule(ctx, id); err == nil {
			s.runOne(ctx, *sc, scheduleTriggerManual)
		}
		s.setRunning("")
	}
	list, err := s.h.controlStore.ListSchedules(ctx)
	if err != nil {
		s.logf("the schedules could not be read: %v", err)
		return
	}
	for _, sc := range list {
		if ctx.Err() != nil {
			return
		}
		now := s.now()
		if !sc.Enabled || sc.NextRunAt == nil || sc.NextRunAt.After(now) {
			continue
		}
		claimed, err := s.h.controlStore.ClaimScheduleRun(ctx, sc.ID, *sc.NextRunAt, s.nextAfter(sc.Cron, now))
		if err != nil {
			s.logf("%s could not be claimed: %v", sc.Name, err)
			continue
		}
		if !claimed {
			continue
		}
		s.setRunning(sc.ID)
		s.runOne(ctx, sc, scheduleTriggerSchedule)
		s.setRunning("")
		// An occurrence that came while it ran is not run late.
		if after, err := s.h.controlStore.GetSchedule(ctx, sc.ID); err == nil && after.Enabled &&
			after.NextRunAt != nil && !after.NextRunAt.After(s.now()) {
			s.skip(ctx, *after, *after.NextRunAt, s.now(), "its previous run was still running")
		}
	}
}

func (s *serverScheduler) popQueued() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queued) == 0 {
		return "", false
	}
	id := s.queued[0]
	s.queued = s.queued[1:]
	s.running = id
	return id, true
}

func (s *serverScheduler) setRunning(id string) {
	s.mu.Lock()
	s.running = id
	s.mu.Unlock()
}

// state says whether id is running or waiting to run.
func (s *serverScheduler) state(id string) (running, queued bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.queued {
		queued = queued || q == id
	}
	return s.running == id, queued
}

// enqueue asks for a run of id as soon as the scheduler is free.
func (s *serverScheduler) enqueue(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return errors.New("the schedules do not run in this server process (FILE4BASE_SERVER_SCHEDULER=false)")
	}
	if s.running == id {
		return errors.New("this schedule is running")
	}
	for _, q := range s.queued {
		if q == id {
			return errors.New("this schedule is already waiting to run")
		}
	}
	s.queued = append(s.queued, id)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// scheduleTarget is what one run did with one solution.
type scheduleTarget struct {
	Database  string   `json:"database"`
	Status    string   `json:"status"`
	BackupID  string   `json:"backup_id,omitempty"`
	SizeBytes int64    `json:"size_bytes,omitempty"`
	Removed   []string `json:"removed,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// runOne runs sc and records the run.
func (s *serverScheduler) runOne(ctx context.Context, sc control.ServerSchedule, trigger string) {
	store := s.h.controlStore
	started := time.Now()
	run := control.ScheduleRun{ScheduleID: sc.ID, Trigger: trigger, StartedAt: s.now()}
	id, err := store.StartScheduleRun(ctx, run)
	if err != nil {
		s.logf("%s: the run could not be recorded, so it was not run: %v", sc.Name, err)
		return
	}
	run.ID = id

	targets, err := s.targets(ctx, sc)
	var results []scheduleTarget
	if err == nil {
		for _, db := range targets {
			if ctx.Err() != nil {
				break
			}
			results = append(results, s.runFor(ctx, sc, db))
		}
	}

	// The run is recorded even if the server is stopping.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	run.Status = control.RunSuccess
	var failures []string
	for _, r := range results {
		run.SizeBytes += r.SizeBytes
		if r.Status != control.RunSuccess {
			failures = append(failures, r.Database+": "+r.Error)
		}
	}
	switch {
	case err != nil:
		run.Status, run.Error = control.RunFailed, err.Error()
	case ctx.Err() != nil:
		run.Status, run.Error = control.RunFailed, "the server stopped during this run"
	case len(targets) == 0:
		run.Status, run.Error = control.RunSkipped, "there is no solution to act on"
	case len(failures) > 0:
		run.Status = control.RunFailed
		run.Error = fmt.Sprintf("%d of %d solutions failed: %s", len(failures), len(targets), strings.Join(failures, "; "))
	}
	run.Details, _ = json.Marshal(results)
	finished := s.now()
	run.FinishedAt = &finished
	run.DurationMs = time.Since(started).Milliseconds()
	if err := store.FinishScheduleRun(recordCtx, run); err != nil {
		s.logf("%s: the outcome of its run could not be recorded: %v", sc.Name, err)
	}
	status := "success"
	if run.Status != control.RunSuccess {
		status = "failure"
		s.logf("%s %s: %s", sc.Name, run.Status, run.Error)
	}
	s.h.recordAudit("schedule", "Run Scheduled Job", "scheduler", "", sc.Name, status,
		fmt.Sprintf("%s run of %s %q: %s %s", trigger, sc.Type, sc.Name, run.Status, run.Error))
}

// targets are the solutions sc acts on: those it names, or every solution
// on the server.
func (s *serverScheduler) targets(ctx context.Context, sc control.ServerSchedule) ([]string, error) {
	if len(sc.Databases) > 0 {
		return sc.Databases, nil
	}
	all, err := s.h.dbMgr.ListDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("the solutions of the server could not be listed: %w", err)
	}
	var out []string
	for _, db := range all {
		if dbal.IsProtectedDatabase(db) {
			continue
		}
		driver, err := s.h.dbMgr.DriverFor(ctx, db)
		if err != nil || !schema.NewService(driver).HasSystemCatalog(ctx) {
			continue // not a File4Base solution
		}
		out = append(out, db)
	}
	sort.Strings(out)
	return out, nil
}

func (s *serverScheduler) runFor(ctx context.Context, sc control.ServerSchedule, db string) scheduleTarget {
	res := scheduleTarget{Database: db, Status: control.RunSuccess}
	fail := func(err error) scheduleTarget {
		res.Status, res.Error = control.RunFailed, err.Error()
		return res
	}
	if s.h.isRestoring(db) {
		return fail(errors.New("a restore of it was running"))
	}
	switch sc.Type {
	case control.ScheduleBackup:
		now := s.now()
		rec, err := s.h.writeSnapshotFor(ctx, db, "scheduled", "scheduler",
			"Scheduled backup "+sc.Name, "scheduled", sc.ID, now)
		if err != nil {
			return fail(err)
		}
		res.BackupID, res.SizeBytes = rec.ID, rec.SizeBytes
		removed, err := s.h.pruneScheduledBackups(sc, db)
		res.Removed = removed
		if err != nil {
			return fail(fmt.Errorf("backed up as %s, but the older backups could not be removed: %w", rec.ID, err))
		}
	case control.ScheduleMaintenance:
		if err := s.h.optimizeDatabase(ctx, db, sc.Operation); err != nil {
			return fail(err)
		}
	default:
		return fail(fmt.Errorf("a %s schedule cannot run", sc.Type))
	}
	return res
}

// pruneScheduledBackups keeps the sc.Keep latest backups sc made of db,
// and deletes the others. Only backups of that schedule are counted or
// deleted: manual, pre-update, pre-restore and uploaded backups never are.
func (h *ServerAdminHandler) pruneScheduledBackups(sc control.ServerSchedule, db string) ([]string, error) {
	if sc.Keep <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(h.backupsDir)
	if err != nil {
		return nil, err
	}
	type found struct{ id, created string }
	var mine []found
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".f4b.json") {
			continue
		}
		hdr, err := readBackupHeader(filepath.Join(h.backupsDir, e.Name()))
		if err != nil || hdr.Type != "scheduled" || hdr.ScheduleID != sc.ID || !strings.EqualFold(hdr.Database, db) {
			continue
		}
		mine = append(mine, found{strings.TrimSuffix(e.Name(), ".f4b.json"), hdr.CreatedAt})
	}
	sort.Slice(mine, func(i, j int) bool {
		if mine[i].created != mine[j].created {
			return mine[i].created > mine[j].created
		}
		return mine[i].id > mine[j].id
	})
	var removed []string
	for i := sc.Keep; i < len(mine); i++ {
		if err := os.Remove(filepath.Join(h.backupsDir, mine[i].id+".f4b.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		removed = append(removed, mine[i].id)
	}
	return removed, nil
}

// optimizeDatabase refreshes the planner statistics of db: VACUUM ANALYZE
// or ANALYZE on PostgreSQL, ANALYZE TABLE of every table on MariaDB (which
// has no VACUUM; OPTIMIZE TABLE rebuilds tables and is not run).
func (h *ServerAdminHandler) optimizeDatabase(ctx context.Context, db, op string) error {
	driver, err := h.dbMgr.DriverFor(ctx, db)
	if err != nil {
		return fmt.Errorf("the database is not reachable: %w", err)
	}
	if h.dbMgr.Engine() == dbal.EnginePostgres {
		stmt := "ANALYZE"
		if op != maintenanceAnalyze {
			stmt = "VACUUM ANALYZE"
		}
		if _, err := driver.DB().ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s failed: %w", stmt, err)
		}
		return nil
	}
	rows, err := driver.DB().QueryContext(ctx, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_type = 'BASE TABLE' ORDER BY table_name`)
	if err != nil {
		return fmt.Errorf("the tables could not be listed: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, "`"+strings.ReplaceAll(name, "`", "``")+"`")
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for start := 0; start < len(tables); start += 50 {
		end := start + 50
		if end > len(tables) {
			end = len(tables)
		}
		if err := analyzeTables(ctx, driver, tables[start:end]); err != nil {
			return err
		}
	}
	return nil
}

// analyzeTables runs ANALYZE TABLE, which reports a table it could not
// analyze as a row of its result rather than as an error.
func analyzeTables(ctx context.Context, driver dbal.DatabaseDriver, tables []string) error {
	rows, err := driver.DB().QueryContext(ctx, "ANALYZE TABLE "+strings.Join(tables, ", "))
	if err != nil {
		return fmt.Errorf("ANALYZE TABLE failed: %w", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		texts := make([]*string, len(cols))
		for i := range vals {
			texts[i] = new(string)
			vals[i] = &nullableText{dst: texts[i]}
		}
		if err := rows.Scan(vals...); err != nil {
			return err
		}
		// Table, Op, Msg_type, Msg_text.
		if len(cols) >= 4 && strings.EqualFold(*texts[2], "error") {
			return fmt.Errorf("ANALYZE TABLE %s: %s", *texts[0], *texts[3])
		}
	}
	return rows.Err()
}

// nullableText scans any value as text, NULL as "".
type nullableText struct{ dst *string }

func (n *nullableText) Scan(v interface{}) error {
	switch t := v.(type) {
	case nil:
		*n.dst = ""
	case []byte:
		*n.dst = string(t)
	default:
		*n.dst = fmt.Sprint(t)
	}
	return nil
}

// ---- HTTP ------------------------------------------------------------------

// scheduleView is a schedule as the API answers it.
type scheduleView struct {
	control.ServerSchedule
	Running bool                 `json:"running"`
	Queued  bool                 `json:"queued"`
	LastRun *control.ScheduleRun `json:"last_run"`
}

func (h *ServerAdminHandler) viewSchedule(sc control.ServerSchedule, last map[string]control.ScheduleRun) scheduleView {
	v := scheduleView{ServerSchedule: sc}
	v.Running, v.Queued = h.scheduler.state(sc.ID)
	if run, ok := last[sc.ID]; ok {
		v.LastRun = &run
	}
	return v
}

// ListSchedules lists the server schedules with their next and last runs.
func (h *ServerAdminHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	list, err := h.controlStore.ListSchedules(r.Context())
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusInternalServerError, "Schedules Unavailable", err.Error())
		return
	}
	last, err := h.controlStore.LastScheduleRuns(r.Context())
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusInternalServerError, "Schedules Unavailable", err.Error())
		return
	}
	views := make([]scheduleView, 0, len(list))
	for _, sc := range list {
		views = append(views, h.viewSchedule(sc, last))
	}
	h.scheduler.mu.Lock()
	started := h.scheduler.started
	h.scheduler.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schedules": views, "time_zone": h.scheduler.loc.String(), "scheduler_running": started,
	})
}

// scheduleInput is what creating or changing a schedule sends; a change
// leaves out what it keeps.
type scheduleInput struct {
	Name      *string   `json:"name"`
	Type      *string   `json:"type"`
	Databases *[]string `json:"databases"`
	Cron      *string   `json:"cron"`
	Keep      *int      `json:"keep"`
	Operation *string   `json:"operation"`
	Enabled   *bool     `json:"enabled"`
}

// apply checks in and applies it to sc.
func (h *ServerAdminHandler) applyScheduleInput(ctx context.Context, sc *control.ServerSchedule, in scheduleInput) error {
	if in.Name != nil {
		sc.Name = strings.TrimSpace(*in.Name)
	}
	if sc.Name == "" {
		return errors.New("a schedule needs a name")
	}
	if in.Type != nil {
		sc.Type = strings.TrimSpace(*in.Type)
	}
	switch sc.Type {
	case control.ScheduleBackup, control.ScheduleMaintenance:
	case "script":
		return errors.New("script schedules need the server-side script engine, which File4Base does not have yet (#204)")
	default:
		return fmt.Errorf("a schedule is a backup or a maintenance, not %q", sc.Type)
	}
	if in.Cron != nil {
		cron, err := cronexpr.Parse(*in.Cron)
		if err != nil {
			return err
		}
		sc.Cron = cron.String()
	}
	if sc.Cron == "" {
		return errors.New("a schedule needs a cron expression")
	}
	if in.Databases != nil {
		known, err := h.dbMgr.ListDatabases(ctx)
		if err != nil {
			return fmt.Errorf("the solutions of the server could not be listed: %w", err)
		}
		exists := map[string]string{}
		for _, db := range known {
			exists[strings.ToLower(db)] = db
		}
		seen := map[string]bool{}
		sc.Databases = []string{}
		for _, db := range *in.Databases {
			db = strings.TrimSpace(db)
			name, ok := exists[strings.ToLower(db)]
			if db == "" || seen[strings.ToLower(db)] {
				continue
			}
			if !ok || dbal.IsProtectedDatabase(name) {
				return fmt.Errorf("there is no solution %q on this server", db)
			}
			seen[strings.ToLower(db)] = true
			sc.Databases = append(sc.Databases, name)
		}
	}
	if in.Keep != nil {
		sc.Keep = *in.Keep
	}
	if in.Operation != nil {
		sc.Operation = strings.TrimSpace(*in.Operation)
	}
	switch sc.Type {
	case control.ScheduleBackup:
		if sc.Keep == 0 {
			sc.Keep = 7
		}
		if sc.Keep < 1 || sc.Keep > 1000 {
			return errors.New("a backup schedule keeps from 1 to 1000 backups of each solution")
		}
		sc.Operation = ""
	case control.ScheduleMaintenance:
		sc.Keep = 0
		if sc.Operation == "" {
			sc.Operation = maintenanceVacuumAnalyze
		}
		if sc.Operation != maintenanceVacuumAnalyze && sc.Operation != maintenanceAnalyze {
			return fmt.Errorf("a maintenance schedule runs %q or %q, not %q",
				maintenanceVacuumAnalyze, maintenanceAnalyze, sc.Operation)
		}
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
	}
	sc.NextRunAt = nil
	if sc.Enabled {
		sc.NextRunAt = h.scheduler.nextAfter(sc.Cron, h.scheduler.now())
	}
	return nil
}

func (h *ServerAdminHandler) writeScheduleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, control.ErrScheduleNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Schedule Not Found", "no schedule has this id")
	case errors.Is(err, control.ErrScheduleNameTaken):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Name Taken", err.Error())
	default:
		telemetry.WriteProblem(w, r, http.StatusInternalServerError, "Schedule Not Saved", err.Error())
	}
}

// CreateSchedule creates a server schedule.
func (h *ServerAdminHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	var in scheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}
	sc := control.ServerSchedule{ID: uuid.NewString(), Enabled: true, CreatedBy: sess.Username}
	if err := h.applyScheduleInput(r.Context(), &sc, in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Validation Failed", err.Error())
		return
	}
	if err := h.controlStore.CreateSchedule(r.Context(), sc); err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	h.recordAudit("schedule", "Create Scheduled Job", sess.Username, sess.Database, sc.Name, "success",
		fmt.Sprintf("Created the %s schedule %q (%s)", sc.Type, sc.Name, sc.Cron))
	saved, err := h.controlStore.GetSchedule(r.Context(), sc.ID)
	if err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, h.viewSchedule(*saved, nil))
}

// UpdateSchedule changes a schedule: its settings, or whether it is enabled.
func (h *ServerAdminHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	sc, err := h.controlStore.GetSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	var in scheduleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}
	if err := h.applyScheduleInput(r.Context(), sc, in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Validation Failed", err.Error())
		return
	}
	if err := h.controlStore.UpdateSchedule(r.Context(), *sc); err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	state := "enabled"
	if !sc.Enabled {
		state = "disabled"
	}
	h.recordAudit("schedule", "Update Scheduled Job", sess.Username, sess.Database, sc.Name, "success",
		fmt.Sprintf("The %s schedule %q is %s (%s)", sc.Type, sc.Name, state, sc.Cron))
	last, _ := h.controlStore.LastScheduleRuns(r.Context())
	writeJSON(w, http.StatusOK, h.viewSchedule(*sc, last))
}

// RunScheduleNow runs a schedule as soon as the scheduler is free, whether
// it is enabled or not, and answers at once: its outcome is its last run.
func (h *ServerAdminHandler) RunScheduleNow(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	sc, err := h.controlStore.GetSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	if err := h.scheduler.enqueue(sc.ID); err != nil {
		telemetry.WriteProblem(w, r, http.StatusConflict, "Not Run", err.Error())
		return
	}
	h.recordAudit("schedule", "Run Scheduled Job", sess.Username, sess.Database, sc.Name, "success",
		fmt.Sprintf("Asked to run the %s schedule %q now", sc.Type, sc.Name))
	writeJSON(w, http.StatusAccepted, map[string]interface{}{"id": sc.ID, "status": "queued"})
}

// ListScheduleRuns gives the latest runs of a schedule.
func (h *ServerAdminHandler) ListScheduleRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.controlStore.GetSchedule(r.Context(), id); err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	runs, err := h.controlStore.ListScheduleRuns(r.Context(), id, 0)
	if err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// DeleteSchedule deletes a schedule and its history. The backups it made
// are kept.
func (h *ServerAdminHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	sc, err := h.controlStore.GetSchedule(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	if running, _ := h.scheduler.state(sc.ID); running {
		telemetry.WriteProblem(w, r, http.StatusConflict, "Schedule Running", "this schedule is running; delete it once its run ends")
		return
	}
	if err := h.controlStore.DeleteSchedule(r.Context(), sc.ID); err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	h.recordAudit("schedule", "Delete Scheduled Job", sess.Username, sess.Database, sc.Name, "success",
		fmt.Sprintf("Deleted the %s schedule %q", sc.Type, sc.Name))
	w.WriteHeader(http.StatusNoContent)
}
