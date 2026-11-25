package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Unstored calculations, and the related records they read.
//
// FileMaker computes a calculation whose result is not stored every time it
// is shown, and it is the only kind that may read related records:
// Sum ( Lines::Amount ) changes when a line is added, without the order being
// written again. File4Base does the same. Such a field's column stays empty;
// every record the service returns has the field computed on the way out.
//
// A reference Occurrence::Field is read through the relationship graph, from
// the occurrence the calculation is evaluated in to the one it names, across
// as many relationships as the path takes.

// ErrUnstoredField is returned for a find, sort or summary on an unstored
// calculation: its value exists only once a record is read, so the database
// cannot search or order by it.
var ErrUnstoredField = errors.New("unstored calculation")

// maxRelatedDepth bounds how deep unstored calculations may read each other
// through relationships (Orders reading Lines reading Products…).
const maxRelatedDepth = 32

// ErrNoAccess is what reading a related field of a table the caller may not
// read evaluates to: FileMaker's <No Access>, never the data.
var ErrNoAccess = errors.New("<No Access>")

// relatedReader computes unstored calculations for one request, in one
// solution. It loads that solution's relationship graph and each table's
// calculations once and keeps them for the request only, so a change to the
// schema is seen by the next one. A calculation reading through an
// occurrence of another solution's table (#107) reads it with a reader of
// that solution, which evaluates that solution's own calculations there;
// all of them share the request's readState.
type relatedReader struct {
	s *Service
	*readState
	// scope tells the records of this solution apart from another's in the
	// shared state: empty for the solution the request is made in.
	scope  string
	graph  *occurrenceGraph
	calcs  map[string][]calculation
	fields map[string]fieldSet
	// hops keeps the records one key leads to across one relationship, so a
	// list whose records share a key, or several formulas reading through the
	// same relationship, query it once; plans keeps what reading across a
	// relationship needs, resolved once.
	hops     map[string][]map[string]interface{}
	plans    map[string]*hopPlan
	readable map[string]error
	// opened keeps the reader of each table of another solution an external
	// occurrence names, or why it cannot be read.
	opened map[string]openedTable
}

// readState is what the readers of every solution a request reaches share.
type readState struct {
	// values keeps each unstored field of each record once it is computed,
	// so a record reached again — through another relationship, another
	// formula or another solution — is not computed again; computing marks
	// the ones under way, so formulas that read each other through
	// relationships stop, across solutions as well.
	values    map[string]interface{}
	computing map[string]bool
	depth     int
	// readers holds the reader of each solution reached, by its database
	// handle, so a ring back to a solution already read reuses its reader.
	readers map[*sql.DB]*relatedReader
	// prefetched marks, for each calculation and related field it reads, the
	// records already read ahead for (see prefetch).
	prefetched map[string]map[string]bool
}

func (s *Service) newRelatedReader() *relatedReader {
	state := &readState{values: map[string]interface{}{}, computing: map[string]bool{},
		readers: map[*sql.DB]*relatedReader{}, prefetched: map[string]map[string]bool{}}
	return state.reader(s, "")
}

// reader is the reader of the solution s reads, the one already made when
// that solution was reached before.
func (st *readState) reader(s *Service, scope string) *relatedReader {
	db := s.driver.DB()
	if r, ok := st.readers[db]; ok {
		return r
	}
	r := &relatedReader{s: s, readState: st, scope: scope,
		calcs: map[string][]calculation{}, fields: map[string]fieldSet{},
		hops: map[string][]map[string]interface{}{}, plans: map[string]*hopPlan{},
		readable: map[string]error{}, opened: map[string]openedTable{}}
	st.readers[db] = r
	return r
}

// WithReadAccess makes the service ask, before a calculation reads a related
// table, whether the caller may read it. Without it every table is readable,
// which is what an administrator and the server's own work get.
func (s *Service) WithReadAccess(canRead func(ctx context.Context, tableName string) (bool, error)) *Service {
	s.canRead = canRead
	return s
}

// checkRead is nil when the caller may read tableName, ErrNoAccess when not.
func (r *relatedReader) checkRead(ctx context.Context, tableName string) error {
	if r.s.canRead == nil {
		return nil
	}
	if err, known := r.readable[tableName]; known {
		return err
	}
	ok, err := r.s.canRead(ctx, tableName)
	if err == nil && !ok {
		err = fmt.Errorf("%w: %s", ErrNoAccess, tableName)
	}
	r.readable[tableName] = err
	return err
}

// fillUnstored computes the unstored calculations of every row.
func (s *Service) fillUnstored(ctx context.Context, tableName string, rows []map[string]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	return s.newRelatedReader().fill(ctx, tableName, rows, nil)
}

func (r *relatedReader) tableCalculations(ctx context.Context, tableName string) ([]calculation, error) {
	if c, ok := r.calcs[tableName]; ok {
		return c, nil
	}
	c, err := r.s.tableCalculations(ctx, tableName)
	if err != nil {
		return nil, err
	}
	r.calcs[tableName] = c
	return c, nil
}

func (r *relatedReader) tableFields(ctx context.Context, tableName string) (fieldSet, error) {
	if f, ok := r.fields[tableName]; ok {
		return f, nil
	}
	f, err := r.s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	r.fields[tableName] = f
	return f, nil
}

// fill computes unstored calculations of rows of tableName: those named in
// want and the unstored ones they read, or all of them when want is nil. They
// are computed in the order the table's calculations depend on each other. A
// formula that fails leaves its field empty for that record, as FileMaker
// shows "?" rather than refusing to show the record.
func (r *relatedReader) fill(ctx context.Context, tableName string, rows []map[string]interface{}, want []string) error {
	calculations, err := r.tableCalculations(ctx, tableName)
	if err != nil {
		return err
	}
	needed := neededCalculations(calculations, want)
	if len(needed) == 0 {
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()
	if r.depth <= maxRelatedDepth {
		r.prefetch(ctx, tableName, calculations, needed, rows, r.depth)
	}

	for _, row := range rows {
		for _, c := range calculations {
			if !c.unstored || !needed[c.field] {
				continue
			}
			key := r.scope + tableName + "\x00" + fmt.Sprint(row["id"]) + "\x00" + c.field
			if v, done := r.values[key]; done {
				row[c.field] = v
				r.restoreRepetitions(key, c, row)
				continue
			}
			if r.computing[key] || r.depth > maxRelatedDepth {
				row[c.field] = nil // a formula that reads itself, through relationships
				continue
			}
			r.computing[key] = true
			env := &unstoredContext{
				RecordContext: c.recordContext(row),
				reader:        r, ctx: ctx, table: tableName, occurrence: c.occurrence, row: row,
			}
			answer, err := calc.EvaluateWith(c.expr, env, c.result)
			delete(r.computing, key)
			if err != nil {
				answer = nil
			} else {
				answer = rowValue(answer, c.types[c.field])
			}
			row[c.field] = answer
			r.values[key] = answer
			r.fillRepetitions(key, c, env, row)
		}
	}
	return nil
}

// neededCalculations are the unstored calculations named in want and the
// unstored ones they read, or all of them when want is nil.
func neededCalculations(calculations []calculation, want []string) map[string]bool {
	needed := map[string]bool{}
	for _, c := range calculations {
		if c.unstored && (want == nil || slices.Contains(want, c.field)) {
			needed[c.field] = true
		}
	}
	// What the wanted ones read, in reverse order of computation so a whole
	// chain is collected in one pass.
	for i := len(calculations) - 1; i >= 0; i-- {
		if c := calculations[i]; needed[c.field] {
			for _, read := range c.expr.Fields() {
				needed[read] = true
			}
		}
	}
	return needed
}

// rowValue writes a computed answer the way the same value is read back from
// a stored column, so a client cannot tell the two apart.
func rowValue(answer interface{}, fieldType dbal.AgnosticFieldType) interface{} {
	switch v := answer.(type) {
	case calc.Container:
		return encodeContainerRef(v)
	case json.Number:
		return dbal.TrimNumericText(v.String())
	case string:
		switch fieldType {
		case dbal.FieldTypeDate:
			if t, err := time.Parse("2006-01-02", v); err == nil {
				return t
			}
		case dbal.FieldTypeTimestamp:
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t
			}
		}
	}
	return answer
}

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
	return c.reader.related(c.ctx, c.table, c.occurrence, c.row, occurrence, field)
}

// related reads field in every record of the occurrence target that is
// related to row, a record of tableName seen from the occurrence contextName.
func (r *relatedReader) related(
	ctx context.Context, tableName, contextName string, row map[string]interface{}, target, field string,
) ([]any, error) {
	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	from, err := graph.contextOf(tableName, contextName)
	if err != nil {
		return nil, err
	}
	to, ok := graph.byName[strings.ToLower(target)]
	if !ok {
		return nil, fmt.Errorf("there is no table occurrence %q", target)
	}
	path, err := graph.path(from, to)
	if err != nil {
		return nil, err
	}
	if err := r.checkPath(ctx, path); err != nil {
		return nil, err
	}
	end, err := r.endpoint(ctx, to, field)
	if err != nil {
		return nil, err
	}

	rows := []map[string]interface{}{row}
	for i, h := range path {
		last := i == len(path)-1
		if rows, err = r.follow(ctx, h, rows, last); err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
	}
	// Only the field read is computed, with what it reads, in the solution
	// the records are in.
	if err := end.reader.fill(ctx, end.table, rows, []string{end.field}); err != nil {
		return nil, err
	}
	describe := end.reader.s.describeContainer(ctx)
	out := make([]any, len(rows))
	for i, rel := range rows {
		out[i] = typedValue(end.table, end.fields, rel, end.field, describe)
	}
	return out, nil
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
// occurrence's records are in, its table there, and the field's name and
// the table's fields there.
type pathEnd struct {
	reader *relatedReader
	table  string
	field  string
	fields fieldSet
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
		end.reader, wrap = reader, o.external.Wrap
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
		if rows, err = r.follow(ctx, h, rows, i == len(path)-1); err != nil {
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
	terms  []matchTerm
	to     *relatedReader
	table  string
	fields fieldSet
	order  string
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
	p := &hopPlan{terms: terms, to: to, table: h.to.TableName, fields: fields}
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

// follow reads, across one relationship, the records related to any of rows,
// each once, in the relationship's sort order when it is the last step. A
// record is related when it meets every criterion of the relationship. The
// records of every key not read yet are read together (fetch).
func (r *relatedReader) follow(ctx context.Context, h hop, rows []map[string]interface{}, last bool) ([]map[string]interface{}, error) {
	p, err := r.plan(ctx, h, last)
	if err != nil {
		return nil, err
	}
	sources := sourceColumns(p.terms)
	memoOf := func(row map[string]interface{}) string {
		memo := fmt.Sprintf("%s\x00%s\x00%t", h.rel.ID, h.to.OccurrenceID, last)
		for _, column := range sources {
			memo += fmt.Sprintf("\x00%v", row[column])
		}
		return memo
	}
	var missing []map[string]interface{}
	var memos []string
	queued := map[string]bool{}
	for _, row := range rows {
		memo := memoOf(row)
		if _, done := r.hops[memo]; done || queued[memo] {
			continue
		}
		queued[memo] = true
		missing, memos = append(missing, row), append(memos, memo)
	}
	if err := r.fetch(ctx, h, p, missing, memos); err != nil {
		return nil, err
	}

	var out []map[string]interface{}
	seen := map[string]bool{}
	for _, row := range rows {
		for _, rel := range r.hops[memoOf(row)] {
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

// fetch reads across a relationship the related records of each of rows,
// whose memos are what each holds in its match fields, and keeps them in
// hops. All of them are read in one statement where the related records are
// (in a few for a large page), one branch per record with the criteria and
// order a single record's read has, as RelatedRowsBatch does: a page of
// records reads a relationship once, not once per record (no N+1).
func (r *relatedReader) fetch(ctx context.Context, h hop, p *hopPlan, rows []map[string]interface{}, memos []string) error {
	dialect := p.to.s.driver.Dialect()
	columns, table := selectList(dialect, p.fields), dialect.QuoteIdentifier(p.table)
	order := "ORDER BY " + batchBranch
	if rest, ok := strings.CutPrefix(p.order, "ORDER BY "); ok {
		order += ", " + rest
	}
	var branches []string
	var args []interface{}
	var pending []string
	flush := func() error {
		if len(branches) == 0 {
			return nil
		}
		q := fmt.Sprintf("SELECT * FROM (%s) f4b_batch %s", strings.Join(branches, " UNION ALL "), order)
		res, err := p.to.s.driver.DB().QueryContext(ctx, q, args...)
		if err != nil {
			return wrapSide(h.to, fmt.Errorf("failed reading the records related through %s: %w", h.rel.Name, err))
		}
		related, err := rowsToMaps(res, p.fields)
		res.Close()
		if err != nil {
			return err
		}
		for _, memo := range pending {
			r.hops[memo] = []map[string]interface{}{}
		}
		for _, row := range related {
			branch := branchOf(row[batchBranch])
			delete(row, batchBranch)
			if branch >= 0 && branch < len(pending) {
				r.hops[pending[branch]] = append(r.hops[pending[branch]], row)
			}
		}
		branches, args, pending = nil, nil, nil
		return nil
	}
	for i, row := range rows {
		match, keys := matchCondition(dialect, p.terms, func(c string) interface{} { return row[c] }, len(args)+1)
		if match == "" {
			r.hops[memos[i]] = nil // an empty key relates to nothing
			continue
		}
		branches = append(branches, fmt.Sprintf("SELECT %d AS %s, %s FROM %s WHERE %s",
			len(pending), batchBranch, columns, table, match))
		args, pending = append(args, keys...), append(pending, memos[i])
		if len(branches) >= maxFetchBranches || len(args) >= maxFetchKeys {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

// prefetch reads ahead what the unstored calculations needed of rows read
// through relationships, for all the rows at once: across each relationship
// of each path in one statement (follow), then, the same way, what the
// calculations of the records reached read in their turn, in their own
// solution. Nothing is computed: when each record is then evaluated, the
// records it reads are already in hops. Each calculation reads ahead once
// for a record, which also stops formulas that read each other in a ring. A
// path that cannot be read is left to the evaluation, which reports it.
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
		for _, ref := range c.expr.RelatedFields() {
			if ref.Missing {
				continue
			}
			key := r.scope + tableName + "\x00" + c.field + "\x00" + ref.Table + "\x00" + ref.Field
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
			r.prefetchPath(ctx, tableName, c.occurrence, ref.Table, ref.Field, fresh, depth)
		}
	}
}

// prefetchPath reads ahead the records rows reach through the occurrence
// target, and what the field read there reads in its turn.
func (r *relatedReader) prefetchPath(
	ctx context.Context, tableName, contextName, target, field string, rows []map[string]interface{}, depth int,
) {
	if len(rows) == 0 {
		return
	}
	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return
	}
	from, err := graph.contextOf(tableName, contextName)
	if err != nil {
		return
	}
	to, ok := graph.byName[strings.ToLower(target)]
	if !ok {
		return
	}
	path, err := graph.path(from, to)
	if err != nil || r.checkPath(ctx, path) != nil {
		return
	}
	end, err := r.endpoint(ctx, to, field)
	if err != nil {
		return
	}
	for i, h := range path {
		if rows, err = r.follow(ctx, h, rows, i == len(path)-1); err != nil || len(rows) == 0 {
			return
		}
	}
	calculations, err := end.reader.tableCalculations(ctx, end.table)
	if err != nil {
		return
	}
	end.reader.prefetch(ctx, end.table, calculations, neededCalculations(calculations, []string{end.field}), rows, depth+1)
}

// occurrenceGraph is the relationship graph: table occurrences joined by
// relationships.
type occurrenceGraph struct {
	byID    map[string]occurrence
	byName  map[string]occurrence // lower-case name
	byTable map[string][]occurrence
	edges   map[string][]hop // occurrence id -> the steps out of it
}

// occurrence is a table occurrence of the graph. One of another solution's
// table (#107) has external set, and table is that solution's name for it:
// it is never taken for a table of this solution.
type occurrence struct {
	id, name, table string
	external        *ExternalTarget
}

// hop is one step across a relationship, from one of its sides to the other.
type hop struct {
	rel      *Relationship
	from, to RelationSide
}

func (r *relatedReader) occurrenceGraph(ctx context.Context) (*occurrenceGraph, error) {
	if r.graph != nil {
		return r.graph, nil
	}
	g := &occurrenceGraph{byID: map[string]occurrence{}, byName: map[string]occurrence{},
		byTable: map[string][]occurrence{}, edges: map[string][]hop{}}
	rows, err := r.s.driver.DB().QueryContext(ctx,
		`SELECT o.id, o.name, o.base_table_id, COALESCE(t.name, ''), COALESCE(o.external_table, ''),
			COALESCE(f.name, ''), COALESCE(f.database_name, '')
		FROM sys_table_occurrences o
		LEFT JOIN sys_tables t ON t.id = o.base_table_id
		LEFT JOIN sys_file_references f ON f.id = o.file_reference_id
		ORDER BY o.created_at, o.name`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the table occurrences: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var o occurrence
		var base sql.NullString
		var extTable, reference, database string
		if err := rows.Scan(&o.id, &o.name, &base, &o.table, &extTable, &reference, &database); err != nil {
			return nil, err
		}
		switch {
		case !base.Valid:
			o.table = extTable
			o.external = &ExternalTarget{Occurrence: o.name, Reference: reference, Database: database, Table: extTable}
		case o.table == "":
			continue // its table no longer exists
		default:
			g.byTable[o.table] = append(g.byTable[o.table], o)
		}
		g.byID[o.id] = o
		g.byName[strings.ToLower(o.name)] = o
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rels, err := r.s.Relationships(ctx)
	if err != nil {
		return nil, err
	}
	external, err := r.s.externalRelationships(ctx)
	if err != nil {
		return nil, err
	}
	for _, rel := range append(rels, external...) {
		g.edges[rel.Left.OccurrenceID] = append(g.edges[rel.Left.OccurrenceID], hop{rel: rel, from: rel.Left, to: rel.Right})
		g.edges[rel.Right.OccurrenceID] = append(g.edges[rel.Right.OccurrenceID], hop{rel: rel, from: rel.Right, to: rel.Left})
	}
	r.graph = g
	return g, nil
}

// contextOf is the occurrence a calculation of tableName is evaluated from:
// the one it names, else the one named like the table, else its first. It
// is always an occurrence of the table itself, never one of another
// solution's table of the same name.
func (g *occurrenceGraph) contextOf(tableName, name string) (occurrence, error) {
	local := func(o occurrence) bool { return o.external == nil && o.table == tableName }
	if name != "" {
		if o, ok := g.byName[strings.ToLower(name)]; ok && local(o) {
			return o, nil
		}
	}
	if o, ok := g.byName[strings.ToLower(tableName)]; ok && local(o) {
		return o, nil
	}
	if list := g.byTable[tableName]; len(list) > 0 {
		return list[0], nil
	}
	return occurrence{}, fmt.Errorf("table %s has no table occurrence to read related records from", tableName)
}

// path is the shortest chain of relationships from one occurrence to another.
func (g *occurrenceGraph) path(from, to occurrence) ([]hop, error) {
	if from.id == to.id {
		return nil, nil
	}
	prev := map[string]hop{}
	visited := map[string]bool{from.id: true}
	queue := []string{from.id}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, h := range g.edges[at] {
			next := h.to.OccurrenceID
			if visited[next] {
				continue
			}
			visited[next] = true
			prev[next] = h
			if next == to.id {
				var path []hop
				for id := to.id; id != from.id; id = prev[id].from.OccurrenceID {
					path = append([]hop{prev[id]}, path...)
				}
				return path, nil
			}
			queue = append(queue, next)
		}
	}
	return nil, fmt.Errorf("no relationship leads from %s to %s", from.name, to.name)
}

// refuseUnstored refuses to search, sort or summarise tableName by one of
// its unstored calculations: the column holds nothing, the value exists only
// once a record is read.
func refuseUnstored(calculations []calculation, tableName, what string, fields ...string) error {
	for _, c := range calculations {
		if c.unstored && slices.Contains(fields, c.field) {
			return fmt.Errorf("%w: cannot %s %s by %q, which is computed only when a record is shown; "+
				"store its result in Field Options, or %s by the fields it reads",
				ErrUnstoredField, what, tableName, c.field, what)
		}
	}
	return nil
}

func (s *Service) refuseUnstored(ctx context.Context, tableName, what string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return err
	}
	return refuseUnstored(calculations, tableName, what, fields...)
}

// orderBy is orderByClause for a table that may have unstored calculations.
func (s *Service) orderBy(ctx context.Context, fields fieldSet, tableName string, order []SortField) (string, error) {
	if err := s.refuseUnstored(ctx, tableName, "sort", sortFields(order)...); err != nil {
		return "", err
	}
	return orderByClause(s.driver.Dialect(), fields, tableName, order)
}

func sortFields(order []SortField) []string {
	names := make([]string, len(order))
	for i, level := range order {
		names[i] = level.Field
	}
	return names
}

// refuseUnstoredCriteria refuses a find on an unstored calculation of the
// table searched, or of a related table a criterion reaches through.
func (s *Service) refuseUnstoredCriteria(
	ctx context.Context, tableName string, requests []FindRequest, related map[string]relatedFindTarget,
) error {
	byTable := map[string][]string{}
	for _, req := range requests {
		for _, crit := range req.Criteria {
			if crit.FieldName == "" {
				continue
			}
			table := tableName
			if crit.isRelated() {
				target := related[relatedFindKey(crit)]
				if target.External != nil {
					continue // refused there, where the criterion is run
				}
				table = target.TableName
			}
			byTable[table] = append(byTable[table], crit.FieldName)
		}
	}
	for table, fields := range byTable {
		if err := s.refuseUnstored(ctx, table, "search", fields...); err != nil {
			return err
		}
	}
	return nil
}
