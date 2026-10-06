package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/testdb"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// apiHarness drives the fully mounted API (public + protected routes) against
// a real PostgreSQL server, exactly as cmd/server wires it.
type apiHarness struct {
	t        *testing.T
	router   *chi.Mux
	mgr      *dbal.MultiDatabaseManager
	sessions *auth.Store
}

func newAPIHarness(t *testing.T, opts api.Options) *apiHarness {
	t.Helper()
	mgr, err := dbal.NewMultiDatabaseManager(testdb.Engine(), testdb.AdminDSN())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Ping(ctx); err != nil {
		t.Skip("PostgreSQL not available:", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })

	r := chi.NewRouter()
	sessions := auth.NewStore(time.Hour)
	api.Mount(r, mgr, sessions, opts)
	return &apiHarness{t: t, router: r, mgr: mgr, sessions: sessions}
}

// do performs a request and decodes a JSON object/array response into out (when non-nil).
func (h *apiHarness) do(method, target, token string, body interface{}, out interface{}) *httptest.ResponseRecorder {
	h.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(h.t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		require.NoError(h.t, json.Unmarshal(rec.Body.Bytes(), out), rec.Body.String())
	}
	return rec
}

func (h *apiHarness) createDatabase(name, user, password string) {
	h.t.Helper()
	rec := h.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": name, "user": user, "password": password}, nil)
	require.Equal(h.t, http.StatusCreated, rec.Code, rec.Body.String())
	h.t.Cleanup(func() { _ = h.mgr.DropDatabase(context.Background(), name) })
}

func (h *apiHarness) login(database, user, password string) string {
	h.t.Helper()
	var res struct {
		Token string `json:"token"`
	}
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": database, "username": user, "password": password}, &res)
	require.Equal(h.t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(h.t, res.Token)
	return res.Token
}

// rebind turns $1-style placeholders into the engine's own.
func (h *apiHarness) rebind(q string) string {
	if testdb.Engine() == dbal.EnginePostgres {
		return q
	}
	for i := 9; i >= 1; i-- {
		q = strings.ReplaceAll(q, fmt.Sprintf("$%d", i), "?")
	}
	return q
}

func uniqueDB(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()%1_000_000_000)
}

func TestAPI_ProtectedRoutesRequireSession(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})

	protected := []struct{ method, target string }{
		{http.MethodGet, "/api/v1/schemas/tables"},
		{http.MethodPost, "/api/v1/schemas/tables"},
		{http.MethodGet, "/api/v1/schemas/layouts"},
		{http.MethodGet, "/api/v1/layouts/"},
		{http.MethodGet, "/api/v1/scripts/"},
		{http.MethodGet, "/api/v1/data/sys_users"},
		{http.MethodPost, "/api/v1/data/sys_users/find"},
		{http.MethodGet, "/api/v1/security/users"},
		{http.MethodPost, "/api/v1/security/users"},
		{http.MethodGet, "/api/v1/solutions/export"},
		{http.MethodGet, "/api/v1/solutions/export-data"},
		{http.MethodPost, "/api/v1/solutions/import"},
		{http.MethodDelete, "/api/v1/databases/anything"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodGet, "/api/v1/auth/session"},
	}
	for _, p := range protected {
		rec := h.do(p.method, p.target, "", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s without token", p.method, p.target)

		rec = h.do(p.method, p.target, "forged-token", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s with forged token", p.method, p.target)
	}

	// The database selector stays reachable before sign-in and reports no active database
	var list struct {
		Databases []string `json:"databases"`
		Active    string   `json:"active"`
	}
	rec := h.do(http.MethodGet, "/api/v1/databases", "", nil, &list)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "", list.Active)
	assert.NotContains(t, list.Databases, "postgres")
}

func TestAPI_DatabaseCreationAndLogin(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_auth")

	// Owner credentials are mandatory: there are no default accounts
	rec := h.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": db}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": "postgres", "user": "a", "password": "b-password"}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// The first owner's password must be chosen deliberately (issue #9)
	for _, weak := range []string{"   ", "short", "alice", db, strings.ToUpper(db)} {
		rec = h.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": db, "user": "alice", "password": weak}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "password %q", weak)
	}
	rec = h.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": db, "user": "alicealice", "password": "AliceAlice"}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "password equal to the username")

	h.createDatabase(db, "alice", "alice-secret")

	// Re-creating an existing database must not add another owner to it
	rec = h.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": db, "user": "mallory", "password": "mallory-secret"}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "mallory", "password": "x"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// No built-in admin/admin account
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "admin", "password": "admin"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// The owner password does not open the database under another (or no) username
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "bob", "password": "alice-secret"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "", "password": "alice-secret"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "alice", "password": "nope"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// System and unknown databases cannot be signed in to
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": "postgres", "username": "alice", "password": "alice-secret"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"username": "alice", "password": "alice-secret"}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	token := h.login(db, "alice", "alice-secret")

	var session struct {
		Database string `json:"database"`
		User     struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	rec = h.do(http.MethodGet, "/api/v1/auth/session", token, nil, &session)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, db, session.Database)
	assert.Equal(t, "alice", session.User.Username)
	assert.Equal(t, "owner", session.User.Role)

	// Logout invalidates the token
	rec = h.do(http.MethodPost, "/api/v1/auth/logout", token, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAPI_PublicDatabaseCreationCanBeDisabled(t *testing.T) {
	open := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_seed")
	open.createDatabase(db, "alice", "alice-secret")

	// Same server, creation locked down (shares the sessions of its own store)
	locked := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: false})
	other := uniqueDB("f4b_locked")
	rec := locked.do(http.MethodPost, "/api/v1/databases", "", map[string]string{"database": other, "user": "x", "password": "y"}, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	ownerToken := locked.login(db, "alice", "alice-secret")
	rec = locked.do(http.MethodPost, "/api/v1/databases", ownerToken, map[string]string{"database": other, "user": "x", "password": "x-owner-secret"}, nil)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	t.Cleanup(func() { _ = locked.mgr.DropDatabase(context.Background(), other) })
}

func TestAPI_SessionsAreIsolatedPerDatabase(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	dbA, dbB := uniqueDB("f4b_iso_a"), uniqueDB("f4b_iso_b")
	h.createDatabase(dbA, "alice", "secret-a")
	h.createDatabase(dbB, "bob", "secret-b")

	tokenA := h.login(dbA, "alice", "secret-a")
	// A second client signing in to another database must not redirect the first one
	tokenB := h.login(dbB, "bob", "secret-b")

	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", tokenA, map[string]string{"display_name": "Only In A", "custom_name": "only_in_a"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", tokenB, map[string]string{"display_name": "Only In B", "custom_name": "only_in_b"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	tableNames := func(token string) []string {
		var tables []struct {
			Name string `json:"name"`
		}
		rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables)
		require.Equal(t, http.StatusOK, rec.Code)
		names := make([]string, 0, len(tables))
		for _, tbl := range tables {
			names = append(names, tbl.Name)
		}
		return names
	}
	// Interleave the two sessions several times
	for i := 0; i < 3; i++ {
		assert.Equal(t, []string{"only_in_a"}, tableNames(tokenA))
		assert.Equal(t, []string{"only_in_b"}, tableNames(tokenB))
	}

	rec = h.do(http.MethodPost, "/api/v1/data/only_in_a", tokenA, map[string]interface{}{}, nil)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/data/only_in_a", tokenB, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, "database B has no such table")

	// "active" reflects each caller's own session
	var list struct {
		Active string `json:"active"`
	}
	h.do(http.MethodGet, "/api/v1/databases", tokenA, nil, &list)
	assert.Equal(t, dbA, list.Active)
	h.do(http.MethodGet, "/api/v1/databases", tokenB, nil, &list)
	assert.Equal(t, dbB, list.Active)

	// A session cannot be pointed at another database by parameter or header
	rec = h.do(http.MethodGet, "/api/v1/security/users?database="+dbB, tokenA, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/security/users", nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	req.Header.Set("X-Database-Name", dbB)
	recHdr := httptest.NewRecorder()
	h.router.ServeHTTP(recHdr, req)
	assert.Equal(t, http.StatusForbidden, recHdr.Code)

	// The legacy switch endpoint only reports existence and changes nothing
	rec = h.do(http.MethodPost, "/api/v1/databases/switch", tokenA, map[string]string{"database": dbB}, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"only_in_a"}, tableNames(tokenA))
	rec = h.do(http.MethodPost, "/api/v1/databases/switch", "", map[string]string{"database": "does_not_exist_db"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Dropping another database needs that database's owner credentials
	rec = h.do(http.MethodDelete, "/api/v1/databases/"+dbB, tokenA, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = h.do(http.MethodDelete, "/api/v1/databases/"+dbB, tokenA, map[string]string{"username": "bob", "password": "wrong"}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = h.do(http.MethodDelete, "/api/v1/databases/postgres", tokenA, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, []string{"only_in_b"}, tableNames(tokenB), "database B is untouched")

	rec = h.do(http.MethodDelete, "/api/v1/databases/"+dbB, tokenA, map[string]string{"username": "bob", "password": "secret-b"}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", tokenB, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "sessions of a dropped database are revoked")

	// An owner can drop the database of their own session
	rec = h.do(http.MethodDelete, "/api/v1/databases/"+dbA, tokenA, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", tokenA, nil, nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAPI_DataAPIOnlyServesCatalogTables(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_data")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	// Even the owner cannot read or write system tables through the data API
	for _, table := range []string{"sys_users", "sys_tables", "sys_user_permissions", "pg_user", "does_not_exist"} {
		rec := h.do(http.MethodGet, "/api/v1/data/"+table, token, nil, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, table)
		rec = h.do(http.MethodPost, "/api/v1/data/"+table+"/find", token, map[string]interface{}{"requests": []interface{}{}}, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, table)
	}
	rec := h.do(http.MethodPost, "/api/v1/data/sys_users", token, map[string]string{"id": "x", "username": "evil", "password_hash": "x", "role": "owner"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = h.do(http.MethodDelete, "/api/v1/data/sys_users/anything", token, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	var table struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Contacts", "custom_name": "contacts"}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", token, map[string]interface{}{"name": "full_name", "display_name": "Full Name", "field_type": "TEXT", "is_nullable": true}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var row map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/contacts", token, map[string]string{"full_name": "Ada"}, &row)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	id, _ := row["id"].(string)
	require.NotEmpty(t, id)

	// Unregistered fields are rejected instead of being spliced into SQL
	rec = h.do(http.MethodPost, "/api/v1/data/contacts", token, map[string]string{`full_name") VALUES ('x'); --`: "x"}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = h.do(http.MethodPut, "/api/v1/data/contacts/"+id, token, map[string]string{"no_such_field": "x"}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = h.do(http.MethodGet, "/api/v1/data/contacts?sort_by=no_such_field", token, nil, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var rows []map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/contacts/find", token, map[string]interface{}{
		"requests": []map[string]interface{}{{"criteria": []map[string]interface{}{{"field_name": "full_name", "operator": "=", "value": "ada"}}}},
	}, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, rows, 1)
}

func TestAPI_RolesAndLayoutPermissions(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_roles")
	h.createDatabase(db, "olivia", "owner-secret")
	owner := h.login(db, "olivia", "owner-secret")

	type userDTO struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	createUser := func(token, name, password, role string) (userDTO, int) {
		var u userDTO
		rec := h.do(http.MethodPost, "/api/v1/security/users", token, map[string]string{"username": name, "password": password, "role": role}, &u)
		return u, rec.Code
	}

	adminUser, code := createUser(owner, "adam", "admin-secret", "admin")
	require.Equal(t, http.StatusCreated, code)
	plainUser, code := createUser(owner, "uma", "user-secret", "user")
	require.Equal(t, http.StatusCreated, code)
	admin := h.login(db, "adam", "admin-secret")
	user := h.login(db, "uma", "user-secret")

	// Schema fixtures created by the owner
	var table struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner, map[string]string{"display_name": "Orders", "custom_name": "orders"}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var layout struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", owner, map[string]interface{}{"name": "Orders Form", "table_occurrence_id": table.ID, "definition": map[string]string{"theme": "default"}}, &layout)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/data/orders", owner, map[string]interface{}{}, nil)
	require.Equal(t, http.StatusCreated, rec.Code)

	t.Run("regular users cannot change the schema or manage accounts", func(t *testing.T) {
		denied := []struct {
			method, target string
			body           interface{}
		}{
			{http.MethodPost, "/api/v1/schemas/tables", map[string]string{"display_name": "X"}},
			{http.MethodDelete, "/api/v1/schemas/tables/" + table.ID, nil},
			{http.MethodPost, "/api/v1/schemas/tables/" + table.ID + "/truncate", nil},
			{http.MethodPost, "/api/v1/schemas/tables/" + table.ID + "/columns", map[string]string{"name": "x", "display_name": "X", "field_type": "TEXT"}},
			{http.MethodPost, "/api/v1/schemas/layouts", map[string]string{"name": "Mine"}},
			{http.MethodDelete, "/api/v1/schemas/layouts/" + layout.ID, nil},
			{http.MethodDelete, "/api/v1/layouts/" + layout.ID, nil},
			{http.MethodPost, "/api/v1/schemas/scripts", map[string]string{"name": "s"}},
			{http.MethodPost, "/api/v1/schemas/occurrences", map[string]string{"base_table_id": table.ID, "name": "o2"}},
			{http.MethodGet, "/api/v1/security/users", nil},
			{http.MethodPost, "/api/v1/security/users", map[string]string{"username": "x", "password": "y", "role": "owner"}},
			{http.MethodDelete, "/api/v1/security/users/" + adminUser.ID, nil},
			{http.MethodPut, "/api/v1/security/users/" + plainUser.ID + "/permissions", map[string]interface{}{"permissions": []interface{}{}}},
			{http.MethodGet, "/api/v1/security/users/" + adminUser.ID + "/permissions", nil},
			{http.MethodGet, "/api/v1/solutions/export-data", nil},
			{http.MethodGet, "/api/v1/solutions/export", nil},
			{http.MethodPost, "/api/v1/solutions/import-data", nil},
			{http.MethodDelete, "/api/v1/databases/" + db, nil},
		}
		for _, d := range denied {
			rec := h.do(d.method, d.target, user, d.body, nil)
			assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s", d.method, d.target)
		}

		// ...but they can read the catalog and their own permissions
		for _, target := range []string{"/api/v1/schemas/tables", "/api/v1/schemas/layouts", "/api/v1/schemas/relationships", "/api/v1/security/users/" + plainUser.ID + "/permissions"} {
			rec := h.do(http.MethodGet, target, user, nil, nil)
			assert.Equal(t, http.StatusOK, rec.Code, target)
		}
	})

	t.Run("a user can change only their own password", func(t *testing.T) {
		rec := h.do(http.MethodPut, "/api/v1/security/users/"+plainUser.ID, user, map[string]string{"role": "owner"}, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "self-promotion")
		rec = h.do(http.MethodPut, "/api/v1/security/users/"+adminUser.ID, user, map[string]string{"role": "admin", "password": "hijack"}, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "someone else's account")

		otherSession := h.login(db, "uma", "user-secret")
		rec = h.do(http.MethodPut, "/api/v1/security/users/"+plainUser.ID, user, map[string]string{"role": "user", "password": "user-secret-2"}, nil)
		assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = h.do(http.MethodGet, "/api/v1/schemas/tables", user, nil, nil)
		assert.Equal(t, http.StatusOK, rec.Code, "the session that changed the password stays signed in")
		rec = h.do(http.MethodGet, "/api/v1/schemas/tables", otherSession, nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "other sessions of that user are revoked")
		rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "uma", "password": "user-secret"}, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "old password no longer works")
	})

	t.Run("admins cannot create or take over owner accounts", func(t *testing.T) {
		_, code := createUser(admin, "eve", "x", "owner")
		assert.Equal(t, http.StatusForbidden, code)

		var users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		}
		rec := h.do(http.MethodGet, "/api/v1/security/users", admin, nil, &users)
		require.Equal(t, http.StatusOK, rec.Code)
		var ownerID string
		for _, u := range users {
			if u.Username == "olivia" {
				ownerID = u.ID
			}
		}
		require.NotEmpty(t, ownerID)

		rec = h.do(http.MethodPut, "/api/v1/security/users/"+ownerID, admin, map[string]string{"role": "owner", "password": "hijack"}, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		rec = h.do(http.MethodPut, "/api/v1/security/users/"+adminUser.ID, admin, map[string]string{"role": "owner"}, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		rec = h.do(http.MethodDelete, "/api/v1/security/users/"+ownerID, admin, nil, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code)
		rec = h.do(http.MethodPost, "/api/v1/solutions/import", admin, nil, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "importing a solution can create accounts: owners only")

		// The only owner cannot lock the database by demoting themselves
		rec = h.do(http.MethodPut, "/api/v1/security/users/"+ownerID, owner, map[string]string{"role": "user"}, nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		rec = h.do(http.MethodDelete, "/api/v1/security/users/"+ownerID, owner, nil, nil)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	setPermission := func(level string) {
		rec := h.do(http.MethodPut, "/api/v1/security/users/"+plainUser.ID+"/permissions", admin,
			map[string]interface{}{"permissions": []map[string]string{{"layout_id": layout.ID, "access_level": level}}}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	layoutIDs := func(token string) []string {
		var layouts []struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodGet, "/api/v1/schemas/layouts", token, nil, &layouts)
		require.Equal(t, http.StatusOK, rec.Code)
		ids := make([]string, 0, len(layouts))
		for _, l := range layouts {
			ids = append(ids, l.ID)
		}
		return ids
	}
	saveLayout := map[string]interface{}{"name": "Orders Form", "definition": map[string]string{"theme": "edited"}}

	t.Run("layout permissions are enforced by the server", func(t *testing.T) {
		// Default (no explicit row): read_write
		assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/orders", user, nil, nil).Code)
		assert.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/orders", user, map[string]interface{}{}, nil).Code)
		assert.Equal(t, http.StatusOK, h.do(http.MethodPut, "/api/v1/schemas/layouts/"+layout.ID, user, saveLayout, nil).Code)

		setPermission("read_only")
		assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/orders", user, nil, nil).Code)
		assert.Equal(t, http.StatusOK, h.do(http.MethodPost, "/api/v1/data/orders/find", user, map[string]interface{}{"requests": []interface{}{}}, nil).Code)
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodPost, "/api/v1/data/orders", user, map[string]interface{}{}, nil).Code)
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodPut, "/api/v1/data/orders/any", user, map[string]interface{}{}, nil).Code)
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodDelete, "/api/v1/data/orders/any", user, nil, nil).Code)
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodPut, "/api/v1/schemas/layouts/"+layout.ID, user, saveLayout, nil).Code)
		assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/layouts/"+layout.ID, user, nil, nil).Code)
		assert.Contains(t, layoutIDs(user), layout.ID)

		setPermission("none")
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodGet, "/api/v1/data/orders", user, nil, nil).Code)
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodPost, "/api/v1/data/orders/find", user, map[string]interface{}{"requests": []interface{}{}}, nil).Code)
		assert.Equal(t, http.StatusForbidden, h.do(http.MethodGet, "/api/v1/schemas/layouts/"+layout.ID, user, nil, nil).Code)
		assert.NotContains(t, layoutIDs(user), layout.ID)
		assert.Contains(t, layoutIDs(admin), layout.ID, "admins always see every layout")

		// Admins and owners are never restricted by layout permissions
		assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/orders", admin, nil, nil).Code)

		setPermission("read_write")
		assert.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/orders", user, map[string]interface{}{}, nil).Code)
	})

	t.Run("account changes revoke the affected sessions", func(t *testing.T) {
		inactive := false
		rec := h.do(http.MethodPut, "/api/v1/security/users/"+plainUser.ID, admin, map[string]interface{}{"role": "user", "is_active": inactive}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		rec = h.do(http.MethodGet, "/api/v1/schemas/tables", user, nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "a deactivated user is signed out immediately")
		rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "uma", "password": "user-secret-2"}, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)

		rec = h.do(http.MethodDelete, "/api/v1/security/users/"+adminUser.ID, owner, nil, nil)
		require.Equal(t, http.StatusNoContent, rec.Code)
		rec = h.do(http.MethodGet, "/api/v1/schemas/tables", admin, nil, nil)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "a deleted user is signed out immediately")
	})
}

func TestAPI_DataImportCannotWriteSystemTables(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_import")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Notes", "custom_name": "notes"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// A crafted data file tries to forge an owner account next to a legitimate row
	forged, err := msgpack.Marshal(schema.DatabaseDataBundle{
		Format:       "file4base_data",
		Version:      "1.0",
		DatabaseName: db,
		TablesData: map[string][]map[string]interface{}{
			"notes": {{"id": "note-1"}},
			"sys_users": {{
				"id":            "forged-owner",
				"username":      "mallory",
				"password_hash": "$2a$10$abcdefghijklmnopqrstuuJ0JmJ1xqUeUoQyqfXK6oD5kq3uXcW6e",
				"role":          "owner",
			}},
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/import-data", bytes.NewReader(forged))
	req.Header.Set("Authorization", "Bearer "+token)
	recImport := httptest.NewRecorder()
	h.router.ServeHTTP(recImport, req)
	require.Equal(t, http.StatusOK, recImport.Code, recImport.Body.String())

	var result struct {
		TablesRestored int `json:"tables_restored"`
		RecordsCount   int `json:"records_count"`
	}
	require.NoError(t, json.Unmarshal(recImport.Body.Bytes(), &result))
	assert.Equal(t, 1, result.TablesRestored, "only the catalog table is restored")
	assert.Equal(t, 1, result.RecordsCount)

	var users []struct {
		Username string `json:"username"`
	}
	rec = h.do(http.MethodGet, "/api/v1/security/users", token, nil, &users)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Username)

	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/notes", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, rows, 1)
}

// A limit above the maximum page size is clamped to it instead of falling
// back to the 100-row default, and pages by offset cover every row (#6).
func TestAPI_ListRowsLimitIsClampedAndPagesCoverAllRows(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_page")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	var table struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Items", "custom_name": "items"}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	driver, err := h.mgr.DriverFor(context.Background(), db)
	require.NoError(t, err)
	// Seeded directly, in one multi-row INSERT that every engine accepts
	var b strings.Builder
	b.WriteString("INSERT INTO items (id) VALUES ")
	for i := 1; i <= 1005; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "('r%05d')", i)
	}
	_, err = driver.DB().ExecContext(context.Background(), b.String())
	require.NoError(t, err)

	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/items?limit=10000", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, rows, 1000, "an oversized limit returns a full page, not the 100-row default")

	seen := map[string]bool{}
	for offset := 0; ; {
		rows = nil
		rec = h.do(http.MethodGet, fmt.Sprintf("/api/v1/data/items?limit=1000&offset=%d&sort_by=id", offset), token, nil, &rows)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		for _, r := range rows {
			seen[r["id"].(string)] = true
		}
		offset += len(rows)
		if len(rows) < 1000 {
			break
		}
	}
	assert.Len(t, seen, 1005)
}

// JSON numbers reach NUMBER fields exactly, without a float64 round trip (#18).
func TestAPI_JSONNumbersKeepTheirPrecision(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_num")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	var table struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Typed", "custom_name": "typed"}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for _, col := range []map[string]interface{}{
		{"name": "amount", "display_name": "Amount", "field_type": "NUMBER", "is_nullable": true},
		{"name": "note", "display_name": "Note", "field_type": "TEXT", "is_nullable": true},
		{"name": "flag", "display_name": "Flag", "field_type": "BOOLEAN", "is_nullable": true},
	} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", token, col, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	post := func(id, rawJSON string) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/data/typed", strings.NewReader(rawJSON))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.router.ServeHTTP(rr, req)
		require.Equal(t, http.StatusCreated, rr.Code, "%s: %s", id, rr.Body.String())
	}
	post("big", `{"id":"big","amount":9007199254740993}`)
	post("neg", `{"id":"neg","amount":-9007199254740993}`)
	post("dec", `{"id":"dec","amount":12345678901234.123456789}`)
	post("exp", `{"id":"exp","amount":1.5e3}`)
	post("txt", `{"id":"txt","note":42}`)

	// The PUT path decodes the same way
	req := httptest.NewRequest(http.MethodPut, "/api/v1/data/typed/dec", strings.NewReader(`{"amount":9007199254740995}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var rows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/typed?sort_by=id", token, nil, &rows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := map[string]interface{}{}
	for _, r := range rows {
		if r["amount"] != nil {
			got[r["id"].(string)] = fmt.Sprint(r["amount"])
		} else {
			got[r["id"].(string)] = fmt.Sprint(r["note"])
		}
	}
	assert.Equal(t, "9007199254740993", got["big"])
	assert.Equal(t, "-9007199254740993", got["neg"])
	assert.Equal(t, "9007199254740995", got["dec"])
	assert.Equal(t, "1500", got["exp"])
	assert.Equal(t, "42", got["txt"])

	// A BOOLEAN reads back as a JSON boolean on every engine
	post("bool", `{"id":"bool","flag":true}`)
	var one map[string]interface{}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/typed/bool", token, nil, &one).Code)
	assert.Equal(t, true, one["flag"])
}

// default_value holds field options (a JSON object). It is never placed in
// DDL, so SQL in it cannot run (#1); options survive table duplication and
// a constant auto-enter value fills new records (#16).
func TestAPI_FieldOptionsAreMetadataNotSQL(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_opts")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")
	ctx := context.Background()
	driver, err := h.mgr.DriverFor(ctx, db)
	require.NoError(t, err)

	var table struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Configured", "custom_name": "configured"}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	physicalColumns := func(tbl string) map[string]sql.NullString {
		q := `SELECT column_name, column_default FROM information_schema.columns WHERE table_name = $1`
		if driver.Dialect().Engine() != dbal.EnginePostgres {
			q = `SELECT column_name, column_default FROM information_schema.columns WHERE table_name = ? AND table_schema = DATABASE()`
		}
		rows, err := driver.DB().QueryContext(ctx, q, tbl)
		require.NoError(t, err)
		defer rows.Close()
		cols := map[string]sql.NullString{}
		for rows.Next() {
			var name string
			var def sql.NullString
			require.NoError(t, rows.Scan(&name, &def))
			cols[name] = def
		}
		return cols
	}

	// SQL expressions and other non-JSON values are rejected before any DDL
	for _, payload := range []string{
		`'x'`,
		`now()`,
		`'a'); DROP TABLE configured; --`,
		`(SELECT current_user)`,
		`[1,2]`,
	} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", token, map[string]interface{}{
			"name": "evil", "display_name": "Evil", "field_type": "TEXT", "is_nullable": true, "default_value": payload,
		}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, payload)
	}
	_, exists := physicalColumns("configured")["evil"]
	assert.False(t, exists, "a rejected column is not created")

	// Options with quotes, semicolons and comment markers stay literal text
	tricky := `{"data_enabled":true,"data_value":"Pending '; DROP TABLE configured; --","serial_enabled":false}`
	var col struct {
		ID           string `json:"id"`
		DefaultValue string `json:"default_value"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", token, map[string]interface{}{
		"name": "label", "display_name": "Label", "field_type": "TEXT", "is_nullable": true, "default_value": tricky,
	}, &col)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, tricky, col.DefaultValue)
	cols := physicalColumns("configured")
	require.Contains(t, cols, "label")
	// MariaDB reports the literal "NULL" for a nullable column with no default
	assert.True(t, !cols["label"].Valid || cols["label"].String == "NULL",
		"no physical DEFAULT is created from options, got %q", cols["label"].String)

	// Updating options validates them the same way
	rec = h.do(http.MethodPut, "/api/v1/schemas/tables/"+table.ID+"/columns/"+col.ID, token, map[string]interface{}{
		"display_name": "Label", "default_value": `now()`,
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	rec = h.do(http.MethodPut, "/api/v1/schemas/tables/"+table.ID+"/columns/"+col.ID, token, map[string]interface{}{
		"display_name": "Label", "default_value": `{"data_enabled":true,"data_value":"Pending","serial_enabled":false}`,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The constant auto-enter value fills records that do not supply the field
	var row map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/configured", token, map[string]string{"id": "auto"}, &row)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/data/configured/auto", token, nil, &row)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Pending", row["label"])
	rec = h.do(http.MethodPost, "/api/v1/data/configured", token, map[string]string{"id": "explicit", "label": "Given"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/data/configured/explicit", token, nil, &row)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Given", row["label"], "an explicit value wins over the auto-enter constant")

	// Duplication copies the column and its options
	var dup struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Columns []struct {
			Name         string `json:"name"`
			DefaultValue string `json:"default_value"`
		} `json:"columns"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/duplicate", token, nil, &dup)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var tables []struct {
		ID      string `json:"id"`
		Columns []struct {
			Name         string `json:"name"`
			DefaultValue string `json:"default_value"`
		} `json:"columns"`
	}
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables)
	require.Equal(t, http.StatusOK, rec.Code)
	var copied map[string]string
	for _, tb := range tables {
		if tb.ID == dup.ID {
			copied = map[string]string{}
			for _, c := range tb.Columns {
				copied[c.Name] = c.DefaultValue
			}
		}
	}
	require.NotNil(t, copied, "the copy is listed")
	assert.Contains(t, copied, "id")
	assert.JSONEq(t, `{"data_enabled":true,"data_value":"Pending","serial_enabled":false}`, copied["label"])
}

// Internal tables cannot be registered as user tables, names longer than the
// engine's identifier limit are rejected instead of colliding, and an existing
// physical table is never adopted (#2).
func TestAPI_ReservedAndCollidingTableNames(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_names")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")
	ctx := context.Background()
	driver, err := h.mgr.DriverFor(ctx, db)
	require.NoError(t, err)

	countUsers := func() int {
		var n int
		require.NoError(t, driver.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sys_users`).Scan(&n))
		return n
	}
	usersBefore := countUsers()

	for _, body := range []map[string]string{
		{"display_name": "Users", "custom_name": "sys_users"},
		{"display_name": "Users", "custom_name": "  SYS_Users "},
		{"display_name": "sys users"}, // name derived from the display name
		{"display_name": "Perms", "custom_name": "sys_user_permissions"},
		{"display_name": "Layouts", "custom_name": "sys_layouts"},
		{"display_name": "Catalog", "custom_name": "pg_user"},
		{"display_name": "Long", "custom_name": strings.Repeat("a", 64)},
	} {
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", token, body, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%v: %s", body, rec.Body.String())
	}
	var tables []struct {
		Name string `json:"name"`
	}
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, tables, "no rejected name reached the catalog")
	assert.Equal(t, usersBefore, countUsers())

	// 63 characters is the limit; the same name twice is a conflict
	long := strings.Repeat("a", 63)
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Long", "custom_name": long}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Long 2", "custom_name": long}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// Duplicating a 63-character table keeps the copy's name within the limit
	var listed []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &listed)
	longID := ""
	for _, tb := range listed {
		if tb.Name == long {
			longID = tb.ID
		}
	}
	require.NotEmpty(t, longID)
	var dup struct {
		Name string `json:"name"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+longID+"/duplicate", token, nil, &dup)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.LessOrEqual(t, len(dup.Name), 63)
	assert.True(t, strings.HasSuffix(dup.Name, "_copy"), dup.Name)

	// A physical table outside the catalog is not adopted and keeps its data
	legacyDDL := `CREATE TABLE legacy (id TEXT PRIMARY KEY, secret TEXT)`
	if driver.Dialect().Engine() != dbal.EnginePostgres {
		legacyDDL = "CREATE TABLE legacy (id VARCHAR(64) PRIMARY KEY, secret TEXT)"
	}
	_, err = driver.DB().ExecContext(ctx, legacyDDL)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(ctx, `INSERT INTO legacy VALUES ('k', 'v')`)
	require.NoError(t, err)
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", token, map[string]string{"display_name": "Legacy", "custom_name": "legacy"}, nil)
	assert.GreaterOrEqual(t, rec.Code, 400, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/data/legacy", token, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, "the existing table did not join the catalog")
	var secret string
	require.NoError(t, driver.DB().QueryRowContext(ctx, `SELECT secret FROM legacy WHERE id = 'k'`).Scan(&secret))
	assert.Equal(t, "v", secret)

	// Even a forged catalog row cannot expose an internal table
	now := time.Now().UTC()
	_, err = driver.DB().ExecContext(ctx,
		h.rebind(`INSERT INTO sys_tables (id, name, display_name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`),
		"forged", "sys_users", "Users", now, now)
	require.NoError(t, err)
	_, err = driver.DB().ExecContext(ctx,
		h.rebind(`INSERT INTO sys_columns (id, table_id, name, display_name, field_type, is_nullable, is_primary_key, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`),
		"forged-id", "forged", "id", "ID", "TEXT", false, true, now)
	require.NoError(t, err)
	rec = h.do(http.MethodGet, "/api/v1/data/sys_users", token, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/data/sys_users", token, map[string]string{"id": "x", "username": "mallory"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	forged, err := msgpack.Marshal(schema.DatabaseDataBundle{
		Format: "file4base_data", Version: "1.0", DatabaseName: db,
		TablesData: map[string][]map[string]interface{}{
			"sys_users": {{"id": "forged-owner", "username": "mallory", "password_hash": "x", "role": "owner"}},
		},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/solutions/import-data", bytes.NewReader(forged))
	req.Header.Set("Authorization", "Bearer "+token)
	h.router.ServeHTTP(httptest.NewRecorder(), req)
	assert.Equal(t, usersBefore, countUsers(), "the import did not write into sys_users")
}

// Import files that declare collections larger than their bytes are rejected
// before the MessagePack decoder allocates from those sizes (#13).
func TestAPI_ImportsRejectOversizedMessagePackDeclarations(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_mpk")
	h.createDatabase(db, "alice", "alice-secret")
	token := h.login(db, "alice", "alice-secret")

	// {"tables": <array32 declaring 2^31-1 elements>} in 13 bytes
	bomb := []byte{0x81, 0xa6, 't', 'a', 'b', 'l', 'e', 's', 0xdd, 0x7f, 0xff, 0xff, 0xff}
	for _, path := range []string{"/api/v1/solutions/import", "/api/v1/solutions/import-data"} {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(bomb))
		req.Header.Set("Content-Type", "application/x-msgpack")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s: %s", path, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "invalid MessagePack", path)
	}

	var tables []interface{}
	rec := h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, &tables)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, tables, "a rejected import changes nothing")
}

// A sign-in that read the account before a security change and creates its
// session after the change was applied (and the user's sessions revoked)
// must not yield a usable session (#12). The interleaving is reproduced
// step by step: read the account state, change it, then create the session
// with the state read before.
func TestAPI_SessionsFromBeforeAnAccountChangeAreRejected(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_stale")
	h.createDatabase(db, "olivia", "olivia-secret")
	owner := h.login(db, "olivia", "olivia-secret")
	ctx := context.Background()
	driver, err := h.mgr.DriverFor(ctx, db)
	require.NoError(t, err)
	svc := schema.NewService(driver)

	newUser := func(name, role string) string {
		var u idOnly
		rec := h.do(http.MethodPost, "/api/v1/security/users", owner, map[string]interface{}{"username": name, "password": name + "-secret", "role": role}, &u)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		return u.ID
	}
	// staleSession simulates the in-flight sign-in: it captured the stamp
	// before the change and registers its session after it.
	staleSession := func(userID, name, role string, change func()) string {
		before, err := svc.CurrentAccountStamp(ctx, userID)
		require.NoError(t, err)
		change()
		token, _, err := h.sessions.Create(userID, name, role, db, before)
		require.NoError(t, err)
		return token
	}
	usable := func(token string) bool {
		return h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, nil).Code == http.StatusOK
	}

	cases := []struct {
		name, role string
		change     func(id string)
	}{
		{"rotated", "admin", func(id string) {
			require.Equal(t, http.StatusOK, h.do(http.MethodPut, "/api/v1/security/users/"+id, owner, map[string]string{"password": "brand-new-secret"}, nil).Code)
		}},
		{"demoted", "admin", func(id string) {
			require.Equal(t, http.StatusOK, h.do(http.MethodPut, "/api/v1/security/users/"+id, owner, map[string]string{"role": "user"}, nil).Code)
		}},
		{"disabled", "user", func(id string) {
			require.Equal(t, http.StatusOK, h.do(http.MethodPut, "/api/v1/security/users/"+id, owner, map[string]interface{}{"role": "user", "is_active": false}, nil).Code)
		}},
		{"deleted", "user", func(id string) {
			require.Equal(t, http.StatusNoContent, h.do(http.MethodDelete, "/api/v1/security/users/"+id, owner, nil, nil).Code)
		}},
	}
	for _, c := range cases {
		id := newUser(c.name, c.role)
		normal := h.login(db, c.name, c.name+"-secret")
		require.True(t, usable(normal), c.name)
		stale := staleSession(id, c.name, c.role, func() { c.change(id) })
		assert.False(t, usable(stale), "%s: the session created after the change is rejected", c.name)
		assert.False(t, usable(normal), "%s: the session opened before the change is rejected", c.name)
		assert.False(t, usable(stale), "%s: and stays rejected", c.name)
	}

	// Changing your own password keeps the session you changed it from,
	// and only that one.
	id := newUser("selfie", "user")
	current := h.login(db, "selfie", "selfie-secret")
	other := h.login(db, "selfie", "selfie-secret")
	rec := h.do(http.MethodPut, "/api/v1/security/users/"+id, current, map[string]string{"password": "selfie-new-secret"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, usable(current), "the session used for the change stays valid")
	assert.False(t, usable(other), "other sessions of the account are closed")
	h.login(db, "selfie", "selfie-new-secret")

	// The owner's own session is unaffected throughout
	assert.True(t, usable(owner))
}
