package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

func pgUser(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		return u.User.Username()
	}
	return "postgres"
}

func mariaCredentials(dsn string) (string, string) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return "root", ""
	}
	return cfg.User, cfg.Passwd
}

const (
	accounts       = 1000
	initialBalance = 100000
)

// vacuumUnderLoad is gate 7: VACUUM INTO while gate 1's load runs, plus
// transfers between accounts whose totals no committed transaction changes.
// The backup is restored and checked against those invariants; a second
// backup taken at rest must restore to the same data, by fingerprint.
func (b *bench) vacuumUnderLoad(ctx context.Context) (map[string]any, error) {
	db := b.driver.DB()
	for _, q := range []string{
		"DROP TABLE IF EXISTS ledger", "DROP TABLE IF EXISTS journal",
		"CREATE TABLE ledger (id INTEGER PRIMARY KEY, balance INTEGER NOT NULL, moves INTEGER NOT NULL)",
		"CREATE TABLE journal (id INTEGER PRIMARY KEY, a INTEGER NOT NULL, b INTEGER NOT NULL, amount INTEGER NOT NULL)",
		fmt.Sprintf("WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < %d) "+
			"INSERT INTO ledger SELECT i, %d, 0 FROM n", accounts, initialBalance),
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}

	loadCtx, cancel := context.WithCancel(ctx)
	editors := &latencies{}
	transfers := &latencies{}
	waitEditors := b.editors(loadCtx, 50, 0, editors)
	var wg sync.WaitGroup
	var journalID atomic.Int64
	for w := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(seed, uint64(1000+w)))
			for loadCtx.Err() == nil {
				start := time.Now()
				err := transfer(loadCtx, db, journalID.Add(1), r)
				transfers.add(time.Since(start), err)
			}
		}()
	}
	var replace, imp map[string]any
	wg.Add(2)
	go func() { defer wg.Done(); replace = b.replaceChunked(loadCtx, "category", "Sauvegardé", 100000, 1000) }()
	go func() { defer wg.Done(); imp = b.importBatches(loadCtx, "members", 20000, 2000) }()

	time.Sleep(2 * time.Second)
	dir := b.backupDir
	if dir == "" {
		dir = b.adminDSN
	}
	dest := filepath.Join(dir, "bench.vacuum.backup")
	_ = os.Remove(dest)
	var maxWAL int64
	stopPoll := make(chan struct{})
	polled := make(chan struct{})
	go func() {
		defer close(polled)
		for {
			if st, err := os.Stat(b.sqlitePath() + "-wal"); err == nil {
				maxWAL = max(maxWAL, st.Size())
			}
			select {
			case <-stopPoll:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	backupErr := dbal.BackupSQLite(ctx, db, dest)
	backup := time.Since(start)
	close(stopPoll)
	<-polled
	time.Sleep(2 * time.Second)
	cancel()
	wg.Wait()
	waitEditors()
	out := map[string]any{
		"backup_seconds": round(backup.Seconds(), 2), "max_wal_mb_during_backup": mb(maxWAL),
		"editors_50": editors.summary(backup + 4*time.Second), "transfers_10": transfers.summary(backup + 4*time.Second),
		"replace_100k": replace, "import_20k_chunked": imp,
	}
	if backupErr != nil {
		out["backup_error"] = backupErr.Error()
		return out, nil
	}
	restored := filepath.Join(b.adminDSN, "bench_vacuum"+dbal.SQLiteFileExtension)
	_ = b.mgr.DropDatabase(ctx, "bench_vacuum")
	if err := dbal.RestoreSQLiteDatabase(ctx, dest, restored); err != nil {
		return out, err
	}
	_ = os.Remove(dest)
	check, err := checkLedger(ctx, restored)
	if err != nil {
		return out, err
	}
	out["backup_under_load"] = check

	// At rest, a backup restores to exactly the data.
	if err := dbal.BackupSQLite(ctx, db, dest); err != nil {
		return out, err
	}
	_ = b.mgr.DropDatabase(ctx, "bench_vacuum")
	if err := dbal.RestoreSQLiteDatabase(ctx, dest, restored); err != nil {
		return out, err
	}
	_ = os.Remove(dest)
	drv, err := b.mgr.GetDriver(ctx, "bench_vacuum")
	if err != nil {
		return out, err
	}
	want, err := dataFingerprint(ctx, db, b.driver.Dialect())
	if err != nil {
		return out, err
	}
	got, err := dataFingerprint(ctx, drv.DB(), drv.Dialect())
	if err != nil {
		return out, err
	}
	out["at_rest_fingerprint_matches"] = want == got
	return out, nil
}

// transfer moves an amount between two accounts and journals it, in one
// transaction.
func transfer(ctx context.Context, db *sql.DB, id int64, r *rand.Rand) error {
	a, b := 1+r.IntN(accounts), 1+r.IntN(accounts)
	amount := 1 + r.IntN(500)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO journal (id, a, b, amount) VALUES ($1, $2, $3, $4)", []any{id, a, b, amount}},
		{"UPDATE ledger SET balance = balance - $1, moves = moves + 1 WHERE id = $2", []any{amount, a}},
		{"UPDATE ledger SET balance = balance + $1, moves = moves + 1 WHERE id = $2", []any{amount, b}},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// checkLedger opens a restored copy and checks it: integrity, the total
// balance, and every account's balance against the journal.
func checkLedger(ctx context.Context, path string) (map[string]any, error) {
	drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: path})
	if err != nil {
		return nil, err
	}
	defer drv.Close()
	db := drv.DB()
	var integrity string
	var total, moves, journal, wrong int64
	for _, q := range []struct {
		sql  string
		dest any
	}{
		{"PRAGMA integrity_check", &integrity},
		{"SELECT SUM(balance) FROM ledger", &total},
		{"SELECT SUM(moves) FROM ledger", &moves},
		{"SELECT COUNT(*) FROM journal", &journal},
		{`SELECT COUNT(*) FROM ledger l WHERE l.balance <> ` + fmt.Sprint(initialBalance) +
			` - COALESCE((SELECT SUM(amount) FROM journal WHERE a = l.id), 0)` +
			` + COALESCE((SELECT SUM(amount) FROM journal WHERE b = l.id), 0)`, &wrong},
	} {
		if err := db.QueryRowContext(ctx, q.sql).Scan(q.dest); err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"integrity": integrity, "transfers": journal,
		"total_balance_kept":           total == accounts*initialBalance,
		"moves_match_journal":          moves == 2*journal,
		"accounts_disagreeing_journal": wrong,
	}, nil
}

// crash is gate 5: the process writing is killed with SIGKILL, again and
// again, in the middle of its transactions and in the middle of a field type
// change. After each kill the database must pass integrity_check, keep
// every transaction the writer was told had committed, and hold no part of
// one that had not.
func (b *bench) crash(ctx context.Context) (map[string]any, error) {
	dir := filepath.Join(b.adminDSN, "crash")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "crash.sqlite")
	if err := dbal.CreateSQLiteDatabase(ctx, path); err != nil {
		return nil, err
	}
	setup, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: path})
	if err != nil {
		return nil, err
	}
	for _, q := range []string{
		"CREATE TABLE crash_log (batch INTEGER NOT NULL, item INTEGER NOT NULL, payload TEXT, PRIMARY KEY (batch, item))",
		"CREATE TABLE crash_total (id INTEGER PRIMARY KEY, n INTEGER NOT NULL)",
		"INSERT INTO crash_total VALUES (1, 0)",
	} {
		if _, err := setup.DB().ExecContext(ctx, q); err != nil {
			return nil, err
		}
	}
	setup.Close()
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	r := rand.New(rand.NewPCG(seed, 5))

	writes := map[string]any{}
	const writeKills = 40
	lost, partial, failedChecks, acked := 0, 0, 0, 0
	for i := range writeKills {
		last, err := runAndKill(ctx, self, []string{"-db", path, "-mode", "write"}, time.Duration(100+r.IntN(900))*time.Millisecond)
		if err != nil {
			return nil, err
		}
		res, err := checkCrashLog(ctx, path, last)
		if err != nil {
			return nil, fmt.Errorf("after kill %d: %w", i+1, err)
		}
		acked = last
		lost += res.lost
		partial += res.partial
		if !res.ok {
			failedChecks++
		}
	}
	writes["kills"] = writeKills
	writes["batches_acknowledged"] = acked
	writes["acknowledged_batches_lost"] = lost
	writes["partial_batches"] = partial
	writes["failed_integrity_or_total"] = failedChecks

	ddl := map[string]any{}
	const ddlKills = 12
	survivors := map[string]int{}
	ddlFailures := 0
	for range ddlKills {
		if err := resetRetypeTable(ctx, path); err != nil {
			return nil, err
		}
		if _, err := runAndKill(ctx, self, []string{"-db", path, "-mode", "ddl"}, time.Duration(50+r.IntN(1500))*time.Millisecond); err != nil {
			return nil, err
		}
		state, err := checkRetype(ctx, path)
		if err != nil {
			return nil, err
		}
		survivors[state]++
		if !strings.HasPrefix(state, "ok") {
			ddlFailures++
		}
	}
	ddl["kills"] = ddlKills
	ddl["states"] = survivors
	ddl["inconsistent"] = ddlFailures
	return map[string]any{"write_transactions": writes, "type_change": ddl,
		"note": "SIGKILL of the writing process; a VM reset or power loss was not simulated"}, nil
}

// runAndKill runs the crash child and kills it with SIGKILL after d. It
// returns the last batch the child said had committed.
func runAndKill(ctx context.Context, self string, args []string, d time.Duration) (int, error) {
	cmd := exec.CommandContext(ctx, self, append([]string{"crash-child"}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	last := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var batch int
			if _, err := fmt.Sscanf(sc.Text(), "committed %d", &batch); err == nil {
				last = batch
			}
		}
	}()
	time.Sleep(d)
	_ = cmd.Process.Signal(syscall.SIGKILL)
	<-done
	_ = cmd.Wait()
	return last, nil
}

type crashCheck struct {
	ok            bool
	lost, partial int
}

func batchSize(batch int) int { return 1 + batch%50 }

func checkCrashLog(ctx context.Context, path string, acked int) (crashCheck, error) {
	drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: path})
	if err != nil {
		return crashCheck{}, err
	}
	defer drv.Close()
	db := drv.DB()
	var integrity string
	var total, rows int
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return crashCheck{}, err
	}
	if err := db.QueryRowContext(ctx, "SELECT n FROM crash_total").Scan(&total); err != nil {
		return crashCheck{}, err
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM crash_log").Scan(&rows); err != nil {
		return crashCheck{}, err
	}
	res := crashCheck{ok: integrity == "ok" && total == rows}
	counts := map[int]int{}
	q, err := db.QueryContext(ctx, "SELECT batch, COUNT(*) FROM crash_log GROUP BY batch")
	if err != nil {
		return res, err
	}
	defer q.Close()
	for q.Next() {
		var batch, n int
		if err := q.Scan(&batch, &n); err != nil {
			return res, err
		}
		counts[batch] = n
		if n != batchSize(batch) {
			res.partial++
		}
	}
	for batch := 1; batch <= acked; batch++ {
		if counts[batch] != batchSize(batch) {
			res.lost++
		}
	}
	return res, q.Err()
}

const retypeRows = 200000

func resetRetypeTable(ctx context.Context, path string) error {
	drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: path})
	if err != nil {
		return err
	}
	defer drv.Close()
	ddl, err := drv.Dialect().BuildCreateTableSQL(dbal.TableDefinition{Name: "retype", Columns: []dbal.ColumnDefinition{
		{Name: "id", Type: dbal.FieldTypeText, IsPrimaryKey: true},
		{Name: "code", Type: dbal.FieldTypeText, IsNullable: true},
	}})
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DROP TABLE IF EXISTS retype`, ddl, `CREATE INDEX retype_code ON retype (code)`,
		fmt.Sprintf(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < %d) `+
			`INSERT INTO retype SELECT 'r' || i, CAST(i AS TEXT) FROM n`, retypeRows),
	} {
		if _, err := drv.DB().ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// checkRetype names the state a killed type change left: "ok text" (not
// done), "ok number" (done), or what is wrong.
func checkRetype(ctx context.Context, path string) (string, error) {
	drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: path})
	if err != nil {
		return "", err
	}
	defer drv.Close()
	db := drv.DB()
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return "", err
	}
	if integrity != "ok" {
		return "integrity: " + integrity, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT name, type FROM pragma_table_info('retype') ORDER BY cid`)
	if err != nil {
		return "", err
	}
	var cols []string
	codeType := ""
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			rows.Close()
			return "", err
		}
		cols = append(cols, name)
		if name == "code" {
			codeType = typ
		}
	}
	rows.Close()
	var n, indexes int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(code) FROM retype`).Scan(&n); err != nil {
		return "", err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'index' AND name = 'retype_code'`).Scan(&indexes); err != nil {
		return "", err
	}
	if strings.Join(cols, ",") != "id,code" || n != retypeRows || indexes != 1 {
		return fmt.Sprintf("broken: columns %v, %d values, %d indexes", cols, n, indexes), nil
	}
	if codeType == "NUMERIC" {
		return "ok number", nil
	}
	return "ok text", nil
}

// crashChild is the process gate 5 kills. In write mode it commits batches
// of rows, saying so after each commit; in ddl mode it changes the type of
// retype.code.
func crashChild(args []string) {
	fs := flag.NewFlagSet("crash-child", flag.ExitOnError)
	path := fs.String("db", "", "database file")
	mode := fs.String("mode", "write", "write or ddl")
	_ = fs.Parse(args)
	ctx := context.Background()
	drv, err := dbal.Connect(dbal.DriverConfig{EngineType: dbal.EngineSQLite, DSN: *path})
	if err != nil {
		log.Fatal(err)
	}
	db := drv.DB()
	if *mode == "ddl" {
		stmt, err := drv.Dialect().BuildAlterColumnTypeSQL("retype", "code", dbal.FieldTypeNumber)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			log.Fatal(err)
		}
		fmt.Println("done")
		select {}
	}
	var batch int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(batch), 0) FROM crash_log").Scan(&batch); err != nil {
		log.Fatal(err)
	}
	payload := strings.Repeat("x", 200)
	w := bufio.NewWriter(os.Stdout)
	for {
		batch++
		k := batchSize(batch)
		if err := func() error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			for item := range k {
				if _, err := tx.ExecContext(ctx, "INSERT INTO crash_log VALUES ($1, $2, $3)", batch, item, payload); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, "UPDATE crash_total SET n = n + $1", k); err != nil {
				return err
			}
			return tx.Commit()
		}(); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Fatal(err)
		}
		fmt.Fprintf(w, "committed %d\n", batch)
		_ = w.Flush()
	}
}

// unsafeStorage is gate 6: what the storage check says of this machine's
// filesystems. A FUSE mount stands in for Docker Desktop's bind mounts.
func unsafeStorage() map[string]any {
	out := map[string]any{}
	dirs := []string{"/var/tmp", os.TempDir(), "/var/lib/lxcfs", fmt.Sprintf("/run/user/%d/gvfs", os.Getuid())}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		fs, refused := dbal.SQLiteFilesystem(dir)
		entry := map[string]any{"filesystem": fs, "refused": refused}
		if err := dbal.CheckSQLiteStorage(dir); err != nil {
			entry["error"] = err.Error()
		}
		out[dir] = entry
	}
	return out
}
