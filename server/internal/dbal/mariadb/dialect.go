package mariadb

import (
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// MariaDBDialect implements dbal.Dialect for MariaDB/MySQL
type MariaDBDialect struct{}

func New() *MariaDBDialect {
	return &MariaDBDialect{}
}

func (m *MariaDBDialect) Engine() dbal.EngineType {
	return dbal.EngineMariaDB
}

func (m *MariaDBDialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func (m *MariaDBDialect) Placeholder(index int) string {
	return "?"
}

func (m *MariaDBDialect) MapType(field dbal.AgnosticFieldType) string {
	switch field {
	case dbal.FieldTypeText:
		return "LONGTEXT"
	case dbal.FieldTypeNumber:
		return "DECIMAL(65, 10)"
	case dbal.FieldTypeDate:
		return "DATE"
	case dbal.FieldTypeTimestamp:
		return "DATETIME(6)"
	case dbal.FieldTypeBoolean:
		return "BOOLEAN"
	case dbal.FieldTypeContainer:
		return "LONGBLOB"
	case dbal.FieldTypeCalculation, dbal.FieldTypeSummary:
		return "LONGTEXT"
	default:
		return "LONGTEXT"
	}
}

func (m *MariaDBDialect) BuildCreateTableSQL(def dbal.TableDefinition) (string, error) {
	if def.Name == "" {
		return "", fmt.Errorf("table name cannot be empty")
	}

	colDefs := make([]string, 0, len(def.Columns))
	for _, col := range def.Columns {
		clause := fmt.Sprintf("%s %s", m.QuoteIdentifier(col.Name), m.MapType(col.Type))
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

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n  %s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;",
		m.QuoteIdentifier(def.Name),
		strings.Join(colDefs, ",\n  "),
	), nil
}

func (m *MariaDBDialect) BuildAddColumnSQL(tableName string, col dbal.ColumnDefinition) (string, error) {
	clause := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		m.QuoteIdentifier(tableName),
		m.QuoteIdentifier(col.Name),
		m.MapType(col.Type),
	)
	if !col.IsNullable {
		clause += " NOT NULL"
	}
	if col.DefaultValue != nil {
		clause += fmt.Sprintf(" DEFAULT %s", *col.DefaultValue)
	}
	return clause + ";", nil
}

func (m *MariaDBDialect) BuildDropColumnSQL(tableName string, columnName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		m.QuoteIdentifier(tableName),
		m.QuoteIdentifier(columnName),
	), nil
}
