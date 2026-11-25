package data

import (
	"fmt"
	"strings"

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
}

// keyColumnOf is the match field column of a table whose fields are
// fields, qualified by the table's name or alias when qualifier is given.
func keyColumnOf(dialect dbal.Dialect, qualifier string, fields fieldSet, column string) multiKeyColumn {
	name := dialect.QuoteIdentifier(column)
	if qualifier != "" {
		name = qualifier + "." + name
	}
	return multiKeyColumn{sql: name, lines: fields[column] == dbal.FieldTypeText}
}

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
		"'$[*]' COLUMNS (f4b_n FOR ORDINALITY)) %s", text, text, mariaLF, alias)
}

// mariaLine is the line of a text expression a row of its mariaLineTable
// alias numbers. It keeps the expression's collation, so a line compares as
// the whole field would.
func mariaLine(expr, alias string) string {
	return fmt.Sprintf("SUBSTRING_INDEX(SUBSTRING_INDEX(%s, %s, %s.f4b_n), %s, -1)", mariaText(expr), mariaLF, alias, mariaLF)
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
		return fmt.Sprintf("EXISTS (SELECT 1 FROM unnest(%s) AS f4b_line(v) WHERE %s)",
			pgLines(column.sql), anyOf("f4b_line.v", op, keys()))
	}
	// A field of one line is compared whole, without being split.
	line := mariaLine(column.sql, "f4b_line")
	return fmt.Sprintf("(CASE WHEN %s THEN %s ELSE EXISTS (SELECT 1 FROM %s WHERE %s <> '' AND %s) END)",
		mariaSingleLine(column.sql), anyOf(column.sql, op, keys()),
		mariaLineTable(column.sql, "f4b_line"), line, anyOf(line, op, keys()))
}

// keysMeet is the condition that the match fields left and right meet op,
// left op right: a key of left against a key of right, each a line of its
// field when the field is text.
func keysMeet(dialect dbal.Dialect, left multiKeyColumn, op string, right multiKeyColumn) string {
	switch {
	case !left.lines && !right.lines:
		return fmt.Sprintf("%s %s %s", left.sql, op, right.sql)
	case !right.lines:
		return meetsKeys(dialect, left, op, func() []string { return []string{right.sql} })
	case !left.lines:
		return meetsKeys(dialect, right, reversed[op], func() []string { return []string{left.sql} })
	}
	if dialect.Engine() == dbal.EnginePostgres {
		if op == "=" {
			return fmt.Sprintf("%s && %s", pgLines(left.sql), pgLines(right.sql))
		}
		return fmt.Sprintf("EXISTS (SELECT 1 FROM unnest(%s) AS f4b_left(v), unnest(%s) AS f4b_right(v) WHERE f4b_left.v %s f4b_right.v)",
			pgLines(left.sql), pgLines(right.sql), op)
	}
	l, r := mariaLine(left.sql, "f4b_left"), mariaLine(right.sql, "f4b_right")
	return fmt.Sprintf("(CASE WHEN %s AND %s THEN %s %s %s ELSE EXISTS (SELECT 1 FROM %s, %s WHERE %s <> '' AND %s <> '' AND %s %s %s) END)",
		mariaSingleLine(left.sql), mariaSingleLine(right.sql), left.sql, op, right.sql,
		mariaLineTable(left.sql, "f4b_left"), mariaLineTable(right.sql, "f4b_right"), l, r, l, op, r)
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
		return fmt.Sprintf("CROSS JOIN LATERAL unnest(%s) AS %s(v)", pgLines(column.sql), alias), alias + ".v", ""
	}
	line := mariaLine(column.sql, alias)
	return "CROSS JOIN " + mariaLineTable(column.sql, alias), line, line + " <> ''"
}

// keyText is a key compared with the lines of a text field, as text.
func keyText(key interface{}) interface{} {
	switch k := key.(type) {
	case string:
		return k
	case []byte:
		return string(k)
	case nil:
		return nil
	}
	return fmt.Sprint(key)
}
