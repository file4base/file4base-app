package schema_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/autoenter"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// designSolution is a solution of its own, in a database created for the
// test, so that what a rename rewrites is only what the test stored.
type designSolution struct {
	ctx     context.Context
	svc     *schema.Service
	data    *data.Service
	driver  dbal.DatabaseDriver
	tableID string
	columns map[string]string // name -> id
}

func newDesignSolution(t *testing.T) *designSolution {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	if err != nil {
		testdb.Unavailable(t, "database not available:", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	if err := mgr.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
	}
	name := fmt.Sprintf("f4b_cfdesign_%d", uniqueTestStamp())
	require.NoError(t, mgr.CreateDatabase(ctx, name))
	t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
	driver, err := mgr.DriverFor(ctx, name)
	require.NoError(t, err)
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	tbl, err := svc.CreateTable(ctx, "invoices", "Invoices")
	require.NoError(t, err)
	s := &designSolution{ctx: ctx, svc: svc, data: data.NewService(driver), driver: driver, tableID: tbl.ID, columns: map[string]string{}}
	for _, col := range []schema.ColumnMetadata{
		{Name: "net", FieldType: dbal.FieldTypeNumber},
		{Name: "discount", FieldType: dbal.FieldTypeNumber},
		{Name: "label", FieldType: dbal.FieldTypeText},
	} {
		s.addColumn(t, col)
	}
	return s
}

func (s *designSolution) addColumn(t *testing.T, col schema.ColumnMetadata) {
	t.Helper()
	col.DisplayName, col.IsNullable = col.Name, true
	added, err := s.svc.AddColumn(s.ctx, s.tableID, col)
	require.NoError(t, err)
	s.columns[col.Name] = added.ID
}

func (s *designSolution) create(t *testing.T, name string, params []string, formula string) *schema.CustomFunction {
	t.Helper()
	fn, err := s.svc.CreateCustomFunction(s.ctx, schema.CustomFunctionInput{Name: name, Parameters: params, Formula: formula})
	require.NoError(t, err)
	return fn
}

func (s *designSolution) column(t *testing.T, name string) schema.ColumnMetadata {
	t.Helper()
	tables, err := s.svc.ListTables(s.ctx)
	require.NoError(t, err)
	for _, tbl := range tables {
		for _, c := range tbl.Columns {
			if c.ID == s.columns[name] {
				return c
			}
		}
	}
	t.Fatalf("no column %s", name)
	return schema.ColumnMetadata{}
}

func stringPtr(s string) *string { return &s }

// Creating, changing and deleting custom functions as Manage Custom
// Functions does: a function that does not read, that calls itself
// endlessly, that takes a built-in name or a name taken already is refused.
func TestCustomFunctionCRUD(t *testing.T) {
	s := newDesignSolution(t)
	rate := s.create(t, "VatRate", nil, "0.2")
	withVat := s.create(t, "WithVat", []string{"amount"}, "amount * ( 1 + VatRate )")
	assert.Equal(t, schema.CustomFunctionAccessAll, withVat.Access)

	for name, in := range map[string]schema.CustomFunctionInput{
		"no formula":       {Name: "Broken", Formula: "1 +"},
		"built-in name":    {Name: "Upper", Parameters: []string{"t"}, Formula: "t"},
		"taken name":       {Name: "vatrate", Formula: "1"},
		"endless":          {Name: "Forever", Parameters: []string{"n"}, Formula: "Forever ( n + 1 )"},
		"arity of a call":  {Name: "Twice", Parameters: []string{"n"}, Formula: "WithVat ( n ; 2 )"},
		"unknown function": {Name: "Other", Formula: "Missing ( 1 )"},
	} {
		_, err := s.svc.CreateCustomFunction(s.ctx, in)
		assert.ErrorIs(t, err, schema.ErrInvalidCustomFunction, name)
	}
	// Recursion a condition stops is a custom function like any other.
	s.create(t, "Countdown", []string{"n"}, "If ( n <= 0 ; 0 ; Countdown ( n - 1 ) )")

	got, err := s.svc.GetCustomFunction(s.ctx, rate.ID)
	require.NoError(t, err)
	assert.Equal(t, "0.2", got.Formula)
	updated, err := s.svc.UpdateCustomFunction(s.ctx, rate.ID, schema.CustomFunctionInput{
		Name: "VatRate", Formula: "0.055", Access: schema.CustomFunctionAccessFullAccess}, false)
	require.NoError(t, err)
	assert.Equal(t, schema.CustomFunctionAccessFullAccess, updated.Access)

	// The library the formulas use follows at once.
	expr := fmt.Sprintf(`WithVat ( 100 )`)
	ev, err := s.data.EvaluateExpression(s.ctx, "invoices", "", expr, "Number")
	require.NoError(t, err)
	assert.Equal(t, "105.5", ev.Text)

	_, err = s.svc.GetCustomFunction(s.ctx, "nope")
	assert.ErrorIs(t, err, schema.ErrCustomFunctionNotFound)
}

// A rename rewrites every stored formula that calls the function, as
// FileMaker, which refers to it by id, shows the new name everywhere:
// calculation, auto-enter and validation formulas, other custom functions,
// script steps, and the parameters of layout buttons and triggers. Text,
// comments, and the formulas that do not call it are left as they were.
func TestRenamingACustomFunctionRewritesTheFormulasCallingIt(t *testing.T) {
	s := newDesignSolution(t)
	_ = s.create(t, "discount_rate", nil, "5")
	fn := s.create(t, "NetOf", []string{"amount"}, "amount - discount_rate")
	caller := s.create(t, "Doubled", []string{"x"}, "NetOf ( x ) * 2")

	s.addColumn(t, schema.ColumnMetadata{Name: "total", FieldType: dbal.FieldTypeCalculation,
		CalculationFormula: stringPtr(`{"formula":"NetOf ( net ) // NetOf here is a comment","result_type":"Number"}`)})
	s.addColumn(t, schema.ColumnMetadata{Name: "untouched", FieldType: dbal.FieldTypeCalculation,
		CalculationFormula: stringPtr(`{"formula":"net * 2","result_type":"Number"}`)})
	auto := autoenter.Encode(autoenter.Options{CalculatedEnabled: true, CalculatedFormula: `"NetOf ( 1 ) is " & NetOf ( 1 )`})
	s.addColumn(t, schema.ColumnMetadata{Name: "note", FieldType: dbal.FieldTypeText, DefaultValue: &auto})
	rules := `{"calculation_enabled":true,"calculation":"NetOf ( Self ) >= 0"}`
	s.addColumn(t, schema.ColumnMetadata{Name: "checked", FieldType: dbal.FieldTypeNumber, ValidationRules: &rules})

	script, err := s.svc.CreateScript(s.ctx, "Totals", "invoices", nil, true, []schema.ScriptStepMetadata{
		{StepType: "set_variable", IsEnabled: true, Params: json.RawMessage(`{"variable":"$t","value":"NetOf ( 10 )"}`)},
		{StepType: "show_dialog", IsEnabled: true, Params: json.RawMessage(`{"title":"NetOf ( 1 )","message":"NetOf ( 2 )","message_is_calculation":true}`)},
	})
	require.NoError(t, err)
	occurrences, err := s.svc.ListTableOccurrences(s.ctx)
	require.NoError(t, err)
	layout, err := s.svc.CreateLayout(s.ctx, "Invoice", occurrences[0].ID, json.RawMessage(`{"objects":[`+
		`{"type":"button","action":{"type":"perform_script","script_id":"`+script.ID+`","parameter":"NetOf ( net )"}},`+
		`{"type":"button","action":{"type":"single_step","step_type":"set_variable","params":{"variable":"$v","value":"NetOf ( 3 )"}}},`+
		`{"type":"text","text":"NetOf ( 4 )"}]}`))
	require.NoError(t, err)

	usages, err := s.svc.CustomFunctionUsages(s.ctx, fn.ID)
	require.NoError(t, err)
	kinds := map[string]int{}
	for _, u := range usages {
		kinds[u.Kind]++
	}
	assert.Equal(t, map[string]int{"calculation": 1, "auto_enter": 1, "validation": 1, "custom_function": 1, "script_step": 2, "layout": 1}, kinds)

	_, err = s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{
		Name: "AmountNet", Parameters: []string{"amount"}, Formula: "amount - discount_rate"}, false)
	require.NoError(t, err)

	calcFormula := data.ReadStoredCalculation(*s.column(t, "total").CalculationFormula).Formula
	assert.Equal(t, "AmountNet ( net ) // NetOf here is a comment", calcFormula)
	assert.Equal(t, `{"formula":"net * 2","result_type":"Number"}`, *s.column(t, "untouched").CalculationFormula)
	ae, err := autoenter.Parse(s.column(t, "note").DefaultValue)
	require.NoError(t, err)
	assert.Equal(t, `"NetOf ( 1 ) is " & AmountNet ( 1 )`, ae.CalculatedFormula)
	assert.Contains(t, *s.column(t, "checked").ValidationRules, `AmountNet ( Self ) >= 0`)
	doubled, err := s.svc.GetCustomFunction(s.ctx, caller.ID)
	require.NoError(t, err)
	assert.Equal(t, "AmountNet ( x ) * 2", doubled.Formula)

	stored, err := s.svc.GetScript(s.ctx, script.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"variable":"$t","value":"AmountNet ( 10 )"}`, string(stored.Steps[0].Params))
	// A title that is not a calculation is text, kept as written.
	assert.JSONEq(t, `{"title":"NetOf ( 1 )","message":"AmountNet ( 2 )","message_is_calculation":true}`, string(stored.Steps[1].Params))
	lay, err := s.svc.GetLayout(s.ctx, layout.ID)
	require.NoError(t, err)
	def := string(lay.Definition)
	assert.Contains(t, def, `"parameter":"AmountNet ( net )"`)
	assert.Contains(t, def, `"value":"AmountNet ( 3 )"`)
	assert.Contains(t, def, `"text":"NetOf ( 4 )"`)

	// And the formulas compute with the new name.
	row, err := s.data.InsertRow(s.ctx, "invoices", map[string]interface{}{"net": 12, "checked": 6})
	require.NoError(t, err)
	assert.EqualValues(t, "7", fmt.Sprint(row["total"]))
	assert.Equal(t, "NetOf ( 1 ) is -4", row["note"])
}

// A rename that would make a formula read a field as a call to the
// function, and new parameters that break the formulas calling it, are
// refused with the formulas listed; force accepts broken callers.
func TestCustomFunctionChangesThatBreakFormulasAreRefused(t *testing.T) {
	s := newDesignSolution(t)
	fn := s.create(t, "Rate", nil, "2")
	s.addColumn(t, schema.ColumnMetadata{Name: "scaled", FieldType: dbal.FieldTypeCalculation,
		CalculationFormula: stringPtr(`{"formula":"net * Rate - discount","result_type":"Number"}`)})

	// "discount" is a field read as a plain word: renaming Rate so would
	// turn it into a call.
	_, err := s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: "discount", Formula: "2"}, true)
	var conflict *schema.CustomFunctionConflict
	require.True(t, errors.As(err, &conflict), "got %v", err)
	require.Len(t, conflict.Usages, 1)
	assert.Equal(t, "invoices::scaled", conflict.Usages[0].Name)
	// Creating it is refused the same way.
	_, err = s.svc.CreateCustomFunction(s.ctx, schema.CustomFunctionInput{Name: "discount", Formula: "1"})
	require.True(t, errors.As(err, &conflict), "got %v", err)

	// A parameter more breaks the call without one.
	_, err = s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: "Rate", Parameters: []string{"x"}, Formula: "x"}, false)
	require.True(t, errors.As(err, &conflict), "got %v", err)
	assert.ErrorIs(t, err, schema.ErrCustomFunctionInUse)
	require.Len(t, conflict.Usages, 1)
	_, err = s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: "Rate", Parameters: []string{"x"}, Formula: "x"}, true)
	require.NoError(t, err)

	// Deleting a function formulas call is refused, unless forced.
	other := s.create(t, "Unit", nil, "1")
	s.addColumn(t, schema.ColumnMetadata{Name: "one", FieldType: dbal.FieldTypeCalculation,
		CalculationFormula: stringPtr(`{"formula":"Unit","result_type":"Number"}`)})
	err = s.svc.DeleteCustomFunction(s.ctx, other.ID, false)
	require.True(t, errors.As(err, &conflict), "got %v", err)
	require.NoError(t, s.svc.DeleteCustomFunction(s.ctx, other.ID, true))
	all, err := s.svc.ListCustomFunctions(s.ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

// The calculation editor checks a formula with the server: where it stops
// reading, and the fields a table does not have.
func TestCheckFormula(t *testing.T) {
	s := newDesignSolution(t)
	s.create(t, "Half", []string{"x"}, "x / 2")

	check, err := s.svc.CheckFormula(s.ctx, schema.FormulaCheckInput{Formula: "Half ( net ) + nothere", Table: "invoices"})
	require.NoError(t, err)
	assert.True(t, check.Valid)
	assert.Equal(t, []string{"nothere"}, check.UnknownFields)

	check, err = s.svc.CheckFormula(s.ctx, schema.FormulaCheckInput{Formula: "Half ( 1 ; 2 )"})
	require.NoError(t, err)
	assert.False(t, check.Valid)
	assert.Contains(t, check.Message, "takes 1 argument")
	require.NotNil(t, check.Position)
	assert.Equal(t, 0, *check.Position)

	// A function being written reads its parameters and calls itself with
	// the ones it is given.
	check, err = s.svc.CheckFormula(s.ctx, schema.FormulaCheckInput{Formula: "If ( n = 0 ; 1 ; n * Fact ( n - 1 ) )",
		Function: "Fact", Parameters: []string{"n"}})
	require.NoError(t, err)
	assert.True(t, check.Valid, check.Message)
	check, err = s.svc.CheckFormula(s.ctx, schema.FormulaCheckInput{Formula: "Loop ( n )", Function: "Loop", Parameters: []string{"n"}})
	require.NoError(t, err)
	assert.False(t, check.Valid)
	assert.True(t, strings.Contains(check.Message, "never end"), check.Message)
}

// A function changed so that it reads a calculation field whose formula
// calls it would leave the table with calculations no record can compute:
// it is refused, and so is a calculation field written into such a ring.
func TestCustomFunctionChangesThatMakeCalculationsReadThemselvesAreRefused(t *testing.T) {
	s := newDesignSolution(t)
	fn := s.create(t, "Bonus", nil, "1")
	s.addColumn(t, schema.ColumnMetadata{Name: "total", FieldType: dbal.FieldTypeCalculation,
		CalculationFormula: stringPtr(`{"formula":"net + Bonus","result_type":"Number"}`)})

	_, err := s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: "Bonus", Formula: "total + 1"}, true)
	assert.ErrorIs(t, err, schema.ErrInvalidCustomFunction)
	assert.Contains(t, err.Error(), "total")
	got, err := s.svc.GetCustomFunction(s.ctx, fn.ID)
	require.NoError(t, err)
	assert.Equal(t, "1", got.Formula, "nothing was saved")
	// Reading an ordinary field is fine.
	_, err = s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: "Bonus", Formula: "discount + 1"}, false)
	require.NoError(t, err)

	// The other way round: a function reading a field, then the field
	// written to call it.
	s.create(t, "Echo", nil, "echoed")
	_, err = s.svc.AddColumn(s.ctx, s.tableID, schema.ColumnMetadata{Name: "echoed", DisplayName: "echoed", IsNullable: true,
		FieldType: dbal.FieldTypeCalculation, CalculationFormula: stringPtr(`{"formula":"Echo + 1","result_type":"Number"}`)})
	assert.ErrorIs(t, err, schema.ErrInvalidFieldOptions)

	// Records are still saved and computed.
	row, err := s.data.InsertRow(s.ctx, "invoices", map[string]interface{}{"net": 10, "discount": 2})
	require.NoError(t, err)
	assert.EqualValues(t, "13", fmt.Sprint(row["total"]))
}

// A custom function a stored calculation calls, directly or through
// another function, cannot be changed to run a query with ExecuteSQL: the
// calculation would store "?" from then on. It is refused as saving that
// calculation is.
func TestCustomFunctionChangesThatMakeStoredCalculationsRunQueriesAreRefused(t *testing.T) {
	s := newDesignSolution(t)
	query := `ExecuteSQL ( "SELECT COUNT(*) FROM Invoices" ; "" ; "" )`
	_, err := s.svc.AddColumn(s.ctx, s.tableID, schema.ColumnMetadata{Name: "counted", DisplayName: "counted", IsNullable: true,
		FieldType: dbal.FieldTypeCalculation, CalculationFormula: stringPtr(fmt.Sprintf(`{"formula":%q,"result_type":"Number"}`, query))})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	saving := strings.TrimPrefix(err.Error(), schema.ErrInvalidFieldOptions.Error())

	direct := s.create(t, "Bonus", nil, "1")
	inner := s.create(t, "Inner", nil, "2")
	s.create(t, "Outer", nil, "Inner + 1")
	s.addColumn(t, schema.ColumnMetadata{Name: "total", FieldType: dbal.FieldTypeCalculation,
		CalculationFormula: stringPtr(`{"formula":"net + Bonus + Outer","result_type":"Number"}`)})
	want := strings.ReplaceAll(saving, `"counted"`, `"total"`)

	for _, fn := range []*schema.CustomFunction{direct, inner} {
		_, err = s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: fn.Name, Formula: query}, false)
		require.ErrorIs(t, err, schema.ErrInvalidCustomFunction, fn.Name)
		assert.Equal(t, want, strings.TrimPrefix(err.Error(), schema.ErrInvalidCustomFunction.Error()), fn.Name)
		got, err := s.svc.GetCustomFunction(s.ctx, fn.ID)
		require.NoError(t, err)
		assert.NotContains(t, got.Formula, "ExecuteSQL", "nothing was saved")
	}

	// Unstored, the calculation may.
	_, err = s.svc.UpdateColumn(s.ctx, s.tableID, s.columns["total"], schema.UpdateColumnOptions{DisplayName: "total",
		UpdateCalculation: true, CalculationFormula: stringPtr(`{"formula":"net + Bonus + Outer","result_type":"Number","stored":false}`)})
	require.NoError(t, err)
	_, err = s.svc.UpdateCustomFunction(s.ctx, inner.ID, schema.CustomFunctionInput{Name: "Inner", Formula: query}, false)
	require.NoError(t, err)
}

// Set Field's value is literal text when the step says it is not a
// calculation: a rename leaves it as written, and it is no usage.
func TestRenamingACustomFunctionLeavesLiteralValues(t *testing.T) {
	s := newDesignSolution(t)
	fn := s.create(t, "Fee", nil, "3")
	script, err := s.svc.CreateScript(s.ctx, "Fees", "invoices", nil, true, []schema.ScriptStepMetadata{
		{StepType: "set_field", IsEnabled: true, Params: json.RawMessage(`{"field":"label","value":"Fee","value_is_calculation":false}`)},
		{StepType: "set_field", IsEnabled: true, Params: json.RawMessage(`{"field":"net","value":"Fee * 2","value_is_calculation":true}`)},
		{StepType: "set_field", IsEnabled: true, Params: json.RawMessage(`{"field":"discount","value":"Fee"}`)},
	})
	require.NoError(t, err)
	usages, err := s.svc.CustomFunctionUsages(s.ctx, fn.ID)
	require.NoError(t, err)
	assert.Len(t, usages, 2)

	_, err = s.svc.UpdateCustomFunction(s.ctx, fn.ID, schema.CustomFunctionInput{Name: "Charge", Formula: "3"}, false)
	require.NoError(t, err)
	stored, err := s.svc.GetScript(s.ctx, script.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"field":"label","value":"Fee","value_is_calculation":false}`, string(stored.Steps[0].Params))
	assert.JSONEq(t, `{"field":"net","value":"Charge * 2","value_is_calculation":true}`, string(stored.Steps[1].Params))
	assert.JSONEq(t, `{"field":"discount","value":"Charge"}`, string(stored.Steps[2].Params))
}

// A value saved over a rule that allows override, unique among them, is
// restored from the data file as it was saved.
func TestRestoreKeepsValuesSavedOverAnOverridableRule(t *testing.T) {
	s := newDesignSolution(t)
	rules := `{"unique":true,"allow_override":true}`
	s.addColumn(t, schema.ColumnMetadata{Name: "ref", FieldType: dbal.FieldTypeText, ValidationRules: &rules})
	first, err := s.data.InsertRow(s.ctx, "invoices", map[string]interface{}{"ref": "A1"})
	require.NoError(t, err)
	_, err = s.data.InsertRow(s.ctx, "invoices", map[string]interface{}{"ref": "A1"})
	require.Error(t, err, "a duplicate is refused unless overridden")
	second, err := s.data.InsertRow(data.WithValidationOverride(s.ctx), "invoices", map[string]interface{}{"ref": "A1"})
	require.NoError(t, err)

	file, err := s.svc.ExportDatabaseData(s.ctx, "")
	require.NoError(t, err)
	for _, row := range []map[string]interface{}{first, second} {
		require.NoError(t, s.data.DeleteRow(s.ctx, "invoices", fmt.Sprint(row["id"])))
	}
	_, err = s.svc.ImportDatabaseData(s.ctx, file)
	require.NoError(t, err)
	rows, err := s.data.ListRows(s.ctx, "invoices", data.QueryOptions{})
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}
