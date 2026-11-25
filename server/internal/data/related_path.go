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
