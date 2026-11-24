package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// readCalculation takes the formula and its result type out of what was saved.
// Older columns hold the formula as plain text, which reads as a text result.
func readCalculation(raw string) (formula string, result calc.ResultType) {
	return data.ParseStoredCalculation(raw)
}

// StorageType is the column type a field is physically stored in. It is the
// field's own type, except for a calculation, which is stored as whatever its
// formula produces so that a numeric result sorts and compares as a number.
func StorageType(fieldType dbal.AgnosticFieldType, calculationFormula *string) dbal.AgnosticFieldType {
	if fieldType != dbal.FieldTypeCalculation || calculationFormula == nil {
		return fieldType
	}
	_, result := readCalculation(*calculationFormula)
	switch result {
	case calc.ResultNumber:
		return dbal.FieldTypeNumber
	case calc.ResultDate:
		return dbal.FieldTypeDate
	case calc.ResultTimestamp:
		return dbal.FieldTypeTimestamp
	case calc.ResultBoolean:
		return dbal.FieldTypeBoolean
	default:
		return dbal.FieldTypeText
	}
}

// checkCalculation validates the formula of a calculation field before it is
// saved: it must parse, it may only read fields the table has, and the
// calculation fields of the table must not depend on each other in a ring.
//
// A related field (Occurrence::Field) may only be read by an unstored
// calculation, which is computed each time a record is read; it must name an
// occurrence and a field that exist, reachable through relationships. A
// stored calculation can then neither read related fields nor an unstored
// calculation, since what it stores would go stale.
//
// `columnID` is the column being saved; it is excluded from the existing ones
// so that saving a field again does not see its own previous formula.
func (s *Service) checkCalculation(
	ctx context.Context, tableID, columnID, fieldName string, fieldType dbal.AgnosticFieldType, formulaJSON *string,
) error {
	if fieldType != dbal.FieldTypeCalculation {
		return nil
	}
	if formulaJSON == nil || strings.TrimSpace(*formulaJSON) == "" {
		// A calculation field with no formula yet is allowed: the field is
		// created first and the formula written in Field Options afterwards.
		return nil
	}

	spec := data.ReadStoredCalculation(*formulaJSON)
	expr, err := calc.Parse(spec.Formula)
	if err != nil {
		return fmt.Errorf("%w: the formula of %q is not valid: %s", ErrInvalidFieldOptions, fieldName, err.Error())
	}

	related := expr.RelatedFields()
	for _, ref := range related {
		if ref.Missing {
			return fmt.Errorf("%w: the formula of %q reads %s::%s, a table or field that was deleted in FileMaker",
				ErrInvalidFieldOptions, fieldName, ref.Table, ref.Field)
		}
	}
	if len(related) > 0 && spec.IsStored() {
		return fmt.Errorf("%w: the formula of %q reads the related field %s::%s; a calculation that reads related records "+
			"must be computed when it is shown: make it unstored (clear \"Store the result\" in Field Options)",
			ErrInvalidFieldOptions, fieldName, related[0].Table, related[0].Field)
	}

	columns, err := s.listColumns(ctx, tableID)
	if err != nil {
		return err
	}

	known := make(map[string]struct{}, len(columns)+1)
	known[fieldName] = struct{}{}
	for _, col := range columns {
		known[col.Name] = struct{}{}
	}
	for _, read := range expr.Fields() {
		if _, ok := known[read]; !ok {
			return fmt.Errorf("%w: the formula of %q reads %q, which this table does not have",
				ErrInvalidFieldOptions, fieldName, read)
		}
	}
	if len(related) > 0 || spec.Context != "" {
		if err := s.checkRelatedReads(ctx, tableID, fieldName, spec.Context, related); err != nil {
			return err
		}
	}

	// The order the table's calculations must be computed in, with this one
	// as it is about to be saved, and which of them are stored.
	reads := map[string][]string{fieldName: expr.Fields()}
	stored := map[string]bool{fieldName: spec.IsStored()}
	for _, col := range columns {
		if col.ID == columnID || col.Name == fieldName {
			continue
		}
		if col.FieldType != dbal.FieldTypeCalculation || col.CalculationFormula == nil {
			continue
		}
		other := data.ReadStoredCalculation(*col.CalculationFormula)
		otherExpr, err := calc.Parse(other.Formula)
		if err != nil {
			continue // already-broken formulas are reported when they are saved
		}
		reads[col.Name] = otherExpr.Fields()
		stored[col.Name] = other.IsStored()
	}

	if _, err := calc.Order(reads); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidFieldOptions, err.Error())
	}
	for field, fields := range reads {
		if !stored[field] {
			continue
		}
		for _, read := range fields {
			readStored, isCalc := stored[read]
			if isCalc && !readStored && (field == fieldName || read == fieldName) {
				return fmt.Errorf("%w: %q is stored and reads %q, which is computed only when it is shown: make %q unstored too",
					ErrInvalidFieldOptions, field, read, field)
			}
		}
	}
	return nil
}

// checkRelatedReads checks that every related field an unstored calculation
// of a table reads exists, and that a relationship leads to it from the
// occurrence the calculation is evaluated in.
func (s *Service) checkRelatedReads(ctx context.Context, tableID, fieldName, contextName string, related []calc.Reference) error {
	graph, err := s.occurrenceGraph(ctx)
	if err != nil {
		return err
	}
	tableName, err := s.tableNameOf(ctx, tableID)
	if err != nil {
		return err
	}
	from, ok := graph.contextOf(tableID, tableName, contextName)
	if !ok {
		if contextName != "" {
			return fmt.Errorf("%w: the formula of %q is evaluated from %q, which is not a table occurrence of this table",
				ErrInvalidFieldOptions, fieldName, contextName)
		}
		return fmt.Errorf("%w: the formula of %q reads related fields, but its table has no table occurrence",
			ErrInvalidFieldOptions, fieldName)
	}
	reachable := graph.reachableFrom(from.id)
	columnsOf := map[string]map[string]bool{}
	for _, ref := range related {
		to, ok := graph.byName[strings.ToLower(ref.Table)]
		if !ok {
			return fmt.Errorf("%w: the formula of %q reads %s::%s, but there is no table occurrence %q",
				ErrInvalidFieldOptions, fieldName, ref.Table, ref.Field, ref.Table)
		}
		if columnsOf[to.tableID] == nil {
			columns, err := s.listColumns(ctx, to.tableID)
			if err != nil {
				return err
			}
			columnsOf[to.tableID] = map[string]bool{}
			for _, col := range columns {
				columnsOf[to.tableID][col.Name] = true
			}
		}
		if !columnsOf[to.tableID][ref.Field] {
			return fmt.Errorf("%w: the formula of %q reads %s::%s, which that table does not have",
				ErrInvalidFieldOptions, fieldName, ref.Table, ref.Field)
		}
		if !reachable[to.id] {
			return fmt.Errorf("%w: the formula of %q reads %s::%s, but no relationship leads from %s to %s",
				ErrInvalidFieldOptions, fieldName, ref.Table, ref.Field, from.name, to.name)
		}
	}
	return nil
}

// checkUnstorable refuses to make unstored a calculation that cannot be: one
// whose column is required, since an unstored calculation's column stays
// empty, or one a relationship matches on, since the match would read that
// empty column.
func (s *Service) checkUnstorable(ctx context.Context, col ColumnMetadata, formulaJSON *string) error {
	if col.FieldType != dbal.FieldTypeCalculation || formulaJSON == nil || data.ReadStoredCalculation(*formulaJSON).IsStored() {
		return nil
	}
	if !col.IsNullable {
		return fmt.Errorf("%w: %q cannot be unstored: an unstored calculation is not kept in the database, so its field cannot be required",
			ErrInvalidFieldOptions, col.Name)
	}
	if col.ID == "" {
		return nil
	}
	q := `SELECT name FROM sys_relationships WHERE left_column_id = $1 OR right_column_id = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		q = `SELECT name FROM sys_relationships WHERE left_column_id = ? OR right_column_id = ?`
	}
	args := []interface{}{col.ID}
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		args = append(args, col.ID)
	}
	var relationship string
	err := s.driver.DB().QueryRowContext(ctx, q, args...).Scan(&relationship)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %q cannot be unstored: the relationship %q matches records on it, and an unstored calculation is not kept in the database to match on",
		ErrInvalidFieldOptions, col.Name, relationship)
}

type occurrenceRef struct{ id, name, tableID string }

// occurrenceGraph is the table occurrences and the relationships between them.
type occurrenceGraph struct {
	byName  map[string]occurrenceRef // lower-case name
	byTable map[string][]occurrenceRef
	links   map[string][]string // occurrence id -> the occurrences it is related to
}

func (s *Service) occurrenceGraph(ctx context.Context) (*occurrenceGraph, error) {
	g := &occurrenceGraph{byName: map[string]occurrenceRef{}, byTable: map[string][]occurrenceRef{}, links: map[string][]string{}}
	rows, err := s.driver.DB().QueryContext(ctx, `SELECT id, name, base_table_id FROM sys_table_occurrences ORDER BY created_at, name`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the table occurrences: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var o occurrenceRef
		if err := rows.Scan(&o.id, &o.name, &o.tableID); err != nil {
			return nil, err
		}
		g.byName[strings.ToLower(o.name)] = o
		g.byTable[o.tableID] = append(g.byTable[o.tableID], o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	links, err := s.driver.DB().QueryContext(ctx, `SELECT left_occurrence_id, right_occurrence_id FROM sys_relationships`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the relationships: %w", err)
	}
	defer links.Close()
	for links.Next() {
		var left, right string
		if err := links.Scan(&left, &right); err != nil {
			return nil, err
		}
		g.links[left] = append(g.links[left], right)
		g.links[right] = append(g.links[right], left)
	}
	return g, links.Err()
}

// contextOf is the occurrence a calculation of a table is evaluated from: the
// one it names, else the one named like the table, else the table's first.
// The data service resolves it the same way.
func (g *occurrenceGraph) contextOf(tableID, tableName, name string) (occurrenceRef, bool) {
	if name != "" {
		o, ok := g.byName[strings.ToLower(name)]
		return o, ok && o.tableID == tableID
	}
	if o, ok := g.byName[strings.ToLower(tableName)]; ok && o.tableID == tableID {
		return o, true
	}
	list := g.byTable[tableID]
	if len(list) == 0 {
		return occurrenceRef{}, false
	}
	return list[0], true
}

func (g *occurrenceGraph) reachableFrom(id string) map[string]bool {
	seen := map[string]bool{id: true}
	for queue := []string{id}; len(queue) > 0; queue = queue[1:] {
		for _, next := range g.links[queue[0]] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}

// listColumns reads a table's registered columns.
func (s *Service) listColumns(ctx context.Context, tableID string) ([]ColumnMetadata, error) {
	q := `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at FROM sys_columns WHERE table_id = $1 ORDER BY created_at ASC`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		q = `SELECT id, table_id, name, display_name, field_type, is_nullable, is_primary_key, default_value, calculation_formula, validation_rules, created_at FROM sys_columns WHERE table_id = ? ORDER BY created_at ASC`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, tableID)
	if err != nil {
		return nil, fmt.Errorf("failed reading the columns of %s: %w", tableID, err)
	}
	defer rows.Close()

	var columns []ColumnMetadata
	for rows.Next() {
		var col ColumnMetadata
		if err := rows.Scan(&col.ID, &col.TableID, &col.Name, &col.DisplayName, &col.FieldType,
			&col.IsNullable, &col.IsPrimaryKey, &col.DefaultValue, &col.CalculationFormula,
			&col.ValidationRules, &col.CreatedAt); err != nil {
			return nil, err
		}
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

// prepareCalculationChange validates a new formula for a column and, when its
// result type means the value is stored differently, returns the work to
// re-type the physical column. The check runs before the catalog is written so
// a bad formula never replaces a good one; the re-typing runs after, because
// it has to match what was written.
func (s *Service) prepareCalculationChange(ctx context.Context, tableID, columnID string, formulaJSON *string) (func(context.Context) error, error) {
	columns, err := s.listColumns(ctx, tableID)
	if err != nil {
		return nil, err
	}
	var current *ColumnMetadata
	for i := range columns {
		if columns[i].ID == columnID {
			current = &columns[i]
			break
		}
	}
	if current == nil {
		return nil, fmt.Errorf("column not found: %s", columnID)
	}

	if err := s.checkCalculation(ctx, tableID, columnID, current.Name, current.FieldType, formulaJSON); err != nil {
		return nil, err
	}
	if err := s.checkUnstorable(ctx, *current, formulaJSON); err != nil {
		return nil, err
	}
	if current.FieldType != dbal.FieldTypeCalculation {
		return nil, nil
	}

	was := StorageType(current.FieldType, current.CalculationFormula)
	now := StorageType(current.FieldType, formulaJSON)
	if was == now {
		return nil, nil
	}

	tableName, err := s.tableNameOf(ctx, tableID)
	if err != nil {
		return nil, err
	}
	columnName := current.Name
	return func(ctx context.Context) error {
		sqlText, err := s.driver.Dialect().BuildAlterColumnTypeSQL(tableName, columnName, now)
		if err != nil {
			return err
		}
		if _, err := s.driver.DB().ExecContext(ctx, sqlText); err != nil {
			return fmt.Errorf("the formula now produces a %s, but %s.%s could not be changed to hold it: %w",
				strings.ToLower(string(now)), tableName, columnName, err)
		}
		return nil
	}, nil
}

func (s *Service) tableNameOf(ctx context.Context, tableID string) (string, error) {
	q := `SELECT name FROM sys_tables WHERE id = $1`
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		q = `SELECT name FROM sys_tables WHERE id = ?`
	}
	var name string
	if err := s.driver.DB().QueryRowContext(ctx, q, tableID).Scan(&name); err != nil {
		return "", fmt.Errorf("table not found: %s", tableID)
	}
	return name, nil
}

// TableNameByID is the physical table name of a registered table.
func (s *Service) TableNameByID(ctx context.Context, tableID string) (string, error) {
	return s.tableNameOf(ctx, tableID)
}
