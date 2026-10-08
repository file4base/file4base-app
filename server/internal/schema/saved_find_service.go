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
	"github.com/google/uuid"
)

// ErrSavedFindNotFound is returned when no saved find has the given id.
var ErrSavedFindNotFound = errors.New("saved find not found")

// ErrSavedFindExists is returned when the table already has a find with that name.
var ErrSavedFindExists = errors.New("a saved find with that name already exists for this table")

// ErrInvalidSavedFind is returned for a find that cannot be stored as given.
var ErrInvalidSavedFind = errors.New("invalid saved find")

// Bounds on a saved find, so a stored find stays something a person wrote
// rather than a payload.
const (
	maxSavedFindName     = 128
	maxSavedFindRequests = 50
	maxSavedFindValue    = 1000
)

// SavedFindRequest is one request of a saved find: what was typed into each
// field, and whether the request omits what it matches.
//
// The criteria are kept **as typed** (">100", "Paris", "=", "a...b"), not as
// the operators they parse into, for the same reason FileMaker keeps them: a
// saved find can be opened in Find mode, read and changed, and it is parsed
// again every time it runs.
type SavedFindRequest struct {
	Values map[string]string `json:"values" msgpack:"values"`
	Omit   bool              `json:"omit" msgpack:"omit"`
}

// SavedFind is a named set of find requests on one table.
type SavedFind struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	TableName string             `json:"table_name"`
	Requests  []SavedFindRequest `json:"requests"`

	// CreatedBy is the account that saved it. A find is shared with everyone
	// who can reach the table; the account decides who may change it.
	CreatedBy string `json:"created_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SavedFindInput is what a caller supplies to save or replace a find.
type SavedFindInput struct {
	Name      string             `json:"name"`
	TableName string             `json:"table_name"`
	Requests  []SavedFindRequest `json:"requests"`
}

func (s *Service) savedFindPlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// checkSavedFind validates a find against the catalog: the table must exist
// and every criterion must name one of its fields, so a stored find cannot
// fail with "unknown field" the first time someone runs it.
func (s *Service) checkSavedFind(ctx context.Context, in SavedFindInput) (SavedFindInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("%w: a saved find needs a name", ErrInvalidSavedFind)
	}
	if len([]rune(in.Name)) > maxSavedFindName {
		return in, fmt.Errorf("%w: the name of a saved find is at most %d characters", ErrInvalidSavedFind, maxSavedFindName)
	}

	in.TableName = dbal.NormalizeIdentifier(in.TableName)
	if in.TableName == "" {
		return in, fmt.Errorf("%w: a saved find needs the table it searches", ErrInvalidSavedFind)
	}
	fields, err := s.tableFieldNames(ctx, in.TableName)
	if err != nil {
		return in, err
	}

	if len(in.Requests) == 0 {
		return in, fmt.Errorf("%w: a saved find needs at least one request with criteria", ErrInvalidSavedFind)
	}
	if len(in.Requests) > maxSavedFindRequests {
		return in, fmt.Errorf("%w: a saved find holds at most %d requests", ErrInvalidSavedFind, maxSavedFindRequests)
	}

	clean := make([]SavedFindRequest, 0, len(in.Requests))
	for i, request := range in.Requests {
		values := make(map[string]string, len(request.Values))
		for field, value := range request.Values {
			field = dbal.NormalizeIdentifier(field)
			if _, ok := fields[field]; !ok {
				return in, fmt.Errorf("%w: request %d searches %q, which is not a field of %q",
					ErrInvalidSavedFind, i+1, field, in.TableName)
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if len([]rune(value)) > maxSavedFindValue {
				return in, fmt.Errorf("%w: the criterion on %q is longer than %d characters",
					ErrInvalidSavedFind, field, maxSavedFindValue)
			}
			values[field] = value
		}
		if len(values) == 0 {
			// An empty request would find everything, which is never what a
			// saved find means; it is dropped rather than stored.
			continue
		}
		clean = append(clean, SavedFindRequest{Values: values, Omit: request.Omit})
	}
	if len(clean) == 0 {
		return in, fmt.Errorf("%w: every request of this find is empty", ErrInvalidSavedFind)
	}
	in.Requests = clean
	return in, nil
}

// tableFieldNames returns the fields of a catalog table, as a set.
func (s *Service) tableFieldNames(ctx context.Context, tableName string) (map[string]struct{}, error) {
	q := fmt.Sprintf(`SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = %s`,
		s.savedFindPlaceholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed reading the fields of %q: %w", tableName, err)
	}
	defer rows.Close()

	fields := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		fields[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: %q is not a table of this database", ErrInvalidSavedFind, tableName)
	}
	// The id column is searchable too, and is not in sys_columns.
	fields["id"] = struct{}{}
	return fields, nil
}

// CreateSavedFind stores a new find. createdBy is the account saving it.
func (s *Service) CreateSavedFind(ctx context.Context, in SavedFindInput, createdBy string) (*SavedFind, error) {
	in, err := s.checkSavedFind(ctx, in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.savedFindNameTaken(ctx, in.TableName, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrSavedFindExists, in.Name)
	}

	encoded, err := json.Marshal(in.Requests)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	find := &SavedFind{
		ID:        uuid.NewString(),
		Name:      in.Name,
		TableName: in.TableName,
		Requests:  in.Requests,
		CreatedBy: createdBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	q := fmt.Sprintf(`INSERT INTO sys_saved_finds (id, name, table_name, requests, created_by, created_at, updated_at)
	                  VALUES (%s, %s, %s, %s, %s, %s, %s)`,
		s.savedFindPlaceholder(1), s.savedFindPlaceholder(2), s.savedFindPlaceholder(3), s.savedFindPlaceholder(4),
		s.savedFindPlaceholder(5), s.savedFindPlaceholder(6), s.savedFindPlaceholder(7))
	var owner interface{}
	if strings.TrimSpace(createdBy) != "" {
		owner = createdBy
	}
	if _, err := s.driver.DB().ExecContext(ctx, q, find.ID, find.Name, find.TableName, string(encoded),
		owner, find.CreatedAt, find.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed saving the find %q: %w", find.Name, err)
	}
	return find, nil
}

// UpdateSavedFind replaces a saved find's name and requests. The table it
// searches cannot change: that would be a different find.
func (s *Service) UpdateSavedFind(ctx context.Context, id string, in SavedFindInput) (*SavedFind, error) {
	current, err := s.GetSavedFind(ctx, id)
	if err != nil {
		return nil, err
	}
	in.TableName = current.TableName
	in, err = s.checkSavedFind(ctx, in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.savedFindNameTaken(ctx, in.TableName, in.Name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrSavedFindExists, in.Name)
	}

	encoded, err := json.Marshal(in.Requests)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`UPDATE sys_saved_finds SET name = %s, requests = %s, updated_at = %s WHERE id = %s`,
		s.savedFindPlaceholder(1), s.savedFindPlaceholder(2), s.savedFindPlaceholder(3), s.savedFindPlaceholder(4))
	if _, err := s.driver.DB().ExecContext(ctx, q, in.Name, string(encoded), time.Now().UTC(), id); err != nil {
		return nil, fmt.Errorf("failed updating the saved find %s: %w", id, err)
	}
	return s.GetSavedFind(ctx, id)
}

// DeleteSavedFind removes a saved find.
func (s *Service) DeleteSavedFind(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_saved_finds WHERE id = %s`, s.savedFindPlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting the saved find %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrSavedFindNotFound, id)
	}
	return nil
}

// GetSavedFind reads one saved find.
func (s *Service) GetSavedFind(ctx context.Context, id string) (*SavedFind, error) {
	q := fmt.Sprintf(`SELECT id, name, table_name, requests, created_by, created_at, updated_at
	                  FROM sys_saved_finds WHERE id = %s`, s.savedFindPlaceholder(1))
	find, err := s.scanSavedFind(s.driver.DB().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrSavedFindNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return find, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func (s *Service) scanSavedFind(row rowScanner) (*SavedFind, error) {
	var find SavedFind
	var encoded string
	var createdBy sql.NullString
	if err := row.Scan(&find.ID, &find.Name, &find.TableName, &encoded, &createdBy,
		&find.CreatedAt, &find.UpdatedAt); err != nil {
		return nil, err
	}
	find.CreatedBy = createdBy.String
	if err := json.Unmarshal([]byte(encoded), &find.Requests); err != nil {
		return nil, fmt.Errorf("the saved find %q cannot be read: %w", find.Name, err)
	}
	return &find, nil
}

// ListSavedFinds reads the saved finds, by name. An empty tableName reads
// every table's.
func (s *Service) ListSavedFinds(ctx context.Context, tableName string) ([]SavedFind, error) {
	q := `SELECT id, name, table_name, requests, created_by, created_at, updated_at
	      FROM sys_saved_finds ORDER BY table_name ASC, name ASC`
	args := []interface{}{}
	if tableName = dbal.NormalizeIdentifier(tableName); tableName != "" {
		q = fmt.Sprintf(`SELECT id, name, table_name, requests, created_by, created_at, updated_at
		      FROM sys_saved_finds WHERE table_name = %s ORDER BY name ASC`, s.savedFindPlaceholder(1))
		args = append(args, tableName)
	}

	rows, err := s.driver.DB().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed reading the saved finds: %w", err)
	}
	defer rows.Close()

	finds := []SavedFind{}
	for rows.Next() {
		find, err := s.scanSavedFind(rows)
		if err != nil {
			return nil, err
		}
		finds = append(finds, *find)
	}
	return finds, rows.Err()
}

func (s *Service) savedFindNameTaken(ctx context.Context, tableName, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_saved_finds WHERE table_name = %s AND LOWER(name) = LOWER(%s) AND id <> %s`,
		s.savedFindPlaceholder(1), s.savedFindPlaceholder(2), s.savedFindPlaceholder(3))
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, q, tableName, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed checking the name %q: %w", name, err)
	}
	return count > 0, nil
}
