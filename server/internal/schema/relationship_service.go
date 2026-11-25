package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// CreateOccurrenceInput creates a table occurrence: of a base table of this
// solution (BaseTableID), or of a table of another solution (External).
type CreateOccurrenceInput struct {
	BaseTableID string                   `json:"base_table_id"`
	Name        string                   `json:"name"`
	XPos        float64                  `json:"x_pos"`
	YPos        float64                  `json:"y_pos"`
	External    *ExternalOccurrenceInput `json:"external,omitempty"`
}

// UpdateOccurrenceInput renames or moves an occurrence. ExternalTable names
// the table of an external occurrence in the other solution (empty when not
// known); an occurrence is never turned from local to external or back.
type UpdateOccurrenceInput struct {
	Name          *string  `json:"name,omitempty"`
	XPos          *float64 `json:"x_pos,omitempty"`
	YPos          *float64 `json:"y_pos,omitempty"`
	ExternalTable *string  `json:"external_table,omitempty"`
}

// relationshipSideColumns are the options of a relationship's left side, which
// a catalog created before they were kept lacks: such a relationship keeps
// the options it had, on its right side, and none on its left.
// server/migrations/006 adds the same.
var relationshipSideColumns = []struct{ table, column, typ string }{
	{"sys_relationships", "left_allow_creation", "BOOLEAN NOT NULL DEFAULT FALSE"},
	{"sys_relationships", "left_cascade_delete", "BOOLEAN NOT NULL DEFAULT FALSE"},
	{"sys_relationships", "left_sort_related", "TEXT"},
}

// CreateRelationshipInput creates a relationship. Its match criteria are
// Predicates, in order; a client that sends none gives its one criterion as
// LeftColumnID, Operator and RightColumnID, as before relationships could
// have several.
//
// AllowCreation, CascadeDelete and SortRelated are the right side's options,
// LeftAllowCreation, LeftCascadeDelete and LeftSortRelated the left side's;
// each acts on the side it is set on (see RelationshipMetadata).
type CreateRelationshipInput struct {
	Name              string                  `json:"name"`
	LeftOccurrenceID  string                  `json:"left_occurrence_id"`
	LeftColumnID      string                  `json:"left_column_id"`
	RightOccurrenceID string                  `json:"right_occurrence_id"`
	RightColumnID     string                  `json:"right_column_id"`
	Operator          string                  `json:"operator"`
	Predicates        []RelationshipPredicate `json:"predicates,omitempty"`
	AllowCreation     bool                    `json:"allow_creation"`
	CascadeDelete     bool                    `json:"cascade_delete"`
	SortRelated       *string                 `json:"sort_related,omitempty"`
	LeftAllowCreation bool                    `json:"left_allow_creation"`
	LeftCascadeDelete bool                    `json:"left_cascade_delete"`
	LeftSortRelated   *string                 `json:"left_sort_related,omitempty"`
}

// UpdateRelationshipInput changes a relationship. Predicates, when sent,
// replaces every criterion; Operator alone changes the first one's operator.
type UpdateRelationshipInput struct {
	Name          *string                 `json:"name,omitempty"`
	Operator      *string                 `json:"operator,omitempty"`
	Predicates    []RelationshipPredicate `json:"predicates,omitempty"`
	AllowCreation *bool                   `json:"allow_creation,omitempty"`
	CascadeDelete *bool                   `json:"cascade_delete,omitempty"`
	SortRelated   *string                 `json:"sort_related,omitempty"`

	LeftAllowCreation *bool   `json:"left_allow_creation,omitempty"`
	LeftCascadeDelete *bool   `json:"left_cascade_delete,omitempty"`
	LeftSortRelated   *string `json:"left_sort_related,omitempty"`
}

// CreateTableOccurrence registers a new occurrence of a base table, or of a
// table of another solution through a file reference.
func (s *Service) CreateTableOccurrence(ctx context.Context, input CreateOccurrenceInput) (*TableOccurrence, error) {
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("occurrence name cannot be empty")
	}
	baseTableID := strings.TrimSpace(input.BaseTableID)
	var external *ExternalOccurrence
	switch {
	case input.External != nil && baseTableID != "":
		return nil, fmt.Errorf("%w: an occurrence has either a base table or an external data source, not both", ErrInvalidOccurrence)
	case input.External != nil:
		ref, err := s.fileReferenceByIDOrName(ctx, input.External.FileReference)
		if err != nil {
			return nil, err
		}
		table, err := normalizeExternalTable(input.External.Table)
		if err != nil {
			return nil, err
		}
		external = &ExternalOccurrence{FileReferenceID: ref.ID, FileReference: ref.Name, Database: ref.Database, Table: table}
	case baseTableID == "":
		return nil, errors.New("base_table_id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	if external == nil {
		// Verify base table exists
		qCheckTbl := `SELECT id FROM sys_tables WHERE id = $1 LIMIT 1`
		if dialect.Engine() == dbal.EngineMariaDB {
			qCheckTbl = `SELECT id FROM sys_tables WHERE id = ? LIMIT 1`
		}
		var existingTblID string
		if err := db.QueryRowContext(ctx, qCheckTbl, baseTableID).Scan(&existingTblID); err != nil {
			return nil, fmt.Errorf("base table '%s' not found: %w", baseTableID, err)
		}
	}

	xPos := input.XPos
	yPos := input.YPos
	if xPos == 0 && yPos == 0 {
		xPos = 100.0
		yPos = 100.0
	}

	occID := uuid.NewString()
	now := time.Now().UTC()

	var err error
	if external == nil {
		qIns := `INSERT INTO sys_table_occurrences (id, base_table_id, name, x_pos, y_pos, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
		if dialect.Engine() == dbal.EngineMariaDB {
			qIns = `INSERT INTO sys_table_occurrences (id, base_table_id, name, x_pos, y_pos, created_at) VALUES (?, ?, ?, ?, ?, ?)`
		}
		_, err = db.ExecContext(ctx, qIns, occID, baseTableID, name, xPos, yPos, now)
	} else {
		marks := make([]string, 7)
		for i := range marks {
			marks[i] = dialect.Placeholder(i + 1)
		}
		qIns := `INSERT INTO sys_table_occurrences (id, name, x_pos, y_pos, created_at, file_reference_id, external_table) VALUES (` +
			strings.Join(marks, ", ") + `)`
		_, err = db.ExecContext(ctx, qIns, occID, name, xPos, yPos, now, external.FileReferenceID, nullable(external.Table))
	}
	if err != nil {
		return nil, fmt.Errorf("failed creating table occurrence: %w", err)
	}

	return &TableOccurrence{
		ID:          occID,
		BaseTableID: baseTableID,
		Name:        name,
		XPos:        xPos,
		YPos:        yPos,
		CreatedAt:   now,
		External:    external,
	}, nil
}

// UpdateTableOccurrence updates the name and/or coordinates of an existing
// table occurrence, and the table of an external one.
func (s *Service) UpdateTableOccurrence(ctx context.Context, id string, input UpdateOccurrenceInput) (*TableOccurrence, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("occurrence id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	cur, err := s.GetTableOccurrence(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.ExternalTable != nil {
		if cur.External == nil {
			return nil, fmt.Errorf("%w: %q is an occurrence of a table of this solution, not of another solution", ErrInvalidOccurrence, cur.Name)
		}
		table, err := normalizeExternalTable(*input.ExternalTable)
		if err != nil {
			return nil, err
		}
		if err := s.setExternalTable(ctx, id, table); err != nil {
			return nil, err
		}
		cur.External.Table = table
	}

	newName := cur.Name
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		newName = strings.TrimSpace(*input.Name)
	}
	newX := cur.XPos
	if input.XPos != nil {
		newX = *input.XPos
	}
	newY := cur.YPos
	if input.YPos != nil {
		newY = *input.YPos
	}

	qUpd := `UPDATE sys_table_occurrences SET name = $1, x_pos = $2, y_pos = $3 WHERE id = $4`
	if dialect.Engine() == dbal.EngineMariaDB {
		qUpd = `UPDATE sys_table_occurrences SET name = ?, x_pos = ?, y_pos = ? WHERE id = ?`
	}

	if _, err := db.ExecContext(ctx, qUpd, newName, newX, newY, id); err != nil {
		return nil, fmt.Errorf("failed updating table occurrence: %w", err)
	}

	cur.Name = newName
	cur.XPos = newX
	cur.YPos = newY
	return cur, nil
}

// DeleteTableOccurrence deletes a table occurrence
func (s *Service) DeleteTableOccurrence(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("occurrence id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	qDel := `DELETE FROM sys_table_occurrences WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qDel = `DELETE FROM sys_table_occurrences WHERE id = ?`
	}

	res, err := db.ExecContext(ctx, qDel, id)
	if err != nil {
		return fmt.Errorf("failed deleting table occurrence: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("table occurrence not found")
	}
	return nil
}

// ListRelationships returns all relationships defined in sys_relationships
func (s *Service) ListRelationships(ctx context.Context) ([]RelationshipMetadata, error) {
	// A catalog not upgraded since the server was lacks the left side's
	// options; sign-in adds them, and so does this on a database reached
	// before anyone signed in to it.
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT `+relationshipColumns+` FROM sys_relationships r
		LEFT JOIN sys_columns lc ON lc.id = r.left_column_id
		LEFT JOIN sys_columns rc ON rc.id = r.right_column_id
		ORDER BY r.created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]RelationshipMetadata, 0)
	names := make([][2]string, 0)
	for rows.Next() {
		r, leftName, rightName, err := scanRelationshipMetadata(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, r)
		names = append(names, [2]string{leftName, rightName})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	extra, err := s.extraPredicates(ctx, "")
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].attachPredicates(names[i][0], names[i][1], extra[items[i].ID])
	}
	return items, nil
}

// relationshipColumns is what scanRelationshipMetadata reads, from
// sys_relationships r joined to the columns of its first criterion as lc and
// rc.
//
// A side on an occurrence of another solution's table has no column id; its
// field is the name kept with it.
const relationshipColumns = `r.id, r.name, r.left_occurrence_id, r.left_column_id, r.right_occurrence_id, r.right_column_id,
	r.operator, r.allow_creation, r.cascade_delete, r.sort_related, r.created_at,
	COALESCE(lc.name, r.left_column_name, ''), COALESCE(rc.name, r.right_column_name, ''),
	r.left_allow_creation, r.left_cascade_delete, r.left_sort_related`

func scanRelationshipMetadata(row interface{ Scan(...any) error }) (r RelationshipMetadata, leftName, rightName string, err error) {
	var sortRelated, leftSortRelated, operator, leftColumn, rightColumn sql.NullString
	var leftAllowCreation, leftCascadeDelete sql.NullBool
	err = row.Scan(
		&r.ID, &r.Name, &r.LeftOccurrenceID, &leftColumn, &r.RightOccurrenceID, &rightColumn,
		&operator, &r.AllowCreation, &r.CascadeDelete, &sortRelated, &r.CreatedAt,
		&leftName, &rightName,
		&leftAllowCreation, &leftCascadeDelete, &leftSortRelated,
	)
	r.LeftColumnID, r.RightColumnID = leftColumn.String, rightColumn.String
	r.SortRelated = sortRelated.String
	r.LeftAllowCreation, r.LeftCascadeDelete, r.LeftSortRelated =
		leftAllowCreation.Bool, leftCascadeDelete.Bool, leftSortRelated.String
	r.Operator = operator.String
	if r.Operator == "" {
		r.Operator = "="
	}
	return r, leftName, rightName, err
}

// CreateRelationship registers a new relationship between two table
// occurrences, matched on every one of its criteria.
func (s *Service) CreateRelationship(ctx context.Context, input CreateRelationshipInput) (*RelationshipMetadata, error) {
	if strings.TrimSpace(input.LeftOccurrenceID) == "" || strings.TrimSpace(input.RightOccurrenceID) == "" {
		return nil, errors.New("left_occurrence_id and right_occurrence_id are required")
	}
	predicates, err := input.predicateList()
	if err != nil {
		return nil, err
	}
	if predicates, err = s.checkPredicates(ctx, input.LeftOccurrenceID, input.RightOccurrenceID, predicates); err != nil {
		return nil, err
	}
	first := predicates[0]

	relID := uuid.NewString()
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = fmt.Sprintf("rel_%s_%s", input.LeftOccurrenceID[:min(8, len(input.LeftOccurrenceID))], input.RightOccurrenceID[:min(8, len(input.RightOccurrenceID))])
	}
	now := time.Now().UTC()

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	if err := s.ensureRelationshipPredicates(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}

	marks := make([]string, 16)
	for i := range marks {
		marks[i] = dialect.Placeholder(i + 1)
	}
	qIns := `INSERT INTO sys_relationships (id, name, left_occurrence_id, left_column_id, left_column_name, right_occurrence_id, right_column_id, right_column_name, operator,
		allow_creation, cascade_delete, sort_related, left_allow_creation, left_cascade_delete, left_sort_related, created_at) VALUES (` + strings.Join(marks, ", ") + `)`

	var sortRelatedVal, leftSortRelatedVal interface{}
	if input.SortRelated != nil {
		sortRelatedVal = *input.SortRelated
	}
	if input.LeftSortRelated != nil {
		leftSortRelatedVal = *input.LeftSortRelated
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, qIns, relID, name, input.LeftOccurrenceID, nullable(first.LeftColumnID), first.LeftColumnName,
		input.RightOccurrenceID, nullable(first.RightColumnID), first.RightColumnName, first.Operator,
		input.AllowCreation, input.CascadeDelete, sortRelatedVal,
		input.LeftAllowCreation, input.LeftCascadeDelete, leftSortRelatedVal, now); err != nil {
		return nil, fmt.Errorf("failed creating relationship: %w", err)
	}
	if len(predicates) > 1 {
		if err := s.writeExtraPredicates(ctx, tx, relID, predicates); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	res := &RelationshipMetadata{
		ID:                relID,
		Name:              name,
		LeftOccurrenceID:  input.LeftOccurrenceID,
		LeftColumnID:      first.LeftColumnID,
		RightOccurrenceID: input.RightOccurrenceID,
		RightColumnID:     first.RightColumnID,
		Operator:          first.Operator,
		Predicates:        predicates,
		AllowCreation:     input.AllowCreation,
		CascadeDelete:     input.CascadeDelete,
		LeftAllowCreation: input.LeftAllowCreation,
		LeftCascadeDelete: input.LeftCascadeDelete,
		CreatedAt:         now,
	}
	if input.SortRelated != nil {
		res.SortRelated = *input.SortRelated
	}
	if input.LeftSortRelated != nil {
		res.LeftSortRelated = *input.LeftSortRelated
	}
	return res, nil
}

// UpdateRelationship updates relationship configuration
func (s *Service) UpdateRelationship(ctx context.Context, id string, input UpdateRelationshipInput) (*RelationshipMetadata, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("relationship id cannot be empty")
	}

	cur, err := s.relationshipByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Only what the request carries is written. Writing back what was read
	// would undo a change made in between: a rename racing a replacement of
	// the criteria would restore the old first criterion.
	dialect := s.driver.Dialect()
	var sets []string
	var args []interface{}
	set := func(column string, value interface{}) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = %s", column, dialect.Placeholder(len(args))))
	}
	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		set("name", strings.TrimSpace(*input.Name))
	}
	var predicates []RelationshipPredicate
	switch {
	case input.Predicates != nil:
		// The occurrences of a relationship never change, so the criteria
		// can be checked against those read above.
		predicates, err = s.checkPredicates(ctx, cur.LeftOccurrenceID, cur.RightOccurrenceID, input.Predicates)
		if err != nil {
			return nil, err
		}
		first := predicates[0]
		set("left_column_id", nullable(first.LeftColumnID))
		set("left_column_name", first.LeftColumnName)
		set("operator", first.Operator)
		set("right_column_id", nullable(first.RightColumnID))
		set("right_column_name", first.RightColumnName)
	case input.Operator != nil && strings.TrimSpace(*input.Operator) != "":
		op, err := NormalizeRelationshipOperator(*input.Operator)
		if err != nil {
			return nil, err
		}
		set("operator", op)
	}
	if input.AllowCreation != nil {
		set("allow_creation", *input.AllowCreation)
	}
	if input.CascadeDelete != nil {
		set("cascade_delete", *input.CascadeDelete)
	}
	if input.SortRelated != nil {
		set("sort_related", *input.SortRelated)
	}
	if input.LeftAllowCreation != nil {
		set("left_allow_creation", *input.LeftAllowCreation)
	}
	if input.LeftCascadeDelete != nil {
		set("left_cascade_delete", *input.LeftCascadeDelete)
	}
	if input.LeftSortRelated != nil {
		set("left_sort_related", *input.LeftSortRelated)
	}
	if len(sets) == 0 {
		return cur, nil
	}
	if predicates != nil {
		if err := s.ensureRelationshipPredicates(ctx); err != nil {
			return nil, err
		}
	}

	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	args = append(args, id)
	qUpd := fmt.Sprintf("UPDATE sys_relationships SET %s WHERE id = %s", strings.Join(sets, ", "), dialect.Placeholder(len(args)))
	if _, err := tx.ExecContext(ctx, qUpd, args...); err != nil {
		return nil, fmt.Errorf("failed updating relationship: %w", err)
	}
	if predicates != nil {
		if err := s.writeExtraPredicates(ctx, tx, id, predicates); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.relationshipByID(ctx, id)
}

// relationshipByID reads one relationship with every criterion.
func (s *Service) relationshipByID(ctx context.Context, id string) (*RelationshipMetadata, error) {
	if err := s.ensureExternalCatalog(ctx); err != nil {
		return nil, err
	}
	qGet := `SELECT ` + relationshipColumns + ` FROM sys_relationships r
		LEFT JOIN sys_columns lc ON lc.id = r.left_column_id
		LEFT JOIN sys_columns rc ON rc.id = r.right_column_id
		WHERE r.id = ` + s.driver.Dialect().Placeholder(1)
	rel, leftName, rightName, err := scanRelationshipMetadata(s.driver.DB().QueryRowContext(ctx, qGet, id))
	if err != nil {
		return nil, fmt.Errorf("relationship not found: %w", err)
	}
	extra, err := s.extraPredicates(ctx, id)
	if err != nil {
		return nil, err
	}
	rel.attachPredicates(leftName, rightName, extra[id])
	return &rel, nil
}

// DeleteRelationship removes a relationship definition
func (s *Service) DeleteRelationship(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("relationship id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	qDel := `DELETE FROM sys_relationships WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qDel = `DELETE FROM sys_relationships WHERE id = ?`
	}

	res, err := db.ExecContext(ctx, qDel, id)
	if err != nil {
		return fmt.Errorf("failed deleting relationship: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("relationship not found")
	}
	return nil
}

// refuseUnstoredMatchField refuses to match a relationship on an unstored
// calculation: it is not kept in the database, so there is nothing to match.
func (s *Service) refuseUnstoredMatchField(ctx context.Context, columnID string) error {
	q := `SELECT name, field_type, calculation_formula FROM sys_columns WHERE id = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		q = `SELECT name, field_type, calculation_formula FROM sys_columns WHERE id = ?`
	}
	var name, fieldType string
	var formula *string
	if err := s.driver.DB().QueryRowContext(ctx, q, columnID).Scan(&name, &fieldType, &formula); err != nil {
		return nil // an unknown column is refused by the insert itself
	}
	if fieldType == string(dbal.FieldTypeCalculation) && formula != nil && !data.ReadStoredCalculation(*formula).IsStored() {
		return fmt.Errorf("%w: a relationship cannot match on %q, an unstored calculation: store its result first",
			ErrInvalidFieldOptions, name)
	}
	return nil
}
