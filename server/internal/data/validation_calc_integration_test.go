package data_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCalcSolution is a solution of its own, in a database created for the
// test, with a table of orders and the custom function MaxQuantity.
func newCalcSolution(t *testing.T) (context.Context, *schema.Service, *data.Service, string) {
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
	name := fmt.Sprintf("f4b_valcalc_%d", uniqueTestStamp())
	require.NoError(t, mgr.CreateDatabase(ctx, name))
	t.Cleanup(func() { _ = mgr.DropDatabase(context.Background(), name) })
	driver, err := mgr.DriverFor(ctx, name)
	require.NoError(t, err)
	svc := schema.NewService(driver)
	require.NoError(t, svc.EnsureSystemTables(ctx))
	_, err = svc.CreateCustomFunction(ctx, schema.CustomFunctionInput{Name: "MaxQuantity", Parameters: []string{"price"},
		Formula: "If ( price > 100 ; 5 ; 50 )"})
	require.NoError(t, err)
	tbl, err := svc.CreateTable(ctx, "orders", "Orders")
	require.NoError(t, err)
	quantity := `{"calculation_enabled":true,"calculation":"Self <= MaxQuantity ( price )","custom_message_enabled":true,"custom_message":"Too many for this price"}`
	code := `{"calculation_enabled":true,"calculation":"Length ( Self ) = 3","allow_override":true}`
	audited := `{"calculation_enabled":true,"calculation":"price < 1000","calculation_always":true,"not_empty_timing":"entry"}`
	for _, col := range []schema.ColumnMetadata{
		{Name: "price", FieldType: dbal.FieldTypeNumber},
		{Name: "quantity", FieldType: dbal.FieldTypeNumber, ValidationRules: &quantity},
		{Name: "code", FieldType: dbal.FieldTypeText, ValidationRules: &code},
		{Name: "audited", FieldType: dbal.FieldTypeText, ValidationRules: &audited},
	} {
		col.DisplayName, col.IsNullable = col.Name, true
		_, err := svc.AddColumn(ctx, tbl.ID, col)
		require.NoError(t, err)
	}
	return ctx, svc, data.NewService(driver), string(driver.Dialect().Engine())
}

func validationError(t *testing.T, err error) *validation.Error {
	t.Helper()
	var ve *validation.Error
	require.True(t, errors.As(err, &ve), "want a validation error, got %v", err)
	return ve
}

// FileMaker's "Validated by calculation": the formula reads the record as
// it is about to be saved, Self being the field's value, may call the
// solution's custom functions, and is applied when the field is modified.
func TestValidationByCalculation(t *testing.T) {
	ctx, _, svc, _ := newCalcSolution(t)

	_, err := svc.InsertRow(ctx, "orders", map[string]interface{}{"price": 200, "quantity": 10})
	ve := validationError(t, err)
	assert.Equal(t, "quantity", ve.Field)
	assert.Equal(t, "calculation", ve.Rule)
	assert.Equal(t, "Too many for this price", ve.Message)
	assert.False(t, ve.Overridable)

	row, err := svc.InsertRow(ctx, "orders", map[string]interface{}{"price": 200, "quantity": 3})
	require.NoError(t, err, "a new record's empty fields were not modified")
	id := fmt.Sprint(row["id"])

	// A write that leaves the quantity alone does not validate it; one
	// that changes it reads the price as stored.
	_, err = svc.UpdateRow(ctx, "orders", id, map[string]interface{}{"price": 50})
	require.NoError(t, err)
	_, err = svc.UpdateRow(ctx, "orders", id, map[string]interface{}{"quantity": 60})
	assert.Equal(t, "quantity", validationError(t, err).Field)
	_, err = svc.UpdateRow(ctx, "orders", id, map[string]interface{}{"quantity": 40})
	require.NoError(t, err)

	// A field validated on every save is, whatever the write changes.
	_, err = svc.UpdateRow(ctx, "orders", id, map[string]interface{}{"price": 2000, "quantity": 1})
	assert.Equal(t, "audited", validationError(t, err).Field)
}

// A rule that allows the person to override it says so, and a write sent
// again with the override is saved; the rules that do not allow it still
// apply.
func TestValidationOverride(t *testing.T) {
	ctx, _, svc, _ := newCalcSolution(t)
	_, err := svc.InsertRow(ctx, "orders", map[string]interface{}{"code": "ABCD"})
	ve := validationError(t, err)
	assert.Equal(t, "code", ve.Field)
	assert.True(t, ve.Overridable)

	row, err := svc.InsertRow(data.WithValidationOverride(ctx), "orders", map[string]interface{}{"code": "ABCD"})
	require.NoError(t, err)
	assert.Equal(t, "ABCD", row["code"])

	_, err = svc.InsertRow(data.WithValidationOverride(ctx), "orders", map[string]interface{}{"code": "ABCD", "price": 200, "quantity": 9})
	assert.Equal(t, "quantity", validationError(t, err).Field)
}

// A script step's formula is evaluated by the server with the script's
// variables and the Get answers only the client knows, and gives back the
// variables it sets.
func TestEvaluateWithScriptVariables(t *testing.T) {
	ctx, _, svc, _ := newCalcSolution(t)
	row, err := svc.InsertRow(ctx, "orders", map[string]interface{}{"price": 150})
	require.NoError(t, err)
	id := fmt.Sprint(row["id"])

	env := data.EvaluationEnv{
		Variables: map[string]any{"$count": "10", "$$Label": "007", "$ratio": 0.5},
		Get:       map[string]any{"ScriptParameter": "go", "LayoutName": "Orders"},
	}
	cases := map[string]string{
		`MaxQuantity ( price ) + $count`: "15",
		`$count > 9`:                     "1", // a number, not the text "10"
		`$$label`:                        "007",
		`Get ( ScriptParameter ) & "/" & Get ( LayoutName )`: "go/Orders",
		`$ratio * 4`: "2",
		`Let ( [ $total = price * 2 ; $$seen = 1 ] ; $total )`: "300",
	}
	for expr, want := range cases {
		ev, err := svc.EvaluateExpressionIn(ctx, "orders", id, expr, "Text", env)
		require.NoError(t, err, expr)
		assert.Equal(t, want, ev.Text, expr)
	}
	ev, err := svc.EvaluateExpressionIn(ctx, "orders", id, `Let ( [ $total = price * 2 ; $$seen = 1 ] ; $total )`, "Text", env)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"$total": "300", "$$seen": "1"}, ev.Variables)

	_, err = svc.EvaluateExpressionIn(ctx, "orders", id, `MaxQuantity ( 1 ; 2 )`, "Text", env)
	assert.ErrorIs(t, err, data.ErrInvalidExpression)
}

// A validation formula that reads an unstored calculation reads it as the
// edit makes it, not as the stored record did.
func TestValidationReadsUnstoredCalculationsOfTheEdit(t *testing.T) {
	ctx, schemaSvc, svc, _ := newCalcSolution(t)
	tables, err := schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	tableID := ""
	for _, tbl := range tables {
		if tbl.Name == "orders" {
			tableID = tbl.ID
		}
	}
	require.NotEmpty(t, tableID)
	twice := `{"formula":"units * 2","result_type":"Number","stored":false}`
	rules := `{"calculation_enabled":true,"calculation":"twice < 10"}`
	for _, col := range []schema.ColumnMetadata{
		{Name: "units", FieldType: dbal.FieldTypeNumber, ValidationRules: &rules},
		{Name: "twice", FieldType: dbal.FieldTypeCalculation, CalculationFormula: &twice},
	} {
		col.DisplayName, col.IsNullable = col.Name, true
		_, err := schemaSvc.AddColumn(ctx, tableID, col)
		require.NoError(t, err)
	}
	row, err := svc.InsertRow(ctx, "orders", map[string]interface{}{"units": 2})
	require.NoError(t, err)
	id := fmt.Sprint(row["id"])
	_, err = svc.UpdateRow(ctx, "orders", id, map[string]interface{}{"units": 6})
	assert.Equal(t, "units", validationError(t, err).Field, "twice is 12 once units is 6")
	_, err = svc.UpdateRow(ctx, "orders", id, map[string]interface{}{"units": 4})
	require.NoError(t, err)
}

// A record being edited and not committed yet is evaluated with its own
// values, and the calculations they change, rather than the stored ones.
func TestEvaluateReadsTheValuesOfTheEdit(t *testing.T) {
	ctx, _, svc, _ := newCalcSolution(t)
	row, err := svc.InsertRow(ctx, "orders", map[string]interface{}{"price": 50})
	require.NoError(t, err)
	id := fmt.Sprint(row["id"])

	env := data.EvaluationEnv{Values: map[string]any{"price": "150", "id": "other"}}
	ev, err := svc.EvaluateExpressionIn(ctx, "orders", id, `MaxQuantity ( price )`, "Text", env)
	require.NoError(t, err)
	assert.Equal(t, "5", ev.Text)
	// A new record has no stored values: only the edit's.
	ev, err = svc.EvaluateExpressionIn(ctx, "orders", "", `MaxQuantity ( price ) & "/" & code`, "Text",
		data.EvaluationEnv{Values: map[string]any{"price": "500", "code": "ABC"}})
	require.NoError(t, err)
	assert.Equal(t, "5/ABC", ev.Text)
}
