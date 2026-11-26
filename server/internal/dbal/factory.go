package dbal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
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

	if cfg.EngineType == EngineSQLite {
		path, err := SQLitePath(cfg.DSN)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), SQLiteBusyTimeout()+5*time.Second)
		defer cancel()
		db, err := openSQLite(ctx, path)
		if err != nil {
			return nil, err
		}
		return NewGenericDriver(dialect, db), nil
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

	dsn, err := NormalizeDSN(cfg.EngineType, cfg.DSN)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driverName, dsn)
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

// IsUndefinedTable reports whether err is the engine saying a table does not
// exist (PostgreSQL 42P01, MariaDB 1146, SQLite "no such table").
func IsUndefinedTable(err error) bool {
	if err == nil {
		return false
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1146
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "42P01"
	}
	var lite *sqlite.Error
	if errors.As(err, &lite) {
		return lite.Code() == sqlite3.SQLITE_ERROR && strings.Contains(lite.Error(), "no such table")
	}
	return false
}

// IsDuplicateKey reports whether err is the engine's unique/primary key
// violation.
func IsDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1062
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "23505"
	}
	var lite *sqlite.Error
	if errors.As(err, &lite) {
		return lite.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || lite.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}
