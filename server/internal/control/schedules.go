package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Server schedules (#188, #204) are the jobs the server runs on its own: a
// backup of some or all of its solutions, or their maintenance. They and
// their runs live here, in the control store, so they survive a restart.

// ErrScheduleNotFound is returned for a schedule id the store does not have.
var ErrScheduleNotFound = errors.New("schedule not found")

// ErrScheduleNameTaken is returned when another schedule has the name.
var ErrScheduleNameTaken = errors.New("another schedule has this name")

// The kinds of server schedule.
const (
	ScheduleBackup      = "backup"
	ScheduleMaintenance = "maintenance"
)

// The outcomes of a schedule run.
const (
	RunRunning = "running"
	RunSuccess = "success"
	RunFailed  = "failed"
	RunSkipped = "skipped"
)

// ServerSchedule is one server job and when it next runs.
type ServerSchedule struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Databases are the solutions it acts on; none means every solution on
	// the server, those created after it included.
	Databases []string `json:"databases"`
	Cron      string   `json:"cron"`
	// Keep is how many of its backups of each solution are kept (backup).
	Keep int `json:"keep,omitempty"`
	// Operation is what a maintenance schedule does.
	Operation string     `json:"operation,omitempty"`
	Enabled   bool       `json:"enabled"`
	NextRunAt *time.Time `json:"next_run_at"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ScheduleRun is one run of a schedule, or an occurrence it did not run.
type ScheduleRun struct {
	ID         int64      `json:"id"`
	ScheduleID string     `json:"schedule_id"`
	Trigger    string     `json:"trigger"` // "schedule" or "manual"
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationMs int64      `json:"duration_ms"`
	SizeBytes  int64      `json:"size_bytes"`
	Error      string     `json:"error,omitempty"`
	// Details says what the run did with each solution, as JSON.
	Details json.RawMessage `json:"details,omitempty"`
}

// keptRuns is how many runs of a schedule its history keeps.
const keptRuns = 50

func (s *Store) initScheduleSchema(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS server_schedules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			name_key TEXT NOT NULL UNIQUE,
			job_type TEXT NOT NULL,
			databases_json TEXT NOT NULL DEFAULT '[]',
			cron_expr TEXT NOT NULL,
			keep_count INTEGER NOT NULL DEFAULT 0,
			operation TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			next_run_at TEXT,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS server_schedule_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			schedule_id TEXT NOT NULL REFERENCES server_schedules(id) ON DELETE CASCADE,
			trigger TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at TEXT NOT NULL,
			finished_at TEXT,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			error TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS server_schedule_runs_by_schedule ON server_schedule_runs (schedule_id, id);`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed initializing the schedules of the control store: %w", err)
		}
	}
	return nil
}

// storeTime is how the store writes a time: UTC, to the nanosecond, so that
// a time read back compares equal to the one written.
func storeTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func storeTimePtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return storeTime(*t)
}

func readTime(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}

func readTimePtr(raw sql.NullString) *time.Time {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	t := readTime(raw.String)
	return &t
}

func scheduleNameKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateSchedule stores a new schedule.
func (s *Store) CreateSchedule(ctx context.Context, sc ServerSchedule) error {
	if s == nil || s.db == nil {
		return errors.New("no control store")
	}
	dbs, _ := json.Marshal(nonNilStrings(sc.Databases))
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO server_schedules (id, name, name_key, job_type, databases_json, cron_expr, keep_count,
			operation, enabled, next_run_at, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sc.ID, strings.TrimSpace(sc.Name), scheduleNameKey(sc.Name), sc.Type, string(dbs), sc.Cron, sc.Keep,
		sc.Operation, boolToInt(sc.Enabled), storeTimePtr(sc.NextRunAt), sc.CreatedBy, storeTime(now), storeTime(now))
	if isUniqueViolation(err) {
		return ErrScheduleNameTaken
	}
	return err
}

// ErrScheduleChanged is returned by UpdateSchedule when the schedule's next
// run moved since it was read: it ran meanwhile.
var ErrScheduleChanged = errors.New("the schedule ran or changed meanwhile")

// UpdateSchedule replaces a schedule's settings. Its next run is replaced
// only when timing is true (its cron or enabled state changed), and then
// only if it is still prevNext, the one the caller read: a run claimed in
// between is never moved back to its occurrence and run again.
func (s *Store) UpdateSchedule(ctx context.Context, sc ServerSchedule, timing bool, prevNext *time.Time) error {
	if s == nil || s.db == nil {
		return errors.New("no control store")
	}
	dbs, _ := json.Marshal(nonNilStrings(sc.Databases))
	query := `UPDATE server_schedules SET name = ?, name_key = ?, job_type = ?, databases_json = ?, cron_expr = ?,
			keep_count = ?, operation = ?, enabled = ?, updated_at = ? WHERE id = ?`
	args := []interface{}{strings.TrimSpace(sc.Name), scheduleNameKey(sc.Name), sc.Type, string(dbs), sc.Cron,
		sc.Keep, sc.Operation, boolToInt(sc.Enabled), storeTime(time.Now()), sc.ID}
	if timing {
		query = `UPDATE server_schedules SET name = ?, name_key = ?, job_type = ?, databases_json = ?, cron_expr = ?,
			keep_count = ?, operation = ?, enabled = ?, updated_at = ?, next_run_at = ?
			WHERE id = ? AND next_run_at IS ?`
		args = append(args[:9], storeTimePtr(sc.NextRunAt), sc.ID, storeTimePtr(prevNext))
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if isUniqueViolation(err) {
		return ErrScheduleNameTaken
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.GetSchedule(ctx, sc.ID); err != nil {
			return err
		}
		return ErrScheduleChanged
	}
	return nil
}

// DeleteSchedule deletes a schedule and its history.
func (s *Store) DeleteSchedule(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return errors.New("no control store")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM server_schedule_runs WHERE schedule_id = ?`, id); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM server_schedules WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

const scheduleColumns = `id, name, job_type, databases_json, cron_expr, keep_count, operation, enabled,
	next_run_at, created_by, created_at, updated_at`

func scanServerSchedule(row interface{ Scan(...interface{}) error }) (*ServerSchedule, error) {
	var sc ServerSchedule
	var dbs, created, updated string
	var enabled int
	var next sql.NullString
	if err := row.Scan(&sc.ID, &sc.Name, &sc.Type, &dbs, &sc.Cron, &sc.Keep, &sc.Operation, &enabled,
		&next, &sc.CreatedBy, &created, &updated); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(dbs), &sc.Databases)
	sc.Databases = nonNilStrings(sc.Databases)
	sc.Enabled = enabled != 0
	sc.NextRunAt = readTimePtr(next)
	sc.CreatedAt, sc.UpdatedAt = readTime(created), readTime(updated)
	return &sc, nil
}

// GetSchedule reads one schedule.
func (s *Store) GetSchedule(ctx context.Context, id string) (*ServerSchedule, error) {
	if s == nil || s.db == nil {
		return nil, ErrScheduleNotFound
	}
	sc, err := scanServerSchedule(s.db.QueryRowContext(ctx,
		`SELECT `+scheduleColumns+` FROM server_schedules WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrScheduleNotFound
	}
	return sc, err
}

// ListSchedules reads every schedule, by name.
func (s *Store) ListSchedules(ctx context.Context) ([]ServerSchedule, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduleColumns+` FROM server_schedules ORDER BY name_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ServerSchedule{}
	for rows.Next() {
		sc, err := scanServerSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *sc)
	}
	return list, rows.Err()
}

// ClaimScheduleRun moves an enabled schedule's next run from due to next,
// and records run for that occurrence, in one transaction: as running, or,
// when run.Status is RunSkipped, as skipped. The update applies only while
// the schedule still has the next run the caller read, so one occurrence is
// claimed once, and a claimed occurrence always has its run recorded. It
// gives the schedule as it is now, and the run's id; nil when the
// occurrence was not claimed.
func (s *Store) ClaimScheduleRun(ctx context.Context, id string, due time.Time, next *time.Time, run ScheduleRun) (*ServerSchedule, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, errors.New("no control store")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx,
		`UPDATE server_schedules SET next_run_at = ? WHERE id = ? AND enabled = 1 AND next_run_at = ?`,
		storeTimePtr(next), id, storeTime(due))
	if err != nil {
		return nil, 0, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, 0, nil
	}
	sc, err := scanServerSchedule(tx.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM server_schedules WHERE id = ?`, id))
	if err != nil {
		return nil, 0, err
	}
	status, finished := RunRunning, interface{}(nil)
	if run.Status == RunSkipped {
		status, finished = RunSkipped, storeTime(run.StartedAt)
	}
	ins, err := tx.ExecContext(ctx, `
		INSERT INTO server_schedule_runs (schedule_id, trigger, status, started_at, finished_at, error)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, run.Trigger, status, storeTime(run.StartedAt), finished, run.Error)
	if err != nil {
		return nil, 0, err
	}
	runID, err := ins.LastInsertId()
	if err != nil {
		return nil, 0, err
	}
	if _, err := tx.ExecContext(ctx, trimRuns, id, id, keptRuns); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return sc, runID, nil
}

// StartScheduleRun records a run that is not an occurrence (run now) as
// started, and gives its id.
func (s *Store) StartScheduleRun(ctx context.Context, run ScheduleRun) (int64, error) {
	if s == nil || s.db == nil {
		return 0, errors.New("no control store")
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO server_schedule_runs (schedule_id, trigger, status, started_at) VALUES (?, ?, ?, ?)
	`, run.ScheduleID, run.Trigger, RunRunning, storeTime(run.StartedAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScheduleRun records the outcome of a started run, or of an
// occurrence that was not run (id 0), and keeps only the latest runs of its
// schedule.
func (s *Store) FinishScheduleRun(ctx context.Context, run ScheduleRun) error {
	if s == nil || s.db == nil {
		return errors.New("no control store")
	}
	finished := time.Now().UTC()
	if run.FinishedAt != nil {
		finished = *run.FinishedAt
	}
	details := string(run.Details)
	var err error
	if run.ID == 0 {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO server_schedule_runs (schedule_id, trigger, status, started_at, finished_at,
				duration_ms, size_bytes, error, details_json)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, run.ScheduleID, run.Trigger, run.Status, storeTime(run.StartedAt), storeTime(finished),
			run.DurationMs, run.SizeBytes, run.Error, details)
	} else {
		_, err = s.db.ExecContext(ctx, `
			UPDATE server_schedule_runs SET status = ?, finished_at = ?, duration_ms = ?, size_bytes = ?,
				error = ?, details_json = ?
			WHERE id = ?
		`, run.Status, storeTime(finished), run.DurationMs, run.SizeBytes, run.Error, details, run.ID)
	}
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, trimRuns, run.ScheduleID, run.ScheduleID, keptRuns)
	return err
}

// trimRuns keeps the keptRuns latest runs of a schedule.
const trimRuns = `DELETE FROM server_schedule_runs WHERE schedule_id = ? AND id NOT IN (
	SELECT id FROM server_schedule_runs WHERE schedule_id = ? ORDER BY id DESC LIMIT ?)`

// InterruptScheduleRuns records every run still marked running as failed:
// called at startup, when no run of this process has begun, they are runs
// the server stopped in the middle of.
func (s *Store) InterruptScheduleRuns(ctx context.Context) (int64, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE server_schedule_runs SET status = ?, finished_at = ?, error = ? WHERE status = ?
	`, RunFailed, storeTime(time.Now()), "the server stopped during this run", RunRunning)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

const runColumns = `id, schedule_id, trigger, status, started_at, finished_at, duration_ms, size_bytes,
	error, details_json`

func scanScheduleRun(row interface{ Scan(...interface{}) error }) (*ScheduleRun, error) {
	var run ScheduleRun
	var started, details string
	var finished sql.NullString
	if err := row.Scan(&run.ID, &run.ScheduleID, &run.Trigger, &run.Status, &started, &finished,
		&run.DurationMs, &run.SizeBytes, &run.Error, &details); err != nil {
		return nil, err
	}
	run.StartedAt = readTime(started)
	run.FinishedAt = readTimePtr(finished)
	if details != "" {
		run.Details = json.RawMessage(details)
	}
	return &run, nil
}

// ListScheduleRuns reads the latest runs of a schedule, the latest first.
func (s *Store) ListScheduleRuns(ctx context.Context, scheduleID string, limit int) ([]ScheduleRun, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > keptRuns {
		limit = keptRuns
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM server_schedule_runs
		WHERE schedule_id = ? ORDER BY id DESC LIMIT ?`, scheduleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ScheduleRun{}
	for rows.Next() {
		run, err := scanScheduleRun(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *run)
	}
	return list, rows.Err()
}

// LastScheduleRuns reads the latest run of every schedule that has one.
func (s *Store) LastScheduleRuns(ctx context.Context) (map[string]ScheduleRun, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+runColumns+` FROM server_schedule_runs
		WHERE id IN (SELECT MAX(id) FROM server_schedule_runs GROUP BY schedule_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	last := map[string]ScheduleRun{}
	for rows.Next() {
		run, err := scanScheduleRun(rows)
		if err != nil {
			return nil, err
		}
		last[run.ScheduleID] = *run
	}
	return last, rows.Err()
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
