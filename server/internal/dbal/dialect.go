package dbal

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	// There is deliberately no default value: physical DEFAULT clauses are
	// never built from caller input (auto-enter options live in sys_columns
	// and are applied by the data service).
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

	// TimestampColumn is the column type (with its default) used by the
	// system catalog for creation and modification times.
	TimestampColumn() string
	// CastToText wraps an expression so any column type compares as text.
	CastToText(expr string) string
	// CaseInsensitiveLike matches expr against a placeholder ignoring case.
	CaseInsensitiveLike(expr, placeholder string) string
	// NumericValue reads expr as a number, or NULL when it does not hold one,
	// so a comparison never fails on rows holding other text.
	NumericValue(expr string) string
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

// NumericPattern matches the text of a number, used by NumericValue to tell
// numeric rows from any other text. It is a plain POSIX pattern so both
// engines read it the same way.
const NumericPattern = `^-?[0-9]+(\.[0-9]+)?$`

// TrimNumericText drops the trailing zeros a fixed-scale numeric type pads a
// value with (MariaDB stores NUMBER as DECIMAL(65,10), PostgreSQL keeps the
// scale it was given), so the same stored number reads back the same text on
// every engine.
func TrimNumericText(text string) string {
	if !strings.Contains(text, ".") {
		return text
	}
	return strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
}

// NormalizeBool reads a stored boolean as a bool. MariaDB stores BOOLEAN as
// TINYINT(1) and returns a number, where PostgreSQL returns a bool, so both
// engines answer the same.
func NormalizeBool(v interface{}) interface{} {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case []byte:
		return string(t) != "0" && !strings.EqualFold(string(t), "false") && len(t) > 0
	}
	return v
}
