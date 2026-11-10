package api_test

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPI_FileReferencesResolveToASolution covers what a Perform Script step
// asks before it calls a script in another solution (#107): where the
// reference goes, from where, and whether it goes anywhere at all.
func TestAPI_FileReferencesResolveToASolution(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_fileref")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	// What the migration creates: a reference nobody has mapped yet.
	rec := h.do(http.MethodPost, "/api/v1/file-references", owner,
		map[string]string{"name": "Results"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// An unmapped reference goes nowhere, and says so, rather than letting a
	// step fall back to this solution.
	rec = h.do(http.MethodGet, "/api/v1/file-references/Results/resolve", owner, nil, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "names no solution yet")

	// Mapped, it resolves to that solution, from this one.
	rec = h.do(http.MethodPut, "/api/v1/file-references/Results", owner,
		map[string]string{"name": "Results", "database": "results_db"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resolved api.FileReferenceResolution
	rec = h.do(http.MethodGet, "/api/v1/file-references/results/resolve", owner, nil, &resolved)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Results", resolved.Name)
	assert.Equal(t, "results_db", resolved.Database)
	assert.Equal(t, db, resolved.CallingDatabase)
	assert.False(t, resolved.Local)

	// A reference to this very solution is local.
	rec = h.do(http.MethodPost, "/api/v1/file-references", owner,
		map[string]string{"name": "Self", "database": db}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodGet, "/api/v1/file-references/Self/resolve", owner, nil, &resolved)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, resolved.Local)

	rec = h.do(http.MethodGet, "/api/v1/file-references/Nowhere/resolve", owner, nil, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// The same name twice, and a database that is not a solution, are refused.
	rec = h.do(http.MethodPost, "/api/v1/file-references", owner,
		map[string]string{"name": "results"}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/file-references", owner,
		map[string]string{"name": "Engine", "database": "template1"}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// A regular user may resolve a reference — running a script needs that —
	// but not change where it points.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]string{"username": "dave", "password": "dave-secret", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(db, "dave", "dave-secret")
	rec = h.do(http.MethodGet, "/api/v1/file-references/Results/resolve", user, nil, nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = h.do(http.MethodPut, "/api/v1/file-references/Results", user,
		map[string]string{"name": "Results", "database": "elsewhere_db"}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = h.do(http.MethodDelete, "/api/v1/file-references/Results", owner, nil, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}
