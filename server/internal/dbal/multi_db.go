package dbal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

var validDatabaseName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// ErrDatabaseExists is returned by CreateDatabase when the database is already present.
var ErrDatabaseExists = errors.New("database already exists")

// Per-database connection pool limits. Every solution database gets its own
// pool, so the limits are deliberately small to keep the total bounded.
const (
	maxOpenConnsPerDatabase = 10
	maxIdleConnsPerDatabase = 2
	connMaxIdleTime         = 5 * time.Minute
)

// protectedDatabases are engine-internal databases that must never be used as
// a File4Base solution database nor dropped through the API.
var protectedDatabases = map[string]struct{}{
	"postgres":           {},
	"template0":          {},
	"template1":          {},
	"mysql":              {},
	"information_schema": {},
	"performance_schema": {},
	"sys":                {},
}

// IsProtectedDatabase reports whether name is an engine-internal database.
func IsProtectedDatabase(name string) bool {
	_, ok := protectedDatabases[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

// IsValidDatabaseName reports whether name is a syntactically safe database identifier.
func IsValidDatabaseName(name string) bool {
	return validDatabaseName.MatchString(name)
}

// MultiDatabaseManager manages connection pools across multiple databases on a server.
//
// The manager is stateless with respect to callers: it has a fixed default
// (administrative) database taken from the base DSN, used for server-level
// operations such as listing, creating and dropping databases. Which solution
// database a request works on is decided per request by the API layer (see
// WithDriver), never by mutating the manager.
type MultiDatabaseManager struct {
	mu            sync.RWMutex
	baseEngine    EngineType
	baseDSN       string
	defaultDBName string
	drivers       map[string]DatabaseDriver
	dsn           dsnTemplate
}

// NewMultiDatabaseManager initializes a MultiDatabaseManager with a base DSN
func NewMultiDatabaseManager(engine EngineType, baseDSN string) (*MultiDatabaseManager, error) {
	parsed, err := parseDSN(engine, baseDSN)
	if err != nil {
		return nil, err
	}

	// The administrative database carries server-level operations (listing,
	// creating and dropping databases). It is an engine-internal database, so
	// it can never be used as a solution database nor dropped.
	initialDB := parsed.database()
	if initialDB == "" {
		initialDB = defaultAdminDatabase(engine)
	}

	return &MultiDatabaseManager{
		baseEngine:    engine,
		baseDSN:       baseDSN,
		defaultDBName: initialDB,
		drivers:       make(map[string]DatabaseDriver),
		dsn:           parsed,
	}, nil
}

// defaultAdminDatabase is the administrative database of an engine, used when
// the base DSN names none.
func defaultAdminDatabase(engine EngineType) string {
	if engine == EngineMariaDB {
		return "mysql"
	}
	return "postgres"
}

// BuildDSN constructs a connection string for a specific database name
func (m *MultiDatabaseManager) BuildDSN(dbName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.buildDSNLocked(dbName)
}

// DefaultDatabase returns the administrative database named in the base DSN.
// It never changes for the lifetime of the manager.
func (m *MultiDatabaseManager) DefaultDatabase() string {
	return m.defaultDBName
}

// Engine returns the database engine this manager talks to.
func (m *MultiDatabaseManager) Engine() EngineType {
	return m.baseEngine
}

// Dialect returns the dialect of the default (administrative) database engine
func (m *MultiDatabaseManager) Dialect() Dialect {
	active := m.defaultDBName

	driver, _ := m.GetDriver(context.Background(), active)
	if driver != nil {
		return driver.Dialect()
	}
	d, _ := NewDialect(m.baseEngine)
	return d
}

// DB returns the *sql.DB connection pool of the default (administrative) database
func (m *MultiDatabaseManager) DB() *sql.DB {
	active := m.defaultDBName

	driver, err := m.GetDriver(context.Background(), active)
	if err != nil {
		return nil
	}
	return driver.DB()
}

// Ping checks connectivity to the default (administrative) database
func (m *MultiDatabaseManager) Ping(ctx context.Context) error {
	active := m.defaultDBName

	driver, err := m.GetDriver(ctx, active)
	if err != nil {
		return err
	}
	return driver.Ping(ctx)
}

// GetDriver returns or establishes a DatabaseDriver for the specified database
func (m *MultiDatabaseManager) GetDriver(ctx context.Context, dbName string) (DatabaseDriver, error) {
	if dbName == "" {
		dbName = m.defaultDBName
	}

	m.mu.RLock()
	driver, exists := m.drivers[dbName]
	m.mu.RUnlock()

	if exists {
		if err := driver.Ping(ctx); err == nil {
			return driver, nil
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double check after acquiring write lock
	if driver, exists := m.drivers[dbName]; exists {
		if err := driver.Ping(ctx); err == nil {
			return driver, nil
		}
		_ = driver.Close()
		delete(m.drivers, dbName)
	}

	dsn := m.buildDSNLocked(dbName)
	newDriver, err := Connect(DriverConfig{
		EngineType: m.baseEngine,
		DSN:        dsn,
	})
	if err != nil {
		return nil, fmt.Errorf("failed connecting to database %s: %w", dbName, err)
	}

	if err := newDriver.Ping(ctx); err != nil {
		_ = newDriver.Close()
		return nil, fmt.Errorf("ping failed for database %s: %w", dbName, err)
	}

	if pool := newDriver.DB(); pool != nil {
		pool.SetMaxOpenConns(maxOpenConnsPerDatabase)
		pool.SetMaxIdleConns(maxIdleConnsPerDatabase)
		pool.SetConnMaxIdleTime(connMaxIdleTime)
	}

	m.drivers[dbName] = newDriver
	return newDriver, nil
}

func (m *MultiDatabaseManager) buildDSNLocked(dbName string) string {
	if m.dsn == nil {
		return m.baseDSN
	}
	return m.dsn.forDatabase(dbName)
}

// DriverFor returns the DatabaseDriver of a solution database after validating
// its name. It does not change any shared state: callers keep the returned
// driver for the duration of their request.
func (m *MultiDatabaseManager) DriverFor(ctx context.Context, dbName string) (DatabaseDriver, error) {
	dbName = strings.TrimSpace(dbName)
	if !validDatabaseName.MatchString(dbName) {
		return nil, fmt.Errorf("invalid database name: %s", dbName)
	}
	if IsProtectedDatabase(dbName) {
		return nil, fmt.Errorf("database '%s' is a protected system database", dbName)
	}
	return m.GetDriver(ctx, dbName)
}

// LookupDatabase finds a solution database by name (case-insensitively) and
// returns its canonical name as stored by the engine.
func (m *MultiDatabaseManager) LookupDatabase(ctx context.Context, dbName string) (string, bool, error) {
	dbName = strings.TrimSpace(dbName)
	if !validDatabaseName.MatchString(dbName) || IsProtectedDatabase(dbName) {
		return "", false, nil
	}
	names, err := m.ListDatabases(ctx)
	if err != nil {
		return "", false, err
	}
	for _, name := range names {
		if name == dbName {
			return name, true, nil
		}
	}
	for _, name := range names {
		if strings.EqualFold(name, dbName) {
			return name, true, nil
		}
	}
	return "", false, nil
}

// ListDatabases returns all non-template databases available on the PostgreSQL server
func (m *MultiDatabaseManager) ListDatabases(ctx context.Context) ([]string, error) {
	driver, err := m.GetDriver(ctx, m.defaultDBName)
	if err != nil {
		return nil, fmt.Errorf("unable to reach database server to list databases: %w", err)
	}

	db := driver.DB()
	var rows *sql.Rows
	if m.baseEngine == EnginePostgres {
		query := `SELECT datname FROM pg_database WHERE datistemplate = false AND datname NOT IN ('postgres', 'template0', 'template1') ORDER BY datname ASC;`
		rows, err = db.QueryContext(ctx, query)
	} else {
		query := `SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys') ORDER BY SCHEMA_NAME ASC;`
		rows, err = db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, fmt.Errorf("failed querying databases: %w", err)
	}
	defer rows.Close()

	result := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if IsProtectedDatabase(name) || !validDatabaseName.MatchString(name) {
			continue
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateDatabase creates a new physical database on the server.
// It returns ErrDatabaseExists when a database with that name is already present.
func (m *MultiDatabaseManager) CreateDatabase(ctx context.Context, dbName string) error {
	dbName = strings.ToLower(strings.TrimSpace(dbName))
	if !validDatabaseName.MatchString(dbName) {
		return fmt.Errorf("invalid database name '%s'; must start with a letter and contain only alphanumeric/underscore characters", dbName)
	}

	if IsProtectedDatabase(dbName) {
		return fmt.Errorf("'%s' is a protected system database name", dbName)
	}

	// CREATE DATABASE runs through the administrative connection
	driver, err := m.GetDriver(ctx, m.defaultDBName)
	if err != nil {
		return fmt.Errorf("unable to connect to database server: %w", err)
	}

	db := driver.DB()

	// Check if already exists
	var exists bool
	if m.baseEngine == EnginePostgres {
		checkQuery := `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1);`
		if err := db.QueryRowContext(ctx, checkQuery, dbName).Scan(&exists); err != nil {
			return fmt.Errorf("failed checking database existence: %w", err)
		}
	} else {
		checkQuery := `SELECT EXISTS(SELECT 1 FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?);`
		if err := db.QueryRowContext(ctx, checkQuery, dbName).Scan(&exists); err != nil {
			return fmt.Errorf("failed checking database existence: %w", err)
		}
	}

	if exists {
		return ErrDatabaseExists
	}

	// CREATE DATABASE cannot run in a transaction block
	createSQL := fmt.Sprintf(`CREATE DATABASE "%s"`, dbName)
	if m.baseEngine == EngineMariaDB {
		createSQL = fmt.Sprintf("CREATE DATABASE `%s`", dbName)
	}

	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed creating database %s: %w", dbName, err)
	}

	return nil
}

// DropDatabase terminates existing connections and drops a database on the server
func (m *MultiDatabaseManager) DropDatabase(ctx context.Context, dbName string) error {
	dbName = strings.ToLower(strings.TrimSpace(dbName))
	if !validDatabaseName.MatchString(dbName) {
		return fmt.Errorf("invalid database name '%s'", dbName)
	}

	if IsProtectedDatabase(dbName) {
		return fmt.Errorf("cannot drop protected system database '%s'", dbName)
	}

	// The administrative database carries the connection used to run DROP DATABASE
	if strings.EqualFold(dbName, m.defaultDBName) {
		return fmt.Errorf("cannot drop '%s': it is the server's administrative database", dbName)
	}

	// Close driver connection to target db if open in pool
	m.mu.Lock()
	if d, ok := m.drivers[dbName]; ok {
		_ = d.Close()
		delete(m.drivers, dbName)
	}
	m.mu.Unlock()

	// Connect through the administrative database to execute DROP DATABASE
	adminDriver, err := m.GetDriver(ctx, m.defaultDBName)
	if err != nil {
		return fmt.Errorf("unable to connect to admin database to drop: %w", err)
	}
	adminDB := adminDriver.DB()

	if m.baseEngine == EnginePostgres {
		// Terminate any active sessions connected to this database
		terminateSQL := `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid();`
		_, _ = adminDB.ExecContext(ctx, terminateSQL, dbName)

		dropSQL := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s";`, dbName)
		if _, err := adminDB.ExecContext(ctx, dropSQL); err != nil {
			return fmt.Errorf("failed dropping database %s: %w", dbName, err)
		}
	} else {
		dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", dbName)
		if _, err := adminDB.ExecContext(ctx, dropSQL); err != nil {
			return fmt.Errorf("failed dropping database %s: %w", dbName, err)
		}
	}

	return nil
}

// Close closes all open database connection pools
func (m *MultiDatabaseManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, d := range m.drivers {
		if err := d.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(m.drivers, name)
	}
	return firstErr
}
