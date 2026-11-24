package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// Multi-predicate relationships.
//
// A FileMaker relationship joins two table occurrences on one or more match
// criteria — Orders::Customer = Customers::Code AND Orders::Date ≥
// Customers::Since — and relates the records that meet all of them. File4Base
// keeps the first criterion where it always was, in the left_column_id,
// operator and right_column_id of sys_relationships, so a relationship with a
// single criterion and every client that only knows that pair are unchanged.
// The criteria after it are kept in sys_relationship_predicates, numbered from
// 1 in the order they are matched in.
//
// Each side of a criterion is kept as the field's id and its name. The id is
// what the criterion is resolved by whenever it is there; the name is kept so
// that a side whose field has no local id — an occurrence of another
// solution's table — can be stored the same way later.

// RelationshipPredicatesTable holds every criterion of a relationship but its
// first.
const RelationshipPredicatesTable = "sys_relationship_predicates"

// relationshipPredicatesSQL creates the table; ts is the engine's timestamp
// column type. server/migrations/002_relationship_predicates.sql is the same
// statement for PostgreSQL.
func relationshipPredicatesSQL(ts string) string {
	return `CREATE TABLE IF NOT EXISTS ` + RelationshipPredicatesTable + ` (
			id VARCHAR(36) PRIMARY KEY,
			relationship_id VARCHAR(36) NOT NULL REFERENCES sys_relationships(id) ON DELETE CASCADE,
			sequence_idx INT NOT NULL,
			left_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
			left_column_name VARCHAR(64),
			operator VARCHAR(8) NOT NULL DEFAULT '=',
			right_column_id VARCHAR(36) NOT NULL REFERENCES sys_columns(id) ON DELETE CASCADE,
			right_column_name VARCHAR(64),
			created_at ` + ts + `,
			CONSTRAINT uq_relationship_predicate UNIQUE (relationship_id, sequence_idx)
		);`
}

// relationshipColumnNamesSQL gives the first criterion's sides their field
// names too, on a database created before they were kept.
var relationshipColumnNamesSQL = []string{
	`ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS left_column_name VARCHAR(64);`,
	`ALTER TABLE sys_relationships ADD COLUMN IF NOT EXISTS right_column_name VARCHAR(64);`,
}

// RelationshipPredicate is one match criterion of a relationship: a field of
// the left occurrence, compared with a field of the right occurrence. A field
// is named by its id; its name is returned with it, and is ignored on input
// when the id is given.
type RelationshipPredicate struct {
	LeftColumnID    string `json:"left_column_id"`
	LeftColumnName  string `json:"left_column_name,omitempty"`
	Operator        string `json:"operator"`
	RightColumnID   string `json:"right_column_id"`
	RightColumnName string `json:"right_column_name,omitempty"`
}

// ErrInvalidRelationship is returned when a relationship's criteria cannot be
// matched: no criterion, an unknown operator, or a field that is not in the
// table of the occurrence it is on.
var ErrInvalidRelationship = errors.New("invalid relationship")

// relationshipOperators are the comparisons a criterion may use, written as
// the Specify Relationship dialog writes them, with the spellings other
// clients and earlier versions sent.
var relationshipOperators = map[string]string{
	"=": "=", "==": "=",
	"≠": "≠", "<>": "≠", "!=": "≠",
	"<": "<",
	"≤": "≤", "<=": "≤",
	">": ">",
	"≥": "≥", ">=": "≥",
	"×": "×", "x": "×", "X": "×",
}

// NormalizeRelationshipOperator returns the canonical sign of a relationship
// operator. An empty operator is "=", which is what a relationship without one
// has always meant.
func NormalizeRelationshipOperator(op string) (string, error) {
	op = strings.TrimSpace(op)
	if op == "" {
		return "=", nil
	}
	if canonical, ok := relationshipOperators[op]; ok {
		return canonical, nil
	}
	return "", fmt.Errorf("%w: unsupported operator %q (use =, ≠, <, ≤, >, ≥ or ×)", ErrInvalidRelationship, op)
}

// predicateList is the criteria a create request asks for: its predicates
// when it sends them, otherwise its one pair of fields.
func (in CreateRelationshipInput) predicateList() ([]RelationshipPredicate, error) {
	if in.Predicates != nil {
		if len(in.Predicates) == 0 {
			return nil, fmt.Errorf("%w: a relationship needs at least one match criterion", ErrInvalidRelationship)
		}
		return in.Predicates, nil
	}
	if strings.TrimSpace(in.LeftColumnID) == "" || strings.TrimSpace(in.RightColumnID) == "" {
		return nil, errors.New("left_column_id and right_column_id are required")
	}
	return []RelationshipPredicate{{LeftColumnID: in.LeftColumnID, Operator: in.Operator, RightColumnID: in.RightColumnID}}, nil
}

// checkPredicates normalizes the criteria and refuses those that cannot be
// matched: each field must be one of the fields of its occurrence's table, and
// no field may be an unstored calculation, which has nothing in the database
// to match on. A Cartesian criterion (×) matches every record whatever its
// fields hold, so its fields are not read and may be anything of the table.
func (s *Service) checkPredicates(ctx context.Context, leftOccurrenceID, rightOccurrenceID string, predicates []RelationshipPredicate) ([]RelationshipPredicate, error) {
	if len(predicates) == 0 {
		return nil, fmt.Errorf("%w: a relationship needs at least one match criterion", ErrInvalidRelationship)
	}
	leftName, leftTable, err := s.occurrenceTable(ctx, leftOccurrenceID)
	if err != nil {
		return nil, err
	}
	rightName, rightTable, err := s.occurrenceTable(ctx, rightOccurrenceID)
	if err != nil {
		return nil, err
	}

	out := make([]RelationshipPredicate, len(predicates))
	for i, p := range predicates {
		n := i + 1
		op, err := NormalizeRelationshipOperator(p.Operator)
		if err != nil {
			return nil, fmt.Errorf("criterion %d: %w", n, err)
		}
		p.Operator = op
		p.LeftColumnID, p.RightColumnID = strings.TrimSpace(p.LeftColumnID), strings.TrimSpace(p.RightColumnID)
		if p.LeftColumnID == "" || p.RightColumnID == "" {
			return nil, fmt.Errorf("%w: criterion %d needs a field on each side", ErrInvalidRelationship, n)
		}
		for _, side := range []struct {
			column, occurrence, table string
			name                      *string
		}{
			{p.LeftColumnID, leftName, leftTable, &p.LeftColumnName},
			{p.RightColumnID, rightName, rightTable, &p.RightColumnName},
		} {
			name, table, err := s.columnTable(ctx, side.column)
			if err != nil {
				return nil, fmt.Errorf("%w: criterion %d: %v", ErrInvalidRelationship, n, err)
			}
			if table != side.table {
				return nil, fmt.Errorf("%w: criterion %d: the field %q is not in the table of %q",
					ErrInvalidRelationship, n, name, side.occurrence)
			}
			*side.name = name
			if op != "×" {
				if err := s.refuseUnstoredMatchField(ctx, side.column); err != nil {
					return nil, err
				}
			}
		}
		out[i] = p
	}
	return out, nil
}

func (s *Service) occurrenceTable(ctx context.Context, occurrenceID string) (name, tableID string, err error) {
	q := `SELECT name, base_table_id FROM sys_table_occurrences WHERE id = ` + s.driver.Dialect().Placeholder(1)
	err = s.driver.DB().QueryRowContext(ctx, q, occurrenceID).Scan(&name, &tableID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: table occurrence %q not found", ErrInvalidRelationship, occurrenceID)
	}
	return name, tableID, err
}

func (s *Service) columnTable(ctx context.Context, columnID string) (name, tableID string, err error) {
	q := `SELECT name, table_id FROM sys_columns WHERE id = ` + s.driver.Dialect().Placeholder(1)
	err = s.driver.DB().QueryRowContext(ctx, q, columnID).Scan(&name, &tableID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("field %q not found", columnID)
	}
	return name, tableID, err
}

// ensureRelationshipPredicates creates the criteria table, and the field-name
// columns of sys_relationships, in a database created before they existed. It
// runs outside any transaction: MariaDB commits one on DDL.
func (s *Service) ensureRelationshipPredicates(ctx context.Context) error {
	for _, q := range append([]string{relationshipPredicatesSQL(s.driver.Dialect().TimestampColumn())}, relationshipColumnNamesSQL...) {
		if _, err := s.driver.DB().ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed creating %s: %w", RelationshipPredicatesTable, err)
		}
	}
	return nil
}

// writeExtraPredicates replaces the criteria after the first of a
// relationship.
func (s *Service) writeExtraPredicates(ctx context.Context, tx *sql.Tx, relationshipID string, predicates []RelationshipPredicate) error {
	d := s.driver.Dialect()
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+RelationshipPredicatesTable+` WHERE relationship_id = `+d.Placeholder(1), relationshipID); err != nil {
		return fmt.Errorf("failed clearing the criteria of the relationship: %w", err)
	}
	if len(predicates) < 2 {
		return nil
	}
	marks := make([]string, 9)
	for i := range marks {
		marks[i] = d.Placeholder(i + 1)
	}
	ins := fmt.Sprintf(`INSERT INTO %s (id, relationship_id, sequence_idx, left_column_id, left_column_name, operator, right_column_id, right_column_name, created_at) VALUES (%s)`,
		RelationshipPredicatesTable, strings.Join(marks, ", "))
	now := time.Now().UTC()
	for i, p := range predicates[1:] {
		if _, err := tx.ExecContext(ctx, ins, uuid.NewString(), relationshipID, i+1,
			p.LeftColumnID, p.LeftColumnName, p.Operator, p.RightColumnID, p.RightColumnName, now); err != nil {
			return fmt.Errorf("failed saving criterion %d of the relationship: %w", i+2, err)
		}
	}
	return nil
}

// extraPredicates reads the criteria after the first of every relationship,
// or of one when relationshipID is not empty, in order. A field is named as it
// is now, through its id, and by the name kept with it only when the id
// resolves to nothing. A database created before the table existed has none.
func (s *Service) extraPredicates(ctx context.Context, relationshipID string) (map[string][]RelationshipPredicate, error) {
	q := `SELECT p.relationship_id, p.left_column_id, COALESCE(lc.name, p.left_column_name, ''), p.operator,
			p.right_column_id, COALESCE(rc.name, p.right_column_name, '')
		FROM ` + RelationshipPredicatesTable + ` p
		LEFT JOIN sys_columns lc ON lc.id = p.left_column_id
		LEFT JOIN sys_columns rc ON rc.id = p.right_column_id`
	var args []interface{}
	if relationshipID != "" {
		q += ` WHERE p.relationship_id = ` + s.driver.Dialect().Placeholder(1)
		args = append(args, relationshipID)
	}
	q += ` ORDER BY p.relationship_id, p.sequence_idx`
	rows, err := s.driver.DB().QueryContext(ctx, q, args...)
	if dbal.IsUndefinedTable(err) {
		return map[string][]RelationshipPredicate{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed reading the relationship criteria: %w", err)
	}
	defer rows.Close()
	out := map[string][]RelationshipPredicate{}
	for rows.Next() {
		var id string
		var p RelationshipPredicate
		if err := rows.Scan(&id, &p.LeftColumnID, &p.LeftColumnName, &p.Operator, &p.RightColumnID, &p.RightColumnName); err != nil {
			return nil, err
		}
		out[id] = append(out[id], p)
	}
	return out, rows.Err()
}

// attachPredicates fills a relationship's ordered criteria: the pair kept on
// the relationship itself, with its field names, then the further ones.
func (r *RelationshipMetadata) attachPredicates(leftName, rightName string, extra []RelationshipPredicate) {
	r.Predicates = append([]RelationshipPredicate{{
		LeftColumnID: r.LeftColumnID, LeftColumnName: leftName, Operator: r.Operator,
		RightColumnID: r.RightColumnID, RightColumnName: rightName,
	}}, extra...)
}

// AllEqual reports whether every criterion is an equality, the only kind a
// record can be created through: there is one value to give each match field.
func AllEqual(predicates []RelationshipPredicate) bool {
	for _, p := range predicates {
		if op, err := NormalizeRelationshipOperator(p.Operator); err != nil || op != "=" {
			return false
		}
	}
	return true
}

// relationshipsUsingColumn names the relationships one of whose criteria
// after the first matches on a column. The first criterion's columns are
// foreign keys of sys_relationships, and deleting the column deletes those
// relationships with it.
func (s *Service) relationshipsUsingColumn(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, columnID string) ([]string, error) {
	d := s.driver.Dialect()
	query := `SELECT DISTINCT relationship_id FROM ` + RelationshipPredicatesTable +
		` WHERE left_column_id = ` + d.Placeholder(1) + ` OR right_column_id = ` + d.Placeholder(2)
	rows, err := q.QueryContext(ctx, query, columnID, columnID)
	if dbal.IsUndefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
