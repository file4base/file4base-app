package api_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/fmdapi"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// TestAPI_BulkPrivileges covers the capabilities an owner can withhold from a
// role (#39): they are enforced on the server, for every caller, and an owner
// keeps them whatever is stored.
func TestAPI_BulkPrivileges(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_priv")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	// A table with one field, and a record in it, so an export has content.
	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
		map[string]string{"display_name": "Members", "custom_name": "members"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner,
		map[string]interface{}{"display_name": "Full Name", "name": "full_name", "field_type": "TEXT"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/data/members", owner, map[string]string{"full_name": "Sophie Tang"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// An admin account, which is the role the restriction is put on.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "bob", "password": "bob-secret-pw", "role": "admin"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	admin := h.login(db, "bob", "bob-secret-pw")

	exportBody := map[string]interface{}{"format": "csv"}
	importBody := map[string]interface{}{
		"file_name":  "members.csv",
		"format":     "csv",
		"has_header": true,
		"content":    base64.StdEncoding.EncodeToString([]byte("full_name\nMarta Ortiz\n")),
		"options": map[string]interface{}{
			"action":   "add",
			"mappings": []map[string]interface{}{{"column": 0, "field": "full_name"}},
		},
	}

	// Nothing is restricted until an owner says so: a database that never
	// stored a privilege grants every bulk capability. The Data API is the
	// exception: only owners hold it until it is granted.
	var listed struct {
		Capabilities []string                `json:"capabilities"`
		Privileges   []schema.RolePrivileges `json:"privileges"`
	}
	rec = h.do(http.MethodGet, "/api/v1/security/privileges", owner, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{schema.CapabilityBulkExport, schema.CapabilityBulkImport, schema.CapabilityFMRest}, listed.Capabilities)
	require.Len(t, listed.Privileges, 3)
	for _, p := range listed.Privileges {
		assert.True(t, p.BulkExport, "%s export", p.Role)
		assert.True(t, p.BulkImport, "%s import", p.Role)
		assert.Equal(t, p.Role == "owner", p.FMRest, "%s fmrest", p.Role)
	}

	assert.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/members/export", admin, exportBody, nil).Code)
	assert.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/members/import/preview", admin, importBody, nil).Code)

	// The sign-in and the session report what the role may do.
	var session struct {
		Capabilities []string `json:"capabilities"`
	}
	rec = h.do(http.MethodGet, "/api/v1/auth/session", admin, nil, &session)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.ElementsMatch(t, []string{schema.CapabilityBulkExport, schema.CapabilityBulkImport}, session.Capabilities)

	// An owner withholds both capabilities from admins and users.
	restricted := []map[string]interface{}{
		{"role": "admin", "bulk_export": false, "bulk_import": false},
		{"role": "user", "bulk_export": false, "bulk_import": false},
	}
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner,
		map[string]interface{}{"privileges": restricted}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// It applies to the session that was already open, on every endpoint
	// that moves records in bulk, and nowhere else.
	for _, target := range []string{
		"/api/v1/data/members/export",
		"/api/v1/data/members/import",
		"/api/v1/data/members/import/preview",
		"/api/v1/solutions/import-data",
	} {
		rec = h.do(http.MethodPost, target, admin, importBody, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "POST %s", target)
	}
	rec = h.do(http.MethodGet, "/api/v1/solutions/export-data", admin, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Reading records one page at a time is not a bulk capability.
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/members", admin, nil, nil).Code)
	assert.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/members/find", admin,
		map[string]interface{}{"requests": []interface{}{}}, nil).Code)

	// The owner is never restricted, whatever is stored for the role.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner,
		map[string]interface{}{"privileges": []map[string]interface{}{
			{"role": "owner", "bulk_export": false, "bulk_import": false},
		}}, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, p := range listed.Privileges {
		if p.Role == "owner" {
			assert.True(t, p.BulkExport)
			assert.True(t, p.BulkImport)
		}
	}
	assert.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/members/export", owner, exportBody, nil).Code)

	// An admin may read the grants but not change them: granting a
	// capability to one's own role would be no restriction at all.
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/security/privileges", admin, nil, nil).Code)
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", admin,
		map[string]interface{}{"privileges": []map[string]interface{}{
			{"role": "admin", "bulk_export": true, "bulk_import": true},
		}}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, http.StatusForbidden, h.do(http.MethodPost, "/api/v1/data/members/export", admin, exportBody, nil).Code)

	// A role File4Base does not have is refused rather than stored.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner,
		map[string]interface{}{"privileges": []map[string]interface{}{
			{"role": "superuser", "bulk_export": true, "bulk_import": true},
		}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Granting it back makes the restriction disappear again.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner,
		map[string]interface{}{"privileges": []map[string]interface{}{
			{"role": "admin", "bulk_export": true, "bulk_import": false},
		}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/members/export", admin, exportBody, nil).Code)
	assert.Equal(t, http.StatusForbidden, h.do(http.MethodPost, "/api/v1/data/members/import", admin, importBody, nil).Code)

	rec = h.do(http.MethodGet, "/api/v1/auth/session", admin, nil, &session)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{schema.CapabilityBulkExport}, session.Capabilities)
}

// TestAPI_DataAPIAccessIsTheFMRestPrivilege covers the fmrest capability: an
// account signs in to the Data API only when its role holds it, which owners
// always do and other roles do once an owner grants it, and the Data API
// sessions of a role it is withdrawn from end at once.
func TestAPI_DataAPIAccessIsTheFMRestPrivilege(t *testing.T) {
	db := uniqueDB("f4b_fmrest")
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		DataAPI:                     &fmdapi.Config{Databases: []fmdapi.DatabaseAlias{{Name: db, Database: db}}},
	})
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")
	for _, u := range []struct{ name, role string }{{"bob", "admin"}, {"carol", "user"}} {
		rec := h.do(http.MethodPost, "/api/v1/security/users", owner,
			map[string]interface{}{"username": u.name, "password": u.name + "-secret-pw", "role": u.role}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	signIn := func(user, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/fmi/data/vLatest/databases/"+db+"/sessions", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}
	validate := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/fmi/data/vLatest/validateSession", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}
	const refused = `{"messages":[{"code":"9","message":"Insufficient privileges"}],"response":{}}`

	// Only the owner holds fmrest until it is granted.
	assert.Equal(t, http.StatusOK, signIn("alice", "alice-secret").Code)
	for _, user := range []string{"bob", "carol"} {
		rec := signIn(user, user+"-secret-pw")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, user)
		assert.Equal(t, refused, rec.Body.String(), user)
		assert.Empty(t, rec.Header().Get("X-FM-Data-Access-Token"), user)
	}

	// Granted to users, it lets them in, and not admins.
	rec := h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = signIn("carol", "carol-secret-pw")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	token := rec.Header().Get("X-FM-Data-Access-Token")
	require.NotEmpty(t, token)
	assert.Equal(t, http.StatusOK, validate(token).Code)
	assert.Equal(t, refused, signIn("bob", "bob-secret-pw").Body.String())

	// Changing another capability of the role keeps the session.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": false, "bulk_import": true, "fmrest": true},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusOK, validate(token).Code)

	// Withdrawn, it ends the role's open Data API sessions, not the owner's.
	ownerToken := signIn("alice", "alice-secret").Header().Get("X-FM-Data-Access-Token")
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": false},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = validate(token)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"952"`)
	assert.Equal(t, http.StatusOK, validate(ownerToken).Code)
	assert.Equal(t, refused, signIn("carol", "carol-secret-pw").Body.String())

	// It is checked on every call, too, however it was withdrawn: here
	// straight in the catalog, as a solution import would.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = signIn("carol", "carol-secret-pw")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	token = rec.Header().Get("X-FM-Data-Access-Token")
	assert.Equal(t, http.StatusOK, validate(token).Code)
	driver, err := h.mgr.DriverFor(t.Context(), db)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(t.Context(), `UPDATE sys_privileges SET fmrest = FALSE WHERE role = 'user'`)
	require.NoError(t, err)
	rec = validate(token)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"952"`)

	// The owner's fmrest cannot be withheld.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "owner", "bulk_export": true, "bulk_import": true, "fmrest": false},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusOK, validate(ownerToken).Code)
	assert.Equal(t, http.StatusOK, signIn("alice", "alice-secret").Code)
}

// The full router answers the methods only Node knows as FileMaker Server
// does on /fmi/data (1704 on a known path, Express's 404 page on another)
// and leaves /api/v1 to chi.
func TestAPI_DataAPIAnswersNodeOnlyMethods(t *testing.T) {
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		DataAPI:                     &fmdapi.Config{Databases: []fmdapi.DatabaseAlias{{Name: "x", Database: "x"}}},
	})
	call := func(method, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec
	}
	recorded := call(http.MethodPut, "/fmi/data/vLatest/productInfo")
	require.Equal(t, http.StatusMethodNotAllowed, recorded.Code)
	require.Contains(t, recorded.Body.String(), `"1704"`)
	unknown := call(http.MethodGet, "/fmi/data/vLatest/nothing")
	require.Equal(t, http.StatusNotFound, unknown.Code)
	require.Contains(t, unknown.Body.String(), "Cannot GET /fmi/data/vLatest/nothing")

	for _, m := range []string{"PROPFIND", "MKCOL", "M-SEARCH", "UNBIND", "UNSUBSCRIBE"} {
		rec := call(m, "/fmi/data/vLatest/productInfo")
		assert.Equal(t, recorded.Code, rec.Code, m)
		assert.Equal(t, recorded.Body.String(), rec.Body.String(), m)
		assert.Equal(t, recorded.Header().Get("Content-Type"), rec.Header().Get("Content-Type"), m)

		rec = call(m, "/fmi/data/vLatest/nothing")
		assert.Equal(t, http.StatusNotFound, rec.Code, m)
		assert.Equal(t, strings.Replace(unknown.Body.String(), "Cannot GET", "Cannot "+m, 1), rec.Body.String(), m)

		assert.Equal(t, http.StatusMethodNotAllowed, call(m, "/api/v1/auth/session").Code, m)
	}
}

// fmrestHarness opens a database with the Data API on, its owner alice and a
// user carol, and returns a Data API sign-in and token check for it.
func fmrestHarness(t *testing.T, prefix string) (h *apiHarness, db, owner, carolID string,
	signIn func(user, password string) *httptest.ResponseRecorder, validate func(token string) *httptest.ResponseRecorder,
) {
	db = uniqueDB(prefix)
	h = newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		DataAPI:                     &fmdapi.Config{Databases: []fmdapi.DatabaseAlias{{Name: db, Database: db}}},
	})
	h.createDatabase(db, "alice", "alice-secret")
	owner = h.login(db, "alice", "alice-secret")
	var created struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, &created)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NotEmpty(t, created.ID)
	signIn = func(user, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/fmi/data/vLatest/databases/"+db+"/sessions", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}
	validate = func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/fmi/data/vLatest/validateSession", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		return rec
	}
	return h, db, owner, created.ID, signIn, validate
}

// rolePrivilege reads the stored privileges of one role.
func rolePrivilege(t *testing.T, h *apiHarness, token, role string) schema.RolePrivileges {
	t.Helper()
	var listed struct {
		Privileges []schema.RolePrivileges `json:"privileges"`
	}
	rec := h.do(http.MethodGet, "/api/v1/security/privileges", token, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	for _, p := range listed.Privileges {
		if p.Role == role {
			return p
		}
	}
	t.Fatalf("no privileges listed for %s", role)
	return schema.RolePrivileges{}
}

// Turning on two-step verification ends the account's Data API sessions,
// which cannot ask for the second factor; and a token whose account has it
// on is refused on its next call however it was turned on.
func TestAPI_TwoStepVerificationEndsDataAPISessions(t *testing.T) {
	h, db, owner, carolID, signIn, validate := fmrestHarness(t, "f4b_fmrest_mfa")
	rec := h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	open := func() string {
		rec := signIn("carol", "carol-secret-pw")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		token := rec.Header().Get("X-FM-Data-Access-Token")
		require.Equal(t, http.StatusOK, validate(token).Code)
		return token
	}

	token := open()
	var setup schema.MFASetupInfo
	rec = h.do(http.MethodPost, "/api/v1/security/users/"+carolID+"/mfa/setup", owner, nil, &setup)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusOK, validate(token).Code, "setting up alone changes nothing")
	code, err := schema.ComputeTOTPCode(setup.Secret, time.Now().UTC())
	require.NoError(t, err)
	rec = h.do(http.MethodPost, "/api/v1/security/users/"+carolID+"/mfa/enable", owner, map[string]string{"code": code}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = validate(token)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"952"`)
	rec = signIn("carol", "carol-secret-pw")
	assert.NotEqual(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Header().Get("X-FM-Data-Access-Token"))

	// Off again, the account signs in; turned on behind the session's back
	// (as a race with the enabling would), its token is refused all the same.
	rec = h.do(http.MethodPost, "/api/v1/security/users/"+carolID+"/mfa/disable", owner, nil, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	token = open()
	driver, err := h.mgr.DriverFor(t.Context(), db)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(t.Context(), `UPDATE sys_users SET mfa_enabled = TRUE WHERE username = 'carol'`)
	require.NoError(t, err)
	rec = validate(token)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"952"`)
}

// A privileges update that leaves fmrest out, as clients written before it
// do, keeps the stored value; one that names it sets it.
func TestAPI_PrivilegesUpdateKeepsOmittedFMRest(t *testing.T) {
	h, _, owner, _, signIn, validate := fmrestHarness(t, "f4b_fmrest_put")
	put := func(p map[string]interface{}) {
		t.Helper()
		rec := h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{p}}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	put(map[string]interface{}{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true})
	rec := signIn("carol", "carol-secret-pw")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	token := rec.Header().Get("X-FM-Data-Access-Token")

	put(map[string]interface{}{"role": "user", "bulk_export": false, "bulk_import": true})
	got := rolePrivilege(t, h, owner, "user")
	assert.False(t, got.BulkExport)
	assert.True(t, got.FMRest, "left out, fmrest is kept")
	assert.Equal(t, http.StatusOK, validate(token).Code, "and so is the session")

	// Left out for a role that never stored it, the default stays.
	put(map[string]interface{}{"role": "admin", "bulk_export": true, "bulk_import": false})
	assert.False(t, rolePrivilege(t, h, owner, "admin").FMRest)

	put(map[string]interface{}{"role": "user", "bulk_export": false, "bulk_import": true, "fmrest": false})
	assert.False(t, rolePrivilege(t, h, owner, "user").FMRest)
	assert.Equal(t, http.StatusUnauthorized, validate(token).Code)
}

// A solution file restores fmrest as a migration grants it: a file written
// before fmrest leaves it as it is, a file may withdraw it, and it grants it
// only to a role that had no account here, reporting the grants it holds
// back.
func TestAPI_SolutionImportFMRest(t *testing.T) {
	h, _, owner, _, signIn, validate := fmrestHarness(t, "f4b_fmrest_sol")
	importPrivileges := func(privileges []map[string]interface{}) schema.ImportReport {
		t.Helper()
		data, err := msgpack.Marshal(map[string]interface{}{
			"format": schema.SolutionFormat, "version": schema.SolutionVersion, "solution_name": "Roles",
			"privileges": privileges,
		})
		require.NoError(t, err)
		var report schema.ImportReport
		rec := h.postBytes("/api/v1/solutions/import", owner, data, &report)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return report
	}
	rec := h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = signIn("carol", "carol-secret-pw")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	token := rec.Header().Get("X-FM-Data-Access-Token")

	// Written before fmrest: the other privileges change, the grant stays.
	report := importPrivileges([]map[string]interface{}{{"role": "user", "bulk_export": false, "bulk_import": true}})
	assert.Empty(t, report.Warnings)
	got := rolePrivilege(t, h, owner, "user")
	assert.False(t, got.BulkExport)
	assert.True(t, got.FMRest)
	assert.Equal(t, http.StatusOK, validate(token).Code)

	// Withdrawn by the file, it is withdrawn, and the next call is refused.
	importPrivileges([]map[string]interface{}{{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": false}})
	assert.False(t, rolePrivilege(t, h, owner, "user").FMRest)
	assert.Equal(t, http.StatusUnauthorized, validate(token).Code)

	// Granted by the file: not to user, which has carol, but to admin,
	// which has no account here; the held-back grant is reported.
	report = importPrivileges([]map[string]interface{}{
		{"role": "admin", "bulk_export": true, "bulk_import": true, "fmrest": true},
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
	})
	assert.True(t, rolePrivilege(t, h, owner, "admin").FMRest)
	got = rolePrivilege(t, h, owner, "user")
	assert.False(t, got.FMRest)
	assert.True(t, got.BulkExport, "the rest of the role's privileges are restored")
	require.Len(t, report.Warnings, 1)
	assert.Contains(t, report.Warnings[0], `role "user", which was not granted`)
	assert.NotEqual(t, http.StatusOK, signIn("carol", "carol-secret-pw").Code)

	// A role that holds it keeps it when the file grants it again.
	rec = h.do(http.MethodPut, "/api/v1/security/privileges", owner, map[string]interface{}{"privileges": []map[string]interface{}{
		{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true},
	}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	report = importPrivileges([]map[string]interface{}{{"role": "user", "bulk_export": true, "bulk_import": true, "fmrest": true}})
	assert.Empty(t, report.Warnings)
	assert.True(t, rolePrivilege(t, h, owner, "user").FMRest)
}
