package data

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
)

// The record a portal's row is evaluated in (see ContextRecord).

// ErrContextNotFound is returned when the layout's record a portal's row is
// to be evaluated in is not there.
var ErrContextNotFound = errors.New("the record of the layout is not found")

// evaluatedRecord is the record expressions are evaluated against, and
// where their related reads start.
type evaluatedRecord struct {
	// table is the record's table, in the solution database names when it
	// is another one (empty for this one); fields are that table's.
	table    string
	database string
	fields   fieldSet
	row      map[string]interface{}
	describe func(*calc.Container)
	// from is where a reference to a related field starts: the record
	// itself, seen from the occurrence given, or the layout's record a
	// portal row is evaluated in, the row then pinned (see pinnedRecord).
	from struct {
		table, occurrence string
		row               map[string]interface{}
	}
	pin *pinnedRecord
}

// pinnedRecord is a portal's row, the current record of the portal's
// occurrence while a formula of the layout is evaluated on it.
type pinnedRecord struct {
	occurrence string // its id
	row        map[string]interface{}
}

// evaluated reads the record recordID of tableName, seen from occurrence,
// that expressions are evaluated against (none when recordID is empty).
// With a context, occurrence is the one of a portal on a layout, which may
// be an occurrence of another solution's table: the record is then read
// there, as the account the caller's session group holds there, and the
// layout's record is read here, as the caller. Each is read only when its
// table may be read.
func (r *relatedReader) evaluated(
	ctx context.Context, tableName, occurrence, recordID string, in *ContextRecord,
) (*evaluatedRecord, error) {
	at := &evaluatedRecord{table: tableName, row: map[string]interface{}{}}
	if in == nil {
		fields, err := r.s.tableFields(ctx, tableName)
		if err != nil {
			return nil, err
		}
		if recordID != "" {
			if at.row, err = r.s.GetRow(ctx, tableName, recordID); err != nil {
				return nil, err
			}
		}
		at.fields, at.describe = fields, r.s.describeContainer(ctx)
		at.from.table, at.from.occurrence, at.from.row = tableName, occurrence, at.row
		return at, nil
	}

	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	layout, ok := graph.named(in.Occurrence)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrOccurrenceNotFound, in.Occurrence)
	}
	if layout.external != nil {
		return nil, layout.external.Unsupported("a portal row evaluated on a layout of another solution's table")
	}
	// No occurrence named is the table's own, as without a context.
	portal, ok := graph.named(occurrence)
	if strings.TrimSpace(occurrence) == "" {
		portal, err = graph.contextOf(tableName, "")
		ok = err == nil
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrOccurrenceNotFound, occurrence)
	}
	if portal.table != tableName {
		return nil, fmt.Errorf("%w: %s is not an occurrence of the table %s", ErrOccurrenceNotFound, portal.name, tableName)
	}

	// The layout's record, as the caller.
	if err := r.checkRead(ctx, layout.table); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPermission, err)
	}
	// A portal of the layout's own occurrence (a self-join, whose rows are
	// read through one of its relationships): the row is the current
	// record of that occurrence, its fields read as its own.
	if portal.id == layout.id {
		return r.evaluated(ctx, tableName, portal.id, recordID, nil)
	}
	at.from.table, at.from.occurrence, at.from.row = layout.table, layout.id, map[string]interface{}{}
	if strings.TrimSpace(in.RecordID) != "" {
		if at.from.row, err = r.s.GetRow(ctx, layout.table, in.RecordID); errors.Is(err, ErrRecordNotFound) {
			// Not a row gone, which answers nothing: the request's.
			return nil, fmt.Errorf("%w: %s", ErrContextNotFound, in.RecordID)
		} else if err != nil {
			return nil, err
		}
	}

	// The row, where its records are.
	rows := r
	if portal.external != nil {
		if rows, err = r.open(ctx, *portal.external); err != nil {
			return nil, err
		}
		at.database = portal.external.Database
	} else if err := r.checkRead(ctx, tableName); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPermission, err)
	}
	if at.fields, err = rows.tableFields(ctx, tableName); err != nil {
		return nil, wrapExternal(portal, err)
	}
	if recordID != "" {
		if at.row, err = rows.s.GetRow(ctx, tableName, recordID); err != nil {
			return nil, wrapExternal(portal, err)
		}
	}
	at.describe = rows.s.describeContainer(ctx)
	at.pin = &pinnedRecord{occurrence: portal.id, row: at.row}
	return at, nil
}

// wrapExternal says, for an occurrence of another solution's table, which
// solution an error met reading it comes from.
func wrapExternal(o occurrence, err error) error {
	if o.external == nil {
		return err
	}
	return o.external.Wrap(err)
}

// named is the occurrence an id or a name names, the id first.
func (g *occurrenceGraph) named(name string) (occurrence, bool) {
	if o, ok := g.byID[name]; ok && name != "" {
		return o, true
	}
	o, ok := g.byName[strings.ToLower(strings.TrimSpace(name))]
	return o, ok && name != ""
}

// pinned reads field of the occurrence target from the pinned row, when the
// path from the layout's occurrence to target reaches the row's occurrence:
// the row itself for its own occurrence, the records related to it for an
// occurrence through it. An aggregate of the row's occurrence itself (every
// related record, rc reachAll) reads, as FileMaker's do, every record
// related to the layout's record, and so is not pinned. handled is false
// when the read is not the row's, which is then read from the layout's
// record; and when the path cannot be resolved, which that read reports.
func (c *unstoredContext) pinned(target, field string, rc reach) (values []any, handled bool, err error) {
	rest, ok := c.fromPin(target, field, rc, false)
	if !ok || (len(rest.path) == 0 && rc == reachAll) {
		return nil, false, nil
	}
	values, err = c.reader.readAlong(c.ctx, rest, c.pin.row, rc)
	return values, true, err
}

// beyondPin is, for a read of field of target that goes through the pinned
// row to an occurrence past it, the rest of its route, from the row: those
// records are the ones related to the row, not to the layout's record. nil
// when the read does not go past the row.
func (c *unstoredContext) beyondPin(target, field string, rc reach, totals bool) *route {
	rest, ok := c.fromPin(target, field, rc, totals)
	if !ok || len(rest.path) == 0 {
		return nil
	}
	return rest
}

// fromPin is the rest, from the pinned row, of the route from the layout's
// occurrence to target, when it reaches the row's occurrence: what follows
// the row, empty for the row's own occurrence. ok is false when there is no
// pinned row, the route cannot be resolved (the read from the layout's
// record reports it) or does not reach the row. The whole route was
// resolved, and so every table it crosses checked readable.
func (c *unstoredContext) fromPin(target, field string, rc reach, totals bool) (*route, bool) {
	if c.pin == nil {
		return nil, false
	}
	rt, err := c.reader.route(c.ctx, routeKey{c.table, c.occurrence, target, field, rc, totals})
	if err != nil {
		return nil, false
	}
	k := slices.IndexFunc(rt.path, func(h hop) bool { return h.to.OccurrenceID == c.pin.occurrence })
	if k < 0 {
		return nil, false
	}
	rest := &route{path: rt.path[k+1:], plans: rt.plans[k+1:], columns: rt.columns[k+1:], end: rt.end, reach: rc,
		totals: totals}
	if rc.n < 0 {
		rest.reach.present = rt.end.field
	}
	// As resolveRoute: across several, every record is read.
	if len(rest.path) > 1 {
		rest.reach = reachAll
	}
	return rest, true
}
