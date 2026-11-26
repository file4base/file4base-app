package dbal

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
)

// The SQLite connection contract (docs/specs/sqlite_engine.md, "Connection
// contract"). Which settings are persistent and which belong to each
// connection matters, because database/sql opens connections as it needs
// them:
//
//   - journal_mode=WAL is persistent: it is set when a database is created
//     (CreateSQLiteDatabase), and read back when a pool is opened and by the
//     diagnostics, never per request.
//   - synchronous, busy_timeout, foreign_keys and journal_size_limit belong
//     to a connection, so they are in the DSN, which the driver applies to
//     every connection it opens before any statement runs.
//   - Write transactions begin IMMEDIATE (_txlock), so a transaction that
//     reads and then writes never meets SQLITE_BUSY halfway through: it waits
//     for the writer lock, up to busy_timeout, before its first statement.
//   - cache_size, mmap_size, temp_store and wal_autocheckpoint keep SQLite's
//     defaults; no measurement has shown a benefit worth the memory.

const (
	// DefaultSQLiteBusyTimeout bounds how long a statement waits for the
	// writer lock. It was kept after measuring concurrent editing by 50
	// clients (docs/specs/sqlite_engine.md, "Benchmarks").
	DefaultSQLiteBusyTimeout = 5 * time.Second

	// sqliteJournalSizeLimit makes a checkpoint shrink the WAL back to this
	// size after a large import or delete has grown it.
	sqliteJournalSizeLimit = 64 << 20
)

var sqliteBusyTimeout atomic.Int64

func init() {
	sqliteBusyTimeout.Store(int64(DefaultSQLiteBusyTimeout))
}

// SetSQLiteBusyTimeout changes the busy timeout of the SQLite connections
// opened afterwards. It is set once, at start-up (SQLITE_BUSY_TIMEOUT).
func SetSQLiteBusyTimeout(d time.Duration) {
	if d > 0 {
		sqliteBusyTimeout.Store(int64(d))
	}
}

// SQLiteBusyTimeout is the busy timeout new SQLite connections get.
func SQLiteBusyTimeout() time.Duration {
	return time.Duration(sqliteBusyTimeout.Load())
}

// ErrSQLiteBusy is returned when a write did not get the writer lock of its
// database within busy_timeout. Its text is SQLite's own for the same case.
var ErrSQLiteBusy = errors.New("database is locked: no write lock within busy_timeout")

// ErrSQLiteTransactionEnded is returned for every statement, and for the
// commit, of a transaction SQLite rolled back by itself.
var ErrSQLiteTransactionEnded = errors.New("sqlite rolled the transaction back")

// sqliteDSN is the driver DSN of a database file. mode=rw opens an existing
// file and never creates one, so a database name that names no database
// fails as it does on a server instead of becoming an empty file.
func sqliteDSN(path string) string {
	q := url.Values{}
	q.Set("mode", "rw")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", SQLiteBusyTimeout().Milliseconds()))
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("journal_size_limit(%d)", sqliteJournalSizeLimit))
	q.Set("_txlock", "immediate")
	// Times are written as "2006-01-02 15:04:05.999999999-07:00" in UTC, so
	// the text of two instants sorts in time order.
	q.Set("_time_format", "sqlite")
	q.Set("_timezone", "UTC")
	return "file:" + (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath() + "?" + q.Encode()
}

// SQLitePath reads the path of a database file from a DSN: a path, or a
// file: URI without parameters (the connection's own are always used).
func SQLitePath(dsn string) (string, error) {
	path := strings.TrimSpace(dsn)
	if rest, ok := strings.CutPrefix(path, "sqlite://"); ok {
		path = rest
	} else if rest, ok := strings.CutPrefix(path, "file:"); ok {
		path = rest
	}
	if strings.ContainsAny(path, "?#") {
		return "", fmt.Errorf("invalid SQLite DSN %q: give the path of the database file, without parameters", dsn)
	}
	if path == "" || path == ":memory:" {
		return "", fmt.Errorf("invalid SQLite DSN %q: a solution database is a file", dsn)
	}
	return filepath.Clean(path), nil
}

// openSQLite opens the pool of a database file that exists, and checks that
// it is in WAL mode, converting a database that is not (a copy taken with
// VACUUM INTO is in rollback-journal mode until it is converted).
//
// The file's own filesystem is checked, not only its directory's: a file
// can be mounted on its own, from a filesystem the directory is not on.
func openSQLite(ctx context.Context, path string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("sqlite database %s: %w", path, err)
	}
	// The -wal and -shm files are made in the file's directory.
	for _, p := range []string{path, filepath.Dir(path)} {
		if err := CheckSQLiteStorage(p); err != nil {
			return nil, err
		}
	}
	db := sql.OpenDB(newSQLiteConnector(path))
	if err := ensureWAL(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite database %s: %w", path, err)
	}
	return db, nil
}

// ensureWAL reads the journal mode and sets WAL when it is anything else,
// then reads it back: WAL needs a local filesystem with shared memory, and
// SQLite answers with the mode it kept when it cannot.
func ensureWAL(ctx context.Context, db *sql.DB) error {
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if strings.EqualFold(mode, "wal") {
		return nil
	}
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("journal_mode is %q, not wal: keep live databases on a local filesystem", mode)
	}
	return nil
}

// sqliteDriver opens every solution database connection. It is a driver of
// its own, so the functions below reach these connections and not the
// control store's.
var sqliteDriver = &sqlite.Driver{}

type sqliteConnector struct {
	dsn    string
	writer chan struct{}
	path   string
	// file is the database file the pool was opened on. A connection that
	// finds another file under the path (the database was dropped and one
	// of the same name created while it was being opened) is refused.
	file os.FileInfo
}

func newSQLiteConnector(path string) *sqliteConnector {
	file, _ := os.Stat(path)
	return &sqliteConnector{dsn: sqliteDSN(path), writer: writerQueue(path), path: path, file: file}
}

// SQLiteConnector is the connector of an existing database file, with the
// connection contract, for a caller that wraps the connections (tests that
// count or fail statements). openSQLite's check of the journal mode is not
// made.
func SQLiteConnector(path string) driver.Connector {
	return newSQLiteConnector(path)
}

func (c *sqliteConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := sqliteDriver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	if c.file != nil {
		if now, err := os.Stat(c.path); err != nil || !os.SameFile(c.file, now) {
			_ = conn.Close()
			return nil, fmt.Errorf("sqlite database %s was dropped or replaced since its pool was opened", c.path)
		}
	}
	return &sqliteConn{inner: conn, writer: c.writer}, nil
}

// writerQueues holds, for each database file, the writer lock of this
// process: a channel of one slot, which a write transaction (and a write
// statement outside one) takes before SQLite's own lock and gives back when
// it ends. SQLite lets one writer at a time and makes the others poll, with
// growing sleeps, so under load a writer could wait far longer than the
// ones that came after it; a channel serves its waiters in the order they
// came. SQLite's busy_timeout still covers writers of other processes (a
// restore, a maintenance tool).
var writerQueues sync.Map

func writerQueue(path string) chan struct{} {
	q, _ := writerQueues.LoadOrStore(writerQueueKey(path), make(chan struct{}, 1))
	return q.(chan struct{})
}

// forgetWriterQueue drops the queue of a file that is being removed, so the
// map does not grow with every temporary file and dropped database.
func forgetWriterQueue(path string) { writerQueues.Delete(writerQueueKey(path)) }

func writerQueueKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func (c *sqliteConnector) Driver() driver.Driver { return sqliteDriver }

// sqliteConn is a connection that watches its transaction. SQLite rolls a
// transaction back by itself on some errors (SQLITE_FULL, IOERR, NOMEM,
// INTERRUPT, some BUSY), where PostgreSQL and MariaDB keep it open; a
// statement run afterwards would then commit on its own. After any error
// inside a transaction, the connection asks SQLite whether the transaction
// is still open, and when it is not, refuses every later statement and the
// commit with ErrSQLiteTransactionEnded, so the work can never commit in
// part. The decision rests on SQLite's state, not on a list of error codes.
//
// It also runs the ALTER TABLE forms SQLite lacks (see columnDDL), so the
// catalog upgrades and field changes are the same statements on every
// engine; and it hands back NUMERIC and BOOLEAN columns as the PostgreSQL
// driver does (see normalizeSQLiteValue).
type sqliteConn struct {
	inner   driver.Conn
	writer  chan struct{}
	holding bool
	inTx    bool
	ended   bool
	bad     bool // its state is unknown: never reused (discard)
}

var (
	_ driver.Conn               = (*sqliteConn)(nil)
	_ driver.ConnBeginTx        = (*sqliteConn)(nil)
	_ driver.ConnPrepareContext = (*sqliteConn)(nil)
	_ driver.ExecerContext      = (*sqliteConn)(nil)
	_ driver.QueryerContext     = (*sqliteConn)(nil)
	_ driver.Pinger             = (*sqliteConn)(nil)
	_ driver.SessionResetter    = (*sqliteConn)(nil)
	_ driver.Validator          = (*sqliteConn)(nil)
)

func (c *sqliteConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *sqliteConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if err := c.refuseIfEnded(); err != nil {
		return nil, err
	}
	stmt, err := c.inner.(driver.ConnPrepareContext).PrepareContext(ctx, query)
	if err != nil {
		return nil, c.afterError(err)
	}
	return &sqliteStmt{c: c, inner: stmt, query: query}, nil
}

func (c *sqliteConn) Close() error {
	c.releaseWriter()
	return c.inner.Close()
}

// acquireWriter waits for this process's writer lock of the database, in
// turn, up to busy_timeout.
func (c *sqliteConn) acquireWriter(ctx context.Context) error {
	if c.writer == nil || c.holding {
		return nil
	}
	select {
	case c.writer <- struct{}{}:
		c.holding = true
		return nil
	default:
	}
	timer := time.NewTimer(SQLiteBusyTimeout())
	defer timer.Stop()
	select {
	case c.writer <- struct{}{}:
		c.holding = true
		return nil
	case <-timer.C:
		return ErrSQLiteBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// statementWriter takes the writer lock for a statement that writes, run
// outside a transaction: it is a transaction of its own, so it waits its
// turn as a transaction does. Within a transaction the lock is already
// held.
func (c *sqliteConn) statementWriter(ctx context.Context, query string, exec bool) (release func(), err error) {
	if c.inTx || c.holding || !writesDatabase(query, exec) {
		return func() {}, nil
	}
	if err := c.acquireWriter(ctx); err != nil {
		return nil, err
	}
	return c.releaseWriter, nil
}

// writesDatabase tells whether a statement run outside a transaction writes
// to the database. Exec is how File4Base writes, so a statement run with it
// does, but VACUUM INTO, which reads a snapshot and writes another file: a
// backup must not hold the writers up for as long as it takes. A query
// writes when it is an INSERT, UPDATE, DELETE or REPLACE (with RETURNING).
func writesDatabase(query string, exec bool) bool {
	query = skipSQLPreamble(query)
	words := sqlWords(query)
	if len(words) == 0 {
		return exec
	}
	first := strings.ToUpper(words[0])
	if exec {
		// A single VACUUM [schema] INTO file: a batch may write after it.
		into := func(w string) bool { return strings.EqualFold(w, "INTO") }
		single := !strings.Contains(strings.TrimRight(query, " \t\r\n;"), ";")
		return first != "VACUUM" || !single || !slices.ContainsFunc(words[1:min(3, len(words))], into)
	}
	switch first {
	case "INSERT", "UPDATE", "DELETE", "REPLACE":
		return true
	case "WITH":
		// WITH ... INSERT/UPDATE/DELETE ... RETURNING. The words of
		// strings, quoted names and comments are left out.
		return slices.ContainsFunc(sqlWords(unquotedSQL(query))[1:], func(w string) bool {
			switch strings.ToUpper(w) {
			case "INSERT", "UPDATE", "DELETE", "REPLACE":
				return true
			}
			return false
		})
	}
	return false
}

func sqlWords(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		return !(r == '_' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z')
	})
}

// unquotedSQL blanks out the strings, quoted names and comments of a
// statement.
func unquotedSQL(query string) string {
	b := []byte(query)
	for i := 0; i < len(b); i++ {
		var end string
		switch {
		case b[i] == '\'' || b[i] == '"' || b[i] == '`':
			end = string(b[i])
		case b[i] == '[':
			end = "]"
		case b[i] == '-' && i+1 < len(b) && b[i+1] == '-':
			end = "\n"
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			end = "*/"
		default:
			continue
		}
		j := strings.Index(string(b[i+1:]), end)
		stop := len(b)
		if j >= 0 {
			stop = i + 1 + j + len(end)
		}
		for k := i; k < stop; k++ {
			b[k] = ' '
		}
		i = stop - 1
	}
	return string(b)
}

// skipSQLPreamble skips the white space, comments and opening parentheses a
// statement starts with.
func skipSQLPreamble(query string) string {
	for {
		trimmed := strings.TrimLeft(query, " \t\r\n(")
		switch {
		case strings.HasPrefix(trimmed, "--"):
			i := strings.IndexByte(trimmed, '\n')
			if i < 0 {
				return ""
			}
			query = trimmed[i+1:]
		case strings.HasPrefix(trimmed, "/*"):
			i := strings.Index(trimmed[2:], "*/")
			if i < 0 {
				return ""
			}
			query = trimmed[i+4:]
		default:
			return trimmed
		}
	}
}

func (c *sqliteConn) releaseWriter() {
	if c.holding {
		c.holding = false
		<-c.writer
	}
}

func (c *sqliteConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *sqliteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly {
		if err := c.acquireWriter(ctx); err != nil {
			return nil, err
		}
	}
	tx, err := c.inner.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		c.releaseWriter()
		return nil, err
	}
	c.inTx, c.ended = true, false
	return &sqliteTx{c: c, inner: tx}, nil
}

func (c *sqliteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.refuseIfEnded(); err != nil {
		return nil, err
	}
	release, err := c.statementWriter(ctx, query, true)
	if err != nil {
		return nil, err
	}
	defer release()
	if handled, err := c.columnDDL(ctx, query); err != nil {
		return nil, c.afterError(err)
	} else if handled {
		return driver.RowsAffected(0), nil
	}
	res, err := c.inner.(driver.ExecerContext).ExecContext(ctx, query, args)
	if err != nil {
		return nil, c.afterError(err)
	}
	return res, nil
}

func (c *sqliteConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := c.refuseIfEnded(); err != nil {
		return nil, err
	}
	release, err := c.statementWriter(ctx, query, false)
	if err != nil {
		return nil, err
	}
	rows, err := c.inner.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil {
		release()
		return nil, c.afterError(err)
	}
	r := newSQLiteRows(c, rows)
	r.release = release
	return r, nil
}

func (c *sqliteConn) Ping(ctx context.Context) error {
	return c.inner.(driver.Pinger).Ping(ctx)
}

func (c *sqliteConn) ResetSession(ctx context.Context) error {
	if c.bad {
		return driver.ErrBadConn
	}
	return c.inner.(driver.SessionResetter).ResetSession(ctx)
}

func (c *sqliteConn) IsValid() bool {
	return !c.bad && c.inner.(driver.Validator).IsValid()
}

func (c *sqliteConn) refuseIfEnded() error {
	if c.inTx && c.ended {
		return ErrSQLiteTransactionEnded
	}
	if c.bad {
		// The pool retries the statement on another connection.
		return driver.ErrBadConn
	}
	return nil
}

// afterError checks, after a statement of a transaction failed, whether the
// transaction is still open.
func (c *sqliteConn) afterError(err error) error {
	if err == nil || !c.inTx || c.ended {
		return err
	}
	if c.transactionEnded() {
		c.ended = true
		return fmt.Errorf("%w: %w", ErrSQLiteTransactionEnded, err)
	}
	return err
}

// transactionEnded asks SQLite whether the connection is still in a
// transaction: BEGIN is refused within one. Outside one it opens a
// transaction, which is rolled back at once. Any other answer is taken as
// the transaction being gone, so an unclear state fails closed.
func (c *sqliteConn) transactionEnded() bool {
	execer := c.inner.(driver.ExecerContext)
	_, err := execer.ExecContext(context.Background(), "BEGIN", nil)
	if err != nil {
		return !strings.Contains(err.Error(), "within a transaction")
	}
	_, _ = execer.ExecContext(context.Background(), "ROLLBACK", nil)
	return true
}

// The column changes SQLite's ALTER TABLE lacks or does differently. Only
// these statement shapes are read; anything else goes to SQLite unchanged.
var (
	addColumnIfMissing = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+` + sqliteIdent + `\s+ADD\s+COLUMN\s+IF\s+NOT\s+EXISTS\s+` + sqliteIdent + `(\s.*?)\s*;?\s*$`)
	dropColumn         = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+` + sqliteIdent + `\s+DROP\s+COLUMN\s+(IF\s+EXISTS\s+)?` + sqliteIdent + `\s*;?\s*$`)
	alterColumnType    = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+` + sqliteIdent + `\s+ALTER\s+COLUMN\s+` + sqliteIdent + `\s+TYPE\s+(\w+)\s*;?\s*$`)
)

const sqliteIdent = `("(?:[^"]|"")+"|[\w$]+)`

// columnDDL runs a column change SQLite does not have, and reports whether
// query was one:
//
//   - ADD COLUMN IF NOT EXISTS and DROP COLUMN IF EXISTS do nothing when
//     there is nothing to do;
//   - DROP COLUMN drops the indexes of the column first, as PostgreSQL and
//     MariaDB do (SQLite refuses to drop an indexed column);
//   - ALTER COLUMN ... TYPE copies the column into a new one of the new
//     type (see alterSQLiteColumnType).
//
// Each runs in a savepoint, so it is whole or not at all, in a transaction
// or on its own.
func (c *sqliteConn) columnDDL(ctx context.Context, query string) (bool, error) {
	if m := addColumnIfMissing.FindStringSubmatch(query); m != nil {
		return true, c.inSavepoint(ctx, func() error {
			exists, err := c.columnExists(ctx, m[1], m[2])
			if err != nil || exists {
				return err
			}
			return c.exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s%s", m[1], m[2], m[3]))
		})
	}
	if m := dropColumn.FindStringSubmatch(query); m != nil {
		table, ifExists, column := m[1], m[2] != "", m[3]
		return true, c.inSavepoint(ctx, func() error {
			if ifExists {
				exists, err := c.columnExists(ctx, table, column)
				if err != nil || !exists {
					return err
				}
			}
			if _, err := c.dropColumnIndexes(ctx, table, column); err != nil {
				return err
			}
			return c.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, column))
		})
	}
	if m := alterColumnType.FindStringSubmatch(query); m != nil {
		return true, c.inSavepoint(ctx, func() error {
			return c.alterSQLiteColumnType(ctx, m[1], m[2], strings.ToUpper(m[3]))
		})
	}
	return false, nil
}

// alterSQLiteColumnType changes the declared type of a column: its values
// are copied, an empty text as NULL, into a new column of the new type,
// which takes the column's name and indexes once the old one is dropped.
// As on PostgreSQL, the change is refused when a value cannot be read as the
// new type (the new column's SQLiteTypeCheck refuses it). The column moves to the end of the table, which nothing reads:
// every statement names its columns. Dropping the old column rewrites every
// record of the table, holding the writer lock meanwhile.
//
// A NOT NULL column is refused: SQLite cannot give the new column the
// constraint on a table that has records, and the change would drop it.
func (c *sqliteConn) alterSQLiteColumnType(ctx context.Context, table, column, newType string) error {
	required, err := c.queryInt(ctx, `SELECT COUNT(*) FROM temp.pragma_table_info($1) WHERE name = $2 COLLATE NOCASE AND "notnull"`,
		driver.NamedValue{Ordinal: 1, Value: unquoteSQLite(table)}, driver.NamedValue{Ordinal: 2, Value: unquoteSQLite(column)})
	if err != nil {
		return err
	}
	if required > 0 {
		return fmt.Errorf("cannot change %s.%s to %s on SQLite: the column is NOT NULL, which the change would drop; make it optional first",
			unquoteSQLite(table), unquoteSQLite(column), newType)
	}
	temp := `"` + strings.ReplaceAll(unquoteSQLite(column)+"$retype", `"`, `""`) + `"`
	add := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, temp, newType)
	if check := SQLiteTypeCheck(temp, newType); check != "" {
		add += " " + check
	}
	if err := c.exec(ctx, add); err != nil {
		return err
	}
	value := sqliteRetypeValue(fmt.Sprintf("NULLIF(%s, '')", column), newType)
	if err := c.exec(ctx, fmt.Sprintf("UPDATE %s SET %s = %s", table, temp, value)); err != nil {
		return fmt.Errorf("cannot change %s.%s to %s: a value cannot be read as %s: %w",
			unquoteSQLite(table), unquoteSQLite(column), newType, newType, err)
	}
	indexes, err := c.dropColumnIndexes(ctx, table, column)
	if err != nil {
		return err
	}
	if err := c.exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, column)); err != nil {
		return err
	}
	if err := c.exec(ctx, fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", table, temp, column)); err != nil {
		return err
	}
	for _, create := range indexes {
		if err := c.exec(ctx, create); err != nil {
			return err
		}
	}
	return nil
}

// dropColumnIndexes drops the indexes made with CREATE INDEX that read a
// column, and returns the statements that made them.
func (c *sqliteConn) dropColumnIndexes(ctx context.Context, table, column string) ([]string, error) {
	rows, err := c.inner.(driver.QueryerContext).QueryContext(ctx,
		`SELECT DISTINCT m.name, m.sql FROM sqlite_schema AS m, temp.pragma_index_xinfo(m.name) AS x `+
			`WHERE m.type = 'index' AND m.tbl_name = $1 AND m.sql IS NOT NULL AND x.name = $2`,
		[]driver.NamedValue{{Ordinal: 1, Value: unquoteSQLite(table)}, {Ordinal: 2, Value: unquoteSQLite(column)}})
	if err != nil {
		return nil, err
	}
	var names, creates []string
	dest := make([]driver.Value, 2)
	for {
		if err := rows.Next(dest); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			rows.Close()
			return nil, err
		}
		names = append(names, textOf(dest[0]))
		creates = append(creates, textOf(dest[1]))
	}
	rows.Close()
	for _, name := range names {
		if err := c.exec(ctx, `DROP INDEX "`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return nil, err
		}
	}
	return creates, nil
}

// inSavepoint runs fn's statements as one: released when fn succeeds, rolled
// back when it fails. Outside a transaction the savepoint is the
// transaction, so it is ended whatever happens to ctx: one left open would
// hold what the connection's next user writes, uncommitted. When it cannot
// be ended, the connection is discarded, which rolls it back; it is never
// released after a failed rollback, which would commit half the change.
func (c *sqliteConn) inSavepoint(ctx context.Context, fn func() error) error {
	// A SAVEPOINT cut short by a cancelled context may be open or not; it
	// runs to its end, after the context is checked.
	if err := ctx.Err(); err != nil {
		return err
	}
	end := context.WithoutCancel(ctx)
	if err := c.exec(end, "SAVEPOINT f4b_ddl"); err != nil {
		return err
	}
	err := fn()
	if err == nil {
		beforeSavepointRelease()
		if err = c.exec(end, "RELEASE f4b_ddl"); err == nil {
			return nil
		}
	}
	if rbErr := c.exec(end, "ROLLBACK TO f4b_ddl"); rbErr != nil {
		c.discard()
		return fmt.Errorf("%w (rolling the change back failed: %v)", err, rbErr)
	}
	if relErr := c.exec(end, "RELEASE f4b_ddl"); relErr != nil {
		c.discard()
		return fmt.Errorf("%w (ending the rolled back change failed: %v)", err, relErr)
	}
	return err
}

// beforeSavepointRelease lets a test act between a column change and the
// release of its savepoint.
var beforeSavepointRelease = func() {}

// discard marks the connection as one the pool must close rather than hand
// out again, and the transaction it is in, if any, as one that cannot
// commit.
func (c *sqliteConn) discard() {
	c.bad = true
	if c.inTx {
		c.ended = true
	}
}

func (c *sqliteConn) exec(ctx context.Context, query string) error {
	_, err := c.inner.(driver.ExecerContext).ExecContext(ctx, query, nil)
	return err
}

func (c *sqliteConn) queryInt(ctx context.Context, query string, args ...driver.NamedValue) (int64, error) {
	rows, err := c.inner.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	dest := make([]driver.Value, 1)
	if err := rows.Next(dest); err != nil {
		return 0, err
	}
	n, _ := dest[0].(int64)
	return n, nil
}

func (c *sqliteConn) columnExists(ctx context.Context, table, column string) (bool, error) {
	n, err := c.queryInt(ctx, `SELECT COUNT(*) FROM temp.pragma_table_info($1) WHERE name = $2`,
		driver.NamedValue{Ordinal: 1, Value: unquoteSQLite(table)}, driver.NamedValue{Ordinal: 2, Value: unquoteSQLite(column)})
	return n > 0, err
}

func unquoteSQLite(ident string) string {
	if len(ident) >= 2 && ident[0] == '"' && ident[len(ident)-1] == '"' {
		return strings.ReplaceAll(ident[1:len(ident)-1], `""`, `"`)
	}
	return ident
}

type sqliteTx struct {
	c     *sqliteConn
	inner driver.Tx
}

func (t *sqliteTx) Commit() error {
	defer t.done()
	if t.c.ended {
		return ErrSQLiteTransactionEnded
	}
	return t.inner.Commit()
}

func (t *sqliteTx) Rollback() error {
	defer t.done()
	if t.c.ended && !t.c.bad {
		return nil // SQLite already rolled it back
	}
	return t.inner.Rollback()
}

func (t *sqliteTx) done() {
	t.c.inTx, t.c.ended = false, false
	t.c.releaseWriter()
}

type sqliteStmt struct {
	c     *sqliteConn
	inner driver.Stmt
	query string
}

var (
	_ driver.StmtExecContext  = (*sqliteStmt)(nil)
	_ driver.StmtQueryContext = (*sqliteStmt)(nil)
)

func (s *sqliteStmt) Close() error  { return s.inner.Close() }
func (s *sqliteStmt) NumInput() int { return s.inner.NumInput() }

func (s *sqliteStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.ExecContext(context.Background(), namedValues(args))
}

func (s *sqliteStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.QueryContext(context.Background(), namedValues(args))
}

func (s *sqliteStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if err := s.c.refuseIfEnded(); err != nil {
		return nil, err
	}
	release, err := s.c.statementWriter(ctx, s.query, true)
	if err != nil {
		return nil, err
	}
	defer release()
	res, err := s.inner.(driver.StmtExecContext).ExecContext(ctx, args)
	if err != nil {
		return nil, s.c.afterError(err)
	}
	return res, nil
}

func (s *sqliteStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if err := s.c.refuseIfEnded(); err != nil {
		return nil, err
	}
	release, err := s.c.statementWriter(ctx, s.query, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.inner.(driver.StmtQueryContext).QueryContext(ctx, args)
	if err != nil {
		release()
		return nil, s.c.afterError(err)
	}
	r := newSQLiteRows(s.c, rows)
	r.release = release
	return r, nil
}

func namedValues(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

// sqliteRows checks the transaction when reading a row fails, so a read that
// stopped halfway is never taken for a complete one, and normalizes values.
type sqliteRows struct {
	c         *sqliteConn
	inner     driver.Rows
	decltypes []string
	release   func() // gives back the writer lock a writing query took
}

func newSQLiteRows(c *sqliteConn, inner driver.Rows) *sqliteRows {
	r := &sqliteRows{c: c, inner: inner}
	if typed, ok := inner.(driver.RowsColumnTypeDatabaseTypeName); ok {
		r.decltypes = make([]string, len(inner.Columns()))
		for i := range r.decltypes {
			r.decltypes[i] = typed.ColumnTypeDatabaseTypeName(i)
		}
	}
	return r
}

func (r *sqliteRows) Columns() []string { return r.inner.Columns() }
func (r *sqliteRows) Close() error {
	err := r.inner.Close()
	if r.release != nil {
		r.release()
		r.release = nil
	}
	return err
}

func (r *sqliteRows) ColumnTypeDatabaseTypeName(index int) string {
	if index < len(r.decltypes) {
		return r.decltypes[index]
	}
	return ""
}

func (r *sqliteRows) Next(dest []driver.Value) error {
	if err := r.c.refuseIfEnded(); err != nil {
		return err
	}
	if err := r.inner.Next(dest); err != nil {
		if errors.Is(err, io.EOF) {
			return err
		}
		return r.c.afterError(err)
	}
	for i := range dest {
		decltype := ""
		if i < len(r.decltypes) {
			decltype = r.decltypes[i]
		}
		dest[i] = normalizeSQLiteValue(decltype, dest[i])
	}
	return nil
}

// normalizeSQLiteValue hands a value back as the PostgreSQL driver hands the
// same column: a NUMERIC column's number, and a number computed by an
// expression (SUM, AVG...), as its decimal text, and a BOOLEAN as a bool.
// SQLite stores both as numbers; database/sql would format a float such as
// 1000000 as "1e+06" when it is scanned into a string. An empty BLOB stays
// empty.
func normalizeSQLiteValue(decltype string, v driver.Value) driver.Value {
	if b, ok := v.([]byte); ok && b == nil {
		// The driver gives an empty BLOB as a nil slice; it is empty, not
		// NULL, as on the other engines.
		return []byte{}
	}
	switch decltype {
	case "NUMERIC":
		switch n := v.(type) {
		case int64:
			return []byte(strconv.FormatInt(n, 10))
		case float64:
			return []byte(strconv.FormatFloat(n, 'f', -1, 64))
		}
	case "":
		if n, ok := v.(float64); ok {
			return []byte(strconv.FormatFloat(n, 'f', -1, 64))
		}
	case "BOOLEAN":
		if n, ok := v.(int64); ok {
			return n != 0
		}
	}
	return v
}
