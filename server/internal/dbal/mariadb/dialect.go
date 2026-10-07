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

// BuildCreateTableSQL creates a user table. There is no IF NOT EXISTS: a name
// that is already taken must fail instead of registering an existing table.
func (m *MariaDBDialect) BuildCreateTableSQL(def dbal.TableDefinition) (string, error) {
	if def.Name == "" {
		return "", fmt.Errorf("table name cannot be empty")
	}

	colDefs := make([]string, 0, len(def.Columns))
	for _, col := range def.Columns {
		clause := fmt.Sprintf("%s %s", m.QuoteIdentifier(col.Name), m.columnType(col))
		if col.IsPrimaryKey {
			clause += " PRIMARY KEY"
		}
		if !col.IsNullable && !col.IsPrimaryKey {
			clause += " NOT NULL"
		}
		colDefs = append(colDefs, clause)
	}

	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;",
		m.QuoteIdentifier(def.Name),
		strings.Join(colDefs, ",\n  "),
	), nil
}

// columnType is the physical type of a column. A key column cannot be a BLOB
// or TEXT type here without a key length ("BLOB/TEXT column used in key
// specification without a key length"), so an indexed text column becomes a
// VARCHAR long enough for the ids File4Base generates.
func (m *MariaDBDialect) columnType(col dbal.ColumnDefinition) string {
	if col.IsPrimaryKey && m.MapType(col.Type) == "LONGTEXT" {
		return fmt.Sprintf("VARCHAR(%d)", dbal.MaxIdentifierLength*4)
	}
	return m.MapType(col.Type)
}

func (m *MariaDBDialect) BuildAddColumnSQL(tableName string, col dbal.ColumnDefinition) (string, error) {
	clause := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		m.QuoteIdentifier(tableName),
		m.QuoteIdentifier(col.Name),
		m.columnType(col),
	)
	if !col.IsNullable {
		clause += " NOT NULL"
	}
	return clause + ";", nil
}

func (m *MariaDBDialect) BuildAlterColumnTypeSQL(tableName, columnName string, newType dbal.AgnosticFieldType) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s MODIFY %s %s;",
		m.QuoteIdentifier(tableName),
		m.QuoteIdentifier(columnName),
		m.columnType(dbal.ColumnDefinition{Name: columnName, Type: newType, IsNullable: true}),
	), nil
}

func (m *MariaDBDialect) TimestampColumn() string {
	// The precision of the default has to match the column's.
	return "DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6)"
}

func (m *MariaDBDialect) CastToText(expr string) string {
	return fmt.Sprintf("CAST(%s AS CHAR)", expr)
}

func (m *MariaDBDialect) CaseInsensitiveLike(expr, placeholder string) string {
	// LIKE is case-insensitive under the usual collations, but not under a
	// binary one, so both sides are lower-cased explicitly.
	return fmt.Sprintf("LOWER(%s) LIKE LOWER(%s)", m.CastToText(expr), placeholder)
}

func (m *MariaDBDialect) NumericValue(expr string) string {
	text := m.CastToText(expr)
	return fmt.Sprintf("(CASE WHEN %s REGEXP '%s' THEN CAST(%s AS DECIMAL(65,10)) ELSE NULL END)", text, dbal.NumericPattern, text)
}

func (m *MariaDBDialect) BuildDropColumnSQL(tableName string, columnName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;",
		m.QuoteIdentifier(tableName),
		m.QuoteIdentifier(columnName),
	), nil
}
