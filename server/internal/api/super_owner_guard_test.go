package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
)

// passwordHash reads the stored hash of an account, to tell whether a refused
// request left it alone.
func (h *apiHarness) passwordHash(database, userID string) string {
	h.t.Helper()
	driver, err := h.mgr.DriverFor(context.Background(), database)
	require.NoError(h.t, err)
	q := `SELECT password_hash FROM sys_users WHERE id = $1`
	if driver.Dialect().Engine() != dbal.EnginePostgres {
		q = `SELECT password_hash FROM sys_users WHERE id = ?`
	}
	var hash string
	require.NoError(h.t, driver.DB().QueryRowContext(context.Background(), q, userID).Scan(&hash))
	return hash
}

func (h *apiHarness) userID(token, username string) string {
	h.t.Helper()
	for _, u := range h.observeUsers(token) {
		if u.Username == username {
			return u.ID
		}
	}
	h.t.Fatalf("no account %q", username)
	return ""
}

// A plain database owner cannot take over a super-owner account of the same
// database, whether the privilege is stored or seeded (#209).
func TestSuperOwnerGuard_PlainOwnerCannotManageSuperOwner(t *testing.T) {
	db := uniqueDB("f4b_sowner")
	h := newAPIHarness(t, api.Options{
		AllowPublicDatabaseCreation: true,
		SuperOwnerSeed:              []string{db + ":root", db + ":ghost"},
	})
	h.createDatabase(db, "root", "root-secret-pw")
	root := h.login(db, "root", "root-secret-pw")

	create := func(name, role string) string {
		var u idOnly
		rec := h.do(http.MethodPost, "/api/v1/security/users", root,
			map[string]interface{}{"username": name, "password": name + "-secret-pw", "role": role}, &u)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		return u.ID
	}
	aliceID := create("alice", "owner")
	require.Equal(t, http.StatusOK, h.do(http.MethodPut, "/api/v1/security/users/"+aliceID+"/super-owner", root,
		map[string]bool{"super_owner": true}, nil).Code)
	create("bob", "owner")
	clerkID := create("clerk", "user")
	rootID := h.userID(root, "root")

	alice := h.login(db, "alice", "alice-secret-pw")
	bob := h.login(db, "bob", "bob-secret-pw")
	aliceHash := h.passwordHash(db, aliceID)
	rootHash := h.passwordHash(db, rootID)

	usable := func(token string) bool {
		return h.do(http.MethodGet, "/api/v1/schemas/tables", token, nil, nil).Code == http.StatusOK
	}
	refused := func(method, target string, body interface{}) {
		t.Helper()
		rec := h.do(method, target, bob, body, nil)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%s %s", method, target)
		assert.Contains(t, rec.Body.String(), "only a super-owner can manage a super-owner account", "%s %s", method, target)
	}

	for _, target := range []string{aliceID, rootID} {
		refused(http.MethodPut, "/api/v1/security/users/"+target, map[string]string{"password": "taken-over-pw"})
		refused(http.MethodPut, "/api/v1/security/users/"+target, map[string]string{"role": "user"})
		refused(http.MethodPut, "/api/v1/security/users/"+target, map[string]interface{}{"is_active": false})
		refused(http.MethodDelete, "/api/v1/security/users/"+target, nil)
		refused(http.MethodPost, "/api/v1/security/users/"+target+"/mfa/setup", map[string]string{})
		refused(http.MethodPost, "/api/v1/security/users/"+target+"/mfa/enable", map[string]string{"code": "123456", "secret": "JBSWY3DPEHPK3PXP"})
		refused(http.MethodPost, "/api/v1/security/users/"+target+"/mfa/disable", map[string]string{})
	}
	// A seeded name not created yet would sign in as a super-owner.
	refused(http.MethodPost, "/api/v1/security/users", map[string]string{"username": "Ghost", "password": "ghost-secret-pw", "role": "user"})

	assert.Equal(t, aliceHash, h.passwordHash(db, aliceID), "the stored super-owner's hash is unchanged")
	assert.Equal(t, rootHash, h.passwordHash(db, rootID), "the seeded super-owner's hash is unchanged")
	assert.True(t, usable(alice), "the super-owner's sessions are not revoked")
	assert.True(t, usable(root))
	h.login(db, "alice", "alice-secret-pw")

	// The plain owner still manages ordinary accounts.
	rec := h.do(http.MethodPut, "/api/v1/security/users/"+clerkID, bob, map[string]string{"password": "clerk-new-pw"}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// A super-owner resets another super-owner's password, without theirs.
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+aliceID, root, map[string]string{"password": "alice-reset-pw"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, usable(alice), "the reset revokes the target's sessions")
	alice = h.login(db, "alice", "alice-reset-pw")
	rec = h.do(http.MethodPost, "/api/v1/security/users/"+rootID+"/mfa/setup", alice, map[string]string{}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/security/users", root,
		map[string]string{"username": "ghost", "password": "ghost-secret-pw", "role": "owner"}, nil)
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// Changing your own password takes the current one, whatever the role; an
// administrator setting someone else's password does not need theirs (#209).
func TestSelfPasswordChange_RequiresCurrentPassword(t *testing.T) {
	th := auth.NewLoginThrottle(3, time.Minute, time.Hour)
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, LoginThrottle: th})
	db := uniqueDB("f4b_selfpw")
	h.createDatabase(db, "olivia", "olivia-secret-pw")
	owner := h.login(db, "olivia", "olivia-secret-pw")
	ownerID := h.userID(owner, "olivia")
	var uma idOnly
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]string{"username": "uma", "password": "uma-secret-pw", "role": "user"}, &uma).Code)
	user := h.login(db, "uma", "uma-secret-pw")
	before := h.passwordHash(db, uma.ID)

	change := func(token, id string, body map[string]string) int {
		return h.do(http.MethodPut, "/api/v1/security/users/"+id, token, body, nil).Code
	}

	assert.Equal(t, http.StatusForbidden, change(user, uma.ID, map[string]string{"password": "uma-new-pw"}), "no current password")
	assert.Equal(t, http.StatusForbidden, change(user, uma.ID, map[string]string{"password": "uma-new-pw", "current_password": "wrong"}), "wrong current password")
	assert.Equal(t, before, h.passwordHash(db, uma.ID), "a refused change keeps the hash")
	assert.Equal(t, http.StatusForbidden, change(owner, ownerID, map[string]string{"password": "olivia-new-pw"}), "owners too")

	rec := h.do(http.MethodPut, "/api/v1/security/users/"+uma.ID, user,
		map[string]string{"password": "uma-new-pw", "current_password": "uma-secret-pw"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	h.login(db, "uma", "uma-new-pw")

	// An owner resets uma's password without knowing it.
	assert.Equal(t, http.StatusOK, change(owner, uma.ID, map[string]string{"password": "uma-reset-pw"}))
	user = h.login(db, "uma", "uma-reset-pw")

	// Wrong current passwords count against the sign-in throttle: a session
	// cannot be used to guess the password.
	assert.Equal(t, http.StatusForbidden, change(user, uma.ID, map[string]string{"password": "x-new-pw", "current_password": "guess-1"}))
	assert.Equal(t, http.StatusForbidden, change(user, uma.ID, map[string]string{"password": "x-new-pw", "current_password": "guess-2"}))
	assert.Equal(t, http.StatusTooManyRequests, change(user, uma.ID, map[string]string{"password": "x-new-pw", "current_password": "guess-3"}))
	assert.Equal(t, http.StatusTooManyRequests, change(user, uma.ID, map[string]string{"password": "x-new-pw", "current_password": "uma-reset-pw"}), "locked out even with the right one")
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "", map[string]string{"database": db, "username": "uma", "password": "uma-reset-pw"}, nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "the sign-in shares the counter")
}
