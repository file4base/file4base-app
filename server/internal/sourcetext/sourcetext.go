// Package sourcetext keeps the original text of field values that a typed
// column cannot hold as they were written (#124).
//
// FileMaker fields are permissive: a Number field can hold "12-A", "(4x2)" or
// "N/A", a Date field text that is no date, a Time field a duration past 24
// hours. FileMaker keeps that text and computes with its own interpretation of
// it (GetAsNumber("12-A") is 12). A migrated File4Base field keeps both: its
// typed column holds the interpretation, which is what sorting, finds,
// calculations and scripts use, and this package keeps the text as it was
// written, which is what the record shows.
//
// Only values whose text differs from what the typed column says are kept, in
// one system table per database keyed by table, record id and field. A record
// carries them, when it has any, under the reserved key Key: a map of field
// name to original text. Writing a new value to a field drops its original
// text, since the field then holds exactly what it says.
package sourcetext

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

const (
	// Key is the reserved record key under which a record carries the
	// original texts of its fields, as a map of field name to text. Field
	// names start with a letter, so it can never name a field.
	Key = "__source_text"

	// Table is the system table that keeps the original texts.
	Table = "sys_source_values"

	// MaxTextBytes bounds one original text. FileMaker field text can be
	// long, but a value a typed field was given is a short one.
	MaxTextBytes = 64 << 10

	// loadChunk bounds the record ids of one lookup query.
	loadChunk = 500
)

// CreateTableSQL creates the system table. It is the same on every engine.
const CreateTableSQL = `CREATE TABLE IF NOT EXISTS ` + Table + ` (
			table_name VARCHAR(64) NOT NULL,
			record_id VARCHAR(64) NOT NULL,
			field_name VARCHAR(64) NOT NULL,
			source_text TEXT NOT NULL,
			PRIMARY KEY (table_name, record_id, field_name)
		);`

// Execer runs statements: a *sql.DB or a *sql.Tx.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// QueryExecer runs statements and queries: a *sql.DB or a *sql.Tx.
type QueryExecer interface {
	Execer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Texts maps a record id to the original texts of its fields.
type Texts map[string]map[string]string

// Ensure creates the system table when it is missing. Databases created before
// it existed get it the first time something needs it.
func Ensure(ctx context.Context, q Execer) error {
	if _, err := q.ExecContext(ctx, CreateTableSQL); err != nil {
		return fmt.Errorf("failed creating %s: %w", Table, err)
	}
	return nil
}

func placeholders(d dbal.Dialect, from, n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = d.Placeholder(from + i)
	}
	return strings.Join(parts, ", ")
}

// Put records the original texts of one record's fields, replacing any it
// had for those fields. The table must exist (see Ensure).
func Put(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string, texts map[string]string) error {
	if len(texts) == 0 {
		return nil
	}
	fields := make([]string, 0, len(texts))
	for field := range texts {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	if err := clearFields(ctx, q, d, table, recordID, fields); err != nil {
		return err
	}
	insert := fmt.Sprintf(`INSERT INTO %s (table_name, record_id, field_name, source_text) VALUES (%s)`,
		Table, placeholders(d, 1, 4))
	for _, field := range fields {
		if _, err := q.ExecContext(ctx, insert, table, recordID, field, texts[field]); err != nil {
			return fmt.Errorf("failed keeping the original text of %s.%s: %w", table, field, err)
		}
	}
	return nil
}

// Load returns the original texts of the given records of a table. A
// database without the system table has none.
func Load(ctx context.Context, q QueryExecer, d dbal.Dialect, table string, recordIDs []string) (Texts, error) {
	out := Texts{}
	for start := 0; start < len(recordIDs); start += loadChunk {
		end := start + loadChunk
		if end > len(recordIDs) {
			end = len(recordIDs)
		}
		ids := recordIDs[start:end]
		args := make([]any, 0, len(ids)+1)
		args = append(args, table)
		for _, id := range ids {
			args = append(args, id)
		}
		query := fmt.Sprintf(`SELECT record_id, field_name, source_text FROM %s WHERE table_name = %s AND record_id IN (%s)`,
			Table, d.Placeholder(1), placeholders(d, 2, len(ids)))
		if err := scanInto(ctx, q, query, args, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// LoadTable returns every original text kept for a table.
func LoadTable(ctx context.Context, q QueryExecer, d dbal.Dialect, table string) (Texts, error) {
	out := Texts{}
	query := fmt.Sprintf(`SELECT record_id, field_name, source_text FROM %s WHERE table_name = %s`, Table, d.Placeholder(1))
	if err := scanInto(ctx, q, query, []any{table}, out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanInto(ctx context.Context, q QueryExecer, query string, args []any, out Texts) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		if dbal.IsUndefinedTable(err) {
			return nil
		}
		return fmt.Errorf("failed reading original texts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, field, text string
		if err := rows.Scan(&id, &field, &text); err != nil {
			return fmt.Errorf("failed reading original texts: %w", err)
		}
		if out[id] == nil {
			out[id] = map[string]string{}
		}
		out[id][field] = text
	}
	return rows.Err()
}

// Attach puts each record's original texts under Key. Records without any
// are left as they are.
func Attach(rows []map[string]interface{}, texts Texts) {
	if len(texts) == 0 {
		return
	}
	for _, row := range rows {
		id := fmt.Sprint(row["id"])
		if fields := texts[id]; len(fields) > 0 {
			copied := make(map[string]string, len(fields))
			for k, v := range fields {
				copied[k] = v
			}
			row[Key] = copied
		}
	}
}

// AttachFromDB loads and attaches the original texts of rows of a table.
func AttachFromDB(ctx context.Context, q QueryExecer, d dbal.Dialect, table string, rows []map[string]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if id, ok := row["id"]; ok && id != nil {
			ids = append(ids, fmt.Sprint(id))
		}
	}
	texts, err := Load(ctx, q, d, table, ids)
	if err != nil {
		return err
	}
	Attach(rows, texts)
	return nil
}

func clearFields(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	args := make([]any, 0, len(fields)+2)
	args = append(args, table, recordID)
	for _, f := range fields {
		args = append(args, f)
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE table_name = %s AND record_id = %s AND field_name IN (%s)`,
		Table, d.Placeholder(1), d.Placeholder(2), placeholders(d, 3, len(fields)))
	if _, err := q.ExecContext(ctx, stmt, args...); err != nil && !dbal.IsUndefinedTable(err) {
		return fmt.Errorf("failed dropping original texts of %s: %w", table, err)
	}
	return nil
}

// ClearFields drops the original texts of the given fields of one record:
// the fields were just given new values, which say exactly what they hold.
//
// On PostgreSQL a failed statement aborts its transaction, so inside one the
// table must exist (see Ensure); on a plain connection a missing table is
// simply nothing to clear.
func ClearFields(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string, fields []string) error {
	return clearFields(ctx, q, d, table, recordID, fields)
}

// DeleteRecord drops every original text of a deleted record.
func DeleteRecord(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string) error {
	return deleteWhere(ctx, q, `table_name = `+d.Placeholder(1)+` AND record_id = `+d.Placeholder(2), table, recordID)
}

// DeleteRecords drops every original text of several deleted records of a
// table, in one statement per batch of ids.
func DeleteRecords(ctx context.Context, q Execer, d dbal.Dialect, table string, recordIDs []string) error {
	for start := 0; start < len(recordIDs); start += deleteBatch {
		ids := recordIDs[start:min(start+deleteBatch, len(recordIDs))]
		args := make([]any, 0, len(ids)+1)
		args = append(args, table)
		for _, id := range ids {
			args = append(args, id)
		}
		where := `table_name = ` + d.Placeholder(1) + ` AND record_id IN (` + placeholders(d, 2, len(ids)) + `)`
		if err := deleteWhere(ctx, q, where, args...); err != nil {
			return err
		}
	}
	return nil
}

// deleteBatch is the most record ids one statement of DeleteRecords names.
const deleteBatch = 500

// DeleteField drops the original texts of a deleted field.
func DeleteField(ctx context.Context, q Execer, d dbal.Dialect, table, field string) error {
	return deleteWhere(ctx, q, `table_name = `+d.Placeholder(1)+` AND field_name = `+d.Placeholder(2), table, field)
}

// DeleteTable drops the original texts of a deleted or emptied table.
func DeleteTable(ctx context.Context, q Execer, d dbal.Dialect, table string) error {
	return deleteWhere(ctx, q, `table_name = `+d.Placeholder(1), table)
}

func deleteWhere(ctx context.Context, q Execer, where string, args ...any) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM `+Table+` WHERE `+where, args...); err != nil && !dbal.IsUndefinedTable(err) {
		return fmt.Errorf("failed dropping original texts: %w", err)
	}
	return nil
}

// FromRecord removes Key from a record being written and returns the
// original texts it carried. A record written through the data API never sets
// them: only a migration or a restored data file does, through this.
//
// Each text must name a field of the record's table (fields), be a string and
// be at most MaxTextBytes long.
func FromRecord(record map[string]interface{}, fields map[string]dbal.AgnosticFieldType) (map[string]string, error) {
	raw, ok := record[Key]
	if !ok {
		return nil, nil
	}
	delete(record, Key)
	if raw == nil {
		return nil, nil
	}
	out := map[string]string{}
	add := func(field string, value interface{}) error {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("the original text of field '%s' is not text", field)
		}
		if _, known := fields[field]; !known {
			return fmt.Errorf("an original text names '%s', which is not a field of the table", field)
		}
		if len(text) > MaxTextBytes {
			return fmt.Errorf("the original text of field '%s' is longer than %d bytes", field, MaxTextBytes)
		}
		out[field] = text
		return nil
	}
	switch m := raw.(type) {
	case map[string]string:
		for k, v := range m {
			if err := add(k, v); err != nil {
				return nil, err
			}
		}
	case map[string]interface{}:
		for k, v := range m {
			if err := add(k, v); err != nil {
				return nil, err
			}
		}
	case map[interface{}]interface{}:
		for k, v := range m {
			field, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("an original text is keyed by something other than a field name")
			}
			if err := add(field, v); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("%s must map field names to text", Key)
	}
	return out, nil
}

// Strip removes Key from a record sent through the data API. The original
// text of a value is the source's, not something a client sets: a client
// writes values, and writing one replaces its original text.
func Strip(record map[string]interface{}) {
	delete(record, Key)
}
