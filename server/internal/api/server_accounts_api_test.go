package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverAccountView is the part of a server account the API answers.
type serverAccountView struct {
	Name         string   `json:"name"`
	Active       bool     `json:"active"`
	SuperOwner   bool     `json:"super_owner"`
	AllDatabases bool     `json:"all_databases"`
	Databases    []string `json:"databases"`
}

func TestServerAccountsAPI_SuperOwnerManagesAccounts(t *testing.T) {
	// The owner of the admin database is an effective super-owner here.
	s := newServerToolsHarness(t, true)

	// Create an account that reaches one database, with a password the
	// response must never echo.
	var created serverAccountView
	rec := s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, map[string]interface{}{
		"name":      "Clerk",
		"password":  "clerk-pass-1",
		"databases": []string{s.aliceDB},
	}, &created)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Clerk", created.Name)
	assert.True(t, created.Active)
	assert.Equal(t, []string{s.aliceDB}, created.Databases)
	assert.NotContains(t, rec.Body.String(), "clerk-pass-1", "a password is never answered")
	assert.NotContains(t, rec.Body.String(), "password", "no password field is answered")

	// The same name, in another case, is refused.
	rec = s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, map[string]interface{}{
		"name": "CLERK", "password": "other-pass-1", "all_databases": true,
	}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())

	// A name with "@" belongs to a directory, not to the server.
	rec = s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, map[string]interface{}{
		"name": "jdoe@empresa.com", "password": "pw-one-1", "all_databases": true,
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// A database that is not registered is refused, naming the parameter.
	rec = s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, map[string]interface{}{
		"name": "ghosted", "password": "pw-one-1", "databases": []string{"no_such_db"},
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "databases")

	// A password longer than bcrypt's limit is a validation error, not a crash.
	rec = s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, map[string]interface{}{
		"name": "long_pass", "password": strings.Repeat("x", 73), "all_databases": true,
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	// The list and the single account answer the same identity.
	var list []serverAccountView
	rec = s.do(http.MethodGet, "/api/v1/server/accounts", s.adminToken, nil, &list)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, list, 1)
	assert.Equal(t, "Clerk", list[0].Name)

	var one serverAccountView
	rec = s.do(http.MethodGet, "/api/v1/server/accounts/clerk", s.adminToken, nil, &one)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Clerk", one.Name)

	rec = s.do(http.MethodGet, "/api/v1/server/accounts/ghost", s.adminToken, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Disable, change scope, change password, then delete.
	rec = s.do(http.MethodPut, "/api/v1/server/accounts/Clerk/active", s.adminToken,
		map[string]bool{"active": false}, &one)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, one.Active)

	rec = s.do(http.MethodPut, "/api/v1/server/accounts/Clerk/scope", s.adminToken, map[string]interface{}{
		"all_databases": true,
	}, &one)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, one.AllDatabases)
	assert.Empty(t, one.Databases)

	rec = s.do(http.MethodPut, "/api/v1/server/accounts/Clerk/password", s.adminToken,
		map[string]string{"password": "clerk-pass-2"}, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	rec = s.do(http.MethodDelete, "/api/v1/server/accounts/Clerk", s.adminToken, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	rec = s.do(http.MethodGet, "/api/v1/server/accounts/Clerk", s.adminToken, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServerAccountsAPI_LastSuperOwnerIsProtected(t *testing.T) {
	s := newServerToolsHarness(t, true)

	// A server-level super-owner that is the only one: it cannot be disabled.
	rec := s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, map[string]interface{}{
		"name": "root2", "password": "root2-pass-1", "super_owner": true, "all_databases": true,
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = s.do(http.MethodPut, "/api/v1/server/accounts/root2/active", s.adminToken,
		map[string]bool{"active": false}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "super-owner")
}

func TestServerAccountsAPI_OnlySuperOwnersReach(t *testing.T) {
	s := newServerToolsHarness(t, true)

	// An ordinary owner of one database is not a super-owner.
	rec := s.do(http.MethodGet, "/api/v1/server/accounts", s.aliceToken, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = s.do(http.MethodPost, "/api/v1/server/accounts", s.aliceToken, map[string]interface{}{
		"name": "sneaky", "password": "sneaky-pass-1", "all_databases": true,
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
