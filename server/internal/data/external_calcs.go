package data

import (
	"context"
	"errors"
	"fmt"
)

// Unstored calculations that read through external occurrences (#107).
//
// An occurrence of another solution's table is part of this solution's
// relationship graph, so Occurrence::Field, Count, Sum, List and the other
// aggregates read it like any related table. Each relationship is still
// followed in two steps — the match values are read where the records in
// hand are, the related records are then looked up where they are — and no
// statement joins two solutions. The other solution is read through its own
// data service, as the account the caller's session group holds there, with
// that account's privileges; its own unstored calculations are computed
// there, with its graph and its calculations.
//
// When the other solution cannot be read, the field is empty, as for a
// formula that fails: the record is still read. A table the account there
// may not read is <No Access>, as a table of this solution the caller may
// not read is.

// openedTable is the reader of a table of another solution, or why it cannot
// be read.
type openedTable struct {
	reader *relatedReader
	err    error
}

// open is the reader of the solution the table of an external occurrence is
// in, reading as the account the caller's session group holds there, once it
// is known that the account may read the table and that the table is there.
// What it finds is kept for the request.
func (r *relatedReader) open(ctx context.Context, t ExternalTarget) (*relatedReader, error) {
	key := t.Database + "\x00" + t.Table
	if o, ok := r.opened[key]; ok {
		return o.reader, o.err
	}
	var reader *relatedReader
	peer, err := r.s.Open(ctx, t)
	if err == nil {
		reader = r.readState.reader(peer, t.Database+"\x00")
	}
	err = noAccessAsLocal(err)
	r.opened[key] = openedTable{reader: reader, err: err}
	return reader, err
}

// noAccessAsLocal makes a table of another solution the account there may
// not read read as <No Access>, as a table of this solution the caller may
// not read does: the error is then both ErrNoAccess and what it says about
// the other solution.
func noAccessAsLocal(err error) error {
	var e *ExternalUnavailableError
	if errors.As(err, &e) && e.Reason == ExternalNoAccess && !errors.Is(err, ErrNoAccess) {
		return fmt.Errorf("%w: %w", ErrNoAccess, err)
	}
	return err
}
