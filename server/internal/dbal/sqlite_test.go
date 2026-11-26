package dbal

import (
	"context"
	"database/sql"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/calc"
)

// newSQLiteDB creates a database file in a temporary directory and opens
// its pool. SQLite needs no server, so these tests always run.
func newSQLiteDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "solution"+SQLiteFileExtension)
	require.NoError(t, CreateSQLiteDatabase(ctx, path))
	db, err := openSQLite(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

// Every connection of a pool runs with the connection contract, not only the
// first: the per-connection settings come from the DSN.
func TestSQLite_EveryPooledConnectionHasTheContract(t *testing.T) {
	db, path := newSQLiteDB(t)
	ctx := context.Background()
	db.SetMaxOpenConns(4)

	// Hold three connections at once, so the pool opens three.
	var conns []*sql.Conn
	for i := 0; i < 3; i++ {
		c, err := db.Conn(ctx)
		require.NoError(t, err)
		conns = append(conns, c)
	}
	for i, c := range conns {
		var mode, sync string
		var timeout, fk, limit int64
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&sync))
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout))
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk))
		require.NoError(t, c.QueryRowContext(ctx, "PRAGMA journal_size_limit").Scan(&limit))
		assert.Equal(t, "wal", mode, "connection %d", i)
		assert.Equal(t, "2", sync, "connection %d: synchronous=FULL", i)
		assert.Equal(t, SQLiteBusyTimeout().Milliseconds(), timeout, "connection %d", i)
		assert.EqualValues(t, 1, fk, "connection %d", i)
		assert.EqualValues(t, sqliteJournalSizeLimit, limit, "connection %d", i)
	}
	for _, c := range conns {
		require.NoError(t, c.Close())
	}

	diag, err := DiagnoseSQLite(ctx, db, path)
	require.NoError(t, err)
	assert.True(t, diag.OK(), "%v", diag.Problems)
	assert.Equal(t, "FULL", diag.Synchronous)
}

// A database is created in WAL mode, and a copy that is not (VACUUM INTO
// writes one in rollback-journal mode) is turned to WAL when it is opened.
func TestSQLite_WALIsSetAtCreationAndReadBack(t *testing.T) {
	db, path := newSQLiteDB(t)
	ctx := context.Background()
	require.NoError(t, db.Close())

	raw := sql.OpenDB(&sqliteConnector{dsn: sqliteReadOnlyDSN(path)})
	var mode string
	require.NoError(t, raw.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	require.NoError(t, raw.Close())
	assert.Equal(t, "wal", mode, "the mode is in the file, not only in a connection")

	copyPath := filepath.Join(filepath.Dir(path), "copy"+SQLiteFileExtension)
	db, err := openSQLite(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(ctx, "VACUUM INTO $1", copyPath)
	require.NoError(t, err)
	copied, err := openSQLite(ctx, copyPath)
	require.NoError(t, err)
	defer copied.Close()
	require.NoError(t, copied.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)
}

// Opening a pool never creates a database: a name that names none fails as
// it does on a server.
func TestSQLite_OpeningNeverCreatesAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing"+SQLiteFileExtension)
	_, err := openSQLite(context.Background(), path)
	require.Error(t, err)
	_, statErr := os.Stat(path)
	assert.True(t, errors.Is(statErr, os.ErrNotExist))
}

func TestSQLite_CreateRefusesAnExistingFile(t *testing.T) {
	_, path := newSQLiteDB(t)
	assert.ErrorIs(t, CreateSQLiteDatabase(context.Background(), path), ErrDatabaseExists)
}

// When SQLite rolls a transaction back by itself, every later statement of
// it and its commit fail, so the work never commits in part. A trigger's
// RAISE(ROLLBACK) ends the transaction as SQLITE_FULL or IOERR would.
func TestSQLite_ATransactionSQLiteEndedCannotCommit(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (v TEXT);
		CREATE TRIGGER t_abort BEFORE INSERT ON t WHEN NEW.v = 'abort' BEGIN SELECT RAISE(ROLLBACK, 'aborted'); END;`)
	require.NoError(t, err)

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO t VALUES ('kept?')`)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO t VALUES ('abort')`)
	require.ErrorIs(t, err, ErrSQLiteTransactionEnded)
	_, err = tx.ExecContext(ctx, `INSERT INTO t VALUES ('after')`)
	require.ErrorIs(t, err, ErrSQLiteTransactionEnded, "nothing runs outside the transaction it was written for")
	assert.ErrorIs(t, tx.Commit(), ErrSQLiteTransactionEnded)

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n))
	assert.Zero(t, n)

	// A statement error that leaves the transaction open keeps it usable.
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO missing VALUES (1)`)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrSQLiteTransactionEnded))
	_, err = tx.ExecContext(ctx, `INSERT INTO t VALUES ('ok')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n))
	assert.Equal(t, 1, n)
}

// Write transactions begin IMMEDIATE: two that read then write wait for the
// writer lock instead of failing with SQLITE_BUSY halfway through.
func TestSQLite_ReadThenWriteTransactionsDoNotDeadlock(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE counter (n INTEGER); INSERT INTO counter VALUES (0);`)
	require.NoError(t, err)
	db.SetMaxOpenConns(8)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				errs <- err
				return
			}
			defer tx.Rollback()
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT n FROM counter`).Scan(&n); err != nil {
				errs <- err
				return
			}
			time.Sleep(time.Millisecond)
			if _, err := tx.ExecContext(ctx, `UPDATE counter SET n = $1`, n+1); err != nil {
				errs <- err
				return
			}
			errs <- tx.Commit()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT n FROM counter`).Scan(&n))
	assert.Equal(t, 16, n, "no update was lost")
}

// Writers of the process wait for the writer lock in the order they asked
// for it, and no longer than busy_timeout; a statement outside a
// transaction waits its turn as well.
func TestSQLite_WritersTakeTurnsAndWaitAtMostTheBusyTimeout(t *testing.T) {
	previous := SQLiteBusyTimeout()
	SetSQLiteBusyTimeout(300 * time.Millisecond)
	t.Cleanup(func() { SetSQLiteBusyTimeout(previous) })
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	db.SetMaxOpenConns(12)
	_, err := db.ExecContext(ctx, `CREATE TABLE turns (n INTEGER)`)
	require.NoError(t, err)

	holder, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	start := time.Now()
	_, err = db.BeginTx(ctx, nil)
	assert.ErrorIs(t, err, ErrSQLiteBusy)
	assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
	assert.Less(t, time.Since(start), 2*time.Second, "the wait is bounded by busy_timeout")
	_, err = db.ExecContext(ctx, `INSERT INTO turns VALUES (-1)`)
	assert.ErrorIs(t, err, ErrSQLiteBusy, "a statement on its own waits for the lock too")
	var n int
	err = db.QueryRowContext(ctx, `INSERT INTO turns VALUES (-2) RETURNING n`).Scan(&n)
	assert.ErrorIs(t, err, ErrSQLiteBusy, "so does a query that writes")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM turns`).Scan(&n), "a query that reads does not wait")
	// A backup reads a snapshot: it does not wait for the writers, nor
	// hold them up.
	require.NoError(t, BackupSQLite(ctx, db, filepath.Join(t.TempDir(), "during.backup")))

	// Eight writers queue one after the other while the lock is held, then
	// get it in that order.
	SetSQLiteBusyTimeout(10 * time.Second)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := db.BeginTx(ctx, nil)
			if !assert.NoError(t, err) {
				return
			}
			defer tx.Rollback()
			_, err = tx.ExecContext(ctx, `INSERT INTO turns VALUES ($1)`, i)
			assert.NoError(t, err)
			assert.NoError(t, tx.Commit())
		}()
		time.Sleep(20 * time.Millisecond)
	}
	require.NoError(t, holder.Rollback())
	wg.Wait()

	rows, err := db.QueryContext(ctx, `SELECT n FROM turns ORDER BY rowid`)
	require.NoError(t, err)
	defer rows.Close()
	var order []int
	for rows.Next() {
		var n int
		require.NoError(t, rows.Scan(&n))
		order = append(order, n)
	}
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7}, order)
}

func TestSQLite_UnicodeCaseFunctions(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	var lower, upper string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT lower($1), upper($2)`,
		"İSTANBUL ıi ÉCOLE STRASSE ẞ Ärger", "ß ı i école").Scan(&lower, &upper))
	// Character by character, as calculations and PostgreSQL map case.
	assert.Equal(t, strings.Map(toLower, "İSTANBUL ıi ÉCOLE STRASSE ẞ Ärger"), lower)
	assert.Equal(t, "istanbul ıi école strasse ß ärger", lower, "as PostgreSQL lowers it")
	assert.Equal(t, "ß I I ÉCOLE", upper)

	var like bool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT LOWER('ÉCOLE Zürich') LIKE LOWER($1) ESCAPE '\'`, "école z%").Scan(&like))
	assert.True(t, like)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT LOWER('100%') LIKE LOWER($1) ESCAPE '\'`, `100\%`).Scan(&like))
	assert.True(t, like)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT LOWER('1000') LIKE LOWER($1) ESCAPE '\'`, `100\%`).Scan(&like))
	assert.False(t, like, "a backslash escapes a wildcard")

	var null sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `SELECT lower(NULL)`).Scan(&null))
	assert.False(t, null.Valid)
}

func toLower(r rune) rune { return []rune(strings.ToLower(string(r)))[0] }

func TestSQLite_RegexpAndLines(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	for text, want := range map[string]bool{"12": true, "-1.5": true, "1e3": false, "abc": false, "": false} {
		var got bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT $1 REGEXP '`+NumericPattern+`'`, text).Scan(&got))
		assert.Equal(t, want, got, text)
	}
	var null sql.NullBool
	require.NoError(t, db.QueryRowContext(ctx, `SELECT NULL REGEXP 'x'`).Scan(&null))
	assert.False(t, null.Valid)

	var lines string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT f4b_lines($1)`, "a\r\nb\rc\n\nd\n").Scan(&lines))
	assert.Equal(t, `["a","b","c","d"]`, lines)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT f4b_lines(NULL)`).Scan(&lines))
	assert.Equal(t, `[]`, lines)
}

// The F4B collation orders text exactly as calc.CompareText does; its
// allocation-free path for ASCII must agree with it on every pair.
// A summary's standard deviation is PostgreSQL's STDDEV_SAMP.
func TestSQLite_StandardDeviation(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (g TEXT, n NUMERIC); INSERT INTO t VALUES ('a', 1), ('a', 3), ('a', NULL), ('b', 2.5), ('c', NULL)`)
	require.NoError(t, err)
	rows, err := db.QueryContext(ctx, `SELECT g, STDDEV_SAMP(n) FROM t GROUP BY g ORDER BY g`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]interface{}{}
	for rows.Next() {
		var g string
		var v sql.NullFloat64
		require.NoError(t, rows.Scan(&g, &v))
		if v.Valid {
			got[g] = v.Float64
		} else {
			got[g] = nil
		}
	}
	require.NoError(t, rows.Err())
	assert.InDelta(t, 1.4142135623730951, got["a"], 1e-12)
	assert.Nil(t, got["b"], "one number has no sample deviation")
	assert.Nil(t, got["c"])
}

func TestSQLite_SortCollationIsCompareText(t *testing.T) {
	alphabet := []string{"a", "A", "b", "B", "e", "E", "z", "Z", " ", "-", "0", "9", "é", "É", "è", "ß", "ẞ", "ı", "İ", "i", "I", "æ", "œ", "ø", "ü", "́"}
	r := rand.New(rand.NewSource(1))
	random := func(ascii bool) string {
		var b strings.Builder
		for n := r.Intn(6); n > 0; n-- {
			c := alphabet[r.Intn(len(alphabet))]
			if ascii {
				c = string(rune(32 + r.Intn(95)))
			}
			b.WriteString(c)
		}
		return b.String()
	}
	sign := func(n int) int {
		switch {
		case n < 0:
			return -1
		case n > 0:
			return 1
		}
		return 0
	}
	for i := 0; i < 50000; i++ {
		a, b := random(i%2 == 0), random(i%3 == 0)
		if i%5 == 0 {
			b = strings.ToUpper(a)
		}
		require.Equal(t, sign(calc.CompareText(a, b)), sign(sortCollation(a, b)), "%q vs %q", a, b)
	}

	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE words (w TEXT)`)
	require.NoError(t, err)
	words := []string{"zèbre", "Éclair", "eclair", "apple", "Banana", "cherry", "straße", "strasse", "Ärger", "Zürich", "İstanbul", "ısık"}
	for _, w := range words {
		_, err := db.ExecContext(ctx, `INSERT INTO words VALUES ($1)`, w)
		require.NoError(t, err)
	}
	rows, err := db.QueryContext(ctx, `SELECT w FROM words ORDER BY w COLLATE `+SQLiteSortCollation)
	require.NoError(t, err)
	var got []string
	for rows.Next() {
		var w string
		require.NoError(t, rows.Scan(&w))
		got = append(got, w)
	}
	require.NoError(t, rows.Err())
	want := append([]string(nil), words...)
	slices.SortFunc(want, calc.CompareText)
	assert.Equal(t, want, got)
}

// ALTER TABLE forms SQLite lacks run as on PostgreSQL, each whole or not at
// all.
func TestSQLite_ColumnChanges(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (id TEXT NOT NULL PRIMARY KEY, v TEXT)`)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		_, err = db.ExecContext(ctx, `ALTER TABLE t ADD COLUMN IF NOT EXISTS extra VARCHAR(10) DEFAULT 'x'`)
		require.NoError(t, err, "adding a column that is there does nothing")
	}
	for i := 0; i < 2; i++ {
		_, err = db.ExecContext(ctx, `ALTER TABLE "t" DROP COLUMN IF EXISTS "extra";`)
		require.NoError(t, err, "dropping a column that is not there does nothing")
	}
	assert.False(t, sqliteColumn(t, db, "t", "extra"))

	// An indexed column drops with its index, as on the other engines.
	_, err = db.ExecContext(ctx, `CREATE UNIQUE INDEX uq_v ON t (v) WHERE v IS NOT NULL AND v <> ''`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE "t" DROP COLUMN "v"`)
	require.NoError(t, err)
	assert.False(t, sqliteColumn(t, db, "t", "v"))

	// A type change keeps the values, an empty text as NULL, and the indexes.
	_, err = db.ExecContext(ctx, `ALTER TABLE t ADD COLUMN n TEXT; CREATE UNIQUE INDEX uq_n ON t (n) WHERE n IS NOT NULL`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO t (id, n) VALUES ('a', '12'), ('b', ''), ('c', '1.5')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE "t" ALTER COLUMN "n" TYPE NUMERIC;`)
	require.NoError(t, err)
	var types string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT group_concat(typeof(n), ',') FROM (SELECT n FROM t ORDER BY id)`).Scan(&types))
	assert.Equal(t, "integer,null,real", types)
	_, err = db.ExecContext(ctx, `INSERT INTO t (id, n) VALUES ('d', 12)`)
	assert.True(t, IsDuplicateKey(err), "the unique index came along: %v", err)

	// A value the new type cannot read refuses the change, which leaves the
	// table as it was.
	_, err = db.ExecContext(ctx, `ALTER TABLE t ADD COLUMN d TEXT`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE t SET d = 'not a date' WHERE id = 'a'`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE t ALTER COLUMN d TYPE DATE`)
	require.Error(t, err)
	assert.False(t, sqliteColumn(t, db, "t", "d$retype"))
	var declared string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT type FROM pragma_table_info('t') WHERE name = 'd'`).Scan(&declared))
	assert.Equal(t, "TEXT", declared)

	// A date or a timestamp a client wrote as text is stored as one once the
	// column is retyped, so a find for that instant matches it.
	_, err = db.ExecContext(ctx, `CREATE TABLE w (id TEXT, ts TEXT, d TEXT);
		INSERT INTO w VALUES ('a', '2026-03-04T12:34:00Z', '2026-03-04'), ('b', '2026-03-04 14:34:00.25+02:00', '2026-03-04T10:00:00Z'), ('c', '', NULL)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE w ALTER COLUMN ts TYPE TIMESTAMP`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE w ALTER COLUMN d TYPE DATE`)
	require.NoError(t, err)
	var stamps, days string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT group_concat(coalesce(ts, '-'), ','), group_concat(coalesce(d, '-'), ',') FROM (SELECT ts, d FROM w ORDER BY id)`).Scan(&stamps, &days))
	assert.Equal(t, "2026-03-04 12:34:00+00:00,2026-03-04 12:34:00.25+00:00,-", stamps)
	assert.Equal(t, "2026-03-04,2026-03-04,-", days)
	_, err = db.ExecContext(ctx, `CREATE TABLE bad (id TEXT, d TEXT); INSERT INTO bad VALUES ('a', '2026-02-30')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE bad ALTER COLUMN d TYPE DATE`)
	assert.Error(t, err, "a day not in the calendar is refused, not moved to 2 March")

	// A required column is not changed: the change would drop NOT NULL.
	_, err = db.ExecContext(ctx, `CREATE TABLE r (id TEXT); ALTER TABLE r ADD COLUMN req TEXT NOT NULL; INSERT INTO r VALUES ('a', '')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE r ALTER COLUMN req TYPE NUMERIC`)
	require.ErrorContains(t, err, "NOT NULL")
	var notnull int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT "notnull" FROM pragma_table_info('r') WHERE name = 'req'`).Scan(&notnull))
	assert.Equal(t, 1, notnull)

	// Nothing is left open on the connection after a failed change.
	_, err = db.ExecContext(ctx, `BEGIN; COMMIT;`)
	require.NoError(t, err)
}

// A table named like one of the table-valued functions the connection and
// the queries call does not hide the function.
func TestSQLite_TablesNamedLikeFunctions(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE pragma_table_info (x TEXT); CREATE TABLE pragma_index_xinfo (x TEXT); CREATE TABLE json_each (x TEXT);
		CREATE TABLE t (id TEXT, v TEXT); CREATE INDEX iv ON t (v)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE t ADD COLUMN IF NOT EXISTS v TEXT`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `ALTER TABLE t DROP COLUMN v`)
	require.NoError(t, err, "the column's index is found and dropped")
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+CatalogColumnsSQL(EngineSQLite)+` AS c WHERE c.table_name = 't'`).Scan(&n))
	assert.Equal(t, 1, n)
}

// A column change whose request is cancelled once its steps are done still
// ends its savepoint: left open, the savepoint would be a transaction the
// connection carries to its next user, whose writes would then vanish.
func TestSQLite_ACancelledColumnChangeLeavesNoTransactionOpen(t *testing.T) {
	db, path := newSQLiteDB(t)
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (id TEXT NOT NULL PRIMARY KEY, n TEXT)`)
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	beforeSavepointRelease = cancel
	t.Cleanup(func() { beforeSavepointRelease = func() {} })
	_, err = db.ExecContext(cancelled, `ALTER TABLE t ALTER COLUMN n TYPE NUMERIC`)
	beforeSavepointRelease = func() {}
	require.NoError(t, err, "the change was whole when the request was cancelled")

	// The next user of the only connection writes outside any transaction.
	_, err = db.ExecContext(ctx, `INSERT INTO t (id, n) VALUES ('a', 1)`)
	require.NoError(t, err)
	other, err := openSQLite(ctx, path)
	require.NoError(t, err)
	defer other.Close()
	var n int
	require.NoError(t, other.QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&n))
	assert.Equal(t, 1, n, "the write is committed, so another connection reads it")
	var declared string
	require.NoError(t, other.QueryRowContext(ctx, `SELECT type FROM pragma_table_info('t') WHERE name = 'n'`).Scan(&declared))
	assert.Equal(t, "NUMERIC", declared)
}

func sqliteColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info($1) WHERE name = $2`, table, column).Scan(&n))
	return n > 0
}

// A column holds the values of its type only, as on PostgreSQL.
func TestSQLite_TypeChecks(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	columns := []string{}
	for name, typ := range map[string]string{"n": "NUMERIC", "d": "DATE", "ts": "TIMESTAMP", "b": "BOOLEAN"} {
		columns = append(columns, `"`+name+`" `+typ+` `+SQLiteTypeCheck(`"`+name+`"`, typ))
	}
	_, err := db.ExecContext(ctx, `CREATE TABLE t (`+strings.Join(columns, ", ")+`)`)
	require.NoError(t, err)
	for _, ok := range []string{
		`INSERT INTO t (n) VALUES ('12.5')`, `INSERT INTO t (n) VALUES (NULL)`,
		`INSERT INTO t (d) VALUES ('2026-01-02')`, `INSERT INTO t (ts) VALUES ('2026-01-02 03:04:05.5+00:00')`,
		`INSERT INTO t (b) VALUES (1)`, `INSERT INTO t (b) VALUES ('0')`,
	} {
		_, err := db.ExecContext(ctx, ok)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{
		`INSERT INTO t (n) VALUES ('NOT_A_NUMBER')`, `INSERT INTO t (d) VALUES ('soon')`,
		`INSERT INTO t (d) VALUES (2460000)`, `INSERT INTO t (b) VALUES (2)`, `INSERT INTO t (b) VALUES ('yes')`,
		`INSERT INTO t (d) VALUES ('2026-02-30')`, `INSERT INTO t (d) VALUES ('13:00')`, `INSERT INTO t (d) VALUES ('2026-01-02 03:04')`,
		`INSERT INTO t (ts) VALUES ('2026-02-30 10:00:00+00:00')`, `INSERT INTO t (ts) VALUES ('13:00')`, `INSERT INTO t (ts) VALUES ('2026-03-04T12:34:00Z')`,
	} {
		_, err := db.ExecContext(ctx, bad)
		assert.Error(t, err, bad)
	}
}

// Values come back as the PostgreSQL driver gives them.
func TestSQLite_ValuesReadAsOnPostgreSQL(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (n NUMERIC, b BOOLEAN, blob BLOB, ts TIMESTAMP)`)
	require.NoError(t, err)
	at := time.Date(2026, 1, 2, 3, 4, 5, 500_000_000, time.FixedZone("CET", 3600))
	_, err = db.ExecContext(ctx, `INSERT INTO t VALUES ($1, $2, $3, $4)`, 1000000, true, []byte{}, at)
	require.NoError(t, err)
	var n, sum string
	var b bool
	var blob interface{}
	var ts time.Time
	var stored string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT n, b, blob, ts, SUM(n) * 1.5, CAST(ts AS TEXT) FROM t`).Scan(&n, &b, &blob, &ts, &sum, &stored))
	assert.Equal(t, "1000000", n)
	assert.Equal(t, "1500000", sum, "never 1.5e+06")
	assert.True(t, b)
	assert.Equal(t, []byte{}, blob, "an empty BLOB is not NULL")
	assert.True(t, ts.Equal(at))
	assert.Equal(t, "2026-01-02 02:04:05.5+00:00", stored, "stored in UTC, so text order is time order")
}

func TestSQLite_ErrorsAreRecognized(t *testing.T) {
	db, _ := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `SELECT * FROM missing`)
	assert.True(t, IsUndefinedTable(err), "%v", err)
	_, err = db.ExecContext(ctx, `CREATE TABLE t (id TEXT PRIMARY KEY, u TEXT UNIQUE); INSERT INTO t VALUES ('a', 'x')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO t VALUES ('a', 'y')`)
	assert.True(t, IsDuplicateKey(err), "%v", err)
	_, err = db.ExecContext(ctx, `INSERT INTO t VALUES ('b', 'x')`)
	assert.True(t, IsDuplicateKey(err), "%v", err)
	assert.False(t, IsUndefinedTable(err))
}

// A backup is a checked, complete copy published under its name only once
// it is whole; it restores to a new database.
func TestSQLite_BackupAndRestore(t *testing.T) {
	db, path := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (v TEXT); INSERT INTO t VALUES ('one'), ('two')`)
	require.NoError(t, err)
	dir := filepath.Dir(path)
	backup := filepath.Join(dir, "backup.sqlite")
	require.NoError(t, BackupSQLite(ctx, db, backup))
	assert.Error(t, BackupSQLite(ctx, db, backup), "an existing backup is never replaced")

	restored := filepath.Join(dir, "restored"+SQLiteFileExtension)
	require.NoError(t, RestoreSQLiteDatabase(ctx, backup, restored))
	assert.ErrorIs(t, RestoreSQLiteDatabase(ctx, backup, restored), ErrDatabaseExists)
	rdb, err := openSQLite(ctx, restored)
	require.NoError(t, err)
	defer rdb.Close()
	var values string
	require.NoError(t, rdb.QueryRowContext(ctx, `SELECT group_concat(v, ',') FROM (SELECT v FROM t ORDER BY v)`).Scan(&values))
	assert.Equal(t, "one,two", values)
	diag, err := DiagnoseSQLite(ctx, rdb, restored)
	require.NoError(t, err)
	assert.True(t, diag.OK(), "%v", diag.Problems)

	info, err := os.Stat(backup)
	require.NoError(t, err)
	assert.Zero(t, info.Mode().Perm()&0o077, "a backup is its owner's only, as the database is, whatever the umask")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "."), "no temporary file is left: %s", e.Name())
	}
}

// A restore takes a whole backup only: SQLite reads an empty or truncated
// file as a database, which quick_check passes, and a database copied with
// its -wal may hold its last commits in the WAL, which a restore leaves out.
func TestSQLite_RestoreRefusesAnIncompleteBackup(t *testing.T) {
	db, path := newSQLiteDB(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `CREATE TABLE t (v TEXT); WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 200) INSERT INTO t SELECT hex(randomblob(500)) FROM n`)
	require.NoError(t, err)
	dir := filepath.Dir(path)
	backup := filepath.Join(dir, "backup.sqlite")
	require.NoError(t, BackupSQLite(ctx, db, backup))
	whole, err := os.ReadFile(backup)
	require.NoError(t, err)

	empty := filepath.Join(dir, "empty.backup")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	truncated := filepath.Join(dir, "truncated.backup")
	require.NoError(t, os.WriteFile(truncated, whole[:len(whole)/2], 0o600))
	withWAL := filepath.Join(dir, "copied.backup")
	require.NoError(t, os.WriteFile(withWAL, whole, 0o600))
	require.NoError(t, os.WriteFile(withWAL+"-wal", []byte("frames"), 0o600))

	for _, bad := range []string{empty, truncated, withWAL} {
		dest := filepath.Join(dir, "restored"+SQLiteFileExtension)
		assert.Error(t, RestoreSQLiteDatabase(ctx, bad, dest), filepath.Base(bad))
		_, err := os.Stat(dest)
		assert.True(t, errors.Is(err, os.ErrNotExist), "%s: nothing is published", filepath.Base(bad))
	}
}

// A database is never created over the -wal or -shm of an earlier one of
// the same name (a crash during a drop leaves them): SQLite would replay
// that WAL into it. A failed creation leaves no file under the name.
func TestSQLite_CreateRefusesTheFilesOfAnEarlierDatabase(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "sales"+SQLiteFileExtension)
	require.NoError(t, os.WriteFile(path+"-wal", []byte("old frames"), 0o600))
	assert.Error(t, CreateSQLiteDatabase(ctx, path))
	_, err := os.Stat(path)
	assert.True(t, errors.Is(err, os.ErrNotExist), "no database file is left")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "only the leftover -wal is there")

	require.NoError(t, os.Remove(path+"-wal"))
	require.NoError(t, CreateSQLiteDatabase(ctx, path))
	assert.ErrorIs(t, CreateSQLiteDatabase(ctx, path), ErrDatabaseExists)
	var mode string
	db, err := openSQLite(ctx, path)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
	assert.Equal(t, "wal", mode)
}

// A database file mounted on its own is checked where it is, not only
// where its directory is.
func TestSQLite_OpeningChecksTheFileItsStorage(t *testing.T) {
	db, path := newSQLiteDB(t)
	_ = db.Close()
	filesystemOf = func(p string) (string, bool) {
		if strings.HasSuffix(p, SQLiteFileExtension) {
			return "fakeowner", true
		}
		return "ext4", false
	}
	t.Cleanup(func() { filesystemOf = sqliteFilesystem })
	require.NoError(t, CheckSQLiteStorage(filepath.Dir(path)), "the directory is local")
	_, err := openSQLite(context.Background(), path)
	assert.ErrorIs(t, err, ErrUnsafeSQLiteStorage)
}

func TestSQLite_VersionQualification(t *testing.T) {
	for version, want := range map[string]bool{
		"3.44.5": false, "3.44.6": true, "3.45.0": false, "3.50.6": false, "3.50.7": true,
		"3.51.2": false, "3.51.3": true, "3.52.0": true, "3.53.4": true, "4.0.0": true, "garbage": false,
	} {
		assert.Equal(t, want, SQLiteVersionQualified(version), version)
	}
	db, _ := newSQLiteDB(t)
	var version string
	require.NoError(t, db.QueryRow(`SELECT sqlite_version()`).Scan(&version))
	assert.True(t, SQLiteVersionQualified(version), "the bundled SQLite %s is qualified", version)
}

func TestSQLite_PathsAndDSN(t *testing.T) {
	for in, want := range map[string]string{
		"/data/solutions":         "/data/solutions",
		"file:/data/x.sqlite":     "/data/x.sqlite",
		"sqlite:///data/x.sqlite": "/data/x.sqlite",
		"./data/solutions/":       "data/solutions",
	} {
		got, err := SQLitePath(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", ":memory:", "file:x.sqlite?mode=memory", "x#y"} {
		_, err := SQLitePath(bad)
		assert.Error(t, err, bad)
	}
	dsn := sqliteDSN("/data/a b.sqlite")
	assert.True(t, strings.HasPrefix(dsn, "file:/data/a%20b.sqlite?"), dsn)
	for _, want := range []string{"mode=rw", "_txlock=immediate", "synchronous%28FULL%29", "busy_timeout%28", "foreign_keys%281%29", "journal_size_limit%2867108864%29"} {
		assert.Contains(t, dsn, want)
	}
}
