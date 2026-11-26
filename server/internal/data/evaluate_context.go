package data

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
)

// The record a portal's row is evaluated in (see ContextRecord).

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
	portal, ok := graph.named(occurrence)
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
	at.from.table, at.from.occurrence, at.from.row = layout.table, layout.id, map[string]interface{}{}
	if strings.TrimSpace(in.RecordID) != "" {
		if at.from.row, err = r.s.GetRow(ctx, layout.table, in.RecordID); err != nil {
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
	rt, k := c.throughPin(target, field, rc)
	if rt == nil || (k == len(rt.path)-1 && rc == reachAll) {
		return nil, false, nil
	}
	rest := &route{path: rt.path[k+1:], plans: rt.plans[k+1:], columns: rt.columns[k+1:], end: rt.end, reach: rc,
		totals: rt.totals}
	if len(rest.path) > 1 {
		rest.reach = reachAll
	}
	values, err = c.reader.readAlong(c.ctx, rest, c.pin.row, rc)
	return values, true, err
}

// throughPin is the route from the layout's occurrence to target, and the
// index of its step that reaches the pinned row's occurrence; nil when
// there is no pinned row, the route cannot be resolved or does not reach it.
func (c *unstoredContext) throughPin(target, field string, rc reach) (*route, int) {
	if c.pin == nil {
		return nil, -1
	}
	rt, err := c.reader.route(c.ctx, routeKey{c.table, c.occurrence, target, field, rc, false})
	if err != nil {
		return nil, -1
	}
	k := slices.IndexFunc(rt.path, func(h hop) bool { return h.to.OccurrenceID == c.pin.occurrence })
	if k < 0 {
		return nil, -1
	}
	return rt, k
}

// beyondPin reports whether a read of field of target goes through the
// pinned row to an occurrence past it: its records are then the ones
// related to the row, read every one with Related, not totalled nor read
// one by one from the layout's record.
func (c *unstoredContext) beyondPin(target, field string) bool {
	rt, k := c.throughPin(target, field, reachAll)
	return rt != nil && k < len(rt.path)-1
}
