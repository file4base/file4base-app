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
		if col.DefaultValue != nil {
			clause += fmt.Sprintf(" DEFAULT %s", *col.DefaultValue)
		}
		colDefs = append(colDefs, clause)
	}

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n);",
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
	if col.DefaultValue != nil {
		clause += fmt.Sprintf(" DEFAULT %s", *col.DefaultValue)
	}
	return clause + ";", nil
}

func (p *PostgresDialect) BuildDropColumnSQL(tableName string, columnName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;",
		p.QuoteIdentifier(tableName),
		p.QuoteIdentifier(columnName),
	), nil
}
