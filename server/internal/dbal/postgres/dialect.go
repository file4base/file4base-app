package postgres

import (
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// PostgresDialect implements dbal.Dialect for PostgreSQL
type PostgresDialect struct{}

func New() *PostgresDialect {
	return &PostgresDialect{}
}

func (p *PostgresDialect) Engine() dbal.EngineType {
	return dbal.EnginePostgres
}

func (p *PostgresDialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

func (p *PostgresDialect) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

func (p *PostgresDialect) MapType(field dbal.AgnosticFieldType) string {
	switch field {
	case dbal.FieldTypeText:
		return "TEXT"
	case dbal.FieldTypeNumber:
		return "NUMERIC"
	case dbal.FieldTypeDate:
		return "DATE"
	case dbal.FieldTypeTimestamp:
		return "TIMESTAMPTZ"
	case dbal.FieldTypeBoolean:
		return "BOOLEAN"
	case dbal.FieldTypeContainer:
		return "BYTEA"
	case dbal.FieldTypeCalculation, dbal.FieldTypeSummary:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// BuildCreateTableSQL creates a user table. There is no IF NOT EXISTS: a name
// that is already taken must fail instead of registering an existing table.
func (p *PostgresDialect) BuildCreateTableSQL(def dbal.TableDefinition) (string, error) {
	if def.Name == "" {
		return "", fmt.Errorf("table name cannot be empty")
	}

	colDefs := make([]string, 0, len(def.Columns))
	for _, col := range def.Columns {
		clause := fmt.Sprintf("%s %s", p.QuoteIdentifier(col.Name), p.MapType(col.Type))
		if col.IsPrimaryKey {
			clause += " PRIMARY KEY"
		}
		if !col.IsNullable && !col.IsPrimaryKey {
			clause += " NOT NULL"
		}
		colDefs = append(colDefs, clause)
	}

	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n);",
		p.QuoteIdentifier(def.Name),
		strings.Join(colDefs, ",\n  "),
	), nil
}

func (p *PostgresDialect) BuildAddColumnSQL(tableName string, col dbal.ColumnDefinition) (string, error) {
	clause := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		p.QuoteIdentifier(tableName),
		p.QuoteIdentifier(col.Name),
		p.MapType(col.Type),
	)
	if !col.IsNullable {
		clause += " NOT NULL"
	}
	return clause + ";", nil
}

func (p *PostgresDialect) BuildAlterColumnTypeSQL(tableName, columnName string, newType dbal.AgnosticFieldType) (string, error) {
	target := p.MapType(newType)
	// USING makes PostgreSQL convert what is there instead of refusing the
	// change; an empty string is not a number, so it becomes NULL.
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s USING NULLIF(%s::text, '')::%s;",
		p.QuoteIdentifier(tableName),
		p.QuoteIdentifier(columnName),
		target,
		p.QuoteIdentifier(columnName),
		target,
	), nil
}

func (p *PostgresDialect) TimestampColumn() string {
	return "TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP"
}

func (p *PostgresDialect) CastToText(expr string) string {
	return fmt.Sprintf("CAST(%s AS TEXT)", expr)
}

func (p *PostgresDialect) CaseInsensitiveLike(expr, placeholder string) string {
	return fmt.Sprintf("%s ILIKE %s", p.CastToText(expr), placeholder)
}

func (p *PostgresDialect) NumericValue(expr string) string {
	text := p.CastToText(expr)
	return fmt.Sprintf("(CASE WHEN %s ~ '%s' THEN CAST(%s AS NUMERIC) ELSE NULL END)", text, dbal.NumericPattern, text)
}

func (p *PostgresDialect) BuildDropColumnSQL(tableName string, columnName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;",
		p.QuoteIdentifier(tableName),
		p.QuoteIdentifier(columnName),
	), nil
}
