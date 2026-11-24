package data

import (
	"context"
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

// relatedReader computes unstored calculations for one request. It loads the
// relationship graph and each table's calculations once and keeps them for
// the request only, so a change to the schema is seen by the next one.
type relatedReader struct {
	s      *Service
	graph  *occurrenceGraph
	calcs  map[string][]calculation
	fields map[string]fieldSet
	// hops keeps the records one key leads to across one relationship, so a
	// list whose records share a key, or several formulas reading through the
	// same relationship, query it once.
	hops map[string][]map[string]interface{}
	// values keeps each unstored field of each record once it is computed,
	// so a record reached again — through another relationship or another
	// formula — is not computed again; computing marks the ones under way,
	// so formulas that read each other through relationships stop.
	values    map[string]interface{}
	computing map[string]bool
	readable  map[string]error
	depth     int
}

func (s *Service) newRelatedReader() *relatedReader {
	return &relatedReader{s: s, calcs: map[string][]calculation{}, fields: map[string]fieldSet{},
		hops: map[string][]map[string]interface{}{}, values: map[string]interface{}{},
		computing: map[string]bool{}, readable: map[string]error{}}
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
	if len(needed) == 0 {
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()

	for _, row := range rows {
		for _, c := range calculations {
			if !c.unstored || !needed[c.field] {
				continue
			}
			key := tableName + "\x00" + fmt.Sprint(row["id"]) + "\x00" + c.field
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

	// Every table the path crosses must be readable, not only the last: a
	// path through a table the caller may not see would tell what it holds.
	for _, h := range path {
		if err := r.checkRead(ctx, h.to.TableName); err != nil {
			return nil, err
		}
	}
	fields, err := r.tableFields(ctx, to.table)
	if err != nil {
		return nil, err
	}
	if err := checkValueKey(fields, to.table, field); err != nil {
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
	// Only the field read is computed, with what it reads.
	if err := r.fill(ctx, to.table, rows, []string{field}); err != nil {
		return nil, err
	}
	out := make([]any, len(rows))
	for i, rel := range rows {
		out[i] = typedValue(to.table, fields, rel, field, r.s.describeContainer(ctx))
	}
	return out, nil
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
	if err := r.checkRead(ctx, start.table); err != nil {
		return nil, err
	}
	for _, h := range path {
		if err := r.checkRead(ctx, h.to.TableName); err != nil {
			return nil, err
		}
	}
	endFields, err := r.tableFields(ctx, end.table)
	if err != nil {
		return nil, err
	}
	for _, field := range fields {
		if err := checkField(endFields, end.table, field); err != nil {
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
	if err := r.fill(ctx, end.table, rows, fields); err != nil {
		return nil, err
	}
	out := make([]map[string]string, len(rows))
	for i, row := range rows {
		out[i] = make(map[string]string, len(fields))
		for _, field := range fields {
			out[i][field] = fieldText(endFields[field], row[field])
		}
	}
	return out, nil
}

// follow reads, across one relationship, the records related to any of rows,
// each once, in the relationship's sort order when it is the last step. A
// record is related when it meets every criterion of the relationship.
func (r *relatedReader) follow(ctx context.Context, h hop, rows []map[string]interface{}, last bool) ([]map[string]interface{}, error) {
	terms, err := h.rel.matchTerms(h.from)
	if err != nil {
		return nil, err
	}
	fields, err := r.tableFields(ctx, h.to.TableName)
	if err != nil {
		return nil, err
	}
	for _, t := range terms {
		if err := checkField(fields, h.to.TableName, t.TargetColumn); err != nil {
			return nil, err
		}
	}
	sources := sourceColumns(terms)
	var order []SortField
	if last {
		order = relatedSortOrder(h.rel, h.to, fields, QueryOptions{})
		calculations, err := r.tableCalculations(ctx, h.to.TableName)
		if err != nil {
			return nil, err
		}
		if err := refuseUnstored(calculations, h.to.TableName, "sort", sortFields(order)...); err != nil {
			return nil, err
		}
	}
	dialect := r.s.driver.Dialect()
	orderClause, err := orderByClause(dialect, fields, h.to.TableName, order)
	if err != nil {
		return nil, err
	}

	var out []map[string]interface{}
	seen := map[string]bool{}
	for _, row := range rows {
		memo := fmt.Sprintf("%s\x00%s\x00%t", h.rel.ID, h.to.OccurrenceID, last)
		for _, column := range sources {
			memo += fmt.Sprintf("\x00%v", row[column])
		}
		related, done := r.hops[memo]
		if !done {
			match, keys := matchCondition(dialect, terms, func(c string) interface{} { return row[c] }, 1)
			if match == "" {
				continue
			}
			q := fmt.Sprintf("SELECT %s FROM %s WHERE %s %s",
				selectList(dialect, fields), dialect.QuoteIdentifier(h.to.TableName), match, orderClause)
			res, err := r.s.driver.DB().QueryContext(ctx, q, keys...)
			if err != nil {
				return nil, fmt.Errorf("failed reading the records related through %s: %w", h.rel.Name, err)
			}
			related, err = rowsToMaps(res, fields)
			res.Close()
			if err != nil {
				return nil, err
			}
			r.hops[memo] = related
		}
		for _, rel := range related {
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

// occurrenceGraph is the relationship graph: table occurrences joined by
// relationships.
type occurrenceGraph struct {
	byID    map[string]occurrence
	byName  map[string]occurrence // lower-case name
	byTable map[string][]occurrence
	edges   map[string][]hop // occurrence id -> the steps out of it
}

type occurrence struct {
	id, name, table string
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
		`SELECT o.id, o.name, t.name FROM sys_table_occurrences o JOIN sys_tables t ON t.id = o.base_table_id ORDER BY o.created_at, o.name`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the table occurrences: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var o occurrence
		if err := rows.Scan(&o.id, &o.name, &o.table); err != nil {
			return nil, err
		}
		g.byID[o.id] = o
		g.byName[strings.ToLower(o.name)] = o
		g.byTable[o.table] = append(g.byTable[o.table], o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rels, err := r.s.Relationships(ctx)
	if err != nil {
		return nil, err
	}
	for _, rel := range rels {
		g.edges[rel.Left.OccurrenceID] = append(g.edges[rel.Left.OccurrenceID], hop{rel: rel, from: rel.Left, to: rel.Right})
		g.edges[rel.Right.OccurrenceID] = append(g.edges[rel.Right.OccurrenceID], hop{rel: rel, from: rel.Right, to: rel.Left})
	}
	r.graph = g
	return g, nil
}

// contextOf is the occurrence a calculation of tableName is evaluated from:
// the one it names, else the one named like the table, else its first.
func (g *occurrenceGraph) contextOf(tableName, name string) (occurrence, error) {
	if name != "" {
		if o, ok := g.byName[strings.ToLower(name)]; ok && o.table == tableName {
			return o, nil
		}
	}
	if o, ok := g.byName[strings.ToLower(tableName)]; ok && o.table == tableName {
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
				table = related[relatedFindKey(crit)].TableName
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
