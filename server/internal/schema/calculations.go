package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// storedCalculation is how the Field Options dialog saves a formula in
// sys_columns.calculation_formula.
type storedCalculation struct {
	Formula    string `json:"formula"`
	ResultType string `json:"result_type"`
}

// readCalculation takes the formula and its result type out of what was saved.
// Older columns hold the formula as plain text, which reads as a text result.
func readCalculation(raw string) (formula string, result calc.ResultType) {
	var stored storedCalculation
	if err := json.Unmarshal([]byte(raw), &stored); err == nil && stored.Formula != "" {
		return stored.Formula, calc.ParseResultType(stored.ResultType)
	}
	return raw, calc.ResultText
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
// `columnID` is the column being saved; it is excluded from the existing ones
// so that saving a field again does not see its own previous formula.
func (s *Service) checkCalculation(ctx context.Context, tableID, columnID, fieldName string, fieldType dbal.AgnosticFieldType, formulaJSON *string) error {
	if fieldType != dbal.FieldTypeCalculation {
		return nil
	}
	if formulaJSON == nil || strings.TrimSpace(*formulaJSON) == "" {
		// A calculation field with no formula yet is allowed: the field is
		// created first and the formula written in Field Options afterwards.
		return nil
	}

	formula, _ := readCalculation(*formulaJSON)
	expr, err := calc.Parse(formula)
	if err != nil {
		return fmt.Errorf("%w: the formula of %q is not valid: %s", ErrInvalidFieldOptions, fieldName, err.Error())
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

	// The order the table's calculations must be computed in, with this one
	// as it is about to be saved.
	reads := map[string][]string{fieldName: expr.Fields()}
	for _, col := range columns {
		if col.ID == columnID || col.Name == fieldName {
			continue
		}
		if col.FieldType != dbal.FieldTypeCalculation || col.CalculationFormula == nil {
			continue
		}
		otherFormula, _ := readCalculation(*col.CalculationFormula)
		other, err := calc.Parse(otherFormula)
		if err != nil {
			continue // already-broken formulas are reported when they are saved
		}
		reads[col.Name] = other.Fields()
	}

	if _, err := calc.Order(reads); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidFieldOptions, err.Error())
	}
	return nil
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
