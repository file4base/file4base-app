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

type CreateOccurrenceInput struct {
	BaseTableID string  `json:"base_table_id"`
	Name        string  `json:"name"`
	XPos        float64 `json:"x_pos"`
	YPos        float64 `json:"y_pos"`
}

type UpdateOccurrenceInput struct {
	Name *string  `json:"name,omitempty"`
	XPos *float64 `json:"x_pos,omitempty"`
	YPos *float64 `json:"y_pos,omitempty"`
}

type CreateRelationshipInput struct {
	Name              string  `json:"name"`
	LeftOccurrenceID  string  `json:"left_occurrence_id"`
	LeftColumnID      string  `json:"left_column_id"`
	RightOccurrenceID string  `json:"right_occurrence_id"`
	RightColumnID     string  `json:"right_column_id"`
	Operator          string  `json:"operator"`
	AllowCreation     bool    `json:"allow_creation"`
	CascadeDelete     bool    `json:"cascade_delete"`
	SortRelated       *string `json:"sort_related,omitempty"`
}

type UpdateRelationshipInput struct {
	Name          *string `json:"name,omitempty"`
	Operator      *string `json:"operator,omitempty"`
	AllowCreation *bool   `json:"allow_creation,omitempty"`
	CascadeDelete *bool   `json:"cascade_delete,omitempty"`
	SortRelated   *string `json:"sort_related,omitempty"`
}

// CreateTableOccurrence registers a new occurrence of a base table
func (s *Service) CreateTableOccurrence(ctx context.Context, input CreateOccurrenceInput) (*TableOccurrence, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, errors.New("occurrence name cannot be empty")
	}
	if strings.TrimSpace(input.BaseTableID) == "" {
		return nil, errors.New("base_table_id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// Verify base table exists
	qCheckTbl := `SELECT id FROM sys_tables WHERE id = $1 LIMIT 1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qCheckTbl = `SELECT id FROM sys_tables WHERE id = ? LIMIT 1`
	}
	var existingTblID string
	if err := db.QueryRowContext(ctx, qCheckTbl, input.BaseTableID).Scan(&existingTblID); err != nil {
		return nil, fmt.Errorf("base table '%s' not found: %w", input.BaseTableID, err)
	}

	xPos := input.XPos
	yPos := input.YPos
	if xPos == 0 && yPos == 0 {
		xPos = 100.0
		yPos = 100.0
	}

	occID := uuid.NewString()
	now := time.Now().UTC()

	qIns := `INSERT INTO sys_table_occurrences (id, base_table_id, name, x_pos, y_pos, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
	if dialect.Engine() == dbal.EngineMariaDB {
		qIns = `INSERT INTO sys_table_occurrences (id, base_table_id, name, x_pos, y_pos, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	}

	if _, err := db.ExecContext(ctx, qIns, occID, input.BaseTableID, name, xPos, yPos, now); err != nil {
		return nil, fmt.Errorf("failed creating table occurrence: %w", err)
	}

	return &TableOccurrence{
		ID:          occID,
		BaseTableID: input.BaseTableID,
		Name:        name,
		XPos:        xPos,
		YPos:        yPos,
		CreatedAt:   now,
	}, nil
}

// UpdateTableOccurrence updates the name and/or coordinates of an existing table occurrence
func (s *Service) UpdateTableOccurrence(ctx context.Context, id string, input UpdateOccurrenceInput) (*TableOccurrence, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("occurrence id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// Fetch current record
	qGet := `SELECT id, base_table_id, name, x_pos, y_pos, created_at FROM sys_table_occurrences WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qGet = `SELECT id, base_table_id, name, x_pos, y_pos, created_at FROM sys_table_occurrences WHERE id = ?`
	}

	var cur TableOccurrence
	if err := db.QueryRowContext(ctx, qGet, id).Scan(&cur.ID, &cur.BaseTableID, &cur.Name, &cur.XPos, &cur.YPos, &cur.CreatedAt); err != nil {
		return nil, fmt.Errorf("table occurrence not found: %w", err)
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
	return &cur, nil
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
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, name, left_occurrence_id, left_column_id, right_occurrence_id, right_column_id, operator, allow_creation, cascade_delete, sort_related, created_at FROM sys_relationships ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]RelationshipMetadata, 0)
	for rows.Next() {
		var r RelationshipMetadata
		var sortRelated sql.NullString
		if err := rows.Scan(
			&r.ID,
			&r.Name,
			&r.LeftOccurrenceID,
			&r.LeftColumnID,
			&r.RightOccurrenceID,
			&r.RightColumnID,
			&r.Operator,
			&r.AllowCreation,
			&r.CascadeDelete,
			&sortRelated,
			&r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if sortRelated.Valid {
			r.SortRelated = sortRelated.String
		}
		items = append(items, r)
	}
	return items, nil
}

// CreateRelationship registers a new relationship between two table occurrence fields
func (s *Service) CreateRelationship(ctx context.Context, input CreateRelationshipInput) (*RelationshipMetadata, error) {
	if strings.TrimSpace(input.LeftOccurrenceID) == "" || strings.TrimSpace(input.RightOccurrenceID) == "" {
		return nil, errors.New("left_occurrence_id and right_occurrence_id are required")
	}
	if strings.TrimSpace(input.LeftColumnID) == "" || strings.TrimSpace(input.RightColumnID) == "" {
		return nil, errors.New("left_column_id and right_column_id are required")
	}

	op := strings.TrimSpace(input.Operator)
	if op == "" {
		op = "="
	}

	relID := uuid.NewString()
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = fmt.Sprintf("rel_%s_%s", input.LeftOccurrenceID[:8], input.RightOccurrenceID[:8])
	}
	now := time.Now().UTC()

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	qIns := `INSERT INTO sys_relationships (id, name, left_occurrence_id, left_column_id, right_occurrence_id, right_column_id, operator, allow_creation, cascade_delete, sort_related, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	if dialect.Engine() == dbal.EngineMariaDB {
		qIns = `INSERT INTO sys_relationships (id, name, left_occurrence_id, left_column_id, right_occurrence_id, right_column_id, operator, allow_creation, cascade_delete, sort_related, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	}

	var sortRelatedVal interface{}
	if input.SortRelated != nil {
		sortRelatedVal = *input.SortRelated
	}

	if _, err := db.ExecContext(ctx, qIns, relID, name, input.LeftOccurrenceID, input.LeftColumnID, input.RightOccurrenceID, input.RightColumnID, op, input.AllowCreation, input.CascadeDelete, sortRelatedVal, now); err != nil {
		return nil, fmt.Errorf("failed creating relationship: %w", err)
	}

	res := &RelationshipMetadata{
		ID:                relID,
		Name:              name,
		LeftOccurrenceID:  input.LeftOccurrenceID,
		LeftColumnID:      input.LeftColumnID,
		RightOccurrenceID: input.RightOccurrenceID,
		RightColumnID:     input.RightColumnID,
		Operator:          op,
		AllowCreation:     input.AllowCreation,
		CascadeDelete:     input.CascadeDelete,
		CreatedAt:         now,
	}
	if input.SortRelated != nil {
		res.SortRelated = *input.SortRelated
	}
	return res, nil
}

// UpdateRelationship updates relationship configuration
func (s *Service) UpdateRelationship(ctx context.Context, id string, input UpdateRelationshipInput) (*RelationshipMetadata, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("relationship id cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	qGet := `SELECT id, name, left_occurrence_id, left_column_id, right_occurrence_id, right_column_id, operator, allow_creation, cascade_delete, sort_related, created_at FROM sys_relationships WHERE id = $1`
	if dialect.Engine() == dbal.EngineMariaDB {
		qGet = `SELECT id, name, left_occurrence_id, left_column_id, right_occurrence_id, right_column_id, operator, allow_creation, cascade_delete, sort_related, created_at FROM sys_relationships WHERE id = ?`
	}

	var cur RelationshipMetadata
	var sortRelated sql.NullString
	if err := db.QueryRowContext(ctx, qGet, id).Scan(
		&cur.ID,
		&cur.Name,
		&cur.LeftOccurrenceID,
		&cur.LeftColumnID,
		&cur.RightOccurrenceID,
		&cur.RightColumnID,
		&cur.Operator,
		&cur.AllowCreation,
		&cur.CascadeDelete,
		&sortRelated,
		&cur.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("relationship not found: %w", err)
	}
	if sortRelated.Valid {
		cur.SortRelated = sortRelated.String
	}

	if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
		cur.Name = strings.TrimSpace(*input.Name)
	}
	if input.Operator != nil && strings.TrimSpace(*input.Operator) != "" {
		cur.Operator = strings.TrimSpace(*input.Operator)
	}
	if input.AllowCreation != nil {
		cur.AllowCreation = *input.AllowCreation
	}
	if input.CascadeDelete != nil {
		cur.CascadeDelete = *input.CascadeDelete
	}
	if input.SortRelated != nil {
		cur.SortRelated = *input.SortRelated
	}

	qUpd := `UPDATE sys_relationships SET name = $1, operator = $2, allow_creation = $3, cascade_delete = $4, sort_related = $5 WHERE id = $6`
	if dialect.Engine() == dbal.EngineMariaDB {
		qUpd = `UPDATE sys_relationships SET name = ?, operator = ?, allow_creation = ?, cascade_delete = ?, sort_related = ? WHERE id = ?`
	}

	if _, err := db.ExecContext(ctx, qUpd, cur.Name, cur.Operator, cur.AllowCreation, cur.CascadeDelete, cur.SortRelated, id); err != nil {
		return nil, fmt.Errorf("failed updating relationship: %w", err)
	}

	return &cur, nil
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
