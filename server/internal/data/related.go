package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrRelationshipNotFound is returned when a request names a relationship that
// is not in the catalog, or one that does not reach the table the request
// started from.
var ErrRelationshipNotFound = errors.New("relationship not found")

// ErrCreationNotAllowed is returned when a record is created through a
// relationship whose "Allow records to be created ... through this
// relationship" option is off.
var ErrCreationNotAllowed = errors.New("records cannot be created through this relationship")

// ErrNoMatchValue is returned when the record a portal starts from has nothing
// in one of its match fields: an empty key matches no related record, and a
// record created through the relationship would have nothing to be matched by.
var ErrNoMatchValue = errors.New("the record has no value in the match field")

// relationalOperators maps the operators the relationship graph stores — it
// writes the mathematical signs — to SQL comparisons. The Cartesian product
// (×) is no comparison at all: it relates every record (see cartesian).
var relationalOperators = map[string]string{
	"=": "=", "==": "=",
	"≠": "<>", "<>": "<>", "!=": "<>",
	"<": "<",
	"≤": "<=", "<=": "<=",
	">": ">",
	"≥": ">=", ">=": ">=",
	"×": cartesian, "x": cartesian, "X": cartesian,
}

// cartesian stands for the × operator: a criterion every pair of records
// meets, whatever their fields hold, empty ones included.
const cartesian = "×"

// RelationSide is one end of a relationship, resolved from the occurrence and
// column ids the catalog stores to the physical table and column a query needs.
//
// The side of an occurrence of another solution's table (#107) has External
// set: TableName and Column are then that solution's names, and its records
// are read there, never from a table of this solution.
type RelationSide struct {
	OccurrenceID   string          `json:"occurrence_id"`
	OccurrenceName string          `json:"occurrence_name"`
	TableName      string          `json:"table_name"`
	Column         string          `json:"column"`
	External       *ExternalTarget `json:"external,omitempty"`
	// End is which end of the relationship the side is. Its occurrence does
	// not say when the relationship joins an occurrence to itself.
	End RelationEnd `json:"end,omitempty"`
}

// RelationEnd names one end of a relationship.
type RelationEnd string

const (
	EndLeft  RelationEnd = "left"
	EndRight RelationEnd = "right"
)

// ParseRelationEnd reads the end a request names: "left", "right", or none.
func ParseRelationEnd(text string) (RelationEnd, error) {
	switch end := RelationEnd(strings.ToLower(strings.TrimSpace(text))); end {
	case "", EndLeft, EndRight:
		return end, nil
	}
	return "", fmt.Errorf("%w: the end of a relationship is left or right, not %q", ErrRelationshipNotFound, text)
}

// isLeft reports whether side is the left end of r.
func (r *Relationship) isLeft(side RelationSide) bool {
	if side.End != "" {
		return side.End == EndLeft
	}
	return side.OccurrenceID == r.Left.OccurrenceID
}

// setEnds records which end each side is.
func (r *Relationship) setEnds() {
	r.Left.End, r.Right.End = EndLeft, EndRight
}

// Relationship is a catalog relationship with both of its sides resolved.
//
// Each side has its own options, as in FileMaker, and each acts on the side it
// is set on. AllowCreation, CascadeDelete and SortRelated are the right side's:
// a record of the left side creates records on the right, deleting it deletes
// its records on the right, and its records on the right are read in that
// order. LeftAllowCreation, LeftCascadeDelete and LeftSortRelated are the left
// side's, which act the other way round.
//
// It relates the records that meet every one of its Predicates. The first
// predicate is also Operator, Left.Column and Right.Column; a relationship
// built without Predicates has that one only.
type Relationship struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Operator      string              `json:"operator"`
	Left          RelationSide        `json:"left"`
	Right         RelationSide        `json:"right"`
	Predicates    []RelationPredicate `json:"predicates,omitempty"`
	AllowCreation bool                `json:"allow_creation"`
	CascadeDelete bool                `json:"cascade_delete"`
	SortRelated   string              `json:"sort_related,omitempty"`

	LeftAllowCreation bool   `json:"left_allow_creation"`
	LeftCascadeDelete bool   `json:"left_cascade_delete"`
	LeftSortRelated   string `json:"left_sort_related,omitempty"`
}

// allowsCreationIn reports whether records of target may be created through
// the relationship, from a record of its other side: the option of the side
// they are created in.
func (r *Relationship) allowsCreationIn(target RelationSide) bool {
	if r.isLeft(target) {
		return r.LeftAllowCreation
	}
	return r.AllowCreation
}

// sortOf is the "Sort records" order of a side: the order its records are
// read in from a record of the other side.
func (r *Relationship) sortOf(target RelationSide) string {
	if r.isLeft(target) {
		return r.LeftSortRelated
	}
	return r.SortRelated
}

// RelationPredicate is one match criterion of a relationship, with its two
// fields resolved to their column names: LeftColumn Operator RightColumn.
type RelationPredicate struct {
	Operator    string `json:"operator"`
	LeftColumn  string `json:"left_column"`
	RightColumn string `json:"right_column"`
}

// predicates are the relationship's criteria, in order.
func (r *Relationship) predicates() []RelationPredicate {
	if len(r.Predicates) > 0 {
		return r.Predicates
	}
	return []RelationPredicate{{Operator: r.Operator, LeftColumn: r.Left.Column, RightColumn: r.Right.Column}}
}

// reversed is the comparison seen from the other side: left < right is
// right > left.
var reversed = map[string]string{"=": "=", "<>": "<>", "<": ">", "<=": ">=", ">": "<", ">=": "<=", cartesian: cartesian}

// matchTerm is one criterion seen from the record in hand: a related record's
// TargetColumn must meet Op against the record's SourceColumn — target op key.
type matchTerm struct {
	SourceColumn string
	TargetColumn string
	Op           string // a SQL comparison, or cartesian
}

// matchTerms are the criteria a record of source relates through. Each is
// drawn left op right, so its comparison is reversed when the source is the
// left side — otherwise Lines::Date > Orders::Date read from an order would
// return the lines dated before it.
func (r *Relationship) matchTerms(source RelationSide) ([]matchTerm, error) {
	fromLeft := r.isLeft(source)
	preds := r.predicates()
	terms := make([]matchTerm, len(preds))
	for i, p := range preds {
		op, ok := relationalOperators[strings.TrimSpace(p.Operator)]
		if !ok {
			return nil, fmt.Errorf("relationship %s: unsupported operator %q", r.Name, p.Operator)
		}
		if fromLeft {
			terms[i] = matchTerm{SourceColumn: p.LeftColumn, TargetColumn: p.RightColumn, Op: reversed[op]}
		} else {
			terms[i] = matchTerm{SourceColumn: p.RightColumn, TargetColumn: p.LeftColumn, Op: op}
		}
	}
	return terms, nil
}

// sourceColumns are the fields of the record in hand the terms read, each
// once.
func sourceColumns(terms []matchTerm) []string {
	var out []string
	for _, t := range terms {
		if t.Op != cartesian && !slices.Contains(out, t.SourceColumn) {
			out = append(out, t.SourceColumn)
		}
	}
	return out
}

// matchKeys are the values a match field holds. A text key holding several
// lines is a multi-key, as in FileMaker: it matches a record holding any of
// them. Empty lines, and an empty key, match nothing; a line of spaces is a
// key, as it is for the statement that splits the other side (multikey.go).
func matchKeys(key interface{}) []interface{} {
	text, isText := key.(string)
	if b, ok := key.([]byte); ok {
		text, isText = string(b), true
	}
	if !isText {
		if key == nil {
			return nil
		}
		return []interface{}{key}
	}
	var keys []interface{}
	// FieldsFunc gives no empty line.
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\r' || r == '\n' }) {
		keys = append(keys, line)
	}
	return keys
}

// matchCondition is the condition on the target's match fields for the
// records related to a source record, with its arguments; placeholders are
// numbered from first. keyOf gives what the source record holds in one of its
// match fields, and fields are the target's fields.
//
// Every criterion must be met. Each compares one target field with what the
// source record holds, which may be several keys (a multi-key) and is met by
// any of them; a target field that is text may hold several keys too, and is
// met through any of its lines (see multikey.go). The condition is empty when
// the record matches nothing: a criterion other than × whose source field is
// empty relates no record. An empty field relates nothing on the target side
// either, whatever the operator: an empty text is ≠ 'north' in SQL, but
// FileMaker never relates a record through an empty match field, so neither
// does a portal, a related read, a sort or a cascading delete.
func matchCondition(
	dialect dbal.Dialect, terms []matchTerm, fields fieldSet, keyOf func(column string) interface{}, first int,
) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	for _, t := range terms {
		if t.Op == cartesian {
			continue
		}
		keys := matchKeys(keyOf(t.SourceColumn))
		if len(keys) == 0 {
			return "", nil
		}
		column := keyColumnOf(dialect, "", fields, t.TargetColumn)
		conditions = append(conditions, notEmpty(dialect, column.sql))
		conditions = append(conditions, meetsKeys(dialect, column, t.Op, func() []string {
			marks := make([]string, len(keys))
			for i, key := range keys {
				if column.lines {
					key = keyText(key)
				}
				marks[i] = dialect.Placeholder(first + len(args))
				args = append(args, key)
			}
			return marks
		}))
	}
	if len(conditions) == 0 {
		return "1 = 1", nil // only × criteria: every record is related
	}
	return strings.Join(conditions, " AND "), args
}

// notEmpty is the condition that a match field holds something. A field of
// spaces does: it is tested by length, as MariaDB's collations hold spaces
// equal to an empty string.
func notEmpty(dialect dbal.Dialect, column string) string {
	return valueThere(dialect, column)
}

// Sides decides which end of the relationship a request reads.
//
// fromTable is the table of the record in hand; targetOccurrence names the
// occurrence whose records are wanted, which is what tells the two ends apart
// when a relationship joins a table to itself. Without it the other table is
// taken as the target.
func (r *Relationship) Sides(fromTable, targetOccurrence string) (source, target RelationSide, err error) {
	return r.SidesFrom(LocalOrigin(fromTable), targetOccurrence)
}

// SidesToward is SidesFrom with the end of the relationship to read named,
// which is how the two ends are told apart when the relationship joins an
// occurrence to itself: there, an occurrence alone names the right end. A
// target occurrence given with an end must be that end's.
func (r *Relationship) SidesToward(from Origin, targetOccurrence string, end RelationEnd) (source, target RelationSide, err error) {
	switch end {
	case "":
		return r.SidesFrom(from, targetOccurrence)
	case EndLeft:
		source, target = r.Right, r.Left
	case EndRight:
		source, target = r.Left, r.Right
	default:
		return source, target, fmt.Errorf("%w: relationship %s has no %s end", ErrRelationshipNotFound, r.Name, end)
	}
	if targetOccurrence != "" && targetOccurrence != target.OccurrenceID && targetOccurrence != target.OccurrenceName {
		return source, target, fmt.Errorf("%w: the %s end of relationship %s is %s, not %q",
			ErrRelationshipNotFound, end, r.Name, target.OccurrenceName, targetOccurrence)
	}
	if source.origin().key() != from.key() {
		return source, target, fmt.Errorf("%w: the other side of relationship %s is %s, not %s",
			ErrRelationshipNotFound, r.Name, source.TableName, from.Table)
	}
	return source, target, nil
}

// SidesFrom is Sides for a record of any table, of this solution or of
// another one an external occurrence names. A side of another solution's
// table never matches a table of this solution of the same name.
func (r *Relationship) SidesFrom(from Origin, targetOccurrence string) (source, target RelationSide, err error) {
	fromTable, key := from.Table, from.key()
	leftIs, rightIs := r.Left.origin().key() == key, r.Right.origin().key() == key
	switch {
	case targetOccurrence == "":
		switch {
		case leftIs && rightIs:
			// A self-join: which end is wanted is not something to guess.
			return source, target, fmt.Errorf(
				"%w: relationship %s joins %s to itself, so the occurrence to read must be named",
				ErrRelationshipNotFound, r.Name, fromTable)
		case leftIs:
			source, target = r.Left, r.Right
		case rightIs:
			source, target = r.Right, r.Left
		default:
			return source, target, fmt.Errorf("%w: relationship %s does not reach %s",
				ErrRelationshipNotFound, r.Name, fromTable)
		}
	case targetOccurrence == r.Right.OccurrenceID || targetOccurrence == r.Right.OccurrenceName:
		source, target = r.Left, r.Right
	case targetOccurrence == r.Left.OccurrenceID || targetOccurrence == r.Left.OccurrenceName:
		source, target = r.Right, r.Left
	default:
		return source, target, fmt.Errorf("%w: relationship %s has no side %q",
			ErrRelationshipNotFound, r.Name, targetOccurrence)
	}

	if source.origin().key() != key {
		return source, target, fmt.Errorf("%w: the other side of relationship %s is %s, not %s",
			ErrRelationshipNotFound, r.Name, source.TableName, fromTable)
	}
	return source, target, nil
}

// relationshipSelect resolves both sides in one statement: the occurrence
// gives the base table, and the match column gives its name. The inner joins
// leave out every relationship with a side on an occurrence of another
// solution's table, which has neither a local table nor local columns.
const relationshipSelect = `SELECT r.id, r.name, r.operator, r.allow_creation, r.cascade_delete, r.sort_related,
		r.left_allow_creation, r.left_cascade_delete, r.left_sort_related,
		lo.id, lo.name, lt.name, lc.name,
		ro.id, ro.name, rt.name, rc.name
	FROM sys_relationships r
	JOIN sys_table_occurrences lo ON lo.id = r.left_occurrence_id
	JOIN sys_tables lt ON lt.id = lo.base_table_id
	JOIN sys_columns lc ON lc.id = r.left_column_id
	JOIN sys_table_occurrences ro ON ro.id = r.right_occurrence_id
	JOIN sys_tables rt ON rt.id = ro.base_table_id
	JOIN sys_columns rc ON rc.id = r.right_column_id`

const relationshipQuery = relationshipSelect + `
	WHERE r.id = `

// Relationship resolves one relationship of the catalog to the tables and
// columns a query can use.
func (s *Service) Relationship(ctx context.Context, id string) (*Relationship, error) {
	dialect := s.driver.Dialect()
	rel, err := scanRelationship(s.driver.DB().QueryRowContext(ctx, relationshipQuery+dialect.Placeholder(1), id))
	if errors.Is(err, sql.ErrNoRows) {
		// The statement resolves local sides only. A relationship to an
		// occurrence of another solution's table is resolved to that
		// solution's names, never to a table of this one.
		rel = s.externalRelationship(ctx, id)
		if rel == nil {
			return nil, fmt.Errorf("%w: %s", ErrRelationshipNotFound, id)
		}
		err = nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed resolving relationship %s: %w", id, err)
	}
	if err := s.attachPredicates(ctx, []*Relationship{rel}, id); err != nil {
		return nil, err
	}
	return rel, nil
}

// Relationships resolves every relationship of the catalog.
func (s *Service) Relationships(ctx context.Context) ([]*Relationship, error) {
	rows, err := s.driver.DB().QueryContext(ctx, relationshipSelect)
	if err != nil {
		return nil, fmt.Errorf("failed reading the relationships: %w", err)
	}
	defer rows.Close()
	var out []*Relationship
	for rows.Next() {
		rel, err := scanRelationship(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachPredicates(ctx, out, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// predicatesSelect reads the criteria after the first, which
// sys_relationship_predicates keeps, with their fields resolved to column
// names: through the field's id, and by the name kept with it otherwise.
const predicatesSelect = `SELECT p.relationship_id, p.operator,
		COALESCE(lc.name, p.left_column_name, ''), COALESCE(rc.name, p.right_column_name, '')
	FROM sys_relationship_predicates p
	LEFT JOIN sys_columns lc ON lc.id = p.left_column_id
	LEFT JOIN sys_columns rc ON rc.id = p.right_column_id`

// attachPredicates gives each relationship its full list of criteria: its
// first, then those kept in sys_relationship_predicates, in order. A database
// created before that table existed has single-criterion relationships only.
func (s *Service) attachPredicates(ctx context.Context, rels []*Relationship, onlyID string) error {
	if len(rels) == 0 {
		return nil
	}
	q := predicatesSelect
	var args []interface{}
	if onlyID != "" {
		q += ` WHERE p.relationship_id = ` + s.driver.Dialect().Placeholder(1)
		args = append(args, onlyID)
	}
	q += ` ORDER BY p.relationship_id, p.sequence_idx`
	rows, err := s.driver.DB().QueryContext(ctx, q, args...)
	if dbal.IsUndefinedTable(err) {
		err = nil
		rows = nil
	}
	if err != nil {
		return fmt.Errorf("failed reading the relationship criteria: %w", err)
	}
	extra := map[string][]RelationPredicate{}
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			var p RelationPredicate
			if err := rows.Scan(&id, &p.Operator, &p.LeftColumn, &p.RightColumn); err != nil {
				return err
			}
			extra[id] = append(extra[id], p)
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for _, rel := range rels {
		rel.Predicates = append([]RelationPredicate{{Operator: rel.Operator, LeftColumn: rel.Left.Column, RightColumn: rel.Right.Column}},
			extra[rel.ID]...)
	}
	return nil
}

func scanRelationship(row interface{ Scan(...any) error }) (*Relationship, error) {
	var rel Relationship
	var sortRelated, leftSortRelated sql.NullString
	var leftAllowCreation, leftCascadeDelete sql.NullBool
	err := row.Scan(
		&rel.ID, &rel.Name, &rel.Operator, &rel.AllowCreation, &rel.CascadeDelete, &sortRelated,
		&leftAllowCreation, &leftCascadeDelete, &leftSortRelated,
		&rel.Left.OccurrenceID, &rel.Left.OccurrenceName, &rel.Left.TableName, &rel.Left.Column,
		&rel.Right.OccurrenceID, &rel.Right.OccurrenceName, &rel.Right.TableName, &rel.Right.Column,
	)
	if err != nil {
		return nil, err
	}
	rel.SortRelated = sortRelated.String
	rel.LeftAllowCreation, rel.LeftCascadeDelete, rel.LeftSortRelated =
		leftAllowCreation.Bool, leftCascadeDelete.Bool, leftSortRelated.String
	rel.setEnds()
	return &rel, nil
}

// ParseSortSpec reads a sort order written as text: field names in the order
// they sort, separated by commas, each optionally prefixed with "-" to sort it
// descending.
//
//	company,-fee_paid
func ParseSortSpec(spec string) []SortField {
	var order []SortField
	for _, part := range strings.Split(spec, ",") {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		descending := false
		switch field[0] {
		case '-':
			descending, field = true, strings.TrimSpace(field[1:])
		case '+':
			field = strings.TrimSpace(field[1:])
		}
		if field == "" {
			continue
		}
		order = append(order, SortField{Field: field, Descending: descending})
	}
	return order
}

// relatedSortOrder is the order a portal's records come back in: the one the
// caller asked for, otherwise the "Sort records" of the side being read.
//
// That option was stored as the bare word "asc" or "desc" before a sort order
// could be written out, which means the match field in that direction.
//
// Each side has its own, as in FileMaker, and it names fields of that side: a
// company's customers are read in the customers side's order, a customer's
// companies in the companies side's. A field the side being read does not have
// is dropped rather than refused: a stale setting on the relationship must not
// leave a portal blank. What the caller asked for is checked as usual, so a
// sort typed into Portal Setup is still reported.
func relatedSortOrder(rel *Relationship, target RelationSide, fields fieldSet, opts QueryOptions) []SortField {
	if order := opts.SortOrder(); len(order) > 0 {
		return order
	}

	// "asc" and "desc" sort by the side's field of the first criterion.

	var order []SortField
	switch spec := strings.TrimSpace(rel.sortOf(target)); strings.ToLower(spec) {
	case "":
		return nil
	case "asc", "ascending":
		order = []SortField{{Field: target.Column}}
	case "desc", "descending":
		order = []SortField{{Field: target.Column, Descending: true}}
	default:
		order = ParseSortSpec(spec)
	}

	kept := order[:0]
	for _, level := range order {
		if _, ok := fields[level.Field]; ok {
			kept = append(kept, level)
		}
	}
	return kept
}

// matchValues reads what a record holds in its match fields, as the driver's
// own types, so they go back into the next query as parameters of the right
// type rather than as text. The record must exist even when there are no
// fields to read, as with a relationship of × criteria only.
func (s *Service) matchValues(ctx context.Context, tableName string, columns []string, id string) (map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	list := make([]string, 0, len(columns)+1)
	for _, column := range columns {
		if err := checkField(fields, tableName, column); err != nil {
			return nil, err
		}
		list = append(list, dialect.QuoteIdentifier(column))
	}
	if len(list) == 0 {
		list = append(list, dialect.QuoteIdentifier("id"))
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1",
		strings.Join(list, ", "),
		dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"),
		dialect.Placeholder(1),
	)
	raw := make([]interface{}, len(list))
	ptrs := make([]interface{}, len(list))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := s.driver.DB().QueryRowContext(ctx, q, id).Scan(ptrs...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("record not found with id: %s", id)
		}
		return nil, fmt.Errorf("failed reading the match fields of %s: %w", tableName, err)
	}
	values := make(map[string]interface{}, len(columns))
	for i, column := range columns {
		// MySQL hands text back as bytes; a parameter is clearer as a string.
		if b, ok := raw[i].([]byte); ok {
			values[column] = string(b)
		} else {
			values[column] = raw[i]
		}
	}
	return values, nil
}

// matchValuesOf is matchValues for several records at once, read in one
// statement, by record id. A record that is not there has no entry.
func (s *Service) matchValuesOf(ctx context.Context, tableName string, columns []string, ids []string) (map[string]map[string]interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	list := []string{dialect.QuoteIdentifier("id")}
	for _, column := range columns {
		if err := checkField(fields, tableName, column); err != nil {
			return nil, err
		}
		list = append(list, dialect.QuoteIdentifier(column))
	}
	marks := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		marks[i], args[i] = dialect.Placeholder(i+1), id
	}
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)",
		strings.Join(list, ", "),
		dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"),
		strings.Join(marks, ", "),
	)
	rows, err := s.driver.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed reading the match fields of %s: %w", tableName, err)
	}
	defer rows.Close()
	out := make(map[string]map[string]interface{}, len(ids))
	for rows.Next() {
		raw := make([]interface{}, len(list))
		ptrs := make([]interface{}, len(list))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		values := make(map[string]interface{}, len(columns))
		for i, column := range columns {
			// As in matchValues: text the driver hands back as bytes is a string.
			if b, ok := raw[i+1].([]byte); ok {
				values[column] = string(b)
			} else {
				values[column] = raw[i+1]
			}
		}
		out[fmt.Sprint(textOf(raw[0]))] = values
	}
	return out, rows.Err()
}

func textOf(v interface{}) interface{} {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// emptyMatchValue reports whether a match value matches nothing. An empty key
// relates to no record, the same way Find mode treats an empty field.
func emptyMatchValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []byte:
		return strings.TrimSpace(string(t)) == ""
	}
	return false
}

// RelatedRows returns the records of the other side of a relationship that
// match the given record.
//
// fromTable and recordID name the record in hand; relationshipID the
// relationship to follow; targetOccurrence the occurrence whose records are
// wanted, which may be empty unless the relationship joins a table to itself.
func (s *Service) RelatedRows(
	ctx context.Context,
	fromTable, recordID, relationshipID, targetOccurrence string,
	opts QueryOptions,
) ([]map[string]interface{}, error) {
	return s.RelatedRowsFrom(ctx, LocalOrigin(fromTable), recordID, relationshipID, targetOccurrence, opts)
}

// RelatedRowsFrom is RelatedRows for a record of any table, of this
// solution or of another one (#107).
//
// The relationship is always this solution's. A side on an occurrence of
// another solution's table is read in that solution, as the account the
// caller's session group holds there: the match values are read where the
// record in hand is, and the related records are then looked up where they
// are with those values. No statement joins two solutions.
func (s *Service) RelatedRowsFrom(
	ctx context.Context, from Origin, recordID, relationshipID, targetOccurrence string, opts QueryOptions,
) ([]map[string]interface{}, error) {
	q, err := s.relatedQuery(ctx, from, relationshipID, targetOccurrence, opts)
	if err != nil {
		return nil, err
	}
	values, err := q.src.matchValues(ctx, q.source.TableName, sourceColumns(q.terms), recordID)
	if err != nil {
		return nil, wrapSide(q.source, err)
	}
	match, keys := matchCondition(q.dst.driver.Dialect(), q.terms, q.fields, func(c string) interface{} { return values[c] }, 1)
	if match == "" {
		return []map[string]interface{}{}, nil
	}
	rows, err := q.dst.driver.DB().QueryContext(ctx, q.page(match), keys...)
	if err != nil {
		return nil, fmt.Errorf("failed reading records related to %s.%s: %w", from.Table, recordID, err)
	}
	defer rows.Close()
	related, err := q.read(ctx, rows)
	if err != nil {
		return nil, err
	}
	if q.sorted != nil {
		related = q.sortPage(related)
	}
	if err := q.attach(ctx, related); err != nil {
		return nil, err
	}
	return related, nil
}

// relatedQuery is everything reading one relationship from one side needs,
// resolved once: the sides, their criteria, the services each side is read
// with, the target's fields and order, and the page.
type relatedQuery struct {
	rel            *Relationship
	source, target RelationSide
	terms          []matchTerm
	src, dst       *Service
	fields         fieldSet
	order          string
	// creation is the column the target's records are numbered in as they
	// are created, which order ends with (see creationOrder).
	creation string
	// sorted is the read's order when it sorts by an unstored calculation:
	// the records are then read in creation order and sorted here
	// (unstoredSort).
	sorted        []SortField
	limit, offset int
}

func (s *Service) relatedQuery(
	ctx context.Context, from Origin, relationshipID, targetOccurrence string, opts QueryOptions,
) (*relatedQuery, error) {
	rel, err := s.Relationship(ctx, relationshipID)
	if err != nil {
		return nil, err
	}
	q, err := s.relatedQueryOf(ctx, rel, from, targetOccurrence, opts.End)
	if err != nil {
		return nil, err
	}
	if err := q.paged(ctx, opts); err != nil {
		return nil, err
	}
	return q, nil
}

// relatedQueryOf is relatedQuery for a relationship already resolved.
//
// It resolves what matching needs, and no order: creating and cascading
// read no page, so the relationship's sort is neither applied nor checked
// there, and a sort its side cannot use never stops a record being created
// or deleted.
func (s *Service) relatedQueryOf(
	ctx context.Context, rel *Relationship, from Origin, targetOccurrence string, end RelationEnd,
) (*relatedQuery, error) {
	q := &relatedQuery{rel: rel}
	var err error
	if q.source, q.target, err = rel.SidesToward(from, targetOccurrence, end); err != nil {
		return nil, err
	}
	if q.terms, err = rel.matchTerms(q.source); err != nil {
		return nil, err
	}
	if q.src, err = s.sideService(ctx, q.source); err != nil {
		return nil, err
	}
	if q.dst, err = s.sideService(ctx, q.target); err != nil {
		return nil, err
	}
	// A predicate on a field of another solution known only by its
	// FileMaker id names it by marker: it reads the field bound to that id.
	for i := range q.terms {
		t := &q.terms[i]
		if q.source.External != nil {
			if t.SourceColumn, err = q.src.ResolveFieldMarker(ctx, q.source.TableName, t.SourceColumn); err != nil {
				return nil, wrapSide(q.source, err)
			}
		}
		if q.target.External != nil {
			if t.TargetColumn, err = q.dst.ResolveFieldMarker(ctx, q.target.TableName, t.TargetColumn); err != nil {
				return nil, wrapSide(q.target, err)
			}
		}
	}
	if q.fields, err = q.dst.tableFields(ctx, q.target.TableName); err != nil {
		return nil, wrapSide(q.target, err)
	}
	for _, t := range q.terms {
		if err := checkField(q.fields, q.target.TableName, t.TargetColumn); err != nil {
			return nil, wrapSide(q.target, err)
		}
	}
	return q, nil
}

// paged gives a read its order, the caller's or the target side's own sort,
// then the records' creation order, and its page. A sort by an unstored
// calculation is done once the records are read (unstoredSort).
func (q *relatedQuery) paged(ctx context.Context, opts QueryOptions) error {
	order := relatedSortOrder(q.rel, q.target, q.fields, opts)
	var err error
	if q.sorted, err = q.dst.unstoredSort(ctx, q.target.TableName, order); err != nil {
		return wrapSide(q.target, err)
	}
	if q.sorted != nil {
		// Checked as the engine's sort is, then done once read.
		if _, err := orderByClause(q.dst.driver.Dialect(), q.fields, q.target.TableName, order); err != nil {
			return wrapSide(q.target, err)
		}
		order = nil
	}
	if q.order, err = q.dst.orderBy(ctx, q.fields, q.target.TableName, order); err != nil {
		return wrapSide(q.target, err)
	}
	if q.creation, err = q.dst.creationOrder(ctx, q.target.TableName); err != nil {
		return wrapSide(q.target, err)
	}
	q.limit, q.offset = opts.Limit, opts.Offset
	if q.limit <= 0 {
		q.limit = 100
	} else if q.limit > MaxPageSize {
		q.limit = MaxPageSize
	}
	if q.offset < 0 {
		q.offset = 0
	}
	return nil
}

// page is the statement reading one page of the related records that meet
// match, or all of them, one more than are sorted at most, when they are
// sorted once read. It reads the creation order too, which a batched read
// orders its branches' rows by again (rowsToMaps leaves it out of the
// records).
func (q *relatedQuery) page(match string) string {
	limit, offset := q.limit, q.offset
	if q.sorted != nil {
		limit, offset = maxRelatedRecords+1, 0
	}
	dialect := q.dst.driver.Dialect()
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s %s LIMIT %d OFFSET %d",
		withCreationOrder(dialect, selectList(dialect, q.fields), q.creation),
		dialect.QuoteIdentifier(q.target.TableName),
		match,
		q.order,
		limit,
		offset,
	)
}

// attach adds what the data API shows with related records: the original
// text of a value the way the record itself shows it (#124), and container
// metadata.
func (q *relatedQuery) attach(ctx context.Context, related []map[string]interface{}) error {
	if err := q.dst.AttachSourceText(ctx, q.target.TableName, related); err != nil {
		return err
	}
	return q.dst.AttachContainerMeta(ctx, q.target.TableName, related)
}

// MaxRelatedBatch is the most records whose related records one batched
// read returns.
const MaxRelatedBatch = 100

// ErrBatchTooLarge is returned for a batched related read of more records
// than MaxRelatedBatch.
var ErrBatchTooLarge = fmt.Errorf("at most %d records are read in one batch", MaxRelatedBatch)

// RelatedRowsBatch is RelatedRowsFrom for several records at once: what a
// list of records showing a related field or a portal needs, read without a
// statement per record (no N+1). The match values of every record are read
// in one statement where the records are, and the related records of all of
// them in one statement where those are: one branch per distinct set of
// match values, each with the criteria, order and page a single record's
// read has, so a record gets exactly what RelatedRowsFrom gives it. Every
// record asked for has an entry; one that matches nothing, or is not there,
// has none.
func (s *Service) RelatedRowsBatch(
	ctx context.Context, from Origin, recordIDs []string, relationshipID, targetOccurrence string, opts QueryOptions,
) (map[string][]map[string]interface{}, error) {
	if len(recordIDs) > MaxRelatedBatch {
		return nil, ErrBatchTooLarge
	}
	out := make(map[string][]map[string]interface{}, len(recordIDs))
	for _, id := range recordIDs {
		out[id] = []map[string]interface{}{}
	}
	if len(recordIDs) == 0 {
		return out, nil
	}
	q, err := s.relatedQuery(ctx, from, relationshipID, targetOccurrence, opts)
	if err != nil {
		return nil, err
	}
	values, err := q.src.matchValuesOf(ctx, q.source.TableName, sourceColumns(q.terms), recordIDs)
	if err != nil {
		return nil, wrapSide(q.source, err)
	}

	// Records holding the same match values share a branch.
	dialect := q.dst.driver.Dialect()
	var branches []string
	var args []interface{}
	byValues := map[string]int{}
	recordsOf := map[int][]string{}
	for _, id := range recordIDs {
		record, ok := values[id]
		if !ok {
			continue
		}
		signature := matchSignature(q.terms, record)
		branch, seen := byValues[signature]
		if !seen {
			match, keys := matchCondition(dialect, q.terms, q.fields, func(c string) interface{} { return record[c] }, len(args)+1)
			if match == "" {
				continue
			}
			branch = len(branches)
			byValues[signature] = branch
			args = append(args, keys...)
			page := q.page(match)
			branches = append(branches, "("+strings.Replace(page, "SELECT ", fmt.Sprintf("SELECT %d AS %s, ", branch, batchBranch), 1)+")")
		}
		recordsOf[branch] = append(recordsOf[branch], id)
	}
	if len(branches) == 0 {
		return out, nil
	}
	order := "ORDER BY " + batchBranch
	if rest, ok := strings.CutPrefix(q.order, "ORDER BY "); ok {
		order += ", " + rest
	}
	sqlText := fmt.Sprintf("SELECT * FROM (%s) f4b_batch %s", strings.Join(branches, " UNION ALL "), order)
	if q.sorted != nil {
		// Sorted once read: no more records across the batch than one
		// record's read sorts.
		sqlText += fmt.Sprintf(" LIMIT %d", maxRelatedRecords+1)
	}
	rows, err := q.dst.driver.DB().QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("failed reading the records related to %d records of %s: %w", len(recordIDs), from.Table, err)
	}
	defer rows.Close()
	related, err := q.read(ctx, rows)
	if err != nil {
		return nil, err
	}
	byBranch := make([][]map[string]interface{}, len(branches))
	for _, row := range related {
		branch := branchOf(row[batchBranch])
		delete(row, batchBranch)
		if branch >= 0 && branch < len(byBranch) {
			byBranch[branch] = append(byBranch[branch], row)
		}
	}
	if q.sorted != nil {
		related = related[:0]
		for branch := range byBranch {
			byBranch[branch] = q.sortPage(byBranch[branch])
			related = append(related, byBranch[branch]...)
		}
	}
	if err := q.attach(ctx, related); err != nil {
		return nil, err
	}
	for branch, rows := range byBranch {
		for _, id := range recordsOf[branch] {
			out[id] = append(out[id], rows...)
		}
	}
	return out, nil
}

// batchBranch is the column a batched related read numbers its branches in.
const batchBranch = "f4b_branch"

func branchOf(v interface{}) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int32:
		return int(n)
	case int:
		return n
	case []byte:
		i, _ := strconv.Atoi(string(n))
		return i
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return -1
}

// matchSignature identifies the match values a record holds, so records
// holding the same ones are read once.
func matchSignature(terms []matchTerm, record map[string]interface{}) string {
	var b strings.Builder
	for _, column := range sourceColumns(terms) {
		fmt.Fprintf(&b, "%T:%v\x00", record[column], record[column])
	}
	return b.String()
}

// CreateRelatedRow creates a record on the other side of a relationship and
// fills its match fields, so the new record belongs to the record in hand.
//
// It is refused unless the relationship allows creation, and unless every
// criterion is an equality: with any other comparison there is no single value
// to write into the new record's match field, as in FileMaker.
func (s *Service) CreateRelatedRow(
	ctx context.Context,
	fromTable, recordID, relationshipID, targetOccurrence string,
	record map[string]interface{},
) (map[string]interface{}, error) {
	return s.CreateRelatedRowFrom(ctx, LocalOrigin(fromTable), recordID, relationshipID, targetOccurrence, record)
}

// CreateRelatedRowFrom is CreateRelatedRow for a record of any table, of this
// solution or of another one (#107).
//
// The match values are read where the record in hand is, and the new record
// is inserted where the related records are: in another solution, by that
// solution's data service as the account the session group holds there,
// which needs that account's create privilege. Its auto-enter values,
// calculations and validation rules are that solution's.
func (s *Service) CreateRelatedRowFrom(
	ctx context.Context, from Origin, recordID, relationshipID, targetOccurrence string,
	record map[string]interface{},
) (map[string]interface{}, error) {
	return s.CreateRelatedRowToward(ctx, from, recordID, relationshipID, targetOccurrence, "", record)
}

// CreateRelatedRowToward is CreateRelatedRowFrom with the end of the
// relationship to create in named, which a relationship joining an
// occurrence to itself needs to create on its left end.
func (s *Service) CreateRelatedRowToward(
	ctx context.Context, from Origin, recordID, relationshipID, targetOccurrence string, end RelationEnd,
	record map[string]interface{},
) (map[string]interface{}, error) {
	rel, err := s.Relationship(ctx, relationshipID)
	if err != nil {
		return nil, err
	}
	source, target, err := rel.SidesToward(from, targetOccurrence, end)
	if err != nil {
		return nil, err
	}
	// Allow-creation is set per side, as in FileMaker, on the side records
	// are created in: a record of one side creates on the other only when
	// that other side allows it.
	if !rel.allowsCreationIn(target) {
		if !rel.AllowCreation && !rel.LeftAllowCreation {
			return nil, fmt.Errorf("%w: %s", ErrCreationNotAllowed, rel.Name)
		}
		return nil, fmt.Errorf("%w: relationship %s creates records of %s from a record of %s, not of %s from %s: "+
			"create the record from %s, or allow creation of records in %s through it",
			ErrCreationNotAllowed, rel.Name, source.OccurrenceName, target.OccurrenceName,
			target.OccurrenceName, source.OccurrenceName, target.OccurrenceName, target.OccurrenceName)
	}
	terms, err := rel.matchTerms(source)
	if err != nil {
		return nil, err
	}
	for i, t := range terms {
		if t.Op != "=" {
			return nil, fmt.Errorf("%w: relationship %s matches with %q, so a new record has no match value to be given",
				ErrCreationNotAllowed, rel.Name, rel.predicates()[i].Operator)
		}
	}
	q, err := s.relatedQueryOf(ctx, rel, from, targetOccurrence, end)
	if err != nil {
		return nil, err
	}
	// The record is created where the related table is, by the account the
	// caller writes there with: this solution's, or the one the session group
	// holds in another (#107).
	if err := q.dst.checkWrite(ctx, target.TableName, ActionCreate); err != nil {
		return nil, wrapSide(target, err)
	}

	keys, err := q.src.matchValues(ctx, source.TableName, sourceColumns(q.terms), recordID)
	if err != nil {
		return nil, wrapSide(source, err)
	}
	values := make(map[string]interface{}, len(record)+len(q.terms))
	for k, v := range record {
		values[k] = v
	}
	// The match fields belong to the relationship, not to the caller: a portal
	// row created here is matched by this record and nothing else.
	for _, t := range q.terms {
		key := keys[t.SourceColumn]
		if emptyMatchValue(key) {
			return nil, fmt.Errorf("%w: %s.%s is empty", ErrNoMatchValue, source.TableName, t.SourceColumn)
		}
		values[t.TargetColumn] = key
	}

	inserted, err := q.dst.InsertRow(ctx, target.TableName, values)
	if err != nil {
		return nil, wrapSide(target, err)
	}
	// The related table is not the one the request names.
	database := ""
	if target.External != nil {
		database = target.External.Database
	}
	observeChange(ctx, database, target.TableName)
	return inserted, nil
}

// cascadeLink is a relationship a delete follows, from the table of the
// record being deleted to the side whose records it deletes.
type cascadeLink struct {
	relationship string
	// toLeft is set when the table is on the right and the left side's
	// option deletes the records of the left side; otherwise the table is on
	// the left and the right side's option deletes those of the right.
	toLeft bool
}

// cascadingRelationships returns the relationships a delete of a record of
// the given table follows: "Delete related records in this table when a
// record is deleted in the other table" is set per side, as in FileMaker, on
// the side whose records go. A relationship with the table on its left
// follows the right side's option, one with the table on its right the left
// side's; a relationship joining the table to itself may follow both.
func (s *Service) cascadingRelationships(ctx context.Context, tableName string) ([]cascadeLink, error) {
	dialect := s.driver.Dialect()
	q := `SELECT r.id, 0 FROM sys_relationships r
		JOIN sys_table_occurrences lo ON lo.id = r.left_occurrence_id
		JOIN sys_tables lt ON lt.id = lo.base_table_id
		WHERE r.cascade_delete = ` + trueLiteral(dialect) + ` AND lt.name = ` + dialect.Placeholder(1) + `
		UNION ALL
		SELECT r.id, 1 FROM sys_relationships r
		JOIN sys_table_occurrences ro ON ro.id = r.right_occurrence_id
		JOIN sys_tables rt ON rt.id = ro.base_table_id
		WHERE r.left_cascade_delete = ` + trueLiteral(dialect) + ` AND rt.name = ` + dialect.Placeholder(2)

	rows, err := s.driver.DB().QueryContext(ctx, q, tableName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the relationships of %s: %w", tableName, err)
	}
	defer rows.Close()

	links := make([]cascadeLink, 0)
	for rows.Next() {
		var link cascadeLink
		var toLeft int
		if err := rows.Scan(&link.relationship, &toLeft); err != nil {
			return nil, err
		}
		link.toLeft = toLeft == 1
		links = append(links, link)
	}
	return links, rows.Err()
}

// trueLiteral is how each engine spells a true boolean in a statement.
func trueLiteral(dialect dbal.Dialect) string {
	if dialect.Engine() == dbal.EnginePostgres {
		return "TRUE"
	}
	return "1"
}

// ─── Find on a related field (#46) ───────────────────────────────────────────

// relatedFindTarget is everything a criterion on a related field needs: the
// table the related records live in, the relationship's criteria seen from
// the table being searched, and the fields the related table has.
type relatedFindTarget struct {
	TableName    string
	Terms        []matchTerm // the related table's field op the searched table's
	Fields       fieldSet
	Relationship string // name, for error messages
	// External is set when the related table is another solution's: each
	// criterion on it is run there first (see externalFind).
	External *externalFind
}

// resolveFindRelations resolves every relationship the criteria reach
// through, once each, before the WHERE clause is built.
func (s *Service) resolveFindRelations(
	ctx context.Context, tableName string, requests []FindRequest,
) (map[string]relatedFindTarget, error) {
	targets := map[string]relatedFindTarget{}
	for _, req := range requests {
		for _, crit := range req.Criteria {
			if !crit.isRelated() {
				continue
			}
			key := relatedFindKey(crit)
			if _, done := targets[key]; done {
				continue
			}

			rel, err := s.Relationship(ctx, strings.TrimSpace(crit.RelationshipID))
			if err != nil {
				return nil, err
			}
			if rel.External() != nil {
				if targets[key], err = s.externalFindTarget(ctx, tableName, rel, crit); err != nil {
					return nil, err
				}
				continue
			}
			source, target, err := rel.Sides(tableName, strings.TrimSpace(crit.Occurrence))
			if err != nil {
				return nil, err
			}
			terms, err := rel.matchTerms(source) // each clause is child op parent
			if err != nil {
				return nil, err
			}
			fields, err := s.tableFields(ctx, target.TableName)
			if err != nil {
				return nil, err
			}
			targets[key] = relatedFindTarget{
				TableName:    target.TableName,
				Terms:        terms,
				Fields:       fields,
				Relationship: rel.Name,
			}
		}
	}
	if err := findInOtherSolutions(ctx, requests, targets); err != nil {
		return nil, err
	}
	return targets, nil
}

// relatedFindKey identifies a resolved relationship: the same relationship
// read from the same side resolves once.
func relatedFindKey(crit FindCriterion) string {
	return strings.TrimSpace(crit.RelationshipID) + "\x00" + strings.TrimSpace(crit.Occurrence)
}

// relatedCriterionClause turns a criterion on a related field into a
// condition on the record being searched: it matches when the record has at
// least one related record the criterion matches.
//
// A record whose match field is empty relates to nothing, which is the rule
// the related reads already follow, so it never matches a related criterion —
// not even through an operator like "is empty", which is about the related
// record's field and not about the absence of related records. A related
// record whose match field is empty is not related either, whatever the
// operator. Every criterion of the relationship must be met, as in the related
// reads, and a text match field holding several lines matches through any of
// them, on either side (see multikey.go). fields are the searched table's.
func relatedCriterionClause(
	dialect dbal.Dialect, tableName string, fields fieldSet, crit FindCriterion,
	targets map[string]relatedFindTarget, idx *int, values *[]interface{},
) (string, error) {
	target, ok := targets[relatedFindKey(crit)]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrRelationshipNotFound, crit.RelationshipID)
	}
	if target.External != nil {
		return target.External.clause(dialect, tableName, fields, target, crit, idx, values)
	}
	if err := checkField(target.Fields, target.TableName, crit.FieldName); err != nil {
		return "", err
	}

	// The subquery's own name for the related table, so a relationship from a
	// table to itself still has two distinguishable sides.
	const alias = "f4b_rel"
	var present, joins []string
	for _, t := range target.Terms {
		if t.Op == cartesian {
			continue
		}
		parent := keyColumnOf(dialect, dialect.QuoteIdentifier(tableName), fields, t.SourceColumn)
		child := keyColumnOf(dialect, alias, target.Fields, t.TargetColumn)
		present = append(present, notEmpty(dialect, parent.sql))
		// The related record's match field must hold something too.
		joins = append(joins, notEmpty(dialect, child.sql), keysMeet(dialect, child, t.Op, parent))
	}
	inner := criterionClause(dialect, alias+"."+dialect.QuoteIdentifier(crit.FieldName), crit, idx, values)
	exists := fmt.Sprintf("EXISTS (SELECT 1 FROM %s %s WHERE %s)",
		dialect.QuoteIdentifier(target.TableName), alias, strings.Join(append(joins, inner), " AND "))

	return "(" + strings.Join(append(present, exists), " AND ") + ")", nil
}
