package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// ErrScheduleRunning is returned when a run of a schedule is asked for while
// another run of the same schedule is still going. One schedule runs one at a
// time, so two runs never race each other into the same destination.
var ErrScheduleRunning = errors.New("this schedule is already running")

// inFlight holds the schedules being run in this process, so the scheduler and
// a "run now" request never run the same schedule twice at once.
var inFlight sync.Map

func flightKey(holding, scheduleID string) string {
	return strings.ToLower(holding) + "/" + scheduleID
}

// RunSchedule runs one schedule of the solution holding, and records the run.
//
// Every run re-establishes its authority from scratch, because nobody is
// signed in to vouch for it:
//
//   - the transfer must still resolve to the two solutions the schedule was
//     authorized for — a definition changed to point elsewhere does not carry
//     the authorization along;
//   - each run-as account must still exist and be active, and hold the
//     privilege and the table access its end needs.
//
// Any of those failing records the run as refused, with nothing read or
// written. A transfer that runs and is refused by its own rules — a rejected
// row under "abort", an unresolved field — is recorded as failed, with its
// report. The returned run is the one recorded.
func RunSchedule(
	ctx context.Context, mgr *dbal.MultiDatabaseManager, holding string,
	sched *schema.TransferSchedule, trigger string,
) (*schema.TransferScheduleRun, error) {
	key := flightKey(holding, sched.ID)
	if _, busy := inFlight.LoadOrStore(key, struct{}{}); busy {
		return nil, ErrScheduleRunning
	}
	defer inFlight.Delete(key)

	holdingDriver, err := mgr.DriverFor(ctx, holding)
	if err != nil {
		return nil, fmt.Errorf("the solution %s holding the schedule is not reachable: %w", holding, err)
	}
	svc := schema.NewService(holdingDriver)

	run := &schema.TransferScheduleRun{
		ScheduleID: sched.ID,
		Trigger:    trigger,
		StartedAt:  time.Now().UTC(),
	}
	finish := func(status string, report *Report, cause error) (*schema.TransferScheduleRun, error) {
		run.FinishedAt = time.Now().UTC()
		run.Status = status
		if cause != nil {
			run.Error = cause.Error()
		}
		if report != nil {
			if encoded, err := json.Marshal(report); err == nil {
				run.Report = string(encoded)
			}
		}
		// The run is recorded even when the caller's context has ended: a run
		// that happened and left no trace is the worst kind of history.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := svc.RecordTransferScheduleRun(recordCtx, run); err != nil {
			return run, err
		}
		return run, nil
	}
	refuse := func(cause error) (*schema.TransferScheduleRun, error) {
		return finish(schema.ScheduleRunRefused, nil, cause)
	}

	def, err := svc.GetTransfer(ctx, sched.TransferID)
	if err != nil {
		return refuse(fmt.Errorf("the transfer of this schedule cannot be read: %w", err))
	}

	sourceDB := def.Source.DatabaseOr(holding)
	destinationDB := def.Destination.DatabaseOr(holding)
	if !strings.EqualFold(sourceDB, sched.SourceDatabase) {
		return refuse(fmt.Errorf("%w: the transfer %q now reads from the solution %s, but this schedule "+
			"was authorized for %s; create the schedule again to authorize it for the new source",
			ErrNotAuthorized, def.Name, sourceDB, sched.SourceDatabase))
	}
	if !strings.EqualFold(destinationDB, sched.DestinationDatabase) {
		return refuse(fmt.Errorf("%w: the transfer %q now writes to the solution %s, but this schedule "+
			"was authorized for %s; create the schedule again to authorize it for the new destination",
			ErrNotAuthorized, def.Name, destinationDB, sched.DestinationDatabase))
	}

	sourceDriver, err := mgr.DriverFor(ctx, sourceDB)
	if err != nil {
		return refuse(fmt.Errorf("the source solution %s is not reachable: %w", sourceDB, err))
	}
	destinationDriver, err := mgr.DriverFor(ctx, destinationDB)
	if err != nil {
		return refuse(fmt.Errorf("the destination solution %s is not reachable: %w", destinationDB, err))
	}

	sourceAccount, err := ResolveAccount(ctx, sourceDriver, sourceDB, sched.SourceAccount)
	if err != nil {
		return refuse(err)
	}
	if err := CheckEnd(ctx, sourceDriver, sourceAccount, sourceDB,
		schema.CapabilityBulkExport, def.Source.Table, false); err != nil {
		return refuse(err)
	}
	destinationAccount, err := ResolveAccount(ctx, destinationDriver, destinationDB, sched.DestinationAccount)
	if err != nil {
		return refuse(err)
	}
	if err := CheckEnd(ctx, destinationDriver, destinationAccount, destinationDB,
		schema.CapabilityBulkImport, def.Destination.Table, true); err != nil {
		return refuse(err)
	}

	report, runErr := Run(ctx, def, sourceDriver, destinationDriver, sourceDB, destinationDB)
	if runErr != nil {
		return finish(schema.ScheduleRunFailed, report, runErr)
	}
	return finish(schema.ScheduleRunCommitted, report, nil)
}

// Scheduler runs the due transfer schedules of every solution on the server.
//
// It wakes up every Interval, asks each solution database for the schedules
// that are due, claims each one (so two server processes never run the same
// occurrence) and runs it in the background. A schedule already running is
// left alone until its run ends.
type Scheduler struct {
	mgr *dbal.MultiDatabaseManager

	// Interval is how often the scheduler looks for due schedules. A cron
	// expression is to the minute, so anything well under a minute is on
	// time.
	Interval time.Duration

	// Now is the clock. Tests set it.
	Now func() time.Time

	// Databases lists the solutions to look in. It defaults to every
	// database on the server; tests narrow it to their own.
	Databases func(ctx context.Context) ([]string, error)

	// Logf reports what the scheduler did. It defaults to the standard
	// logger.
	Logf func(format string, args ...interface{})

	wg sync.WaitGroup
}

// NewScheduler makes a scheduler over every solution mgr reaches.
func NewScheduler(mgr *dbal.MultiDatabaseManager) *Scheduler {
	return &Scheduler{
		mgr:       mgr,
		Interval:  20 * time.Second,
		Now:       func() time.Time { return time.Now().UTC() },
		Databases: mgr.ListDatabases,
		Logf:      log.Printf,
	}
}

// Run ticks until ctx ends, then waits for the runs it started.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Wait()
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Wait blocks until every run the scheduler started has finished.
func (s *Scheduler) Wait() { s.wg.Wait() }

// Tick looks once for due schedules and starts them. It answers how many it
// started.
func (s *Scheduler) Tick(ctx context.Context) int {
	databases, err := s.Databases(ctx)
	if err != nil {
		s.Logf("transfer scheduler: the solutions could not be listed: %v", err)
		return 0
	}
	now := s.Now()
	started := 0
	for _, database := range databases {
		if dbal.IsProtectedDatabase(database) {
			continue
		}
		driver, err := s.mgr.DriverFor(ctx, database)
		if err != nil {
			continue // a database that is not reachable has nothing due here
		}
		if !hasScheduleTable(ctx, driver) {
			continue // not a solution, or one from before schedules existed
		}
		svc := schema.NewService(driver)
		due, err := svc.DueTransferSchedules(ctx, now)
		if err != nil {
			s.Logf("transfer scheduler: the schedules of %s could not be read: %v", database, err)
			continue
		}
		for i := range due {
			sched := due[i]
			if _, busy := inFlight.Load(flightKey(database, sched.ID)); busy {
				continue // still running from last time; its next turn waits
			}
			claimed, err := svc.ClaimTransferScheduleRun(ctx, &sched, now)
			if err != nil {
				s.Logf("transfer scheduler: %s/%s could not be claimed: %v", database, sched.Name, err)
				continue
			}
			if !claimed {
				continue // another scheduler took this occurrence
			}
			started++
			s.wg.Add(1)
			go func(database string, sched schema.TransferSchedule) {
				defer s.wg.Done()
				run, err := RunSchedule(ctx, s.mgr, database, &sched, schema.ScheduleTriggerSchedule)
				switch {
				case err != nil:
					s.Logf("transfer scheduler: %s/%s: %v", database, sched.Name, err)
				case run.Status != schema.ScheduleRunCommitted:
					s.Logf("transfer scheduler: %s/%s %s: %s", database, sched.Name, run.Status, run.Error)
				}
			}(database, sched)
		}
	}
	return started
}

// hasScheduleTable reports whether a database carries the schedule catalog,
// asked of the engine's own catalog so a database that is not a solution does
// not log an error every tick.
func hasScheduleTable(ctx context.Context, driver dbal.DatabaseDriver) bool {
	q := `SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'sys_transfer_schedules'`
	if driver.Dialect().Engine() == dbal.EngineMariaDB {
		q += ` AND table_schema = DATABASE()`
	} else {
		q += ` AND table_schema = current_schema()`
	}
	var n int
	if err := driver.DB().QueryRowContext(ctx, q).Scan(&n); err != nil {
		return false
	}
	return n > 0
}
