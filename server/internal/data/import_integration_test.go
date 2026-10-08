package data_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type importFixture struct {
	schemaSvc *schema.Service
	dataSvc   *data.Service
	table     string
}

func newImportFixture(t *testing.T, ctx context.Context, driver dbal.DatabaseDriver) *importFixture {
	t.Helper()
	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))

	name := fmt.Sprintf("import_customers_%d", time.Now().UnixNano()%1000000)
	tbl, err := schemaSvc.CreateTable(ctx, "Import Customers", name)
	require.NoError(t, err)
	t.Cleanup(func() { _ = schemaSvc.DeleteTable(context.Background(), tbl.ID) })

	add := func(field, label string, fieldType dbal.AgnosticFieldType, formula *string) {
		t.Helper()
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: field, DisplayName: label, FieldType: fieldType,
			IsNullable: true, CalculationFormula: formula,
		})
		require.NoError(t, err)
	}
	add("last_name", "Last Name", dbal.FieldTypeText, nil)
	add("city", "City", dbal.FieldTypeText, nil)
	add("customer_type", "Customer Type", dbal.FieldTypeText, nil)
	add("fee_paid", "Fee Paid", dbal.FieldTypeNumber, nil)
	add("date_paid", "Date Paid", dbal.FieldTypeDate, nil)

	annualFee := `{"formula":"IF(customer_type = \"Continuing\"; 100; 200)","result_type":"Number"}`
	add("annual_fee", "Annual Fee", dbal.FieldTypeCalculation, &annualFee)

	feeTotal := `{"summary_type":"total","field":"fee_paid"}`
	add("fee_total", "Total Fees", dbal.FieldTypeSummary, &feeTotal)

	return &importFixture{schemaSvc: schemaSvc, dataSvc: data.NewService(driver), table: name}
}

func parseCSV(t *testing.T, text string) *dataio.Table {
	t.Helper()
	table, err := dataio.Parse([]byte(text),
		dataio.Options{Format: dataio.FormatCSV, HasHeader: true, Delimiter: ","})
	require.NoError(t, err)
	return table
}

// Straight through: a CSV becomes records (#38).
func TestImportRecords(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	source := parseCSV(t, "Surname,Town,Type,Paid,When\n"+
		"Durand,Paris,Continuing,100,2011-03-01\n"+
		"Smith,New York,New,200,2011-04-15\n")

	report, err := f.dataSvc.ImportRecords(ctx, f.table, source, data.ImportOptions{
		Action: data.ImportAdd,
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: "city"},
			{Column: 2, Field: "customer_type"},
			{Column: 3, Field: "fee_paid"},
			{Column: 4, Field: "date_paid"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, report.Added)
	assert.Equal(t, 0, report.Skipped)

	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{Sort: []data.SortField{{Field: "last_name"}}})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	assert.Equal(t, "Durand", rows[0]["last_name"])
	assert.Equal(t, "Paris", rows[0]["city"])
	assert.Equal(t, "100", rows[0]["fee_paid"])
	assert.Contains(t, fmt.Sprint(rows[0]["date_paid"]), "2011-03-01")
	assert.Equal(t, "100", rows[0]["annual_fee"],
		"a calculation is worked out for an imported record too")
	assert.Equal(t, "200", rows[1]["annual_fee"])
	assert.NotEmpty(t, rows[0]["id"], "every imported record gets an id")
}

func TestImportSkipsUnmappedColumns(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	source := parseCSV(t, "Surname,Notes,Town\nDurand,ignore me,Paris\n")
	_, err := f.dataSvc.ImportRecords(ctx, f.table, source, data.ImportOptions{
		Action: data.ImportAdd,
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: ""}, // a column nobody wants
			{Column: 2, Field: "city"},
		},
	})
	require.NoError(t, err)

	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Durand", rows[0]["last_name"])
	assert.Equal(t, "Paris", rows[0]["city"])
}

// The whole file, or none of it.
func TestImportIsOneTransaction(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	source := parseCSV(t, "Surname,Paid\nDurand,100\nSmith,not a number\nLee,300\n")
	_, err := f.dataSvc.ImportRecords(ctx, f.table, source, data.ImportOptions{
		Action: data.ImportAdd,
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: "fee_paid"},
		},
	})
	require.ErrorIs(t, err, data.ErrImport)
	assert.Contains(t, err.Error(), "row 2", "the error says which row")
	assert.Contains(t, err.Error(), "Paid", "and which column")
	assert.Contains(t, err.Error(), "not a number")

	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	assert.Empty(t, rows, "the first row was fine, and it is not there either")
}

func TestImportUpdateMatching(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	_, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{
		"last_name": "Durand", "city": "Paris", "fee_paid": "100",
	})
	require.NoError(t, err)

	source := parseCSV(t, "Surname,Town\nDurand,Lyon\nSmith,New York\n")
	opts := data.ImportOptions{
		Action:      data.ImportUpdateMatching,
		MatchFields: []string{"last_name"},
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: "city"},
		},
	}

	t.Run("a row that matches updates, and one that does not is passed over", func(t *testing.T) {
		report, err := f.dataSvc.ImportRecords(ctx, f.table, source, opts)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 1, report.Skipped)
		assert.Equal(t, 0, report.Added)

		rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, rows, 1, "nothing was added")
		assert.Equal(t, "Lyon", rows[0]["city"], "Durand moved")
		assert.Equal(t, "100", rows[0]["fee_paid"], "a field no column was sent to is left alone")
	})

	t.Run("with add unmatched, the rest are added", func(t *testing.T) {
		withAdd := opts
		withAdd.AddUnmatched = true
		report, err := f.dataSvc.ImportRecords(ctx, f.table, source, withAdd)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 1, report.Added)

		rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
		require.NoError(t, err)
		assert.Len(t, rows, 2)
	})

	t.Run("matching is case-insensitive, as a find is", func(t *testing.T) {
		upper := parseCSV(t, "Surname,Town\nDURAND,Nice\n")
		report, err := f.dataSvc.ImportRecords(ctx, f.table, upper, opts)
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
	})
}

func TestImportRefusesABadSetup(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)
	source := parseCSV(t, "a,b\n1,2\n")

	for name, opts := range map[string]data.ImportOptions{
		"no action": {
			Mappings: []data.ImportMapping{{Column: 0, Field: "last_name"}},
		},
		"no column sent anywhere": {
			Action:   data.ImportAdd,
			Mappings: []data.ImportMapping{{Column: 0, Field: ""}},
		},
		"a field the table does not have": {
			Action:   data.ImportAdd,
			Mappings: []data.ImportMapping{{Column: 0, Field: "no_such_field"}},
		},
		"a column the file does not have": {
			Action:   data.ImportAdd,
			Mappings: []data.ImportMapping{{Column: 9, Field: "last_name"}},
		},
		"two columns to one field": {
			Action: data.ImportAdd,
			Mappings: []data.ImportMapping{
				{Column: 0, Field: "last_name"},
				{Column: 1, Field: "last_name"},
			},
		},
		"a summary field, which has no value in a record": {
			Action:   data.ImportAdd,
			Mappings: []data.ImportMapping{{Column: 0, Field: "fee_total"}},
		},
		"a calculation field, whose formula owns its value": {
			Action:   data.ImportAdd,
			Mappings: []data.ImportMapping{{Column: 0, Field: "annual_fee"}},
		},
		"updating with nothing to match on": {
			Action:   data.ImportUpdateMatching,
			Mappings: []data.ImportMapping{{Column: 0, Field: "last_name"}},
		},
		"matching on a field no column was sent to": {
			Action:      data.ImportUpdateMatching,
			MatchFields: []string{"city"},
			Mappings:    []data.ImportMapping{{Column: 0, Field: "last_name"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.dataSvc.ImportRecords(ctx, f.table, source, opts)
			assert.ErrorIs(t, err, data.ErrImport)
		})
	}
}

// The validation rules apply to an import as they do to any other write (#15).
func TestImportHonoursValidationRules(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	tables, err := f.schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	var tableID, columnID string
	for _, t2 := range tables {
		if t2.Name != f.table {
			continue
		}
		tableID = t2.ID
		for _, c := range t2.Columns {
			if c.Name == "last_name" {
				columnID = c.ID
			}
		}
	}
	require.NotEmpty(t, columnID)

	rules := `{"not_empty":true,"not_empty_timing":"always"}`
	_, err = f.schemaSvc.UpdateColumn(ctx, tableID, columnID, schema.UpdateColumnOptions{
		DisplayName: "Last Name", ValidationRules: &rules, UpdateValidation: true,
	})
	require.NoError(t, err)

	source := parseCSV(t, "Surname,Town\nDurand,Paris\n,Lyon\n")
	_, err = f.dataSvc.ImportRecords(ctx, f.table, source, data.ImportOptions{
		Action: data.ImportAdd,
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: "city"},
		},
	})
	require.ErrorIs(t, err, data.ErrImport)
	assert.Contains(t, err.Error(), "row 2")

	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	assert.Empty(t, rows, "nothing was imported")
}

// Exporting is the other half: what comes out reads the way the fields read.
func TestExportRecords(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	for _, row := range []map[string]interface{}{
		{"last_name": "Durand", "city": "Paris", "customer_type": "Continuing",
			"fee_paid": "100", "date_paid": "2011-03-01"},
		{"last_name": "Smith", "city": "New York", "customer_type": "New",
			"fee_paid": "200", "date_paid": "2011-04-15"},
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.table, row)
		require.NoError(t, err)
	}

	t.Run("the chosen fields, in the chosen order, with their headings", func(t *testing.T) {
		table, err := f.dataSvc.ExportRecords(ctx, f.table, data.ExportOptions{
			Fields:   []string{"city", "last_name", "date_paid"},
			Headings: []string{"Town", "Surname", "When"},
			Sort:     []data.SortField{{Field: "last_name"}},
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"Town", "Surname", "When"}, table.Columns)
		assert.Equal(t, [][]string{
			{"Paris", "Durand", "2011-03-01"},
			{"New York", "Smith", "2011-04-15"},
		}, table.Rows, "a date comes out as a date, not as the timestamp the column holds")
	})

	t.Run("it exports the found set, not the table", func(t *testing.T) {
		table, err := f.dataSvc.ExportRecords(ctx, f.table, data.ExportOptions{
			Requests: []data.FindRequest{
				{Criteria: []data.FindCriterion{{FieldName: "city", Operator: "=", Value: "Paris"}}},
			},
			Fields: []string{"last_name"},
		})
		require.NoError(t, err)
		assert.Equal(t, [][]string{{"Durand"}}, table.Rows)
	})

	t.Run("without a field list it writes every field", func(t *testing.T) {
		table, err := f.dataSvc.ExportRecords(ctx, f.table, data.ExportOptions{})
		require.NoError(t, err)
		assert.Contains(t, table.Columns, "last_name")
		assert.Contains(t, table.Columns, "annual_fee")
		assert.Len(t, table.Rows, 2)
	})

	t.Run("a field the table does not have is refused", func(t *testing.T) {
		_, err := f.dataSvc.ExportRecords(ctx, f.table, data.ExportOptions{Fields: []string{"nope"}})
		assert.ErrorIs(t, err, data.ErrUnknownField)
	})
}

// What goes out comes back in.
func TestExportThenImportRoundTrip(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	_, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{
		"last_name": "Dûrand, Marie", "city": "Paris", "customer_type": "Continuing",
		"fee_paid": "100", "date_paid": "2011-03-01",
	})
	require.NoError(t, err)

	exported, err := f.dataSvc.ExportRecords(ctx, f.table, data.ExportOptions{
		Fields: []string{"last_name", "city", "customer_type", "fee_paid", "date_paid"},
	})
	require.NoError(t, err)

	for _, format := range []struct {
		name  string
		write func() ([]byte, error)
		opts  dataio.Options
	}{
		{"csv", func() ([]byte, error) { return dataio.WriteDelimited(exported, ',', true) },
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true}},
		{"xlsx", func() ([]byte, error) { return dataio.WriteXLSX(exported, "Customers") },
			dataio.Options{Format: dataio.FormatXLSX, HasHeader: true}},
	} {
		t.Run(format.name, func(t *testing.T) {
			raw, err := format.write()
			require.NoError(t, err)

			back, err := dataio.Parse(raw, format.opts)
			require.NoError(t, err)
			assert.Equal(t, exported.Columns, back.Columns)
			require.Len(t, back.Rows, 1)
			assert.Equal(t, "Dûrand, Marie", back.Rows[0][0],
				"a comma and an accent survive the round trip")
			assert.Equal(t, "2011-03-01", back.Rows[0][4])
		})
	}
}
