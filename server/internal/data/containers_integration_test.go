package data_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/containermeta"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/stretchr/testify/require"
)

// Container fields in formulas, and calculations whose result is a
// container, end to end against a real engine.
func TestContainerCalculations(t *testing.T) {
	driver, err := dbal.Connect(dbal.DriverConfig{EngineType: testdb.Engine(), DSN: testdb.DevDSN()})
	if err != nil {
		t.Skip("database not available:", err)
		return
	}
	defer driver.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := driver.Ping(ctx); err != nil {
		t.Skip("database ping failed:", err)
		return
	}

	schemaSvc := schema.NewService(driver)
	require.NoError(t, schemaSvc.EnsureSystemTables(ctx))
	dataSvc := data.NewService(driver)

	name := fmt.Sprintf("box_cards_%d", uniqueTestStamp())
	tbl, err := schemaSvc.CreateTable(ctx, "Box Cards", name)
	require.NoError(t, err)
	defer func() { _ = schemaSvc.DeleteTable(ctx, tbl.ID) }()

	add := func(field string, kind dbal.AgnosticFieldType, reps int, formula *string) {
		_, err := schemaSvc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
			Name: field, DisplayName: field, FieldType: kind, IsNullable: true,
			CalculationFormula: formula, MaxRepetitions: reps,
		})
		require.NoError(t, err, field)
	}
	unstored := func(formula, result string) *string {
		s := fmt.Sprintf(`{"formula":%q,"result_type":%q,"stored":false}`, formula, result)
		return &s
	}
	add("side", dbal.FieldTypeText, 1, nil)
	add("front", dbal.FieldTypeContainer, 2, nil)
	add("back", dbal.FieldTypeContainer, 1, nil)
	add("shown", dbal.FieldTypeCalculation, 1, calcJSON(`If ( side = "a" ; front ; back )`, "Container"))
	add("inner", dbal.FieldTypeCalculation, 1, calcJSON(`GetRepetition ( front ; 2 )`, "Container"))
	add("again", dbal.FieldTypeCalculation, 1, calcJSON(`shown`, "Container"))
	add("caption", dbal.FieldTypeCalculation, 1, calcJSON(
		`GetAsText ( shown ) & "|" & GetContainerAttribute ( back ; "fileSize" ) & "|" & IsEmpty ( front ) & "|" & IsEmpty ( inner )`, "Text"))
	add("live", dbal.FieldTypeCalculation, 1, unstored(`Case ( side = "b" ; back ; front )`, "Container"))

	const hello, xy, abcdef = "aGVsbG8=", "eHk=", "YWJjZGVm"
	row, err := dataSvc.InsertRow(ctx, name, map[string]interface{}{
		"side": "a", "front": hello, "front[2]": xy, "back": abcdef,
	})
	require.NoError(t, err)
	id := row["id"].(string)
	require.NoError(t, containermeta.Put(ctx, driver.DB(), driver.Dialect(), name, id, map[string]containermeta.Entry{
		"front": {Filename: "front.txt", MimeType: "text/plain", SizeBytes: 5},
	}))
	_, err = dataSvc.RecalculateTable(ctx, name)
	require.NoError(t, err)

	read := func() map[string]interface{} {
		t.Helper()
		got, err := dataSvc.GetRow(ctx, name, id)
		require.NoError(t, err)
		require.NoError(t, dataSvc.AttachContainerMeta(ctx, name, []map[string]interface{}{got}))
		return got
	}

	t.Run("a container result reads as the container it refers to", func(t *testing.T) {
		got := read()
		require.Equal(t, hello, got["shown"])
		require.Equal(t, xy, got["inner"])
		require.Equal(t, hello, got["again"], "a result read from another result")
		require.Equal(t, hello, got["live"], "unstored")
		meta := got[containermeta.Key].(map[string]containermeta.Entry)
		require.Equal(t, "front.txt", meta["shown"].Filename)
		require.Equal(t, "text/plain", meta["live"].MimeType)
		require.Equal(t, "front.txt|6|0|0", got["caption"])
	})

	t.Run("the result stores a reference, not a copy", func(t *testing.T) {
		d := driver.Dialect()
		var stored string
		require.NoError(t, driver.DB().QueryRowContext(ctx,
			fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", d.QuoteIdentifier("shown"), d.QuoteIdentifier(name), d.Placeholder(1)),
			id).Scan(&stored))
		require.JSONEq(t, fmt.Sprintf(`{"table":%q,"record":%q,"field":"front"}`, name, id), stored)
		require.NoError(t, driver.DB().QueryRowContext(ctx,
			fmt.Sprintf("SELECT %s FROM %s WHERE id = %s", d.QuoteIdentifier("inner"), d.QuoteIdentifier(name), d.Placeholder(1)),
			id).Scan(&stored))
		require.Contains(t, stored, `"field":"front[2]"`)
	})

	t.Run("the result follows the record", func(t *testing.T) {
		_, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"side": "b"})
		require.NoError(t, err)
		got := read()
		require.Equal(t, abcdef, got["shown"])
		require.Equal(t, abcdef, got["live"])
		require.Equal(t, "|6|0|0", got["caption"], "back has no stored name")

		_, err = dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"back": nil, "front[2]": nil})
		require.NoError(t, err)
		got = read()
		require.Nil(t, got["shown"])
		require.Nil(t, got["inner"])
		require.Equal(t, "||0|1", got["caption"])
	})

	t.Run("an export writes the container a result refers to", func(t *testing.T) {
		_, err := dataSvc.UpdateRow(ctx, name, id, map[string]interface{}{"side": "a"})
		require.NoError(t, err)
		table, err := dataSvc.ExportRecords(ctx, name, data.ExportOptions{Fields: []string{"shown", "live"}})
		require.NoError(t, err)
		require.Equal(t, [][]string{{hello, hello}}, table.Rows)
	})

	t.Run("a formula tried on a record sees containers by name", func(t *testing.T) {
		ev, err := dataSvc.EvaluateExpression(ctx, name, id, `GetAsText ( front ) & "/" & GetContainerAttribute ( front ; "fileSize" )`, "Text")
		require.NoError(t, err)
		require.Equal(t, "front.txt/5", ev.Text)
		ev, err = dataSvc.EvaluateExpression(ctx, name, id, `front`, "Container")
		require.NoError(t, err)
		require.Equal(t, "front.txt", ev.Text)
	})
}

// A related container, in an unstored container calculation and by name in a
// text one.
func TestRelatedContainerCalculations(t *testing.T) {
	driver, ctx := relatedTestDriver(t)
	if driver == nil {
		return
	}
	p := newPair(t, ctx, driver, dbal.FieldTypeText, "=")
	_, err := p.schemaSvc.AddColumn(ctx, p.rightID, schema.ColumnMetadata{Name: "scan", DisplayName: "scan",
		FieldType: dbal.FieldTypeContainer, IsNullable: true})
	require.NoError(t, err)
	for field, formula := range map[string]string{
		"first_scan": `{"formula":"` + p.rightOccurr + `::scan","result_type":"Container","stored":false}`,
		"scan_name":  `{"formula":"GetAsText ( ` + p.rightOccurr + `::scan ) & \"/\" & GetContainerAttribute ( ` + p.rightOccurr + `::scan ; \"fileSize\" )","result_type":"Text","stored":false}`,
	} {
		f := formula
		_, err := p.schemaSvc.AddColumn(ctx, p.leftID, schema.ColumnMetadata{Name: field, DisplayName: field,
			FieldType: dbal.FieldTypeCalculation, IsNullable: true, CalculationFormula: &f})
		require.NoError(t, err, field)
	}
	left := p.insert(t, ctx, p.left, map[string]interface{}{"k": "x"})
	right := p.insert(t, ctx, p.right, map[string]interface{}{"k": "x", "scan": "c2Nhbg=="})
	require.NoError(t, containermeta.Put(ctx, driver.DB(), driver.Dialect(), p.right, right, map[string]containermeta.Entry{
		"scan": {Filename: "scan.bin", MimeType: "application/octet-stream", SizeBytes: 4},
	}))

	got, err := p.dataSvc.GetRow(ctx, p.left, left)
	require.NoError(t, err)
	require.NoError(t, p.dataSvc.AttachContainerMeta(ctx, p.left, []map[string]interface{}{got}))
	require.Equal(t, "c2Nhbg==", got["first_scan"])
	require.Equal(t, "scan.bin", got[containermeta.Key].(map[string]containermeta.Entry)["first_scan"].Filename)
	require.Equal(t, "scan.bin/4", got["scan_name"])

	// A reader who may not read the related table gets no file.
	guarded := p.dataSvc.WithReadAccess(func(_ context.Context, table string) (bool, error) { return table != p.right, nil })
	got, err = guarded.GetRow(ctx, p.left, left)
	require.NoError(t, err)
	require.NoError(t, guarded.AttachContainerMeta(ctx, p.left, []map[string]interface{}{got}))
	require.Nil(t, got["first_scan"])
}
