package data_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
)

// Repeating fields, end to end against a real engine: repetition 1 is the
// field's own column, the others travel as "field[n]".
func TestRepeatingFields(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		testdb.Unavailable(t, "database not available:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		testdb.Unavailable(t, "database ping failed:", err)
		return
	}

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	dataSvc := data.NewService(driver)

	name := fmt.Sprintf("rep_orders_%d", uniqueTestStamp())
	tbl, err := schemaSvc.CreateTable(ctx, "Rep Orders", name)
	require.NoError(t, err)
	defer func() { _ = schemaSvc.DeleteTable(ctx, tbl.ID) }()

	add := func(field string, kind dbal.AgnosticFieldType, reps int, formula *string) *schema.ColumnMetadata {
		col, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: field, DisplayName: field, FieldType: kind, IsNullable: true,
			CalculationFormula: formula, MaxRepetitions: reps,
		})
		require.NoError(t, err)
		return col
	}
	add("item", dbal.FieldTypeText, 3, nil)
	qty := add("qty", dbal.FieldTypeNumber, 3, nil)
	add("due", dbal.FieldTypeDate, 2, nil)
	add("rate", dbal.FieldTypeNumber, 1, nil)
	add("total", dbal.FieldTypeCalculation, 1, calcJSON(`Sum ( qty ) & "/" & Count ( item ) & "/" & Last ( item )`, "Text"))
	add("line", dbal.FieldTypeCalculation, 3,
		calcJSON(`If ( not IsEmpty ( item ) ; Get ( CalculationRepetitionNumber ) & ":" & item & "x" & qty & "@" & Extend ( rate ) & rate )`, "Text"))
	add("second", dbal.FieldTypeCalculation, 1, calcJSON(`item[2] & "|" & GetRepetition ( qty ; 3 )`, "Text"))

	t.Run("the field options carry the maximum", func(t *testing.T) {
		require.Equal(t, 3, qty.MaxRepetitions)
		tables, err := schemaSvc.ListTables(ctx)
		require.NoError(t, err)
		for _, tb := range tables {
			if tb.ID != tbl.ID {
				continue
			}
			for _, c := range tb.Columns {
				if c.Name == "rate" || c.Name == "id" {
					require.Equal(t, 1, c.MaxRepetitions, c.Name)
				}
				if c.Name == "item" {
					require.Equal(t, 3, c.MaxRepetitions)
				}
			}
		}
	})

	var id string
	t.Run("repetitions are written, read and computed", func(t *testing.T) {
		row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
			"item": "bolt", "item[2]": "nut", "item[3]": "washer",
			"qty": 4, "qty[2]": "2.5", "due[2]": "2026-03-01", "rate": 7,
		})
		require.NoError(t, err)
		id = row["id"].(string)

		got, err := dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.Equal(t, "bolt", got["item"])
		require.Equal(t, "nut", got["item[2]"])
		require.Equal(t, "washer", got["item[3]"])
		require.Equal(t, "4", fmt.Sprint(got["qty"]))
		require.Equal(t, "2.5", fmt.Sprint(got["qty[2]"]))
		require.NotContains(t, got, "qty[3]", "an empty repetition is not returned")
		require.Nil(t, got["due"])
		due, ok := got["due[2]"].(time.Time)
		require.True(t, ok, "a date repetition reads as a date, got %T", got["due[2]"])
		require.Equal(t, "2026-03-01", due.Format("2006-01-02"))
		for key := range got {
			require.NotContains(t, key, "$", "the companion column is not a field")
			require.False(t, strings.HasSuffix(key, "[]"), key)
		}

		require.Equal(t, "6.5/3/washer", got["total"])
		require.Equal(t, "1:boltx4@77", got["line"])
		require.Equal(t, "2:nutx2.5@7", got["line[2]"], "Extend gives every repetition the value; the plain field only the first")
		require.Equal(t, "3:washerx@7", got["line[3]"])
		require.Equal(t, "nut|", got["second"])
	})

	t.Run("an update changes only the repetitions it names", func(t *testing.T) {
		got, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"qty[3]": 1, "item[2]": nil})
		require.NoError(t, err)
		require.Equal(t, "bolt", got["item"])
		require.NotContains(t, got, "item[2]")
		require.Equal(t, "washer", got["item[3]"])
		require.Equal(t, "2.5", fmt.Sprint(got["qty[2]"]))
		require.Equal(t, "1", fmt.Sprint(got["qty[3]"]))
		require.Equal(t, "7.5/2/washer", got["total"])
		require.NotContains(t, got, "line[2]", "the repetition of the calculation is empty again")
		require.Equal(t, "|1", got["second"])

		got, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"item[1]": "screw"})
		require.NoError(t, err)
		require.Equal(t, "screw", got["item"], `"field[1]" is the field itself`)
	})

	t.Run("a repetition past the maximum or of a field that does not repeat is refused", func(t *testing.T) {
		_, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"item[4]": "x"})
		require.ErrorIs(t, err, data.ErrUnknownField)
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"rate[2]": 1})
		require.ErrorIs(t, err, data.ErrUnknownField)
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"item[]": "x"})
		require.ErrorIs(t, err, data.ErrUnknownField)
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"qty[2]": "many"})
		require.ErrorIs(t, err, data.ErrInvalidValue)
	})

	t.Run("the primary key cannot be changed through a repetition key", func(t *testing.T) {
		got, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"id[1]": "another-id", "rate": 7})
		require.NoError(t, err)
		require.Equal(t, id, got["id"], `"id[1]" is the primary key, which an update does not change`)
		_, err = dataSvc.GetRow(ctx, name, "another-id")
		require.Error(t, err)
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"id[2]": "another-id"})
		require.ErrorIs(t, err, data.ErrUnknownField, "the primary key has no other repetition")
	})

	t.Run("export writes every repetition separated by the group separator", func(t *testing.T) {
		table, err := dataSvc.ExportRecords(ctx, name, data.ExportOptions{Fields: []string{"item", "qty", "item[3]"}})
		require.NoError(t, err)
		require.Equal(t, []string{"screw\x1d\x1dwasher", "4\x1d2.5\x1d1", "washer"}, table.Rows[0])

		first, err := dataSvc.ExportRecords(ctx, name, data.ExportOptions{Fields: []string{"item"}, FirstRepetitionOnly: true})
		require.NoError(t, err)
		require.Equal(t, []string{"screw"}, first.Rows[0])

		// Repetition 1 kept with its original text (#124) is written as that
		// text, and the other repetitions still follow it.
		require.NoError(t, sourcetext.Ensure(ctx, driver.DB()))
		require.NoError(t, sourcetext.Put(ctx, driver.DB(), driver.Dialect(), name, id, map[string]string{"qty": "4,0"}))
		defer func() {
			require.NoError(t, sourcetext.ClearFields(ctx, driver.DB(), driver.Dialect(), name, id, []string{"qty"}))
		}()
		kept, err := dataSvc.ExportRecords(ctx, name, data.ExportOptions{Fields: []string{"qty"}})
		require.NoError(t, err)
		require.Equal(t, []string{"4,0\x1d2.5\x1d1"}, kept.Rows[0])
		kept, err = dataSvc.ExportRecords(ctx, name, data.ExportOptions{Fields: []string{"qty"}, FirstRepetitionOnly: true})
		require.NoError(t, err)
		require.Equal(t, []string{"4,0"}, kept.Rows[0])
	})

	t.Run("import spreads separated values over the repetitions", func(t *testing.T) {
		src := &dataio.Table{Columns: []string{"Item", "Qty", "Rate"}, Rows: [][]string{{"pin\x1dclip", "1\x1d\x1d3", "2"}}}
		_, err := dataSvc.ImportRecords(ctx, name, src, data.ImportOptions{
			Action:   data.ImportAdd,
			Mappings: []data.ImportMapping{{Column: 0, Field: "item"}, {Column: 1, Field: "qty"}, {Column: 2, Field: "rate"}},
		})
		require.NoError(t, err)
		rows, err := dataSvc.ExecuteFind(ctx, name, []data.FindRequest{{Criteria: []data.FindCriterion{{FieldName: "item", Operator: "==", Value: "pin"}}}}, data.QueryOptions{})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "clip", rows[0]["item[2]"])
		require.Equal(t, "3", fmt.Sprint(rows[0]["qty[3]"]))
		require.Equal(t, "4/2/clip", rows[0]["total"])
	})

	t.Run("lowering the maximum hides repetitions, raising it shows them again", func(t *testing.T) {
		itemCol := findColumn(t, schemaSvc, ctx, tbl.ID, "item")
		_, err := schemaSvc.UpdateColumn(ctx, tbl.ID, itemCol.ID, schema.UpdateColumnOptions{
			DisplayName: "item", MaxRepetitions: 2, UpdateRepetitions: true})
		require.NoError(t, err)
		got, err := dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.NotContains(t, got, "item[3]")
		// Writing a repetition the field still has keeps the hidden ones.
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"item[2]": "nut"})
		require.NoError(t, err)

		_, err = schemaSvc.UpdateColumn(ctx, tbl.ID, itemCol.ID, schema.UpdateColumnOptions{
			DisplayName: "item", MaxRepetitions: 3, UpdateRepetitions: true})
		require.NoError(t, err)
		got, err = dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.Equal(t, "nut", got["item[2]"])
		require.Equal(t, "washer", got["item[3]"], "a repetition past a lowered maximum survives an update")
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"item[2]": nil})
		require.NoError(t, err)

		rateCol := findColumn(t, schemaSvc, ctx, tbl.ID, "rate")
		_, err = schemaSvc.UpdateColumn(ctx, tbl.ID, rateCol.ID, schema.UpdateColumnOptions{
			DisplayName: "rate", MaxRepetitions: 0, UpdateRepetitions: true})
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		_, err = schemaSvc.UpdateColumn(ctx, tbl.ID, rateCol.ID, schema.UpdateColumnOptions{
			DisplayName: "rate", MaxRepetitions: 32001, UpdateRepetitions: true})
		require.ErrorIs(t, err, schema.ErrInvalidFieldOptions)
		_, err = schemaSvc.UpdateColumn(ctx, tbl.ID, rateCol.ID, schema.UpdateColumnOptions{
			DisplayName: "rate", MaxRepetitions: 2, UpdateRepetitions: true})
		require.NoError(t, err)
		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"rate[2]": 9})
		require.NoError(t, err, "a field given repetitions accepts them")
	})

	t.Run("a data bundle keeps every repetition", func(t *testing.T) {
		// Repetitions a lowered maximum hides travel too, also when the
		// maximum is down to 1, and come back when it is raised.
		itemCol := findColumn(t, schemaSvc, ctx, tbl.ID, "item")
		rateCol := findColumn(t, schemaSvc, ctx, tbl.ID, "rate")
		setMax := func(col schema.ColumnMetadata, max int) {
			_, err := schemaSvc.UpdateColumn(ctx, tbl.ID, col.ID, schema.UpdateColumnOptions{
				DisplayName: col.Name, MaxRepetitions: max, UpdateRepetitions: true})
			require.NoError(t, err)
		}
		setMax(itemCol, 2)
		setMax(rateCol, 1)
		bundle, err := schemaSvc.ExportDatabaseData(ctx, "repetitions")
		require.NoError(t, err)
		require.NoError(t, dataSvc.DeleteRow(ctx, name, id))
		_, err = schemaSvc.ImportDatabaseData(ctx, bundle)
		require.NoError(t, err)
		got, err := dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.NotContains(t, got, "item[3]")
		require.NotContains(t, got, "rate[2]")
		setMax(itemCol, 3)
		setMax(rateCol, 2)
		got, err = dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.Equal(t, "washer", got["item[3]"])
		require.Equal(t, "1", fmt.Sprint(got["qty[3]"]))
		require.Equal(t, "9", fmt.Sprint(got["rate[2]"]))
		due, ok := got["due[2]"].(time.Time)
		require.True(t, ok)
		require.Equal(t, "2026-03-01", due.Format("2006-01-02"))
	})

	t.Run("a field with the longest name keeps its repetitions apart", func(t *testing.T) {
		long := "f" + strings.Repeat("x", 62) // 63 bytes: "long[]" would be cut back to "long"
		add(long, dbal.FieldTypeText, 2, nil)
		_, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{long: "one", long + "[2]": "two"})
		require.NoError(t, err)
		got, err := dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.Equal(t, "one", got[long])
		require.Equal(t, "two", got[long+"[2]"])
		for key := range got {
			require.NotContains(t, key, "$", "the companion column is not a field")
		}

		bundle, err := schemaSvc.ExportDatabaseData(ctx, "repetitions")
		require.NoError(t, err)
		require.NoError(t, dataSvc.DeleteRow(ctx, name, id))
		_, err = schemaSvc.ImportDatabaseData(ctx, bundle)
		require.NoError(t, err)
		got, err = dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.Equal(t, "one", got[long])
		require.Equal(t, "two", got[long+"[2]"])
	})

	t.Run("deleting a repeating field drops its repetitions", func(t *testing.T) {
		itemCol := findColumn(t, schemaSvc, ctx, tbl.ID, "item")
		require.NoError(t, schemaSvc.DeleteColumn(ctx, tbl.ID, itemCol.ID))
		add("item", dbal.FieldTypeText, 3, nil)
		got, err := dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.NotContains(t, got, "item[3]", "a new field of the same name starts empty")
	})
}

func findColumn(t *testing.T, svc *schema.Service, ctx context.Context, tableID, name string) schema.ColumnMetadata {
	t.Helper()
	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	for _, tb := range tables {
		if tb.ID != tableID {
			continue
		}
		for _, c := range tb.Columns {
			if c.Name == name {
				return c
			}
		}
	}
	t.Fatalf("no column %s", name)
	return schema.ColumnMetadata{}
}
