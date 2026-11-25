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
