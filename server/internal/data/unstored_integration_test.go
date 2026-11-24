package data_test

import (
	"fmt"
	"testing"

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

	t.Run("finding or sorting by it is refused", func(t *testing.T) {
		_, err := f.dataSvc.ListRows(ctx, f.companies, data.QueryOptions{SortBy: "total_fees"})
		require.ErrorIs(t, err, data.ErrUnstoredField)
		_, err = f.dataSvc.ExecuteFind(ctx, f.companies, []data.FindRequest{
			{Criteria: []data.FindCriterion{{FieldName: "total_fees", Value: ">100"}}},
		}, data.QueryOptions{})
		require.ErrorIs(t, err, data.ErrUnstoredField)
	})
}
