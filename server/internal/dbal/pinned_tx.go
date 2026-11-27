package dbal

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// TxDriver is a DatabaseDriver whose every statement runs on one connection,
// inside one transaction (#203). Code written against DatabaseDriver, which
// runs statements on DB() and opens transactions of its own, then reads one
// consistent snapshot, or writes changes that commit or roll back together:
//
//   - a transaction opened on DB() becomes a savepoint of the outer one, so
//     its Commit releases it and its Rollback undoes only its own work;
//   - on PostgreSQL, a statement run outside such a transaction gets a
//     savepoint of its own, so an error the caller tolerates does not abort
//     the outer transaction, as it would not have in autocommit mode;
//   - a second connection is never opened: a statement that would need one
//     fails instead of running outside the transaction.
//
// MariaDB commits implicitly before any DDL statement, so a TxDriver there
// is only for reading (OpenSnapshot).
type TxDriver struct {
	dialect  Dialect
	db       *sql.DB
	conn     *pinnedConn
	outer    driver.Tx
	readOnly bool
	done     bool
}

// OpenSnapshot opens a read-only, repeatable-read transaction on dbName: all
// that is read through the driver is one snapshot of the database. Close it
// with Rollback.
func (m *MultiDatabaseManager) OpenSnapshot(ctx context.Context, dbName string) (*TxDriver, error) {
	return m.openTx(ctx, dbName, driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelRepeatableRead), ReadOnly: true})
}

// OpenTransaction opens a read-write transaction on dbName, for changes that
// must commit or roll back as a whole, DDL included. PostgreSQL only: MariaDB
// commits implicitly before DDL.
func (m *MultiDatabaseManager) OpenTransaction(ctx context.Context, dbName string) (*TxDriver, error) {
	if m.baseEngine != EnginePostgres {
		return nil, fmt.Errorf("%s has no transactional DDL", m.baseEngine)
	}
	return m.openTx(ctx, dbName, driver.TxOptions{})
}

func (m *MultiDatabaseManager) openTx(ctx context.Context, dbName string, opts driver.TxOptions) (*TxDriver, error) {
	dialect, err := NewDialect(m.baseEngine)
	if err != nil {
		return nil, err
	}
	var driverName string
	switch m.baseEngine {
	case EnginePostgres:
		driverName = "pgx"
	case EngineMariaDB:
		driverName = "mysql"
	default:
		return nil, fmt.Errorf("no transaction support for engine: %s", m.baseEngine)
	}
	dsn, err := NormalizeDSN(m.baseEngine, m.BuildDSN(strings.ToLower(strings.TrimSpace(dbName))))
	if err != nil {
		return nil, err
	}
	probe, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	dc, ok := probe.Driver().(driver.DriverContext)
	_ = probe.Close()
	if !ok {
		return nil, fmt.Errorf("the %s driver cannot open a dedicated connection", driverName)
	}
	connector, err := dc.OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	raw, err := connector.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed connecting to database %s: %w", dbName, err)
	}
	beginner, ok := raw.(driver.ConnBeginTx)
	if !ok {
		_ = raw.Close()
		return nil, errors.New("the driver cannot begin a transaction with options")
	}
	outer, err := beginner.BeginTx(ctx, opts)
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("failed beginning a transaction on %s: %w", dbName, err)
	}
	conn := &pinnedConn{Conn: raw, isolate: m.baseEngine == EnginePostgres}
	db := sql.OpenDB(&pinnedConnector{conn: conn, drv: connector.Driver()})
	db.SetMaxIdleConns(1)
	return &TxDriver{dialect: dialect, db: db, conn: conn, outer: outer, readOnly: opts.ReadOnly}, nil
}

func (t *TxDriver) Dialect() Dialect               { return t.dialect }
func (t *TxDriver) DB() *sql.DB                    { return t.db }
func (t *TxDriver) Ping(ctx context.Context) error { return t.db.PingContext(ctx) }

// Close rolls the transaction back if it is still open.
func (t *TxDriver) Close() error { return t.Rollback() }

// Check reports an error if the statements run so far did not all run in the
// transaction: a MariaDB DDL statement would have committed it implicitly.
func (t *TxDriver) Check(ctx context.Context) error {
	if t.done {
		return errors.New("the transaction is over")
	}
	if t.dialect.Engine() != EngineMariaDB {
		return nil
	}
	var inTx int
	if err := t.db.QueryRowContext(ctx, `SELECT @@in_transaction`).Scan(&inTx); err != nil {
		return err
	}
	if inTx != 1 {
		return errors.New("the transaction was committed implicitly")
	}
	return nil
}

// Commit commits the transaction and closes the connection.
func (t *TxDriver) Commit(ctx context.Context) error {
	if t.done {
		return errors.New("the transaction is over")
	}
	if err := t.Check(ctx); err != nil {
		_ = t.Rollback()
		return err
	}
	t.conn.mu.Lock()
	if err := t.conn.settle(ctx); err != nil {
		t.conn.mu.Unlock()
		_ = t.Rollback()
		return err
	}
	t.done = true
	err := t.outer.Commit()
	t.conn.mu.Unlock()
	t.close()
	return err
}

// Rollback rolls the transaction back and closes the connection. It does
// nothing once the transaction is over.
func (t *TxDriver) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true
	t.conn.mu.Lock()
	err := t.outer.Rollback()
	t.conn.mu.Unlock()
	t.close()
	return err
}

func (t *TxDriver) close() {
	_ = t.db.Close()
	_ = t.conn.Conn.Close()
}

// pinnedConnector hands out the one connection, once.
type pinnedConnector struct {
	conn *pinnedConn
	drv  driver.Driver
	mu   sync.Mutex
	used bool
}

var errSecondConnection = errors.New("this transaction runs on one connection: a statement needed another one")

func (c *pinnedConnector) Connect(context.Context) (driver.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		return nil, errSecondConnection
	}
	c.used = true
	return c.conn, nil
}

func (c *pinnedConnector) Driver() driver.Driver { return c.drv }

// pinnedConn forwards to the driver's connection, turning transactions into
// savepoints. database/sql uses it from one goroutine at a time, but a
// context cancellation may roll a transaction back from another.
type pinnedConn struct {
	driver.Conn
	mu sync.Mutex
	// isolate runs each statement outside a nested transaction in a savepoint
	// of its own (PostgreSQL, where an error aborts the whole transaction).
	isolate bool
	depth   int
	seq     int
	// pending is the savepoint of the last isolated query, released (or
	// rolled back to, if its rows failed) before the next statement.
	pending string
}

var (
	_ driver.ExecerContext      = (*pinnedConn)(nil)
	_ driver.QueryerContext     = (*pinnedConn)(nil)
	_ driver.ConnPrepareContext = (*pinnedConn)(nil)
	_ driver.ConnBeginTx        = (*pinnedConn)(nil)
	_ driver.NamedValueChecker  = (*pinnedConn)(nil)
	_ driver.SessionResetter    = (*pinnedConn)(nil)
	_ driver.Validator          = (*pinnedConn)(nil)
	_ driver.Pinger             = (*pinnedConn)(nil)
)

// Close keeps the connection: TxDriver closes it with the transaction.
func (c *pinnedConn) Close() error { return nil }

func (c *pinnedConn) raw(ctx context.Context, query string) error {
	ex, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return errors.New("the driver cannot run a statement directly")
	}
	_, err := ex.ExecContext(ctx, query, nil)
	return err
}

// settle releases the savepoint of the last isolated query. If its rows
// failed after the query started, the transaction is aborted: rolling back
// to the savepoint recovers it.
func (c *pinnedConn) settle(ctx context.Context) error {
	if c.pending == "" {
		return nil
	}
	sp := c.pending
	c.pending = ""
	if err := c.raw(ctx, "RELEASE SAVEPOINT "+sp); err == nil {
		return nil
	}
	if err := c.raw(ctx, "ROLLBACK TO SAVEPOINT "+sp); err != nil {
		return err
	}
	return c.raw(ctx, "RELEASE SAVEPOINT "+sp)
}

func (c *pinnedConn) savepoint(ctx context.Context) (string, error) {
	c.seq++
	sp := fmt.Sprintf("f4b_sp_%d", c.seq)
	return sp, c.raw(ctx, "SAVEPOINT "+sp)
}

func (c *pinnedConn) isolated() bool { return c.isolate && c.depth == 0 }

func (c *pinnedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ex, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.settle(ctx); err != nil {
		return nil, err
	}
	if !c.isolated() {
		return ex.ExecContext(ctx, query, args)
	}
	sp, err := c.savepoint(ctx)
	if err != nil {
		return nil, err
	}
	res, err := ex.ExecContext(ctx, query, args)
	if err != nil {
		if rbErr := c.raw(ctx, "ROLLBACK TO SAVEPOINT "+sp); rbErr != nil {
			return nil, errors.Join(err, rbErr)
		}
	}
	if relErr := c.raw(ctx, "RELEASE SAVEPOINT "+sp); relErr != nil && err == nil {
		return nil, relErr
	}
	return res, err
}

func (c *pinnedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	q, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.settle(ctx); err != nil {
		return nil, err
	}
	if !c.isolated() {
		return q.QueryContext(ctx, query, args)
	}
	sp, err := c.savepoint(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, query, args)
	if err != nil {
		if rbErr := c.raw(ctx, "ROLLBACK TO SAVEPOINT "+sp); rbErr != nil {
			return nil, errors.Join(err, rbErr)
		}
		_ = c.raw(ctx, "RELEASE SAVEPOINT "+sp)
		return nil, err
	}
	// The rows are still being read: the savepoint is settled before the
	// next statement.
	c.pending = sp
	return rows, nil
}

func (c *pinnedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.settle(ctx); err != nil {
		return nil, err
	}
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, query)
	}
	return c.Conn.Prepare(query)
}

func (c *pinnedConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *pinnedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx opens a savepoint. The options are those of the outer transaction.
func (c *pinnedConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.settle(ctx); err != nil {
		return nil, err
	}
	sp, err := c.savepoint(ctx)
	if err != nil {
		return nil, err
	}
	c.depth++
	return &savepointTx{conn: c, name: sp}, nil
}

func (c *pinnedConn) CheckNamedValue(nv *driver.NamedValue) error {
	if ch, ok := c.Conn.(driver.NamedValueChecker); ok {
		return ch.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// ResetSession is not forwarded: a driver discards a connection left in a
// transaction (pgx), and this one is meant to be.
func (c *pinnedConn) ResetSession(context.Context) error {
	if !c.IsValid() {
		return driver.ErrBadConn
	}
	return nil
}

func (c *pinnedConn) IsValid() bool {
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *pinnedConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

// savepointTx is a transaction opened inside the outer one.
type savepointTx struct {
	conn *pinnedConn
	name string
	done bool
}

func (t *savepointTx) finish(statements ...string) error {
	c := t.conn
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.done {
		return nil
	}
	t.done = true
	c.depth--
	ctx := context.Background()
	if err := c.settle(ctx); err != nil {
		return err
	}
	for _, s := range statements {
		if err := c.raw(ctx, s+" "+t.name); err != nil {
			return err
		}
	}
	return nil
}

func (t *savepointTx) Commit() error { return t.finish("RELEASE SAVEPOINT") }

func (t *savepointTx) Rollback() error {
	return t.finish("ROLLBACK TO SAVEPOINT", "RELEASE SAVEPOINT")
}
