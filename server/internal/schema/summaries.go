package schema

import (
	"context"
	"fmt"
	"strings"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// checkSummary validates a summary field's definition before it is saved: the
// kind of summary must be one File4Base computes, the field it is taken over
// must exist, must not be another summary, and must hold numbers when the
// summary needs them (#32).
//
// Like a calculation, the definition is stored in `calculation_formula` — the
// column that already means "how this field is computed".
func (s *Service) checkSummary(
	ctx context.Context, tableID, columnID, fieldName string,
	fieldType dbal.AgnosticFieldType, specJSON *string,
) error {
	if fieldType != dbal.FieldTypeSummary {
		return nil
	}
	if specJSON == nil || strings.TrimSpace(*specJSON) == "" {
		// A summary field with no definition yet is allowed: the field is
		// created first and defined in Field Options afterwards.
		return nil
	}

	spec, err := data.ParseSummarySpec(*specJSON)
	if err != nil {
		return fmt.Errorf("%w: %q: %s", ErrInvalidFieldOptions, fieldName, err.Error())
	}

	columns, err := s.listColumns(ctx, tableID)
	if err != nil {
		return err
	}
	// What each field *stores*, not what it is declared as: a calculation
	// whose result is a number is held in a number column and totals like one,
	// which is what the tutorial's Annual Fee is (#30, #32).
	types := make(map[string]dbal.AgnosticFieldType, len(columns))
	for _, col := range columns {
		if col.ID == columnID {
			continue
		}
		types[col.Name] = StorageType(col.FieldType, col.CalculationFormula)
	}

	if err := data.CheckSummarySpec(*spec, types, fieldName); err != nil {
		return fmt.Errorf("%w: %q: %s", ErrInvalidFieldOptions, fieldName, err.Error())
	}
	return nil
}

// prepareSummaryChange validates a new definition for an existing summary
// field. A summary has no value stored in a record, so unlike a calculation
// there is never a physical column to re-type.
func (s *Service) prepareSummaryChange(ctx context.Context, tableID, columnID string, specJSON *string) error {
	columns, err := s.listColumns(ctx, tableID)
	if err != nil {
		return err
	}
	for i := range columns {
		if columns[i].ID == columnID {
			return s.checkSummary(ctx, tableID, columnID, columns[i].Name, columns[i].FieldType, specJSON)
		}
	}
	return fmt.Errorf("column not found: %s", columnID)
}
