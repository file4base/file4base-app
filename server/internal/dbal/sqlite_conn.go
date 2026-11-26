package dbal

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
func openSQLite(ctx context.Context, path string) (*sql.DB, error) {
	db := sql.OpenDB(&sqliteConnector{dsn: sqliteDSN(path)})
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

type sqliteConnector struct{ dsn string }

// SQLiteConnector is the connector of an existing database file, with the
// connection contract, for a caller that wraps the connections (tests that
// count or fail statements). openSQLite's check of the journal mode is not
// made.
func SQLiteConnector(path string) driver.Connector {
	return &sqliteConnector{dsn: sqliteDSN(path)}
}

func (c *sqliteConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := sqliteDriver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &sqliteConn{inner: conn}, nil
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
	inner driver.Conn
	inTx  bool
	ended bool
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
	return &sqliteStmt{c: c, inner: stmt}, nil
}

func (c *sqliteConn) Close() error { return c.inner.Close() }

func (c *sqliteConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *sqliteConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.inner.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	c.inTx, c.ended = true, false
	return &sqliteTx{c: c, inner: tx}, nil
}

func (c *sqliteConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.refuseIfEnded(); err != nil {
		return nil, err
	}
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
	rows, err := c.inner.(driver.QueryerContext).QueryContext(ctx, query, args)
	if err != nil {
		return nil, c.afterError(err)
	}
	return newSQLiteRows(c, rows), nil
}

func (c *sqliteConn) Ping(ctx context.Context) error {
	return c.inner.(driver.Pinger).Ping(ctx)
}

func (c *sqliteConn) ResetSession(ctx context.Context) error {
	return c.inner.(driver.SessionResetter).ResetSession(ctx)
}

func (c *sqliteConn) IsValid() bool {
	return c.inner.(driver.Validator).IsValid()
}

func (c *sqliteConn) refuseIfEnded() error {
	if c.inTx && c.ended {
		return ErrSQLiteTransactionEnded
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
func (c *sqliteConn) alterSQLiteColumnType(ctx context.Context, table, column, newType string) error {
	temp := `"` + strings.ReplaceAll(unquoteSQLite(column)+"$retype", `"`, `""`) + `"`
	add := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, temp, newType)
	if check := SQLiteTypeCheck(temp, newType); check != "" {
		add += " " + check
	}
	if err := c.exec(ctx, add); err != nil {
		return err
	}
	if err := c.exec(ctx, fmt.Sprintf("UPDATE %s SET %s = NULLIF(%s, '')", table, temp, column)); err != nil {
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
		`SELECT DISTINCT m.name, m.sql FROM sqlite_schema AS m, pragma_index_xinfo(m.name) AS x `+
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
// back when it fails.
func (c *sqliteConn) inSavepoint(ctx context.Context, fn func() error) error {
	if err := c.exec(ctx, "SAVEPOINT f4b_ddl"); err != nil {
		return err
	}
	if err := fn(); err != nil {
		_ = c.exec(context.Background(), "ROLLBACK TO f4b_ddl")
		_ = c.exec(context.Background(), "RELEASE f4b_ddl")
		return err
	}
	return c.exec(ctx, "RELEASE f4b_ddl")
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
	n, err := c.queryInt(ctx, `SELECT COUNT(*) FROM pragma_table_info($1) WHERE name = $2`,
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
	if t.c.ended {
		return nil // SQLite already rolled it back
	}
	return t.inner.Rollback()
}

func (t *sqliteTx) done() { t.c.inTx, t.c.ended = false, false }

type sqliteStmt struct {
	c     *sqliteConn
	inner driver.Stmt
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
	rows, err := s.inner.(driver.StmtQueryContext).QueryContext(ctx, args)
	if err != nil {
		return nil, s.c.afterError(err)
	}
	return newSQLiteRows(s.c, rows), nil
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
func (r *sqliteRows) Close() error      { return r.inner.Close() }

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
