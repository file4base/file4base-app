package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
)

type accessView struct {
	ServerAdmin   bool `json:"server_admin"`
	SuperOwner    bool `json:"super_owner"`
	Configured    bool `json:"admin_database_configured"`
	DatabaseTools bool `json:"database_tools"`
}

// superOwnerHarness has a database whose owner is a bootstrap super-owner
// (seeded by FILE4BASE_SUPER_OWNER, with no admin database configured) and a
// second, unrelated database whose owner holds no server-wide privilege.
func newSuperOwnerHarness(t *testing.T) (*apiHarness, string, string, string, string) {
	t.Helper()
	opsDB, otherDB := uniqueDB("ops"), uniqueDB("other")
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		BackupsDir:                  t.TempDir(),
		// Bootstrap root@ops as a super-owner; deliberately no AdminDatabase.
		SuperOwnerSeed: []string{opsDB + ":root"},
	})
	h.createDatabase(opsDB, "root", "root-pass-1")
	h.createDatabase(otherDB, "boss", "boss-pass-1")
	rootToken := h.login(opsDB, "root", "root-pass-1")
	bossToken := h.login(otherDB, "boss", "boss-pass-1")
	return h, opsDB, otherDB, rootToken, bossToken
}

func TestSuperOwner_SeedGrantsServerWideAccessWithoutAdminDatabase(t *testing.T) {
	h, _, _, rootToken, bossToken := newSuperOwnerHarness(t)

	var rootAccess, bossAccess accessView
	h.do(http.MethodGet, "/api/v1/server/access", rootToken, nil, &rootAccess)
	assert.True(t, rootAccess.SuperOwner, "the seeded owner signs in as a super-owner")
	assert.True(t, rootAccess.ServerAdmin, "a super-owner may use the server-wide tools")
	assert.False(t, rootAccess.Configured, "no admin database is configured")

	h.do(http.MethodGet, "/api/v1/server/access", bossToken, nil, &bossAccess)
	assert.False(t, bossAccess.SuperOwner)
	assert.False(t, bossAccess.ServerAdmin, "an ordinary owner is not a server administrator")

	// A server-wide tool is reachable for the super-owner, refused for the other owner.
	assert.Equal(t, http.StatusOK,
		h.do(http.MethodGet, "/api/v1/server/sessions", rootToken, nil, nil).Code)
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodGet, "/api/v1/server/sessions", bossToken, nil, nil).Code)
}

func TestSuperOwner_AdminDatabaseOwnerManagesWithoutSeed(t *testing.T) {
	// FILE4BASE_ADMIN_DATABASE is set, FILE4BASE_SUPER_OWNER is not: the owner
	// of the admin database must still be able to grant super-owner from the UI
	// (the bootstrap gap this fixes).
	s := newServerToolsHarness(t, true)

	var access accessView
	s.do(http.MethodGet, "/api/v1/server/access", s.adminToken, nil, &access)
	assert.True(t, access.SuperOwner, "an owner of the admin database is an effective super-owner")
	assert.True(t, access.ServerAdmin)

	// An owner of an ordinary database is neither.
	s.do(http.MethodGet, "/api/v1/server/access", s.aliceToken, nil, &access)
	assert.False(t, access.SuperOwner)
	assert.False(t, access.ServerAdmin)

	// The admin-database owner creates another owner there and grants it
	// super-owner — no FILE4BASE_SUPER_OWNER seed was needed.
	var created struct {
		ID string `json:"id"`
	}
	rec := s.do(http.MethodPost, "/api/v1/security/users", s.adminToken,
		map[string]interface{}{"username": "deputy", "password": "deputy-pass-1", "role": "owner"}, &created)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = s.do(http.MethodPut, "/api/v1/security/users/"+created.ID+"/super-owner", s.adminToken,
		map[string]bool{"super_owner": true}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// An ordinary owner of another database still cannot grant it.
	var aliceOwner struct {
		ID string `json:"id"`
	}
	s.do(http.MethodPost, "/api/v1/security/users", s.aliceToken,
		map[string]interface{}{"username": "alice2", "password": "alice2-pass-1", "role": "owner"}, &aliceOwner)
	rec = s.do(http.MethodPut, "/api/v1/security/users/"+aliceOwner.ID+"/super-owner", s.aliceToken,
		map[string]bool{"super_owner": true}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a non-server-admin owner cannot grant super-owner")
}

func TestSuperOwner_GrantIsPersistedAndRestrictedToSuperOwners(t *testing.T) {
	h, opsDB, otherDB, rootToken, bossToken := newSuperOwnerHarness(t)

	// The super-owner creates a second owner in its own database.
	var created struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/security/users", rootToken,
		map[string]interface{}{"username": "deputy", "password": "deputy-pass-1", "role": "owner"}, &created)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.NotEmpty(t, created.ID)

	// Before any grant, the deputy is a plain owner, not a super-owner.
	deputyToken := h.login(opsDB, "deputy", "deputy-pass-1")
	var deputyAccess accessView
	h.do(http.MethodGet, "/api/v1/server/access", deputyToken, nil, &deputyAccess)
	assert.False(t, deputyAccess.SuperOwner)

	// Only a super-owner may grant the privilege: the other owner is refused.
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+created.ID+"/super-owner", bossToken,
		map[string]bool{"super_owner": true}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a non-super-owner cannot grant super-owner")

	// The super-owner grants it; the deputy's old session is revoked.
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+created.ID+"/super-owner", rootToken,
		map[string]bool{"super_owner": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, http.StatusUnauthorized,
		h.do(http.MethodGet, "/api/v1/server/access", deputyToken, nil, nil).Code,
		"granting super-owner revokes the target's existing sessions")

	// The grant is persisted: a fresh sign-in is a super-owner with no seed.
	deputyToken = h.login(opsDB, "deputy", "deputy-pass-1")
	h.do(http.MethodGet, "/api/v1/server/access", deputyToken, nil, &deputyAccess)
	assert.True(t, deputyAccess.SuperOwner, "the persisted flag makes the deputy a super-owner")
	assert.True(t, deputyAccess.ServerAdmin)

	// Super-owner can only sit on an owner account.
	var plainUser struct {
		ID string `json:"id"`
	}
	h.do(http.MethodPost, "/api/v1/security/users", rootToken,
		map[string]interface{}{"username": "clerk", "password": "clerk-pass-1", "role": "user"}, &plainUser)
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+plainUser.ID+"/super-owner", rootToken,
		map[string]bool{"super_owner": true}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "super-owner requires an owner account")

	// A super-owner cannot strip their own privilege (no accidental lock-out).
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	h.do(http.MethodGet, "/api/v1/auth/session", rootToken, nil, &me)
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+me.User.ID+"/super-owner", rootToken,
		map[string]bool{"super_owner": false}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a super-owner cannot demote themselves")

	_ = otherDB
}
