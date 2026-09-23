package dbal

import (
	"context"
	"database/sql"
	"fmt"
)

// EngineType represents the database engine
type EngineType string

const (
	EnginePostgres EngineType = "postgres"
	EngineMariaDB  EngineType = "mariadb"
	EngineSQLite   EngineType = "sqlite"
)

// AgnosticFieldType abstracts field types across database engines
type AgnosticFieldType string

const (
	FieldTypeText        AgnosticFieldType = "TEXT"
	FieldTypeNumber      AgnosticFieldType = "NUMBER"
	FieldTypeDate        AgnosticFieldType = "DATE"
	FieldTypeTimestamp   AgnosticFieldType = "TIMESTAMP"
	FieldTypeBoolean     AgnosticFieldType = "BOOLEAN"
	FieldTypeContainer   AgnosticFieldType = "CONTAINER"
	FieldTypeCalculation AgnosticFieldType = "CALCULATION"
	FieldTypeSummary     AgnosticFieldType = "SUMMARY"
)

// ColumnDefinition defines an agnostic column schema
type ColumnDefinition struct {
	Name         string
	Type         AgnosticFieldType
	IsNullable   bool
	IsPrimaryKey bool
	DefaultValue *string
}

// TableDefinition defines an agnostic table schema
type TableDefinition struct {
	Name    string
	Columns []ColumnDefinition
}

// Dialect generates engine-specific SQL
type Dialect interface {
	Engine() EngineType
	QuoteIdentifier(name string) string
	Placeholder(index int) string
	MapType(field AgnosticFieldType) string
	BuildCreateTableSQL(def TableDefinition) (string, error)
	BuildAddColumnSQL(tableName string, col ColumnDefinition) (string, error)
	BuildDropColumnSQL(tableName string, columnName string) (string, error)
}

// DatabaseDriver wraps sql.DB or connection pool
type DatabaseDriver interface {
	Dialect() Dialect
	DB() *sql.DB
	Ping(ctx context.Context) error
	Close() error
}

type genericDriver struct {
	dialect Dialect
	db      *sql.DB
}

func (g *genericDriver) Dialect() Dialect {
	return g.dialect
}

func (g *genericDriver) DB() *sql.DB {
	return g.db
}

func (g *genericDriver) Ping(ctx context.Context) error {
	if g.db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return g.db.PingContext(ctx)
}

func (g *genericDriver) Close() error {
	if g.db == nil {
		return nil
	}
	return g.db.Close()
}

// NewGenericDriver creates a generic DatabaseDriver instance
func NewGenericDriver(dialect Dialect, db *sql.DB) DatabaseDriver {
	return &genericDriver{
		dialect: dialect,
		db:      db,
	}
}
