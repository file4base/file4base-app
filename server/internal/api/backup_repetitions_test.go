package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/repetition"
	"github.com/file4base/file4base-app/server/internal/schema"
)

// backupFixture gives Alice's database a table with a repeating field and a
// repeating stored calculation, and one record using three repetitions.
func backupFixture(t *testing.T, s *serverToolsHarness) (context.Context, *schema.Service, *data.Service, schema.ColumnMetadata, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	driver, err := s.mgr.DriverFor(ctx, s.aliceDB)
	require.NoError(t, err)
	svc, dataSvc := schema.NewService(driver), data.NewService(driver)
	tbl, err := svc.CreateTable(ctx, "samples", "samples")
	require.NoError(t, err)
	_, err = svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "note", DisplayName: "note", FieldType: dbal.FieldTypeText, IsNullable: true, MaxRepetitions: 3})
	require.NoError(t, err)
	formula := `{"formula":"note","result_type":"Text"}`
	echo, err := svc.AddColumn(ctx, tbl.ID, schema.ColumnMetadata{
		Name: "echo", DisplayName: "echo", FieldType: dbal.FieldTypeCalculation, IsNullable: true,
		CalculationFormula: &formula, MaxRepetitions: 3})
	require.NoError(t, err)
	row, err := dataSvc.InsertRow(ctx, "samples", map[string]interface{}{"note": "a", "note[2]": "b", "note[3]": "c"})
	require.NoError(t, err)
	require.Equal(t, "c", row["echo[3]"])
	return ctx, svc, dataSvc, *echo, row["id"].(string)
}

// A restore puts back the maximum of an existing calculation, whose records
// carry that many repetitions, and the records themselves.
func TestServerTools_RestoreBringsBackTheMaximumOfACalculation(t *testing.T) {
	s := newServerToolsHarness(t, false)
	ctx, svc, dataSvc, echo, id := backupFixture(t, s)

	code, backupID := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	_, err := svc.UpdateColumn(ctx, echo.TableID, echo.ID, schema.UpdateColumnOptions{
		DisplayName: "echo", MaxRepetitions: 1, UpdateRepetitions: true})
	require.NoError(t, err)

	var res map[string]interface{}
	rec := s.do(http.MethodPost, "/api/v1/server/backups/"+backupID+"/restore", s.aliceToken, nil, &res)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 1, res["records_restored"])

	got, err := dataSvc.GetRow(ctx, "samples", id)
	require.NoError(t, err)
	assert.Equal(t, "c", got["note[3]"])
	assert.Equal(t, "c", got["echo[3]"], "the calculation has its three repetitions again")
}

// A restore whose records cannot be put back fails instead of reporting an
// emptied database as restored.
func TestServerTools_RestoreFailsWhenTheRecordsCannotBeRestored(t *testing.T) {
	s := newServerToolsHarness(t, false)
	backupFixture(t, s)

	code, backupID := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	raw := s.do(http.MethodGet, "/api/v1/server/backups/"+backupID+"/download", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusOK, raw.Code)
	var snap map[string]interface{}
	require.NoError(t, json.Unmarshal(raw.Body.Bytes(), &snap))
	snap["data_bundle_b64"] = base64.StdEncoding.EncodeToString([]byte("not a data bundle"))
	damaged, err := json.Marshal(snap)
	require.NoError(t, err)
	up := s.doRaw(http.MethodPost, "/api/v1/server/backups/upload", s.aliceToken, "application/json", damaged)
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	var uploaded struct{ ID string }
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &uploaded))

	rec := s.do(http.MethodPost, "/api/v1/server/backups/"+uploaded.ID+"/restore", s.aliceToken, nil, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "records could not be restored")
	assert.NotContains(t, rec.Body.String(), `"restored"`)
}

// Backing up a database whose catalog predates repeating fields (nobody has
// signed in to it since the upgrade) brings the catalog up to date first,
// and the backup carries its tables and records.
func TestServerTools_BackupOfADatabaseWithAnOlderCatalog(t *testing.T) {
	s := newServerToolsHarness(t, true)
	backupFixture(t, s)
	ctx := context.Background()
	driver, err := s.mgr.DriverFor(ctx, s.aliceDB)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(ctx, `ALTER TABLE sys_columns DROP COLUMN max_repetitions`)
	require.NoError(t, err)

	var res struct {
		ID          string `json:"id"`
		TablesCount int    `json:"tables_count"`
	}
	rec := s.do(http.MethodPost, "/api/v1/server/backups", s.adminToken, map[string]string{"database": s.aliceDB}, &res)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, 1, res.TablesCount)
	raw := s.do(http.MethodGet, "/api/v1/server/backups/"+res.ID+"/download", s.adminToken, nil, nil)
	require.Equal(t, http.StatusOK, raw.Code)
	var snap struct {
		DataBundleB64 string `json:"data_bundle_b64"`
	}
	require.NoError(t, json.Unmarshal(raw.Body.Bytes(), &snap))
	bundle, err := base64.StdEncoding.DecodeString(snap.DataBundleB64)
	require.NoError(t, err)
	assert.Contains(t, string(bundle), "samples", "the records are in the backup")

	var cols int
	require.NoError(t, driver.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sys_columns WHERE max_repetitions = 1`).Scan(&cols), "the catalog was migrated")
	assert.Positive(t, cols)
}

// A backup whose records cannot be exported fails instead of writing a
// snapshot without them.
func TestServerTools_BackupFailsWhenTheRecordsCannotBeExported(t *testing.T) {
	s := newServerToolsHarness(t, false)
	backupFixture(t, s)
	ctx := context.Background()
	driver, err := s.mgr.DriverFor(ctx, s.aliceDB)
	require.NoError(t, err)
	d := driver.Dialect()
	_, err = driver.DB().ExecContext(ctx, "UPDATE "+d.QuoteIdentifier("samples")+" SET "+
		d.QuoteIdentifier(repetition.Column("note"))+" = 'not an array'")
	require.NoError(t, err)

	rec := s.do(http.MethodPost, "/api/v1/server/backups", s.aliceToken, nil, nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "records could not be exported")
	var list backupList
	s.do(http.MethodGet, "/api/v1/server/backups", s.aliceToken, nil, &list)
	assert.Empty(t, list.Backups, "no backup was written")
}

// A restore keeps a script in its menu folder, and puts back a field's
// FileMaker id and type.
func TestServerTools_RestoreKeepsScriptFoldersAndSourceFacts(t *testing.T) {
	s := newServerToolsHarness(t, false)
	ctx, svc, _, echo, _ := backupFixture(t, s)
	sc, err := svc.CreateScript(ctx, "Tidy", "samples", nil, true, nil)
	require.NoError(t, err)
	file, err := svc.ExportSolution(ctx, schema.ExportOptions{SolutionName: "Menus"})
	require.NoError(t, err)
	bundle, err := schema.DecodeSolutionBundle(file)
	require.NoError(t, err)
	bundle.Catalog = []schema.BundleCatalogEntry{
		{Catalog: schema.CatalogScripts, Kind: schema.MenuFolder, ID: "f1", Name: "Tools"},
		{Catalog: schema.CatalogScripts, Kind: schema.MenuScript, ID: sc.ID, FolderID: "f1"},
	}
	withMenu, err := msgpack.Marshal(bundle)
	require.NoError(t, err)
	_, err = svc.ImportSolution(ctx, withMenu)
	require.NoError(t, err)
	timed, err := svc.AddColumn(ctx, echo.TableID, schema.ColumnMetadata{
		Name: "starts", DisplayName: "starts", FieldType: dbal.FieldTypeTimestamp, IsNullable: true,
		SourceFieldID: "12", SourceType: schema.SourceTypeTime})
	require.NoError(t, err)

	code, backupID := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	require.NoError(t, svc.DeleteColumn(ctx, echo.TableID, timed.ID))

	rec := s.do(http.MethodPost, "/api/v1/server/backups/"+backupID+"/restore", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	menu, err := svc.Menu(ctx, schema.CatalogScripts)
	require.NoError(t, err)
	require.Len(t, menu, 1)
	assert.Equal(t, "Tools", menu[0].Name)
	require.Len(t, menu[0].Items, 1, "the script is in its folder again")
	assert.Equal(t, "Tidy", menu[0].Items[0].Name)
	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	found := false
	for _, tb := range tables {
		for _, c := range tb.Columns {
			if c.Name == "starts" {
				found = true
				assert.Equal(t, "12", c.SourceFieldID)
				assert.Equal(t, schema.SourceTypeTime, c.SourceType)
			}
		}
	}
	assert.True(t, found, "the field is back")
}
