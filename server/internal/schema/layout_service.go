package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// TableOccurrence represents an occurrence in the relationship graph
type TableOccurrence struct {
	ID          string    `json:"id"`
	BaseTableID string    `json:"base_table_id"`
	Name        string    `json:"name"`
	XPos        float64   `json:"x_pos"`
	YPos        float64   `json:"y_pos"`
	CreatedAt   time.Time `json:"created_at"`
}

// LayoutMetadata represents a layout entry in sys_layouts
type LayoutMetadata struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	TableOccurrenceID string          `json:"table_occurrence_id"`
	Definition        json.RawMessage `json:"definition"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// ListTableOccurrences returns all table occurrences
func (s *Service) ListTableOccurrences(ctx context.Context) ([]TableOccurrence, error) {
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, base_table_id, name, x_pos, y_pos, created_at FROM sys_table_occurrences ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]TableOccurrence, 0)
	for rows.Next() {
		var o TableOccurrence
		if err := rows.Scan(&o.ID, &o.BaseTableID, &o.Name, &o.XPos, &o.YPos, &o.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, nil
}

// CreateLayout persists a new visual layout
func (s *Service) CreateLayout(ctx context.Context, name string, toID string, definition json.RawMessage) (*LayoutMetadata, error) {
	if name == "" {
		return nil, errors.New("layout name cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()

	// If toID is empty, fallback to the first available table occurrence
	if toID == "" {
		var firstTOID string
		err := db.QueryRowContext(ctx, `SELECT id FROM sys_table_occurrences LIMIT 1`).Scan(&firstTOID)
		if err != nil {
			return nil, fmt.Errorf("no table occurrence found for layout: %w", err)
		}
		toID = firstTOID
	}

	layoutID := uuid.NewString()
	now := time.Now().UTC()

	defStr := string(definition)
	if defStr == "" || defStr == "null" {
		defStr = "{}"
	}

	query := `INSERT INTO sys_layouts (id, name, table_occurrence_id, definition, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	if dialect.Engine() == dbal.EngineMariaDB {
		query = `INSERT INTO sys_layouts (id, name, table_occurrence_id, definition, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	}

	if _, err := db.ExecContext(ctx, query, layoutID, name, toID, defStr, now, now); err != nil {
		return nil, fmt.Errorf("failed creating layout: %w", err)
	}

	return &LayoutMetadata{
		ID:                layoutID,
		Name:              name,
		TableOccurrenceID: toID,
		Definition:        json.RawMessage(defStr),
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

// GetLayout retrieves a single layout by ID
func (s *Service) GetLayout(ctx context.Context, id string) (*LayoutMetadata, error) {
	db := s.driver.DB()
	query := `SELECT id, name, table_occurrence_id, definition, created_at, updated_at FROM sys_layouts WHERE id = $1 LIMIT 1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		query = `SELECT id, name, table_occurrence_id, definition, created_at, updated_at FROM sys_layouts WHERE id = ? LIMIT 1`
	}

	var l LayoutMetadata
	var defStr string
	err := db.QueryRowContext(ctx, query, id).Scan(&l.ID, &l.Name, &l.TableOccurrenceID, &defStr, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("layout %s not found", id)
		}
		return nil, err
	}
	l.Definition = json.RawMessage(defStr)
	return &l, nil
}

// ListLayouts retrieves all layouts
func (s *Service) ListLayouts(ctx context.Context) ([]LayoutMetadata, error) {
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, name, table_occurrence_id, definition, created_at, updated_at FROM sys_layouts ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	layouts := make([]LayoutMetadata, 0)
	for rows.Next() {
		var l LayoutMetadata
		var defStr string
		if err := rows.Scan(&l.ID, &l.Name, &l.TableOccurrenceID, &defStr, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		l.Definition = json.RawMessage(defStr)
		layouts = append(layouts, l)
	}
	return layouts, nil
}

// UpdateLayout updates an existing layout's name and JSON definition
func (s *Service) UpdateLayout(ctx context.Context, id string, name string, definition json.RawMessage) (*LayoutMetadata, error) {
	db := s.driver.DB()
	dialect := s.driver.Dialect()
	now := time.Now().UTC()

	defStr := string(definition)
	if defStr == "" || defStr == "null" {
		defStr = "{}"
	}

	query := `UPDATE sys_layouts SET name = $1, definition = $2, updated_at = $3 WHERE id = $4`
	if dialect.Engine() == dbal.EngineMariaDB {
		query = `UPDATE sys_layouts SET name = ?, definition = ?, updated_at = ? WHERE id = ?`
	}

	res, err := db.ExecContext(ctx, query, name, defStr, now, id)
	if err != nil {
		return nil, fmt.Errorf("failed updating layout %s: %w", id, err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, fmt.Errorf("layout %s not found", id)
	}

	return s.GetLayout(ctx, id)
}

// DeleteLayout removes a layout
func (s *Service) DeleteLayout(ctx context.Context, id string) error {
	db := s.driver.DB()
	query := `DELETE FROM sys_layouts WHERE id = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		query = `DELETE FROM sys_layouts WHERE id = ?`
	}
	_, err := db.ExecContext(ctx, query, id)
	return err
}
