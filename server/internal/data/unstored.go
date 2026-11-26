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
	// routes keeps each path to a related field, resolved; overflow marks
	// the hops whose record has more than maxRelatedRecords related records;
	// sums keeps the totals of a related field for one record (fetchTotals).
	routes   map[routeKey]resolvedRoute
	overflow map[string]bool
	sums     map[string]calc.RelatedTotals
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
		readable: map[string]error{}, opened: map[string]openedTable{},
		routes: map[routeKey]resolvedRoute{}, overflow: map[string]bool{}, sums: map[string]calc.RelatedTotals{}}
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
	if o, ok := g.byID[name]; ok && name != "" && local(o) {
		return o, nil
	}
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

// orderBy is orderByClause for a table that may have unstored calculations,
// with the records' creation order last: alone when nothing sorts them, as
// FileMaker lists them, and to keep records that sort alike in that order.
func (s *Service) orderBy(ctx context.Context, fields fieldSet, tableName string, order []SortField) (string, error) {
	if err := s.refuseUnstored(ctx, tableName, "sort", sortFields(order)...); err != nil {
		return "", err
	}
	clause, err := orderByClause(s.driver.Dialect(), fields, tableName, order)
	if err != nil {
		return "", err
	}
	creation, err := s.creationOrder(ctx, tableName)
	if err != nil {
		return "", err
	}
	return thenBy(s.driver.Dialect(), clause, creation, false), nil
}

// sortIndexOrder is, on SQLite, the order of a list of a table's records
// by a text field that has its sort index (dbal.SQLiteSortIndex), and the
// index: the sort reads it by the field's sort key, and sorts only the
// records that tie on the field by the rest of the order. index is "" when
// the list is sorted otherwise (orderBy). The index is named to SQLite,
// which does not use an index for a sort by more than one field unless told.
func (s *Service) sortIndexOrder(
	ctx context.Context, fields fieldSet, tableName string, order []SortField,
) (index, clause string, err error) {
	dialect := s.driver.Dialect()
	if dialect.Engine() != dbal.EngineSQLite || len(order) == 0 || fields[order[0].Field] != dbal.FieldTypeText {
		return "", "", nil
	}
	if index = dbal.SQLiteSortIndex(ctx, s.driver.DB(), tableName, order[0].Field); index == "" {
		return "", "", nil
	}
	first := order[0].Field
	if clause, err = orderByKeyed(dialect, fields, tableName, order, func(f string) bool { return f == first }); err != nil {
		return "", "", err
	}
	creation, err := s.creationOrder(ctx, tableName)
	if err != nil {
		return "", "", err
	}
	return index, thenBy(dialect, clause, creation, false), nil
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
