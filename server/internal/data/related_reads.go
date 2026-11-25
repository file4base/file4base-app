package data

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
)

// The related records unstored calculations read.
//
// A formula reads a related field in one of three ways (calc.RelatedReads),
// and no further than that way needs:
//
//   - Occurrence::Field reads the first related record, in the
//     relationship's sort order: one record per record in hand;
//   - Count, Sum, Average, Min and Max of a stored field of the records one
//     relationship away are totalled where the records are, without reading
//     them;
//   - List, Last and GetNthRecord, and the aggregates that cannot be totalled
//     there, read every related record, at most maxRelatedRecords for one
//     record in hand.
//
// Only the fields a read needs are read: the field, what the relationship
// sorts by, and, on the way, what the next relationship matches on. A record
// whose field is an unstored calculation is read whole, since the
// calculation reads the others.
//
// Each relationship is followed for every record in hand at once, in one
// statement where the related records are (see fetch), and before the
// records of a page are computed, what their calculations read is fetched
// ahead the same way (see prefetch), so a page reads a relationship once, not
// once per record.

// maxRelatedRecords is the most records a read takes across one relationship
// from one record in hand. More is refused (ErrTooManyRelated) rather than
// read whole into memory.
const maxRelatedRecords = 10000

// ErrTooManyRelated is returned for a read of more than maxRelatedRecords
// related records of one record: a List, Last or GetNthRecord of them, or a
// path through them.
var ErrTooManyRelated = fmt.Errorf("more than %d related records", maxRelatedRecords)

// reach is how far a read goes into the records related across the last
// relationship of its path.
type reach int

const (
	// reachAll reads every related record.
	reachAll reach = iota
	// reachFirst reads the first, in the relationship's sort order.
	reachFirst
)

// unstoredContext is what an unstored calculation reads: its record, and the
// records related to it.
type unstoredContext struct {
	calc.RecordContext
	reader     *relatedReader
	ctx        context.Context
	table      string
	occurrence string
	row        map[string]interface{}
}

func (c *unstoredContext) Related(occurrence, field string) ([]any, error) {
	return c.reader.related(c.ctx, c.table, c.occurrence, c.row, occurrence, field, reachAll)
}

// RelatedFirst implements calc.Bounded.
func (c *unstoredContext) RelatedFirst(occurrence, field string) ([]any, error) {
	return c.reader.related(c.ctx, c.table, c.occurrence, c.row, occurrence, field, reachFirst)
}

// RelatedTotals implements calc.Bounded.
func (c *unstoredContext) RelatedTotals(occurrence, field, function string) (calc.RelatedTotals, bool, error) {
	return c.reader.totals(c.ctx, c.table, c.occurrence, c.row, occurrence, field, function)
}

// related reads field in the records of the occurrence target that are
// related to row, a record of tableName seen from the occurrence contextName:
// every one, or the first.
func (r *relatedReader) related(
	ctx context.Context, tableName, contextName string, row map[string]interface{}, target, field string, rc reach,
) ([]any, error) {
	rt, err := r.route(ctx, routeKey{tableName, contextName, target, field, rc, false})
	if err != nil {
		return nil, err
	}
	rows, err := r.walk(ctx, rt, []map[string]interface{}{row})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	if rc == reachFirst {
		rows = rows[:1]
	}
	end := rt.end
	// Only the field read is computed, with what it reads, in the solution
	// the records are in.
	if err := end.reader.fill(ctx, end.table, rows, []string{end.field}); err != nil {
		return nil, err
	}
	describe := end.reader.s.describeContainer(ctx)
	out := make([]any, len(rows))
	for i, rel := range rows {
		out[i] = inSolution(typedValue(end.table, end.fields, rel, end.field, describe), end.database)
	}
	return out, nil
}

// totals totals field, for function, across the records of the occurrence
// target related to row, where they are, when it can be: the field is a
// stored field of the records one relationship away, of a type function
// totals the same there. ok is false otherwise, and the records are read.
func (r *relatedReader) totals(
	ctx context.Context, tableName, contextName string, row map[string]interface{}, target, field, function string,
) (calc.RelatedTotals, bool, error) {
	rt, err := r.route(ctx, routeKey{tableName, contextName, target, field, reachAll, true})
	if err != nil {
		return calc.RelatedTotals{}, false, err
	}
	if !rt.totalled(function) {
		return calc.RelatedTotals{}, false, nil
	}
	if err := r.fetchTotals(ctx, rt, []map[string]interface{}{row}); err != nil {
		return calc.RelatedTotals{}, false, err
	}
	return r.sums[r.totalsMemo(rt, row)], true, nil
}

// inSolution marks a container read in another solution as being there, so
// a container result referring to it finds it there (see containerRef). Its
// description is still read where it was read.
func inSolution(v any, database string) any {
	c, ok := v.(calc.Container)
	if !ok || database == "" || c.Database != "" {
		return v
	}
	c.Database = database
	if describe := c.Describe; describe != nil {
		c.Describe = func(x *calc.Container) {
			x.Database = ""
			describe(x)
			x.Database = database
		}
	}
	return c
}

// route is a path to a related field, resolved once for a request: its
// relationships, what reading across each needs, and where the field is read.
type route struct {
	path  []hop
	plans []*hopPlan
	// columns are the fields read across each relationship: what the next
	// one matches on, then the field and what the last sorts by; nil reads
	// every field.
	columns []fieldSet
	end     pathEnd
	reach   reach
	// totals is set for a route that totals the field instead of reading it.
	totals bool
}

// routeKey names a route: a field read from a table's occurrence, how far,
// and whether it is totalled.
type routeKey struct {
	table, context, target, field string
	reach                         reach
	totals                        bool
}

type resolvedRoute struct {
	route *route
	err   error
}

// route resolves a path to a related field, once for the request: the path,
// the tables it crosses being readable, the field's name where it is, and
// what each relationship reads.
func (r *relatedReader) route(ctx context.Context, key routeKey) (*route, error) {
	if known, ok := r.routes[key]; ok {
		return known.route, known.err
	}
	rt, err := r.resolveRoute(ctx, key)
	r.routes[key] = resolvedRoute{rt, err}
	return rt, err
}

func (r *relatedReader) resolveRoute(ctx context.Context, key routeKey) (*route, error) {
	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	from, err := graph.contextOf(key.table, key.context)
	if err != nil {
		return nil, err
	}
	to, ok := graph.byName[strings.ToLower(key.target)]
	if !ok {
		return nil, fmt.Errorf("there is no table occurrence %q", key.target)
	}
	path, err := graph.path(from, to)
	if err != nil {
		return nil, err
	}
	if err := r.checkPath(ctx, path); err != nil {
		return nil, err
	}
	end, err := r.endpoint(ctx, to, key.field)
	if err != nil {
		return nil, err
	}
	rt := &route{path: path, end: end, reach: key.reach, totals: key.totals,
		plans: make([]*hopPlan, len(path)), columns: make([]fieldSet, len(path))}
	for i, h := range path {
		// A total needs no order.
		if rt.plans[i], err = r.plan(ctx, h, i == len(path)-1 && !key.totals); err != nil {
			return nil, err
		}
	}
	for i := range path {
		if i < len(path)-1 {
			rt.columns[i] = rt.plans[i].fields.pick(append([]string{"id"}, sourceColumns(rt.plans[i+1].terms)...))
			continue
		}
		if rt.columns[i], err = end.columns(ctx, rt.plans[i]); err != nil {
			return nil, err
		}
	}
	return rt, nil
}

// walk follows a route from rows, the records in hand, to the records it
// ends at, each once.
func (r *relatedReader) walk(ctx context.Context, rt *route, rows []map[string]interface{}) ([]map[string]interface{}, error) {
	var err error
	for i, h := range rt.path {
		rc := reachAll
		if i == len(rt.path)-1 {
			rc = rt.reach
		}
		if rows, err = r.follow(ctx, h, rt.plans[i], rows, rc, rt.columns[i]); err != nil || len(rows) == 0 {
			return nil, err
		}
	}
	return rows, nil
}

// checkPath checks that every table a path crosses is readable, not only the
// last: a path through a table the caller may not see would tell what it
// holds. A table of another solution must be readable there, by the account
// the caller's session group holds in it.
func (r *relatedReader) checkPath(ctx context.Context, path []hop) error {
	for _, h := range path {
		if h.to.External != nil {
			if _, err := r.open(ctx, *h.to.External); err != nil {
				return err
			}
			continue
		}
		if err := r.checkRead(ctx, h.to.TableName); err != nil {
			return err
		}
	}
	return nil
}

// pathEnd is where a related field is read: the reader of the solution the
// occurrence's records are in, that solution's name when it is another one,
// its table there, and the field's name and the table's fields there.
type pathEnd struct {
	reader   *relatedReader
	database string
	table    string
	field    string
	fields   fieldSet
}

// endpoint resolves the field of an occurrence a path ends at. A field of
// another solution known only by its FileMaker id is the one bound to it.
func (r *relatedReader) endpoint(ctx context.Context, o occurrence, field string) (pathEnd, error) {
	end := pathEnd{reader: r, table: o.table, field: field}
	wrap := func(err error) error { return err }
	if o.external != nil {
		reader, err := r.open(ctx, *o.external)
		if err != nil {
			return end, err
		}
		end.reader, end.database, wrap = reader, o.external.Database, o.external.Wrap
		if end.field, err = reader.s.ResolveFieldMarker(ctx, end.table, field); err != nil {
			return end, wrap(err)
		}
	}
	fields, err := end.reader.tableFields(ctx, end.table)
	if err != nil {
		return end, wrap(err)
	}
	if err := checkValueKey(fields, end.table, end.field); err != nil {
		return end, wrap(err)
	}
	end.fields = fields
	return end, nil
}

// base is the field a key of a record is of: the field itself, or the
// repeating field "field[n]" is a repetition of.
func (e pathEnd) base() string {
	if base, _, ok := repetition.Split(e.field); ok {
		return base
	}
	return e.field
}

// unstored reports whether the field is an unstored calculation.
func (e pathEnd) unstored(ctx context.Context) (bool, error) {
	calculations, err := e.reader.tableCalculations(ctx, e.table)
	if err != nil {
		return false, err
	}
	base := e.base()
	for _, c := range calculations {
		if c.unstored && c.field == base {
			return true, nil
		}
	}
	return false, nil
}

// columns are the fields read of the records a path ends at: the field, its
// repetitions and what the relationship sorts by; or every field when it is
// an unstored calculation, which reads the others.
func (e pathEnd) columns(ctx context.Context, p *hopPlan) (fieldSet, error) {
	unstored, err := e.unstored(ctx)
	if err != nil || unstored {
		return nil, err
	}
	base := e.base()
	return p.fields.pick(append([]string{"id", base, repetitionsKey(base)}, p.sorts...)), nil
}

// pick is the part of a fieldSet with these names, those it has.
func (f fieldSet) pick(names []string) fieldSet {
	out := fieldSet{}
	for _, name := range names {
		if t, ok := f[name]; ok {
			out[name] = t
		}
	}
	return out
}

// signature names a set of fields read, nil for all of them.
func (f fieldSet) signature() string {
	if f == nil {
		return "*"
	}
	names := make([]string, 0, len(f))
	for name := range f {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// RelatedValues reads fields of the records of the occurrence target that
// are related to the record recordID of the occurrence from, across as many
// relationships as the path between the two takes, each record once. It is
// what a value list that includes only related values offers (#31). The
// path is the one an unstored calculation reading target from from would
// follow, and every table on it, the first included, must be readable.
// Each value is given as the text a client shows for its field (fieldText).
func (s *Service) RelatedValues(ctx context.Context, from, recordID, target string, fields ...string) ([]map[string]string, error) {
	r := s.newRelatedReader()
	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	start, ok := graph.byID[from]
	if !ok {
		return nil, fmt.Errorf("there is no table occurrence %q", from)
	}
	end, ok := graph.byID[target]
	if !ok {
		return nil, fmt.Errorf("there is no table occurrence %q", target)
	}
	path, err := graph.path(start, end)
	if err != nil {
		return nil, err
	}
	if start.external != nil {
		return nil, start.external.Unsupported("a value list of related values from a record of another solution")
	}
	if err := r.checkRead(ctx, start.table); err != nil {
		return nil, err
	}
	if err := r.checkPath(ctx, path); err != nil {
		return nil, err
	}
	ends := make([]pathEnd, len(fields))
	for i, field := range fields {
		if ends[i], err = r.endpoint(ctx, end, field); err != nil {
			return nil, err
		}
	}

	row, err := s.GetRow(ctx, start.table, recordID)
	if err != nil {
		return nil, err
	}
	rows := []map[string]interface{}{row}
	for i, h := range path {
		p, err := r.plan(ctx, h, i == len(path)-1)
		if err != nil {
			return nil, err
		}
		if rows, err = r.follow(ctx, h, p, rows, reachAll, nil); err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
	}
	if len(ends) > 0 {
		names := make([]string, len(ends))
		for i, e := range ends {
			names[i] = e.field
		}
		if err := ends[0].reader.fill(ctx, ends[0].table, rows, names); err != nil {
			return nil, err
		}
	}
	out := make([]map[string]string, len(rows))
	for i, row := range rows {
		out[i] = make(map[string]string, len(fields))
		for j, field := range fields {
			out[i][field] = fieldText(ends[j].fields[ends[j].field], row[ends[j].field])
		}
	}
	return out, nil
}

// hopPlan is what reading across one relationship needs, resolved once: the
// criteria seen from the side in hand, with the fields of another solution
// resolved to their names there, and the reader, table, fields and order of
// the side reached.
type hopPlan struct {
	key    string
	terms  []matchTerm
	to     *relatedReader
	table  string
	fields fieldSet
	order  string
	sorts  []string
}

// plan resolves a hop, in the relationship's sort order when it is the last
// step of a path.
func (r *relatedReader) plan(ctx context.Context, h hop, last bool) (*hopPlan, error) {
	key := fmt.Sprintf("%s\x00%s\x00%t", h.rel.ID, h.to.OccurrenceID, last)
	if p, ok := r.plans[key]; ok {
		return p, nil
	}
	terms, err := h.rel.matchTerms(h.from)
	if err != nil {
		return nil, err
	}
	to, err := r.side(ctx, h.to)
	if err != nil {
		return nil, err
	}
	// A predicate on a field of another solution known only by its
	// FileMaker id names it by marker: it reads the field bound to that id.
	if h.from.External != nil {
		from, err := r.side(ctx, h.from)
		if err != nil {
			return nil, err
		}
		for i := range terms {
			if terms[i].SourceColumn, err = from.s.ResolveFieldMarker(ctx, h.from.TableName, terms[i].SourceColumn); err != nil {
				return nil, wrapSide(h.from, err)
			}
		}
	}
	if h.to.External != nil {
		for i := range terms {
			if terms[i].TargetColumn, err = to.s.ResolveFieldMarker(ctx, h.to.TableName, terms[i].TargetColumn); err != nil {
				return nil, wrapSide(h.to, err)
			}
		}
	}
	fields, err := to.tableFields(ctx, h.to.TableName)
	if err != nil {
		return nil, wrapSide(h.to, err)
	}
	for _, t := range terms {
		if err := checkField(fields, h.to.TableName, t.TargetColumn); err != nil {
			return nil, wrapSide(h.to, err)
		}
	}
	p := &hopPlan{key: key, terms: terms, to: to, table: h.to.TableName, fields: fields}
	if last {
		order := relatedSortOrder(h.rel, h.to, fields, QueryOptions{})
		calculations, err := to.tableCalculations(ctx, h.to.TableName)
		if err != nil {
			return nil, wrapSide(h.to, err)
		}
		if err := refuseUnstored(calculations, h.to.TableName, "sort", sortFields(order)...); err != nil {
			return nil, err
		}
		if p.order, err = orderByClause(to.s.driver.Dialect(), fields, h.to.TableName, order); err != nil {
			return nil, err
		}
		p.sorts = sortFields(order)
	}
	r.plans[key] = p
	return p, nil
}

// side is the reader of the solution a side's records are in.
func (r *relatedReader) side(ctx context.Context, side RelationSide) (*relatedReader, error) {
	if side.External == nil {
		return r, nil
	}
	return r.open(ctx, *side.External)
}

// hopMemo names what one record in hand reads across a relationship: the
// plan, how far, which fields, and what it holds in its match fields.
func hopMemo(p *hopPlan, rc reach, columns fieldSet, row map[string]interface{}) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\x00%d\x00%s", p.key, rc, columns.signature())
	for _, column := range sourceColumns(p.terms) {
		fmt.Fprintf(&b, "\x00%v", row[column])
	}
	return b.String()
}

// follow reads, across one relationship, the records related to any of rows,
// each once, in the relationship's sort order when it is the last step: the
// fields in columns (every field when nil), of every related record or of
// the first of each record in hand. A record is related when it meets every
// criterion of the relationship. The records of every key not read yet are
// read together (fetch).
func (r *relatedReader) follow(
	ctx context.Context, h hop, p *hopPlan, rows []map[string]interface{}, rc reach, columns fieldSet,
) ([]map[string]interface{}, error) {
	var missing []map[string]interface{}
	var memos []string
	queued := map[string]bool{}
	for _, row := range rows {
		memo := hopMemo(p, rc, columns, row)
		if _, done := r.hops[memo]; done || queued[memo] {
			continue
		}
		queued[memo] = true
		missing, memos = append(missing, row), append(memos, memo)
	}
	if err := r.fetch(ctx, h, p, rc, columns, missing, memos); err != nil {
		return nil, err
	}

	var out []map[string]interface{}
	seen := map[string]bool{}
	for _, row := range rows {
		memo := hopMemo(p, rc, columns, row)
		if r.overflow[memo] {
			return nil, wrapSide(h.to, fmt.Errorf("%w through %s from one record; narrow the relationship",
				ErrTooManyRelated, h.rel.Name))
		}
		for _, rel := range r.hops[memo] {
			id := fmt.Sprint(rel["id"])
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, rel)
		}
	}
	return out, nil
}

// Bounds of one statement reading ahead across a relationship: at most so
// many records in hand, and so many match values.
const (
	maxFetchBranches = 500
	maxFetchKeys     = 10000
)

// batch runs, where the related records are, one branch per record in hand,
// in statements of at most maxFetchBranches branches and about maxFetchKeys
// match values, and hands each statement's rows to read with the memos of
// its branches. A record whose match fields are empty relates to nothing:
// empty gets its memo, and it has no branch.
func (r *relatedReader) batch(
	ctx context.Context, h hop, p *hopPlan, rows []map[string]interface{}, memos []string,
	branch func(n int, match string) string, statement func(branches []string) string, fields fieldSet,
	empty func(memo string), read func(pending []string, related []map[string]interface{}),
) error {
	dialect := p.to.s.driver.Dialect()
	var branches, pending []string
	var args []interface{}
	flush := func() error {
		if len(branches) == 0 {
			return nil
		}
		res, err := p.to.s.driver.DB().QueryContext(ctx, statement(branches), args...)
		if err != nil {
			return wrapSide(h.to, fmt.Errorf("failed reading the records related through %s: %w", h.rel.Name, err))
		}
		related, err := rowsToMaps(res, fields)
		res.Close()
		if err != nil {
			return err
		}
		read(pending, related)
		branches, args, pending = nil, nil, nil
		return nil
	}
	for i, row := range rows {
		match, keys := matchCondition(dialect, p.terms, func(c string) interface{} { return row[c] }, len(args)+1)
		if match == "" {
			empty(memos[i])
			continue
		}
		branches = append(branches, branch(len(pending), match))
		args, pending = append(args, keys...), append(pending, memos[i])
		if len(branches) >= maxFetchBranches || len(args) >= maxFetchKeys {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// fetch reads across a relationship the related records of each of rows,
// whose memos are what each holds in its match fields, and keeps them in
// hops: the first of each, or at most maxRelatedRecords, marking in overflow
// a record that has more. All of them are read in one statement where the
// related records are (in a few for a large page), one branch per record
// with the criteria and order a single record's read has, as
// RelatedRowsBatch does: a page of records reads a relationship once, not
// once per record (no N+1).
func (r *relatedReader) fetch(
	ctx context.Context, h hop, p *hopPlan, rc reach, columns fieldSet, rows []map[string]interface{}, memos []string,
) error {
	if len(rows) == 0 {
		return nil
	}
	if columns == nil {
		columns = p.fields
	}
	dialect := p.to.s.driver.Dialect()
	list, table := selectList(dialect, columns), dialect.QuoteIdentifier(p.table)
	limit := maxRelatedRecords + 1
	if rc == reachFirst {
		limit = 1
	}
	order := ""
	if p.order != "" {
		order = " " + p.order
	}
	outer := "ORDER BY " + batchBranch
	if rest, ok := strings.CutPrefix(p.order, "ORDER BY "); ok {
		outer += ", " + rest
	}
	return r.batch(ctx, h, p, rows, memos,
		func(n int, match string) string {
			return fmt.Sprintf("(SELECT %d AS %s, %s FROM %s WHERE %s%s LIMIT %d)",
				n, batchBranch, list, table, match, order, limit)
		},
		func(branches []string) string {
			return fmt.Sprintf("SELECT * FROM (%s) f4b_batch %s", strings.Join(branches, " UNION ALL "), outer)
		},
		columns,
		func(memo string) { r.hops[memo] = nil },
		func(pending []string, related []map[string]interface{}) {
			for _, memo := range pending {
				r.hops[memo] = []map[string]interface{}{}
			}
			for _, row := range related {
				branch := branchOf(row[batchBranch])
				delete(row, batchBranch)
				if branch < 0 || branch >= len(pending) {
					continue
				}
				memo := pending[branch]
				if len(r.hops[memo]) == maxRelatedRecords {
					r.overflow[memo] = true
					continue
				}
				r.hops[memo] = append(r.hops[memo], row)
			}
		})
}

// totalled reports whether a route can total its field for function where
// the records are, with the same answer as the formula computing it from
// every value: a stored field, of the records one relationship away, of a
// type the database totals as the formula does.
func (rt *route) totalled(function string) bool {
	if len(rt.path) != 1 || rt.end.base() != rt.end.field {
		return false
	}
	t := rt.end.fields[rt.end.field]
	if rt.columns[0] == nil { // an unstored calculation
		return false
	}
	switch function {
	case "COUNT":
		switch t {
		case dbal.FieldTypeText, dbal.FieldTypeNumber, dbal.FieldTypeDate, dbal.FieldTypeTimestamp, dbal.FieldTypeBoolean:
			return true
		}
	case "SUM", "AVERAGE":
		return t == dbal.FieldTypeNumber
	case "MIN", "MAX":
		return ordered(t)
	}
	return false
}

// ordered reports whether the database orders values of a type as a formula
// does.
func ordered(t dbal.AgnosticFieldType) bool {
	return t == dbal.FieldTypeNumber || t == dbal.FieldTypeDate || t == dbal.FieldTypeTimestamp
}

// totalsMemo names the totals of a route's field for one record in hand.
func (r *relatedReader) totalsMemo(rt *route, row map[string]interface{}) string {
	return hopMemo(rt.plans[0], reachAll, nil, row) + "\x00totals\x00" + rt.end.field
}

// The columns a totals statement reads.
const (
	totalRecords = "f4b_records"
	totalValues  = "f4b_values"
	totalSum     = "f4b_sum"
	totalMin     = "f4b_min"
	totalMax     = "f4b_max"
)

// fetchTotals totals a route's field across the records related to each of
// rows not totalled yet, and keeps the totals in sums: in one statement
// where the records are (in a few for a large page), one branch per record.
func (r *relatedReader) fetchTotals(ctx context.Context, rt *route, rows []map[string]interface{}) error {
	var missing []map[string]interface{}
	var memos []string
	queued := map[string]bool{}
	for _, row := range rows {
		memo := r.totalsMemo(rt, row)
		if _, done := r.sums[memo]; done || queued[memo] {
			continue
		}
		queued[memo] = true
		missing, memos = append(missing, row), append(memos, memo)
	}
	if len(missing) == 0 {
		return nil
	}
	h, p, end := rt.path[0], rt.plans[0], rt.end
	dialect := p.to.s.driver.Dialect()
	column := dialect.QuoteIdentifier(end.field)
	t := end.fields[end.field]
	// A value is there when the formula would see one: not NULL, nor empty
	// text.
	selects := []string{
		"COUNT(*) AS " + totalRecords,
		fmt.Sprintf("COUNT(CASE WHEN %s IS NOT NULL AND LENGTH(%s) > 0 THEN 1 END) AS %s",
			column, dialect.CastToText(column), totalValues),
	}
	fields := fieldSet{totalRecords: dbal.FieldTypeNumber, totalValues: dbal.FieldTypeNumber}
	if t == dbal.FieldTypeNumber {
		selects = append(selects, fmt.Sprintf("SUM(%s) AS %s", column, totalSum))
		fields[totalSum] = t
	}
	if ordered(t) {
		selects = append(selects, fmt.Sprintf("MIN(%s) AS %s", column, totalMin), fmt.Sprintf("MAX(%s) AS %s", column, totalMax))
		fields[totalMin], fields[totalMax] = t, t
	}
	list, table := strings.Join(selects, ", "), dialect.QuoteIdentifier(p.table)
	result := resultTypeOf(t)
	return r.batch(ctx, h, p, missing, memos,
		func(n int, match string) string {
			return fmt.Sprintf("SELECT %d AS %s, %s FROM %s WHERE %s", n, batchBranch, list, table, match)
		},
		func(branches []string) string { return strings.Join(branches, " UNION ALL ") },
		fields,
		func(memo string) { r.sums[memo] = calc.RelatedTotals{} },
		func(pending []string, totals []map[string]interface{}) {
			for _, row := range totals {
				branch := branchOf(row[batchBranch])
				if branch < 0 || branch >= len(pending) {
					continue
				}
				r.sums[pending[branch]] = calc.RelatedTotals{
					Records: countOf(row[totalRecords]),
					Values:  countOf(row[totalValues]),
					Sum:     calc.Typed(calc.ResultNumber, row[totalSum]),
					Min:     calc.Typed(result, row[totalMin]),
					Max:     calc.Typed(result, row[totalMax]),
				}
			}
		})
}

// countOf reads a COUNT the driver returned.
func countOf(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case []byte:
		i, _ := strconv.ParseInt(string(n), 10, 64)
		return i
	}
	return 0
}

// prefetch reads ahead what the unstored calculations needed of rows read
// through relationships, for all the rows at once: across each relationship
// of each path in one statement (follow), or the totals of a field in one
// (fetchTotals), then, the same way, what the calculations of the records
// reached read in their turn, in their own solution. Nothing is computed:
// when each record is then evaluated, what it reads is already there. Each
// calculation reads ahead once for a record, which also stops formulas that
// read each other in a ring. A path that cannot be read is left to the
// evaluation, which reports it.
func (r *relatedReader) prefetch(
	ctx context.Context, tableName string, calculations []calculation, needed map[string]bool,
	rows []map[string]interface{}, depth int,
) {
	if depth > maxRelatedDepth || len(rows) == 0 {
		return
	}
	for _, c := range calculations {
		if !c.unstored || !needed[c.field] {
			continue
		}
		for _, read := range c.expr.RelatedReads() {
			if read.Missing {
				continue
			}
			key := fmt.Sprintf("%s%s\x00%s\x00%s\x00%s\x00%d\x00%s",
				r.scope, tableName, c.field, read.Table, read.Field, read.Use, read.Function)
			done := r.prefetched[key]
			if done == nil {
				done = map[string]bool{}
				r.prefetched[key] = done
			}
			var fresh []map[string]interface{}
			for _, row := range rows {
				if id := fmt.Sprint(row["id"]); !done[id] {
					done[id] = true
					fresh = append(fresh, row)
				}
			}
			r.prefetchRead(ctx, tableName, c.occurrence, read, fresh, depth)
		}
	}
}

// prefetchRead reads ahead what one way of reading a related field reads
// for rows, and what the field read there reads in its turn.
func (r *relatedReader) prefetchRead(
	ctx context.Context, tableName, contextName string, read calc.RelatedRead, rows []map[string]interface{}, depth int,
) {
	if len(rows) == 0 {
		return
	}
	if read.Use == calc.UseTotals {
		rt, err := r.route(ctx, routeKey{tableName, contextName, read.Table, read.Field, reachAll, true})
		if err != nil {
			return
		}
		if rt.totalled(read.Function) {
			_ = r.fetchTotals(ctx, rt, rows)
			return
		}
	}
	rc := reachAll
	if read.Use == calc.UseFirst {
		rc = reachFirst
	}
	rt, err := r.route(ctx, routeKey{tableName, contextName, read.Table, read.Field, rc, false})
	if err != nil {
		return
	}
	if rows, err = r.walk(ctx, rt, rows); err != nil || len(rows) == 0 {
		return
	}
	end := rt.end
	calculations, err := end.reader.tableCalculations(ctx, end.table)
	if err != nil {
		return
	}
	end.reader.prefetch(ctx, end.table, calculations, neededCalculations(calculations, []string{end.field}), rows, depth+1)
}
