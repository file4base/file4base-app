package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The script trigger review (#26) is a developer's tool: a regular user can
// neither list nor switch triggers, and an owner switches them in bulk, all
// or none.
func TestAPI_ScriptTriggerReview(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_trig")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
		map[string]string{"display_name": "Plants", "custom_name": "plants"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var script struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/scripts", owner, map[string]interface{}{"name": "Greet", "is_active": true}, &script)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	trigger := func(id, event string, extra map[string]interface{}) map[string]interface{} {
		m := map[string]interface{}{"id": id, "event": event, "type": "perform_script", "script_name": "Greet",
			"enabled": false, "quarantined_by_migration": true}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	var layout struct {
		ID string `json:"id"`
	}
	definition := map[string]interface{}{
		"objects": []interface{}{},
		"script_triggers": []interface{}{
			trigger("t-enter", "OnLayoutEnter", nil),
			trigger("t-save", "OnObjectSave", map[string]interface{}{"object": map[string]interface{}{"type": "Edit Box", "field": "Plants::Height"}}),
		},
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", owner, map[string]interface{}{
		"name": "Plant Card", "table_occurrence_id": tbl.ID, "definition": definition,
	}, &layout)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	// A created layout's triggers get ids of their own; an owner's update
	// writes these.
	rec = h.do(http.MethodPut, "/api/v1/schemas/layouts/"+layout.ID, owner, map[string]interface{}{
		"name": "Plant Card", "definition": definition,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "bob", "password": "bob-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(db, "bob", "bob-secret-pw")
	rec = h.do(http.MethodGet, "/api/v1/schemas/script-triggers", user, nil, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "listing triggers is a developer's tool")
	rec = h.do(http.MethodPut, "/api/v1/schemas/script-triggers", user, map[string]interface{}{"ids": []string{"t-enter"}, "enabled": true}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, "and so is switching them")

	// Nor can a user switch one on by saving the layout (#26): neither by
	// moving it where a client runs it, nor by changing the stored list.
	enabledInSlot := map[string]interface{}{
		"objects": []interface{}{},
		"on_layout_enter": func() map[string]interface{} {
			m := trigger("t-enter", "OnLayoutEnter", nil)
			m["enabled"] = true
			delete(m, "quarantined_by_migration")
			return m
		}(),
		"script_triggers": definition["script_triggers"].([]interface{})[1:],
	}
	rec = h.do(http.MethodPut, "/api/v1/schemas/layouts/"+layout.ID, user, map[string]interface{}{"name": "Plant Card", "definition": enabledInSlot}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	edited := map[string]interface{}{"objects": []interface{}{map[string]interface{}{"id": "o1", "type": "text"}}, "script_triggers": definition["script_triggers"]}
	rec = h.do(http.MethodPut, "/api/v1/schemas/layouts/"+layout.ID, user, map[string]interface{}{"name": "Plant Card", "definition": edited}, nil)
	assert.Equal(t, http.StatusOK, rec.Code, "any other edit is the user's to make: %s", rec.Body.String())

	var listed struct {
		Triggers []schema.ScriptTrigger `json:"triggers"`
	}
	rec = h.do(http.MethodGet, "/api/v1/schemas/script-triggers", owner, nil, &listed)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, listed.Triggers, 2)
	byID := map[string]schema.ScriptTrigger{}
	for _, tr := range listed.Triggers {
		byID[tr.ID] = tr
	}
	assert.Equal(t, "Plant Card", byID["t-enter"].LayoutName)
	assert.Equal(t, script.ID, byID["t-enter"].ScriptID)
	assert.True(t, byID["t-enter"].CanEnable)
	assert.Equal(t, "object", byID["t-save"].Scope)
	assert.Equal(t, "Plants::Height", byID["t-save"].ObjectLabel)
	assert.False(t, byID["t-save"].CanEnable)

	rec = h.do(http.MethodPut, "/api/v1/schemas/script-triggers", owner, map[string]interface{}{"ids": []string{"t-enter", "t-save"}, "enabled": true}, nil)
	assert.Equal(t, http.StatusConflict, rec.Code, "a trigger File4Base cannot run refuses the batch")
	rec = h.do(http.MethodPut, "/api/v1/schemas/script-triggers", owner, map[string]interface{}{"ids": []string{"nope"}, "enabled": true}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = h.do(http.MethodPut, "/api/v1/schemas/script-triggers", owner, map[string]interface{}{"ids": []string{"t-enter"}}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "enabled is required")

	var update schema.ScriptTriggerUpdate
	rec = h.do(http.MethodPut, "/api/v1/schemas/script-triggers", owner, map[string]interface{}{"ids": []string{"t-enter"}, "enabled": true}, &update)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 1, update.Updated)
	assert.Equal(t, []string{layout.ID}, update.Layouts)

	var stored struct {
		Definition json.RawMessage `json:"definition"`
	}
	rec = h.do(http.MethodGet, "/api/v1/schemas/layouts/"+layout.ID, owner, nil, &stored)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var def struct {
		OnLayoutEnter struct {
			ScriptID string `json:"script_id"`
			Enabled  bool   `json:"enabled"`
		} `json:"on_layout_enter"`
		ScriptTriggers []map[string]interface{} `json:"script_triggers"`
	}
	require.NoError(t, json.Unmarshal(stored.Definition, &def))
	assert.Equal(t, script.ID, def.OnLayoutEnter.ScriptID, "an enabled trigger is where a client runs it")
	assert.True(t, def.OnLayoutEnter.Enabled)
	require.Len(t, def.ScriptTriggers, 1, "the object trigger stays stored")
}
