package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrImportStepNotFound is returned for a step that is not a migrated Import
// Records step still waiting for its transfer.
var ErrImportStepNotFound = errors.New("no Import Records step waits for a transfer with that id")

// AwaitingImportStep is an Import Records step migrated from FileMaker that
// imports from another FileMaker file and waits, disabled, for the transfer
// that runs it (#191). Params are the step's own, as the migration saved them.
type AwaitingImportStep struct {
	StepID     string
	ScriptID   string
	ScriptName string
	// StepIndex is the step's 1-based place in its script.
	StepIndex int
	Params    map[string]interface{}
}

// ListAwaitingImportSteps reads the Import Records steps that wait for a
// transfer, by script and place.
func (s *Service) ListAwaitingImportSteps(ctx context.Context) ([]AwaitingImportStep, error) {
	q := fmt.Sprintf(`SELECT st.id, st.script_id, sc.name, st.sequence_idx, st.params
		FROM sys_script_steps st JOIN sys_scripts sc ON sc.id = st.script_id
		WHERE st.step_type = %s ORDER BY sc.name, st.sequence_idx`, s.transferPlaceholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, "Import Records")
	if err != nil {
		return nil, fmt.Errorf("failed reading the Import Records steps: %w", err)
	}
	defer rows.Close()
	steps := []AwaitingImportStep{}
	for rows.Next() {
		var st AwaitingImportStep
		var raw sql.NullString
		if err := rows.Scan(&st.StepID, &st.ScriptID, &st.ScriptName, &st.StepIndex, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw.String), &st.Params); err != nil || st.Params["awaiting_transfer"] != true {
			continue
		}
		steps = append(steps, st)
	}
	return steps, rows.Err()
}

// AwaitingImportStep reads one step that waits for a transfer.
func (s *Service) AwaitingImportStep(ctx context.Context, stepID string) (*AwaitingImportStep, error) {
	steps, err := s.ListAwaitingImportSteps(ctx)
	if err != nil {
		return nil, err
	}
	for i := range steps {
		if steps[i].StepID == stepID {
			return &steps[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrImportStepNotFound, stepID)
}

// CreateImportStepTransfer creates the transfer an Import Records step waits
// for and names it in the step, in one transaction (#191): the step runs the
// transfer from then on, is enabled again if FileMaker had it enabled, and no
// longer carries the migration note that said it waited. When the name is
// taken, a number is added to it. createdBy is the account confirming it.
func (s *Service) CreateImportStepTransfer(
	ctx context.Context, stepID string, in TransferInput, createdBy string,
) (*TransferDefinition, error) {
	in, err := checkTransfer(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var scriptID string
	var raw sql.NullString
	q := fmt.Sprintf(`SELECT script_id, params FROM sys_script_steps WHERE id = %s AND step_type = %s`,
		s.transferPlaceholder(1), s.transferPlaceholder(2))
	if err := tx.QueryRowContext(ctx, q, stepID, "Import Records").Scan(&scriptID, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrImportStepNotFound, stepID)
		}
		return nil, err
	}
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(raw.String), &params); err != nil || params["awaiting_transfer"] != true {
		return nil, fmt.Errorf("%w: %s", ErrImportStepNotFound, stepID)
	}

	base := in.Name
	for n := 2; ; n++ {
		var count int
		cq := fmt.Sprintf(`SELECT COUNT(*) FROM sys_transfers WHERE LOWER(name) = LOWER(%s)`, s.transferPlaceholder(1))
		if err := tx.QueryRowContext(ctx, cq, in.Name).Scan(&count); err != nil {
			return nil, fmt.Errorf("failed checking the name %q: %w", in.Name, err)
		}
		if count == 0 {
			break
		}
		if n > 100 {
			return nil, fmt.Errorf("%w: %s", ErrTransferExists, base)
		}
		suffix := fmt.Sprintf(" (%d)", n)
		trimmed := []rune(base)
		if len(trimmed)+len(suffix) > maxTransferName {
			trimmed = trimmed[:maxTransferName-len(suffix)]
		}
		in.Name = string(trimmed) + suffix
	}

	now := time.Now().UTC()
	def := &TransferDefinition{
		ID: uuid.NewString(), Name: in.Name, Source: in.Source, Destination: in.Destination,
		FoundSet: in.FoundSet, Sort: in.Sort, Mappings: in.Mappings, MatchFields: in.MatchFields,
		Mode: in.Mode, OnUnmatched: in.OnUnmatched, OnRejected: in.OnRejected, DateOrder: in.DateOrder,
		PerformAutoEnter: in.PerformAutoEnter, AutoEnterFields: in.AutoEnterFields,
		CreatedBy: createdBy, CreatedAt: now, UpdatedAt: now,
	}
	encoded, err := encodeTransferPayload(def)
	if err != nil {
		return nil, err
	}
	var owner interface{}
	if strings.TrimSpace(createdBy) != "" {
		owner = createdBy
	}
	iq := fmt.Sprintf(`INSERT INTO sys_transfers
	    (id, name, source_database, source_table, destination_database, destination_table,
	     definition, created_by, created_at, updated_at)
	    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		s.transferPlaceholder(1), s.transferPlaceholder(2), s.transferPlaceholder(3),
		s.transferPlaceholder(4), s.transferPlaceholder(5), s.transferPlaceholder(6),
		s.transferPlaceholder(7), s.transferPlaceholder(8), s.transferPlaceholder(9),
		s.transferPlaceholder(10))
	if _, err := tx.ExecContext(ctx, iq, def.ID, def.Name, def.Source.Database, def.Source.Table,
		def.Destination.Database, def.Destination.Table, encoded, owner, def.CreatedAt, def.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed saving the transfer %q: %w", def.Name, err)
	}

	enabled, _ := params["filemaker_step_enabled"].(bool)
	params["transfer"] = def.Name
	delete(params, "awaiting_transfer")
	delete(params, "migration_note")
	delete(params, "filemaker_step_enabled")
	updated, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	uq := fmt.Sprintf(`UPDATE sys_script_steps SET params = %s, is_enabled = %s WHERE id = %s`,
		s.transferPlaceholder(1), s.transferPlaceholder(2), s.transferPlaceholder(3))
	if _, err := tx.ExecContext(ctx, uq, string(updated), enabled, stepID); err != nil {
		return nil, fmt.Errorf("failed naming the transfer in the step: %w", err)
	}
	sq := fmt.Sprintf(`UPDATE sys_scripts SET updated_at = %s WHERE id = %s`,
		s.transferPlaceholder(1), s.transferPlaceholder(2))
	if _, err := tx.ExecContext(ctx, sq, now, scriptID); err != nil {
		return nil, fmt.Errorf("failed naming the transfer in the step: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed saving the transfer %q: %w", def.Name, err)
	}
	return def, nil
}
