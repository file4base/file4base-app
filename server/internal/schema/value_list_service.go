package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
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

// The field a list taken from a field sorts its values by.
const (
	ValueListSortFirst  = "first"
	ValueListSortSecond = "second"
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

	// SecondColumnID is a second field of the same table, whose value is
	// shown beside the first one's, or instead of it with ShowSecondOnly.
	// Choosing an entry always stores the first field's value.
	SecondColumnID *string `json:"second_column_id,omitempty"`
	ShowSecondOnly bool    `json:"show_second_only,omitempty"`
	// SortBy is the field a "field" list is sorted by, first or second.
	SortBy string `json:"sort_by,omitempty"`

	// RelatedOccurrenceID, when set, includes only the values of the records
	// related to the current record of that table occurrence. They are read
	// through SourceOccurrenceID, the occurrence of the list's table the
	// relationship path leads to.
	SourceOccurrenceID  *string `json:"source_occurrence_id,omitempty"`
	RelatedOccurrenceID *string `json:"related_occurrence_id,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ValueListInput is what a caller supplies to create or update a list.
type ValueListInput struct {
	Name                string  `json:"name"`
	Kind                string  `json:"kind"`
	CustomValues        string  `json:"custom_values"`
	SourceTableID       *string `json:"source_table_id"`
	SourceColumnID      *string `json:"source_column_id"`
	SecondColumnID      *string `json:"second_column_id"`
	ShowSecondOnly      bool    `json:"show_second_only"`
	SortBy              string  `json:"sort_by"`
	SourceOccurrenceID  *string `json:"source_occurrence_id"`
	RelatedOccurrenceID *string `json:"related_occurrence_id"`
}

// valueListOptionsSQL adds the options of a list taken from a field to
// sys_value_lists. PostgreSQL skips a column that exists together with its
// foreign key; MariaDB would add the key again each time, so there the key is
// named and added only when it is missing.
//
// A deleted second field leaves the list with its first one; a deleted
// occurrence takes the list with it, like a deleted source field does, rather
// than leave a list of related values offering every value.
func valueListOptionsSQL(engine dbal.EngineType) []string {
	reference := func(column, target, onDelete string) string {
		if engine == dbal.EngineMariaDB {
			return fmt.Sprintf(`ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS %[1]s VARCHAR(36),
				ADD CONSTRAINT fk_value_list_%[1]s FOREIGN KEY IF NOT EXISTS (%[1]s) REFERENCES %[2]s(id) ON DELETE %[3]s;`,
				column, target, onDelete)
		}
		return fmt.Sprintf(`ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS %s VARCHAR(36) REFERENCES %s(id) ON DELETE %s;`,
			column, target, onDelete)
	}
	return []string{
		reference("second_column_id", "sys_columns", "SET NULL"),
		`ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS show_second_only BOOLEAN;`,
		`ALTER TABLE sys_value_lists ADD COLUMN IF NOT EXISTS sort_by VARCHAR(16);`,
		reference("source_occurrence_id", "sys_table_occurrences", "CASCADE"),
		reference("related_occurrence_id", "sys_table_occurrences", "CASCADE"),
	}
}

// ensureValueListOptions gives a database created before a value list had
// field options their columns.
func (s *Service) ensureValueListOptions(ctx context.Context) error {
	for _, q := range valueListOptionsSQL(s.driver.Dialect().Engine()) {
		if _, err := s.driver.DB().ExecContext(ctx, q); err != nil {
			return fmt.Errorf("failed adding the value list options: %w", err)
		}
	}
	return nil
}

func (s *Service) valueListPlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// optionalID is an optional id, with a blank one read as none.
func optionalID(id *string) *string {
	if id == nil || strings.TrimSpace(*id) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*id)
	return &trimmed
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
		in.SourceTableID, in.SourceColumnID, in.SecondColumnID = nil, nil, nil
		in.SourceOccurrenceID, in.RelatedOccurrenceID = nil, nil
		in.ShowSecondOnly, in.SortBy = false, ""
		if strings.TrimSpace(in.CustomValues) == "" {
			return in, fmt.Errorf("%w: a custom value list needs at least one value", ErrInvalidFieldOptions)
		}
	case ValueListFromField:
		in.Kind = ValueListFromField
		in.CustomValues = ""
		in.SourceTableID, in.SourceColumnID = optionalID(in.SourceTableID), optionalID(in.SourceColumnID)
		if in.SourceTableID == nil || in.SourceColumnID == nil {
			return in, fmt.Errorf("%w: a value list taken from a field needs the table and the field to read", ErrInvalidFieldOptions)
		}
		return normalizeFieldOptions(in)
	default:
		return in, fmt.Errorf("%w: a value list is either %q or %q", ErrInvalidFieldOptions, ValueListCustom, ValueListFromField)
	}
	return in, nil
}

// normalizeFieldOptions checks the options of a list taken from a field.
// Leaving them out gives the list FileMaker gives by default: the first
// field's values, sorted by them, from every record.
func normalizeFieldOptions(in ValueListInput) (ValueListInput, error) {
	in.SecondColumnID = optionalID(in.SecondColumnID)
	in.SourceOccurrenceID = optionalID(in.SourceOccurrenceID)
	in.RelatedOccurrenceID = optionalID(in.RelatedOccurrenceID)

	switch strings.ToLower(strings.TrimSpace(in.SortBy)) {
	case "", ValueListSortFirst:
		in.SortBy = ValueListSortFirst
	case ValueListSortSecond:
		in.SortBy = ValueListSortSecond
	default:
		return in, fmt.Errorf("%w: a value list is sorted by its %q or its %q field",
			ErrInvalidFieldOptions, ValueListSortFirst, ValueListSortSecond)
	}
	if in.SecondColumnID == nil && (in.ShowSecondOnly || in.SortBy == ValueListSortSecond) {
		return in, fmt.Errorf("%w: showing or sorting by the second field of a value list needs a second field", ErrInvalidFieldOptions)
	}

	if in.RelatedOccurrenceID == nil {
		in.SourceOccurrenceID = nil
	} else if in.SourceOccurrenceID == nil {
		return in, fmt.Errorf("%w: a value list of related values needs the table occurrence its field is read through", ErrInvalidFieldOptions)
	}
	return in, nil
}

// checkValueListReferences checks that the fields and occurrences a list
// taken from a field names exist and fit together: both fields in the list's
// table, and the related values read through an occurrence of that table.
func (s *Service) checkValueListReferences(ctx context.Context, in ValueListInput) error {
	if in.Kind != ValueListFromField {
		return nil
	}
	db := s.driver.DB()
	inTable := func(columnID string) (bool, error) {
		var n int
		q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_columns WHERE id = %s AND table_id = %s`,
			s.valueListPlaceholder(1), s.valueListPlaceholder(2))
		err := db.QueryRowContext(ctx, q, columnID, *in.SourceTableID).Scan(&n)
		return n > 0, err
	}
	if ok, err := inTable(*in.SourceColumnID); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: the field a value list reads must be a field of its table", ErrInvalidFieldOptions)
	}
	if in.SecondColumnID != nil {
		if ok, err := inTable(*in.SecondColumnID); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("%w: the second field of a value list must be a field of the same table as the first", ErrInvalidFieldOptions)
		}
	}
	if in.RelatedOccurrenceID == nil {
		return nil
	}

	baseTable := func(occurrenceID string) (string, error) {
		var tableID string
		q := fmt.Sprintf(`SELECT base_table_id FROM sys_table_occurrences WHERE id = %s`, s.valueListPlaceholder(1))
		err := db.QueryRowContext(ctx, q, occurrenceID).Scan(&tableID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return tableID, err
	}
	if tableID, err := baseTable(*in.RelatedOccurrenceID); err != nil {
		return err
	} else if tableID == "" {
		return fmt.Errorf("%w: the table occurrence the related values start from does not exist", ErrInvalidFieldOptions)
	}
	if tableID, err := baseTable(*in.SourceOccurrenceID); err != nil {
		return err
	} else if tableID != *in.SourceTableID {
		return fmt.Errorf("%w: the related values must be read through an occurrence of the value list's table", ErrInvalidFieldOptions)
	}
	return nil
}

// prepareValueList checks a list before it is written: its definition, that
// its name is free, and what it refers to.
func (s *Service) prepareValueList(ctx context.Context, in ValueListInput, exceptID string) (ValueListInput, error) {
	in, err := normalizeValueListInput(in)
	if err != nil {
		return in, err
	}
	if taken, err := s.valueListNameTaken(ctx, in.Name, exceptID); err != nil {
		return in, err
	} else if taken {
		return in, fmt.Errorf("%w: %s", ErrValueListExists, in.Name)
	}
	if err := s.checkValueListReferences(ctx, in); err != nil {
		return in, err
	}
	return in, s.ensureValueListOptions(ctx)
}

// nullableSort is the sort order as stored: nothing for a custom list.
func nullableSort(sortBy string) *string {
	if sortBy == "" {
		return nil
	}
	return &sortBy
}

// CreateValueList registers a new value list.
func (s *Service) CreateValueList(ctx context.Context, in ValueListInput) (*ValueList, error) {
	in, err := s.prepareValueList(ctx, in, "")
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	list := &ValueList{
		ID:                  uuid.NewString(),
		Name:                in.Name,
		Kind:                in.Kind,
		CustomValues:        in.CustomValues,
		SourceTableID:       in.SourceTableID,
		SourceColumnID:      in.SourceColumnID,
		SecondColumnID:      in.SecondColumnID,
		ShowSecondOnly:      in.ShowSecondOnly,
		SortBy:              in.SortBy,
		SourceOccurrenceID:  in.SourceOccurrenceID,
		RelatedOccurrenceID: in.RelatedOccurrenceID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	marks := make([]string, 13)
	for i := range marks {
		marks[i] = s.valueListPlaceholder(i + 1)
	}
	q := `INSERT INTO sys_value_lists (id, name, kind, custom_values, source_table_id, source_column_id,
	      second_column_id, show_second_only, sort_by, source_occurrence_id, related_occurrence_id, created_at, updated_at)
	      VALUES (` + strings.Join(marks, ", ") + `)`
	if _, err := s.driver.DB().ExecContext(ctx, q, list.ID, list.Name, list.Kind, list.CustomValues,
		list.SourceTableID, list.SourceColumnID, list.SecondColumnID, list.ShowSecondOnly, nullableSort(list.SortBy),
		list.SourceOccurrenceID, list.RelatedOccurrenceID, list.CreatedAt, list.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed creating value list %q: %w", list.Name, err)
	}
	return list, nil
}

// UpdateValueList replaces a value list's definition.
func (s *Service) UpdateValueList(ctx context.Context, id string, in ValueListInput) (*ValueList, error) {
	in, err := s.prepareValueList(ctx, in, id)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	p := s.valueListPlaceholder
	q := fmt.Sprintf(`UPDATE sys_value_lists SET name = %s, kind = %s, custom_values = %s,
	                  source_table_id = %s, source_column_id = %s, second_column_id = %s, show_second_only = %s,
	                  sort_by = %s, source_occurrence_id = %s, related_occurrence_id = %s, updated_at = %s WHERE id = %s`,
		p(1), p(2), p(3), p(4), p(5), p(6), p(7), p(8), p(9), p(10), p(11), p(12))
	res, err := s.driver.DB().ExecContext(ctx, q, in.Name, in.Kind, in.CustomValues,
		in.SourceTableID, in.SourceColumnID, in.SecondColumnID, in.ShowSecondOnly, nullableSort(in.SortBy),
		in.SourceOccurrenceID, in.RelatedOccurrenceID, now, id)
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
	lists, err := s.readValueLists(ctx, `WHERE id = `+s.valueListPlaceholder(1), id)
	if err != nil {
		return nil, err
	}
	if len(lists) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrValueListNotFound, id)
	}
	return &lists[0], nil
}

// ListValueLists reads every value list, by name.
func (s *Service) ListValueLists(ctx context.Context) ([]ValueList, error) {
	lists, err := s.readValueLists(ctx, `ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the value lists: %w", err)
	}
	return lists, nil
}

// readValueLists reads the lists a clause selects. A database created before
// a list had field options gets their columns the first time one is read.
func (s *Service) readValueLists(ctx context.Context, clause string, args ...interface{}) ([]ValueList, error) {
	q := `SELECT id, name, kind, custom_values, source_table_id, source_column_id, second_column_id,
	      show_second_only, sort_by, source_occurrence_id, related_occurrence_id, created_at, updated_at
	      FROM sys_value_lists ` + clause
	rows, err := s.driver.DB().QueryContext(ctx, q, args...)
	if err != nil {
		if s.ensureValueListOptions(ctx) != nil {
			return nil, err
		}
		if rows, err = s.driver.DB().QueryContext(ctx, q, args...); err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	lists := []ValueList{}
	for rows.Next() {
		var list ValueList
		var custom, sortBy sql.NullString
		var showSecondOnly sql.NullBool
		if err := rows.Scan(&list.ID, &list.Name, &list.Kind, &custom, &list.SourceTableID, &list.SourceColumnID,
			&list.SecondColumnID, &showSecondOnly, &sortBy, &list.SourceOccurrenceID, &list.RelatedOccurrenceID,
			&list.CreatedAt, &list.UpdatedAt); err != nil {
			return nil, err
		}
		list.CustomValues = custom.String
		if list.Kind == ValueListFromField {
			// A list written before it had these options, or whose second
			// field was deleted, shows and sorts by its first field.
			list.SortBy = ValueListSortFirst
			if list.SecondColumnID != nil {
				list.ShowSecondOnly = showSecondOnly.Bool
				if sortBy.String == ValueListSortSecond {
					list.SortBy = ValueListSortSecond
				}
			}
		}
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

// ValueListItem is one entry a value list offers.
type ValueListItem struct {
	// Value is what choosing the entry stores in the field.
	Value string `json:"value"`
	// Second is the second field's value, for a list that has one: what is
	// shown beside Value, or in its place.
	Second *string `json:"second,omitempty"`
}

// ValueListRecord is the record a value list is shown on, which a list of
// related values starts from: a record of the table named.
type ValueListRecord struct {
	Table string
	ID    string
}

// ValueListValues resolves a list to the values it offers right now, with no
// record in hand: the lines of a custom list, or the distinct values a field
// holds.
func (s *Service) ValueListValues(ctx context.Context, id string) ([]string, error) {
	items, err := s.ValueListItems(ctx, id, nil, nil)
	if err != nil {
		return nil, err
	}
	return ItemValues(items), nil
}

// ItemValues are the values the entries store.
func ItemValues(items []ValueListItem) []string {
	values := make([]string, len(items))
	for i, item := range items {
		values[i] = item.Value
	}
	return values
}

// ValueListItems resolves a list to the entries it offers right now: the
// lines of a custom list in the order they were typed, or the values a field
// holds, each once, sorted by the first or the second field.
//
// A list that includes only related values offers those of the records
// related to record, and nothing without one. They are read with reader, the
// caller's data service, so a table the caller may not read offers nothing;
// without a reader every table is read.
func (s *Service) ValueListItems(ctx context.Context, id string, record *ValueListRecord, reader *data.Service) ([]ValueListItem, error) {
	list, err := s.GetValueList(ctx, id)
	if err != nil {
		return nil, err
	}
	if list.Kind == ValueListCustom {
		values := SplitCustomValues(list.CustomValues)
		items := make([]ValueListItem, len(values))
		for i, value := range values {
			items[i] = ValueListItem{Value: value}
		}
		return items, nil
	}

	src, err := s.valueListSource(ctx, list)
	if err != nil {
		return nil, err
	}
	var items []ValueListItem
	if list.RelatedOccurrenceID != nil {
		if reader == nil {
			reader = data.NewService(s.driver)
		}
		items, err = s.relatedFieldItems(ctx, list, src, record, reader)
	} else {
		items, err = s.distinctFieldItems(ctx, src)
	}
	if err != nil {
		return nil, err
	}
	return sortedItems(items, list.SortBy == ValueListSortSecond), nil
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

// fieldSource is the table and columns a list taken from a field reads.
// second is empty for a list with one field.
type fieldSource struct {
	table, first, second string
}

func (s *Service) valueListSource(ctx context.Context, list *ValueList) (fieldSource, error) {
	var src fieldSource
	q := fmt.Sprintf(`SELECT t.name, c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
	                  WHERE c.id = %s AND t.id = %s`, s.valueListPlaceholder(1), s.valueListPlaceholder(2))
	if err := s.driver.DB().QueryRowContext(ctx, q, *list.SourceColumnID, *list.SourceTableID).Scan(&src.table, &src.first); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return src, fmt.Errorf("%w: the field this value list reads no longer exists", ErrValueListNotFound)
		}
		return src, err
	}
	if dbal.IsReservedTableName(src.table) {
		return src, fmt.Errorf("%w: internal tables cannot feed a value list", ErrInvalidFieldOptions)
	}
	if list.SecondColumnID != nil {
		q := fmt.Sprintf(`SELECT name FROM sys_columns WHERE id = %s AND table_id = %s`,
			s.valueListPlaceholder(1), s.valueListPlaceholder(2))
		err := s.driver.DB().QueryRowContext(ctx, q, *list.SecondColumnID, *list.SourceTableID).Scan(&src.second)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return src, err
		}
	}
	return src, nil
}

// distinctFieldItems reads the values the list's fields hold in every record.
func (s *Service) distinctFieldItems(ctx context.Context, src fieldSource) ([]ValueListItem, error) {
	dialect := s.driver.Dialect()
	first := dialect.QuoteIdentifier(src.first)
	columns := first
	if src.second != "" {
		columns += ", " + dialect.QuoteIdentifier(src.second)
	}
	sqlText := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL LIMIT %d",
		columns, dialect.QuoteIdentifier(src.table), first, maxDynamicValues)

	rows, err := s.driver.DB().QueryContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("failed reading the values of %s.%s: %w", src.table, src.first, err)
	}
	defer rows.Close()

	items := []ValueListItem{}
	for rows.Next() {
		var value, second sql.NullString
		dest := []interface{}{&value}
		if src.second != "" {
			dest = append(dest, &second)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		if item, ok := newItem(value.String, second.String, src.second != ""); ok {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

// relatedFieldItems reads the values the list's fields hold in the records
// related to record, from the occurrence the list starts from to the one its
// field is read through.
func (s *Service) relatedFieldItems(
	ctx context.Context, list *ValueList, src fieldSource, record *ValueListRecord, reader *data.Service,
) ([]ValueListItem, error) {
	items := []ValueListItem{}
	if record == nil || strings.TrimSpace(record.ID) == "" {
		return items, nil
	}
	// The record must be one the relationship path starts from: a record of
	// another table has no related values here.
	var startTable string
	q := fmt.Sprintf(`SELECT t.name FROM sys_table_occurrences o JOIN sys_tables t ON t.id = o.base_table_id
	                  WHERE o.id = %s`, s.valueListPlaceholder(1))
	if err := s.driver.DB().QueryRowContext(ctx, q, *list.RelatedOccurrenceID).Scan(&startTable); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: the table occurrence this value list starts from no longer exists", ErrValueListNotFound)
		}
		return nil, err
	}
	if record.Table != startTable {
		return items, nil
	}

	fields := []string{src.first}
	if src.second != "" {
		fields = append(fields, src.second)
	}
	rows, err := reader.RelatedValues(ctx, *list.RelatedOccurrenceID, record.ID, *list.SourceOccurrenceID, fields...)
	if errors.Is(err, data.ErrRecordNotFound) || errors.Is(err, data.ErrNoAccess) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		item, ok := newItem(valueText(row[src.first]), valueText(row[src.second]), src.second != "")
		if ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// newItem is the entry for a record's values. A record with nothing in the
// first field offers nothing: there would be no value to store.
func newItem(value, second string, hasSecond bool) (ValueListItem, bool) {
	item := ValueListItem{Value: strings.TrimSpace(value)}
	if item.Value == "" {
		return item, false
	}
	if hasSecond {
		text := strings.TrimSpace(second)
		item.Second = &text
	}
	return item, true
}

// valueText is a value read through the data service, as text the way the
// database driver gives the same value.
func valueText(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case time.Time:
		return t.Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(t)
	}
}

// sortedItems orders the entries by the field the list sorts by, then keeps
// each value once and at most maxDynamicValues of them. A value the second
// field gives two texts for is offered once, with the first in that order,
// because both entries would store the same value.
func sortedItems(items []ValueListItem, bySecond bool) []ValueListItem {
	key := func(item ValueListItem) string {
		if bySecond && item.Second != nil {
			return *item.Second
		}
		return item.Value
	}
	sort.SliceStable(items, func(i, j int) bool {
		if a, b := key(items[i]), key(items[j]); a != b {
			return a < b
		}
		return items[i].Value < items[j].Value
	})
	seen := map[string]bool{}
	kept := items[:0]
	for _, item := range items {
		if seen[item.Value] || len(kept) == maxDynamicValues {
			continue
		}
		seen[item.Value] = true
		kept = append(kept, item)
	}
	return kept
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
