package dbal

import (
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DriverConfig holds parameters for database connection
type DriverConfig struct {
	EngineType EngineType
	DSN        string
}

// DialectRegistry provides access to dialect implementations
var dialectRegistry = make(map[EngineType]func() Dialect)

// RegisterDialect registers a dialect constructor
func RegisterDialect(engine EngineType, fn func() Dialect) {
	dialectRegistry[engine] = fn
}

// NewDialect returns a dialect instance for the specified engine
func NewDialect(engine EngineType) (Dialect, error) {
	fn, ok := dialectRegistry[engine]
	if !ok {
		return nil, fmt.Errorf("unsupported database engine dialect: %s", engine)
	}
	return fn(), nil
}

// Connect creates a DatabaseDriver connected to the specified engine
func Connect(cfg DriverConfig) (DatabaseDriver, error) {
	dialect, err := NewDialect(cfg.EngineType)
	if err != nil {
		return nil, err
	}

	var driverName string
	switch cfg.EngineType {
	case EnginePostgres:
		driverName = "pgx"
	case EngineMariaDB:
		driverName = "mysql"
	default:
		return nil, fmt.Errorf("no standard SQL driver available for engine: %s", cfg.EngineType)
	}

	db, err := sql.Open(driverName, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	return NewGenericDriver(dialect, db), nil
}

// ParseEngineType converts a string to an EngineType
func ParseEngineType(val string) (EngineType, error) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "postgres", "postgresql", "pg":
		return EnginePostgres, nil
	case "mariadb", "mysql":
		return EngineMariaDB, nil
	case "sqlite", "sqlite3":
		return EngineSQLite, nil
	default:
		return "", fmt.Errorf("unknown database engine: %s", val)
	}
}
