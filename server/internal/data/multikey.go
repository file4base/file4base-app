package data

import (
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Multi-keys.
//
// A text match field holding several lines is a multi-key, as in FileMaker:
// each line is a key of its own, and the field matches through any of them,
// on either side of a relationship and for every operator, since FileMaker
// resolves a relationship through the field's index, which holds each line
// as a value. A record keyed A¶B is related to a record keyed A and to one
// keyed B, whichever of the two holds the multi-key.
//
// The keys of the record in hand are split before the statement is built
// (matchKeys); the keys of the records looked for are split by the
// statement, below. Only a text field holds lines: a number, date, time or
// timestamp column holds one value.
//
// FileMaker separates the lines of a field with a carriage return (CR). A
// migrated file's text has its CRs turned into line feeds (LF), and a client
// may write either, or both (CRLF), so a CR and an LF both end a line here,
// and the empty line between the two of a CRLF is no key.
//
// A line is a key when it is not empty, whatever it holds: a line of
// spaces is a key, compared as the engine's collation compares it. The keys
// of the record in hand (matchKeys) and the lines a statement splits follow
// this one rule; on MariaDB, whose collations pad with spaces, a line is
// tested by its length, since a line of spaces equals an empty one there.
//
// The names a statement gives the lines it splits hold a $, which no
// field or table name does (dbal.CheckIdentifier), so a line can never be
// mistaken for a field of the table, nor a field for a line.
//
// Performance. On PostgreSQL the condition for = is an overlap of the
// field's lines with the keys,
//
//	array_remove(string_to_array(replace(field, E'\r', E'\n'), E'\n'), '') && ARRAY[...]
//
// which a GIN index on that same expression serves:
//
//	CREATE INDEX ON "table" USING gin (array_remove(string_to_array(replace("field", E'\r', E'\n'), E'\n'), ''));
//
// Without one, or for another operator, each record's field is split as it
// is read. MariaDB has no index on lines: a field holding one line is
// compared whole, as before, and a field holding several is split with
// JSON_TABLE, line by line, at a cost growing with its length and number of
// lines. Neither engine indexes a LONGTEXT/TEXT match field by default.

// multiKeyColumn is a match field in SQL: an expression, and whether it is
// a text field, whose lines are its keys.
type multiKeyColumn struct {
	sql   string
	lines bool
	// kind is the field's type, which says how a field that is not text is
	// written as text when it meets the lines of one that is (asKeyText).
	kind dbal.AgnosticFieldType
}

// keyColumnOf is the match field column of a table whose fields are
// fields, qualified by the table's name or alias when qualifier is given.
func keyColumnOf(dialect dbal.Dialect, qualifier string, fields fieldSet, column string) multiKeyColumn {
	name := dialect.QuoteIdentifier(column)
	if qualifier != "" {
		name = qualifier + "." + name
	}
	kind := fields[column]
	return multiKeyColumn{sql: name, lines: kind == dbal.FieldTypeText, kind: kind}
}

// keyValue is a key as it is bound against a match field of type kind. On
// SQLite, the driver reads a DATE or TIMESTAMP column as a time.Time and
// would bind one back as a timestamp text, "2026-01-02 00:00:00+00:00",
// which a DATE column holding "2026-01-02" does not equal: a date or a
// timestamp is bound as the text a column of kind holds
// (dbal.TemporalValue). That is what PostgreSQL does too, which takes the
// parameter as the column's type: a timestamp compared with a date field
// is its day.
func keyValue(dialect dbal.Dialect, kind dbal.AgnosticFieldType, key interface{}) interface{} {
	if dialect.Engine() != dbal.EngineSQLite {
		return key
	}
	return dbal.TemporalValue(dbal.EngineSQLite, kind, key)
}

// Names of the lines a statement splits, and of their parts. Each holds a $
// so as not to collide with a field.
const (
	lineAlias  = "f4b$line"  // a line of the field in hand
	leftAlias  = "f4b$left"  // a line of the left field of two
	rightAlias = "f4b$right" // a line of the right field of two
	lineValue  = "f4b$v"     // PostgreSQL: the line itself
	lineNumber = "f4b$n"     // MariaDB: the line's number, from 1
)

// lineAliasOf is the name of the lines of the i-th match field a statement
// splits, when it splits several.
func lineAliasOf(i int) string { return fmt.Sprintf("f4b$l%d", i) }

// pgLines is the lines of a text expression on PostgreSQL, as an array of
// text without the empty ones. It is the expression a GIN index serves.
func pgLines(expr string) string {
	return fmt.Sprintf(`array_remove(string_to_array(replace(%s, E'\r', E'\n'), E'\n'), '')`, expr)
}

// MariaDB's line feed and carriage return, in the connection's character
// set: CHAR(10) alone is a binary string, which would compare the lines of
// a field byte for byte rather than in its collation.
const (
	mariaLF = "CHAR(10 USING utf8mb4)"
	mariaCR = "CHAR(13 USING utf8mb4)"
)

// mariaText is a text expression on MariaDB with its CRs read as LFs.
func mariaText(expr string) string {
	return fmt.Sprintf("REPLACE(%s, %s, %s)", expr, mariaCR, mariaLF)
}

// mariaSingleLine is the condition that a text expression holds one line.
func mariaSingleLine(expr string) string {
	return fmt.Sprintf("INSTR(%s, %s) = 0", mariaText(expr), mariaLF)
}

// mariaLineTable is a table of one row per line of a text expression on
// MariaDB, named alias: JSON_TABLE numbers as many rows as the expression
// has lines, and mariaLine reads the line of each. The JSON it reads holds
// no part of the text, so no value has to be escaped.
func mariaLineTable(expr, alias string) string {
	text := mariaText(expr)
	return fmt.Sprintf("JSON_TABLE(CONCAT('[', REPEAT('0,', CHAR_LENGTH(%s) - CHAR_LENGTH(REPLACE(%s, %s, ''))), '0]'), "+
		"'$[*]' COLUMNS (%s FOR ORDINALITY)) %s", text, text, mariaLF, lineNumber, alias)
}

// mariaLine is the line of a text expression a row of its mariaLineTable
// alias numbers. It keeps the expression's collation, so a line compares as
// the whole field would.
func mariaLine(expr, alias string) string {
	return fmt.Sprintf("SUBSTRING_INDEX(SUBSTRING_INDEX(%s, %s, %s.%s), %s, -1)", mariaText(expr), mariaLF, alias, lineNumber, mariaLF)
}

// mariaPresent is the condition that a line on MariaDB is not empty. It is
// tested by length: a collation that pads with spaces holds a line of spaces
// equal to an empty string.
func mariaPresent(line string) string {
	return fmt.Sprintf("CHAR_LENGTH(%s) > 0", line)
}

// sqliteLines is the FROM items of a table of the lines of a text
// expression on SQLite, as alias: json_each over the JSON array f4b_lines (a
// function registered on every connection) makes of them, without the empty
// ones. alias.value is the line. There is no index on lines, as there is
// none on MariaDB.
//
// The expression is read in a subquery of its own: inside json_each's
// arguments, a field named like one of json_each's columns (key, value,
// type, path...) would be read as that column.
func sqliteLines(expr, alias string) string {
	return fmt.Sprintf("(SELECT f4b_lines(%s) AS f4b$j) AS %s$s, temp.json_each(%s$s.f4b$j) AS %s", expr, alias, alias, alias)
}

// anyOf is the condition that expr meets op against one of keys.
func anyOf(expr, op string, keys []string) string {
	if op == "=" {
		return fmt.Sprintf("%s IN (%s)", expr, strings.Join(keys, ", "))
	}
	parts := make([]string, len(keys))
	for i, key := range keys {
		parts[i] = fmt.Sprintf("%s %s %s", expr, op, key)
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// meetsKeys is the condition that the match field column meets op against
// one of the keys keys gives, each of which is one value: column op key.
// When column is a text field, any of its lines does.
//
// keys gives the keys' SQL each time it is called, so a placeholder used
// twice is bound twice: a MariaDB placeholder takes the next value.
func meetsKeys(dialect dbal.Dialect, column multiKeyColumn, op string, keys func() []string) string {
	if !column.lines {
		return anyOf(column.sql, op, keys())
	}
	if dialect.Engine() == dbal.EnginePostgres {
		if op == "=" {
			list := keys()
			for i, key := range list {
				list[i] = fmt.Sprintf("CAST(%s AS TEXT)", key)
			}
			return fmt.Sprintf("%s && ARRAY[%s]", pgLines(column.sql), strings.Join(list, ", "))
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM unnest(%s) AS %s(%s) WHERE %s)",
			pgLines(column.sql), lineAlias, lineValue, anyOf(lineAlias+"."+lineValue, op, keys()))
	}
	if dialect.Engine() == dbal.EngineSQLite {
		return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s)",
			sqliteLines(column.sql, lineAlias), anyOf(lineAlias+".value", op, keys()))
	}
	// A field of one line is compared whole, without being split.
	line := mariaLine(column.sql, lineAlias)
	return fmt.Sprintf("(CASE WHEN %s THEN %s ELSE EXISTS (SELECT 1 FROM %s WHERE %s AND %s) END)",
		mariaSingleLine(column.sql), anyOf(column.sql, op, keys()),
		mariaLineTable(column.sql, lineAlias), mariaPresent(line), anyOf(line, op, keys()))
}

// keysMeet is the condition that the match fields left and right meet op,
// left op right: a key of left against a key of right, each a line of its
// field when the field is text.
func keysMeet(dialect dbal.Dialect, left multiKeyColumn, op string, right multiKeyColumn) string {
	switch {
	case !left.lines && !right.lines:
		l, r := left.sql, right.sql
		if dialect.Engine() == dbal.EngineSQLite {
			// A date against a timestamp is midnight of its day, as
			// PostgreSQL promotes it: the texts compare in time order once
			// both are timestamps (dbal.TemporalValue).
			l, r = sqliteAsTimestamp(left, right.kind), sqliteAsTimestamp(right, left.kind)
		}
		return fmt.Sprintf("%s %s %s", l, op, r)
	case !right.lines:
		return meetsKeys(dialect, left, op, func() []string { return []string{asKeyText(dialect, right)} })
	case !left.lines:
		return meetsKeys(dialect, right, reversed[op], func() []string { return []string{asKeyText(dialect, left)} })
	}
	if dialect.Engine() == dbal.EnginePostgres {
		if op == "=" {
			return fmt.Sprintf("%s && %s", pgLines(left.sql), pgLines(right.sql))
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM unnest(%s) AS %s(%s), unnest(%s) AS %s(%s) WHERE %s.%s %s %s.%s)",
			pgLines(left.sql), leftAlias, lineValue, pgLines(right.sql), rightAlias, lineValue,
			leftAlias, lineValue, op, rightAlias, lineValue)
	}
	if dialect.Engine() == dbal.EngineSQLite {
		return fmt.Sprintf("EXISTS (SELECT 1 FROM %s, %s WHERE %s.value %s %s.value)",
			sqliteLines(left.sql, leftAlias), sqliteLines(right.sql, rightAlias), leftAlias, op, rightAlias)
	}
	l, r := mariaLine(left.sql, leftAlias), mariaLine(right.sql, rightAlias)
	return fmt.Sprintf("(CASE WHEN %s AND %s THEN %s %s %s ELSE EXISTS (SELECT 1 FROM %s, %s WHERE %s AND %s AND %s %s %s) END)",
		mariaSingleLine(left.sql), mariaSingleLine(right.sql), left.sql, op, right.sql,
		mariaLineTable(left.sql, leftAlias), mariaLineTable(right.sql, rightAlias), mariaPresent(l), mariaPresent(r), l, op, r)
}

// sqliteAsTimestamp is a date column, compared with a timestamp, as the
// text of the timestamp at midnight of its day; any other column as it is.
func sqliteAsTimestamp(column multiKeyColumn, other dbal.AgnosticFieldType) string {
	if column.kind == dbal.FieldTypeDate && other == dbal.FieldTypeTimestamp {
		return fmt.Sprintf("(%s || ' 00:00:00+00:00')", column.sql)
	}
	return column.sql
}

// lineSource is how a statement reads the keys of a text match field one
// line at a time: a FROM item to join the field's table with (each record
// then appears once per line), and the expression of the line. A field that
// is not text is read as it is, with nothing to join.
func lineSource(dialect dbal.Dialect, column multiKeyColumn, alias string) (from, key, present string) {
	if !column.lines {
		return "", column.sql, ""
	}
	if dialect.Engine() == dbal.EnginePostgres {
		return fmt.Sprintf("CROSS JOIN LATERAL unnest(%s) AS %s(%s)", pgLines(column.sql), alias, lineValue), alias + "." + lineValue, ""
	}
	if dialect.Engine() == dbal.EngineSQLite {
		// The column is qualified by its table, so json_each's own columns
		// cannot hide it.
		return fmt.Sprintf("CROSS JOIN temp.json_each(f4b_lines(%s)) AS %s", column.sql, alias), alias + ".value", ""
	}
	line := mariaLine(column.sql, alias)
	return "CROSS JOIN " + mariaLineTable(column.sql, alias), line, mariaPresent(line)
}

// A date or timestamp compared with the lines of a text field is written as
// text, the same way in Go (keyText) and in SQL (asKeyText): a date as
// YYYY-MM-DD, as a date field shows it, and a timestamp in UTC as
// YYYY-MM-DD HH:MM:SS, without its fraction of a second, or as a date alone
// at midnight exactly. The drivers give a date as a timestamp at midnight
// UTC, so a value read in Go says no more than that; SQL writes a timestamp
// column by the same rule, so both sides agree.
const (
	keyDateLayout      = "2006-01-02"
	keyTimestampLayout = "2006-01-02 15:04:05"
)

// keyText is a key compared with the lines of a text field, as text.
func keyText(key interface{}) interface{} {
	switch k := key.(type) {
	case string:
		return k
	case []byte:
		return string(k)
	case nil:
		return nil
	case time.Time:
		k = k.UTC()
		if k.Equal(k.Truncate(24 * time.Hour)) {
			return k.Format(keyDateLayout)
		}
		return k.Format(keyTimestampLayout)
	}
	return fmt.Sprint(key)
}

// asKeyText is a match field that is not text, compared with the lines of
// one that is, as text: a date or timestamp as keyText writes one.
func asKeyText(dialect dbal.Dialect, column multiKeyColumn) string {
	postgres := dialect.Engine() == dbal.EnginePostgres
	if dialect.Engine() == dbal.EngineSQLite {
		// Both hold the text of their UTC value (dbal.TemporalValue), which
		// SQLite's date functions read.
		// A timestamp is midnight exactly when its stored text says so: the
		// date functions round to the millisecond, where keyText does not.
		// A number is compared as its text, as a line is text and SQLite
		// never holds a number equal to a text.
		switch column.kind {
		case dbal.FieldTypeDate:
			return fmt.Sprintf("date(%s)", column.sql)
		case dbal.FieldTypeTimestamp:
			return fmt.Sprintf("CASE WHEN substr(%s, 11) = ' 00:00:00+00:00' THEN date(%s) ELSE datetime(%s) END",
				column.sql, column.sql, column.sql)
		case dbal.FieldTypeNumber:
			return fmt.Sprintf("%s(%s)", dbal.SQLiteNumberText, column.sql)
		}
		return column.sql
	}
	switch column.kind {
	case dbal.FieldTypeDate:
		if postgres {
			return fmt.Sprintf("to_char(%s, 'YYYY-MM-DD')", column.sql)
		}
		return fmt.Sprintf("DATE_FORMAT(%s, '%%Y-%%m-%%d')", column.sql)
	case dbal.FieldTypeTimestamp:
		if postgres {
			utc := fmt.Sprintf("(%s AT TIME ZONE 'UTC')", column.sql)
			return fmt.Sprintf("CASE WHEN CAST(%s AS TIME) = TIME '00:00' THEN to_char(%s, 'YYYY-MM-DD') ELSE to_char(%s, 'YYYY-MM-DD HH24:MI:SS') END",
				utc, utc, utc)
		}
		// A DATETIME holds the UTC time it was given.
		return fmt.Sprintf("CASE WHEN TIME(%s) = '00:00:00' THEN DATE_FORMAT(%s, '%%Y-%%m-%%d') ELSE DATE_FORMAT(%s, '%%Y-%%m-%%d %%H:%%i:%%s') END",
			column.sql, column.sql, column.sql)
	}
	return column.sql
}
