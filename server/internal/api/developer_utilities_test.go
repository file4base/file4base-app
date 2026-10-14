package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_DeveloperUtilities(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_devutil")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	// 1. Initial solution metadata defaults to the database name and unlocked.
	var meta struct {
		Database     string `json:"database"`
		SolutionName string `json:"solution_name"`
		DesignLocked bool   `json:"design_locked"`
	}
	rec := h.do(http.MethodGet, "/api/v1/solutions/meta", owner, nil, &meta)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, db, meta.Database)
	assert.Equal(t, db, meta.SolutionName)
	assert.False(t, meta.DesignLocked)

	// 2. Build a table, a column, a record, a script and a layout referencing the solution name.
	var table schema.TableMetadata
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", owner, map[string]interface{}{
		"display_name": "Orders",
	}, &table)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", owner, map[string]interface{}{
		"name":         "customer",
		"display_name": "Customer",
		"field_type":   "text",
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Insert a record into Orders on the source database.
	rec = h.do(http.MethodPost, "/api/v1/data/orders", owner, map[string]interface{}{
		"customer": "Acme Bakery",
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var script schema.ScriptMetadata
	rec = h.do(http.MethodPost, "/api/v1/schemas/scripts", owner, map[string]interface{}{
		"name":      "Open Orders",
		"is_active": true,
		"steps": []map[string]interface{}{
			{
				"sequence_idx": 1,
				"step_type":    "show_dialog",
				"params": map[string]interface{}{
					"title":         "Welcome",
					"solution_name": db,
				},
				"is_enabled": true,
			},
		},
	}, &script)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// 3. Rename the solution and verify references are updated.
	rec = h.do(http.MethodPost, "/api/v1/solutions/rename", owner, map[string]interface{}{
		"solution_name": "Bakery Suite",
	}, &meta)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Bakery Suite", meta.SolutionName)
	assert.False(t, meta.DesignLocked)

	var updatedScript schema.ScriptMetadata
	rec = h.do(http.MethodGet, "/api/v1/schemas/scripts/"+script.ID, owner, nil, &updatedScript)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, updatedScript.Steps, 1)
	assert.Contains(t, string(updatedScript.Steps[0].Params), "Bakery Suite")

	// 4. Clone the solution without records into a new database, locking the clone's design.
	cloneDB := uniqueDB("f4b_clone")
	t.Cleanup(func() {
		_ = h.mgr.DropDatabase(context.Background(), cloneDB)
	})
	var cloneRes struct {
		Status        string `json:"status"`
		Database      string `json:"database"`
		SolutionName  string `json:"solution_name"`
		DesignLocked  bool   `json:"design_locked"`
		OwnerUser     string `json:"owner_user"`
		TablesCreated int    `json:"tables_created"`
	}
	rec = h.do(http.MethodPost, "/api/v1/solutions/clone", owner, map[string]interface{}{
		"target_database": cloneDB,
		"solution_name":   "Bakery Production",
		"lock_design":     true,
	}, &cloneRes)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "cloned", cloneRes.Status)
	assert.Equal(t, cloneDB, cloneRes.Database)
	assert.Equal(t, "Bakery Production", cloneRes.SolutionName)
	assert.True(t, cloneRes.DesignLocked)
	assert.Equal(t, "alice", cloneRes.OwnerUser)
	assert.Equal(t, 1, cloneRes.TablesCreated)

	// Sign in to the cloned database with the same owner credentials.
	cloneOwner := h.login(cloneDB, "alice", "alice-secret")

	// Verify the clone has the table structure, zero records, updated solution name, and locked design.
	var cloneRows []map[string]interface{}
	rec = h.do(http.MethodGet, "/api/v1/data/orders", cloneOwner, nil, &cloneRows)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, cloneRows, "clone must contain zero user records")

	// Verify records can still be created and queried on a design-locked database.
	rec = h.do(http.MethodPost, "/api/v1/data/orders", cloneOwner, map[string]interface{}{
		"customer": "New Clone Customer",
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Verify schema/design mutations on the locked clone are rejected with 403 Forbidden.
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", cloneOwner, map[string]interface{}{
		"display_name": "Invoices",
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPost, "/api/v1/solutions/rename", cloneOwner, map[string]interface{}{
		"solution_name": "Cannot Rename Locked",
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// 5. Lock the original database's design in place via POST /api/v1/solutions/lock-design.
	// Without confirm: true, it is rejected with 422.
	rec = h.do(http.MethodPost, "/api/v1/solutions/lock-design", owner, map[string]interface{}{
		"confirm": false,
	}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPost, "/api/v1/solutions/lock-design", owner, map[string]interface{}{
		"confirm": true,
	}, &meta)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, meta.DesignLocked)

	// After locking, schema/design changes on the original database return 403 Forbidden.
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+table.ID+"/columns", owner, map[string]interface{}{
		"name":         "extra",
		"display_name": "Extra",
		"field_type":   "text",
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPut, "/api/v1/schemas/scripts/"+script.ID, owner, map[string]interface{}{
		"name":      "Tampered",
		"is_active": true,
		"steps":     []map[string]interface{}{},
	}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}
