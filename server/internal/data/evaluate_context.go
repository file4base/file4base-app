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

// ErrOccurrenceElsewhere is returned when the records to evaluate are of an
// occurrence of another solution's table but were not asked for through
// that occurrence (the table's route reads this solution's tables only).
var ErrOccurrenceElsewhere = errors.New("an occurrence of another solution's table")

// ErrPortalRowNotFound is returned when the active portal row a layout's
// record is evaluated with is not there.
var ErrPortalRowNotFound = errors.New("the active portal row is not found")

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

// evaluationScope is what the records of one request are evaluated in,
// resolved once for them all: their table and where it is read, and, with
// a context, the layout's record and the portal's occurrence each row is
// the current record of.
type evaluationScope struct {
	table    string
	database string
	fields   fieldSet
	describe func(*calc.Container)
	// rows reads the records: this reader, or the one of the solution an
	// occurrence of another solution's table is in.
	rows *relatedReader
	// portal wraps an error met reading a record there.
	portal occurrence
	// from is where a reference to a related field starts: the record
	// itself, seen from occurrence (own); or the layout's record, the row
	// then pinned to pinOccurrence.
	own        bool
	occurrence string
	from       struct {
		table, occurrence string
		row               map[string]interface{}
	}
	pinOccurrence string
	// activeRow is, for the layout's record (own), the active portal row:
	// the current record of the portal's occurrence (see PortalRow).
	activeRow *pinnedRecord
}

// evaluationScope resolves what the records of tableName, seen from occurrence, are
// evaluated in. With a context, occurrence is the one of a portal on a
// layout, which may be an occurrence of another solution's table: the
// records are then read there, as the account the caller's session group
// holds there, and the layout's record is read here, as the caller. Each
// is read only when its table may be read. external says the caller asks
// for an occurrence of another solution's table, and the occurrence must
// be one; and one of this solution's tables when it does not.
func (r *relatedReader) evaluationScope(
	ctx context.Context, tableName, occurrence string, in *ContextRecord, external bool, active *PortalRow,
) (*evaluationScope, error) {
	if in == nil {
		fields, err := r.s.tableFields(ctx, tableName)
		if err != nil {
			return nil, err
		}
		sc := &evaluationScope{table: tableName, fields: fields, describe: r.s.describeContainer(ctx), rows: r,
			own: true, occurrence: occurrence}
		if active != nil {
			if sc.activeRow, err = r.activePortalRow(ctx, tableName, occurrence, *active); err != nil {
				return nil, err
			}
		}
		return sc, nil
	}
	if active != nil {
		return nil, fmt.Errorf("%w: a portal's rows evaluated in the layout's record have no active portal row", ErrInvalidValue)
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
	// The occurrence is resolved here only, so what is asked and what is
	// read are the same one.
	if (portal.external != nil) != external {
		if portal.external != nil {
			return nil, fmt.Errorf("%w: %s", ErrOccurrenceElsewhere, portal.name)
		}
		return nil, fmt.Errorf("%w: %s is an occurrence of this solution's table %s", ErrOccurrenceNotFound, portal.name, tableName)
	}

	// The layout's record, as the caller.
	if err := r.checkRead(ctx, layout.table); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPermission, err)
	}
	// A portal of the layout's own occurrence (a self-join, whose rows are
	// read through one of its relationships): the row is the current
	// record of that occurrence, its fields read as its own.
	if portal.id == layout.id {
		return r.evaluationScope(ctx, tableName, portal.id, nil, false, nil)
	}
	sc := &evaluationScope{table: tableName, portal: portal, pinOccurrence: portal.id, rows: r}
	sc.from.table, sc.from.occurrence, sc.from.row = layout.table, layout.id, map[string]interface{}{}
	if strings.TrimSpace(in.RecordID) != "" {
		if sc.from.row, err = r.s.GetRow(ctx, layout.table, in.RecordID); errors.Is(err, ErrRecordNotFound) {
			// Not a row gone, which answers nothing: the request's.
			return nil, fmt.Errorf("%w: %s", ErrContextNotFound, in.RecordID)
		} else if err != nil {
			return nil, err
		}
	}

	// The rows, where their records are.
	if portal.external != nil {
		if sc.rows, err = r.open(ctx, *portal.external); err != nil {
			return nil, err
		}
		sc.database = portal.external.Database
	} else if err := r.checkRead(ctx, tableName); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPermission, err)
	}
	if sc.fields, err = sc.rows.tableFields(ctx, tableName); err != nil {
		return nil, wrapExternal(portal, err)
	}
	sc.describe = sc.rows.s.describeContainer(ctx)
	return sc, nil
}

// at reads the record recordID expressions are evaluated against (none
// when recordID is empty).
func (sc *evaluationScope) at(ctx context.Context, recordID string) (*evaluatedRecord, error) {
	at := &evaluatedRecord{table: sc.table, database: sc.database, fields: sc.fields, describe: sc.describe,
		row: map[string]interface{}{}}
	if recordID != "" {
		var err error
		if at.row, err = sc.rows.s.GetRow(ctx, sc.table, recordID); err != nil {
			return nil, wrapExternal(sc.portal, err)
		}
	}
	if sc.own {
		at.from.table, at.from.occurrence, at.from.row = sc.table, sc.occurrence, at.row
		at.pin = sc.activeRow
		return at, nil
	}
	at.from = sc.from
	at.pin = &pinnedRecord{occurrence: sc.pinOccurrence, row: at.row}
	return at, nil
}

// activePortalRow reads the active portal row a record of tableName, seen
// from occurrence (a layout's), is evaluated with: the row's record, read
// where the portal's occurrence is, there as the account the caller's
// session group holds for an occurrence of another solution's table, here
// as the caller. A row of the layout's own occurrence pins nothing.
func (r *relatedReader) activePortalRow(
	ctx context.Context, tableName, occurrence string, active PortalRow,
) (*pinnedRecord, error) {
	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	layout, ok := graph.named(occurrence)
	if strings.TrimSpace(occurrence) == "" {
		layout, err = graph.contextOf(tableName, "")
		ok = err == nil
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrOccurrenceNotFound, occurrence)
	}
	portal, ok := graph.named(active.Occurrence)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrOccurrenceNotFound, active.Occurrence)
	}
	if strings.TrimSpace(active.RecordID) == "" {
		return nil, fmt.Errorf("%w: the active portal row has no record id", ErrInvalidValue)
	}
	if portal.id == layout.id {
		// A portal of the layout's own occurrence: its fields are the
		// layout record's, there is no other occurrence to pin the row
		// on. The client, whose layout may not name its occurrence, need
		// not resolve it the way the server does.
		return nil, nil
	}
	rows := r
	if portal.external != nil {
		if rows, err = r.open(ctx, *portal.external); err != nil {
			return nil, err
		}
	} else if err := r.checkRead(ctx, portal.table); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoPermission, err)
	}
	row, err := rows.s.GetRow(ctx, portal.table, active.RecordID)
	if errors.Is(err, ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrPortalRowNotFound, active.RecordID)
	}
	if err != nil {
		return nil, wrapExternal(portal, err)
	}
	return &pinnedRecord{occurrence: portal.id, row: row}, nil
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
