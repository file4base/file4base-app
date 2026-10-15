package api_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
)

// serverToolsHarness has three databases: the server administration
// database, and two ordinary databases each created by someone with no
// other account on the server (database creation is public by default).
type serverToolsHarness struct {
	*apiHarness
	adminDB, aliceDB, bobDB          string
	adminToken, aliceToken, bobToken string
}

func newServerToolsHarness(t *testing.T, adminDatabase bool) *serverToolsHarness {
	t.Helper()
	s := &serverToolsHarness{adminDB: uniqueDB("srvadmin"), aliceDB: uniqueDB("alice"), bobDB: uniqueDB("bob")}
	opts := api.Options{AllowPublicDatabaseCreation: true, BackupsDir: t.TempDir()}
	if adminDatabase {
		opts.AdminDatabase = s.adminDB
	}
	s.apiHarness = newAPIHarness(t, opts)
	s.createDatabase(s.adminDB, "root", "root-pass-1")
	s.createDatabase(s.aliceDB, "alice", "alice-pass-1")
	s.createDatabase(s.bobDB, "bob", "bob-pass-1")
	s.adminToken = s.login(s.adminDB, "root", "root-pass-1")
	s.aliceToken = s.login(s.aliceDB, "alice", "alice-pass-1")
	s.bobToken = s.login(s.bobDB, "bob", "bob-pass-1")
	return s
}

type backupList struct {
	Backups []struct {
		ID       string `json:"id"`
		Database string `json:"database"`
	} `json:"backups"`
}

func (s *serverToolsHarness) backup(token string, body map[string]string) (int, string) {
	var res struct {
		ID string `json:"id"`
	}
	rec := s.do(http.MethodPost, "/api/v1/server/backups", token, body, &res)
	return rec.Code, res.ID
}

func (s *serverToolsHarness) doRaw(method, path, token, contentType string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

var serverWideRoutes = []struct{ method, path string }{
	{http.MethodGet, "/api/v1/server/sessions"},
	{http.MethodGet, "/api/v1/server/containers"},
	{http.MethodGet, "/api/v1/server/telemetry"},
	{http.MethodGet, "/api/v1/server/tls"},
	{http.MethodGet, "/api/v1/server/audit-logs"},
	{http.MethodDelete, "/api/v1/server/audit-logs"},
	{http.MethodGet, "/api/v1/server/schedules"},
	{http.MethodGet, "/api/v1/server/notifications"},
}

func TestServerTools_ServerWideToolsNeedTheServerAdministrator(t *testing.T) {
	s := newServerToolsHarness(t, true)

	for _, route := range serverWideRoutes {
		rec := s.do(route.method, route.path, s.aliceToken, nil, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "owner of another database: %s %s", route.method, route.path)
	}
	for _, route := range serverWideRoutes {
		if route.method != http.MethodGet {
			continue
		}
		rec := s.do(route.method, route.path, s.adminToken, nil, nil)
		assert.Equal(t, http.StatusOK, rec.Code, "server administrator: %s %s %s", route.method, route.path, rec.Body.String())
	}
}

func TestServerTools_ServerWideToolsAreDisabledWithoutAnAdminDatabase(t *testing.T) {
	s := newServerToolsHarness(t, false)
	for _, token := range []string{s.adminToken, s.aliceToken} {
		rec := s.do(http.MethodGet, "/api/v1/server/sessions", token, nil, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.Contains(t, rec.Body.String(), "FILE4BASE_ADMIN_DATABASE")
	}
	// The tools of one database keep working for its owner.
	code, _ := s.backup(s.aliceToken, nil)
	assert.Equal(t, http.StatusCreated, code)
}

func TestServerTools_DatabaseToolsOnlyReachTheSessionsDatabase(t *testing.T) {
	s := newServerToolsHarness(t, true)

	// Another database cannot be backed up or inspected.
	code, _ := s.backup(s.aliceToken, map[string]string{"database": s.bobDB})
	assert.Equal(t, http.StatusForbidden, code)
	rec := s.do(http.MethodGet, "/api/v1/server/maintenance?database="+s.bobDB, s.aliceToken, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = s.do(http.MethodPost, "/api/v1/server/maintenance/optimize", s.aliceToken, map[string]string{"database": s.bobDB}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Each owner backs up their own database.
	code, aliceBackup := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)
	code, bobBackup := s.backup(s.bobToken, nil)
	require.Equal(t, http.StatusCreated, code)
	require.NotEmpty(t, bobBackup)

	// Alice only sees, and can only use, her own backups. Bob's is "not found".
	var list backupList
	s.do(http.MethodGet, "/api/v1/server/backups", s.aliceToken, nil, &list)
	require.Len(t, list.Backups, 1)
	assert.Equal(t, aliceBackup, list.Backups[0].ID)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/server/backups/" + bobBackup + "/download"},
		{http.MethodPost, "/api/v1/server/backups/" + bobBackup + "/restore"},
		{http.MethodDelete, "/api/v1/server/backups/" + bobBackup},
	} {
		rec := s.do(route.method, route.path, s.aliceToken, nil, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, "%s %s", route.method, route.path)
	}
	// Bob's backup is still there for Bob.
	rec = s.do(http.MethodGet, "/api/v1/server/backups/"+bobBackup+"/download", s.bobToken, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), s.bobDB)

	// The server administrator sees every backup and can back up any database.
	s.do(http.MethodGet, "/api/v1/server/backups", s.adminToken, nil, &list)
	assert.Len(t, list.Backups, 2)
	code, _ = s.backup(s.adminToken, map[string]string{"database": s.aliceDB})
	assert.Equal(t, http.StatusCreated, code)
}

func TestServerTools_BackupDownloadsAsTarGz(t *testing.T) {
	s := newServerToolsHarness(t, true)
	code, id := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)

	rec := s.do(http.MethodGet, "/api/v1/server/backups/"+id+"/download?format=targz", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), ".tar.gz")

	gz, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	require.NoError(t, err, "the archive holds one file")
	assert.True(t, strings.HasSuffix(hdr.Name, ".f4b.json"))
	content, err := io.ReadAll(tr)
	require.NoError(t, err)
	assert.Contains(t, string(content), s.aliceDB, "the snapshot JSON is inside the archive")
	_, err = tr.Next()
	assert.ErrorIs(t, err, io.EOF, "only one file in the archive")
}

func TestServerTools_BackupUploadRoundTrip(t *testing.T) {
	s := newServerToolsHarness(t, true)
	code, id := s.backup(s.aliceToken, nil)
	require.Equal(t, http.StatusCreated, code)

	// Download the tar.gz and upload it straight back.
	dl := s.do(http.MethodGet, "/api/v1/server/backups/"+id+"/download?format=targz", s.aliceToken, nil, nil)
	require.Equal(t, http.StatusOK, dl.Code)

	up := s.doRaw(http.MethodPost, "/api/v1/server/backups/upload", s.aliceToken, "application/gzip", dl.Body.Bytes())
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	var rec struct {
		ID, Database, Type string
	}
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &rec))
	assert.Equal(t, s.aliceDB, rec.Database)
	assert.Equal(t, "uploaded", rec.Type)

	// It appears in Alice's list and restores.
	var list backupList
	s.do(http.MethodGet, "/api/v1/server/backups", s.aliceToken, nil, &list)
	ids := make([]string, 0, len(list.Backups))
	for _, b := range list.Backups {
		ids = append(ids, b.ID)
	}
	assert.Contains(t, ids, rec.ID)
	assert.Equal(t, http.StatusOK, s.do(http.MethodPost, "/api/v1/server/backups/"+rec.ID+"/restore", s.aliceToken, nil, nil).Code)

	// A raw .f4b.json upload also works.
	raw := s.do(http.MethodGet, "/api/v1/server/backups/"+id+"/download", s.aliceToken, nil, nil)
	up = s.doRaw(http.MethodPost, "/api/v1/server/backups/upload", s.aliceToken, "application/json", raw.Body.Bytes())
	assert.Equal(t, http.StatusCreated, up.Code, up.Body.String())

	// A non-admin's upload is stamped with their own database, whatever the
	// file claims: Bob uploads Alice's backup and it becomes Bob's.
	up = s.doRaw(http.MethodPost, "/api/v1/server/backups/upload", s.bobToken, "application/json", raw.Body.Bytes())
	require.Equal(t, http.StatusCreated, up.Code, up.Body.String())
	require.NoError(t, json.Unmarshal(up.Body.Bytes(), &rec))
	assert.Equal(t, s.bobDB, rec.Database, "an uploaded backup can only target the uploader's own database")

	// Garbage is rejected.
	assert.Equal(t, http.StatusUnprocessableEntity,
		s.doRaw(http.MethodPost, "/api/v1/server/backups/upload", s.aliceToken, "application/json", []byte("not a backup")).Code)
}

func TestServerTools_AccessTellsTheClientWhatItMayUse(t *testing.T) {
	s := newServerToolsHarness(t, true)
	var access struct {
		ServerAdmin   bool `json:"server_admin"`
		Configured    bool `json:"admin_database_configured"`
		DatabaseTools bool `json:"database_tools"`
	}
	s.do(http.MethodGet, "/api/v1/server/access", s.aliceToken, nil, &access)
	assert.False(t, access.ServerAdmin)
	assert.True(t, access.Configured)
	assert.True(t, access.DatabaseTools)

	s.do(http.MethodGet, "/api/v1/server/access", s.adminToken, nil, &access)
	assert.True(t, access.ServerAdmin)
}
