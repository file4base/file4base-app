package data

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// Finds on a field of related records of another solution's table (#107).
//
// A criterion on a related field of this solution's table is an EXISTS on
// the related table, which another solution's table cannot be part of. A
// criterion on a related field of another solution's table is run there
// instead, through that solution's data service as the account the caller's
// session group holds in it: it finds the related records that match, and
// returns what this solution's records must hold to be related to one of
// them. The records searched are then filtered with that, here. No statement
// joins two solutions.
//
// What is returned depends on the relationship's criteria, the ones other
// than × (which every pair of records meets):
//
//   - none: whether any related record matches;
//   - one <, ≤, > or ≥: the least, or greatest, match value of the records
//     found, since a record is related to one of them exactly when it is
//     related to that one;
//   - one ≠: up to two distinct match values, since two are enough for any
//     record to differ from one of them;
//   - otherwise, every distinct set of match values. They are bound once in
//     the statement on this solution, as a common table expression, however
//     many requests read them.
//
// A find carries back at most MaxExternalFindKeys match values in all, for
// every criterion on another solution's table it has.
//
// The rules are those of a find on a field of a related record of this
// solution: every criterion of the relationship must be met, an empty match
// field relates nothing on either side, and a text key holding several lines
// is compared as a whole, not as a multi-key.

// MaxExternalFindKeys is the most match values a find carries back from
// other solutions, for all its criteria on related fields of their tables.
const MaxExternalFindKeys = 10000

// ErrFindTooBroad is returned for a find whose criteria on related fields of
// other solutions' tables match related records holding more match values
// than MaxExternalFindKeys.
var ErrFindTooBroad = fmt.Errorf("a find on related fields of other solutions matches records holding more than %d match values",
	MaxExternalFindKeys)

// externalFind is a relationship a find reaches another solution's table
// through, and what each criterion on it found there.
type externalFind struct {
	target ExternalTarget
	peer   *Service
	found  map[string]externalMatch
	// sets names, for the statement being built, the common table
	// expression each criterion's match values are bound in.
	sets map[string]string
}

// externalMatch is what one criterion found in the other solution.
type externalMatch struct {
	kind  matchKind
	any   bool            // matchAny: some related record matches
	bound interface{}     // matchBound: the least or greatest match value, nil for none
	keys  [][]interface{} // matchTuples and matchDistinct: the distinct match values, by active term
}

// matchKind is how the records searched are filtered with what was found.
type matchKind int

const (
	matchAny matchKind = iota
	matchBound
	matchDistinct
	matchTuples
)

// activeTerms are the criteria other than ×.
func activeTerms(terms []matchTerm) []matchTerm {
	var out []matchTerm
	for _, t := range terms {
		if t.Op != cartesian {
			out = append(out, t)
		}
	}
	return out
}

// kindOf is how a find through these criteria is answered.
func kindOf(active []matchTerm) matchKind {
	switch {
	case len(active) == 0:
		return matchAny
	case len(active) > 1:
		return matchTuples
	}
	switch active[0].Op {
	case "<", "<=", ">", ">=":
		return matchBound
	case "<>":
		return matchDistinct
	}
	return matchTuples
}

// externalFindTarget resolves a relationship from tableName, a table of this
// solution, to another solution's table, opening that solution.
func (s *Service) externalFindTarget(ctx context.Context, tableName string, rel *Relationship, crit FindCriterion) (relatedFindTarget, error) {
	source, target, err := rel.Sides(tableName, strings.TrimSpace(crit.Occurrence))
	if err != nil {
		return relatedFindTarget{}, err
	}
	if target.External == nil || source.External != nil {
		return relatedFindTarget{}, rel.External().Unsupported("a find through a relationship between two other solutions")
	}
	terms, err := rel.matchTerms(source) // each clause is child op parent
	if err != nil {
		return relatedFindTarget{}, err
	}
	peer, err := s.Open(ctx, *target.External)
	if err != nil {
		return relatedFindTarget{}, err
	}
	// A predicate on a field known only by its FileMaker id reads the field
	// bound to that id.
	for i := range terms {
		if terms[i].TargetColumn, err = peer.ResolveFieldMarker(ctx, target.TableName, terms[i].TargetColumn); err != nil {
			return relatedFindTarget{}, wrapSide(target, err)
		}
	}
	fields, err := peer.tableFields(ctx, target.TableName)
	if err != nil {
		return relatedFindTarget{}, wrapSide(target, err)
	}
	for _, t := range terms {
		if err := checkField(fields, target.TableName, t.TargetColumn); err != nil {
			return relatedFindTarget{}, wrapSide(target, err)
		}
	}
	return relatedFindTarget{
		TableName:    target.TableName,
		Terms:        terms,
		Fields:       fields,
		Relationship: rel.Name,
		External:     &externalFind{target: *target.External, peer: peer, found: map[string]externalMatch{}, sets: map[string]string{}},
	}, nil
}

// findInOtherSolutions runs, in the other solution, every criterion on a
// related field of another solution's table, each once.
func findInOtherSolutions(ctx context.Context, requests []FindRequest, targets map[string]relatedFindTarget) error {
	budget := MaxExternalFindKeys
	for _, req := range requests {
		for _, crit := range req.Criteria {
			if crit.FieldName == "" || !crit.isRelated() {
				continue
			}
			target := targets[relatedFindKey(crit)]
			if target.External == nil {
				continue
			}
			crit = normalizeCriterion(crit)
			key := criterionKey(crit)
			if _, done := target.External.found[key]; done {
				continue
			}
			match, err := target.External.run(ctx, target, crit, budget)
			if err != nil {
				return err
			}
			target.External.found[key] = match
			for _, k := range match.keys {
				budget -= len(k)
			}
		}
	}
	return nil
}

// criterionKey identifies a criterion, as buildFindClauses reads it.
func criterionKey(crit FindCriterion) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%v\x00%v", relatedFindKey(crit), crit.FieldName, crit.Operator, crit.Value, crit.ValueTo)
}

// run finds, in the other solution, the related records crit matches, and
// reads what the records searched must hold to be related to one of them:
// at most budget match values.
func (f *externalFind) run(ctx context.Context, target relatedFindTarget, crit FindCriterion, budget int) (externalMatch, error) {
	table := target.TableName
	field, err := f.peer.ResolveFieldMarker(ctx, table, crit.FieldName)
	if err != nil {
		return externalMatch{}, f.target.Wrap(err)
	}
	if err := checkField(target.Fields, table, field); err != nil {
		return externalMatch{}, f.target.Wrap(err)
	}
	if err := f.peer.refuseUnstored(ctx, table, "search", field); err != nil {
		return externalMatch{}, err
	}

	dialect := f.peer.driver.Dialect()
	active := activeTerms(target.Terms)
	columns := make([]string, len(active))
	var where []string
	for i, t := range active {
		columns[i] = dialect.QuoteIdentifier(t.TargetColumn)
		// The related record's match field must hold something.
		where = append(where, notEmpty(dialect, columns[i]))
	}
	idx := 1
	var values []interface{}
	clause, err := criterionClause(dialect, dialect.QuoteIdentifier(field), target.Fields[field], crit, &idx, &values)
	if err != nil {
		return externalMatch{}, err
	}
	where = append(where, clause)
	from := fmt.Sprintf("FROM %s WHERE %s", dialect.QuoteIdentifier(table), strings.Join(where, " AND "))

	m := externalMatch{kind: kindOf(active)}
	var q string
	limit := 0
	switch m.kind {
	case matchAny:
		q = "SELECT 1 " + from + " LIMIT 1"
	case matchBound:
		// The least, or greatest, by ordering rather than MIN or MAX, which
		// PostgreSQL has no Boolean version of.
		direction := "ASC"
		if active[0].Op == ">" || active[0].Op == ">=" {
			direction = "DESC"
		}
		q = fmt.Sprintf("SELECT %s %s ORDER BY %s %s LIMIT 1", columns[0], from, columns[0], direction)
	case matchDistinct:
		q = fmt.Sprintf("SELECT DISTINCT %s %s LIMIT 2", columns[0], from)
	default:
		limit = max(budget/len(active), 0)
		q = fmt.Sprintf("SELECT DISTINCT %s %s LIMIT %d", strings.Join(columns, ", "), from, limit+1)
	}
	rows, err := f.peer.driver.DB().QueryContext(ctx, q, values...)
	if err != nil {
		return m, fmt.Errorf("failed finding the records related through %s in %s: %w", target.Relationship, f.target.Database, err)
	}
	defer rows.Close()
	width := max(len(active), 1)
	for rows.Next() {
		raw := make([]interface{}, width)
		ptrs := make([]interface{}, width)
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return m, err
		}
		for i := range raw {
			raw[i] = textOf(raw[i])
		}
		switch m.kind {
		case matchAny:
			m.any = true
		case matchBound:
			m.bound = raw[0]
		default:
			m.keys = append(m.keys, raw)
		}
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	if m.kind == matchTuples && len(m.keys) > limit {
		return m, fmt.Errorf("%w: the find on %s through %s matches too many records of %s in %s; narrow it",
			ErrFindTooBroad, crit.FieldName, target.Relationship, table, f.target.Database)
	}
	return m, nil
}

// clause is the condition on the records of tableName that crit, run in the
// other solution, matched a related record of. A record whose match field is
// empty relates to nothing, so it never matches.
func (f *externalFind) clause(
	dialect dbal.Dialect, tableName string, target relatedFindTarget, crit FindCriterion, idx *int, values *[]interface{},
) (string, error) {
	m, ok := f.found[criterionKey(crit)]
	if !ok {
		return "", errors.New("a find on a related field of another solution was not run there; run it again")
	}
	active := activeTerms(target.Terms)
	parents := make([]string, len(active))
	var parts []string
	for i, t := range active {
		parents[i] = dialect.QuoteIdentifier(tableName) + "." + dialect.QuoteIdentifier(t.SourceColumn)
		parts = append(parts, notEmpty(dialect, parents[i]))
	}
	place := func(v interface{}) string {
		p := dialect.Placeholder(*idx)
		*idx++
		*values = append(*values, v)
		return p
	}
	const none = "(1 = 0)"
	switch m.kind {
	case matchAny:
		if !m.any {
			return none, nil
		}
	case matchBound:
		if m.bound == nil {
			return none, nil
		}
		parts = append(parts, fmt.Sprintf("%s %s %s", place(m.bound), active[0].Op, parents[0]))
	case matchDistinct:
		switch len(m.keys) {
		case 0:
			return none, nil
		case 1:
			parts = append(parts, fmt.Sprintf("%s <> %s", place(m.keys[0][0]), parents[0]))
		}
	default:
		if len(m.keys) == 0 {
			return none, nil
		}
		set, ok := f.sets[criterionKey(crit)]
		if !ok {
			return "", errors.New("the match values a find found in another solution were not bound; run it again")
		}
		parts = append(parts, keysCondition(active, parents, set))
	}
	if len(parts) == 0 {
		return "(1 = 1)", nil
	}
	return "(" + strings.Join(parts, " AND ") + ")", nil
}

// keysCondition is the condition that a record is related to one of the
// related records whose match values are the rows of the common table
// expression set: an IN list when every criterion is an equality, of tuples
// for several, and otherwise one related record meeting every criterion.
func keysCondition(active []matchTerm, parents []string, set string) string {
	equality := true
	keys := make([]string, len(active))
	for i, t := range active {
		equality = equality && t.Op == "="
		keys[i] = keyColumn(i)
	}
	switch {
	case equality && len(active) == 1:
		return fmt.Sprintf("%s IN (SELECT %s FROM %s)", parents[0], keys[0], set)
	case equality:
		return fmt.Sprintf("(%s) IN (SELECT %s FROM %s)", strings.Join(parents, ", "), strings.Join(keys, ", "), set)
	}
	terms := make([]string, len(active))
	for i, t := range active {
		terms[i] = fmt.Sprintf("%s.%s %s %s", set, keys[i], t.Op, parents[i])
	}
	return fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE %s)", set, strings.Join(terms, " AND "))
}

// keyColumn names the column of a key set holding the values of the i-th
// criterion.
func keyColumn(i int) string { return fmt.Sprintf("f4b_k%d", i) }

// externalKeySets binds, once for the statement, the match values every
// criterion on a related field of another solution's table found there, as
// common table expressions the criteria's clauses read (see keysCondition),
// and returns the WITH clause that holds them, empty when there are none.
// Each value is bound as the field of this solution it is compared with, so
// PostgreSQL compares it as that field's type.
func externalKeySets(
	dialect dbal.Dialect, fields fieldSet, requests []FindRequest, targets map[string]relatedFindTarget,
	idx *int, values *[]interface{},
) string {
	var sets []string
	for _, req := range requests {
		for _, crit := range req.Criteria {
			if crit.FieldName == "" || !crit.isRelated() {
				continue
			}
			target := targets[relatedFindKey(crit)]
			f := target.External
			if f == nil {
				continue
			}
			key := criterionKey(normalizeCriterion(crit))
			m, found := f.found[key]
			if _, bound := f.sets[key]; bound || !found || m.kind != matchTuples || len(m.keys) == 0 {
				continue
			}
			active := activeTerms(target.Terms)
			types := make([]string, len(active))
			names := make([]string, len(active))
			for i, t := range active {
				types[i] = dialect.MapType(fields.typeOf(t.SourceColumn))
				names[i] = keyColumn(i)
			}
			name := fmt.Sprintf("f4b_keys%d", len(sets))
			f.sets[key] = name
			sets = append(sets, fmt.Sprintf("%s (%s) AS (%s)",
				name, strings.Join(names, ", "), keyRows(dialect, types, m.keys, idx, values)))
		}
	}
	if len(sets) == 0 {
		return ""
	}
	return "WITH " + strings.Join(sets, ", ") + " "
}

// keyRows is a table of rows of bound values. PostgreSQL takes a VALUES list,
// each value cast to the type it is compared as. MariaDB loses the values a
// VALUES list binds, so it takes a SELECT per row.
func keyRows(dialect dbal.Dialect, types []string, rows [][]interface{}, idx *int, values *[]interface{}) string {
	postgres := dialect.Engine() == dbal.EnginePostgres
	out := make([]string, len(rows))
	for i, row := range rows {
		marks := make([]string, len(row))
		for j, v := range row {
			marks[j] = dialect.Placeholder(*idx)
			if postgres {
				marks[j] = fmt.Sprintf("CAST(%s AS %s)", marks[j], types[j])
			}
			*idx++
			*values = append(*values, v)
		}
		if postgres {
			out[i] = "(" + strings.Join(marks, ", ") + ")"
		} else {
			out[i] = "SELECT " + strings.Join(marks, ", ")
		}
	}
	if postgres {
		return "VALUES " + strings.Join(out, ", ")
	}
	return strings.Join(out, " UNION ALL ")
}
