package data_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func summaryJSON(t *testing.T, kind data.SummaryType, field string) *string {
	t.Helper()
	encoded, err := data.SummarySpec{Type: kind, Field: field}.Encode()
	require.NoError(t, err)
	return &encoded
}

// The tutorial's Annual Fee Report (#32): fees subtotalled per customer type,
// with a grand total, over the found set.
func TestSummarize(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	dataSvc := data.NewService(driver)

	name := fmt.Sprintf("summary_customers_%d", time.Now().UnixNano()%1000000)
	tbl, err := schemaSvc.CreateTable(ctx, "Summary Customers", name)
	require.NoError(t, err)
	defer func() { _ = schemaSvc.DeleteTable(context.Background(), tbl.ID) }()

	add := func(field, label string, fieldType dbal.AgnosticFieldType, formula *string) {
		t.Helper()
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: field, DisplayName: label, FieldType: fieldType,
			IsNullable: true, CalculationFormula: formula,
		})
		require.NoError(t, err)
	}
	add("last_name", "Last Name", dbal.FieldTypeText, nil)
	add("customer_type", "Customer Type", dbal.FieldTypeText, nil)
	add("city", "City", dbal.FieldTypeText, nil)
	add("fee_paid", "Fee Paid", dbal.FieldTypeNumber, nil)
	// A calculation whose result is a number, which is what the tutorial's
	// Annual Fee is: it is stored in a number column, so it totals like one.
	annualFee := `{"formula":"IF(customer_type = \"Continuing\"; 100; 200)","result_type":"Number"}`
	add("annual_fee", "Annual Fee", dbal.FieldTypeCalculation, &annualFee)
	add("fee_total", "Total Fees", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryTotal, "fee_paid"))
	add("annual_total", "Total Annual Fees", dbal.FieldTypeSummary,
		summaryJSON(t, data.SummaryTotal, "annual_fee"))
	add("fee_average", "Average Fee", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryAverage, "fee_paid"))
	add("fee_count", "Customers", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryCount, "fee_paid"))
	add("fee_lowest", "Lowest Fee", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryMinimum, "fee_paid"))
	add("fee_highest", "Highest Fee", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryMaximum, "fee_paid"))
	add("fee_share", "Share of Fees", dbal.FieldTypeSummary, summaryJSON(t, data.SummaryFractionOfTotal, "fee_paid"))

	for _, c := range []struct {
		lastName, customerType, city string
		feePaid                      string
	}{
		{"Alvarez", "Continuing", "Madrid", "100"},
		{"Cannon", "New", "New York", "200"},
		{"Lee", "New", "Toronto", "200"},
		{"Murphy", "Continuing", "Dublin", "100"},
		{"Smith", "Continuing", "London", "100"},
		{"Wilson", "Continuing", "London", "100"},
	} {
		_, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"last_name": c.lastName, "customer_type": c.customerType,
			"city": c.city, "fee_paid": c.feePaid,
		})
		require.NoError(t, err)
	}

	t.Run("the grand totals are over every record", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{})
		require.NoError(t, err)

		assert.Equal(t, 6, result.Count)
		assert.Equal(t, "800", result.Grand["fee_total"])
		assert.Equal(t, "6", result.Grand["fee_count"])
		assert.Equal(t, "100", result.Grand["fee_lowest"])
		assert.Equal(t, "200", result.Grand["fee_highest"])

		// 800/6. Each engine answers to its own precision, and the figure is
		// passed on as the decimal text it gave rather than through a float,
		// so nothing is lost on the way to the client (#18).
		average, ok := result.Grand["fee_average"].(string)
		require.True(t, ok)
		asNumber, err := strconv.ParseFloat(average, 64)
		require.NoError(t, err, "the average came back as %q", average)
		assert.InDelta(t, 800.0/6.0, asNumber, 0.0001)
		assert.True(t, strings.HasPrefix(average, "133.3333"), "got %q", average)
	})

	t.Run("a report groups by its break field", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{
			Fields:  []string{"fee_total", "fee_count"},
			GroupBy: []string{"customer_type"},
		})
		require.NoError(t, err)

		require.Len(t, result.Groups, 2)
		// The groups come back in the order the break field sorts, so the
		// report reads top to bottom.
		assert.Equal(t, "Continuing", result.Groups[0].Values["customer_type"])
		assert.Equal(t, 4, result.Groups[0].Count)
		assert.Equal(t, "400", result.Groups[0].Summaries["fee_total"])

		assert.Equal(t, "New", result.Groups[1].Values["customer_type"])
		assert.Equal(t, 2, result.Groups[1].Count)
		assert.Equal(t, "400", result.Groups[1].Summaries["fee_total"])

		assert.Equal(t, "800", result.Grand["fee_total"], "and the grand total is still the whole set")
	})

	t.Run("a fraction of the total is a share, not a sum", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{
			Fields:  []string{"fee_share"},
			GroupBy: []string{"customer_type"},
		})
		require.NoError(t, err)

		assert.Equal(t, "1", result.Grand["fee_share"], "the whole set is all of it")
		assert.Equal(t, "0.5", result.Groups[0].Summaries["fee_share"])
		assert.Equal(t, "0.5", result.Groups[1].Summaries["fee_share"])
	})

	t.Run("the figures are over the found set, not the table", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{
			Requests: []data.FindRequest{
				{Criteria: []data.FindCriterion{{FieldName: "customer_type", Operator: "=", Value: "Continuing"}}},
			},
			Fields:  []string{"fee_total", "fee_count"},
			GroupBy: []string{"city"},
		})
		require.NoError(t, err)

		assert.Equal(t, 4, result.Count)
		assert.Equal(t, "400", result.Grand["fee_total"])
		assert.Equal(t, []string{"Dublin", "London", "Madrid"},
			[]string{
				fmt.Sprint(result.Groups[0].Values["city"]),
				fmt.Sprint(result.Groups[1].Values["city"]),
				fmt.Sprint(result.Groups[2].Values["city"]),
			}, "New York and Toronto were not in the found set")
		assert.Equal(t, "200", result.Groups[1].Summaries["fee_total"], "the two Londoners")
	})

	t.Run("an omitting request subtracts from the figures too", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{
			Requests: []data.FindRequest{
				{Criteria: []data.FindCriterion{{FieldName: "customer_type", Operator: "=", Value: "Continuing"}}},
				{Criteria: []data.FindCriterion{{FieldName: "city", Operator: "=", Value: "London"}}, Omit: true},
			},
			Fields: []string{"fee_total"},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, result.Count)
		assert.Equal(t, "200", result.Grand["fee_total"])
	})

	t.Run("grouping by more than one field nests the report", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{
			Fields:  []string{"fee_total"},
			GroupBy: []string{"customer_type", "city"},
		})
		require.NoError(t, err)
		require.Len(t, result.Groups, 5, "four cities among the Continuing, plus two New ones minus none")

		first := result.Groups[0]
		assert.Equal(t, "Continuing", first.Values["customer_type"])
		assert.Equal(t, "Dublin", first.Values["city"])
	})

	t.Run("a numeric calculation field can be totalled", func(t *testing.T) {
		result, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{
			Fields:  []string{"annual_total"},
			GroupBy: []string{"customer_type"},
		})
		require.NoError(t, err, "a calculation whose result is a number is stored as one")

		// Four Continuing at 100, two New at 200.
		assert.Equal(t, "800", result.Grand["annual_total"])
		assert.Equal(t, "400", result.Groups[0].Summaries["annual_total"])
		assert.Equal(t, "400", result.Groups[1].Summaries["annual_total"])
	})

	t.Run("a field that is not a summary is refused", func(t *testing.T) {
		_, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{Fields: []string{"fee_paid"}})
		assert.ErrorIs(t, err, data.ErrInvalidSummary)
	})

	t.Run("grouping by a field the table does not have is refused", func(t *testing.T) {
		_, err := dataSvc.Summarize(ctx, name, data.SummaryRequest{GroupBy: []string{"no_such_field"}})
		assert.ErrorIs(t, err, data.ErrUnknownField)
	})

	t.Run("a summary field has no value in a record", func(t *testing.T) {
		row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"last_name": "Ignored", "fee_paid": "50", "fee_total": "9999",
		})
		require.NoError(t, err)
		assert.Nil(t, row["fee_total"], "a value sent for a summary field is dropped, not stored")

		updated, err := dataSvc.UpdateRow(ctx, name, row["id"].(string), map[string]interface{}{"fee_total": "1234"})
		require.NoError(t, err, "an update of summary fields alone is an update of nothing")
		assert.Nil(t, updated["fee_total"])

		require.NoError(t, dataSvc.DeleteRow(ctx, name, row["id"].(string)))
	})
}
