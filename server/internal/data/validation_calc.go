package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/validation"
)

type validationOverrideKey struct{}

// WithValidationOverride marks the writes made with ctx as saved by a person
// who was told the values break validation rules and chose to keep them:
// the rules of the fields that allow it (allow_override) are skipped, during
// data entry only.
func WithValidationOverride(ctx context.Context) context.Context {
	return context.WithValue(ctx, validationOverrideKey{}, true)
}

func validationOverride(ctx context.Context) bool {
	override, _ := ctx.Value(validationOverrideKey{}).(bool)
	return override
}

// validationCalculator evaluates the validation formulas of a table's
// fields (FileMaker's "Validated by calculation") over the record as it is
// about to be saved: the record as stored, when there is one, with the
// values the write gives over it. Self is the value of the field validated.
// The stored record is read once per write, whatever the number of formulas.
func (s *Service) validationCalculator(tableName string) validation.Calculator {
	var (
		fields  fieldSet
		stored  map[string]interface{}
		storeID string
	)
	return func(ctx context.Context, recordID string, values map[string]interface{}, f validation.Field) (bool, error) {
		lib, err := LoadLibrary(ctx, s.driver)
		if err != nil {
			return false, err
		}
		expr, err := calc.ParseWith(f.Rules.Calculation, calc.Options{Library: lib})
		if err != nil {
			return false, fmt.Errorf("%w: %s", ErrInvalidValue, err.Error())
		}
		if fields == nil {
			if fields, err = s.tableFields(ctx, tableName); err != nil {
				return false, err
			}
		}
		if recordID != "" && (stored == nil || storeID != recordID) {
			row, err := s.GetRow(ctx, tableName, recordID)
			switch {
			case errors.Is(err, ErrRecordNotFound):
				row = map[string]interface{}{} // a record being created
			case err != nil:
				return false, err
			}
			stored, storeID = row, recordID
		}
		row := make(map[string]interface{}, len(stored)+len(values))
		for k, v := range stored {
			row[k] = v
		}
		for k, v := range values {
			row[k] = v
		}
		// The unstored calculations the formula reads were computed from
		// the record as stored: they are computed again from the edit.
		reader := s.newRelatedReader()
		if err := reader.fill(ctx, tableName, []map[string]interface{}{row}, expr.Fields()); err != nil {
			return false, err
		}
		describe := s.describeContainer(ctx)
		record := make(calc.Record, len(row))
		for name := range row {
			record[name] = typedValue(tableName, fields, row, name, describe)
		}
		env := &selfContext{
			unstoredContext: unstoredContext{RecordContext: calc.RecordContext{Record: record, Repeats: fields.repeating()},
				reader: reader, ctx: ctx, table: tableName, occurrence: f.Rules.CalculationContext, row: row},
			self: record[f.Name],
		}
		answer, err := calc.EvaluateWith(expr, env, calc.ResultBoolean)
		if err != nil {
			var formula *calc.Error
			if errors.As(err, &formula) {
				return false, fmt.Errorf("%w: %s", ErrInvalidValue, err.Error())
			}
			return false, err
		}
		ok, _ := answer.(bool)
		return ok, nil
	}
}

// selfContext answers Self with the value of the field a formula belongs to.
type selfContext struct {
	unstoredContext
	self any
}

func (c *selfContext) Get(selector string) (any, error) {
	if selector == "SELF" {
		return c.self, nil
	}
	return c.unstoredContext.Get(selector)
}
