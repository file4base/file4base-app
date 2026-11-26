package data_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
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

// A FileMaker "Value is" condition is migrated as the calculation FileMaker
// keeps for it, its Self written as the object's field through the layout's
// occurrence: from that occurrence it reads the record shown and compares
// as the field's type does, a number as a number, a date as a date and a
// text in FileMaker's collation.
func TestEvaluateExpressions_ValueConditionsCompareAsTheFieldsType(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "xevalvalue", "xevalvalue_unused")
	visits, _, _ := a.table(t, ctx, "visits", [2]string{"code", "TEXT"}, [2]string{"score", "NUMBER"}, [2]string{"due", "DATE"})
	_, err := a.schema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{BaseTableID: visits, Name: "VIS__Visits"})
	require.NoError(t, err)
	svc := data.NewService(a.driver)
	// Another record first, so reading the first record of the occurrence
	// instead of the one shown would answer otherwise.
	_, err = svc.InsertRow(ctx, "visits", map[string]interface{}{"code": "other", "score": 1, "due": "1900-01-01"})
	require.NoError(t, err)
	row, err := svc.InsertRow(ctx, "visits", map[string]interface{}{"code": "École ABC", "score": 10, "due": "2001-02-03"})
	require.NoError(t, err)
	id := fmt.Sprint(row["id"])

	conditions := map[string]string{
		"VIS__Visits::score>9":                                 "1", // 10 > 9 as numbers, not as texts
		"VIS__Visits::score≥1 and VIS__Visits::score≤10":       "1",
		"VIS__Visits::score=10":                                "1",
		"VIS__Visits::code=\"ecole abc\"":                      "1", // case and accents ignored
		"VIS__Visits::code≠\"ECOLE ABC\"":                      "0",
		"PatternCount(VIS__Visits::code; \"abc\") > 0":         "1",
		"Left(VIS__Visits::code; Length(\"éco\")) = \"éco\"":   "1",
		"VIS__Visits::due>Date(3;15;1990)":                     "1",
		"VIS__Visits::due≥Date(2;3;2001)":                      "1",
		"IsEmpty(VIS__Visits::due)":                            "0",
		"GetAsDate(VIS__Visits::due) < Get(CurrentDate)":       "1",
		"(Get(CurrentDate) - GetAsDate(VIS__Visits::due)) < 4": "0",
	}
	var expressions []string
	for e := range conditions {
		expressions = append(expressions, e)
	}
	got, err := svc.EvaluateExpressions(ctx, "visits", "VIS__Visits", id, expressions, "Number")
	require.NoError(t, err)
	require.Len(t, got, len(expressions))
	for i, e := range expressions {
		require.NoError(t, got[i].Err, e)
		assert.Equal(t, conditions[e], got[i].Text, e)
	}
}

// A condition of a layout object reads Self as the field the object shows:
// a field of the record shown, one repetition of a repeating field, or a
// related field, each comparing as its type does. Without a field given,
// Self is unknown.
func TestEvaluateExpressions_SelfIsTheObjectsField(t *testing.T) {
	ctx, a, _ := twoSolutions(t, "xevalself", "xevalself_unused")
	visits, _, visitFields := a.table(t, ctx, "visits", [2]string{"code", "TEXT"}, [2]string{"score", "NUMBER"}, [2]string{"due", "DATE"})
	_, err := a.schema.AddColumn(ctx, visits, schema.ColumnMetadata{Name: "marks", DisplayName: "marks",
		FieldType: dbal.FieldTypeNumber, IsNullable: true, MaxRepetitions: 3})
	require.NoError(t, err)
	_, ordersOcc, orderFields := a.table(t, ctx, "orders", [2]string{"code", "TEXT"}, [2]string{"amount", "NUMBER"})
	anchor, err := a.schema.CreateTableOccurrence(ctx, schema.CreateOccurrenceInput{BaseTableID: visits, Name: "VIS__Visits"})
	require.NoError(t, err)
	a.relate(t, ctx, schema.CreateRelationshipInput{Name: "visit_orders",
		LeftOccurrenceID: anchor.ID, RightOccurrenceID: ordersOcc,
		Predicates: []schema.RelationshipPredicate{{LeftColumnID: visitFields["code"], Operator: "=", RightColumnID: orderFields["code"]}}})
	svc := data.NewService(a.driver)
	_, err = svc.InsertRow(ctx, "visits", map[string]interface{}{"code": "other", "score": 1, "due": "1900-01-01", "marks[2]": 1})
	require.NoError(t, err)
	row, err := svc.InsertRow(ctx, "visits", map[string]interface{}{"code": "École ABC", "score": 10, "due": "2001-02-03",
		"marks": 1, "marks[2]": 20})
	require.NoError(t, err)
	id := fmt.Sprint(row["id"])
	_, err = svc.InsertRow(ctx, "orders", map[string]interface{}{"code": "École ABC", "amount": 7})
	require.NoError(t, err)

	cases := []struct {
		expression string
		self       *data.SelfField
		want       string
	}{
		{"Self>9", &data.SelfField{Field: "score"}, "1"}, // 10 > 9 as numbers, not as texts
		{"Self≥1 and Self≤10", &data.SelfField{Occurrence: "VIS__Visits", Field: "score"}, "1"},
		{`Self="ecole abc"`, &data.SelfField{Field: "code"}, "1"}, // case and accents ignored
		{`PatternCount(Self; "abc") > 0 and "Self" = "self"`, &data.SelfField{Field: "code"}, "1"},
		{"Self>Date(3;15;1990)", &data.SelfField{Field: "due"}, "1"},
		{"GetAsDate(Self) < Get(CurrentDate)", &data.SelfField{Field: "due"}, "1"},
		{"IsEmpty(Self)", &data.SelfField{Field: "due"}, "0"},
		{"Self>9", &data.SelfField{Field: "marks"}, "0"},
		{"Self>9", &data.SelfField{Field: "marks", Repetition: 2}, "1"},
		{"IsEmpty(Self)", &data.SelfField{Field: "marks", Repetition: 3}, "1"},
		{"Self=7", &data.SelfField{Occurrence: "orders", Field: "amount"}, "1"},
		// Self is the field's value: a Let variable of the field's name does
		// not change it.
		{"Let(score = 0; Self > 5)", &data.SelfField{Field: "score"}, "1"},
	}
	expressions := make([]string, 0, len(cases)+1)
	self := make([]*data.SelfField, 0, len(cases))
	for _, c := range cases {
		expressions = append(expressions, c.expression)
		self = append(self, c.self)
	}
	expressions = append(expressions, "IsEmpty(Self)") // no field given
	got, err := svc.EvaluateExpressionsIn(ctx, "visits", "VIS__Visits", id, expressions, "Number", data.EvaluationEnv{Self: self})
	require.NoError(t, err)
	require.Len(t, got, len(expressions))
	for i, c := range cases {
		require.NoError(t, got[i].Err, "%s of %+v", c.expression, *c.self)
		assert.Equal(t, c.want, got[i].Text, "%s of %+v", c.expression, *c.self)
	}
	assert.True(t, errors.Is(got[len(cases)].Err, data.ErrInvalidExpression), "Self without a field is unknown")
}
