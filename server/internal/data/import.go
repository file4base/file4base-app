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
	"github.com/file4base/file4base-app/server/internal/repetition"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
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

	// ImportReplaceFoundSet is FileMaker's "Replace field contents in found
	// set, in order": the first row replaces the mapped fields of the first
	// record of the found set, the second of the second, and so on. Rows
	// left over once the found set runs out are added or skipped according
	// to AddUnmatched; records left over keep what they hold (#191).
	ImportReplaceFoundSet ImportAction = "replace_found_set"
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
	// passing over them. ImportReplaceFoundSet: add the rows left over once
	// the found set runs out.
	AddUnmatched bool `json:"add_unmatched"`

	// FoundSet is the records of the table the import works on, in the
	// order they are in: the ones ImportReplaceFoundSet replaces (it is
	// required), and the only ones ImportUpdateMatching matches, every
	// matching one being updated as FileMaker updates them. Nil, an update
	// updates the first record of the table that matches. An id no longer
	// in the table keeps its place, and its row is passed over.
	FoundSet []string `json:"found_set,omitempty"`

	// ReturnRecordIDs lists in the report the records the import added or
	// updated, which FileMaker makes the found set once it is done.
	ReturnRecordIDs bool `json:"return_record_ids,omitempty"`

	// PassOver are rows, by 0-based index, that are not imported but still
	// take their place: ImportReplaceFoundSet leaves the record they would
	// have replaced as it is. A transfer passes over the rows it rejected.
	PassOver map[int]bool `json:"-"`

	DateOrder DateOrder `json:"date_order"`

	// PerformAutoEnter applies the fields' auto-enter options, as
	// FileMaker's "Perform auto-enter options while importing": the values
	// they enter replace the imported ones, except a calculated value that
	// does not replace an imported value. Off, the imported values are
	// written as they are (see docs/specs/auto_enter_options.md).
	PerformAutoEnter bool `json:"perform_auto_enter"`

	// AutoEnterFields, when given, are the only fields whose auto-enter
	// options PerformAutoEnter applies, as FileMaker 2023 and later choose
	// them field by field in the import field map.
	AutoEnterFields []string `json:"auto_enter_fields,omitempty"`
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

	// RecordIDs are the records added or updated, in the order the rows
	// reached them, when ReturnRecordIDs asked for them.
	RecordIDs []string `json:"record_ids,omitempty"`
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
		// A container column holds the bytes themselves, so the base64 the
		// cell is written in is decoded here. Storing the base64 text
		// instead wrote the characters "aGVsbG8=" into the column, and
		// reading the record back encoded those again, so an imported
		// container came out doubly encoded and no longer the file that went
		// in (#96).
		decoded, err := base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			return nil, errors.New("is not base64, which is how a container value is written")
		}
		return decoded, nil

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
	switch opts.Action {
	case ImportAdd, ImportUpdateMatching, ImportReplaceFoundSet:
	default:
		return nil, fmt.Errorf("%w: %q is not something an import can do", ErrImport, opts.Action)
	}
	if opts.Action == ImportReplaceFoundSet && len(opts.MatchFields) > 0 {
		return nil, fmt.Errorf("%w: replacing the found set in order matches records by position, not on fields",
			ErrImport)
	}
	if opts.Action == ImportReplaceFoundSet && opts.FoundSet == nil {
		// Never the whole table by default: what is overwritten is what
		// the found set holds, and nobody said what it holds.
		return nil, fmt.Errorf("%w: replacing field contents works on a found set, and none was given", ErrImport)
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
		if !known || fields.isInternal(field) {
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

	// The identities of the records added are known to the formulas computed
	// for them (see record_identity.go).
	ctx, scope := withIdentityScope(ctx)

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

	var ae *autoEnter
	if opts.PerformAutoEnter {
		if ae, err = s.loadAutoEnter(ctx, tableName, fields); err != nil {
			return nil, err
		}
		if opts.AutoEnterFields != nil {
			ae.only(opts.AutoEnterFields)
		}
		if !ae.active() {
			ae = nil
		} else {
			ae.withCalculations(calculations)
		}
	}

	// An updated record is modified when the row sets a field stored with it,
	// which counts the modification. Replacing updates records as matching
	// does.
	updates := opts.Action == ImportUpdateMatching || opts.Action == ImportReplaceFoundSet
	var modifying map[string]bool
	countsModifications, numbered := false, false
	if updates {
		if countsModifications, err = HasModCount(ctx, s.driver, tableName); err != nil {
			return nil, err
		}
		if countsModifications {
			if modifying, err = s.modifyingFields(ctx, tableName); err != nil {
				return nil, err
			}
			if numbered, err = HasCreationOrder(ctx, s.driver, tableName); err != nil {
				return nil, err
			}
		}
	}
	// The record IDs of the records added are taken outside the transaction,
	// as a failed import spends them: before it when every row is added, one
	// by one for the rows an update does not match or a replace has no record
	// left for.
	var recordIDs []int64
	if !updates {
		if recordIDs, err = ReserveRecordIDs(ctx, s.driver, tableName, len(source.Rows)); err != nil {
			return nil, err
		}
	}
	nextRecordID := func() (int64, error) {
		if len(recordIDs) == 0 {
			if !updates {
				return 0, nil // a table not numbered yet
			}
			ids, err := ReserveRecordIDs(ctx, s.driver, tableName, 1)
			if err != nil || len(ids) == 0 {
				return 0, err
			}
			recordIDs = ids
		}
		next := recordIDs[0]
		recordIDs = recordIDs[1:]
		return next, nil
	}

	dialect := s.driver.Dialect()
	db := s.driver.DB()
	report := &ImportReport{Rows: len(source.Rows), Fields: written}
	// An updated record loses the original texts of the fields it is given
	// (#124); the table they are kept in has to exist before the transaction.
	if opts.Action != ImportAdd {
		if err := sourcetext.Ensure(ctx, db); err != nil {
			return nil, err
		}
	}
	usesFoundSet := opts.Action != ImportAdd && (opts.FoundSet != nil || opts.Action == ImportReplaceFoundSet)
	// The creation order is resolved outside the transaction, which holds
	// a connection of its own.
	var order string
	if usesFoundSet {
		if order, err = s.creationOrder(ctx, tableName); err != nil {
			return nil, err
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rules, err := validation.LoadFields(ctx, tx, dialect, tableName)
	if err != nil {
		return nil, err
	}
	checker := &validation.Checker{Dialect: dialect, DB: tx, Table: tableName, Fields: rules,
		Calculate: s.validationCalculator(tableName)}

	// The found set is read in the transaction, so a record deleted since
	// it was taken is passed over rather than written to.
	var foundSet []string
	var inFoundSet map[string]bool
	if usesFoundSet {
		if foundSet, err = s.importFoundSet(ctx, tx, tableName, order, opts.FoundSet); err != nil {
			return nil, err
		}
		if opts.FoundSet != nil {
			inFoundSet = make(map[string]bool, len(foundSet))
			for _, id := range foundSet {
				if id != "" {
					inFoundSet[id] = true
				}
			}
		}
	}
	reached := map[string]bool{}
	reach := func(id string) {
		if opts.ReturnRecordIDs && !reached[id] {
			reached[id] = true
			report.RecordIDs = append(report.RecordIDs, id)
		}
	}

	// update writes the mapped values of a row over a stored record. The
	// record is the row's own: each record a row updates gets a copy.
	update := func(rowNumber int, id string, record map[string]interface{}) error {
		var current map[string]interface{}
		var counted modification
		var err error
		if countsModifications {
			// Read with its identity, as this transaction sees it: a row
			// updating a record an earlier row updated sees that.
			var read Identity
			var found bool
			if current, read, found, err = storedRowIdentity(ctx, tx, dialect, tableName, id, fields, numbered); err != nil {
				return fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
			}
			counted = modification{counted: true, modifies: setsStoredField(fields, modifying, record),
				modifying: modifying}
			if found && numbered {
				scope.put(db, tableName, id, read) // as before this row's write
			}
		} else if ae != nil {
			if current, err = storedRow(ctx, tx, dialect, tableName, id, fields); err != nil {
				return fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
			}
		}
		if ae != nil {
			if err := ae.enterModified(ctx, autoMode{overwrite: true, tx: tx}, record, current); err != nil {
				return fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
			}
		}
		counted.alsoModifiedBy(fields, record)
		if err := applyCalculationsForUpdate(
			ctx, tx, dialect, tableName, id, fields, calculations, record,
		); err != nil {
			return fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		if err := checker.Check(ctx, id, record, nil, false); err != nil {
			return fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		writes, err := packRepetitions(ctx, tx, dialect, fields, tableName, id, record)
		if err != nil {
			return fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		if err := updateInTx(ctx, tx, dialect, tableName, id, writes, counted); err != nil {
			return fmt.Errorf("%w: row %d could not be updated: %v", ErrImport, rowNumber, err)
		}
		scope.drop(db, tableName, id)
		updatedFields := make([]string, 0, len(record))
		for field := range record {
			updatedFields = append(updatedFields, field)
		}
		if err := sourcetext.ClearFields(ctx, tx, dialect, tableName, id, updatedFields); err != nil {
			return fmt.Errorf("%w: row %d could not be updated: %v", ErrImport, rowNumber, err)
		}
		reach(id)
		return nil
	}

	for i := range source.Rows {
		rowNumber := i + 1
		if opts.PassOver[i] {
			continue
		}
		record := make(map[string]interface{}, len(written))

		for _, mapping := range opts.Mappings {
			field := strings.TrimSpace(mapping.Field)
			if field == "" {
				continue
			}
			cell := source.Cell(i, mapping.Column)
			parts := []string{cell}
			if reps := fields.maxRepetitions(field); reps > 1 {
				parts = splitRepetitionText(cell, reps)
			} else {
				// Several repetitions sent to a field that does not repeat
				// are kept one per line, as File4Base keeps several values
				// in a field (#31).
				parts[0] = strings.ReplaceAll(cell, "\x1d", "\n")
			}
			for n, part := range parts {
				value, err := coerce(fields[field], part, opts.DateOrder)
				if err != nil {
					column := source.Columns[mapping.Column]
					return nil, importError(rowNumber, column, cell, err.Error())
				}
				record[repetition.Key(field, n+1)] = value
			}
		}
		normalizeTemporalValues(fields, record)

		switch opts.Action {
		case ImportUpdateMatching:
			// The match is looked up before the calculations are computed: an
			// update evaluates its formulas over the stored row merged with the
			// mapped values, so a formula reading a field this row does not
			// carry sees what the record holds rather than nothing (#92).
			// A row whose match fields are all empty is neither matched nor
			// added, even with the rows that match nothing added, as
			// FileMaker imports it.
			if allEmpty(record, opts.MatchFields) {
				report.Skipped++
				continue
			}
			var ids []string
			if inFoundSet == nil {
				id, err := findMatchingRecord(ctx, tx, dialect, fields, tableName, opts.MatchFields, record)
				if err != nil {
					return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
				}
				if id != "" {
					ids = []string{id}
				}
			} else {
				matches, err := findMatchingRecords(ctx, tx, dialect, fields, tableName, opts.MatchFields, record)
				if err != nil {
					return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
				}
				// Every match the found set holds is updated, in the found
				// set's order.
				matched := make(map[string]bool, len(matches))
				for _, id := range matches {
					matched[id] = true
				}
				for _, id := range foundSet {
					if id != "" && matched[id] {
						ids = append(ids, id)
					}
				}
			}
			if len(ids) > 0 {
				for _, id := range ids {
					if err := update(rowNumber, id, copyRecord(record)); err != nil {
						return nil, err
					}
				}
				report.Updated += len(ids)
				continue
			}
			if !opts.AddUnmatched {
				report.Skipped++
				continue
			}
		case ImportReplaceFoundSet:
			if i < len(foundSet) {
				if foundSet[i] == "" {
					// The record in this place is gone: its row is passed
					// over, and the next row still goes to the next record.
					report.Skipped++
					continue
				}
				if err := update(rowNumber, foundSet[i], record); err != nil {
					return nil, err
				}
				report.Updated++
				continue
			}
			if !opts.AddUnmatched {
				report.Skipped++
				continue
			}
		}

		// A row that is added carries only its own mapped values, so its
		// formulas are computed over the new record alone. Its id is known
		// first: a container result refers to the record it is in.
		id := uuid.NewString()
		record["id"] = id
		recordID, err := nextRecordID()
		if err != nil {
			return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		if recordID > 0 {
			scope.put(db, tableName, id, Identity{RecordID: recordID})
		}
		if ae != nil {
			if err := ae.enterNew(ctx, autoMode{overwrite: true, tx: tx}, record); err != nil {
				return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
			}
		}
		if err := applyCalculationsTo(calculations, record); err != nil {
			return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		if err := checker.Check(ctx, id, record, nil, false); err != nil {
			return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		writes, err := packRepetitions(ctx, tx, dialect, fields, tableName, "", record)
		if err != nil {
			return nil, fmt.Errorf("%w: row %d: %v", ErrImport, rowNumber, err)
		}
		if err := insertInTx(ctx, tx, dialect, tableName, writes, recordID); err != nil {
			if dbal.IsRecordIDConflict(err) {
				recoverRecordIDs(ctx, s.driver, tableName) // the next import gets free IDs
			}
			return nil, fmt.Errorf("%w: row %d could not be added: %v", ErrImport, rowNumber, err)
		}
		reach(id)
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
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// applyCalculationsForUpdate computes the calculation fields of the record a
// keyed import is about to update. The formulas read the stored row overlaid
// with the mapped values, which is what the row holds once the update lands, so
// an import that touches one field of a formula's inputs keeps the others
// (#92). The answers are written into record, so the update writes them too.
//
// The row is read inside the import transaction, so a row an earlier line of
// the same file wrote is seen as it now stands.
func applyCalculationsForUpdate(
	ctx context.Context, tx execer, dialect dbal.Dialect,
	tableName, id string, fields fieldSet, calculations []calculation,
	record map[string]interface{},
) error {
	if len(calculations) == 0 {
		return nil
	}

	stored, err := storedRow(ctx, tx, dialect, tableName, id, fields)
	if err != nil {
		return err
	}

	merged := make(map[string]interface{}, len(fields)+len(record))
	for field, value := range stored {
		merged[field] = value
	}
	for field, value := range record {
		merged[field] = value
	}

	for _, c := range calculations {
		if c.unstored {
			c.clear(record) // computed when read, never stored
			delete(record, c.field)
			continue
		}
		answers, err := c.evaluate(merged)
		if err != nil {
			return fmt.Errorf("%w: the formula of %q could not be computed: %s",
				ErrInvalidValue, c.field, err.Error())
		}
		for key, answer := range answers {
			merged[key] = answer
			record[key] = answer
		}
	}
	return nil
}

// storedRow reads a record as the transaction sees it: its stored columns,
// with no unstored calculation computed. A record that is not there is an
// empty one.
func storedRow(
	ctx context.Context, tx execer, dialect dbal.Dialect, tableName, id string, fields fieldSet,
) (map[string]interface{}, error) {
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1",
		selectList(dialect, fields), dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"), dialect.Placeholder(1))
	rows, err := tx.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stored, err := rowsToMaps(rows, fields)
	if err != nil || len(stored) == 0 {
		return map[string]interface{}{}, err
	}
	return stored[0], nil
}

func allEmpty(record map[string]interface{}, fields []string) bool {
	for _, field := range fields {
		if value := record[field]; value != nil && fmt.Sprint(value) != "" {
			return false
		}
	}
	return true
}

// matchClauses are the conditions a record's match fields meet when they
// hold the row's values, or false when a key is empty. Text is compared
// whatever its case; a date or a timestamp as the day or the instant it is,
// a number as the number and a boolean as the boolean the column holds,
// since none of their texts is the same on two engines (MariaDB writes a
// number with all its decimals and a boolean as 1).
func matchClauses(
	dialect dbal.Dialect, fields fieldSet, matchFields []string, record map[string]interface{},
) ([]string, []interface{}, bool) {
	clauses := make([]string, 0, len(matchFields))
	values := make([]interface{}, 0, len(matchFields))
	for i, field := range matchFields {
		value := record[field]
		if value == nil || fmt.Sprint(value) == "" {
			return nil, nil, false
		}
		column := dialect.QuoteIdentifier(field)
		switch fields[field] {
		case dbal.FieldTypeDate, dbal.FieldTypeTimestamp:
			clauses = append(clauses, fmt.Sprintf("%s = %s", column, dialect.Placeholder(i+1)))
			values = append(values, dbal.TemporalValue(dialect.Engine(), fields[field], value))
		case dbal.FieldTypeNumber, dbal.FieldTypeBoolean:
			clauses = append(clauses, fmt.Sprintf("%s = %s", column, dialect.Placeholder(i+1)))
			values = append(values, value)
		default:
			clauses = append(clauses, fmt.Sprintf("LOWER(%s) = LOWER(%s)",
				dialect.CastToText(column), dialect.Placeholder(i+1)))
			values = append(values, fmt.Sprint(value))
		}
	}
	return clauses, values, true
}

// findMatchingRecord returns the id of the record whose match fields hold the
// same values as the row, or "" when none does. Text is compared whatever its
// case; a date or a timestamp is compared as the day or the instant it is, as
// the column holds it, since its text is not the same on two engines (and
// not the RFC 3339 an import reads).
func findMatchingRecord(
	ctx context.Context, tx execer, dialect dbal.Dialect, fields fieldSet,
	tableName string, matchFields []string, record map[string]interface{},
) (string, error) {
	clauses, values, ok := matchClauses(dialect, fields, matchFields, record)
	if !ok {
		// An empty key matches nothing, the same rule a relationship
		// follows (#36).
		return "", nil
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

// findMatchingRecords returns every record whose match fields hold the same
// values as the row, in creation order, compared as findMatchingRecord
// compares them.
func findMatchingRecords(
	ctx context.Context, tx execer, dialect dbal.Dialect, fields fieldSet,
	tableName string, matchFields []string, record map[string]interface{},
) ([]string, error) {
	clauses, values, ok := matchClauses(dialect, fields, matchFields, record)
	if !ok {
		return nil, nil // an empty key matches nothing
	}
	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s",
		dialect.QuoteIdentifier("id"), dialect.QuoteIdentifier(tableName),
		strings.Join(clauses, " AND "))
	rows, err := tx.QueryContext(ctx, query, values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// importFoundSet is the found set an import works on, read and locked in
// its transaction, so no record of it is deleted or changed by another
// writer until the import is done: the ids given, in the order given, an id
// no longer in the table keeping its place as "", or every record of the
// table in creation order when none are. order is the table's creation
// order column, resolved before the transaction began.
func (s *Service) importFoundSet(
	ctx context.Context, tx execer, tableName, order string, given []string,
) ([]string, error) {
	dialect := s.driver.Dialect()
	query := fmt.Sprintf("SELECT %s FROM %s %s", dialect.QuoteIdentifier("id"),
		dialect.QuoteIdentifier(tableName), thenBy(dialect, "", order, false))
	if order != "id" {
		query = thenBy(dialect, query, "id", false)
	}
	query += " FOR UPDATE"
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("%w: the found set could not be read: %v", ErrImport, err)
	}
	defer rows.Close()
	var all []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		all = append(all, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if given == nil {
		return all, nil
	}
	present := make(map[string]bool, len(all))
	for _, id := range all {
		present[id] = true
	}
	kept := make([]string, 0, len(given))
	seen := make(map[string]bool, len(given))
	for _, id := range given {
		if seen[id] {
			continue // a record is in a found set once
		}
		seen[id] = true
		if !present[id] {
			id = ""
		}
		kept = append(kept, id)
	}
	return kept, nil
}

func copyRecord(record map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(record))
	for k, v := range record {
		out[k] = v
	}
	return out
}

// insertInTx adds a record, with its record ID when recordID is not 0.
func insertInTx(
	ctx context.Context, tx execer, dialect dbal.Dialect,
	tableName string, record map[string]interface{}, recordID int64,
) error {
	columns := make([]string, 0, len(record))
	placeholders := make([]string, 0, len(record))
	values := make([]interface{}, 0, len(record))
	i := 1
	for field, value := range record {
		columns = append(columns, columnSQL(dialect, field))
		placeholders = append(placeholders, dialect.Placeholder(i))
		values = append(values, value)
		i++
	}
	overriding := ""
	if recordID > 0 {
		columns = append(columns, dialect.QuoteIdentifier(dbal.CreationOrderColumn))
		placeholders = append(placeholders, dialect.Placeholder(i))
		values = append(values, recordID)
		overriding = dbal.OverridingSystemValue(dialect.Engine())
	}
	query := fmt.Sprintf("INSERT INTO %s (%s)%s VALUES (%s)",
		dialect.QuoteIdentifier(tableName),
		strings.Join(columns, ", "), overriding, strings.Join(placeholders, ", "))
	_, err := tx.ExecContext(ctx, query, values...)
	return err
}

// updateInTx writes a record's new values, counting the modification as m
// says (see modification.countSQL).
func updateInTx(
	ctx context.Context, tx execer, dialect dbal.Dialect,
	tableName, id string, record map[string]interface{}, m modification,
) error {
	sets := make([]string, 0, len(record))
	values := make([]interface{}, 0, len(record)+1)
	i := 1
	for field, value := range record {
		if field == "id" {
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = %s", columnSQL(dialect, field), dialect.Placeholder(i)))
		values = append(values, value)
		i++
	}
	if len(sets) == 0 {
		return nil
	}
	if count := m.countSQL(dialect); count != "" {
		sets = append(sets, count)
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

	// FirstRepetitionOnly writes only the first repetition of a repeating
	// field. By default a cell holds every repetition, separated by the
	// group separator (0x1D) as FileMaker exports them. A single repetition
	// can also be chosen by naming it as a field, "field[n]".
	FirstRepetitionOnly bool `json:"first_repetition_only,omitempty"`
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
			if !fields.isInternal(name) {
				chosen = append(chosen, name)
			}
		}
		sort.Strings(chosen)
	}
	for _, field := range chosen {
		if err := checkValueKey(fields, tableName, field); err != nil {
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
	// A value kept with its original text is exported as that text, the way
	// the record shows it (#124).
	if err := s.AttachSourceText(ctx, tableName, rows); err != nil {
		return nil, err
	}
	// A container result exports as the container it refers to.
	if err := s.resolveContainerResults(ctx, tableName, rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		originals, _ := row[sourcetext.Key].(map[string]string)
		cells := make([]string, len(chosen))
		for i, field := range chosen {
			if reps := fields.maxRepetitions(field); reps > 1 && !opts.FirstRepetitionOnly {
				cells[i] = joinRepetitionText(fields[field], field, reps, row, originals)
				continue
			}
			if text, ok := originals[field]; ok {
				cells[i] = text
				continue
			}
			cells[i] = exportText(fields.typeOf(field), row[field])
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
		// With no sort the records come in creation order, as FileMaker
		// exports an unsorted found set; every order ends on it, so the
		// pages are stable.
		query := QueryOptions{Limit: MaxPageSize, Offset: len(all), Sort: opts.Sort}

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

// CoerceImportValue turns one cell of exported text into the value its field
// holds: a number as the exact decimal it was written as, a date and a
// timestamp in File4Base's own notation, text with its spaces intact, and an
// empty cell as nothing at all. Anything that will not convert is an error
// naming why.
//
// It is the conversion every import runs, exported so that the assisted
// FileMaker migration converts a record export the same exact way rather than
// through a float of its own (#90), and so the one piece of an import with no
// database in it can be tested on its own.
func CoerceImportValue(fieldType dbal.AgnosticFieldType, text string, order DateOrder) (interface{}, error) {
	return coerce(fieldType, text, order)
}

// CoerceForTest is the previous name of CoerceImportValue.
//
// Deprecated: call CoerceImportValue.
func CoerceForTest(fieldType dbal.AgnosticFieldType, text string, order DateOrder) (interface{}, error) {
	return coerce(fieldType, text, order)
}
