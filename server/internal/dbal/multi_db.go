package dbal

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

var validDatabaseName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)

// MultiDatabaseManager manages connection pools across multiple databases on a server
type MultiDatabaseManager struct {
	mu             sync.RWMutex
	baseEngine     EngineType
	baseDSN        string
	activeDBName   string
	drivers        map[string]DatabaseDriver
	parsedURL      *url.URL
}

// NewMultiDatabaseManager initializes a MultiDatabaseManager with a base DSN
func NewMultiDatabaseManager(engine EngineType, baseDSN string) (*MultiDatabaseManager, error) {
	u, err := url.Parse(baseDSN)
	if err != nil {
		return nil, fmt.Errorf("invalid base DSN: %w", err)
	}

	initialDB := strings.TrimPrefix(u.Path, "/")
	if initialDB == "" {
		initialDB = "postgres"
	}

	mgr := &MultiDatabaseManager{
		baseEngine:   engine,
		baseDSN:      baseDSN,
		activeDBName: initialDB,
		drivers:      make(map[string]DatabaseDriver),
		parsedURL:    u,
	}

	return mgr, nil
}

// BuildDSN constructs a connection string for a specific database name
func (m *MultiDatabaseManager) BuildDSN(dbName string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.parsedURL == nil {
		return m.baseDSN
	}

	uCopy := *m.parsedURL
	uCopy.Path = "/" + dbName
	return uCopy.String()
}

// ActiveDatabase returns the current active database name
func (m *MultiDatabaseManager) ActiveDatabase() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeDBName
}

// Dialect returns the dialect for the active database engine
func (m *MultiDatabaseManager) Dialect() Dialect {
	m.mu.RLock()
	active := m.activeDBName
	m.mu.RUnlock()

	driver, _ := m.GetDriver(context.Background(), active)
	if driver != nil {
		return driver.Dialect()
	}
	d, _ := NewDialect(m.baseEngine)
	return d
}

// DB returns the *sql.DB connection pool for the active database
func (m *MultiDatabaseManager) DB() *sql.DB {
	m.mu.RLock()
	active := m.activeDBName
	m.mu.RUnlock()

	driver, err := m.GetDriver(context.Background(), active)
	if err != nil {
		return nil
	}
	return driver.DB()
}

// Ping checks connectivity to the active database
func (m *MultiDatabaseManager) Ping(ctx context.Context) error {
	m.mu.RLock()
	active := m.activeDBName
	m.mu.RUnlock()

	driver, err := m.GetDriver(ctx, active)
	if err != nil {
		return err
	}
	return driver.Ping(ctx)
}

// GetDriver returns or establishes a DatabaseDriver for the specified database
func (m *MultiDatabaseManager) GetDriver(ctx context.Context, dbName string) (DatabaseDriver, error) {
	if dbName == "" {
		m.mu.RLock()
		dbName = m.activeDBName
		m.mu.RUnlock()
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

	m.drivers[dbName] = newDriver
	return newDriver, nil
}

func (m *MultiDatabaseManager) buildDSNLocked(dbName string) string {
	if m.parsedURL == nil {
		return m.baseDSN
	}
	uCopy := *m.parsedURL
	uCopy.Path = "/" + dbName
	return uCopy.String()
}

// SetActiveDatabase changes the active database, initializing a connection if needed
func (m *MultiDatabaseManager) SetActiveDatabase(ctx context.Context, dbName string) (DatabaseDriver, error) {
	dbName = strings.TrimSpace(dbName)
	if !validDatabaseName.MatchString(dbName) {
		return nil, fmt.Errorf("invalid database name: %s", dbName)
	}

	driver, err := m.GetDriver(ctx, dbName)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.activeDBName = dbName
	m.mu.Unlock()

	return driver, nil
}

// ListDatabases returns all non-template databases available on the PostgreSQL server
func (m *MultiDatabaseManager) ListDatabases(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	active := m.activeDBName
	m.mu.RUnlock()

	driver, err := m.GetDriver(ctx, active)
	if err != nil {
		// Try admin/postgres fallback
		driver, err = m.GetDriver(ctx, "postgres")
		if err != nil {
			return nil, fmt.Errorf("unable to reach database server to list databases: %w", err)
		}
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

	var result []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		result = append(result, name)
	}

	return result, nil
}

// CreateDatabase creates a new physical database on the server
func (m *MultiDatabaseManager) CreateDatabase(ctx context.Context, dbName string) error {
	dbName = strings.ToLower(strings.TrimSpace(dbName))
	if !validDatabaseName.MatchString(dbName) {
		return fmt.Errorf("invalid database name '%s'; must start with a letter and contain only alphanumeric/underscore characters", dbName)
	}

	// Use an existing connection (e.g. active or postgres) to execute CREATE DATABASE
	m.mu.RLock()
	active := m.activeDBName
	m.mu.RUnlock()

	driver, err := m.GetDriver(ctx, active)
	if err != nil {
		driver, err = m.GetDriver(ctx, "postgres")
		if err != nil {
			return fmt.Errorf("unable to connect to database server: %w", err)
		}
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
		return nil // Already exists
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
