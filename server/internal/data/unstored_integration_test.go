package data_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/require"
)

func unstoredCalcJSON(formula, resultType string) *string {
	s := fmt.Sprintf(`{"formula":%q,"result_type":%q,"stored":false}`, formula, resultType)
	return &s
}

// An unstored calculation reads related records each time its record is
// read: adding a related record changes it without the record being written.
func TestUnstoredRelatedCalculation(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newRelatedFixture(t, ctx, driver, false, false, "")

	tables, err := f.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	tableID := map[string]string{}
	for _, tbl := range tables {
		tableID[tbl.Name] = tbl.ID
	}
	occurrences, err := f.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	var customersOcc string
	for _, o := range occurrences {
		if o.ID == f.customersOccurrence {
			customersOcc = o.Name
		}
	}
	require.NotEmpty(t, customersOcc)

	add := func(table, field string, kind dbal.AgnosticFieldType, formula *string) error {
		_, err := f.schemaSvc.AddColumn(ctx, tableID[table], schema.ColumnMetadata{
			Name: field, DisplayName: field, FieldType: kind, IsNullable: true, CalculationFormula: formula,
		})
		return err
	}
	require.NoError(t, add(f.customers, "fee", dbal.FieldTypeNumber, nil))
	require.NoError(t, add(f.companies, "total_fees", dbal.FieldTypeCalculation,
		unstoredCalcJSON(fmt.Sprintf("Sum ( %s::fee )", customersOcc), "Number")))
	require.NoError(t, add(f.companies, "headline", dbal.FieldTypeCalculation,
		unstoredCalcJSON(fmt.Sprintf(`company & ": " & %s::last_name`, customersOcc), "Text")))

	t.Run("a stored calculation may not read related records", func(t *testing.T) {
		err := add(f.companies, "stored_total", dbal.FieldTypeCalculation,
			calcJSON(fmt.Sprintf("Sum ( %s::fee )", customersOcc), "Number"))
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		require.Contains(t, err.Error(), "make it unstored")
	})
	t.Run("nor read an unstored calculation", func(t *testing.T) {
		err := add(f.companies, "double_total", dbal.FieldTypeCalculation, calcJSON("total_fees * 2", "Number"))
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})
	t.Run("an unstored one must name a field that exists", func(t *testing.T) {
		err := add(f.companies, "bad_total", dbal.FieldTypeCalculation,
			unstoredCalcJSON(fmt.Sprintf("Sum ( %s::no_such_field )", customersOcc), "Number"))
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	})

	bakery, err := f.dataSvc.InsertRow(ctx, f.companies, map[string]interface{}{"company": "Synthetic Bakery"})
	require.NoError(t, err)
	require.Nil(t, bakery["total_fees"], "an unstored calculation is not written")
	id := fmt.Sprint(bakery["id"])

	_, err = f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Synthetic Bakery", "last_name": "Alpha", "fee": 100})
	require.NoError(t, err)
	row, err := f.dataSvc.GetRow(ctx, f.companies, id)
	require.NoError(t, err)
	require.Equal(t, "100", fmt.Sprint(row["total_fees"]))
	require.Equal(t, "Synthetic Bakery: Alpha", row["headline"])

	_, err = f.dataSvc.InsertRow(ctx, f.customers, map[string]interface{}{"company": "Synthetic Bakery", "last_name": "Beta", "fee": 25.5})
	require.NoError(t, err)
	row, err = f.dataSvc.GetRow(ctx, f.companies, id)
	require.NoError(t, err)
	require.Equal(t, "125.5", fmt.Sprint(row["total_fees"]), "the new related record is counted without the company being written")

	var stored interface{}
	q := fmt.Sprintf("SELECT %s FROM %s", driver.Dialect().QuoteIdentifier("total_fees"), driver.Dialect().QuoteIdentifier(f.companies))
	require.NoError(t, driver.DB().QueryRowContext(ctx, q).Scan(&stored))
	require.Nil(t, stored, "the column stays empty")

	t.Run("every read computes it", func(t *testing.T) {
		rows, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "125.5", fmt.Sprint(rows[0]["total_fees"]))

		found, err := f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "company", Value: "Synthetic Bakery"}}},
		}, data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, found, 1)
		require.Equal(t, "125.5", fmt.Sprint(found[0]["total_fees"]))
	})

	t.Run("finding or sorting by it computes it (#149)", func(t *testing.T) {
		rows, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{SortBy: "total_fees"})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		found, err := f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "total_fees", Value: ">100"}}},
		}, data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, found, 1)
		found, err = f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "total_fees", Value: ">200"}}},
		}, data.QueryOptions{})
		require.NoError(t, err)
		require.Empty(t, found)
	})
}

// pair is two tables, left and right, each with a match field k of the given
// type, joined by left::k op right::k.
type pair struct {
	schemaSvc                   *schema.Service
	dataSvc                     *data.Service
	left, right                 string // physical table names
	leftID, rightID             string
	leftOccurrence, rightOccurr string // occurrence names
	relationship                string
	leftKey                     string // column id
}

func newPair(t *testing.T, ctx context.Context, driver dbal.DatabaseDriver, kind dbal.AgnosticFieldType, op string) *pair {
	t.Helper()
	p := &pair{schemaSvc: schema.NewService(driver), dataSvc: data.NewService(driver)}
	require.NoError(t, p.schemaSvc.EnsureSystemTables(ctx))
	stamp := uniqueTestStamp()
	p.left, p.right = fmt.Sprintf("pair_left_%d", stamp), fmt.Sprintf("pair_right_%d", stamp)
	keys := map[string]string{}
	for _, name := range []string{p.left, p.right} {
		tbl, err := p.schemaSvc.CreateTable(ctx, name, name)
		require.NoError(t, err)
		id := tbl.ID
		t.Cleanup(func() { _ = p.schemaSvc.DeleteTable(context.Background(), id) })
		col, err := p.schemaSvc.AddColumn(ctx, id, schema.ColumnMetadata{Name: "k", DisplayName: "k", FieldType: kind, IsNullable: true})
		require.NoError(t, err)
		keys[name] = col.ID
		if name == p.left {
			p.leftID = id
		} else {
			p.rightID = id
		}
	}
	p.leftKey = keys[p.left]
	occurrences, err := p.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, o := range occurrences {
		switch o.BaseTableID {
		case p.leftID:
			p.leftOccurrence, ids[p.left] = o.Name, o.ID
		case p.rightID:
			p.rightOccurr, ids[p.right] = o.Name, o.ID
		}
	}
	rel, err := p.schemaSvc.CreateRelationship(ctx, schema.CreateRelationshipInput{
		Name:             fmt.Sprintf("pair_%d", stamp),
		LeftOccurrenceID: ids[p.left], LeftColumnID: keys[p.left],
		RightOccurrenceID: ids[p.right], RightColumnID: keys[p.right],
		Operator: op,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.schemaSvc.DeleteRelationship(context.Background(), rel.ID) })
	p.relationship = rel.ID
	return p
}

func (p *pair) insert(t *testing.T, ctx context.Context, table string, record map[string]interface{}) string {
	t.Helper()
	row, err := p.dataSvc.InsertRow(ctx, table, record)
	require.NoError(t, err)
	return fmt.Sprint(row["id"])
}

func (p *pair) related(t *testing.T, ctx context.Context, from, id string) []string {
	t.Helper()
	rows, err := p.dataSvc.RelatedRows(ctx, from, id, p.relationship, "", data.QueryOptions{})
	require.NoError(t, err)
	var keys []string
	for _, row := range rows {
		keys = append(keys, fmt.Sprint(row["k"]))
	}
	return keys
}

// left::k < right::k relates a left record to the right records above it,
// read from either side, in a portal and in a calculation.
func TestInequalityRelationshipDirection(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	p := newPair(t, ctx, driver, dbal.FieldTypeNumber, "<")
	_, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "above", DisplayName: "above",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: unstoredCalcJSON(fmt.Sprintf("List ( %s::k )", p.rightOccurr), "Text")})
	require.NoError(t, err)

	five := p.insert(t, ctx, p.left, map[string]interface{}{"k": 5})
	three := p.insert(t, ctx, p.right, map[string]interface{}{"k": 3})
	seven := p.insert(t, ctx, p.right, map[string]interface{}{"k": 7})

	require.Equal(t, []string{"7"}, p.related(t, ctx, p.left, five))
	require.Empty(t, p.related(t, ctx, p.right, three))
	require.Equal(t, []string{"5"}, p.related(t, ctx, p.right, seven))

	row, err := p.dataSvc.GetRow(ctx, p.left, five)
	require.NoError(t, err)
	require.Equal(t, "7", row["above"])
}

// A text match field holding several lines matches a record holding any of
// them, as FileMaker's multi-key does.
func TestMultiKeyMatchField(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	p := newPair(t, ctx, driver, dbal.FieldTypeText, "=")
	_, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "matches", DisplayName: "matches",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: unstoredCalcJSON(fmt.Sprintf("Count ( %s::k )", p.rightOccurr), "Number")})
	require.NoError(t, err)

	multi := p.insert(t, ctx, p.left, map[string]interface{}{"k": "alpha\rbeta\n\ngamma"})
	for _, k := range []string{"alpha", "beta", "delta"} {
		p.insert(t, ctx, p.right, map[string]interface{}{"k": k})
	}
	require.ElementsMatch(t, []string{"alpha", "beta"}, p.related(t, ctx, p.left, multi))
	row, err := p.dataSvc.GetRow(ctx, p.left, multi)
	require.NoError(t, err)
	require.Equal(t, "2", fmt.Sprint(row["matches"]))
}

// Unstored fields of two tables that read each other: only the field read is
// computed, each record's field once, and a formula that reaches itself
// through the relationship stops instead of recursing.
func TestUnstoredCalculationsReadingEachOther(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	// left is the invoice, right its lines.
	p := newPair(t, ctx, driver, dbal.FieldTypeText, "=")
	add := func(tableID, name, formula, result string) {
		_, err := p.schemaSvc.AddColumn(ctx, tableID, schema.ColumnMetadata{Name: name, DisplayName: name,
			FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: unstoredCalcJSON(formula, result)})
		require.NoError(t, err)
	}
	for _, f := range []struct{ table, name string }{{p.leftID, "fee"}, {p.rightID, "amount"}} {
		_, err := p.schemaSvc.AddColumn(ctx, f.table, schema.ColumnMetadata{Name: f.name, DisplayName: f.name,
			FieldType: dbal.FieldTypeNumber, IsNullable: true})
		require.NoError(t, err)
	}
	add(p.rightID, "with_fee", fmt.Sprintf("amount + %s::fee", p.leftOccurrence), "Number")
	add(p.leftID, "total", fmt.Sprintf("Sum ( %s::with_fee )", p.rightOccurr), "Number")
	// A ring across the two tables, closed once both fields exist.
	echo, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "echo", DisplayName: "echo",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: unstoredCalcJSON("0", "Number")})
	require.NoError(t, err)
	add(p.rightID, "back", fmt.Sprintf("%s::echo + 1", p.leftOccurrence), "Number")
	_, err = p.schemaSvc.UpdateColumn(ctx, p.leftID, echo.ID, schema.UpdateColumnOptions{DisplayName: "echo",
		UpdateCalculation: true, CalculationFormula: unstoredCalcJSON(fmt.Sprintf("Sum ( %s::back )", p.rightOccurr), "Number")})
	require.NoError(t, err)

	invoice := p.insert(t, ctx, p.left, map[string]interface{}{"k": "INV-1", "fee": 2})
	for i := 1; i <= 30; i++ {
		p.insert(t, ctx, p.right, map[string]interface{}{"k": "INV-1", "amount": i})
	}

	start := time.Now()
	row, err := p.dataSvc.GetRow(ctx, p.left, invoice)
	require.NoError(t, err)
	require.Less(t, time.Since(start), 10*time.Second, "reading one invoice must not recompute the graph again and again")
	require.Equal(t, "525", fmt.Sprint(row["total"]), "sum of 1..30 plus a fee of 2 on each line")
	require.Contains(t, row, "echo") // computed, whatever a ring comes to, without hanging
	lines, err := p.dataSvc.RelatedRows(ctx, p.left, invoice, p.relationship, "", data.QueryOptions{SortBy: "amount", SortAsc: true})
	require.NoError(t, err)
	require.Len(t, lines, 30)
	require.Equal(t, "3", fmt.Sprint(lines[0]["with_fee"]), "a line's amount of 1 plus its invoice's fee")
}

// A related read is made as the caller: a table they may not read gives
// <No Access>, never its data.
func TestUnstoredRelatedReadRespectsAccess(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	p := newPair(t, ctx, driver, dbal.FieldTypeText, "=")
	_, err := p.schemaSvc.AddColumn(ctx, p.rightID, schema.ColumnMetadata{Name: "salary", DisplayName: "salary",
		FieldType: dbal.FieldTypeNumber, IsNullable: true})
	require.NoError(t, err)
	_, err = p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "pay", DisplayName: "pay",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: unstoredCalcJSON(fmt.Sprintf("Sum ( %s::salary )", p.rightOccurr), "Number")})
	require.NoError(t, err)
	employee := p.insert(t, ctx, p.left, map[string]interface{}{"k": "E-1"})
	p.insert(t, ctx, p.right, map[string]interface{}{"k": "E-1", "salary": 1000})

	row, err := p.dataSvc.GetRow(ctx, p.left, employee)
	require.NoError(t, err)
	require.Equal(t, "1000", fmt.Sprint(row["pay"]))

	restricted := data.NewService(driver).WithReadAccess(func(_ context.Context, table string) (bool, error) {
		return table != p.right, nil
	})
	row, err = restricted.GetRow(ctx, p.left, employee)
	require.NoError(t, err)
	require.Nil(t, row["pay"], "no value read from a table the caller may not read")

	_, err = restricted.EvaluateExpression(ctx, p.left, employee, fmt.Sprintf("Sum ( %s::salary )", p.rightOccurr), "Number")
	require.ErrorContains(t, err, "<No Access>")
}

// An unstored calculation's column holds nothing, so it can be neither
// required nor matched on by a relationship.
func TestUnstoredCalculationConstraints(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	p := newPair(t, ctx, driver, dbal.FieldTypeText, "=")
	_, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "required_calc", DisplayName: "required_calc",
		FieldType: dbal.FieldTypeCalculation, IsNullable: false, CalculationFormula: unstoredCalcJSON(`"x"`, "Text")})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)

	// A stored calculation a relationship matches on cannot become unstored.
	code, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "code", DisplayName: "code",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: calcJSON(`k & "-1"`, "Text")})
	require.NoError(t, err)
	occurrences, err := p.schemaSvc.ListTableOccurrences(ctx)
	require.NoError(t, err)
	var leftOcc, rightOcc string
	for _, o := range occurrences {
		switch o.BaseTableID {
		case p.leftID:
			leftOcc = o.ID
		case p.rightID:
			rightOcc = o.ID
		}
	}
	tables, err := p.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	var rightKey string
	for _, tbl := range tables {
		if tbl.ID == p.rightID {
			for _, c := range tbl.Columns {
				if c.Name == "k" {
					rightKey = c.ID
				}
			}
		}
	}
	rel, err := p.schemaSvc.CreateRelationship(ctx, schema.CreateRelationshipInput{Name: fmt.Sprintf("by_code_%d", uniqueTestStamp()),
		LeftOccurrenceID: leftOcc, LeftColumnID: code.ID, RightOccurrenceID: rightOcc, RightColumnID: rightKey, Operator: "="})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.schemaSvc.DeleteRelationship(context.Background(), rel.ID) })
	_, err = p.schemaSvc.UpdateColumn(ctx, p.leftID, code.ID, schema.UpdateColumnOptions{DisplayName: "code",
		UpdateCalculation: true, CalculationFormula: unstoredCalcJSON(`k & "-1"`, "Text")})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
	require.ErrorContains(t, err, "matches records on it")

	// Nor can a relationship be drawn on an unstored one.
	loose, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: "loose", DisplayName: "loose",
		FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: unstoredCalcJSON(`k`, "Text")})
	require.NoError(t, err)
	_, err = p.schemaSvc.CreateRelationship(ctx, schema.CreateRelationshipInput{Name: fmt.Sprintf("by_loose_%d", uniqueTestStamp()),
		LeftOccurrenceID: leftOcc, LeftColumnID: loose.ID, RightOccurrenceID: rightOcc, RightColumnID: rightKey, Operator: "="})
	require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
}
