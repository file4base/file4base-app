package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// ErrInvalidCustomFunction is returned for a custom function that cannot be
// stored as given: no name, a name taken twice, or a formula that does not
// read.
var ErrInvalidCustomFunction = errors.New("invalid custom function")

// Who may see and change a custom function in the design, as FileMaker's
// "Availability" option says. Formulas that call it are computed for every
// account either way.
const (
	CustomFunctionAccessAll        = "all"
	CustomFunctionAccessFullAccess = "full_access"
)

const maxCustomFunctionName = 128

// CustomFunction is a function of the solution's own, which every formula of
// the solution may call: what FileMaker defines in Manage Custom Functions.
// Its formula reads its parameters, and may call itself and the solution's
// other custom functions.
type CustomFunction struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Parameters []string  `json:"parameters"`
	Formula    string    `json:"formula"`
	Access     string    `json:"access"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// customFunctionsSQL creates the catalog table of custom functions (see
// migrations/007_custom_functions.sql).
func customFunctionsSQL(ts string) string {
	return `CREATE TABLE IF NOT EXISTS ` + data.CustomFunctionsTable + ` (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(128) NOT NULL UNIQUE,
			parameters TEXT NOT NULL,
			formula TEXT NOT NULL,
			access VARCHAR(16) NOT NULL DEFAULT 'all',
			created_at ` + ts + `,
			updated_at ` + ts + `
		);`
}

// customFunctionNameIndexSQL makes a name unique whatever its case, so two
// imports at once cannot add two spellings of one function. MariaDB's
// case-insensitive collation already makes the UNIQUE name so.
func customFunctionNameIndexSQL(engine dbal.EngineType) []string {
	if engine == dbal.EngineMariaDB {
		return nil
	}
	return []string{`CREATE UNIQUE INDEX IF NOT EXISTS sys_custom_functions_name_ci ON ` + data.CustomFunctionsTable + ` (LOWER(name));`}
}

// ListCustomFunctions reads the solution's custom functions, in name order.
func (s *Service) ListCustomFunctions(ctx context.Context) ([]CustomFunction, error) {
	rows, err := s.driver.DB().QueryContext(ctx,
		`SELECT id, name, parameters, formula, access, created_at, updated_at FROM `+data.CustomFunctionsTable+` ORDER BY name`)
	if err != nil {
		if dbal.IsUndefinedTable(err) {
			return []CustomFunction{}, nil
		}
		return nil, fmt.Errorf("failed reading the custom functions: %w", err)
	}
	defer rows.Close()
	out := []CustomFunction{}
	for rows.Next() {
		var fn CustomFunction
		var params string
		if err := rows.Scan(&fn.ID, &fn.Name, &params, &fn.Formula, &fn.Access, &fn.CreatedAt, &fn.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(params), &fn.Parameters); err != nil {
			return nil, fmt.Errorf("the parameters of the custom function %q cannot be read: %w", fn.Name, err)
		}
		out = append(out, fn)
	}
	return out, rows.Err()
}

// normalizeCustomFunction trims a custom function and checks what can be
// checked of it alone.
func normalizeCustomFunction(fn BundleCustomFunction) (BundleCustomFunction, error) {
	fn.Name = strings.TrimSpace(fn.Name)
	if fn.Name == "" {
		return fn, fmt.Errorf("%w: a custom function needs a name", ErrInvalidCustomFunction)
	}
	if calc.IsBuiltinFunction(fn.Name) {
		return fn, fmt.Errorf("%w: %s is the name of a built-in function, which a custom function cannot take", ErrInvalidCustomFunction, fn.Name)
	}
	if len([]rune(fn.Name)) > maxCustomFunctionName {
		return fn, fmt.Errorf("%w: the name of the custom function %q is longer than %d characters", ErrInvalidCustomFunction, fn.Name, maxCustomFunctionName)
	}
	params := make([]string, 0, len(fn.Parameters))
	seen := map[string]bool{}
	for _, p := range fn.Parameters {
		p = strings.TrimSpace(p)
		if p == "" || seen[strings.ToLower(p)] {
			return fn, fmt.Errorf("%w: the parameters of %s must be named, each once", ErrInvalidCustomFunction, fn.Name)
		}
		seen[strings.ToLower(p)] = true
		params = append(params, p)
	}
	fn.Parameters = params
	switch fn.Access {
	case "":
		fn.Access = CustomFunctionAccessAll
	case CustomFunctionAccessAll, CustomFunctionAccessFullAccess:
	default:
		return fn, fmt.Errorf("%w: the access of %s must be %q or %q", ErrInvalidCustomFunction, fn.Name,
			CustomFunctionAccessAll, CustomFunctionAccessFullAccess)
	}
	return fn, nil
}

// importCustomFunctions adds a bundle's custom functions to the solution,
// before any formula that calls them is saved. A function the solution
// already has, by name, keeps its definition, as an existing table keeps its
// fields. Every function is checked to read, with all the others, so one may
// call another defined after it. It returns the ids of the functions it
// added, for an import that fails later to remove them.
func (s *Service) importCustomFunctions(ctx context.Context, functions []BundleCustomFunction) ([]string, error) {
	if len(functions) == 0 {
		return nil, nil
	}
	existing, err := s.ListCustomFunctions(ctx)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	var all []calc.CustomFunction
	for _, fn := range existing {
		have[strings.ToUpper(fn.Name)] = true
		all = append(all, calc.CustomFunction{Name: fn.Name, Parameters: fn.Parameters, Formula: fn.Formula})
	}
	var added []BundleCustomFunction
	for _, fn := range functions {
		fn, err := normalizeCustomFunction(fn)
		if err != nil {
			return nil, err
		}
		key := strings.ToUpper(fn.Name)
		if have[key] {
			continue
		}
		have[key] = true
		added = append(added, fn)
		all = append(all, calc.CustomFunction{Name: fn.Name, Parameters: fn.Parameters, Formula: fn.Formula})
	}
	if len(added) == 0 {
		return nil, nil
	}
	if _, err := calc.NewLibrary(all, calc.Options{}); err != nil {
		var broken *calc.LibraryError
		if !errors.As(err, &broken) {
			return nil, err
		}
		// Only the functions added are checked: one the solution already
		// has is no reason to refuse the file.
		for _, fn := range added {
			if problem := broken.Problems[fn.Name]; problem != nil {
				return nil, fmt.Errorf("%w: %s %v", ErrInvalidCustomFunction, fn.Name, problem)
			}
		}
	}

	d := s.driver.Dialect()
	q := fmt.Sprintf(`INSERT INTO %s (id, name, parameters, formula, access, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s)`,
		data.CustomFunctionsTable, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
		d.Placeholder(5), d.Placeholder(6), d.Placeholder(7))
	now := time.Now().UTC()
	ids := make([]string, 0, len(added))
	for _, fn := range added {
		params, err := json.Marshal(fn.Parameters)
		if err != nil {
			return ids, err
		}
		id := uuid.NewString()
		if _, err := s.driver.DB().ExecContext(ctx, q, id, fn.Name, string(params), fn.Formula, fn.Access, now, now); err != nil {
			if dbal.IsDuplicateKey(err) {
				// Added meanwhile, in some spelling, by another import:
				// it is there, and keeps its definition.
				continue
			}
			return ids, fmt.Errorf("failed saving the custom function %s: %w", fn.Name, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// deleteCustomFunction removes one custom function, by id.
func (s *Service) deleteCustomFunction(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM %s WHERE id = %s`, data.CustomFunctionsTable, s.driver.Dialect().Placeholder(1))
	_, err := s.driver.DB().ExecContext(ctx, q, id)
	return err
}

// library is the solution's custom functions, for its formulas to be read
// with.
func (s *Service) library(ctx context.Context) (*calc.Library, error) {
	return data.LoadLibrary(ctx, s.driver)
}
