package data

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// A calculation field and the formula that fills it.
type calculation struct {
	field  string
	expr   *calc.Expr
	result calc.ResultType
}

// StoredCalculation is how a formula is kept in sys_columns.calculation_formula.
// It is the shape the Field Options dialog writes.
type StoredCalculation struct {
	Formula    string `json:"formula"`
	ResultType string `json:"result_type"`
}

// ParseStoredCalculation reads the formula a CALCULATION column was saved with.
// Older columns hold the formula as plain text rather than as this object, so
// anything that is not the object is taken as the formula itself with a text
// result.
func ParseStoredCalculation(raw string) (formula string, result calc.ResultType) {
	var stored StoredCalculation
	if err := json.Unmarshal([]byte(raw), &stored); err == nil && stored.Formula != "" {
		return stored.Formula, calc.ParseResultType(stored.ResultType)
	}
	return raw, calc.ResultText
}

// tableCalculations loads the table's calculation fields, parses their
// formulas and puts them in the order they must be computed in: a formula that
// reads another calculation field is evaluated after it.
//
// A formula that no longer parses is skipped rather than failing the write:
// the field keeps whatever it held, and saving the field again reports the
// error. A write must not become impossible because an unrelated formula is
// broken.
func (s *Service) tableCalculations(ctx context.Context, tableName string) ([]calculation, error) {
	q := `SELECT c.name, c.calculation_formula FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
	      WHERE t.name = $1 AND c.field_type = $2 AND c.calculation_formula IS NOT NULL`
	if s.driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT c.name, c.calculation_formula FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		     WHERE t.name = ? AND c.field_type = ? AND c.calculation_formula IS NOT NULL`
	}
	rows, err := s.driver.DB().QueryContext(ctx, q, tableName, string(dbal.FieldTypeCalculation))
	if err != nil {
		return nil, fmt.Errorf("failed reading the calculations of %s: %w", tableName, err)
	}
	defer rows.Close()

	parsed := make(map[string]calculation)
	reads := make(map[string][]string)
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		formula, result := ParseStoredCalculation(raw)
		expr, err := calc.Parse(formula)
		if err != nil {
			continue
		}
		parsed[name] = calculation{field: name, expr: expr, result: result}
		reads[name] = expr.Fields()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(parsed) == 0 {
		return nil, nil
	}

	order, err := calc.Order(reads)
	if err != nil {
		// A ring of formulas is refused when it is saved; if one is already
		// stored, say so rather than looping.
		return nil, fmt.Errorf("%w: %s", ErrInvalidValue, err.Error())
	}

	ordered := make([]calculation, 0, len(order))
	for _, name := range order {
		ordered = append(ordered, parsed[name])
	}
	return ordered, nil
}

// applyCalculations computes the table's calculation fields over the record
// and writes each answer into it, so the stored row carries the result. They
// are computed in dependency order, so a formula reading another calculation
// field sees the value just computed.
//
// Whatever the caller sent for a calculation field is discarded: the formula
// owns the value.
func (s *Service) applyCalculations(ctx context.Context, tableName string, record map[string]interface{}) error {
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return err
	}
	for _, c := range calculations {
		answer, err := calc.Evaluate(c.expr, calc.Record(record), c.result)
		if err != nil {
			return fmt.Errorf("%w: the formula of %q could not be computed: %s", ErrInvalidValue, c.field, err.Error())
		}
		record[c.field] = answer
	}
	return nil
}

// recalculateForUpdate adds the calculation fields to an update. The formulas
// read the record as it will be once the update is applied, so the answers
// match what the row ends up holding.
func (s *Service) recalculateForUpdate(ctx context.Context, tableName, id string, updates map[string]interface{}) error {
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return err
	}
	if len(calculations) == 0 {
		return nil
	}

	current, err := s.GetRow(ctx, tableName, id)
	if err != nil {
		return err
	}
	merged := make(map[string]interface{}, len(current)+len(updates))
	for col, val := range current {
		merged[col] = val
	}
	for col, val := range updates {
		merged[col] = val
	}

	for _, c := range calculations {
		answer, err := calc.Evaluate(c.expr, calc.Record(merged), c.result)
		if err != nil {
			return fmt.Errorf("%w: the formula of %q could not be computed: %s", ErrInvalidValue, c.field, err.Error())
		}
		merged[c.field] = answer
		updates[c.field] = answer
	}
	return nil
}

// RecalculateTable recomputes every calculation field of every record. It is
// run after a formula changes, so the records already stored show the new
// answer rather than keeping the old one until each is edited.
//
// It reports how many records it wrote.
func (s *Service) RecalculateTable(ctx context.Context, tableName string) (int, error) {
	calculations, err := s.tableCalculations(ctx, tableName)
	if err != nil {
		return 0, err
	}
	if len(calculations) == 0 {
		return 0, nil
	}

	written := 0
	for offset := 0; ; offset += MaxPageSize {
		page, err := s.ListRows(ctx, tableName, QueryOptions{
			Limit:  MaxPageSize,
			Offset: offset,
			SortBy: "id",
		})
		if err != nil {
			return written, err
		}
		if len(page) == 0 {
			return written, nil
		}
		for _, row := range page {
			id, _ := row["id"].(string)
			if id == "" {
				continue
			}
			updates := make(map[string]interface{}, len(calculations))
			for _, c := range calculations {
				answer, err := calc.Evaluate(c.expr, calc.Record(row), c.result)
				if err != nil {
					return written, fmt.Errorf("%w: the formula of %q could not be computed for record %s: %s",
						ErrInvalidValue, c.field, id, err.Error())
				}
				row[c.field] = answer
				updates[c.field] = answer
			}
			if _, err := s.UpdateRow(ctx, tableName, id, updates); err != nil {
				return written, err
			}
			written++
		}
		if len(page) < MaxPageSize {
			return written, nil
		}
	}
}

// StorageType is the column type a field is physically stored in: its own
// type, except for a calculation, which is stored as whatever its formula
// produces so that a numeric result is read back, sorted and compared as a
// number on both engines.
func StorageType(fieldType dbal.AgnosticFieldType, calculationFormula *string) dbal.AgnosticFieldType {
	if fieldType != dbal.FieldTypeCalculation || calculationFormula == nil {
		return fieldType
	}
	_, result := ParseStoredCalculation(*calculationFormula)
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
