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

// ScriptStepMetadata represents an individual step in a script sequence
type ScriptStepMetadata struct {
	ID           string          `json:"id"`
	ScriptID     string          `json:"script_id"`
	SequenceIdx  int             `json:"sequence_idx"`
	StepType     string          `json:"step_type"`
	Params       json.RawMessage `json:"params"`
	IsEnabled    bool            `json:"is_enabled"`
	ParentStepID *string         `json:"parent_step_id,omitempty"`
}

// ScriptMetadata represents a complete script definition in sys_scripts
type ScriptMetadata struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	ContextTable string               `json:"context_table"`
	FolderID     *string              `json:"folder_id,omitempty"`
	IsActive     bool                 `json:"is_active"`
	Steps        []ScriptStepMetadata `json:"steps"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// ListScripts returns all scripts registered in the active database
func (s *Service) ListScripts(ctx context.Context) ([]ScriptMetadata, error) {
	db := s.driver.DB()
	rows, err := db.QueryContext(ctx, `SELECT id, name, context_table, folder_id, is_active, created_at, updated_at FROM sys_scripts ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed querying scripts: %w", err)
	}
	defer rows.Close()

	scripts := make([]ScriptMetadata, 0)
	for rows.Next() {
		var scr ScriptMetadata
		var folderID sql.NullString
		if err := rows.Scan(&scr.ID, &scr.Name, &scr.ContextTable, &folderID, &scr.IsActive, &scr.CreatedAt, &scr.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed scanning script row: %w", err)
		}
		if folderID.Valid {
			scr.FolderID = &folderID.String
		}
		scr.Steps = make([]ScriptStepMetadata, 0)
		scripts = append(scripts, scr)
	}

	// Fetch all steps in a single query
	stepRows, err := db.QueryContext(ctx, `SELECT id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id FROM sys_script_steps ORDER BY script_id, sequence_idx ASC`)
	if err == nil {
		defer stepRows.Close()
		stepsMap := make(map[string][]ScriptStepMetadata)
		for stepRows.Next() {
			var st ScriptStepMetadata
			var paramsRaw string
			var parentStepID sql.NullString
			if err := stepRows.Scan(&st.ID, &st.ScriptID, &st.SequenceIdx, &st.StepType, &paramsRaw, &st.IsEnabled, &parentStepID); err == nil {
				st.Params = json.RawMessage(paramsRaw)
				if parentStepID.Valid {
					st.ParentStepID = &parentStepID.String
				}
				stepsMap[st.ScriptID] = append(stepsMap[st.ScriptID], st)
			}
		}
		for i := range scripts {
			if stList, ok := stepsMap[scripts[i].ID]; ok {
				scripts[i].Steps = stList
			}
		}
	}

	return scripts, nil
}

// GetScript returns a single script with its complete step sequence
func (s *Service) GetScript(ctx context.Context, id string) (*ScriptMetadata, error) {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	q := `SELECT id, name, context_table, folder_id, is_active, created_at, updated_at FROM sys_scripts WHERE id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `SELECT id, name, context_table, folder_id, is_active, created_at, updated_at FROM sys_scripts WHERE id = ?`
	}

	var scr ScriptMetadata
	var folderID sql.NullString
	err := db.QueryRowContext(ctx, q, id).Scan(&scr.ID, &scr.Name, &scr.ContextTable, &folderID, &scr.IsActive, &scr.CreatedAt, &scr.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("script not found")
		}
		return nil, fmt.Errorf("failed querying script: %w", err)
	}
	if folderID.Valid {
		scr.FolderID = &folderID.String
	}

	// Fetch steps
	sq := `SELECT id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id FROM sys_script_steps WHERE script_id = $1 ORDER BY sequence_idx ASC`
	if dialect.Engine() != dbal.EnginePostgres {
		sq = `SELECT id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id FROM sys_script_steps WHERE script_id = ? ORDER BY sequence_idx ASC`
	}

	rows, err := db.QueryContext(ctx, sq, id)
	if err != nil {
		return nil, fmt.Errorf("failed querying script steps: %w", err)
	}
	defer rows.Close()

	scr.Steps = make([]ScriptStepMetadata, 0)
	for rows.Next() {
		var st ScriptStepMetadata
		var paramsRaw string
		var parentStepID sql.NullString
		if err := rows.Scan(&st.ID, &st.ScriptID, &st.SequenceIdx, &st.StepType, &paramsRaw, &st.IsEnabled, &parentStepID); err != nil {
			return nil, fmt.Errorf("failed scanning script step: %w", err)
		}
		st.Params = json.RawMessage(paramsRaw)
		if parentStepID.Valid {
			st.ParentStepID = &parentStepID.String
		}
		scr.Steps = append(scr.Steps, st)
	}

	return &scr, nil
}

// CreateScript creates and stores a new script definition with steps
func (s *Service) CreateScript(ctx context.Context, name, contextTable string, folderID *string, isActive bool, steps []ScriptStepMetadata) (*ScriptMetadata, error) {
	name = fmt.Sprint(name)
	if name == "" {
		return nil, errors.New("script name cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	id := uuid.NewString()
	now := time.Now().UTC()

	var fID sql.NullString
	if folderID != nil && *folderID != "" {
		fID = sql.NullString{String: *folderID, Valid: true}
	}

	q := `INSERT INTO sys_scripts (id, name, context_table, folder_id, is_active, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `INSERT INTO sys_scripts (id, name, context_table, folder_id, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`
	}

	_, err := db.ExecContext(ctx, q, id, name, contextTable, fID, isActive, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed inserting script: %w", err)
	}

	// Insert steps
	insertedSteps := make([]ScriptStepMetadata, 0, len(steps))
	for idx, st := range steps {
		stepID := st.ID
		if stepID == "" {
			stepID = uuid.NewString()
		}
		paramsStr := string(st.Params)
		if paramsStr == "" || paramsStr == "null" {
			paramsStr = "{}"
		}

		var pID sql.NullString
		if st.ParentStepID != nil && *st.ParentStepID != "" {
			pID = sql.NullString{String: *st.ParentStepID, Valid: true}
		}

		iq := `INSERT INTO sys_script_steps (id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		if dialect.Engine() != dbal.EnginePostgres {
			iq = `INSERT INTO sys_script_steps (id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		}

		seq := idx + 1
		if st.SequenceIdx > 0 {
			seq = st.SequenceIdx
		}

		_, err := db.ExecContext(ctx, iq, stepID, id, seq, st.StepType, paramsStr, st.IsEnabled, pID, now)
		if err != nil {
			return nil, fmt.Errorf("failed inserting script step: %w", err)
		}

		st.ID = stepID
		st.ScriptID = id
		st.SequenceIdx = seq
		st.Params = json.RawMessage(paramsStr)
		insertedSteps = append(insertedSteps, st)
	}

	return &ScriptMetadata{
		ID:           id,
		Name:         name,
		ContextTable: contextTable,
		FolderID:     folderID,
		IsActive:     isActive,
		Steps:        insertedSteps,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

// UpdateScript updates a script and replaces its steps sequence
func (s *Service) UpdateScript(ctx context.Context, id, name, contextTable string, folderID *string, isActive bool, steps []ScriptStepMetadata) (*ScriptMetadata, error) {
	if name == "" {
		return nil, errors.New("script name cannot be empty")
	}

	db := s.driver.DB()
	dialect := s.driver.Dialect()
	now := time.Now().UTC()

	var fID sql.NullString
	if folderID != nil && *folderID != "" {
		fID = sql.NullString{String: *folderID, Valid: true}
	}

	uq := `UPDATE sys_scripts SET name = $1, context_table = $2, folder_id = $3, is_active = $4, updated_at = $5 WHERE id = $6`
	if dialect.Engine() != dbal.EnginePostgres {
		uq = `UPDATE sys_scripts SET name = ?, context_table = ?, folder_id = ?, is_active = ?, updated_at = ? WHERE id = ?`
	}

	res, err := db.ExecContext(ctx, uq, name, contextTable, fID, isActive, now, id)
	if err != nil {
		return nil, fmt.Errorf("failed updating script: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, errors.New("script not found")
	}

	// Delete existing steps and insert new steps
	dq := `DELETE FROM sys_script_steps WHERE script_id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		dq = `DELETE FROM sys_script_steps WHERE script_id = ?`
	}
	if _, err := db.ExecContext(ctx, dq, id); err != nil {
		return nil, fmt.Errorf("failed clearing script steps: %w", err)
	}

	updatedSteps := make([]ScriptStepMetadata, 0, len(steps))
	for idx, st := range steps {
		stepID := st.ID
		if stepID == "" {
			stepID = uuid.NewString()
		}
		paramsStr := string(st.Params)
		if paramsStr == "" || paramsStr == "null" {
			paramsStr = "{}"
		}

		var pID sql.NullString
		if st.ParentStepID != nil && *st.ParentStepID != "" {
			pID = sql.NullString{String: *st.ParentStepID, Valid: true}
		}

		iq := `INSERT INTO sys_script_steps (id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
		if dialect.Engine() != dbal.EnginePostgres {
			iq = `INSERT INTO sys_script_steps (id, script_id, sequence_idx, step_type, params, is_enabled, parent_step_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
		}

		seq := idx + 1
		if st.SequenceIdx > 0 {
			seq = st.SequenceIdx
		}

		if _, err := db.ExecContext(ctx, iq, stepID, id, seq, st.StepType, paramsStr, st.IsEnabled, pID, now); err != nil {
			return nil, fmt.Errorf("failed inserting updated step: %w", err)
		}

		st.ID = stepID
		st.ScriptID = id
		st.SequenceIdx = seq
		st.Params = json.RawMessage(paramsStr)
		updatedSteps = append(updatedSteps, st)
	}

	return s.GetScript(ctx, id)
}

// DeleteScript removes a script and all its associated steps
func (s *Service) DeleteScript(ctx context.Context, id string) error {
	db := s.driver.DB()
	dialect := s.driver.Dialect()

	q := `DELETE FROM sys_scripts WHERE id = $1`
	if dialect.Engine() != dbal.EnginePostgres {
		q = `DELETE FROM sys_scripts WHERE id = ?`
	}

	res, err := db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting script: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return errors.New("script not found")
	}
	return nil
}

// DuplicateScript clones a script with all its steps and suffix _copia
func (s *Service) DuplicateScript(ctx context.Context, id string) (*ScriptMetadata, error) {
	orig, err := s.GetScript(ctx, id)
	if err != nil {
		return nil, err
	}

	newName := fmt.Sprintf("%s_copia", orig.Name)
	clonedSteps := make([]ScriptStepMetadata, len(orig.Steps))
	for i, st := range orig.Steps {
		clonedSteps[i] = ScriptStepMetadata{
			ID:          uuid.NewString(),
			SequenceIdx: st.SequenceIdx,
			StepType:    st.StepType,
			Params:      st.Params,
			IsEnabled:   st.IsEnabled,
		}
	}

	return s.CreateScript(ctx, newName, orig.ContextTable, orig.FolderID, orig.IsActive, clonedSteps)
}
