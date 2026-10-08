package data

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/google/uuid"
)

// ErrImport is returned when an import cannot be carried out. Nothing is
// written when it is: an import is one transaction, so the first row that
// cannot be read takes the whole file with it (#38).
var ErrImport = errors.New("import failed")

// ImportAction is what an import does with the rows it read.
type ImportAction string

const (
	// ImportAdd makes every row a new record.
	ImportAdd ImportAction = "add"

	// ImportUpdateMatching finds the record whose match fields hold the same
	// values and updates it. Rows matching nothing are added or skipped
	// according to AddUnmatched.
	ImportUpdateMatching ImportAction = "update_matching"
)

// DateOrder says how a date like 03/04/2011 is to be read. There is no way to
// tell from the value itself, so the caller says rather than the import
// guessing and being wrong half the time.
type DateOrder string

const (
	DateISO DateOrder = "iso" // 2011-04-03, and 2011/04/03
	DateDMY DateOrder = "dmy" // 3 April 2011
	DateMDY DateOrder = "mdy" // 4 March 2011
)

// ImportMapping sends one column of the source to one field of the table. A
// column with no field is skipped.
type ImportMapping struct {
	Column int    `json:"column"`
	Field  string `json:"field"`
}

// ImportOptions is everything an import was told to do.
type ImportOptions struct {
	Action   ImportAction    `json:"action"`
	Mappings []ImportMapping `json:"mappings"`

	// ImportUpdateMatching: the fields a row is matched on.
	MatchFields []string `json:"match_fields"`

	// ImportUpdateMatching: add the rows that match no record, rather than
	// passing over them.
	AddUnmatched bool `json:"add_unmatched"`

	DateOrder DateOrder `json:"date_order"`
}

// ImportReport says what an import did.
type ImportReport struct {
	Rows    int `json:"rows"`
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`

	// Fields that were written to, in the order they were mapped, so the
	// report can name them.
	Fields []string `json:"fields"`
}

// importError names where a value went wrong, because "that will not convert"
// is no use without the row and the column it came from.
func importError(row int, column, value, why string) error {
	if value == "" {
		return fmt.Errorf("%w: row %d, %s: %s", ErrImport, row, column, why)
	}
	return fmt.Errorf("%w: row %d, %s: %q %s", ErrImport, row, column, value, why)
}

// decimalSeparators works out which of . and , is the decimal point in a
// number written with both, by which one comes last.
func normalizeNumber(text string) (string, bool) {
	t := strings.TrimSpace(text)
	t = strings.ReplaceAll(t, " ", "")
	t = strings.ReplaceAll(t, " ", "") // a non-breaking space groups digits too
	if t == "" {
		return "", true
	}

	lastDot := strings.LastIndex(t, ".")
	lastComma := strings.LastIndex(t, ",")
	switch {
	case lastDot >= 0 && lastComma >= 0:
		// Whichever is last is the decimal point; the other groups digits.
		if lastDot > lastComma {
			t = strings.ReplaceAll(t, ",", "")
		} else {
			t = strings.ReplaceAll(t, ".", "")
			t = strings.Replace(t, ",", ".", 1)
		}
	case lastComma >= 0:
		// A lone comma is a decimal point when it has one to three digits
		// after it and appears once; otherwise it groups.
		if strings.Count(t, ",") == 1 && len(t)-lastComma-1 != 3 {
			t = strings.Replace(t, ",", ".", 1)
		} else {
			t = strings.ReplaceAll(t, ",", "")
		}
	}

	if _, err := strconv.ParseFloat(t, 64); err != nil {
		return "", false
	}
	return t, true
}

var dateLayouts = map[DateOrder][]string{
	DateISO: {"2006-01-02", "2006/01/02"},
	DateDMY: {"2006-01-02", "02/01/2006", "02-01-2006", "02.01.2006"},
	DateMDY: {"2006-01-02", "01/02/2006", "01-02-2006"},
}

var timestampLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// coerce turns one cell of text into the value its field holds.
//
// An empty cell is nothing (SQL NULL), never a zero or a blank date. Anything
// that will not convert is an error naming where it came from, rather than a
// silently wrong number.
func coerce(fieldType dbal.AgnosticFieldType, text string, order DateOrder) (interface{}, error) {
	trimmed := strings.TrimSpace(text)

	switch fieldType {
	case dbal.FieldTypeNumber:
		if trimmed == "" {
			return nil, nil
		}
		number, ok := normalizeNumber(trimmed)
		if !ok {
			return nil, errors.New("is not a number")
		}
		return number, nil

	case dbal.FieldTypeBoolean:
		if trimmed == "" {
			return nil, nil
		}
		switch strings.ToLower(trimmed) {
		case "true", "yes", "y", "1", "t":
			return true, nil
		case "false", "no", "n", "0", "f":
			return false, nil
		}
		return nil, errors.New("is not a yes or a no")

	case dbal.FieldTypeDate:
		if trimmed == "" {
			return nil, nil
		}
		layouts := dateLayouts[order]
		if layouts == nil {
			layouts = dateLayouts[DateISO]
		}
		for _, layout := range layouts {
			if at, err := time.Parse(layout, trimmed); err == nil {
				return at.Format("2006-01-02"), nil
			}
		}
		return nil, fmt.Errorf("is not a date File4Base can read in %s order", order)

	case dbal.FieldTypeTimestamp:
		if trimmed == "" {
			return nil, nil
		}
		for _, layout := range timestampLayouts {
			if at, err := time.Parse(layout, trimmed); err == nil {
				return at.Format(time.RFC3339), nil
			}
		}
		return nil, errors.New("is not a date and time")

	case dbal.FieldTypeContainer:
		if trimmed == "" {
			return nil, nil
		}
		if _, err := base64.StdEncoding.DecodeString(trimmed); err != nil {
			return nil, errors.New("is not base64, which is how a container value is written")
		}
		return trimmed, nil

	default:
		// Text keeps whatever it was given, spaces and all: trimming someone's
		// data is not the import's business.
		if text == "" {
			return nil, nil
		}
		return text, nil
	}
}

// checkImportOptions makes sure an import can be carried out before any of it
// is, so a bad mapping is reported rather than half-applied.
func (s *Service) checkImportOptions(
	fields fieldSet, tableName string, source *dataio.Table, opts ImportOptions,
) ([]string, error) {
	if opts.Action != ImportAdd && opts.Action != ImportUpdateMatching {
		return nil, fmt.Errorf("%w: %q is not something an import can do", ErrImport, opts.Action)
	}

	written := make([]string, 0, len(opts.Mappings))
	seen := map[string]bool{}
	for _, mapping := range opts.Mappings {
		field := strings.TrimSpace(mapping.Field)
		if field == "" {
			continue // a column nobody wants
		}
		if mapping.Column < 0 || mapping.Column >= len(source.Columns) {
			return nil, fmt.Errorf("%w: the file has no column %d", ErrImport, mapping.Column+1)
		}
		fieldType, known := fields[field]
		if !known {
			return nil, fmt.Errorf("%w: %s has no field named %q", ErrImport, tableName, field)
		}
		if seen[field] {
			return nil, fmt.Errorf("%w: two columns were sent to %q", ErrImport, field)
		}
		// A calculation and a summary are worked out, not written. Sending a
		// column to one is a mistake worth reporting rather than ignoring.
		if fieldType == dbal.FieldTypeSummary {
			return nil, fmt.Errorf("%w: %q is a summary field, which has no value in a record",
				ErrImport, field)
		}
		seen[field] = true
		written = append(written, field)
	}

	if len(written) == 0 {
		return nil, fmt.Errorf("%w: no column was sent to a field", ErrImport)
	}

	if opts.Action == ImportUpdateMatching {
		if len(opts.MatchFields) == 0 {
			return nil, fmt.Errorf("%w: updating needs a field to match records on", ErrImport)
		}
		for _, field := range opts.MatchFields {
			if err := checkField(fields, tableName, field); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrImport, err)
			}
			if !seen[field] {
				return nil, fmt.Errorf("%w: records are matched on %q, so a column has to be sent to it",
					ErrImport, field)
			}
		}
	}

	return written, nil
}

// ImportRecords brings the rows of a parsed source into a table (#38).
//
// It is one transaction: the first row that cannot be read or stored takes the
// whole file with it, and says which row it was. The field validation rules
// apply, as they do to any other write.
func (s *Service) ImportRecords(
	ctx context.Context, tableName string, source *dataio.Table, opts ImportOptions,
) (*ImportReport, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	written, err := s.checkImportOptions(fields, tableName, source, opts)
	if err != nil {
		return nil, err
	}
	if opts.DateOrder == "" {
		opts.DateOrder = DateISO
	}

	// The calculations are read once, outside the transaction, and applied to
	// each row as it is built (#30).
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return nil, err
	}
	calculated := map[string]bool{}
	for _, c := range calculations {
		calculated[c.field] = true
	}
	for _, field := range written {
		if calculated[field] {
			return nil, fmt.Errorf("%w: %q is a calculation field, and its formula owns its value",
				ErrImport, field)
		}
	}

	dialect := s.driver.Dialect()
	db := s.driver.DB()
	report := &ImportReport{Rows: len(source.Rows), Fields: written}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rules, err := validation.LoadFields(ctx, tx, dialect, tableName)
	if err != nil {
		return nil, err
	}
	checker := &validation.Checker{Dialect: dialect, DB: tx, Table: tableName, Fields: rules}

	for i := range source.Rows {
		rowNumber := i + 1
		record := make(map[string]interface{}, len(written))

		for _, mapping := range opts.Mappings {
			field := strings.TrimSpace(mapping.Field)
			if field == "" {
				continue
			}
			value, err := coerce(fields[field], source.Cell(i, mapping.Column), opts.DateOrder)
			if err != nil {
				column := source.Columns[mapping.Column]
				return nil, importError(rowNumber, column, source.Cell(i, mapping.Column), err.Error())
			}
			record[field] = value
		}

		if err := applyCalculationsTo(calculations, record); err != nil {
			return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}

		if opts.Action == ImportUpdateMatching {
			id, err := findMatchingRecord(ctx, tx, dialect, tableName, opts.MatchFields, record)
			if err != nil {
				return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
			}
			if id != "" {
				if err := checker.Check(ctx, id, record, nil, false); err != nil {
					return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
				}
				if err := updateInTx(ctx, tx, dialect, tableName, id, record); err != nil {
					return nil, fmt.Errorf("%w: row %d could not be updated: %v", ErrImport, rowNumber, err)
				}
				report.Updated++
				continue
			}
			if !opts.AddUnmatched {
				report.Skipped++
				continue
			}
		}

		id := uuid.NewString()
		record["id"] = id
		if err := checker.Check(ctx, id, record, nil, false); err != nil {
			return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		if err := insertInTx(ctx, tx, dialect, tableName, record); err != nil {
			return nil, fmt.Errorf("%w: row %d could not be added: %v", ErrImport, rowNumber, err)
		}
		report.Added++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("%w: nothing was imported: %v", ErrImport, err)
	}
	return report, nil
}

// execer is a transaction, or anything else that can run the statements an
// import needs.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// findMatchingRecord returns the id of the record whose match fields hold the
// same values as the row, or "" when none does.
func findMatchingRecord(
	ctx context.Context, tx execer, dialect dbal.Dialect,
	tableName string, matchFields []string, record map[string]interface{},
) (string, error) {
	clauses := make([]string, 0, len(matchFields))
	values := make([]interface{}, 0, len(matchFields))
	for i, field := range matchFields {
		value := record[field]
		if value == nil || fmt.Sprint(value) == "" {
			// An empty key matches nothing, the same rule a relationship
			// follows (#36).
			return "", nil
		}
		clauses = append(clauses, fmt.Sprintf("LOWER(%s) = LOWER(%s)",
			dialect.CastToText(dialect.QuoteIdentifier(field)), dialect.Placeholder(i+1)))
		values = append(values, fmt.Sprint(value))
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s LIMIT 1",
		dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier(tableName),
		strings.Join(clauses, " AND "))

	var id string
	err := tx.QueryRowContext(ctx, query, values...).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func insertInTx(
	ctx context.Context, tx execer, dialect dbal.Dialect,
	tableName string, record map[string]interface{},
) error {
	columns := make([]string, 0, len(record))
	placeholders := make([]string, 0, len(record))
	values := make([]interface{}, 0, len(record))
	i := 1
	for field, value := range record {
		columns = append(columns, dialect.QuoteIdentifier(field))
		placeholders = append(placeholders, dialect.Placeholder(i))
		values = append(values, value)
		i++
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		dialect.QuoteIdentifier(tableName),
		strings.Join(columns, ", "), strings.Join(placeholders, ", "))
	_, err := tx.ExecContext(ctx, query, values...)
	return err
}

func updateInTx(
	ctx context.Context, tx execer, dialect dbal.Dialect,
	tableName, id string, record map[string]interface{},
) error {
	sets := make([]string, 0, len(record))
	values := make([]interface{}, 0, len(record)+1)
	i := 1
	for field, value := range record {
		if field == "id" {
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = %s", dialect.QuoteIdentifier(field), dialect.Placeholder(i)))
		values = append(values, value)
		i++
	}
	if len(sets) == 0 {
		return nil
	}
	values = append(values, id)
	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = %s",
		dialect.QuoteIdentifier(tableName), strings.Join(sets, ", "),
		dialect.QuoteIdentifier("id"), dialect.Placeholder(i))
	_, err := tx.ExecContext(ctx, query, values...)
	return err
}

// ExportOptions says what to export.
type ExportOptions struct {
	// The found set, in the same form a find takes. Empty exports the table.
	Requests []FindRequest `json:"requests"`

	// The fields to write, in the order the columns appear. Empty writes
	// every field of the table, by name.
	Fields []string `json:"fields"`

	// Column headings, one per field. Empty uses the field names.
	Headings []string `json:"headings"`

	Sort []SortField `json:"sort"`
}

// ExportRecords reads a found set out as a table of text, ready to be written
// as CSV, TSV or a workbook (#38).
//
// Everything is text, written the way the field reads: a DATE comes out as a
// date rather than the timestamp the column holds.
func (s *Service) ExportRecords(
	ctx context.Context, tableName string, opts ExportOptions,
) (*dataio.Table, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}

	chosen := opts.Fields
	if len(chosen) == 0 {
		chosen = make([]string, 0, len(fields))
		for name := range fields {
			chosen = append(chosen, name)
		}
		sort.Strings(chosen)
	}
	for _, field := range chosen {
		if err := checkField(fields, tableName, field); err != nil {
			return nil, err
		}
	}

	headings := opts.Headings
	if len(headings) != len(chosen) {
		headings = chosen
	}

	rows, err := s.exportRows(ctx, tableName, opts)
	if err != nil {
		return nil, err
	}

	table := &dataio.Table{
		Columns: append([]string(nil), headings...),
		Rows:    make([][]string, 0, len(rows)),
	}
	for _, row := range rows {
		cells := make([]string, len(chosen))
		for i, field := range chosen {
			cells[i] = exportText(fields[field], row[field])
		}
		table.Rows = append(table.Rows, cells)
	}
	return table, nil
}

// exportRows reads the whole found set, page by page, so an export is the
// found set rather than the first page of it.
func (s *Service) exportRows(
	ctx context.Context, tableName string, opts ExportOptions,
) ([]map[string]interface{}, error) {
	all := make([]map[string]interface{}, 0, 256)
	for {
		query := QueryOptions{Limit: MaxPageSize, Offset: len(all), Sort: opts.Sort}
		if len(query.Sort) == 0 {
			query.SortBy, query.SortAsc = "id", true // a stable order to page in
		}

		var page []map[string]interface{}
		var err error
		if len(opts.Requests) == 0 {
			page, err = s.ListRows(ctx, tableName, query)
		} else {
			page, err = s.ExecuteFind(ctx, tableName, opts.Requests, query)
		}
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < MaxPageSize {
			return all, nil
		}
	}
}

// exportText writes a stored value the way its field reads.
//
// A date and a timestamp arrive from the driver as a time.Time on PostgreSQL
// and as text on MariaDB, so both are written out the same way rather than one
// of them coming out as Go's default time format.
func exportText(fieldType dbal.AgnosticFieldType, value interface{}) string {
	if value == nil {
		return ""
	}
	switch fieldType {
	case dbal.FieldTypeDate:
		if at, ok := value.(time.Time); ok {
			return at.Format("2006-01-02")
		}
		text := fmt.Sprint(value)
		// A DATE comes back as a full timestamp; a column of dates should
		// hold dates.
		if len(text) > 10 && text[10] == 'T' {
			return text[:10]
		}
		return text
	case dbal.FieldTypeTimestamp:
		if at, ok := value.(time.Time); ok {
			return at.Format(time.RFC3339)
		}
		return fmt.Sprint(value)
	case dbal.FieldTypeBoolean:
		switch v := dbal.NormalizeBool(value).(type) {
		case bool:
			if v {
				return "true"
			}
			return "false"
		}
		return fmt.Sprint(value)
	default:
		return fmt.Sprint(value)
	}
}

// CoerceForTest exposes the value conversion so it can be tested on its own.
// It is the one piece of an import with no database in it, and the one most
// worth pinning down.
func CoerceForTest(fieldType dbal.AgnosticFieldType, text string, order DateOrder) (interface{}, error) {
	return coerce(fieldType, text, order)
}
