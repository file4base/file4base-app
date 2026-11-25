package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type signInReply struct {
	Token    string `json:"token"`
	Database string `json:"database"`
	User     struct {
		Username      string `json:"username"`
		Role          string `json:"role"`
		SuperOwner    bool   `json:"super_owner"`
		ServerAccount bool   `json:"server_account"`
	} `json:"user"`
}

func signIn(t *testing.T, s *serverToolsHarness, database, username, password string) (int, signInReply) {
	t.Helper()
	var reply signInReply
	rec := s.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"database": database, "username": username, "password": password,
	}, &reply)
	return rec.Code, reply
}

func createServerAccount(t *testing.T, s *serverToolsHarness, body map[string]interface{}) {
	t.Helper()
	rec := s.do(http.MethodPost, "/api/v1/server/accounts", s.adminToken, body, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestServerAccountLogin_SignsInToReachedDatabasesOnly(t *testing.T) {
	s := newServerToolsHarness(t, true)
	createServerAccount(t, s, map[string]interface{}{
		"name": "Clerk", "password": "clerk-pass-1", "databases": []string{s.aliceDB},
	})

	code, reply := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-1")
	require.Equal(t, http.StatusOK, code)
	assert.NotEmpty(t, reply.Token)
	assert.Equal(t, s.aliceDB, reply.Database)
	assert.Equal(t, "Clerk", reply.User.Username)
	assert.True(t, reply.User.ServerAccount)
	assert.Equal(t, "user", reply.User.Role)
	assert.False(t, reply.User.SuperOwner)

	// The session works on the database it signed in to.
	rec := s.do(http.MethodGet, "/api/v1/server/access", reply.Token, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// A database the account does not reach refuses the same way as a wrong password.
	codeOther, _ := signIn(t, s, s.bobDB, "clerk", "clerk-pass-1")
	codeWrong, _ := signIn(t, s, s.aliceDB, "clerk", "not-the-password")
	assert.Equal(t, http.StatusUnauthorized, codeOther)
	assert.Equal(t, http.StatusUnauthorized, codeWrong)
}

func TestServerAccountLogin_LeftoverRowNeverTakesThePassword(t *testing.T) {
	s := newServerToolsHarness(t, true)
	createServerAccount(t, s, map[string]interface{}{
		"name": "clerk", "password": "clerk-pass-1", "databases": []string{s.aliceDB},
	})
	code, _ := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-1")
	require.Equal(t, http.StatusOK, code)

	// Deleting the account leaves its row in the database. That row must not
	// accept the old password through the database's own check.
	rec := s.do(http.MethodDelete, "/api/v1/server/accounts/clerk", s.adminToken, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	code, _ = signIn(t, s, s.aliceDB, "clerk", "clerk-pass-1")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestServerAccountLogin_SessionEndsWithAccountChanges(t *testing.T) {
	s := newServerToolsHarness(t, true)
	createServerAccount(t, s, map[string]interface{}{
		"name": "clerk", "password": "clerk-pass-1", "databases": []string{s.aliceDB},
	})
	_, reply := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-1")
	require.NotEmpty(t, reply.Token)
	access := func(token string) int {
		return s.do(http.MethodGet, "/api/v1/server/access", token, nil, nil).Code
	}
	require.Equal(t, http.StatusOK, access(reply.Token))

	// Disabling the account ends its session on the next request.
	rec := s.do(http.MethodPut, "/api/v1/server/accounts/clerk/active", s.adminToken, map[string]bool{"active": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusUnauthorized, access(reply.Token))
	codeDisabled, _ := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-1")
	assert.Equal(t, http.StatusUnauthorized, codeDisabled)

	rec = s.do(http.MethodPut, "/api/v1/server/accounts/clerk/active", s.adminToken, map[string]bool{"active": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	_, again := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-1")
	require.NotEmpty(t, again.Token)
	require.Equal(t, http.StatusOK, access(again.Token))

	// A new password ends the session opened with the old one.
	rec = s.do(http.MethodPut, "/api/v1/server/accounts/clerk/password", s.adminToken, map[string]string{"password": "clerk-pass-2"}, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusUnauthorized, access(again.Token))

	// A new role takes effect at the next sign-in, and ends the session before it.
	_, third := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-2")
	require.NotEmpty(t, third.Token)
	rec = s.do(http.MethodPut, "/api/v1/server/accounts/clerk/role", s.adminToken, map[string]string{"role": "admin"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusUnauthorized, access(third.Token))
	_, fourth := signIn(t, s, s.aliceDB, "clerk", "clerk-pass-2")
	assert.Equal(t, "admin", fourth.User.Role)

	// Deleting the account ends every session it has.
	rec = s.do(http.MethodDelete, "/api/v1/server/accounts/clerk", s.adminToken, nil, nil)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusUnauthorized, access(fourth.Token))
}

func TestServerAccountLogin_NameUsedByDatabaseAccountIsRefused(t *testing.T) {
	s := newServerToolsHarness(t, true)
	// bob is a database account of bobDB, so a server account of that name
	// cannot be signed in there without merging the two.
	createServerAccount(t, s, map[string]interface{}{
		"name": "bob", "password": "server-bob-1", "databases": []string{s.bobDB},
	})
	code, _ := signIn(t, s, s.bobDB, "bob", "server-bob-1")
	assert.Equal(t, http.StatusConflict, code)
}

func TestServerAccountLogin_SuperOwnerAccountReachesServerTools(t *testing.T) {
	s := newServerToolsHarness(t, true)
	createServerAccount(t, s, map[string]interface{}{
		"name": "root3", "password": "root3-pass-1", "super_owner": true, "all_databases": true,
	})
	code, reply := signIn(t, s, s.aliceDB, "root3", "root3-pass-1")
	require.Equal(t, http.StatusOK, code)
	assert.True(t, reply.User.SuperOwner)

	rec := s.do(http.MethodGet, "/api/v1/server/accounts", reply.Token, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}
