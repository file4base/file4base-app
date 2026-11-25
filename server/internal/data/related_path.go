package data

import (
	"context"
	"fmt"
	"strings"
)

// FirstRelatedAlong reads, for each of recordIDs, records of tableName seen
// from the occurrence contextName, the first record of the last occurrence of
// path related to it, reaching each occurrence of path in turn: what
// FileMaker shows in a field of that occurrence placed on a layout of
// contextName when the occurrence is several relationships away (an
// anchor–buoy graph). The records reached are ordered all together, in the
// last relationship's sort order then the order they were created, as an
// unstored calculation reading the field across that path orders them.
//
// path names occurrences, the first one relationship away from contextName,
// each joined to the one before it; it is never searched for, so a graph
// with several ways between two occurrences reads the one the layout was
// built on. Every table on the path must be readable. Each record asked for
// has an entry, nil when nothing is related to it; the record given is the
// one GetRow gives, so it can be shown and edited like any other.
func (s *Service) FirstRelatedAlong(
	ctx context.Context, tableName, contextName string, path []string, recordIDs []string,
) (map[string]map[string]interface{}, error) {
	if len(recordIDs) > MaxRelatedBatch {
		return nil, ErrBatchTooLarge
	}
	w, err := s.walkOf(ctx, tableName, contextName, path)
	if err != nil {
		return nil, err
	}
	r, hops, plans, columns := w.reader, w.hops, w.plans, w.columns

	out := make(map[string]map[string]interface{}, len(recordIDs))
	for _, id := range recordIDs {
		out[id] = nil
	}
	if len(recordIDs) == 0 {
		return out, nil
	}
	values, err := s.matchValuesOf(ctx, tableName, sourceColumns(plans[0].terms), recordIDs)
	if err != nil {
		return nil, err
	}
	starts := make([]map[string]interface{}, 0, len(values))
	for _, id := range recordIDs {
		if row, ok := values[id]; ok {
			row["id"] = id
			starts = append(starts, row)
		}
	}

	// Each relationship is followed for every record at once, so the walk
	// of each record below reads what has been fetched (follow's memo).
	rows := starts
	for i, h := range hops {
		if len(rows) == 0 {
			break
		}
		if rows, err = r.follow(ctx, h, plans[i], rows, reachAll, columns[i]); err != nil {
			return nil, err
		}
	}

	last := plans[len(plans)-1]
	firstOf := map[string]string{}
	for _, start := range starts {
		reached := []map[string]interface{}{start}
		for i, h := range hops {
			if reached, err = r.follow(ctx, h, plans[i], reached, reachAll, columns[i]); err != nil {
				return nil, err
			}
			if len(reached) == 0 {
				break
			}
		}
		if len(reached) == 0 {
			continue
		}
		if len(hops) > 1 {
			// follow gives a slice of its own, so sorting it leaves the
			// memo as it was.
			last.sortReached(reached)
		}
		firstOf[fmt.Sprint(start["id"])] = fmt.Sprint(reached[0]["id"])
	}

	end := hops[len(hops)-1].to
	records := map[string]map[string]interface{}{}
	for start, id := range firstOf {
		record, known := records[id]
		if !known {
			if record, err = last.to.s.GetRow(ctx, end.TableName, id); err != nil {
				return nil, wrapSide(end, err)
			}
			records[id] = record
		}
		out[start] = record
	}
	return out, nil
}

// MaxRecordsAlong is the most records RelatedAlong returns.
const MaxRecordsAlong = 1000

// RelatedAlong returns every record of the last occurrence of path related
// to the record recordID of tableName seen from the occurrence contextName,
// following path as FirstRelatedAlong does: what a portal of an occurrence
// several relationships away from its layout's lists (#26). A record reached
// several ways is listed once. They are in the last relationship's sort
// order, then the order they were created, and at most MaxRecordsAlong of
// them are returned, the first ones in that order; more tells whether there
// were others. A sort given (a portal's) then orders those.
func (s *Service) RelatedAlong(
	ctx context.Context, tableName, contextName string, path []string, recordID string, sort []SortField,
) (records []map[string]interface{}, more bool, err error) {
	w, err := s.walkOf(ctx, tableName, contextName, path)
	if err != nil {
		return nil, false, err
	}
	values, err := s.matchValuesOf(ctx, tableName, sourceColumns(w.plans[0].terms), []string{recordID})
	if err != nil {
		return nil, false, err
	}
	start, ok := values[recordID]
	if !ok {
		return nil, false, fmt.Errorf("%w: %s", ErrRecordNotFound, recordID)
	}
	start["id"] = recordID
	reached := []map[string]interface{}{start}
	for i, h := range w.hops {
		if reached, err = w.reader.follow(ctx, h, w.plans[i], reached, reachAll, w.columns[i]); err != nil {
			return nil, false, err
		}
		if len(reached) == 0 {
			return []map[string]interface{}{}, false, nil
		}
	}
	last := w.plans[len(w.plans)-1]
	if len(w.hops) > 1 {
		last.sortReached(reached)
	}
	ids := make([]string, 0, len(reached))
	seen := map[string]bool{}
	for _, row := range reached {
		id := fmt.Sprint(row["id"])
		if seen[id] {
			continue
		}
		if len(ids) == MaxRecordsAlong {
			more = true
			break
		}
		seen[id] = true
		ids = append(ids, id)
	}
	end := w.hops[len(w.hops)-1].to
	if len(sort) > 0 {
		fields, err := last.to.s.tableFields(ctx, end.TableName)
		if err != nil {
			return nil, false, wrapSide(end, err)
		}
		for _, level := range sort {
			if err := checkField(fields, end.TableName, level.Field); err != nil {
				return nil, false, err
			}
		}
		defer func() {
			if err == nil {
				sortRecords(last.to.s.driver.Dialect().Engine(), fields, sort, records)
			}
		}()
	}
	if records, err = last.to.s.rowsByIDs(ctx, end.TableName, ids); err != nil {
		return nil, false, wrapSide(end, err)
	}
	return records, more, nil
}

// rowsByIDs reads the records of a table given by id, as GetRow reads each,
// in the order given; an id no record has is left out.
func (s *Service) rowsByIDs(ctx context.Context, tableName string, ids []string) ([]map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	byID := make(map[string]map[string]interface{}, len(ids))
	for begin := 0; begin < len(ids); begin += 500 {
		chunk := ids[begin:min(begin+500, len(ids))]
		marks := make([]string, len(chunk))
		args := make([]interface{}, len(chunk))
		for i, id := range chunk {
			marks[i] = dialect.Placeholder(i + 1)
			args[i] = id
		}
		query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)", selectList(dialect, fields),
			dialect.QuoteIdentifier(tableName), dialect.QuoteIdentifier("id"), strings.Join(marks, ", "))
		rows, err := s.driver.DB().QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed reading records of %s: %w", tableName, err)
		}
		read, err := s.readRows(ctx, tableName, rows, fields)
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
		for _, row := range read {
			byID[fmt.Sprint(row["id"])] = row
		}
	}
	out := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// pathWalk is what following a path of occurrences needs, resolved once:
// each relationship, how it is read, and the columns read on the way.
type pathWalk struct {
	reader  *relatedReader
	hops    []hop
	plans   []*hopPlan
	columns []fieldSet
}

// walkOf resolves path from the occurrence contextName of tableName (see
// FirstRelatedAlong), checking every table on it can be read.
func (s *Service) walkOf(ctx context.Context, tableName, contextName string, path []string) (*pathWalk, error) {
	if len(path) == 0 {
		return nil, fmt.Errorf("the occurrences to follow must be given")
	}
	r := s.newRelatedReader()
	graph, err := r.occurrenceGraph(ctx)
	if err != nil {
		return nil, err
	}
	at, err := graph.contextOf(tableName, contextName)
	if err != nil {
		return nil, err
	}
	hops := make([]hop, len(path))
	for i, name := range path {
		next, ok := graph.byName[strings.ToLower(strings.TrimSpace(name))]
		if !ok {
			return nil, fmt.Errorf("there is no table occurrence %q", name)
		}
		found := false
		for _, h := range graph.edges[at.id] {
			if h.to.OccurrenceID == next.id {
				hops[i], found = h, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: none joins %s to %s", ErrRelationshipNotFound, at.name, next.name)
		}
		at = next
	}
	if err := r.checkPath(ctx, hops); err != nil {
		return nil, err
	}
	plans := make([]*hopPlan, len(hops))
	for i, h := range hops {
		if plans[i], err = r.plan(ctx, h, i == len(hops)-1); err != nil {
			return nil, err
		}
	}
	// Only what the next relationship matches on is read on the way, and
	// the records' ids and order at the end.
	columns := make([]fieldSet, len(hops))
	for i := range hops {
		if i < len(hops)-1 {
			columns[i] = plans[i].fields.pick(append([]string{"id"}, sourceColumns(plans[i+1].terms)...))
		} else {
			columns[i] = plans[i].fields.pick(append([]string{"id"}, plans[i].sorts...))
		}
	}

	return &pathWalk{reader: r, hops: hops, plans: plans, columns: columns}, nil
}
