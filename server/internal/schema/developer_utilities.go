package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
)

// ErrDesignLocked is returned when a caller tries to alter the schema, layouts,
// scripts, value lists, or menu sets of a solution whose design has been
// permanently locked (#53).
var ErrDesignLocked = errors.New("the design of this solution has been locked and cannot be modified")

// ErrInvalidSolutionName is returned when a solution name is empty or too long.
var ErrInvalidSolutionName = errors.New("invalid solution name")

const (
	solutionMetaRowID   = "default"
	maxSolutionNameLen  = 128
)

// SolutionMeta holds the solution's name and whether its design is locked (#53).
type SolutionMeta struct {
	SolutionName string    `json:"solution_name"`
	DesignLocked bool      `json:"design_locked"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (s *Service) metaPlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// GetSolutionMeta returns the solution's name and design lock state. A database
// that has no row in sys_solution_meta yet is unlocked and defaults its name to
// fallbackName.
func (s *Service) GetSolutionMeta(ctx context.Context, fallbackName string) (SolutionMeta, error) {
	q := fmt.Sprintf(
		`SELECT solution_name, design_locked, updated_at FROM sys_solution_meta WHERE id = %s`,
		s.metaPlaceholder(1),
	)
	var meta SolutionMeta
	err := s.driver.DB().QueryRowContext(ctx, q, solutionMetaRowID).Scan(
		&meta.SolutionName, &meta.DesignLocked, &meta.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SolutionMeta{SolutionName: fallbackName}, nil
	}
	if err != nil {
		// If sys_solution_meta does not exist yet on an older database,
		// ensure system tables and treat as unlocked.
		if ensureErr := s.EnsureSystemTables(ctx); ensureErr == nil {
			if retryErr := s.driver.DB().QueryRowContext(ctx, q, solutionMetaRowID).Scan(
				&meta.SolutionName, &meta.DesignLocked, &meta.UpdatedAt,
			); errors.Is(retryErr, sql.ErrNoRows) {
				return SolutionMeta{SolutionName: fallbackName}, nil
			} else if retryErr == nil {
				if meta.SolutionName == "" {
					meta.SolutionName = fallbackName
				}
				return meta, nil
			}
		}
		return SolutionMeta{SolutionName: fallbackName}, nil
	}
	if meta.SolutionName == "" {
		meta.SolutionName = fallbackName
	}
	return meta, nil
}

// IsDesignLocked reports whether the solution's design has been permanently
// locked (#53).
func (s *Service) IsDesignLocked(ctx context.Context) (bool, error) {
	meta, err := s.GetSolutionMeta(ctx, "")
	if err != nil {
		return false, err
	}
	return meta.DesignLocked, nil
}

// LockDesign irreversibly marks the solution's design as locked (#53). Once
// locked, the server refuses every schema, layout, script, value list, and
// menu set mutation on this database.
func (s *Service) LockDesign(ctx context.Context, fallbackName string) (SolutionMeta, error) {
	if err := s.EnsureSystemTables(ctx); err != nil {
		return SolutionMeta{}, err
	}
	current, err := s.GetSolutionMeta(ctx, fallbackName)
	if err != nil {
		return SolutionMeta{}, err
	}
	now := time.Now().UTC()
	if err := s.upsertSolutionMeta(ctx, current.SolutionName, true, now); err != nil {
		return SolutionMeta{}, err
	}
	return SolutionMeta{
		SolutionName: current.SolutionName,
		DesignLocked: true,
		UpdatedAt:    now,
	}, nil
}

// RenameSolution updates the solution's name and rewrites any layout or script
// step references that named the previous solution name (#53).
func (s *Service) RenameSolution(ctx context.Context, fallbackOldName, newName string) (SolutionMeta, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return SolutionMeta{}, fmt.Errorf("%w: solution name cannot be empty", ErrInvalidSolutionName)
	}
	if len([]rune(newName)) > maxSolutionNameLen {
		return SolutionMeta{}, fmt.Errorf("%w: solution name is at most %d characters", ErrInvalidSolutionName, maxSolutionNameLen)
	}
	if err := s.EnsureSystemTables(ctx); err != nil {
		return SolutionMeta{}, err
	}
	current, err := s.GetSolutionMeta(ctx, fallbackOldName)
	if err != nil {
		return SolutionMeta{}, err
	}
	if current.DesignLocked {
		return SolutionMeta{}, ErrDesignLocked
	}
	oldName := strings.TrimSpace(current.SolutionName)
	now := time.Now().UTC()
	if err := s.upsertSolutionMeta(ctx, newName, current.DesignLocked, now); err != nil {
		return SolutionMeta{}, err
	}

	if oldName != "" && !strings.EqualFold(oldName, newName) {
		if err := s.rewriteSolutionNameRefs(ctx, oldName, newName); err != nil {
			return SolutionMeta{}, err
		}
	}

	return SolutionMeta{
		SolutionName: newName,
		DesignLocked: current.DesignLocked,
		UpdatedAt:    now,
	}, nil
}

func (s *Service) upsertSolutionMeta(ctx context.Context, name string, locked bool, now time.Time) error {
	updateQ := fmt.Sprintf(
		`UPDATE sys_solution_meta SET solution_name = %s, design_locked = %s, updated_at = %s WHERE id = %s`,
		s.metaPlaceholder(1), s.metaPlaceholder(2), s.metaPlaceholder(3), s.metaPlaceholder(4),
	)
	res, err := s.driver.DB().ExecContext(ctx, updateQ, name, locked, now, solutionMetaRowID)
	if err != nil {
		return fmt.Errorf("failed updating solution metadata: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		return nil
	}
	insertQ := fmt.Sprintf(
		`INSERT INTO sys_solution_meta (id, solution_name, design_locked, updated_at) VALUES (%s, %s, %s, %s)`,
		s.metaPlaceholder(1), s.metaPlaceholder(2), s.metaPlaceholder(3), s.metaPlaceholder(4),
	)
	_, err = s.driver.DB().ExecContext(ctx, insertQ, solutionMetaRowID, name, locked, now)
	return err
}

func (s *Service) rewriteSolutionNameRefs(ctx context.Context, oldName, newName string) error {
	layouts, err := s.ListLayouts(ctx)
	if err != nil {
		return err
	}
	for _, l := range layouts {
		def, err := jsonObject(l.Definition)
		if err != nil {
			continue
		}
		if cur, ok := def["solution_name"].(string); ok && strings.EqualFold(strings.TrimSpace(cur), oldName) {
			def["solution_name"] = newName
			raw, err := json.Marshal(def)
			if err == nil {
				_, _ = s.UpdateLayout(ctx, l.ID, l.Name, raw)
			}
		}
	}

	scripts, err := s.ListScripts(ctx)
	if err != nil {
		return err
	}
	for _, sc := range scripts {
		changed := false
		for i := range sc.Steps {
			params, err := jsonObject(sc.Steps[i].Params)
			if err != nil {
				continue
			}
			if cur, ok := params["solution_name"].(string); ok && strings.EqualFold(strings.TrimSpace(cur), oldName) {
				params["solution_name"] = newName
				if raw, err := json.Marshal(params); err == nil {
					sc.Steps[i].Params = raw
					changed = true
				}
			}
		}
		if changed {
			_, _ = s.UpdateScript(ctx, sc.ID, sc.Name, sc.ContextTable, sc.FolderID, sc.IsActive, sc.Steps)
		}
	}
	return nil
}

// CopyOwnerAccount copies an existing owner account (including its password
// hash) into a newly created target database so that a cloned database can be
// signed into immediately by the owner who cloned it (#53).
func (s *Service) CopyOwnerAccount(ctx context.Context, dst *Service, userID string) error {
	q := fmt.Sprintf(
		`SELECT id, username, password_hash, role, is_active, created_at, updated_at FROM sys_users WHERE id = %s`,
		s.metaPlaceholder(1),
	)
	var id, username, hash, role string
	var active bool
	var createdAt, updatedAt time.Time
	if err := s.driver.DB().QueryRowContext(ctx, q, userID).Scan(
		&id, &username, &hash, &role, &active, &createdAt, &updatedAt,
	); err != nil {
		return fmt.Errorf("failed reading source owner account: %w", err)
	}
	insertQ := fmt.Sprintf(
		`INSERT INTO sys_users (id, username, password_hash, role, is_active, created_at, updated_at)
		 VALUES (%s, %s, %s, %s, %s, %s, %s)`,
		dst.metaPlaceholder(1), dst.metaPlaceholder(2), dst.metaPlaceholder(3),
		dst.metaPlaceholder(4), dst.metaPlaceholder(5), dst.metaPlaceholder(6), dst.metaPlaceholder(7),
	)
	_, err := dst.driver.DB().ExecContext(ctx, insertQ, id, username, hash, role, active, createdAt, updatedAt)
	return err
}
