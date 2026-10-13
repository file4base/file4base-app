package api_test

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Custom menus (#52): what a solution may change about the menu bar, what it
// may not, and what travels in the solution file.
func TestAPI_MenuSets(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_menus")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	var script struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/scripts", owner, map[string]interface{}{
		"name": "Greet customer", "is_active": true,
		"steps": []map[string]interface{}{
			{"sequence_idx": 1, "step_type": "show_dialog", "params": map[string]string{"title": "Hello"}, "is_enabled": true},
		},
	}, &script)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	set := map[string]interface{}{
		"name":       "Bakery menus",
		"is_default": true,
		"menus": []map[string]interface{}{
			{
				"menu": "Records",
				"mode": "replace",
				"items": []map[string]interface{}{
					{"title": "Greet the customer", "shortcut": "cmd+shift+G",
						"action": map[string]interface{}{"type": "perform_script",
							"script_id": script.ID, "script_name": "Greet customer"}},
					{"separator": true},
					{"title": "Start a customer", "action": map[string]interface{}{
						"type": "single_step", "step_type": "new_record"}},
				},
			},
			{
				"menu": "File",
				"mode": "extend",
				"items": []map[string]interface{}{
					{"title": "Today's orders", "action": map[string]interface{}{
						"type": "perform_script", "script_name": "Greet customer"}},
				},
			},
		},
	}

	var created schema.MenuSet
	rec = h.do(http.MethodPost, "/api/v1/menu-sets", owner, set, &created)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, "Bakery menus", created.Name)
	assert.True(t, created.IsDefault)
	require.Len(t, created.Menus, 2)
	assert.Equal(t, "replace", created.Menus[0].Mode)
	require.Len(t, created.Menus[0].Items, 3)
	assert.True(t, created.Menus[0].Items[1].Separator)

	// The list says which menus a solution may change, and which of them it
	// may only extend, so a client does not have to know the rule twice.
	var listing struct {
		Menus      []string         `json:"menus"`
		ExtendOnly []string         `json:"extend_only"`
		MenuSets   []schema.MenuSet `json:"menu_sets"`
	}
	rec = h.do(http.MethodGet, "/api/v1/menu-sets", owner, nil, &listing)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, listing.Menus, "Records")
	assert.Equal(t, []string{"File"}, listing.ExtendOnly)
	require.Len(t, listing.MenuSets, 1)

	// ─── What a menu set may not do ───────────────────────────────────────
	for name, bad := range map[string]map[string]interface{}{
		"replacing File": {"name": "No file", "menus": []map[string]interface{}{
			{"menu": "File", "mode": "replace", "items": []map[string]interface{}{
				{"title": "Only mine", "action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}}}}}},
		"a menu that is not one": {"name": "No such", "menus": []map[string]interface{}{
			{"menu": "Apple", "mode": "extend", "items": []map[string]interface{}{
				{"title": "x", "action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}}}}}},
		"an item that does nothing": {"name": "Empty item", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{{"title": "Does nothing"}}}}},
		"an item with no title": {"name": "No title", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{
				{"action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}}}}}},
		"a script item naming no script": {"name": "No script", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{
				{"title": "Run", "action": map[string]interface{}{"type": "perform_script"}}}}}},
		"nothing but separators": {"name": "Separators", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{{"separator": true}}}}},
		"shadowing a mode shortcut (Cmd+B)": {"name": "Mode shadow", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{
				{"title": "Steal Browse", "shortcut": "cmd+B",
					"action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}}}}}},
		"shadowing Find mode shortcut (Ctrl+F)": {"name": "Find shadow", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{
				{"title": "Steal Find", "shortcut": "ctrl+f",
					"action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}}}}}},
		"duplicate shortcut within the same set": {"name": "Duplicate key", "menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{
				{"title": "First", "shortcut": "cmd+shift+K",
					"action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}},
				{"title": "Second", "shortcut": "ctrl+shift+k",
					"action": map[string]interface{}{"type": "single_step", "step_type": "delete_record"}}}}}},
		"no menus at all": {"name": "Nothing"},
		"no name":         {"menus": []map[string]interface{}{{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{{"title": "x", "action": map[string]interface{}{"type": "single_step", "step_type": "new_record"}}}}}},
	} {
		rec = h.do(http.MethodPost, "/api/v1/menu-sets", owner, bad, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%s: %s", name, rec.Body.String())
	}

	// A second set with the same name is refused.
	rec = h.do(http.MethodPost, "/api/v1/menu-sets", owner, set, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// ─── Only one set is the solution's ───────────────────────────────────
	second := map[string]interface{}{
		"name": "Counter menus", "is_default": true,
		"menus": []map[string]interface{}{
			{"menu": "Records", "mode": "extend", "items": []map[string]interface{}{
				{"title": "Take an order", "action": map[string]interface{}{
					"type": "single_step", "step_type": "new_record"}}}},
		},
	}
	rec = h.do(http.MethodPost, "/api/v1/menu-sets", owner, second, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = h.do(http.MethodGet, "/api/v1/menu-sets", owner, nil, &listing)
	require.Equal(t, http.StatusOK, rec.Code)
	defaults := []string{}
	for _, s := range listing.MenuSets {
		if s.IsDefault {
			defaults = append(defaults, s.Name)
		}
	}
	assert.Equal(t, []string{"Counter menus"}, defaults, "the newest default replaces the previous one")

	// ─── Who may change them ──────────────────────────────────────────────
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(db, "carol", "carol-secret-pw")

	// A user reads them, because the client needs them to draw the bar.
	assert.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/menu-sets", user, nil, nil).Code)
	// Changing them is design.
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodPost, "/api/v1/menu-sets", user, second, nil).Code)
	assert.Equal(t, http.StatusForbidden,
		h.do(http.MethodDelete, "/api/v1/menu-sets/"+created.ID, user, nil, nil).Code)

	// ─── The solution file carries them, with the scripts remapped ────────
	file := exportSolution(t, h, owner, map[string]interface{}{"solution_name": "Bakery"})
	dstDB := uniqueDB("f4b_menus_dst")
	h.createDatabase(dstDB, "olivia", "olivia-secret")
	dst := h.login(dstDB, "olivia", "olivia-secret")

	report := map[string]interface{}{}
	rec = h.postBytes("/api/v1/solutions/import", dst, file, &report)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.EqualValues(t, 2, report["menu_sets_created"])

	var restored struct {
		MenuSets []schema.MenuSet `json:"menu_sets"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/menu-sets", dst, nil, &restored).Code)
	require.Len(t, restored.MenuSets, 2)

	var scripts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/scripts", dst, nil, &scripts).Code)
	require.Len(t, scripts, 1)
	for _, s := range restored.MenuSets {
		for _, menu := range s.Menus {
			for _, item := range menu.Items {
				if id, ok := item.Action["script_id"].(string); ok && id != "" {
					assert.Equal(t, scripts[0].ID, id,
						"a menu item points at the destination's own script, not the source's")
				}
			}
		}
	}

	// The Database Design Report (#49) carries the menu sets as well.
	var ddr struct {
		Counts struct {
			MenuSets int `json:"menu_sets"`
		} `json:"counts"`
		MenuSets []struct {
			Name string `json:"name"`
		} `json:"menu_sets"`
	}
	rec = h.do(http.MethodGet, "/api/v1/solutions/design-report?format=json", dst, nil, &ddr)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, 2, ddr.Counts.MenuSets)
	assert.Len(t, ddr.MenuSets, 2)

	// Deleting a set leaves the standard menus; nothing else breaks.
	assert.Equal(t, http.StatusNoContent,
		h.do(http.MethodDelete, "/api/v1/menu-sets/"+created.ID, owner, nil, nil).Code)
	assert.Equal(t, http.StatusNotFound,
		h.do(http.MethodGet, "/api/v1/menu-sets/"+created.ID, owner, nil, nil).Code)
}
