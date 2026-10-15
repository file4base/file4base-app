package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/auth"
)

func TestAPI_LoginAttemptsAreRateLimited(t *testing.T) {
	th := auth.NewLoginThrottle(3, time.Minute, time.Hour)
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, LoginThrottle: th})
	db := uniqueDB("f4b_throttle")
	h.createDatabase(db, "alice", "alice-secret-pw")

	bad := func() int {
		return h.do(http.MethodPost, "/api/v1/auth/login", "",
			map[string]string{"database": db, "username": "alice", "password": "wrong"}, nil).Code
	}
	// The first two wrong passwords answer 401; the third failure trips the
	// lockout and answers 429.
	assert.Equal(t, http.StatusUnauthorized, bad())
	assert.Equal(t, http.StatusUnauthorized, bad())
	rec := h.do(http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"database": db, "username": "alice", "password": "wrong"}, nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	// Even the right password is refused while locked out.
	rec = h.do(http.MethodPost, "/api/v1/auth/login", "",
		map[string]string{"database": db, "username": "alice", "password": "alice-secret-pw"}, nil)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// A different account is unaffected, and a success clears its counter.
	th2 := auth.NewLoginThrottle(3, time.Minute, time.Hour)
	h2 := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true, LoginThrottle: th2})
	db2 := uniqueDB("f4b_throttle2")
	h2.createDatabase(db2, "bob", "bob-secret-pw")
	for i := 0; i < 2; i++ {
		require.Equal(t, http.StatusUnauthorized, h2.do(http.MethodPost, "/api/v1/auth/login", "",
			map[string]string{"database": db2, "username": "bob", "password": "no"}, nil).Code)
	}
	require.NotEmpty(t, h2.login(db2, "bob", "bob-secret-pw")) // success resets
	for i := 0; i < 2; i++ {
		require.Equal(t, http.StatusUnauthorized, h2.do(http.MethodPost, "/api/v1/auth/login", "",
			map[string]string{"database": db2, "username": "bob", "password": "no"}, nil).Code)
	}
}
