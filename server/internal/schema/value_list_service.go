package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// ErrValueListNotFound is returned when no value list has the given id.
var ErrValueListNotFound = errors.New("value list not found")

// ErrValueListExists is returned when another list already has that name.
var ErrValueListExists = errors.New("a value list with that name already exists")

// The two kinds of value list.
const (
	// ValueListCustom is a fixed list the designer typed.
	ValueListCustom = "custom"
	// ValueListFromField offers the values a field already holds, so the list
	// grows with the data.
	ValueListFromField = "field"
)

// ValueList is a named set of values a field can be filled from.
//
// A value list only decides what a field *offers*. It does not restrict what
// can be stored: that is the "existing value" validation rule, so the two
// choices stay separate as they do in FileMaker.
type ValueList struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`

	// CustomValues is one value per line, for a custom list.
	CustomValues string `json:"custom_values"`

	// SourceTableID and SourceColumnID say which field a "field" list reads.
	SourceTableID  *string `json:"source_table_id,omitempty"`
	SourceColumnID *string `json:"source_column_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValueListInput is what a caller supplies to create or update a list.
type ValueListInput struct {
	Name           string  `json:"name"`
	Kind           string  `json:"kind"`
	CustomValues   string  `json:"custom_values"`
	SourceTableID  *string `json:"source_table_id"`
	SourceColumnID *string `json:"source_column_id"`
}

func (s *Service) valueListPlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// normalizeValueListInput checks a list before it is written.
func normalizeValueListInput(in ValueListInput) (ValueListInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("%w: a value list needs a name", ErrInvalidFieldOptions)
	}
	if len([]rune(in.Name)) > 128 {
		return in, fmt.Errorf("%w: the name of a value list is at most 128 characters", ErrInvalidFieldOptions)
	}

	switch strings.ToLower(strings.TrimSpace(in.Kind)) {
	case "", ValueListCustom:
		in.Kind = ValueListCustom
		in.SourceTableID, in.SourceColumnID = nil, nil
		if strings.TrimSpace(in.CustomValues) == "" {
			return in, fmt.Errorf("%w: a custom value list needs at least one value", ErrInvalidFieldOptions)
		}
	case ValueListFromField:
		in.Kind = ValueListFromField
		in.CustomValues = ""
		if in.SourceTableID == nil || strings.TrimSpace(*in.SourceTableID) == "" ||
			in.SourceColumnID == nil || strings.TrimSpace(*in.SourceColumnID) == "" {
			return in, fmt.Errorf("%w: a value list taken from a field needs the table and the field to read", ErrInvalidFieldOptions)
		}
	default:
		return in, fmt.Errorf("%w: a value list is either %q or %q", ErrInvalidFieldOptions, ValueListCustom, ValueListFromField)
	}
	return in, nil
}

// CreateValueList registers a new value list.
func (s *Service) CreateValueList(ctx context.Context, in ValueListInput) (*ValueList, error) {
	in, err := normalizeValueListInput(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.valueListNameTaken(ctx, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrValueListExists, in.Name)
	}

	now := time.Now().UTC()
	list := &ValueList{
		ID:             uuid.NewString(),
		Name:           in.Name,
		Kind:           in.Kind,
		CustomValues:   in.CustomValues,
		SourceTableID:  in.SourceTableID,
		SourceColumnID: in.SourceColumnID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	q := fmt.Sprintf(`INSERT INTO sys_value_lists (id, name, kind, custom_values, source_table_id, source_column_id, created_at, updated_at)
	                  VALUES (%s, %s, %s, %s, %s, %s, %s, %s)`,
		s.valueListPlaceholder(1), s.valueListPlaceholder(2), s.valueListPlaceholder(3), s.valueListPlaceholder(4),
		s.valueListPlaceholder(5), s.valueListPlaceholder(6), s.valueListPlaceholder(7), s.valueListPlaceholder(8))
	if _, err := s.driver.DB().ExecContext(ctx, q, list.ID, list.Name, list.Kind, list.CustomValues,
		list.SourceTableID, list.SourceColumnID, list.CreatedAt, list.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed creating value list %q: %w", list.Name, err)
	}
	return list, nil
}

// UpdateValueList replaces a value list's definition.
func (s *Service) UpdateValueList(ctx context.Context, id string, in ValueListInput) (*ValueList, error) {
	in, err := normalizeValueListInput(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.valueListNameTaken(ctx, in.Name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrValueListExists, in.Name)
	}

	now := time.Now().UTC()
	q := fmt.Sprintf(`UPDATE sys_value_lists SET name = %s, kind = %s, custom_values = %s,
	                  source_table_id = %s, source_column_id = %s, updated_at = %s WHERE id = %s`,
		s.valueListPlaceholder(1), s.valueListPlaceholder(2), s.valueListPlaceholder(3), s.valueListPlaceholder(4),
		s.valueListPlaceholder(5), s.valueListPlaceholder(6), s.valueListPlaceholder(7))
	res, err := s.driver.DB().ExecContext(ctx, q, in.Name, in.Kind, in.CustomValues,
		in.SourceTableID, in.SourceColumnID, now, id)
	if err != nil {
		return nil, fmt.Errorf("failed updating value list %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, fmt.Errorf("%w: %s", ErrValueListNotFound, id)
	}
	return s.GetValueList(ctx, id)
}

// DeleteValueList removes a value list. Layout fields that pointed at it fall
// back to a plain edit box, so nothing breaks.
func (s *Service) DeleteValueList(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_value_lists WHERE id = %s`, s.valueListPlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting value list %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrValueListNotFound, id)
	}
	return nil
}

// GetValueList reads one list.
func (s *Service) GetValueList(ctx context.Context, id string) (*ValueList, error) {
	q := fmt.Sprintf(`SELECT id, name, kind, custom_values, source_table_id, source_column_id, created_at, updated_at
	                  FROM sys_value_lists WHERE id = %s`, s.valueListPlaceholder(1))
	var list ValueList
	var custom sql.NullString
	err := s.driver.DB().QueryRowContext(ctx, q, id).Scan(&list.ID, &list.Name, &list.Kind, &custom,
		&list.SourceTableID, &list.SourceColumnID, &list.CreatedAt, &list.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrValueListNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	list.CustomValues = custom.String
	return &list, nil
}

// ListValueLists reads every value list, by name.
func (s *Service) ListValueLists(ctx context.Context) ([]ValueList, error) {
	q := `SELECT id, name, kind, custom_values, source_table_id, source_column_id, created_at, updated_at
	      FROM sys_value_lists ORDER BY name ASC`
	rows, err := s.driver.DB().QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed reading the value lists: %w", err)
	}
	defer rows.Close()

	lists := []ValueList{}
	for rows.Next() {
		var list ValueList
		var custom sql.NullString
		if err := rows.Scan(&list.ID, &list.Name, &list.Kind, &custom, &list.SourceTableID,
			&list.SourceColumnID, &list.CreatedAt, &list.UpdatedAt); err != nil {
			return nil, err
		}
		list.CustomValues = custom.String
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

// ValueListValues resolves a list to the values it offers right now: the lines
// of a custom list, or the distinct values a field holds.
func (s *Service) ValueListValues(ctx context.Context, id string) ([]string, error) {
	list, err := s.GetValueList(ctx, id)
	if err != nil {
		return nil, err
	}
	if list.Kind == ValueListCustom {
		return SplitCustomValues(list.CustomValues), nil
	}
	return s.distinctFieldValues(ctx, *list.SourceTableID, *list.SourceColumnID)
}

// SplitCustomValues turns the stored text into values: one per line, blank
// lines dropped, each value appearing once.
func SplitCustomValues(raw string) []string {
	seen := map[string]struct{}{}
	values := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		if _, repeated := seen[value]; repeated {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

// maxDynamicValues bounds a list taken from a field, so a column with many
// distinct values cannot make an unusable control or a huge response.
const maxDynamicValues = 500

// distinctFieldValues reads the values a field already holds, sorted, without
// blanks or repeats.
func (s *Service) distinctFieldValues(ctx context.Context, tableID, columnID string) ([]string, error) {
	var tableName, columnName string
	q := fmt.Sprintf(`SELECT t.name, c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
	                  WHERE c.id = %s AND t.id = %s`, s.valueListPlaceholder(1), s.valueListPlaceholder(2))
	if err := s.driver.DB().QueryRowContext(ctx, q, columnID, tableID).Scan(&tableName, &columnName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: the field this value list reads no longer exists", ErrValueListNotFound)
		}
		return nil, err
	}
	if dbal.IsReservedTableName(tableName) {
		return nil, fmt.Errorf("%w: internal tables cannot feed a value list", ErrInvalidFieldOptions)
	}

	dialect := s.driver.Dialect()
	column := dialect.QuoteIdentifier(columnName)
	sqlText := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL LIMIT %d",
		column, dialect.QuoteIdentifier(tableName), column, maxDynamicValues)

	rows, err := s.driver.DB().QueryContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("failed reading the values of %s.%s: %w", tableName, columnName, err)
	}
	defer rows.Close()

	values := []string{}
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		value := strings.TrimSpace(raw.String)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Strings(values)
	return values, nil
}

func (s *Service) valueListNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT id FROM sys_value_lists WHERE LOWER(name) = LOWER(%s)`, s.valueListPlaceholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, name)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return false, err
		}
		if id != exceptID {
			return true, nil
		}
	}
	return false, rows.Err()
}
