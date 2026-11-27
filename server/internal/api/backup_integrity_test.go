package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
)

// backupFileOf reads a backup as its download gives it.
func backupFileOf(t *testing.T, s *serverToolsHarness, token, id string) map[string]json.RawMessage {
	t.Helper()
	raw := s.do(http.MethodGet, "/api/v1/server/backups/"+id+"/download", token, nil, nil)
	require.Equal(t, http.StatusOK, raw.Code)
	var file map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw.Body.Bytes(), &file))
	return file
}

func dataBundleOf(t *testing.T, file map[string]json.RawMessage) schema.DatabaseDataBundle {
	t.Helper()
	var b64 string
	require.NoError(t, json.Unmarshal(file["data_bundle_b64"], &b64))
	raw, err := base64.StdEncoding.DecodeString(b64)
	require.NoError(t, err)
	var bundle schema.DatabaseDataBundle
	require.NoError(t, msgpack.Unmarshal(raw, &bundle))
	return bundle
}

// A backup is the database at one moment: what users change while it is
// taken, between its design and its records, is not in it (#203).
func TestBackup_IsOneSnapshotWhileUsersWrite(t *testing.T) {
	s := newServerToolsHarness(t, false)
	ctx, svc, dataSvc, _, _ := backupFixture(t, s)
	defer api.SetBackupHookForTest(func() {
		_, err := dataSvc.InsertRow(ctx, "samples", map[string]interface{}{"note": "written during the backup"})
		require.NoError(t, err)
		_, err = svc.CreateTable(ctx, "later", "later")
		require.NoError(t, err)
	})()

	code, id := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	file := backupFileOf(t, s, s.aliceToken, id)
	var tables map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(file["tables"], &tables))
	assert.Contains(t, tables, "samples")
	assert.NotContains(t, tables, "later", "a table created during the backup is not in it")
	bundle := dataBundleOf(t, file)
	assert.Len(t, bundle.TablesData["samples"], 1, "nor is a record written during it")
	assert.NotContains(t, bundle.TablesData, "later")

	rows, err := dataSvc.ListRows(ctx, "samples", data.QueryOptions{Limit: 100})
	require.NoError(t, err)
	assert.Len(t, rows, 2, "the write itself went through")
}

// The records of a backup are written as they are read, and are those the
// data export gives.
func TestBackup_StreamsTheRecordsTheExportGives(t *testing.T) {
	s := newServerToolsHarness(t, false)
	ctx, svc, dataSvc, _, _ := backupFixture(t, s)
	for i := 0; i < 300; i++ {
		_, err := dataSvc.InsertRow(ctx, "samples", map[string]interface{}{"note": fmt.Sprintf("record %d", i), "note[2]": "x"})
		require.NoError(t, err)
	}
	code, id := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	streamed := dataBundleOf(t, backupFileOf(t, s, s.aliceToken, id))

	exported, err := svc.ExportDatabaseData(ctx, s.aliceDB)
	require.NoError(t, err)
	var buffered schema.DatabaseDataBundle
	require.NoError(t, msgpack.Unmarshal(exported, &buffered))
	assert.Equal(t, buffered.Format, streamed.Format)
	assert.Equal(t, buffered.DatabaseName, streamed.DatabaseName)
	assert.Len(t, streamed.TablesData["samples"], 301)
	assert.Equal(t, buffered.TablesData, streamed.TablesData)
}

// A restore that fails part-way leaves the database as it was: here its
// records cannot be put back after its tables and fields were (#203).
func TestRestore_FailingPartWayLeavesTheDatabaseUnchanged(t *testing.T) {
	s := newServerToolsHarness(t, false)
	ctx, svc, dataSvc, echo, id := backupFixture(t, s)
	code, backupID := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)

	// Changed since the backup: a field and a table added, a record edited.
	_, err := svc.AddColumn(ctx, echo.TableID, schema.ColumnMetadata{
		Name: "extra", DisplayName: "extra", FieldType: dbal.FieldTypeText, IsNullable: true})
	require.NoError(t, err)
	later, err := svc.CreateTable(ctx, "later", "later")
	require.NoError(t, err)
	_, err = dataSvc.InsertRow(ctx, "later", map[string]interface{}{})
	require.NoError(t, err)
	_, err = dataSvc.UpdateRow(ctx, "samples", id, map[string]interface{}{"note": "edited", "extra": "kept"})
	require.NoError(t, err)

	// A backup whose records name a field its tables do not have.
	file := backupFileOf(t, s, s.aliceToken, backupID)
	bundle := dataBundleOf(t, file)
	bundle.TablesData["samples"][0]["ghost"] = "no such field"
	raw, err := msgpack.Marshal(bundle)
	require.NoError(t, err)
	file["data_bundle_b64"], _ = json.Marshal(base64.StdEncoding.EncodeToString(raw))
	damaged, err := json.Marshal(file)
	require.NoError(t, err)
	up := s.doRaw(http.MethodPost, "/api/v1/server/backups/upload", s.aliceToken, "application/json", damaged)
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	var uploaded struct{ ID string }
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &uploaded))

	rec := s.do(http.MethodPost, "/api/v1/server/backups/"+uploaded.ID+"/restore", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "ghost", "the error says what failed")
	if testdb.Engine() == dbal.EnginePostgres {
		assert.Contains(t, rec.Body.String(), "rolled back")
	} else {
		assert.Contains(t, rec.Body.String(), "put back as it was")
	}

	tables, err := svc.ListTables(ctx)
	require.NoError(t, err)
	names := map[string]schema.TableMetadata{}
	for _, tb := range tables {
		names[tb.Name] = tb
	}
	require.Contains(t, names, "later", "the table added since is still there")
	hasExtra := false
	for _, c := range names["samples"].Columns {
		hasExtra = hasExtra || c.Name == "extra"
	}
	assert.True(t, hasExtra, "so is the field")
	got, err := dataSvc.GetRow(ctx, "samples", id)
	require.NoError(t, err)
	assert.Equal(t, "edited", got["note"], "and the record as it was edited")
	assert.Equal(t, "kept", got["extra"])
	laterRows, err := dataSvc.ListRows(ctx, later.Name, data.QueryOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, laterRows, 1)

	var list backupList
	s.do(http.MethodGet, "/api/v1/server/backups", s.aliceToken, nil, &list)
	assert.Len(t, list.Backups, 2, "no pre-restore backup is left: nothing was changed")
}

// A restore that completes keeps a pre-restore backup, which undoes it.
func TestRestore_KeepsAPreRestoreBackup(t *testing.T) {
	s := newServerToolsHarness(t, false)
	ctx, _, dataSvc, _, id := backupFixture(t, s)
	code, backupID := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	_, err := dataSvc.UpdateRow(ctx, "samples", id, map[string]interface{}{"note": "edited"})
	require.NoError(t, err)

	var res struct {
		Status           string `json:"status"`
		PreRestoreBackup string `json:"pre_restore_backup"`
	}
	rec := s.do(http.MethodPost, "/api/v1/server/backups/"+backupID+"/restore", s.aliceToken, nil, &res)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "restored", res.Status)
	require.NotEmpty(t, res.PreRestoreBackup)
	got, err := dataSvc.GetRow(ctx, "samples", id)
	require.NoError(t, err)
	assert.Equal(t, "a", got["note"])

	var list struct {
		Backups []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"backups"`
	}
	s.do(http.MethodGet, "/api/v1/server/backups", s.aliceToken, nil, &list)
	types := map[string]string{}
	for _, b := range list.Backups {
		types[b.ID] = b.Type
	}
	assert.Equal(t, "pre_restore", types[res.PreRestoreBackup])

	rec = s.do(http.MethodPost, "/api/v1/server/backups/"+res.PreRestoreBackup+"/restore", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got, err = dataSvc.GetRow(ctx, "samples", id)
	require.NoError(t, err)
	assert.Equal(t, "edited", got["note"], "the pre-restore backup undid the restore")
}

// A backup written before #203, its fields in alphabetical order, is still
// listed and restored; an interrupted one is not listed.
func TestBackup_OlderFilesAreStillRead(t *testing.T) {
	s := newServerToolsHarness(t, true)
	ctx, _, dataSvc, _, id := backupFixture(t, s)
	code, backupID := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	file := backupFileOf(t, s, s.aliceToken, backupID)

	var dir struct {
		BackupsDir string `json:"backups_dir"`
	}
	s.do(http.MethodGet, "/api/v1/server/backups", s.adminToken, nil, &dir)
	require.NotEmpty(t, dir.BackupsDir)
	file["id"], _ = json.Marshal("bkp_legacy")
	file["notes"], _ = json.Marshal("written by 0.22")
	legacy, err := json.MarshalIndent(file, "", "  ") // keys sorted, as 0.22 wrote them
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir.BackupsDir, "bkp_legacy.f4b.json"), legacy, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir.BackupsDir, "bkp_cut.f4b.json.partial"), legacy[:100], 0o600))

	var list struct {
		Backups []struct {
			ID       string `json:"id"`
			Database string `json:"database"`
			Notes    string `json:"notes"`
		} `json:"backups"`
	}
	s.do(http.MethodGet, "/api/v1/server/backups", s.aliceToken, nil, &list)
	ids := map[string]string{}
	for _, b := range list.Backups {
		ids[b.ID] = b.Notes
		assert.Equal(t, s.aliceDB, b.Database)
	}
	assert.Equal(t, "written by 0.22", ids["bkp_legacy"])
	assert.NotContains(t, ids, "bkp_cut")

	_, err = dataSvc.UpdateRow(ctx, "samples", id, map[string]interface{}{"note": "edited"})
	require.NoError(t, err)
	rec := s.do(http.MethodPost, "/api/v1/server/backups/bkp_legacy/restore", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got, err := dataSvc.GetRow(context.Background(), "samples", id)
	require.NoError(t, err)
	assert.Equal(t, "a", got["note"])
}
