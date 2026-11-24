// Package containermeta keeps file metadata (original filename, MIME type,
// SHA-256 digest and byte length) alongside binary container fields (#125).
//
// File4Base stores container payloads directly in BYTEA / LONGBLOB columns.
// This package keeps the container's file metadata in one system table per
// database keyed by table, record id and field, attached under the reserved
// record key Key ("__container_meta") on Data API reads and carried in .f4data
// bundles. Writing a new value to a container field clears stale metadata, and
// deleting a record, field or table removes its entries.
package containermeta

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
)

const (
	// Key is the reserved record key under which a record carries metadata for
	// its container fields, as a map of field name to Entry.
	Key = "__container_meta"

	// Table is the system table storing container file metadata.
	Table = "sys_container_meta"

	loadChunk = 500
)

// Entry describes one reconstructed or stored container file.
type Entry struct {
	Filename  string `json:"filename" msgpack:"filename"`
	MimeType  string `json:"mime_type" msgpack:"mime_type"`
	SHA256    string `json:"sha256" msgpack:"sha256"`
	SizeBytes int64  `json:"size_bytes" msgpack:"size_bytes"`
}

// CreateTableSQL creates the system table. It works identically on PostgreSQL and MariaDB.
const CreateTableSQL = `CREATE TABLE IF NOT EXISTS ` + Table + ` (
			table_name VARCHAR(64) NOT NULL,
			record_id VARCHAR(64) NOT NULL,
			field_name VARCHAR(64) NOT NULL,
			filename TEXT NOT NULL,
			mime_type VARCHAR(128) NOT NULL,
			sha256 VARCHAR(64) NOT NULL,
			size_bytes BIGINT NOT NULL,
			PRIMARY KEY (table_name, record_id, field_name)
		);`

// Execer executes SQL statements (*sql.DB or *sql.Tx).
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// QueryExecer executes statements and queries (*sql.DB or *sql.Tx).
type QueryExecer interface {
	Execer
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Records maps record_id -> field_name -> Entry.
type Records map[string]map[string]Entry

// Ensure creates sys_container_meta if missing.
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

// Put stores container metadata entries for one record, replacing any previous
// entries for those fields. The table must exist (see Ensure).
func Put(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string, entries map[string]Entry) error {
	if len(entries) == 0 {
		return nil
	}
	fields := make([]string, 0, len(entries))
	for f := range entries {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	if err := clearFields(ctx, q, d, table, recordID, fields); err != nil {
		return err
	}
	insert := fmt.Sprintf(`INSERT INTO %s (table_name, record_id, field_name, filename, mime_type, sha256, size_bytes) VALUES (%s)`,
		Table, placeholders(d, 1, 7))
	for _, field := range fields {
		e := entries[field]
		if _, err := q.ExecContext(ctx, insert, table, recordID, field, e.Filename, e.MimeType, e.SHA256, e.SizeBytes); err != nil {
			return fmt.Errorf("failed storing container metadata for %s.%s: %w", table, field, err)
		}
	}
	return nil
}

// Load returns container metadata for the given record IDs of a table.
func Load(ctx context.Context, q QueryExecer, d dbal.Dialect, table string, recordIDs []string) (Records, error) {
	out := Records{}
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
		query := fmt.Sprintf(
			`SELECT record_id, field_name, filename, mime_type, sha256, size_bytes FROM %s WHERE table_name = %s AND record_id IN (%s)`,
			Table, d.Placeholder(1), placeholders(d, 2, len(ids)),
		)
		if err := scanInto(ctx, q, query, args, out); err != nil {
			if isMissingTable(err) {
				return Records{}, nil
			}
			return nil, err
		}
	}
	return out, nil
}

// LoadTable returns container metadata for every record of a table.
func LoadTable(ctx context.Context, q QueryExecer, d dbal.Dialect, table string) (Records, error) {
	out := Records{}
	query := fmt.Sprintf(
		`SELECT record_id, field_name, filename, mime_type, sha256, size_bytes FROM %s WHERE table_name = %s`,
		Table, d.Placeholder(1),
	)
	if err := scanInto(ctx, q, query, []any{table}, out); err != nil {
		if isMissingTable(err) {
			return Records{}, nil
		}
		return nil, err
	}
	return out, nil
}

func scanInto(ctx context.Context, q QueryExecer, query string, args []any, out Records) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var recID, field string
		var e Entry
		if err := rows.Scan(&recID, &field, &e.Filename, &e.MimeType, &e.SHA256, &e.SizeBytes); err != nil {
			return err
		}
		if out[recID] == nil {
			out[recID] = map[string]Entry{}
		}
		out[recID][field] = e
	}
	return rows.Err()
}

// Attach writes loaded metadata onto each matching row under Key.
func Attach(rows []map[string]interface{}, records Records) {
	if len(records) == 0 {
		return
	}
	for _, row := range rows {
		id, ok := row["id"]
		if !ok || id == nil {
			continue
		}
		if entries := records[fmt.Sprint(id)]; len(entries) > 0 {
			copied := make(map[string]Entry, len(entries))
			for k, v := range entries {
				copied[k] = v
			}
			row[Key] = copied
		}
	}
}

// AttachFromDB loads and attaches container metadata for the given rows.
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
	if len(ids) == 0 {
		return nil
	}
	recs, err := Load(ctx, q, d, table, ids)
	if err != nil {
		return err
	}
	Attach(rows, recs)
	return nil
}

// Strip removes Key from a client-supplied record map so clients cannot forge
// container metadata directly.
func Strip(record map[string]interface{}) {
	delete(record, Key)
}

// FromRecord extracts and removes Key from a record map (such as a migration
// row or .f4data bundle row), validating that every named field is a CONTAINER
// column of the table.
func FromRecord(row map[string]interface{}, cols map[string]dbal.AgnosticFieldType) (map[string]Entry, error) {
	raw, exists := row[Key]
	if !exists || raw == nil {
		delete(row, Key)
		return nil, nil
	}
	delete(row, Key)

	out := map[string]Entry{}
	switch typed := raw.(type) {
	case map[string]Entry:
		for field, entry := range typed {
			if err := validateField(field, cols); err != nil {
				return nil, err
			}
			out[field] = entry
		}
	case map[string]interface{}:
		for field, v := range typed {
			if err := validateField(field, cols); err != nil {
				return nil, err
			}
			entry, err := decodeEntry(v)
			if err != nil {
				return nil, fmt.Errorf("invalid %s for field %q: %w", Key, field, err)
			}
			out[field] = entry
		}
	default:
		return nil, fmt.Errorf("%s must be a map of container metadata entries", Key)
	}
	return out, nil
}

func validateField(field string, cols map[string]dbal.AgnosticFieldType) error {
	ft, ok := cols[field]
	if !ok {
		// A repetition of a repeating container field, "field[n]", is
		// described under that key.
		if base, _, rep := repetition.Split(field); rep {
			ft, ok = cols[base]
		}
	}
	if !ok {
		return fmt.Errorf("%s names unknown field %q", Key, field)
	}
	if ft != dbal.FieldTypeContainer {
		return fmt.Errorf("%s names non-container field %q (%s)", Key, field, ft)
	}
	return nil
}

func decodeEntry(v interface{}) (Entry, error) {
	if e, ok := v.(Entry); ok {
		return e, nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return Entry{}, fmt.Errorf("expected object")
	}
	var e Entry
	if s, ok := m["filename"].(string); ok {
		e.Filename = s
	}
	if s, ok := m["mime_type"].(string); ok {
		e.MimeType = s
	}
	if s, ok := m["sha256"].(string); ok {
		e.SHA256 = s
	}
	switch n := m["size_bytes"].(type) {
	case int64:
		e.SizeBytes = n
	case int:
		e.SizeBytes = int64(n)
	case uint64:
		e.SizeBytes = int64(n)
	case uint32:
		e.SizeBytes = int64(n)
	case int32:
		e.SizeBytes = int64(n)
	case int16:
		e.SizeBytes = int64(n)
	case int8:
		e.SizeBytes = int64(n)
	case uint16:
		e.SizeBytes = int64(n)
	case uint8:
		e.SizeBytes = int64(n)
	case float64:
		e.SizeBytes = int64(n)
	}
	return e, nil
}

// ClearFields deletes stored container metadata for the given fields of a record.
func ClearFields(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string, fields []string) error {
	if len(fields) == 0 {
		return nil
	}
	if err := clearFields(ctx, q, d, table, recordID, fields); err != nil && !isMissingTable(err) {
		return err
	}
	return nil
}

func clearFields(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string, fields []string) error {
	args := make([]any, 0, len(fields)+2)
	args = append(args, table, recordID)
	for _, f := range fields {
		args = append(args, f)
	}
	query := fmt.Sprintf(`DELETE FROM %s WHERE table_name = %s AND record_id = %s AND field_name IN (%s)`,
		Table, d.Placeholder(1), d.Placeholder(2), placeholders(d, 3, len(fields)))
	_, err := q.ExecContext(ctx, query, args...)
	return err
}

// DeleteRecord removes all container metadata for one record.
func DeleteRecord(ctx context.Context, q Execer, d dbal.Dialect, table, recordID string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE table_name = %s AND record_id = %s`,
		Table, d.Placeholder(1), d.Placeholder(2))
	if _, err := q.ExecContext(ctx, query, table, recordID); err != nil && !isMissingTable(err) {
		return err
	}
	return nil
}

// DeleteField removes all container metadata for one field of a table.
func DeleteField(ctx context.Context, q Execer, d dbal.Dialect, table, field string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE table_name = %s AND field_name = %s`,
		Table, d.Placeholder(1), d.Placeholder(2))
	if _, err := q.ExecContext(ctx, query, table, field); err != nil && !isMissingTable(err) {
		return err
	}
	return nil
}

// DeleteTable removes all container metadata for a table.
func DeleteTable(ctx context.Context, q Execer, d dbal.Dialect, table string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE table_name = %s`, Table, d.Placeholder(1))
	if _, err := q.ExecContext(ctx, query, table); err != nil && !isMissingTable(err) {
		return err
	}
	return nil
}

func isMissingTable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, Table) && (strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "doesn't exist") ||
		strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "undefined table"))
}
