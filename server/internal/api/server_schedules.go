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
	// Stopped, if set, is closed once the scheduler has stopped and its
	// last run is recorded.
	Stopped chan struct{}
}

// missedAfter is how late a run can start after the server starts: an
// occurrence older than that was missed while the server was down. A
// server restarted within it (an upgrade) still runs the occurrence.
const missedAfter = 5 * time.Minute

// The maintenance operations.
const (
	maintenanceVacuumAnalyze = "vacuum_analyze"
	maintenanceAnalyze       = "analyze"
)

var errScheduleBusy = errors.New("this schedule is running; delete it once its run ends")

type serverScheduler struct {
	h        *ServerAdminHandler
	interval time.Duration
	loc      *time.Location
	now      func() time.Time
	wake     chan struct{}

	// mu guards what follows. A schedule is marked running before its
	// occurrence is claimed, and deleted only while it is not, under mu, so
	// a schedule is never deleted while it runs.
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

// StartSchedules runs the server schedules until opts.Context ends. It
// starts them once: a second call does nothing.
func (h *ServerAdminHandler) StartSchedules(opts ScheduleOptions) {
	s := h.scheduler
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	if opts.Interval > 0 {
		s.interval = opts.Interval
	}
	if opts.Location != nil {
		s.loc = opts.Location
	}
	if opts.Now != nil {
		s.now = opts.Now
	}
	s.started = true
	go s.run(opts.Context, opts.Stopped)
}

func (s *serverScheduler) logf(format string, args ...interface{}) {
	log.Printf("server schedules: "+format, args...)
}

func (s *serverScheduler) run(ctx context.Context, stopped chan struct{}) {
	defer func() {
		s.mu.Lock()
		s.started, s.queued = false, nil
		s.mu.Unlock()
		if stopped != nil {
			close(stopped)
		}
	}()
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
// running and moves their schedules on, without running them: one skipped
// run per schedule, from the first occurrence missed.
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
	claimed, _, err := s.h.controlStore.ClaimScheduleRun(ctx, sc.ID, due, s.nextAfter(sc.Cron, now),
		control.ScheduleRun{ScheduleID: sc.ID, Trigger: scheduleTriggerSchedule, Status: control.RunSkipped,
			StartedAt: due, Error: why})
	if err != nil {
		s.logf("%s: its skipped occurrence could not be recorded: %v", sc.Name, err)
		return
	}
	if claimed != nil {
		s.h.recordAudit("schedule", "Skip Scheduled Job", "scheduler", "", sc.Name, "warning", why)
	}
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
	store := s.h.controlStore
	for ctx.Err() == nil {
		id, ok := s.popQueued()
		if !ok {
			break
		}
		started := s.now()
		if sc, err := store.GetSchedule(ctx, id); err == nil {
			runID, err := store.StartScheduleRun(ctx, control.ScheduleRun{ScheduleID: id, Trigger: scheduleTriggerManual, StartedAt: started})
			if err != nil {
				s.logf("%s: the run could not be recorded, so it was not run: %v", sc.Name, err)
			} else {
				s.runOne(ctx, *sc, scheduleTriggerManual, runID)
				s.skipOverlap(ctx, id, started)
			}
		}
		s.setRunning("")
	}
	list, err := store.ListSchedules(ctx)
	if err != nil {
		s.logf("the schedules could not be read: %v", err)
		return
	}
	for _, listed := range list {
		if ctx.Err() != nil {
			return
		}
		now := s.now()
		if !listed.Enabled || listed.NextRunAt == nil || listed.NextRunAt.After(now) {
			continue
		}
		s.setRunning(listed.ID)
		// The claim gives the schedule as it is now: a change saved since
		// the list was read (its retention) applies to this run.
		sc, runID, err := store.ClaimScheduleRun(ctx, listed.ID, *listed.NextRunAt, s.nextAfter(listed.Cron, now),
			control.ScheduleRun{ScheduleID: listed.ID, Trigger: scheduleTriggerSchedule, StartedAt: now})
		if err != nil {
			s.logf("%s could not be claimed: %v", listed.Name, err)
		}
		if sc != nil {
			s.runOne(ctx, *sc, scheduleTriggerSchedule, runID)
			s.skipOverlap(ctx, sc.ID, now)
		}
		s.setRunning("")
	}
}

// skipOverlap skips the occurrence of id that came while its run, begun at
// started, was going: it is not run late.
func (s *serverScheduler) skipOverlap(ctx context.Context, id string, started time.Time) {
	after, err := s.h.controlStore.GetSchedule(ctx, id)
	now := s.now()
	if err != nil || !after.Enabled || after.NextRunAt == nil || after.NextRunAt.After(now) || !after.NextRunAt.After(started) {
		return
	}
	s.skip(ctx, *after, *after.NextRunAt, now, "its previous run was still running")
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

func (s *serverScheduler) isStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// enqueue asks for a run of id as soon as the scheduler is free.
func (s *serverScheduler) enqueue(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return errors.New("the schedules do not run in this server process")
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

// deleteUnlessRunning deletes id with del, unless it is running; a run
// waiting for it is dropped.
func (s *serverScheduler) deleteUnlessRunning(id string, del func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running == id {
		return errScheduleBusy
	}
	if err := del(); err != nil {
		return err
	}
	kept := s.queued[:0]
	for _, q := range s.queued {
		if q != id {
			kept = append(kept, q)
		}
	}
	s.queued = kept
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

// runOne runs sc and records the outcome of its run runID.
func (s *serverScheduler) runOne(ctx context.Context, sc control.ServerSchedule, trigger string, runID int64) {
	store := s.h.controlStore
	started := time.Now()
	run := control.ScheduleRun{ID: runID, ScheduleID: sc.ID, Trigger: trigger}

	targets, unreadable, err := s.targets(ctx, sc)
	results := unreadable
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
	case len(results) == 0:
		run.Status, run.Error = control.RunSkipped, "there is no solution to act on"
	case len(failures) > 0:
		run.Status = control.RunFailed
		run.Error = fmt.Sprintf("%d of %d solutions failed: %s", len(failures), len(results), strings.Join(failures, "; "))
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
// on the server. A database that could not be checked is not left out: it
// is given as a failure of the run.
func (s *serverScheduler) targets(ctx context.Context, sc control.ServerSchedule) ([]string, []scheduleTarget, error) {
	if len(sc.Databases) > 0 {
		return sc.Databases, nil, nil
	}
	all, err := s.h.dbMgr.ListDatabases(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("the solutions of the server could not be listed: %w", err)
	}
	var out []string
	var unreadable []scheduleTarget
	for _, db := range all {
		if dbal.IsProtectedDatabase(db) {
			continue
		}
		solution, err := s.h.isSolution(ctx, db)
		if err != nil {
			unreadable = append(unreadable, scheduleTarget{Database: db, Status: control.RunFailed,
				Error: "it could not be checked: " + err.Error()})
			continue
		}
		if solution {
			out = append(out, db)
		}
	}
	sort.Strings(out)
	return out, unreadable, nil
}

// isSolution says whether db has a File4Base catalog. An error is a
// database that could not be checked, not one without a catalog.
func (h *ServerAdminHandler) isSolution(ctx context.Context, db string) (bool, error) {
	driver, err := h.dbMgr.DriverFor(ctx, db)
	if err != nil {
		return false, err
	}
	q := `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'sys_users'`
	if h.dbMgr.Engine() != dbal.EnginePostgres {
		q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'sys_users'`
	}
	var n int
	if err := driver.DB().QueryRowContext(ctx, q).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *serverScheduler) runFor(ctx context.Context, sc control.ServerSchedule, db string) scheduleTarget {
	res := scheduleTarget{Database: db, Status: control.RunSuccess}
	fail := func(err error) scheduleTarget {
		res.Status, res.Error = control.RunFailed, err.Error()
		return res
	}
	solution, err := s.h.isSolution(ctx, db)
	if err != nil {
		return fail(fmt.Errorf("it could not be reached: %w", err))
	}
	if !solution {
		return fail(errors.New("it is not a File4Base solution"))
	}
	switch sc.Type {
	case control.ScheduleBackup:
		done, ok := s.h.beginBackup(db)
		if !ok {
			return fail(errors.New("a restore of it was running"))
		}
		defer done()
		rec, err := s.h.writeSnapshotFor(ctx, db, "scheduled", "scheduler",
			"Scheduled backup "+sc.Name, "scheduled", sc.ID, s.now())
		if err != nil {
			return fail(err)
		}
		res.BackupID, res.SizeBytes = rec.ID, rec.SizeBytes
		removed, err := s.h.pruneScheduledBackups(sc, db, rec.ID)
		res.Removed = removed
		if err != nil {
			return fail(fmt.Errorf("backed up as %s, but the older backups could not be removed: %w", rec.ID, err))
		}
	case control.ScheduleMaintenance:
		if s.h.isRestoring(db) {
			return fail(errors.New("a restore of it was running"))
		}
		if err := s.h.optimizeDatabase(ctx, db, sc.Operation); err != nil {
			return fail(err)
		}
	default:
		return fail(fmt.Errorf("a %s schedule cannot run", sc.Type))
	}
	return res
}

// pruneScheduledBackups keeps the sc.Keep latest backups sc made of db,
// written the latest, and deletes the others. Only regular files named as
// that schedule's backups are read, and only backups of that schedule and
// of that very database are counted or deleted: manual, pre-update,
// pre-restore and uploaded backups never are.
func (h *ServerAdminHandler) pruneScheduledBackups(sc control.ServerSchedule, db, latest string) ([]string, error) {
	if sc.Keep <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(h.backupsDir)
	if err != nil {
		return nil, err
	}
	prefix := "bkp_" + db + "_"
	type found struct{ id, created string }
	var mine []found
	for _, e := range entries {
		name := e.Name()
		id := strings.TrimSuffix(name, ".f4b.json")
		if !e.Type().IsRegular() || id == name || id == latest || !strings.HasPrefix(id, prefix) || !strings.Contains(id, "_scheduled") {
			continue
		}
		hdr, err := readBackupHeader(filepath.Join(h.backupsDir, name))
		if err != nil || hdr.Type != "scheduled" || hdr.ScheduleID != sc.ID || hdr.Database != db {
			continue
		}
		mine = append(mine, found{id, hdr.CreatedAt})
	}
	sort.Slice(mine, func(i, j int) bool {
		if mine[i].created != mine[j].created {
			return mine[i].created > mine[j].created
		}
		return mine[i].id > mine[j].id
	})
	// The backup just written is the first kept.
	var removed []string
	for i := sc.Keep - 1; i < len(mine); i++ {
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
		// Table, Op, Msg_type, Msg_text. A table that could not be
		// analyzed has an error row, or a status other than OK and
		// "Table is already up to date" ("Operation failed").
		if len(cols) >= 4 {
			kind, text := strings.ToLower(*texts[2]), *texts[3]
			failed := kind == "error" ||
				(kind == "status" && !strings.EqualFold(text, "OK") && !strings.Contains(strings.ToLower(text), "up to date"))
			if failed {
				return fmt.Errorf("ANALYZE TABLE %s: %s", *texts[0], text)
			}
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
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"schedules": views, "time_zone": h.scheduler.loc.String(), "scheduler_running": h.scheduler.isStarted(),
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

// applyScheduleInput checks in and applies it to sc, a schedule being
// created when creating is true. It says whether sc's timing (its cron or
// whether it is enabled) changed, and so its next run.
func (h *ServerAdminHandler) applyScheduleInput(ctx context.Context, sc *control.ServerSchedule, in scheduleInput, creating bool) (bool, error) {
	prevCron, prevEnabled := sc.Cron, sc.Enabled
	if in.Name != nil {
		sc.Name = strings.TrimSpace(*in.Name)
	}
	if sc.Name == "" {
		return false, errors.New("a schedule needs a name")
	}
	if in.Type != nil {
		sc.Type = strings.TrimSpace(*in.Type)
	}
	switch sc.Type {
	case control.ScheduleBackup, control.ScheduleMaintenance:
	case "script":
		return false, errors.New("script schedules need the server-side script engine, which File4Base does not have yet (#204)")
	default:
		return false, fmt.Errorf("a schedule is a backup or a maintenance, not %q", sc.Type)
	}
	if in.Cron != nil {
		cron, err := cronexpr.Parse(*in.Cron)
		if err != nil {
			return false, err
		}
		sc.Cron = cron.String()
	}
	if sc.Cron == "" {
		return false, errors.New("a schedule needs a cron expression")
	}
	if in.Databases != nil {
		known, err := h.dbMgr.ListDatabases(ctx)
		if err != nil {
			return false, fmt.Errorf("the solutions of the server could not be listed: %w", err)
		}
		seen := map[string]bool{}
		sc.Databases = []string{}
		for _, db := range *in.Databases {
			db = strings.TrimSpace(db)
			if db == "" {
				continue
			}
			name, ok := matchDatabase(known, db)
			if !ok || dbal.IsProtectedDatabase(name) {
				return false, fmt.Errorf("there is no solution %q on this server", db)
			}
			if !seen[name] {
				seen[name] = true
				sc.Databases = append(sc.Databases, name)
			}
		}
	}
	if in.Keep != nil {
		sc.Keep = *in.Keep
	} else if creating && sc.Type == control.ScheduleBackup {
		sc.Keep = 7
	}
	if in.Operation != nil {
		sc.Operation = strings.TrimSpace(*in.Operation)
	}
	switch sc.Type {
	case control.ScheduleBackup:
		// A count given is never replaced by a default: a backup schedule
		// deletes the backups it does not keep.
		if sc.Keep < 1 || sc.Keep > 1000 {
			return false, errors.New("a backup schedule keeps from 1 to 1000 backups of each solution")
		}
		sc.Operation = ""
	case control.ScheduleMaintenance:
		sc.Keep = 0
		if sc.Operation == "" {
			sc.Operation = maintenanceVacuumAnalyze
		}
		if sc.Operation != maintenanceVacuumAnalyze && sc.Operation != maintenanceAnalyze {
			return false, fmt.Errorf("a maintenance schedule runs %q or %q, not %q",
				maintenanceVacuumAnalyze, maintenanceAnalyze, sc.Operation)
		}
	}
	if in.Enabled != nil {
		sc.Enabled = *in.Enabled
	}
	if !creating && sc.Cron == prevCron && sc.Enabled == prevEnabled {
		return false, nil // its next run stays as it is
	}
	next := h.scheduler.nextAfter(sc.Cron, h.scheduler.now())
	if next == nil {
		return false, fmt.Errorf("%q never comes round: no date matches it", sc.Cron)
	}
	sc.NextRunAt = nil
	if sc.Enabled {
		sc.NextRunAt = next
	}
	return true, nil
}

// matchDatabase finds name among known: the database of that very name,
// or else the only one of that name in another case.
func matchDatabase(known []string, name string) (string, bool) {
	var folded []string
	for _, db := range known {
		if db == name {
			return db, true
		}
		if strings.EqualFold(db, name) {
			folded = append(folded, db)
		}
	}
	if len(folded) == 1 {
		return folded[0], true
	}
	return "", false
}

func (h *ServerAdminHandler) writeScheduleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, control.ErrScheduleNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Schedule Not Found", "no schedule has this id")
	case errors.Is(err, control.ErrScheduleNameTaken):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Name Taken", err.Error())
	case errors.Is(err, control.ErrScheduleChanged):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Schedule Changed", "the schedule ran or changed meanwhile; read it again and retry")
	case errors.Is(err, errScheduleBusy):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Schedule Running", err.Error())
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
	if _, err := h.applyScheduleInput(r.Context(), &sc, in, true); err != nil {
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
	prevNext := sc.NextRunAt
	timing, err := h.applyScheduleInput(r.Context(), sc, in, false)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Validation Failed", err.Error())
		return
	}
	if err := h.controlStore.UpdateSchedule(r.Context(), *sc, timing, prevNext); err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	state := "enabled"
	if !sc.Enabled {
		state = "disabled"
	}
	h.recordAudit("schedule", "Update Scheduled Job", sess.Username, sess.Database, sc.Name, "success",
		fmt.Sprintf("The %s schedule %q is %s (%s)", sc.Type, sc.Name, state, sc.Cron))
	saved, err := h.controlStore.GetSchedule(r.Context(), sc.ID)
	if err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	last, _ := h.controlStore.LastScheduleRuns(r.Context())
	writeJSON(w, http.StatusOK, h.viewSchedule(*saved, last))
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
	if err := h.scheduler.deleteUnlessRunning(sc.ID, func() error {
		return h.controlStore.DeleteSchedule(r.Context(), sc.ID)
	}); err != nil {
		h.writeScheduleError(w, r, err)
		return
	}
	h.recordAudit("schedule", "Delete Scheduled Job", sess.Username, sess.Database, sc.Name, "success",
		fmt.Sprintf("Deleted the %s schedule %q", sc.Type, sc.Name))
	w.WriteHeader(http.StatusNoContent)
}
