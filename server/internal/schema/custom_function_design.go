package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/calc"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// Manage Custom Functions: creating, changing and deleting a solution's
// custom functions, as FileMaker's dialog does (docs/specs/custom_functions.md).
//
// FileMaker refers to a custom function by its id, so renaming one renames it
// in every formula that calls it. File4Base stores formulas as text: a rename
// rewrites every stored formula that calls the function, in one transaction,
// and refuses when one cannot be rewritten.

// ErrCustomFunctionNotFound is returned for an id the solution has no custom
// function for.
var ErrCustomFunctionNotFound = errors.New("custom function not found")

// ErrCustomFunctionInUse is wrapped by a *CustomFunctionConflict.
var ErrCustomFunctionInUse = errors.New("custom function in use")

// ErrCustomFunctionChanged is returned when a formula a change rewrites was
// saved by someone else while the change was worked out: nothing is
// written, and saving again reads it as it is now.
var ErrCustomFunctionChanged = errors.New("custom function change outdated")

// CustomFunctionInput is a custom function as the editor saves it.
type CustomFunctionInput struct {
	Name       string   `json:"name"`
	Parameters []string `json:"parameters"`
	Formula    string   `json:"formula"`
	Access     string   `json:"access"`
}

// CustomFunctionUsage is a formula of the solution that calls a custom
// function.
type CustomFunctionUsage struct {
	// Kind is what the formula belongs to: "calculation" (a calculation
	// field), "auto_enter" (a field's calculated value), "validation" (a
	// field's validation by calculation), "custom_function", "script_step"
	// or "layout" (a button's or a trigger's parameter).
	Kind string `json:"kind"`
	// ID is the field's, the function's, the step's or the layout's.
	ID string `json:"id"`
	// Name is Table::Field, the function's, the script's or the layout's.
	Name string `json:"name"`
	// Detail says where in it: the step type, the parameter.
	Detail string `json:"detail,omitempty"`
}

// CustomFunctionConflict is a change refused because of the formulas it
// would break or change, which it lists.
type CustomFunctionConflict struct {
	Message string
	Usages  []CustomFunctionUsage
}

func (e *CustomFunctionConflict) Error() string { return e.Message }
func (e *CustomFunctionConflict) Unwrap() error { return ErrCustomFunctionInUse }

// GetCustomFunction reads one custom function, by id.
func (s *Service) GetCustomFunction(ctx context.Context, id string) (*CustomFunction, error) {
	all, err := s.ListCustomFunctions(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrCustomFunctionNotFound, id)
}

// CreateCustomFunction adds a custom function. Its formula must read, with
// the solution's other functions, and must not call itself before a
// condition could stop it. A formula that reads a field of the new name as
// a plain word, which would then call the function instead, is listed in a
// *CustomFunctionConflict.
func (s *Service) CreateCustomFunction(ctx context.Context, in CustomFunctionInput) (*CustomFunction, error) {
	fn, err := normalizeCustomFunction(BundleCustomFunction(in))
	if err != nil {
		return nil, err
	}
	existing, err := s.ListCustomFunctions(ctx)
	if err != nil {
		return nil, err
	}
	for _, other := range existing {
		if strings.EqualFold(other.Name, fn.Name) {
			return nil, fmt.Errorf("%w: the solution already has a custom function called %s", ErrInvalidCustomFunction, other.Name)
		}
	}
	defs := definitions(existing, "", nil)
	created := calc.CustomFunction{Name: fn.Name, Parameters: fn.Parameters, Formula: fn.Formula}
	lib, err := libraryOf(append(defs, created), fn.Name)
	if err != nil {
		return nil, err
	}
	// A calculation field may call the function before it exists (one
	// deleted with force, now made again).
	if err := s.checkCalculationRings(ctx, lib, nil); err != nil {
		return nil, err
	}
	before := signatures(defs)
	plan, err := s.planRename(ctx, before, before, signatures(append(defs, created)), "", fn.Name, "")
	if err != nil {
		return nil, err
	}
	if len(plan.captured) > 0 {
		return nil, &CustomFunctionConflict{
			Message: fmt.Sprintf("%d formula(s) read a field called %s as a plain word, which would then call the function: quote the field as ${%s} in them, or choose another name",
				len(plan.captured), fn.Name, fn.Name),
			Usages: plan.captured,
		}
	}

	params, err := json.Marshal(fn.Parameters)
	if err != nil {
		return nil, err
	}
	d := s.driver.Dialect()
	now := time.Now().UTC()
	id := uuid.NewString()
	q := fmt.Sprintf(`INSERT INTO %s (id, name, parameters, formula, access, created_at, updated_at) VALUES (%s, %s, %s, %s, %s, %s, %s)`,
		data.CustomFunctionsTable, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
		d.Placeholder(5), d.Placeholder(6), d.Placeholder(7))
	_, err = s.driver.DB().ExecContext(ctx, q, id, fn.Name, string(params), fn.Formula, fn.Access, now, now)
	data.InvalidateLibrary(s.driver)
	if dbal.IsDuplicateKey(err) {
		return nil, fmt.Errorf("%w: the solution already has a custom function called %s", ErrInvalidCustomFunction, fn.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("failed saving the custom function %s: %w", fn.Name, err)
	}
	return s.GetCustomFunction(ctx, id)
}

// UpdateCustomFunction changes a custom function. A new name is written into
// every stored formula that calls it, in the same transaction, as FileMaker
// renames a function everywhere it is used; when one of them cannot be
// rewritten, nothing changes. New parameters that break the formulas calling
// it, which pass the old ones, are refused with the list of them, unless
// force is set: they are then left to be corrected.
func (s *Service) UpdateCustomFunction(ctx context.Context, id string, in CustomFunctionInput, force bool) (*CustomFunction, error) {
	fn, err := normalizeCustomFunction(BundleCustomFunction(in))
	if err != nil {
		return nil, err
	}
	existing, err := s.ListCustomFunctions(ctx)
	if err != nil {
		return nil, err
	}
	var current *CustomFunction
	for i := range existing {
		if existing[i].ID == id {
			current = &existing[i]
		}
	}
	if current == nil {
		return nil, fmt.Errorf("%w: %s", ErrCustomFunctionNotFound, id)
	}
	for _, other := range existing {
		if other.ID != id && strings.EqualFold(other.Name, fn.Name) {
			return nil, fmt.Errorf("%w: the solution already has a custom function called %s", ErrInvalidCustomFunction, other.Name)
		}
	}

	// The stored formulas are read with the functions' names and
	// parameters only: as they are (before), with the new parameters under
	// the old name (middle), and under the new name (after).
	sig := calc.CustomFunction{Name: current.Name, Parameters: fn.Parameters}
	before := signatures(definitions(existing, "", nil))
	middle := signatures(definitions(existing, id, &sig))
	sig.Name = fn.Name
	after := signatures(definitions(existing, id, &sig))

	// The function's own formula, as given, may call it by its old name.
	if current.Name != fn.Name {
		if out, ok, err := calc.RenameFunction(fn.Formula, fn.Parameters, calc.Options{Library: middle}, after, current.Name, fn.Name); err == nil && ok {
			fn.Formula = out
		}
	}

	plan, err := s.planRename(ctx, before, middle, after, current.Name, fn.Name, id)
	if err != nil {
		return nil, err
	}
	if len(plan.captured) > 0 {
		return nil, &CustomFunctionConflict{
			Message: fmt.Sprintf("%d formula(s) cannot be rewritten for the new name %s: %s",
				len(plan.captured), fn.Name, plan.why),
			Usages: plan.captured,
		}
	}
	if len(plan.broken) > 0 && !force {
		return nil, &CustomFunctionConflict{
			Message: fmt.Sprintf("%d formula(s) call %s in a way the new parameters break: correct them, or save with force to correct them afterwards",
				len(plan.broken), current.Name),
			Usages: plan.broken,
		}
	}

	// The library once saved, with the other functions as rewritten, must
	// read this one, and it must be able to end.
	changed := calc.CustomFunction{Name: fn.Name, Parameters: fn.Parameters, Formula: fn.Formula}
	final := definitions(existing, id, &changed)
	for i := range final {
		for _, w := range plan.writes {
			if w.table == data.CustomFunctionsTable && w.name == final[i].Name {
				final[i].Formula = w.value
			}
		}
	}
	lib, err := libraryOf(final, fn.Name)
	if err != nil {
		return nil, err
	}
	if err := s.checkCalculationRings(ctx, lib, plan.writes); err != nil {
		return nil, err
	}

	params, err := json.Marshal(fn.Parameters)
	if err != nil {
		return nil, err
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if err := plan.apply(ctx, tx, s.driver.Dialect()); err != nil {
			return err
		}
		d := s.driver.Dialect()
		q := fmt.Sprintf(`UPDATE %s SET name = %s, parameters = %s, formula = %s, access = %s, updated_at = %s WHERE id = %s`,
			data.CustomFunctionsTable, d.Placeholder(1), d.Placeholder(2), d.Placeholder(3), d.Placeholder(4),
			d.Placeholder(5), d.Placeholder(6))
		_, err := tx.ExecContext(ctx, q, fn.Name, string(params), fn.Formula, fn.Access, time.Now().UTC(), id)
		if dbal.IsDuplicateKey(err) {
			return fmt.Errorf("%w: the solution already has a custom function called %s", ErrInvalidCustomFunction, fn.Name)
		}
		return err
	})
	data.InvalidateLibrary(s.driver)
	if err != nil {
		return nil, err
	}
	return s.GetCustomFunction(ctx, id)
}

// DeleteCustomFunction removes a custom function. One that formulas call is
// refused with the list of them, unless force is set: they then report that
// they call a function that does not exist, as FileMaker's show
// <Function Missing>.
func (s *Service) DeleteCustomFunction(ctx context.Context, id string, force bool) error {
	fn, err := s.GetCustomFunction(ctx, id)
	if err != nil {
		return err
	}
	if !force {
		usages, err := s.CustomFunctionUsages(ctx, id)
		if err != nil {
			return err
		}
		if len(usages) > 0 {
			return &CustomFunctionConflict{
				Message: fmt.Sprintf("%d formula(s) call %s: change them first, or delete with force", len(usages), fn.Name),
				Usages:  usages,
			}
		}
	}
	err = s.deleteCustomFunction(ctx, id)
	if err != nil {
		return fmt.Errorf("failed deleting the custom function %s: %w", fn.Name, err)
	}
	return nil
}

// CustomFunctionUsages lists the stored formulas that call a custom function
// themselves: calculation fields, auto-enter and validation formulas, other
// custom functions, script steps, and the parameters of layout buttons and
// triggers.
func (s *Service) CustomFunctionUsages(ctx context.Context, id string) ([]CustomFunctionUsage, error) {
	fn, err := s.GetCustomFunction(ctx, id)
	if err != nil {
		return nil, err
	}
	lib, err := data.LoadLibrary(ctx, s.driver)
	if err != nil {
		return nil, err
	}
	usages := []CustomFunctionUsage{}
	_, err = s.visitFormulas(ctx, func(site formulaSite, formula string, params []string) (string, bool) {
		if site.usage.Kind == "custom_function" && site.usage.ID == id {
			return formula, false // its own recursion
		}
		expr, err := calc.ParseFunction(calc.CustomFunction{Formula: formula, Parameters: params}, calc.Options{Library: lib})
		if err == nil && expr.CallsFunction(fn.Name) {
			usages = appendUsage(usages, site.usage)
		}
		return formula, false
	})
	return usages, err
}

// FormulaCheck is what the server says of a formula being written.
type FormulaCheck struct {
	Valid bool `json:"valid"`
	// Message says what is wrong, and Position where, in characters from
	// the start, when it is not valid.
	Message  string `json:"message,omitempty"`
	Position *int   `json:"position,omitempty"`
	// Fields are the fields of the table the formula reads, and
	// UnknownFields those the table does not have (a table was given).
	Fields        []string `json:"fields,omitempty"`
	UnknownFields []string `json:"unknown_fields,omitempty"`
}

// FormulaCheckInput is a formula to check: of a table's (a calculation, a
// script step) or, with Parameters set or Function named, of a custom
// function being written, read with the definitions it is about to have.
type FormulaCheckInput struct {
	Formula    string   `json:"formula"`
	Table      string   `json:"table,omitempty"`
	Function   string   `json:"function,omitempty"`
	Parameters []string `json:"parameters,omitempty"`
}

// CheckFormula reads a formula with the solution's custom functions, as it
// would be read once saved, and says whether it reads and where it does not.
func (s *Service) CheckFormula(ctx context.Context, in FormulaCheckInput) (*FormulaCheck, error) {
	lib, err := data.LoadLibrary(ctx, s.driver)
	if err != nil {
		return nil, err
	}
	function := strings.TrimSpace(in.Function)
	if function != "" || len(in.Parameters) > 0 {
		// A function being written calls itself with the parameters it is
		// given now.
		existing, err := s.ListCustomFunctions(ctx)
		if err != nil {
			return nil, err
		}
		self := calc.CustomFunction{Name: function, Parameters: in.Parameters, Formula: in.Formula}
		skip := ""
		for _, fn := range existing {
			if strings.EqualFold(fn.Name, function) {
				skip = fn.ID
			}
		}
		defs := definitions(existing, skip, nil)
		if function != "" && !calc.IsBuiltinFunction(function) {
			defs = append(defs, self)
		}
		lib, _ = calc.NewLibrary(defs, calc.Options{})
	}
	expr, err := calc.ParseFunction(calc.CustomFunction{Formula: in.Formula, Parameters: in.Parameters}, calc.Options{Library: lib})
	if err != nil {
		out := &FormulaCheck{Message: err.Error()}
		var problem *calc.Error
		if errors.As(err, &problem) {
			pos := problem.Pos
			out.Position = &pos
			out.Message = problem.Msg
		}
		return out, nil
	}
	if function != "" && lib.RecursesEndlessly(function) {
		return &FormulaCheck{Message: fmt.Sprintf("%s calls itself before any condition could stop it, so it would never end", function)}, nil
	}
	out := &FormulaCheck{Valid: true}
	if table := strings.TrimSpace(in.Table); table != "" {
		cols, err := s.columnNamesOf(ctx, table)
		if err != nil {
			return nil, err
		}
		out.Fields = expr.Fields()
		// A table this solution does not have tells nothing; a related
		// field is read through a relationship, not from the table.
		for _, f := range out.Fields {
			if len(cols) > 0 && !cols[f] && !strings.Contains(f, "::") {
				out.UnknownFields = append(out.UnknownFields, f)
			}
		}
	}
	return out, nil
}

// columnNamesOf lists the fields of a table, by name.
func (s *Service) columnNamesOf(ctx context.Context, table string) (map[string]bool, error) {
	q := fmt.Sprintf(`SELECT c.name FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id WHERE t.name = %s`,
		s.driver.Dialect().Placeholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// definitions are the solution's custom functions for a library, with the
// one of id replaced by with, or left out when with is nil.
func definitions(existing []CustomFunction, id string, with *calc.CustomFunction) []calc.CustomFunction {
	out := make([]calc.CustomFunction, 0, len(existing)+1)
	for _, fn := range existing {
		if id != "" && fn.ID == id {
			if with != nil {
				out = append(out, *with)
			}
			continue
		}
		out = append(out, calc.CustomFunction{Name: fn.Name, Parameters: fn.Parameters, Formula: fn.Formula})
	}
	return out
}

// libraryOf builds a library and, when name is set, requires that function
// to read and to be able to end.
func libraryOf(defs []calc.CustomFunction, name string) (*calc.Library, error) {
	lib, err := calc.NewLibrary(defs, calc.Options{})
	var broken *calc.LibraryError
	if err != nil && !errors.As(err, &broken) {
		return nil, err
	}
	if name == "" {
		return lib, nil
	}
	if broken != nil {
		for fn, problem := range broken.Problems {
			if strings.EqualFold(fn, name) {
				return nil, fmt.Errorf("%w: %s %v", ErrInvalidCustomFunction, name, problem)
			}
		}
	}
	if lib.RecursesEndlessly(name) {
		return nil, fmt.Errorf("%w: %s calls itself before any condition could stop it, so it would never end", ErrInvalidCustomFunction, name)
	}
	return lib, nil
}

// checkCalculationRings refuses a library under which calculation fields
// would read each other in a ring: a function's formula may read a field
// whose own formula calls the function, and no record of that table could
// then be computed. writes are the formulas the change rewrites, read in
// place of the stored ones.
func (s *Service) checkCalculationRings(ctx context.Context, lib *calc.Library, writes []formulaWrite) error {
	d := s.driver.Dialect()
	q := fmt.Sprintf(`SELECT c.id, t.name, c.name, c.calculation_formula FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id
		WHERE c.field_type = %s AND c.calculation_formula IS NOT NULL`, d.Placeholder(1))
	rows, err := s.driver.DB().QueryContext(ctx, q, string(dbal.FieldTypeCalculation))
	if err != nil {
		return fmt.Errorf("failed reading the calculation fields: %w", err)
	}
	defer rows.Close()
	reads := map[string]map[string][]string{}
	for rows.Next() {
		var id, table, name, raw string
		if err := rows.Scan(&id, &table, &name, &raw); err != nil {
			return err
		}
		for _, w := range writes {
			if w.table == "sys_columns" && w.column == "calculation_formula" && w.id == id {
				raw = w.value
			}
		}
		expr, err := calc.ParseWith(data.ReadStoredCalculation(raw).Formula, calc.Options{Library: lib})
		if err != nil {
			continue // broken already: reported when it is computed
		}
		if reads[table] == nil {
			reads[table] = map[string][]string{}
		}
		reads[table][name] = expr.Fields()
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tables := make([]string, 0, len(reads))
	for table := range reads {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		if _, err := calc.Order(reads[table]); err != nil {
			return fmt.Errorf("%w: with this formula, the calculation fields of %s could not be computed: %s",
				ErrInvalidCustomFunction, table, err.Error())
		}
	}
	return nil
}

// renamePlan is what a change to a custom function does to the stored
// formulas: those it rewrites, those it would read differently (captured),
// and those its new parameters break.
type renamePlan struct {
	writes   []formulaWrite
	captured []CustomFunctionUsage
	why      string
	broken   []CustomFunctionUsage
}

// planRename reads every stored formula with before, the library as it is,
// and works out what the change of the function from (none when creating
// one) to to does to it: middle is the new library under the old name,
// after the new library. skipID is the function being changed, whose own
// formula is given with the change.
func (s *Service) planRename(ctx context.Context, before, middle, after *calc.Library, from, to, skipID string) (*renamePlan, error) {
	plan := &renamePlan{}
	writes, err := s.visitFormulas(ctx, func(site formulaSite, formula string, params []string) (string, bool) {
		if site.usage.Kind == "custom_function" && site.usage.ID == skipID {
			return formula, false
		}
		read := func(lib *calc.Library) (*calc.Expr, error) {
			return calc.ParseFunction(calc.CustomFunction{Formula: formula, Parameters: params}, calc.Options{Library: lib})
		}
		old, err := read(before)
		if err != nil {
			return formula, false // broken already: left as it is
		}
		// With the new parameters, the formula must still read, and call
		// the function where it did: a call without arguments to a
		// function given parameters would read as a field.
		next, err := read(middle)
		if err != nil || from != "" && next.CallCount(from) != old.CallCount(from) {
			plan.broken = appendUsage(plan.broken, site.usage)
			return formula, false
		}
		out, changed, err := calc.RenameFunction(formula, params, calc.Options{Library: middle}, after, from, to)
		if err != nil {
			plan.captured = appendUsage(plan.captured, site.usage)
			plan.why = err.Error()
			return formula, false
		}
		return out, changed
	})
	if err != nil {
		return nil, err
	}
	plan.writes = writes
	return plan, nil
}

// signatures is a library of the functions' names and parameters only, which
// is all reading a formula that calls them needs: it reads whatever their
// own formulas say.
func signatures(defs []calc.CustomFunction) *calc.Library {
	out := make([]calc.CustomFunction, len(defs))
	for i, d := range defs {
		out[i] = calc.CustomFunction{Name: d.Name, Parameters: d.Parameters, Formula: `""`}
	}
	lib, _ := calc.NewLibrary(out, calc.Options{})
	return lib
}

func appendUsage(list []CustomFunctionUsage, u CustomFunctionUsage) []CustomFunctionUsage {
	for _, have := range list {
		if have == u {
			return list
		}
	}
	return append(list, u)
}

// apply writes the rewritten formulas. Each row is locked and read again
// first: one changed since the plan read it (a layout or a script saved in
// the meantime) stops the change rather than being overwritten with a
// rewrite of what it held before.
func (p *renamePlan) apply(ctx context.Context, tx *sql.Tx, d dbal.Dialect) error {
	lock := " FOR UPDATE"
	if d.Engine() == dbal.EnginePostgres {
		lock = " FOR NO KEY UPDATE"
	}
	for _, w := range p.writes {
		var current sql.NullString
		q := fmt.Sprintf(`SELECT %s FROM %s WHERE id = %s%s`, w.column, w.table, d.Placeholder(1), lock)
		if err := tx.QueryRowContext(ctx, q, w.id).Scan(&current); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed reading %s %s: %w", w.table, w.id, err)
		}
		if current.String != w.old {
			return fmt.Errorf("%w: a formula calling the function was changed while it was being saved: save it again", ErrCustomFunctionChanged)
		}
		q = fmt.Sprintf(`UPDATE %s SET %s = %s WHERE id = %s`, w.table, w.column, d.Placeholder(1), d.Placeholder(2))
		if _, err := tx.ExecContext(ctx, q, w.value, w.id); err != nil {
			return fmt.Errorf("failed rewriting %s %s: %w", w.table, w.id, err)
		}
	}
	return nil
}

func (s *Service) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// formulaSite is where a stored formula is: what it belongs to.
type formulaSite struct {
	usage CustomFunctionUsage
}

// formulaWrite is a catalog value to write back once its formulas changed.
type formulaWrite struct {
	table, column, id string
	value             string
	// name is the custom function's, for a function's formula.
	name string
	// old is the value the formulas were read from. The rows are read
	// before the write transaction; one that has changed since is not
	// overwritten.
	old string
}

// visitFormulas calls visit with every stored formula of the solution, and
// the parameters it may read when it is a custom function's. visit returns
// the formula to store in its place and whether it changed; the values whose
// formulas changed are returned, to be written back.
func (s *Service) visitFormulas(ctx context.Context, visit func(site formulaSite, formula string, params []string) (string, bool)) ([]formulaWrite, error) {
	var writes []formulaWrite
	db := s.driver.DB()

	// Fields: calculations, auto-enter calculations, validation formulas.
	rows, err := db.QueryContext(ctx, `SELECT c.id, t.name, c.name, c.field_type, c.calculation_formula, c.default_value, c.validation_rules
		FROM sys_columns c JOIN sys_tables t ON t.id = c.table_id ORDER BY t.name, c.name`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the fields: %w", err)
	}
	type column struct {
		id, table, name, fieldType string
		calc, auto, rules          sql.NullString
	}
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.id, &c.table, &c.name, &c.fieldType, &c.calc, &c.auto, &c.rules); err != nil {
			rows.Close()
			return nil, err
		}
		columns = append(columns, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, c := range columns {
		name := c.table + "::" + c.name
		if c.fieldType == string(dbal.FieldTypeCalculation) && c.calc.Valid {
			site := formulaSite{CustomFunctionUsage{Kind: "calculation", ID: c.id, Name: name}}
			if out, ok := rewriteJSONKey(c.calc.String, "formula", true, func(f string) (string, bool) { return visit(site, f, nil) }); ok {
				writes = append(writes, formulaWrite{"sys_columns", "calculation_formula", c.id, out, "", c.calc.String})
			}
		}
		if c.auto.Valid {
			site := formulaSite{CustomFunctionUsage{Kind: "auto_enter", ID: c.id, Name: name}}
			if out, ok := rewriteJSONKey(c.auto.String, "calculated_formula", false, func(f string) (string, bool) { return visit(site, f, nil) }); ok {
				writes = append(writes, formulaWrite{"sys_columns", "default_value", c.id, out, "", c.auto.String})
			}
		}
		if c.rules.Valid {
			site := formulaSite{CustomFunctionUsage{Kind: "validation", ID: c.id, Name: name}}
			if out, ok := rewriteJSONKey(c.rules.String, "calculation", false, func(f string) (string, bool) { return visit(site, f, nil) }); ok {
				writes = append(writes, formulaWrite{"sys_columns", "validation_rules", c.id, out, "", c.rules.String})
			}
		}
	}

	// Custom functions.
	functions, err := s.ListCustomFunctions(ctx)
	if err != nil {
		return nil, err
	}
	for _, fn := range functions {
		site := formulaSite{CustomFunctionUsage{Kind: "custom_function", ID: fn.ID, Name: fn.Name}}
		if out, ok := visit(site, fn.Formula, fn.Parameters); ok {
			writes = append(writes, formulaWrite{data.CustomFunctionsTable, "formula", fn.ID, out, fn.Name, fn.Formula})
		}
	}

	// Script steps.
	rows, err = db.QueryContext(ctx, `SELECT st.id, sc.name, st.step_type, st.params FROM sys_script_steps st
		JOIN sys_scripts sc ON sc.id = st.script_id ORDER BY sc.name, st.sequence_idx`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the script steps: %w", err)
	}
	type step struct{ id, script, stepType, params string }
	var steps []step
	for rows.Next() {
		var st step
		if err := rows.Scan(&st.id, &st.script, &st.stepType, &st.params); err != nil {
			rows.Close()
			return nil, err
		}
		steps = append(steps, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, st := range steps {
		site := formulaSite{CustomFunctionUsage{Kind: "script_step", ID: st.id, Name: st.script, Detail: st.stepType}}
		if out, ok := rewriteJSONFormulas(st.params, true, func(f string) (string, bool) { return visit(site, f, nil) }); ok {
			writes = append(writes, formulaWrite{"sys_script_steps", "params", st.id, out, "", st.params})
		}
	}

	// Layouts: the parameters of buttons and triggers, and the steps of
	// single-step buttons.
	rows, err = db.QueryContext(ctx, `SELECT id, name, definition FROM sys_layouts ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the layouts: %w", err)
	}
	type layout struct{ id, name, definition string }
	var layouts []layout
	for rows.Next() {
		var l layout
		if err := rows.Scan(&l.id, &l.name, &l.definition); err != nil {
			rows.Close()
			return nil, err
		}
		layouts = append(layouts, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, l := range layouts {
		site := formulaSite{CustomFunctionUsage{Kind: "layout", ID: l.id, Name: l.name}}
		if out, ok := rewriteJSONFormulas(l.definition, false, func(f string) (string, bool) { return visit(site, f, nil) }); ok {
			writes = append(writes, formulaWrite{"sys_layouts", "definition", l.id, out, "", l.definition})
		}
	}
	return writes, nil
}

// rewriteJSONKey rewrites the formula a JSON object holds under key. A value
// that is not a JSON object is the formula itself when plain is set (a
// calculation saved before formulas were stored as JSON).
func rewriteJSONKey(raw, key string, plain bool, rewrite func(string) (string, bool)) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "null" {
		return raw, false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		if !plain {
			return raw, false
		}
		return rewrite(raw)
	}
	formula, ok := obj[key].(string)
	if !ok || strings.TrimSpace(formula) == "" {
		return raw, false
	}
	out, changed := rewrite(formula)
	if !changed {
		return raw, false
	}
	obj[key] = out
	encoded, err := marshalPlain(obj)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

// scriptFormulaKeys are the parameters of a script step that hold a formula.
// Any other parameter holds one when its "<name>_is_calculation" is true,
// and none of them does when it is false.
var scriptFormulaKeys = map[string]bool{
	"expression": true, "condition": true, "calculation": true, "calc": true, "formula": true,
	"result": true, "value": true, "parameter": true, "script_parameter": true,
	"layout_calculation": true, "record_calculation": true,
}

// layoutFormulaKeys are the keys of a layout definition that hold a formula
// anywhere in it: a button's or a trigger's script parameter. The params of
// a single-step button are a script step's.
var layoutFormulaKeys = map[string]bool{"parameter": true, "script_parameter": true}

// rewriteJSONFormulas rewrites every formula a JSON document holds: the
// parameters of a script step (step set), or a layout definition.
func rewriteJSONFormulas(raw string, step bool, rewrite func(string) (string, bool)) (string, bool) {
	var doc any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return raw, false
	}
	changed := false
	var walk func(v any, inStep bool) any
	walk = func(v any, inStep bool) any {
		switch t := v.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				child := t[k]
				if text, ok := child.(string); ok {
					// An option that says whether it is a calculation is
					// believed: Set Field's value may be literal text.
					formula := layoutFormulaKeys[k] || inStep && scriptFormulaKeys[k]
					if flag, ok := t[k+"_is_calculation"]; ok && (inStep || formula) {
						formula = flag == true
					}
					if formula && strings.TrimSpace(text) != "" {
						if out, ok := rewrite(text); ok && out != text {
							t[k] = out
							changed = true
						}
					}
					continue
				}
				t[k] = walk(child, inStep || k == "params")
			}
			return t
		case []any:
			for i := range t {
				t[i] = walk(t[i], inStep)
			}
			return t
		}
		return v
	}
	doc = walk(doc, step)
	if !changed {
		return raw, false
	}
	encoded, err := marshalPlain(doc)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

// marshalPlain writes JSON with <, > and & as they are, as the formulas were
// written.
func marshalPlain(v any) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
