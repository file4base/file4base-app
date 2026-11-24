package api_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// permissiveDesign declares table 129 with a Number, a Date and a calculation
// over the Number, which must compute from FileMaker's reading of the text.
const permissiveDesign = `<FMPReport><File name="Synthetic.fmp12"><BaseTableCatalog><BaseTable id="129" name="Visits"><FieldCatalog>` +
	`<Field id="1" name="Amount" dataType="Number"/><Field id="2" name="Visited" dataType="Date"/>` +
	`<Field id="3" name="Doubled" dataType="Number" fieldType="Calculation"><Calculation><Text>Amount * 2</Text></Calculation></Field>` +
	`</FieldCatalog></BaseTable></BaseTableCatalog></File></FMPReport>`

// syntheticNativeRecord builds an original HBAM7 file whose single leaf block
// holds one record of table 129 with the given field values (id → text).
func syntheticNativeRecord(values map[byte]string) []byte {
	file := make([]byte, 3*4096)
	copy(file[15:20], "HBAM7")
	binary.BigEndian.PutUint16(file[4096+12:], 2)
	payload := []byte{0x28, 0x80, 1, 0x20, 5, 0x20, 1}
	for id := byte(1); id < 16; id++ {
		value, ok := values[id]
		if !ok {
			continue
		}
		payload = append(payload, 0x06, id, byte(len(value)))
		for _, b := range []byte(value) {
			payload = append(payload, b^0x5a)
		}
	}
	copy(file[2*4096+20:], payload)
	return file
}

type permissiveCounts struct {
	Report struct {
		Counts struct {
			Detected   int `json:"records_detected"`
			Ready      int `json:"records_ready"`
			SourceText int `json:"source_text_values"`
		} `json:"counts"`
		Rejected []struct {
			Detail string `json:"detail"`
		} `json:"rejected"`
	} `json:"report"`
}

func sourceTextsOf(t *testing.T, row map[string]interface{}) map[string]interface{} {
	t.Helper()
	texts, _ := row[sourcetext.Key].(map[string]interface{})
	return texts
}

func countSourceRows(t *testing.T, h *apiHarness, database, where string) int {
	t.Helper()
	driver, err := h.mgr.DriverFor(context.Background(), database)
	require.NoError(t, err)
	var n int
	require.NoError(t, driver.DB().QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+sourcetext.Table+" WHERE "+where).Scan(&n))
	return n
}

// #124 acceptance: a native migration of permissive values reports every
// record ready; reading a value back gives its original text, and
// calculations see FileMaker's numeric reading of it.
func TestAPI_PermissiveNativeValuesMigrateWithTheirOriginalText(t *testing.T) {
	ctx := context.Background()
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_permissive")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	file := syntheticNativeRecord(map[byte]string{1: "12-A", 2: "soon"})

	// Strict without a locale, and saying how to migrate the values.
	var strict permissiveCounts
	rec := h.doMultipart("/api/v1/solutions/migrate/preflight", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": permissiveDesign}),
		{name: "native", filename: "Synthetic.fmp12", body: file},
	}, &strict)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, strict.Report.Counts.Detected)
	require.Equal(t, 0, strict.Report.Counts.Ready)
	require.Contains(t, rec.Body.String(), "source_locale")

	// With the file's locale, every record is ready.
	var preflight permissiveCounts
	withLocale := map[string]interface{}{"design_xml": permissiveDesign, "source_locale": "fr-FR"}
	rec = h.doMultipart("/api/v1/solutions/migrate/preflight", token, []uploadPart{
		requestPart(t, withLocale), {name: "native", filename: "Synthetic.fmp12", body: file},
	}, &preflight)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, preflight.Report.Counts.Detected)
	require.Equal(t, preflight.Report.Counts.Detected, preflight.Report.Counts.Ready)
	require.Equal(t, 2, preflight.Report.Counts.SourceText)

	var applied struct {
		RecordsInserted int `json:"records_inserted"`
	}
	rec = h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, withLocale), {name: "native", filename: "Synthetic.fmp12", body: file},
	}, &applied)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, 1, applied.RecordsInserted)

	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/visits/", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, rows, 1)
	row := rows[0]
	id := fmt.Sprint(row["id"])
	require.Equal(t, "12", fmt.Sprint(row["amount"]), "the field holds FileMaker's reading")
	require.Nil(t, row["visited"], "text that is no date has no date value")
	require.Equal(t, "24", fmt.Sprint(row["doubled"]), "a calculation computes from FileMaker's reading")
	require.Equal(t, map[string]interface{}{"amount": "12-A", "visited": "soon"}, sourceTextsOf(t, row))

	var one map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/visits/"+id, token, nil, &one)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "12-A", sourceTextsOf(t, one)["amount"])

	// A find on the field matches the number FileMaker reads.
	var found []map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/visits/find", token, map[string]interface{}{
		"requests": []map[string]interface{}{{"criteria": []map[string]interface{}{{"field_name": "amount", "operator": "=", "value": 12}}}},
	}, &found)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, found, 1)
	require.Equal(t, "12-A", sourceTextsOf(t, found[0])["amount"])

	driver, err := h.mgr.DriverFor(ctx, database)
	require.NoError(t, err)

	// An export writes the value the way the record shows it.
	exported, err := data.NewService(driver).ExportRecords(ctx, "visits", data.ExportOptions{Fields: []string{"amount", "doubled"}})
	require.NoError(t, err)
	require.Equal(t, [][]string{{"12-A", "24"}}, exported.Rows)

	// A data file (backup, transfer) carries the original texts.
	schemaSvc := schema.NewService(driver)
	bundleBytes, err := schemaSvc.ExportDatabaseData(ctx, database)
	require.NoError(t, err)
	var bundle schema.DatabaseDataBundle
	require.NoError(t, msgpack.Unmarshal(bundleBytes, &bundle))
	require.Len(t, bundle.TablesData["visits"], 1)
	require.Equal(t, map[string]interface{}{"amount": "12-A", "visited": "soon"}, bundle.TablesData["visits"][0][sourcetext.Key])

	// A client sending the record back cannot set an original text, and
	// writing a value drops the original text of that field only.
	var updated map[string]interface{}
	rec = h.do(http.MethodPut, "/api/v1/data/visits/"+id, token, map[string]interface{}{
		"amount": 5, sourcetext.Key: map[string]interface{}{"amount": "forged"},
	}, &updated)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "5", fmt.Sprint(updated["amount"]))
	require.Equal(t, "10", fmt.Sprint(updated["doubled"]))
	require.Equal(t, map[string]interface{}{"visited": "soon"}, sourceTextsOf(t, updated))

	// Deleting the record deletes its original texts.
	rec = h.do(http.MethodDelete, "/api/v1/data/visits/"+id, token, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Zero(t, countSourceRows(t, h, database, "table_name = 'visits'"))

	// Restoring the data file restores the record with its original texts.
	report, err := schemaSvc.ImportDatabaseData(ctx, bundleBytes)
	require.NoError(t, err)
	require.Equal(t, 1, report.RecordsInserted)
	rec = h.do(http.MethodGet, "/api/v1/data/visits/"+id, token, nil, &one)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "12", fmt.Sprint(one["amount"]))
	require.Equal(t, map[string]interface{}{"amount": "12-A", "visited": "soon"}, sourceTextsOf(t, one))

	// Deleting a field deletes its original texts, and no others.
	tables, err := schemaSvc.ListTables(ctx)
	require.NoError(t, err)
	for _, tbl := range tables {
		if tbl.Name != "visits" {
			continue
		}
		for _, col := range tbl.Columns {
			if col.Name == "visited" {
				require.NoError(t, schemaSvc.DeleteColumn(ctx, tbl.ID, col.ID))
			}
		}
	}
	require.Zero(t, countSourceRows(t, h, database, "field_name = 'visited'"))
	require.Equal(t, 1, countSourceRows(t, h, database, "field_name = 'amount'"))
}

// A database created before the original-text table existed reads and
// writes records as before, and gets the table when something needs it.
func TestAPI_RecordsWorkWithoutTheSourceTextTable(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	database := uniqueDB("f4b_no_source_table")
	h.createDatabase(database, "qualifier", "synthetic-test-password")
	token := h.login(database, "qualifier", "synthetic-test-password")
	rec := h.doMultipart("/api/v1/solutions/migrate/apply", token, []uploadPart{
		requestPart(t, map[string]interface{}{"design_xml": permissiveDesign, "source_locale": "fr-FR"}),
		{name: "native", filename: "Synthetic.fmp12", body: syntheticNativeRecord(map[byte]string{1: "7"})},
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	driver, err := h.mgr.DriverFor(context.Background(), database)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(context.Background(), "DROP TABLE "+sourcetext.Table)
	require.NoError(t, err)

	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/visits/", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, rows, 1)
	require.Nil(t, rows[0][sourcetext.Key])
	id := fmt.Sprint(rows[0]["id"])
	rec = h.do(http.MethodPut, "/api/v1/data/visits/"+id, token, map[string]interface{}{"amount": 8}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodDelete, "/api/v1/data/visits/"+id, token, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}
