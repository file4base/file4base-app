package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/cronexpr"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// ErrTransferScheduleNotFound is returned when no schedule has the given id.
var ErrTransferScheduleNotFound = errors.New("transfer schedule not found")

// ErrTransferScheduleExists is returned when the solution already has a
// schedule with that name.
var ErrTransferScheduleExists = errors.New("a transfer schedule with that name already exists")

// ErrInvalidTransferSchedule is returned for a schedule that cannot be stored
// as given.
var ErrInvalidTransferSchedule = errors.New("invalid transfer schedule")

// The outcome of one run of a scheduled transfer.
const (
	// ScheduleRunCommitted: the destination transaction was committed.
	ScheduleRunCommitted = "committed"

	// ScheduleRunRefused: a run-as account no longer held the authority the
	// run needs, or the transfer now points somewhere the schedule was not
	// authorized for. Nothing was read or written.
	ScheduleRunRefused = "refused"

	// ScheduleRunFailed: the transfer ran and was refused by its own rules —
	// a rejected row under "abort", an unresolved field, a row the
	// destination would not store. The destination is as it was.
	ScheduleRunFailed = "failed"
)

// What started a run.
const (
	ScheduleTriggerSchedule = "schedule"
	ScheduleTriggerManual   = "manual"
)

// maxScheduleRuns is how many runs of each schedule are kept: enough history
// to see a pattern, bounded so a minute-by-minute schedule cannot fill the
// catalog.
const maxScheduleRuns = 50

const maxScheduleName = 128

// TransferSchedule runs a transfer definition on a cron schedule, with nobody
// signed in (#108).
//
// There is no session to authorize an unattended run, and the definition
// holds no credentials, so a schedule names an account in each solution to
// run as — and nothing else. No password and no token is stored. The
// authority comes from the administrators who created the schedule, proven
// when it was saved, and it is checked again on every run: each account must
// still exist, be active, hold the privilege its end needs and reach its
// table. A check that fails refuses the run and writes nothing.
//
// The solutions both ends resolved to when the schedule was authorized are
// kept with it. If the transfer definition is later changed to point at
// another solution, the schedule refuses to run rather than carry an
// authorization over to a solution nobody authorized it for.
type TransferSchedule struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	TransferID   string `json:"transfer_id"`
	TransferName string `json:"transfer_name,omitempty"`

	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`

	// SourceAccount and DestinationAccount are usernames, each in the
	// solution of its end.
	SourceAccount      string `json:"source_account"`
	DestinationAccount string `json:"destination_account"`

	// SourceDatabase and DestinationDatabase are the solutions the schedule
	// was authorized for. Empty is the solution holding the schedule.
	SourceDatabase      string `json:"source_database"`
	DestinationDatabase string `json:"destination_database"`

	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

// TransferScheduleInput is what a caller supplies to create a schedule.
type TransferScheduleInput struct {
	Name string `json:"name"`

	// Transfer is the definition's id or name.
	Transfer string `json:"transfer"`

	Cron    string `json:"cron"`
	Enabled *bool  `json:"enabled,omitempty"`

	SourceAccount      string `json:"source_account"`
	DestinationAccount string `json:"destination_account"`
}

// TransferSchedulePatch is what may change on an existing schedule. The
// transfer and the run-as accounts may not: they are what was authorized, so
// changing them is creating a different schedule.
type TransferSchedulePatch struct {
	Name    *string `json:"name,omitempty"`
	Cron    *string `json:"cron,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// TransferScheduleRun is one run of a schedule.
type TransferScheduleRun struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	Trigger    string    `json:"trigger"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`

	// Report is the transfer's run report, as JSON, when the transfer ran.
	Report string `json:"report,omitempty"`
}

func (s *Service) schedulePlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

func (s *Service) schedulePlaceholders(from, count int) []interface{} {
	out := make([]interface{}, count)
	for i := range out {
		out[i] = s.schedulePlaceholder(from + i)
	}
	return out
}

// CreateTransferSchedule stores a schedule for the transfer the input names.
//
// The caller has already proven authority over both ends and checked the
// accounts (see the API handler); this stores what was authorized, including
// the two solutions the definition resolved to at that moment — holding is
// the database of the solution holding the schedule.
func (s *Service) CreateTransferSchedule(
	ctx context.Context, in TransferScheduleInput, holding, createdBy string,
) (*TransferSchedule, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, fmt.Errorf("%w: a schedule needs a name", ErrInvalidTransferSchedule)
	}
	if len([]rune(in.Name)) > maxScheduleName {
		return nil, fmt.Errorf("%w: the name of a schedule is at most %d characters",
			ErrInvalidTransferSchedule, maxScheduleName)
	}
	cron, err := cronexpr.Parse(in.Cron)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTransferSchedule, err)
	}
	in.SourceAccount = strings.ToLower(strings.TrimSpace(in.SourceAccount))
	in.DestinationAccount = strings.ToLower(strings.TrimSpace(in.DestinationAccount))
	if in.SourceAccount == "" || in.DestinationAccount == "" {
		return nil, fmt.Errorf("%w: a schedule names the account to run as in each solution",
			ErrInvalidTransferSchedule)
	}

	def, err := s.GetTransfer(ctx, strings.TrimSpace(in.Transfer))
	if errors.Is(err, ErrTransferNotFound) {
		def, err = s.GetTransferByName(ctx, in.Transfer)
	}
	if err != nil {
		return nil, err
	}

	if taken, err := s.scheduleNameTaken(ctx, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrTransferScheduleExists, in.Name)
	}

	enabled := in.Enabled == nil || *in.Enabled
	now := time.Now().UTC()
	sched := &TransferSchedule{
		ID:                  uuid.NewString(),
		Name:                in.Name,
		TransferID:          def.ID,
		TransferName:        def.Name,
		Cron:                cron.String(),
		Enabled:             enabled,
		SourceAccount:       in.SourceAccount,
		DestinationAccount:  in.DestinationAccount,
		SourceDatabase:      def.Source.DatabaseOr(holding),
		DestinationDatabase: def.Destination.DatabaseOr(holding),
		CreatedBy:           createdBy,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if enabled {
		next := cron.Next(now)
		if !next.IsZero() {
			sched.NextRunAt = &next
		}
	}

	q := fmt.Sprintf(`INSERT INTO sys_transfer_schedules
	    (id, name, transfer_id, cron_expr, enabled, source_account, destination_account,
	     source_database, destination_database, created_by, created_at, updated_at, next_run_at)
	    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		s.schedulePlaceholders(1, 13)...)
	var owner interface{}
	if strings.TrimSpace(createdBy) != "" {
		owner = createdBy
	}
	if _, err := s.driver.DB().ExecContext(ctx, q,
		sched.ID, sched.Name, sched.TransferID, sched.Cron, sched.Enabled,
		sched.SourceAccount, sched.DestinationAccount, sched.SourceDatabase, sched.DestinationDatabase,
		owner, sched.CreatedAt, sched.UpdatedAt, nullableTime(sched.NextRunAt)); err != nil {
		return nil, fmt.Errorf("failed saving the schedule %q: %w", sched.Name, err)
	}
	return s.GetTransferSchedule(ctx, sched.ID)
}

// UpdateTransferSchedule renames a schedule, changes when it runs, or turns
// it on or off. Its next run is worked out again from now.
func (s *Service) UpdateTransferSchedule(ctx context.Context, id string, patch TransferSchedulePatch) (*TransferSchedule, error) {
	sched, err := s.GetTransferSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	if patch.Name != nil {
		name := strings.TrimSpace(*patch.Name)
		if name == "" || len([]rune(name)) > maxScheduleName {
			return nil, fmt.Errorf("%w: a schedule needs a name of at most %d characters",
				ErrInvalidTransferSchedule, maxScheduleName)
		}
		if taken, err := s.scheduleNameTaken(ctx, name, id); err != nil {
			return nil, err
		} else if taken {
			return nil, fmt.Errorf("%w: %s", ErrTransferScheduleExists, name)
		}
		sched.Name = name
	}
	if patch.Cron != nil {
		cron, err := cronexpr.Parse(*patch.Cron)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTransferSchedule, err)
		}
		sched.Cron = cron.String()
	}
	if patch.Enabled != nil {
		sched.Enabled = *patch.Enabled
	}

	cron, err := cronexpr.Parse(sched.Cron)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTransferSchedule, err)
	}
	now := time.Now().UTC()
	var next *time.Time
	if sched.Enabled {
		if n := cron.Next(now); !n.IsZero() {
			next = &n
		}
	}

	q := fmt.Sprintf(`UPDATE sys_transfer_schedules SET name = %s, cron_expr = %s, enabled = %s,
	    next_run_at = %s, updated_at = %s WHERE id = %s`, s.schedulePlaceholders(1, 6)...)
	if _, err := s.driver.DB().ExecContext(ctx, q,
		sched.Name, sched.Cron, sched.Enabled, nullableTime(next), now, id); err != nil {
		return nil, fmt.Errorf("failed updating the schedule %s: %w", id, err)
	}
	return s.GetTransferSchedule(ctx, id)
}

// DeleteTransferSchedule removes a schedule and its run history.
func (s *Service) DeleteTransferSchedule(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_transfer_schedules WHERE id = %s`, s.schedulePlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting the schedule %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrTransferScheduleNotFound, id)
	}
	return nil
}

const scheduleColumns = `s.id, s.name, s.transfer_id, t.name, s.cron_expr, s.enabled, s.source_account,
	s.destination_account, s.source_database, s.destination_database, s.created_by, s.created_at,
	s.updated_at, s.next_run_at, s.last_run_at, s.last_status, s.last_error`

const scheduleFrom = `FROM sys_transfer_schedules s LEFT JOIN sys_transfers t ON t.id = s.transfer_id`

// GetTransferSchedule reads one schedule.
func (s *Service) GetTransferSchedule(ctx context.Context, id string) (*TransferSchedule, error) {
	q := fmt.Sprintf(`SELECT %s %s WHERE s.id = %s`, scheduleColumns, scheduleFrom, s.schedulePlaceholder(1))
	sched, err := scanSchedule(s.driver.DB().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrTransferScheduleNotFound, id)
	}
	return sched, err
}

// ListTransferSchedules reads every schedule, by name.
func (s *Service) ListTransferSchedules(ctx context.Context) ([]TransferSchedule, error) {
	return s.querySchedules(ctx, fmt.Sprintf(`SELECT %s %s ORDER BY s.name ASC`, scheduleColumns, scheduleFrom))
}

// DueTransferSchedules reads the enabled schedules whose next run is at or
// before now.
func (s *Service) DueTransferSchedules(ctx context.Context, now time.Time) ([]TransferSchedule, error) {
	q := fmt.Sprintf(`SELECT %s %s WHERE s.enabled = %s AND s.next_run_at IS NOT NULL AND s.next_run_at <= %s
	    ORDER BY s.next_run_at ASC`, scheduleColumns, scheduleFrom, s.schedulePlaceholder(1), s.schedulePlaceholder(2))
	return s.querySchedules(ctx, q, true, now.UTC())
}

// ClaimTransferScheduleRun moves a due schedule's next run forward, and says
// whether this caller was the one that did.
//
// The update only applies while next_run_at still holds the value the caller
// read, so when two schedulers see the same due run — two server processes,
// or a tick that overlaps the last — exactly one of them claims it.
func (s *Service) ClaimTransferScheduleRun(ctx context.Context, sched *TransferSchedule, now time.Time) (bool, error) {
	if sched.NextRunAt == nil {
		return false, nil
	}
	cron, err := cronexpr.Parse(sched.Cron)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidTransferSchedule, err)
	}
	var next *time.Time
	if n := cron.Next(now); !n.IsZero() {
		next = &n
	}
	q := fmt.Sprintf(`UPDATE sys_transfer_schedules SET next_run_at = %s WHERE id = %s AND next_run_at = %s`,
		s.schedulePlaceholders(1, 3)...)
	res, err := s.driver.DB().ExecContext(ctx, q, nullableTime(next), sched.ID, *sched.NextRunAt)
	if err != nil {
		return false, fmt.Errorf("failed claiming the schedule %s: %w", sched.ID, err)
	}
	affected, _ := res.RowsAffected()
	return affected == 1, nil
}

// RecordTransferScheduleRun stores one run, notes it as the schedule's last,
// and keeps only the most recent runs.
func (s *Service) RecordTransferScheduleRun(ctx context.Context, run *TransferScheduleRun) error {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	q := fmt.Sprintf(`INSERT INTO sys_transfer_schedule_runs
	    (id, schedule_id, trigger_kind, started_at, finished_at, status, error, report)
	    VALUES (%s, %s, %s, %s, %s, %s, %s, %s)`, s.schedulePlaceholders(1, 8)...)
	if _, err := s.driver.DB().ExecContext(ctx, q, run.ID, run.ScheduleID, run.Trigger,
		run.StartedAt.UTC(), run.FinishedAt.UTC(), run.Status, run.Error, run.Report); err != nil {
		return fmt.Errorf("failed recording a run of the schedule %s: %w", run.ScheduleID, err)
	}

	q = fmt.Sprintf(`UPDATE sys_transfer_schedules SET last_run_at = %s, last_status = %s, last_error = %s
	    WHERE id = %s`, s.schedulePlaceholders(1, 4)...)
	if _, err := s.driver.DB().ExecContext(ctx, q, run.StartedAt.UTC(), run.Status, run.Error, run.ScheduleID); err != nil {
		return fmt.Errorf("failed noting the last run of the schedule %s: %w", run.ScheduleID, err)
	}

	// Keep the most recent runs. Reading the ids and deleting the rest works
	// the same on every engine, which a LIMIT inside a DELETE does not.
	q = fmt.Sprintf(`SELECT id FROM sys_transfer_schedule_runs WHERE schedule_id = %s
	    ORDER BY started_at DESC, id DESC`, s.schedulePlaceholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, run.ScheduleID)
	if err != nil {
		return fmt.Errorf("failed reading the runs of the schedule %s: %w", run.ScheduleID, err)
	}
	var stale []string
	seen := 0
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		seen++
		if seen > maxScheduleRuns {
			stale = append(stale, id)
		}
	}
	rows.Close()
	for _, id := range stale {
		q := fmt.Sprintf(`DELETE FROM sys_transfer_schedule_runs WHERE id = %s`, s.schedulePlaceholder(1))
		if _, err := s.driver.DB().ExecContext(ctx, q, id); err != nil {
			return fmt.Errorf("failed pruning the runs of the schedule %s: %w", run.ScheduleID, err)
		}
	}
	return nil
}

// ListTransferScheduleRuns reads a schedule's runs, newest first.
func (s *Service) ListTransferScheduleRuns(ctx context.Context, scheduleID string) ([]TransferScheduleRun, error) {
	q := fmt.Sprintf(`SELECT id, schedule_id, trigger_kind, started_at, finished_at, status, error, report
	    FROM sys_transfer_schedule_runs WHERE schedule_id = %s ORDER BY started_at DESC, id DESC`,
		s.schedulePlaceholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("failed reading the runs of the schedule %s: %w", scheduleID, err)
	}
	defer rows.Close()

	runs := []TransferScheduleRun{}
	for rows.Next() {
		var run TransferScheduleRun
		var runErr, report sql.NullString
		if err := rows.Scan(&run.ID, &run.ScheduleID, &run.Trigger, &run.StartedAt, &run.FinishedAt,
			&run.Status, &runErr, &report); err != nil {
			return nil, err
		}
		run.Error, run.Report = runErr.String, report.String
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Service) scheduleNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_transfer_schedules WHERE LOWER(name) = LOWER(%s) AND id <> %s`,
		s.schedulePlaceholder(1), s.schedulePlaceholder(2))
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, q, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed checking the name %q: %w", name, err)
	}
	return count > 0, nil
}

func (s *Service) querySchedules(ctx context.Context, q string, args ...interface{}) ([]TransferSchedule, error) {
	rows, err := s.driver.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed reading the transfer schedules: %w", err)
	}
	defer rows.Close()
	out := []TransferSchedule{}
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sched)
	}
	return out, rows.Err()
}

func scanSchedule(row rowScanner) (*TransferSchedule, error) {
	var sched TransferSchedule
	var transferName, sourceDB, destinationDB, createdBy, lastStatus, lastError sql.NullString
	var next, last sql.NullTime
	if err := row.Scan(&sched.ID, &sched.Name, &sched.TransferID, &transferName, &sched.Cron, &sched.Enabled,
		&sched.SourceAccount, &sched.DestinationAccount, &sourceDB, &destinationDB, &createdBy,
		&sched.CreatedAt, &sched.UpdatedAt, &next, &last, &lastStatus, &lastError); err != nil {
		return nil, err
	}
	sched.TransferName = transferName.String
	sched.SourceDatabase = sourceDB.String
	sched.DestinationDatabase = destinationDB.String
	sched.CreatedBy = createdBy.String
	sched.LastStatus = lastStatus.String
	sched.LastError = lastError.String
	if next.Valid {
		t := next.Time.UTC()
		sched.NextRunAt = &t
	}
	if last.Valid {
		t := last.Time.UTC()
		sched.LastRunAt = &t
	}
	return &sched, nil
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC()
}
