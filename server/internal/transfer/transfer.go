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
}

// RejectedCount is how many rows the destination would not accept.
func (r *Report) RejectedCount() int { return len(r.Rejected) }

// Run carries out one transfer.
//
// sourceDriver and destinationDriver are the two solutions' databases, each
// already authorized for the account that presented it: this package never
// opens a connection and never reads a credential. They may be the same
// driver, for a transfer between two tables of one solution.
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

	source := data.NewService(sourceDriver)
	destination := data.NewService(destinationDriver)

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
	if def.Mode == schema.TransferModeUpdateMatching {
		action = data.ImportUpdateMatching
	}

	imported, err := destination.ImportRecords(ctx, def.Destination.Table, accepted, data.ImportOptions{
		Action:       action,
		Mappings:     mappings,
		MatchFields:  def.MatchFields,
		AddUnmatched: def.OnUnmatched != schema.TransferUnmatchedSkip,
		DateOrder:    order,
	})
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
	report.Committed = true
	return report, nil
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
