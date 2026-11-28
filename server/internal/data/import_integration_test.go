package data_test

import (
	"context"
	"fmt"
	"testing"

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

	name := fmt.Sprintf("import_customers_%d", uniqueTestStamp())
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

// A value its field cannot hold is left out of its record, which is imported
// with the rest of the file; told to abort on error, the import writes none
// of it (#38, #231).
func TestImportLeavesOutAValueItsFieldCannotHold(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	source := parseCSV(t, "Surname,Paid\nDurand,100\nSmith,not a number\nLee,300\n")
	opts := data.ImportOptions{
		Action: data.ImportAdd,
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: "fee_paid"},
		},
		AbortOnError: true,
	}
	report, err := f.dataSvc.ImportRecords(ctx, f.table, source, opts)
	require.ErrorIs(t, err, data.ErrImport)
	require.ErrorIs(t, err, data.ErrImportProblems)
	assert.Contains(t, err.Error(), "row 2", "the error says which row")
	assert.Contains(t, err.Error(), "not a number")
	require.NotNil(t, report, "a refused import still answers what it would have left out")
	require.Len(t, report.Problems, 1)
	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	assert.Empty(t, rows, "the first row was fine, and it is not there either")

	opts.AbortOnError = false
	report, err = f.dataSvc.ImportRecords(ctx, f.table, source, opts)
	require.NoError(t, err)
	assert.Equal(t, 3, report.Added, "the record is imported without the value")
	assert.Equal(t, 0, report.SkippedErrors)
	assert.Equal(t, 1, report.FieldsSkipped)
	assert.Equal(t, []data.ImportProblem{{Row: 2, Field: "fee_paid", Rule: data.ProblemType,
		Value: "not a number", Message: "fee_paid is not a number"}}, report.Problems)

	rows, err = f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{SortBy: "last_name", SortAsc: true})
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "Smith", rows[2]["last_name"])
	assert.Nil(t, rows[2]["fee_paid"], "the field is left empty")
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

// A keyed update recomputes its formulas over the stored record merged with
// the mapped values, so a calculation reading a field the file does not carry
// keeps its answer rather than being computed from nothing (#92).
func TestImportUpdateMatchingKeepsCalculationsOfUntouchedFields(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	_, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{
		"last_name": "Durand", "city": "Paris", "customer_type": "Continuing",
	})
	require.NoError(t, err)

	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "100", fmt.Sprint(rows[0]["annual_fee"]), "a Continuing customer pays 100")

	report, err := f.dataSvc.ImportRecords(ctx, f.table,
		parseCSV(t, "Surname,Town\nDurand,Lyon\n"), data.ImportOptions{
			Action:      data.ImportUpdateMatching,
			MatchFields: []string{"last_name"},
			Mappings: []data.ImportMapping{
				{Column: 0, Field: "last_name"},
				{Column: 1, Field: "city"},
			},
		})
	require.NoError(t, err)
	assert.Equal(t, 1, report.Updated)

	rows, err = f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Lyon", rows[0]["city"], "the mapped column was written")
	assert.Equal(t, "Continuing", rows[0]["customer_type"], "the field no column was sent to is left alone")
	assert.Equal(t, "100", fmt.Sprint(rows[0]["annual_fee"]),
		"the formula read the stored customer type, not an empty one")
}

// A stored calculation can be a match field, as FileMaker offers one "for
// matching only": its value finds the record, and the formula, not the row,
// keeps giving it (#191). An unstored one has no value to find a record by.
func TestImportUpdateMatchingOnAStoredCalculation(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)

	for _, row := range []map[string]interface{}{
		{"last_name": "Durand", "city": "Paris", "customer_type": "Continuing"}, // pays 100
		{"last_name": "Smith", "city": "Boston", "customer_type": "New"},        // pays 200
	} {
		_, err := f.dataSvc.InsertRow(ctx, f.table, row)
		require.NoError(t, err)
	}
	opts := data.ImportOptions{
		Action:      data.ImportUpdateMatching,
		MatchFields: []string{"annual_fee"},
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "annual_fee"},
			{Column: 1, Field: "city"},
		},
		AddUnmatched: true,
	}
	report, err := f.dataSvc.ImportRecords(ctx, f.table, parseCSV(t, "Fee,Town\n200,Rome\n300,Oslo\n"), opts)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Updated)
	assert.Equal(t, 1, report.Added)
	assert.Equal(t, []string{"city"}, report.Fields, "the calculation matched on is not reported as written")

	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	byCity := map[string]map[string]interface{}{}
	for _, r := range rows {
		byCity[fmt.Sprint(r["city"])] = r
	}
	require.Contains(t, byCity, "Rome", "the record whose fee is 200 was found by it")
	assert.Equal(t, "Smith", byCity["Rome"]["last_name"])
	assert.Equal(t, "200", fmt.Sprint(byCity["Rome"]["annual_fee"]))
	assert.Equal(t, "Paris", byCity["Paris"]["city"], "the record whose fee is 100 is left alone")
	require.Contains(t, byCity, "Oslo", "a row matching nothing is added")
	assert.Equal(t, "200", fmt.Sprint(byCity["Oslo"]["annual_fee"]),
		"the formula gives the added record its fee, not the row's 300")

	t.Run("adding into the calculation is still refused", func(t *testing.T) {
		add := opts
		add.Action, add.MatchFields = data.ImportAdd, nil
		_, err := f.dataSvc.ImportRecords(ctx, f.table, parseCSV(t, "Fee,Town\n1,X\n"), add)
		assert.ErrorIs(t, err, data.ErrImport)
	})

	t.Run("an unstored calculation is refused as a match field", func(t *testing.T) {
		tables, err := f.schemaSvc.ListTables(ctx)
		require.NoError(t, err)
		tableID := ""
		for _, t2 := range tables {
			if t2.Name == f.table {
				tableID = t2.ID
			}
		}
		require.NotEmpty(t, tableID)
		twice := `{"formula":"fee_paid * 2","result_type":"Number","stored":false}`
		_, err = f.schemaSvc.AddColumn(ctx, tableID, schema.ColumnMetadata{
			Name: "fee_twice", DisplayName: "Fee Twice", FieldType: dbal.FieldTypeCalculation,
			IsNullable: true, CalculationFormula: &twice,
		})
		require.NoError(t, err)
		unstored := opts
		unstored.MatchFields = []string{"fee_twice"}
		unstored.Mappings = []data.ImportMapping{{Column: 0, Field: "fee_twice"}, {Column: 1, Field: "city"}}
		_, err = f.dataSvc.ImportRecords(ctx, f.table, parseCSV(t, "Fee,Town\n1,X\n"), unstored)
		require.ErrorIs(t, err, data.ErrImport)
		assert.Contains(t, err.Error(), "unstored calculation")
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

// setRules gives a field of the fixture validation rules.
func setRules(t *testing.T, ctx context.Context, f *importFixture, field, label, rules string) {
	t.Helper()
	tableID, columnID := importColumnID(t, ctx, f, field)
	_, err := f.schemaSvc.UpdateColumn(ctx, tableID, columnID, schema.UpdateColumnOptions{
		DisplayName: label, ValidationRules: &rules, UpdateValidation: true,
	})
	require.NoError(t, err)
}

// The validation rules apply to an import as they do to any other write
// (#15). A record failing not empty is skipped and the rest imported, as
// FileMaker imports it; told to abort on error, nothing is (#231).
func TestImportHonoursValidationRules(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	f := newImportFixture(t, ctx, driver)
	setRules(t, ctx, f, "last_name", "Last Name", `{"not_empty":true,"not_empty_timing":"always"}`)

	source := parseCSV(t, "Surname,Town\nDurand,Paris\n,Lyon\nSmith,Nantes\n")
	opts := data.ImportOptions{
		Action: data.ImportAdd,
		Mappings: []data.ImportMapping{
			{Column: 0, Field: "last_name"},
			{Column: 1, Field: "city"},
		},
		AbortOnError: true,
	}
	opts.ReturnRecordIDs = true
	refused, err := f.dataSvc.ImportRecords(ctx, f.table, source, opts)
	require.ErrorIs(t, err, data.ErrImportProblems)
	assert.Contains(t, err.Error(), "row 2")
	require.NotNil(t, refused)
	assert.Zero(t, refused.Added, "the rows before the problem were rolled back")
	assert.Empty(t, refused.RecordIDs)
	assert.Len(t, refused.Problems, 1)
	opts.ReturnRecordIDs = false
	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{})
	require.NoError(t, err)
	assert.Empty(t, rows, "nothing was imported")

	opts.AbortOnError = false
	report, err := f.dataSvc.ImportRecords(ctx, f.table, source, opts)
	require.NoError(t, err)
	assert.Equal(t, 2, report.Added)
	assert.Equal(t, 1, report.SkippedErrors)
	assert.Equal(t, 0, report.FieldsSkipped)
	require.Len(t, report.Problems, 1)
	assert.Equal(t, data.ImportProblem{Row: 2, Field: "last_name", Rule: "not_empty",
		Message: "last_name requires a value", RecordSkipped: true}, report.Problems[0])
	assert.ElementsMatch(t, []string{"Durand", "Smith"}, lastNames(listAll(t, ctx, f)))
}

// FileMaker checks unique, existing value, range, strict data type and
// maximum length as a value is entered, and imports the record without the
// value that breaks one; not empty and a validation by calculation are
// checked as the record is committed, and skip the whole record ("About
// validating data during import", #231).
func TestImportSkipsAsFileMakerValidates(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	mappings := []data.ImportMapping{{Column: 0, Field: "last_name"}, {Column: 1, Field: "city"}}

	t.Run("a value that is not unique is left out, its record imported", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "last_name", "Last Name", `{"unique":true,"not_empty_timing":"always"}`)
		_, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{"last_name": "Durand"})
		require.NoError(t, err)

		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Town\nMartin,Paris\nDurand,Lyon\nLee,Nantes\nLee,Brest\n"),
			data.ImportOptions{Action: data.ImportAdd, Mappings: mappings})
		require.NoError(t, err)
		assert.Equal(t, 4, report.Added)
		assert.Equal(t, 0, report.SkippedErrors)
		assert.Equal(t, 2, report.FieldsSkipped, "the existing Durand, and the second Lee of the file")
		require.Len(t, report.Problems, 2)
		assert.Equal(t, data.ImportProblem{Row: 2, Field: "last_name", Rule: "unique", Value: "Durand",
			Message: "another record already has this last_name"}, report.Problems[0])
		assert.Equal(t, 4, report.Problems[1].Row)

		byCity := map[string]interface{}{}
		for _, row := range listAll(t, ctx, f) {
			byCity[fmt.Sprint(row["city"])] = row["last_name"]
		}
		assert.Nil(t, byCity["Lyon"], "imported, without the surname")
		assert.Nil(t, byCity["Brest"])
		assert.Equal(t, "Lee", byCity["Nantes"])
	})

	t.Run("a value left out of a field that must not be empty skips the record", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "last_name", "Last Name", `{"unique":true,"not_empty":true,"not_empty_timing":"always"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Town\nLee,Paris\nLee,Lyon\nKim,Nantes\n"),
			data.ImportOptions{Action: data.ImportAdd, Mappings: mappings})
		require.NoError(t, err)
		assert.Equal(t, 2, report.Added)
		assert.Equal(t, 1, report.SkippedErrors)
		assert.Equal(t, 0, report.FieldsSkipped)
		require.Len(t, report.Problems, 1)
		problem := report.Problems[0]
		assert.Equal(t, 2, problem.Row)
		assert.True(t, problem.RecordSkipped)
		assert.Equal(t, "unique", problem.Rule, "the cause, not only its consequence")
		assert.Equal(t, "Lee", problem.Value)
		assert.Contains(t, problem.Message, "requires a value")
	})

	t.Run("a validation by calculation that fails skips the record", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "city", "City",
			`{"calculation_enabled":true,"calculation":"Self <> \"Atlantis\"","not_empty_timing":"always"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Town\nLee,Paris\nKim,Atlantis\n"),
			data.ImportOptions{Action: data.ImportAdd, Mappings: mappings})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Added)
		assert.Equal(t, 1, report.SkippedErrors)
		require.Len(t, report.Problems, 1)
		assert.Equal(t, data.ImportProblem{Row: 2, Field: "city", Rule: "calculation", Value: "Atlantis",
			Message: "city does not meet its validation formula", RecordSkipped: true}, report.Problems[0])
		assert.Equal(t, []string{"Lee"}, lastNames(listAll(t, ctx, f)))
	})

	t.Run("a value left out recomputes the calculations that read it", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "customer_type", "Customer Type",
			`{"max_length_enabled":true,"max_length":10,"not_empty_timing":"always"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Type\nLee,Continuing\nKim,Continuing for ever\n"),
			data.ImportOptions{Action: data.ImportAdd, Mappings: []data.ImportMapping{
				{Column: 0, Field: "last_name"}, {Column: 1, Field: "customer_type"}}})
		require.NoError(t, err)
		assert.Equal(t, 2, report.Added)
		assert.Equal(t, 1, report.FieldsSkipped)
		fees := map[string]string{}
		for _, row := range listAll(t, ctx, f) {
			fees[fmt.Sprint(row["last_name"])] = fmt.Sprint(row["annual_fee"])
		}
		assert.Equal(t, "100", fees["Lee"])
		assert.Equal(t, "200", fees["Kim"], "the fee of a customer without a type")
	})

	t.Run("rules validated only during data entry do not apply", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "city", "City", `{"max_length_enabled":true,"max_length":3,"not_empty_timing":"entry"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table, parseCSV(t, "Surname,Town\nLee,Paris\n"),
			data.ImportOptions{Action: data.ImportAdd, Mappings: mappings})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Added)
		assert.Empty(t, report.Problems)
	})

	t.Run("an update skips a record, leaves a value out, and keeps the fields it does not write", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		for _, name := range []string{"Lee", "Kim", "Roy"} {
			_, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{
				"last_name": name, "city": "Old", "customer_type": "New"})
			require.NoError(t, err)
		}
		// customer_type must not be empty: an update that does not write it
		// leaves it as it is, and is not refused for it.
		setRules(t, ctx, f, "customer_type", "Customer Type", `{"not_empty":true,"not_empty_timing":"always"}`)
		setRules(t, ctx, f, "city", "City",
			`{"calculation_enabled":true,"calculation":"Self <> \"Atlantis\"","not_empty_timing":"always"}`)
		setRules(t, ctx, f, "fee_paid", "Fee Paid",
			`{"range_enabled":true,"range_min":"0","range_max":"500","not_empty_timing":"always"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Town,Paid\nLee,Paris,100\nKim,Atlantis,100\nRoy,Lyon,900\n"),
			data.ImportOptions{Action: data.ImportUpdateMatching, MatchFields: []string{"last_name"},
				Mappings: []data.ImportMapping{
					{Column: 0, Field: "last_name"}, {Column: 1, Field: "city"}, {Column: 2, Field: "fee_paid"}}})
		require.NoError(t, err)
		assert.Equal(t, 2, report.Updated)
		assert.Equal(t, 1, report.SkippedErrors)
		assert.Equal(t, 1, report.FieldsSkipped)
		stored := map[string]map[string]interface{}{}
		for _, row := range listAll(t, ctx, f) {
			stored[fmt.Sprint(row["last_name"])] = row
		}
		assert.Equal(t, "Paris", stored["Lee"]["city"])
		assert.Equal(t, "Old", stored["Kim"]["city"], "the skipped record is as it was")
		assert.Equal(t, "Lyon", stored["Roy"]["city"])
		assert.Nil(t, stored["Roy"]["fee_paid"], "the value out of range is not written")
	})

	t.Run("a record skipped while replacing the found set keeps its place", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		var ids []string
		for _, name := range []string{"Lee", "Kim"} {
			row, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{"last_name": name, "city": "Old"})
			require.NoError(t, err)
			ids = append(ids, fmt.Sprint(row["id"]))
		}
		setRules(t, ctx, f, "city", "City",
			`{"calculation_enabled":true,"calculation":"Self <> \"Atlantis\"","not_empty_timing":"always"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table, parseCSV(t, "Town\nAtlantis\nParis\n"),
			data.ImportOptions{Action: data.ImportReplaceFoundSet, FoundSet: ids,
				Mappings: []data.ImportMapping{{Column: 0, Field: "city"}}})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Updated)
		assert.Equal(t, 1, report.SkippedErrors)
		stored := map[string]interface{}{}
		for _, row := range listAll(t, ctx, f) {
			stored[fmt.Sprint(row["last_name"])] = row["city"]
		}
		assert.Equal(t, "Old", stored["Lee"], "the first record is as it was")
		assert.Equal(t, "Paris", stored["Kim"], "and the second row still went to the second record")
	})

	t.Run("an auto-enter calculation reads the record without the value left out", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "city", "City", `{"max_length_enabled":true,"max_length":5,"not_empty_timing":"always"}`)
		tableID, columnID := importColumnID(t, ctx, f, "customer_type")
		_, err := f.schemaSvc.UpdateColumn(ctx, tableID, columnID, schema.UpdateColumnOptions{
			DisplayName: "Customer Type", UpdateDefaultValue: true,
			DefaultValue: options(map[string]any{"calculated_enabled": true, "calculated_formula": `"from " & city`,
				"calculated_skip_if_empty": false}),
		})
		require.NoError(t, err)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Town\nLee,Paris\nKim,Brussels\n"),
			data.ImportOptions{Action: data.ImportAdd, Mappings: mappings, PerformAutoEnter: true})
		require.NoError(t, err)
		assert.Equal(t, 2, report.Added)
		assert.Equal(t, 1, report.FieldsSkipped)
		types := map[string]string{}
		for _, row := range listAll(t, ctx, f) {
			types[fmt.Sprint(row["last_name"])] = fmt.Sprint(row["customer_type"])
		}
		assert.Equal(t, "from Paris", types["Lee"])
		assert.Equal(t, "from ", types["Kim"], "the value left out is not entered, so not read")
	})

	t.Run("a match field left out skips a row that would be added", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		setRules(t, ctx, f, "last_name", "Last Name", `{"max_length_enabled":true,"max_length":5,"not_empty_timing":"always"}`)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Town\nLee,Paris\nLongname,Lyon\n"),
			data.ImportOptions{Action: data.ImportUpdateMatching, MatchFields: []string{"last_name"},
				AddUnmatched: true, Mappings: mappings})
		require.NoError(t, err)
		assert.Equal(t, 1, report.Added, "no record is added without its key, to be added again next time")
		assert.Equal(t, 1, report.SkippedErrors)
		require.Len(t, report.Problems, 1)
		assert.Equal(t, "max_length", report.Problems[0].Rule)
		assert.True(t, report.Problems[0].RecordSkipped)
	})

	t.Run("a match field that cannot be read skips its row", func(t *testing.T) {
		f := newImportFixture(t, ctx, driver)
		_, err := f.dataSvc.InsertRow(ctx, f.table, map[string]interface{}{"last_name": "Lee", "fee_paid": "100"})
		require.NoError(t, err)
		report, err := f.dataSvc.ImportRecords(ctx, f.table,
			parseCSV(t, "Surname,Paid\nKim,one hundred\n"),
			data.ImportOptions{Action: data.ImportUpdateMatching, MatchFields: []string{"fee_paid"},
				AddUnmatched: true, Mappings: []data.ImportMapping{
					{Column: 0, Field: "last_name"}, {Column: 1, Field: "fee_paid"}}})
		require.NoError(t, err)
		assert.Equal(t, 0, report.Updated+report.Added)
		assert.Equal(t, 1, report.SkippedErrors)
		require.Len(t, report.Problems, 1)
		assert.True(t, report.Problems[0].RecordSkipped)
		assert.Equal(t, data.ProblemType, report.Problems[0].Rule)
		assert.Equal(t, []string{"Lee"}, lastNames(listAll(t, ctx, f)))
	})
}

func listAll(t *testing.T, ctx context.Context, f *importFixture) []map[string]interface{} {
	t.Helper()
	rows, err := f.dataSvc.ListRows(ctx, f.table, data.QueryOptions{Limit: 100})
	require.NoError(t, err)
	return rows
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
