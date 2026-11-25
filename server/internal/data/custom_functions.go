package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// CustomFunctionsTable holds a solution's custom functions: what FileMaker
// calls Manage Custom Functions, a name, its parameters and a formula over
// them, which every formula of the solution may call.
const CustomFunctionsTable = "sys_custom_functions"

// ReadCustomFunctions reads the solution's custom functions, in name order.
// A database created before custom functions were stored has none.
func ReadCustomFunctions(ctx context.Context, driver dbal.DatabaseDriver) ([]calc.CustomFunction, error) {
	rows, err := driver.DB().QueryContext(ctx, `SELECT name, parameters, formula FROM `+CustomFunctionsTable+` ORDER BY name`)
	if dbal.IsUndefinedTable(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed reading the custom functions: %w", err)
	}
	defer rows.Close()
	var out []calc.CustomFunction
	for rows.Next() {
		var fn calc.CustomFunction
		var params string
		if err := rows.Scan(&fn.Name, &params, &fn.Formula); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(params), &fn.Parameters); err != nil {
			return nil, fmt.Errorf("the parameters of the custom function %q cannot be read: %w", fn.Name, err)
		}
		out = append(out, fn)
	}
	return out, rows.Err()
}

// LoadLibrary is the solution's custom functions, ready for its formulas to
// call. A function whose formula cannot be read is left out of it, so the
// formulas that do not call it still work; one that does is reported as
// calling a function that does not exist.
func LoadLibrary(ctx context.Context, driver dbal.DatabaseDriver) (*calc.Library, error) {
	defs, err := ReadCustomFunctions(ctx, driver)
	if err != nil || len(defs) == 0 {
		return nil, err
	}
	lib, err := calc.NewLibrary(defs, calc.Options{})
	var broken *calc.LibraryError
	if err != nil && !errors.As(err, &broken) {
		return nil, err
	}
	return lib, nil
}
