package api_test

import (
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	// stored a privilege grants every capability.
	var listed struct {
		Capabilities []string                `json:"capabilities"`
		Privileges   []schema.RolePrivileges `json:"privileges"`
	}
	rec = h.do(http.MethodGet, "/api/v1/security/privileges", owner, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{schema.CapabilityBulkExport, schema.CapabilityBulkImport}, listed.Capabilities)
	require.Len(t, listed.Privileges, 3)
	for _, p := range listed.Privileges {
		assert.True(t, p.BulkExport, "%s export", p.Role)
		assert.True(t, p.BulkImport, "%s import", p.Role)
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
