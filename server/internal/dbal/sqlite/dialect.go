// Package sqlite is the dbal dialect of SQLite, where each solution
// database is one file (see docs/specs/sqlite_engine.md). The connection
// contract (pragmas, functions, transaction guard) lives in package dbal,
// next to the other engines' drivers.
package sqlite

import (
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// SQLiteDialect implements dbal.Dialect for SQLite.
type SQLiteDialect struct{}

func New() *SQLiteDialect {
	return &SQLiteDialect{}
}

func (d *SQLiteDialect) Engine() dbal.EngineType {
	return dbal.EngineSQLite
}

func (d *SQLiteDialect) QuoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

// Placeholder is numbered as on PostgreSQL: SQLite reads $N as the N-th
// argument, so a statement written for PostgreSQL, which may use one
// placeholder twice, binds the same values here.
func (d *SQLiteDialect) Placeholder(index int) string {
	return fmt.Sprintf("$%d", index)
}

// MapType gives each field type the declared type whose affinity stores it
// best; a CHECK keeps each column to values of its type
// (dbal.SQLiteTypeCheck). DATE and TIMESTAMP are declared as such so the driver reads them
// back as time.Time, as the other engines' drivers do; both hold text that
// sorts in time order (see dbal.TemporalValue). NUMERIC holds an integer or
// an IEEE double: 15 significant digits, where PostgreSQL's NUMERIC is
// exact (docs/specs/sqlite_engine.md, "Numbers").
func (d *SQLiteDialect) MapType(field dbal.AgnosticFieldType) string {
	switch field {
	case dbal.FieldTypeText:
		return "TEXT"
	case dbal.FieldTypeNumber:
		return "NUMERIC"
	case dbal.FieldTypeDate:
		return "DATE"
	case dbal.FieldTypeTimestamp:
		return "TIMESTAMP"
	case dbal.FieldTypeBoolean:
		return "BOOLEAN"
	case dbal.FieldTypeContainer:
		return "BLOB"
	default:
		return "TEXT"
	}
}

// BuildCreateTableSQL creates a user table. There is no IF NOT EXISTS: a name
// that is already taken must fail instead of registering an existing table.
//
// The creation order column is the table's INTEGER PRIMARY KEY (its rowid),
// the only column SQLite numbers by itself; AUTOINCREMENT keeps a number
// from being given again after the last record is deleted, as an identity
// does. The column declared the primary key then becomes NOT NULL UNIQUE,
// which every lookup and ON CONFLICT target reads the same way.
func (d *SQLiteDialect) BuildCreateTableSQL(def dbal.TableDefinition) (string, error) {
	if def.Name == "" {
		return "", fmt.Errorf("table name cannot be empty")
	}

	colDefs := make([]string, 0, len(def.Columns)+1)
	for _, col := range def.Columns {
		column := d.QuoteIdentifier(col.Name)
		clause := fmt.Sprintf("%s %s", column, d.MapType(col.Type))
		switch {
		case col.IsPrimaryKey && def.CreationOrder:
			clause += " NOT NULL UNIQUE"
		case col.IsPrimaryKey:
			// Only an INTEGER PRIMARY KEY refuses NULL by itself.
			clause += " NOT NULL PRIMARY KEY"
		case !col.IsNullable:
			clause += " NOT NULL"
		}
		if check := dbal.SQLiteTypeCheck(column, d.MapType(col.Type)); check != "" {
			clause += " " + check
		}
		colDefs = append(colDefs, clause)
	}
	if def.CreationOrder {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", d.QuoteIdentifier(dbal.CreationOrderColumn), dbal.CreationOrderType(dbal.EngineSQLite)))
	}

	return fmt.Sprintf("CREATE TABLE %s (\n  %s\n);",
		d.QuoteIdentifier(def.Name),
		strings.Join(colDefs, ",\n  "),
	), nil
}

// BuildAddColumnSQL adds a column. SQLite cannot add a NOT NULL column
// without a default; a CHECK refuses NULL instead, and like PostgreSQL's NOT
// NULL it refuses to be added to a table that already has records.
func (d *SQLiteDialect) BuildAddColumnSQL(tableName string, col dbal.ColumnDefinition) (string, error) {
	column := d.QuoteIdentifier(col.Name)
	clause := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		d.QuoteIdentifier(tableName), column, d.MapType(col.Type))
	if !col.IsNullable {
		clause += fmt.Sprintf(" CHECK (%s IS NOT NULL)", column)
	}
	if check := dbal.SQLiteTypeCheck(column, d.MapType(col.Type)); check != "" {
		clause += " " + check
	}
	return clause + ";", nil
}

// BuildAlterColumnTypeSQL changes a column's declared type. SQLite has no
// ALTER COLUMN; the connection runs this statement as PostgreSQL would,
// keeping the column's values and indexes (see dbal's SQLite connection).
func (d *SQLiteDialect) BuildAlterColumnTypeSQL(tableName, columnName string, newType dbal.AgnosticFieldType) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s;",
		d.QuoteIdentifier(tableName), d.QuoteIdentifier(columnName), d.MapType(newType)), nil
}

// TimestampColumn defaults to the current time in the text format bound
// timestamps are stored in (UTC, fraction without trailing zeros), so both
// sort together.
func (d *SQLiteDialect) TimestampColumn() string {
	return "TIMESTAMP DEFAULT (rtrim(rtrim(strftime('%Y-%m-%d %H:%M:%f', 'now'), '0'), '.') || '+00:00')"
}

func (d *SQLiteDialect) CastToText(expr string) string {
	return fmt.Sprintf("CAST(%s AS TEXT)", expr)
}

// CaseInsensitiveLike lower-cases both sides with the Unicode lower()
// File4Base registers (SQLite's own folds ASCII only), and reads a backslash
// as the escape character, as PostgreSQL and MariaDB do by default.
func (d *SQLiteDialect) CaseInsensitiveLike(expr, placeholder string) string {
	return fmt.Sprintf("LOWER(%s) LIKE LOWER(%s) ESCAPE '\\'", d.CastToText(expr), placeholder)
}

// NumericValue uses the REGEXP operator, which calls the regexp() function
// File4Base registers on every connection.
func (d *SQLiteDialect) NumericValue(expr string) string {
	text := d.CastToText(expr)
	return fmt.Sprintf("(CASE WHEN %s REGEXP '%s' THEN CAST(%s AS NUMERIC) ELSE NULL END)", text, dbal.NumericPattern, text)
}

// BuildDropColumnSQL drops a column. SQLite has no DROP COLUMN IF EXISTS; the
// connection reads the IF EXISTS PostgreSQL writes (see dbal's SQLite
// connection), so the statement is the same.
func (d *SQLiteDialect) BuildDropColumnSQL(tableName string, columnName string) (string, error) {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;",
		d.QuoteIdentifier(tableName),
		d.QuoteIdentifier(columnName),
	), nil
}
