// Package transfer runs a persisted transfer definition: it reads a found set
// out of one solution and writes it into another, by explicit field mappings
// and stable match keys (#96).
//
// It owns no import path of its own. The source side is the export every
// manual export runs and the destination side is the import every manual
// import runs, so a transfer converts values, evaluates calculations,
// enforces validation rules and commits in one transaction exactly as a
// person importing the same file by hand would (#38, #92).
package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// ErrTransfer is returned when a transfer cannot be carried out. Nothing is
// written when it is.
var ErrTransfer = errors.New("transfer failed")

// ErrRejected is returned when rows were rejected and the definition's
// policy is "abort". The report carries every rejected row, and the
// destination is untouched.
var ErrRejected = errors.New("transfer refused: rows the destination cannot hold")

// Rejection is one source value the destination field cannot hold.
type Rejection struct {
	// Row is the 1-based position of the record in the source found set, in
	// the order the source was read.
	Row int `json:"row"`

	SourceField      string `json:"source_field"`
	DestinationField string `json:"destination_field"`

	// Value is the text the source wrote. It is the source's own data, so it
	// is reported back to the caller that asked for the transfer and nowhere
	// else.
	Value string `json:"value"`

	// Reason says what the destination field would not accept, in the words
	// the import itself uses.
	Reason string `json:"reason"`
}

// Report says what one run of a transfer did.
type Report struct {
	Transfer string `json:"transfer"`

	SourceDatabase      string `json:"source_database"`
	SourceTable         string `json:"source_table"`
	DestinationDatabase string `json:"destination_database"`
	DestinationTable    string `json:"destination_table"`

	Mode        string `json:"mode"`
	OnUnmatched string `json:"on_unmatched,omitempty"`
	OnRejected  string `json:"on_rejected"`

	// Rows is how many records the source found set held.
	Rows int `json:"rows"`

	Added    int         `json:"added"`
	Updated  int         `json:"updated"`
	Skipped  int         `json:"skipped"`
	Rejected []Rejection `json:"rejected"`

	// Committed says whether the destination transaction was committed. It
	// is false for every refused transfer, and a refused transfer leaves the
	// destination exactly as it was, so the same run can be replayed once
	// the source is corrected.
	Committed bool `json:"committed"`

	// Fields are the destination fields that were written, in mapping order.
	Fields []string `json:"fields"`

	// Unpaired are the destination fields of a transfer matching names that
	// no source field was named as on this run, and so were left out of it,
	// as FileMaker leaves a target field no source name matches (#191).
	Unpaired []string `json:"unpaired,omitempty"`

	// RecordIDs are the destination records the run added or updated, when
	// RunOptions.ReturnRecordIDs asked for them: the found set FileMaker
	// shows once an Import Records step is done (#191).
	RecordIDs []string `json:"record_ids,omitempty"`
}

// RunOptions are what a run is told on top of its definition, by the script
// step that runs it.
type RunOptions struct {
	// DestinationFoundSet is the found set of the destination table, in its
	// order, as the window running an Import Records step holds it: the
	// records a "replace_found_set" transfer replaces one by one and the only
	// ones an "update_matching" transfer matches, every matching record being
	// updated, as FileMaker updates them. Nil is the whole table in creation
	// order, and an update then updates the first record that matches.
	DestinationFoundSet []string

	// ReturnRecordIDs lists in the report the records the run added or
	// updated.
	ReturnRecordIDs bool
}

// RejectedCount is how many rows the destination would not accept.
func (r *Report) RejectedCount() int { return len(r.Rejected) }

// Run carries out one transfer.
//
// sourceDriver and destinationDriver are the two solutions' databases, each
// already authorized for the account that presented it: this package never
// opens a connection and never reads a credential. They may be the same
// driver, for a transfer between two tables of one solution. The source is
// read as sourceAccount, so a calculation that reaches a related table, or a
// container result that refers to one, carries nothing of a table that
// account may not read; the destination's calculations (validation, an
// ExecuteSQL query) read as destinationAccount, as that account importing
// the records by hand would.
//
// The transaction boundary is the destination table: one transfer writes one
// table, inside one transaction, so a row that cannot be stored takes the
// whole run with it and leaves nothing half-applied. A transfer of parents
// and children is two definitions run in order, and each commits on its own.
func Run(
	ctx context.Context,
	def *schema.TransferDefinition,
	sourceDriver, destinationDriver dbal.DatabaseDriver,
	sourceDatabase, destinationDatabase string,
	sourceAccount, destinationAccount Account,
) (*Report, error) {
	return RunWith(ctx, def, sourceDriver, destinationDriver, sourceDatabase, destinationDatabase,
		sourceAccount, destinationAccount, RunOptions{})
}

// RunWith is Run, told what the step running it knows: the destination's
// found set, and whether to answer the records it wrote.
func RunWith(
	ctx context.Context,
	def *schema.TransferDefinition,
	sourceDriver, destinationDriver dbal.DatabaseDriver,
	sourceDatabase, destinationDatabase string,
	sourceAccount, destinationAccount Account,
	opts RunOptions,
) (*Report, error) {
	if def == nil {
		return nil, fmt.Errorf("%w: no transfer definition", ErrTransfer)
	}

	report := &Report{
		Transfer:            def.Name,
		SourceDatabase:      sourceDatabase,
		SourceTable:         def.Source.Table,
		DestinationDatabase: destinationDatabase,
		DestinationTable:    def.Destination.Table,
		Mode:                def.Mode,
		OnUnmatched:         def.OnUnmatched,
		OnRejected:          def.OnRejected,
		Rejected:            []Rejection{},
		Fields:              def.DestinationFields(),
	}

	if def.Mode == schema.TransferModeReplaceFoundSet && opts.DestinationFoundSet == nil {
		return report, fmt.Errorf("%w: %q replaces field contents in the destination's found set, "+
			"and the run names none; it never defaults to the whole table", ErrTransfer, def.Name)
	}

	source := readingAs(sourceDriver, sourceAccount)
	destination := readingAs(destinationDriver, destinationAccount)
	if destinationAccount.Name != "" {
		destination = destination.WithAccount(destinationAccount.Name)
	}

	// Both ends are resolved before a single record moves. A mapping that
	// names a field neither table has stops the run here, which is what the
	// issue asks of an unresolved target: it blocks, it is never guessed at.
	sourceTypes, err := source.FieldTypes(ctx, def.Source.Table)
	if err != nil {
		return report, fmt.Errorf("%w: the source table %q of %s cannot be read: %v",
			ErrTransfer, def.Source.Table, describeDatabase(sourceDatabase), err)
	}
	destinationTypes, err := destination.FieldTypes(ctx, def.Destination.Table)
	if err != nil {
		return report, fmt.Errorf("%w: the destination table %q of %s cannot be read: %v",
			ErrTransfer, def.Destination.Table, describeDatabase(destinationDatabase), err)
	}
	if def.MatchByName {
		paired, unpaired, err := pairByName(def, sourceTypes)
		if err != nil {
			return report, err
		}
		def, report.Unpaired, report.Fields = paired, unpaired, paired.DestinationFields()
	}
	for _, m := range def.Mappings {
		if _, ok := sourceTypes[m.SourceField]; !ok {
			return report, fmt.Errorf("%w: %q is not a field of the source table %q",
				ErrTransfer, m.SourceField, def.Source.Table)
		}
		if _, ok := destinationTypes[m.DestinationField]; !ok {
			return report, fmt.Errorf("%w: %q is not a field of the destination table %q",
				ErrTransfer, m.DestinationField, def.Destination.Table)
		}
	}
	for _, field := range def.MatchFields {
		if _, ok := destinationTypes[field]; !ok {
			return report, fmt.Errorf("%w: records are matched on %q, which is not a field of "+
				"the destination table %q", ErrTransfer, field, def.Destination.Table)
		}
	}

	// The source side is an ordinary export of the found set, with one column
	// per mapping, in mapping order.
	exported, err := source.ExportRecords(ctx, def.Source.Table, data.ExportOptions{
		Requests: def.FoundSet,
		Fields:   def.SourceFields(),
		Headings: def.DestinationFields(),
		Sort:     def.Sort,
	})
	if err != nil {
		return report, fmt.Errorf("%w: the source found set could not be read: %v", ErrTransfer, err)
	}
	report.Rows = len(exported.Rows)

	order := data.DateOrder(def.DateOrder)
	accepted, rejected := screen(def, exported, destinationTypes, order)
	report.Rejected = rejected
	// Replacing the found set pairs the n-th row with the n-th record, so a
	// row left out still takes its record with it rather than shifting every
	// row after it onto the wrong record.
	var passOver map[int]bool
	if def.Mode == schema.TransferModeReplaceFoundSet && len(rejected) > 0 {
		accepted = exported
		passOver = make(map[int]bool, len(rejected))
		for _, r := range rejected {
			passOver[r.Row-1] = true
		}
	}

	if len(rejected) > 0 && def.OnRejected == schema.TransferRejectedAbort {
		// Nothing has been written: the whole transfer is refused, and the
		// caller can correct the source and replay it.
		return report, fmt.Errorf("%w: %d of %d rows were rejected and this transfer aborts on a "+
			"rejected row, so the destination was left unchanged",
			ErrRejected, len(rejected), report.Rows)
	}

	mappings := make([]data.ImportMapping, 0, len(def.Mappings))
	for i, m := range def.Mappings {
		mappings = append(mappings, data.ImportMapping{Column: i, Field: m.DestinationField})
	}
	action := data.ImportAdd
	switch def.Mode {
	case schema.TransferModeUpdateMatching:
		action = data.ImportUpdateMatching
	case schema.TransferModeReplaceFoundSet:
		action = data.ImportReplaceFoundSet
	}
	importOpts := data.ImportOptions{
		Action:           action,
		Mappings:         mappings,
		MatchFields:      def.MatchFields,
		AddUnmatched:     def.OnUnmatched != schema.TransferUnmatchedSkip,
		DateOrder:        order,
		PerformAutoEnter: def.PerformAutoEnter,
		AutoEnterFields:  def.AutoEnterFields,
		ReturnRecordIDs:  opts.ReturnRecordIDs,
		PassOver:         passOver,
	}
	if action != data.ImportAdd && opts.DestinationFoundSet != nil {
		importOpts.FoundSet = opts.DestinationFoundSet
	}

	imported, err := destination.ImportRecords(ctx, def.Destination.Table, accepted, importOpts)
	if err != nil {
		// The destination transaction rolled back, so the destination is as
		// it was and the transfer can be replayed.
		return report, fmt.Errorf("%w: nothing was written to %q: %v",
			ErrTransfer, def.Destination.Table, err)
	}

	report.Added = imported.Added
	report.Updated = imported.Updated
	// A row left out by the rejected-row policy is not an import's "skipped":
	// it is counted as rejected, and only the rows the destination passed
	// over for want of a match are skipped.
	report.Skipped = imported.Skipped
	report.Fields = imported.Fields
	report.RecordIDs = imported.RecordIDs
	report.Committed = true
	return report, nil
}

// namePairing pairs fields by name, as FileMaker pairs matching names each
// time its Import Records step is performed. FileMaker field names are unique
// within a table regardless of case, so the names are compared without it:
// one name pairs with at most one field. A table with two names a case apart
// (possible in File4Base, not in FileMaker) pairs neither; twice names them.
//
// It is the one pairing both the review of a step and the run of its
// transfer use, so the review shows what the run does.
type namePairing struct {
	byKey map[string]string
	twice map[string]bool
}

func newNamePairing(names []string) namePairing {
	p := namePairing{byKey: make(map[string]string, len(names)), twice: map[string]bool{}}
	for _, name := range names {
		if strings.HasSuffix(name, "[]") {
			continue // a repeating field's internal entry, not a field
		}
		key := strings.ToLower(name)
		if _, taken := p.byKey[key]; taken {
			p.twice[key] = true
		}
		p.byKey[key] = name
	}
	return p
}

// pair answers the field named as destination, whatever its case.
func (p namePairing) pair(destination string) (string, bool) {
	key := strings.ToLower(destination)
	if p.twice[key] {
		return "", false
	}
	name, ok := p.byKey[key]
	return name, ok
}

// pairByName pairs each destination field of a transfer matching names with
// the source field named as it, on this run (see namePairing). A destination
// field no source field is named as is left out and answered in unpaired,
// unless records are matched on it: the run is then refused, as it would
// match on nothing.
func pairByName(
	def *schema.TransferDefinition, sourceTypes map[string]dbal.AgnosticFieldType,
) (*schema.TransferDefinition, []string, error) {
	names := make([]string, 0, len(sourceTypes))
	for field := range sourceTypes {
		names = append(names, field)
	}
	pairing := newNamePairing(names)
	matched := make(map[string]bool, len(def.MatchFields))
	for _, field := range def.MatchFields {
		matched[field] = true
	}
	paired := *def
	paired.Mappings = make([]schema.TransferMapping, 0, len(def.Mappings))
	var unpaired []string
	for _, m := range def.Mappings {
		if pairing.twice[strings.ToLower(m.DestinationField)] {
			return def, nil, fmt.Errorf("%w: %q pairs fields by name, and the source table %q has two fields "+
				"named %q whatever their case", ErrTransfer, def.Name, def.Source.Table, m.DestinationField)
		}
		source, ok := pairing.pair(m.DestinationField)
		if !ok {
			if matched[m.DestinationField] {
				return def, nil, fmt.Errorf("%w: %q pairs fields by name and matches records on %q, and the "+
					"source table %q has no field of that name", ErrTransfer, def.Name, m.DestinationField,
					def.Source.Table)
			}
			unpaired = append(unpaired, m.DestinationField)
			continue
		}
		paired.Mappings = append(paired.Mappings,
			schema.TransferMapping{SourceField: source, DestinationField: m.DestinationField})
	}
	if len(paired.Mappings) == 0 {
		return def, unpaired, fmt.Errorf("%w: %q pairs fields by name, and the source table %q has no field "+
			"named as any of its destination fields", ErrTransfer, def.Name, def.Source.Table)
	}
	return &paired, unpaired, nil
}

// screen converts every mapped cell the way the destination import will, and
// separates the rows the destination can hold from the rows it cannot.
//
// It is the same conversion the import runs (data.CoerceImportValue), so a
// row this function accepts is a row the import accepts: screening a row here
// rather than letting the import fail is what lets one bad row be reported
// with its field and its value, and lets a transfer either refuse everything
// or commit the rest.
func screen(
	def *schema.TransferDefinition,
	exported *dataio.Table,
	destinationTypes map[string]dbal.AgnosticFieldType,
	order data.DateOrder,
) (*dataio.Table, []Rejection) {
	accepted := &dataio.Table{
		Columns: append([]string(nil), exported.Columns...),
		Rows:    make([][]string, 0, len(exported.Rows)),
	}
	rejected := []Rejection{}

	for i := range exported.Rows {
		var why *Rejection
		for column, m := range def.Mappings {
			cell := exported.Cell(i, column)
			if _, err := data.CoerceImportValue(destinationTypes[m.DestinationField], cell, order); err != nil {
				why = &Rejection{
					Row:              i + 1,
					SourceField:      m.SourceField,
					DestinationField: m.DestinationField,
					Value:            cell,
					Reason:           err.Error(),
				}
				break // the first field that will not convert is why the row is out
			}
		}
		if why != nil {
			rejected = append(rejected, *why)
			continue
		}
		accepted.Rows = append(accepted.Rows, exported.Rows[i])
	}
	return accepted, rejected
}

func describeDatabase(name string) string {
	if strings.TrimSpace(name) == "" {
		return "this solution"
	}
	return "solution " + name
}
