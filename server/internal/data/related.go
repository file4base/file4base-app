package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
// in its match field: an empty key matches no related record, and a record
// created through the relationship would have nothing to be matched by.
var ErrNoMatchValue = errors.New("the record has no value in the match field")

// relationalOperators maps the operators the relationship graph stores — it
// writes the mathematical signs — to SQL comparisons.
var relationalOperators = map[string]string{
	"=": "=", "==": "=",
	"≠": "<>", "<>": "<>", "!=": "<>",
	"<": "<",
	"≤": "<=", "<=": "<=",
	">": ">",
	"≥": ">=", ">=": ">=",
}

// RelationSide is one end of a relationship, resolved from the occurrence and
// column ids the catalog stores to the physical table and column a query needs.
type RelationSide struct {
	OccurrenceID   string `json:"occurrence_id"`
	OccurrenceName string `json:"occurrence_name"`
	TableName      string `json:"table_name"`
	Column         string `json:"column"`
}

// Relationship is a catalog relationship with both of its sides resolved.
//
// A relationship is drawn from a left side to a right side, and File4Base reads
// that direction as parent to child: AllowCreation and CascadeDelete act on the
// right side when a record of the left side is the one in hand.
type Relationship struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Operator      string       `json:"operator"`
	Left          RelationSide `json:"left"`
	Right         RelationSide `json:"right"`
	AllowCreation bool         `json:"allow_creation"`
	CascadeDelete bool         `json:"cascade_delete"`
	SortRelated   string       `json:"sort_related,omitempty"`
}

// sqlOperator is the comparison to put between the two match fields.
func (r *Relationship) sqlOperator() (string, error) {
	op, ok := relationalOperators[strings.TrimSpace(r.Operator)]
	if !ok {
		return "", fmt.Errorf("relationship %s: unsupported operator %q", r.Name, r.Operator)
	}
	return op, nil
}

// Sides decides which end of the relationship a request reads.
//
// fromTable is the table of the record in hand; targetOccurrence names the
// occurrence whose records are wanted, which is what tells the two ends apart
// when a relationship joins a table to itself. Without it the other table is
// taken as the target.
func (r *Relationship) Sides(fromTable, targetOccurrence string) (source, target RelationSide, err error) {
	switch {
	case targetOccurrence == "":
		switch {
		case r.Left.TableName == fromTable && r.Right.TableName == fromTable:
			// A self-join: which end is wanted is not something to guess.
			return source, target, fmt.Errorf(
				"%w: relationship %s joins %s to itself, so the occurrence to read must be named",
				ErrRelationshipNotFound, r.Name, fromTable)
		case r.Left.TableName == fromTable:
			source, target = r.Left, r.Right
		case r.Right.TableName == fromTable:
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

	if source.TableName != fromTable {
		return source, target, fmt.Errorf("%w: the other side of relationship %s is %s, not %s",
			ErrRelationshipNotFound, r.Name, source.TableName, fromTable)
	}
	return source, target, nil
}

// relationshipQuery resolves both sides in one statement: the occurrence gives
// the base table, and the match column gives its name.
const relationshipQuery = `SELECT r.id, r.name, r.operator, r.allow_creation, r.cascade_delete, r.sort_related,
		lo.id, lo.name, lt.name, lc.name,
		ro.id, ro.name, rt.name, rc.name
	FROM sys_relationships r
	JOIN sys_table_occurrences lo ON lo.id = r.left_occurrence_id
	JOIN sys_tables lt ON lt.id = lo.base_table_id
	JOIN sys_columns lc ON lc.id = r.left_column_id
	JOIN sys_table_occurrences ro ON ro.id = r.right_occurrence_id
	JOIN sys_tables rt ON rt.id = ro.base_table_id
	JOIN sys_columns rc ON rc.id = r.right_column_id
	WHERE r.id = `

// Relationship resolves one relationship of the catalog to the tables and
// columns a query can use.
func (s *Service) Relationship(ctx context.Context, id string) (*Relationship, error) {
	dialect := s.driver.Dialect()
	var rel Relationship
	var sortRelated sql.NullString
	err := s.driver.DB().QueryRowContext(ctx, relationshipQuery+dialect.Placeholder(1), id).Scan(
		&rel.ID, &rel.Name, &rel.Operator, &rel.AllowCreation, &rel.CascadeDelete, &sortRelated,
		&rel.Left.OccurrenceID, &rel.Left.OccurrenceName, &rel.Left.TableName, &rel.Left.Column,
		&rel.Right.OccurrenceID, &rel.Right.OccurrenceName, &rel.Right.TableName, &rel.Right.Column,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrRelationshipNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed resolving relationship %s: %w", id, err)
	}
	if sortRelated.Valid {
		rel.SortRelated = sortRelated.String
	}
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
// caller asked for, otherwise the relationship's own "Sort related records".
//
// That option was stored as the bare word "asc" or "desc" before a sort order
// could be written out, which means the match field in that direction.
//
// Like the relationship's other two options it belongs to the **right** side,
// because that is the side they all act on. A relationship is read both ways —
// a company's customers, and a customer's company — and the fields it names
// only exist on one of them, so reading the left side ignores it. A field the
// side being read does not have is dropped rather than refused: a stale
// setting on the relationship must not leave a portal blank. What the caller
// asked for is checked as usual, so a sort typed into Portal Setup is still
// reported.
func relatedSortOrder(rel *Relationship, target RelationSide, fields fieldSet, opts QueryOptions) []SortField {
	if order := opts.SortOrder(); len(order) > 0 {
		return order
	}
	if target.OccurrenceID != rel.Right.OccurrenceID {
		return nil
	}

	var order []SortField
	switch spec := strings.TrimSpace(rel.SortRelated); strings.ToLower(spec) {
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

// matchValue reads what a record holds in a match field, as the driver's own
// type, so it goes back into the next query as a parameter of the right type
// rather than as text.
func (s *Service) matchValue(ctx context.Context, tableName, column, id string) (interface{}, error) {
	fields, err := s.tableFields(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if err := checkField(fields, tableName, column); err != nil {
		return nil, err
	}
	dialect := s.driver.Dialect()
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s LIMIT 1",
		dialect.QuoteIdentifier(column),
		dialect.QuoteIdentifier(tableName),
		dialect.QuoteIdentifier("id"),
		dialect.Placeholder(1),
	)
	var raw interface{}
	if err := s.driver.DB().QueryRowContext(ctx, q, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("record not found with id: %s", id)
		}
		return nil, fmt.Errorf("failed reading match field %s.%s: %w", tableName, column, err)
	}
	// MySQL hands text back as bytes; a parameter is clearer as a string.
	if b, ok := raw.([]byte); ok {
		return string(b), nil
	}
	return raw, nil
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
	rel, err := s.Relationship(ctx, relationshipID)
	if err != nil {
		return nil, err
	}
	source, target, err := rel.Sides(fromTable, targetOccurrence)
	if err != nil {
		return nil, err
	}
	op, err := rel.sqlOperator()
	if err != nil {
		return nil, err
	}

	targetFields, err := s.tableFields(ctx, target.TableName)
	if err != nil {
		return nil, err
	}
	if err := checkField(targetFields, target.TableName, target.Column); err != nil {
		return nil, err
	}

	key, err := s.matchValue(ctx, source.TableName, source.Column, recordID)
	if err != nil {
		return nil, err
	}
	if emptyMatchValue(key) {
		return []map[string]interface{}{}, nil
	}

	dialect := s.driver.Dialect()
	orderClause, err := orderByClause(dialect, targetFields, target.TableName,
		relatedSortOrder(rel, target, targetFields, opts))
	if err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	} else if limit > MaxPageSize {
		limit = MaxPageSize
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s %s %s %s LIMIT %d OFFSET %d",
		selectList(dialect, targetFields),
		dialect.QuoteIdentifier(target.TableName),
		dialect.QuoteIdentifier(target.Column),
		op,
		dialect.Placeholder(1),
		orderClause,
		limit,
		offset,
	)

	rows, err := s.driver.DB().QueryContext(ctx, q, key)
	if err != nil {
		return nil, fmt.Errorf("failed reading records related to %s.%s: %w", fromTable, recordID, err)
	}
	defer rows.Close()

	return rowsToMaps(rows, targetFields)
}

// CreateRelatedRow creates a record on the other side of a relationship and
// fills its match field, so the new record belongs to the record in hand.
//
// It is refused unless the relationship allows creation, and unless the match
// is an equality: with any other comparison there is no single value to write
// into the new record's match field.
func (s *Service) CreateRelatedRow(
	ctx context.Context,
	fromTable, recordID, relationshipID, targetOccurrence string,
	record map[string]interface{},
) (map[string]interface{}, error) {
	rel, err := s.Relationship(ctx, relationshipID)
	if err != nil {
		return nil, err
	}
	source, target, err := rel.Sides(fromTable, targetOccurrence)
	if err != nil {
		return nil, err
	}
	if !rel.AllowCreation {
		return nil, fmt.Errorf("%w: %s", ErrCreationNotAllowed, rel.Name)
	}
	op, err := rel.sqlOperator()
	if err != nil {
		return nil, err
	}
	if op != "=" {
		return nil, fmt.Errorf("%w: relationship %s matches with %q, so a new record has no match value to be given",
			ErrCreationNotAllowed, rel.Name, rel.Operator)
	}

	key, err := s.matchValue(ctx, source.TableName, source.Column, recordID)
	if err != nil {
		return nil, err
	}
	if emptyMatchValue(key) {
		return nil, fmt.Errorf("%w: %s.%s is empty", ErrNoMatchValue, source.TableName, source.Column)
	}

	values := make(map[string]interface{}, len(record)+1)
	for k, v := range record {
		values[k] = v
	}
	// The match field belongs to the relationship, not to the caller: a portal
	// row created here is matched by this record and nothing else.
	values[target.Column] = key

	return s.InsertRow(ctx, target.TableName, values)
}

// cascadingRelationships returns the relationships whose "Delete related
// records" option is on and whose left side is the given table, which is the
// parent-to-child direction a cascade follows.
func (s *Service) cascadingRelationships(ctx context.Context, tableName string) ([]string, error) {
	dialect := s.driver.Dialect()
	q := `SELECT r.id FROM sys_relationships r
		JOIN sys_table_occurrences lo ON lo.id = r.left_occurrence_id
		JOIN sys_tables lt ON lt.id = lo.base_table_id
		WHERE r.cascade_delete = ` + trueLiteral(dialect) + ` AND lt.name = ` + dialect.Placeholder(1)

	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the relationships of %s: %w", tableName, err)
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// trueLiteral is how each engine spells a true boolean in a statement.
func trueLiteral(dialect dbal.Dialect) string {
	if dialect.Engine() == dbal.EnginePostgres {
		return "TRUE"
	}
	return "1"
}

// cascadeDelete deletes the records that belong to the one about to be deleted,
// following every relationship of the table that asks for it.
//
// Children are deleted through DeleteRow itself, so a chain of cascades is
// followed all the way down. seen stops a relationship that points back at a
// record already on the way out.
func (s *Service) cascadeDelete(ctx context.Context, tableName, id string, seen map[string]struct{}) error {
	relationships, err := s.cascadingRelationships(ctx, tableName)
	if err != nil {
		return err
	}
	for _, relID := range relationships {
		rel, err := s.Relationship(ctx, relID)
		if err != nil {
			return err
		}
		// Every child goes, not the first page of them: read a page, delete
		// it, and read again until the relationship matches nothing.
		for {
			children, err := s.RelatedRows(ctx, tableName, id, relID, rel.Right.OccurrenceID,
				QueryOptions{Limit: MaxPageSize})
			if err != nil {
				// A relationship pointing at a table that is no longer a user
				// table must not make a record undeletable.
				if errors.Is(err, ErrTableNotFound) || errors.Is(err, ErrUnknownField) {
					break
				}
				return err
			}
			deleted := 0
			for _, child := range children {
				childID, _ := child["id"].(string)
				if childID == "" {
					continue
				}
				if _, already := seen[seenKey(rel.Right.TableName, childID)]; already {
					continue
				}
				if err := s.deleteRow(ctx, rel.Right.TableName, childID, seen); err != nil {
					return fmt.Errorf("deleting the records related to %s.%s: %w", tableName, id, err)
				}
				deleted++
			}
			// Nothing left that this cascade can remove: either the page was
			// empty or every row in it is already on its way out.
			if deleted == 0 {
				break
			}
		}
	}
	return nil
}

// ─── Find on a related field (#46) ───────────────────────────────────────────

// relatedFindTarget is everything a criterion on a related field needs: the
// table the related records live in, the two match columns, the comparison
// between them, and the fields that table has.
type relatedFindTarget struct {
	TableName    string
	SourceColumn string // the match field of the table being searched
	TargetColumn string // the match field of the related table
	Operator     string // SQL comparison between the two match fields
	Fields       fieldSet
	Relationship string // name, for error messages
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
			source, target, err := rel.Sides(tableName, strings.TrimSpace(crit.Occurrence))
			if err != nil {
				return nil, err
			}
			op, err := rel.sqlOperator()
			if err != nil {
				return nil, err
			}
			fields, err := s.tableFields(ctx, target.TableName)
			if err != nil {
				return nil, err
			}
			targets[key] = relatedFindTarget{
				TableName:    target.TableName,
				SourceColumn: source.Column,
				TargetColumn: target.Column,
				Operator:     op,
				Fields:       fields,
				Relationship: rel.Name,
			}
		}
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
// record's field and not about the absence of related records.
func relatedCriterionClause(
	dialect dbal.Dialect, tableName string, crit FindCriterion,
	targets map[string]relatedFindTarget, idx *int, values *[]interface{},
) (string, error) {
	target, ok := targets[relatedFindKey(crit)]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrRelationshipNotFound, crit.RelationshipID)
	}
	if err := checkField(target.Fields, target.TableName, crit.FieldName); err != nil {
		return "", err
	}

	// The subquery's own name for the related table, so a relationship from a
	// table to itself still has two distinguishable sides.
	const alias = "f4b_rel"
	parent := dialect.QuoteIdentifier(tableName) + "." + dialect.QuoteIdentifier(target.SourceColumn)
	child := alias + "." + dialect.QuoteIdentifier(target.TargetColumn)
	inner := criterionClause(dialect, alias+"."+dialect.QuoteIdentifier(crit.FieldName), crit, idx, values)

	return fmt.Sprintf(
		"(%s IS NOT NULL AND %s <> '' AND EXISTS (SELECT 1 FROM %s %s WHERE %s %s %s AND %s))",
		parent, dialect.CastToText(parent),
		dialect.QuoteIdentifier(target.TableName), alias,
		child, target.Operator, parent,
		inner,
	), nil
}
