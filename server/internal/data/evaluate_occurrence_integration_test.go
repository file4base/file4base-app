package data_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A formula on a layout reads its related fields from the layout's
// occurrence, which need not be the table's own: an anchor–buoy layout is on
// an occurrence of its own, joined to what it shows.
func TestEvaluateExpressions_FromTheLayoutsOccurrence(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "xevalocc", "xevalocc_unused")
	customers, _, customerFields := a.table(t, ctx, "customers", [2]string{"code", "TEXT"})
	_, ordersOcc, orderFields := a.table(t, ctx, "orders", [2]string{"code", "TEXT"}, [2]string{"label", "TEXT"})
	anchor, err := a.schema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{BaseTableID: customers, Name: "CUS__Customers"})
	require.NoError(t, err)
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "anchor_orders",
		LeftOccurrenceID: anchor.ID, RightOccurrenceID: ordersOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: customerFields["code"], Operator: "=", RightColumnID: orderFields["code"]}}})
	svc := data.NewService(a.driver)
	row, err := svc.InsertRow(ctx, "customers", map[string]interface{}{"code": "c1"})
	require.NoError(t, err)
	id := fmt.Sprint(row["id"])
	_, err = svc.InsertRow(ctx, "orders", map[string]interface{}{"code": "c1", "label": "first order"})
	require.NoError(t, err)

	for _, occurrence := range []string{"CUS__Customers", anchor.ID} {
		got, err := svc.EvaluateExpressions(ctx, "customers", occurrence, id,
			[]string{"orders::label", "IsEmpty ( orders::label )", "1 +"}, "Text")
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.NoError(t, got[0].Err)
		assert.Equal(t, "first order", got[0].Text, occurrence)
		require.NoError(t, got[1].Err)
		assert.Equal(t, "0", got[1].Text)
		assert.True(t, errors.Is(got[2].Err, data.ErrInvalidExpression), "one expression failing leaves the others")
	}

	// From the table's own occurrence nothing joins orders.
	got, err := svc.EvaluateExpressions(ctx, "customers", "", id, []string{"orders::label"}, "Text")
	require.NoError(t, err)
	assert.Error(t, got[0].Err)

	_, err = svc.EvaluateExpressions(ctx, "customers", "", "no-such-record", []string{"1"}, "Text")
	assert.True(t, errors.Is(err, data.ErrRecordNotFound), "%v", err)
}

// A layout's batch of conditions, evaluated from its occurrence, knows the
// solution's custom functions and a script's variables, as a single
// evaluation does.
func TestEvaluateExpressionsIn_FromAnOccurrenceWithCustomFunctions(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "xevalcf", "xevalcf_unused")
	customers, _, customerFields := a.table(t, ctx, "customers", [2]string{"code", "TEXT"})
	_, ordersOcc, orderFields := a.table(t, ctx, "orders", [2]string{"code", "TEXT"}, [2]string{"label", "TEXT"})
	anchor, err := a.schema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{BaseTableID: customers, Name: "CUS__Customers"})
	require.NoError(t, err)
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "anchor_orders",
		LeftOccurrenceID: anchor.ID, RightOccurrenceID: ordersOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: customerFields["code"], Operator: "=", RightColumnID: orderFields["code"]}}})
	_, err = a.schema.CreateCustomFunction(ctx, schema.CustomFunctionInput{Name: "Twice", Parameters: []string{"x"},
		Formula: "x & x"})
	require.NoError(t, err)
	svc := data.NewService(a.driver)
	ids := map[string]string{}
	for _, code := range []string{"c1", "c2"} {
		row, err := svc.InsertRow(ctx, "customers", map[string]interface{}{"code": code})
		require.NoError(t, err)
		ids[code] = fmt.Sprint(row["id"])
		_, err = svc.InsertRow(ctx, "orders", map[string]interface{}{"code": code, "label": "o" + code})
		require.NoError(t, err)
	}

	env := data.EvaluationEnv{Variables: map[string]any{"$sep": "-"}}
	for code, id := range ids {
		got, err := svc.EvaluateExpressionsIn(ctx, "customers", "CUS__Customers", id,
			[]string{"Twice ( orders::label )", "Twice ( code ) & $sep", "Let ( $x = 1 ; Twice ( 2 ) )", "Twice ( 1 ; 2 )"}, "Text", env)
		require.NoError(t, err)
		require.Len(t, got, 4)
		require.NoError(t, got[0].Err)
		assert.Equal(t, "o"+code+"o"+code, got[0].Text, "a custom function reads a related field from the occurrence")
		require.NoError(t, got[1].Err)
		assert.Equal(t, code+code+"-", got[1].Text)
		require.NoError(t, got[2].Err)
		assert.Equal(t, "22", got[2].Text)
		assert.Equal(t, map[string]string{"$x": "1"}, got[2].Variables, "each expression reports the variables it set")
		assert.Nil(t, got[1].Variables, "and only those")
		assert.True(t, errors.Is(got[3].Err, data.ErrInvalidExpression), "a wrong number of arguments fails that expression only")
	}
}
