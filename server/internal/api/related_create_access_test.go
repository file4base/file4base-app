package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Creating a related record needs the create privilege on the related table,
// a table of this solution as much as another's, and goes from the left side
// of the relationship to its right, never the reverse.
func TestAPI_CreateRelatedNeedsCreateOnTheRelatedTable(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_related_create")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	tableIDs, columns := map[string]string{}, map[string]string{}
	for _, name := range []string{"teams", "members"} {
		var tbl struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner, map[string]string{"display_name": name, "custom_name": name}, &tbl)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		tableIDs[name] = tbl.ID
		var col struct {
			ID string `json:"id"`
		}
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner, map[string]interface{}{
			"name": "code", "display_name": "code", "field_type": "TEXT", "is_nullable": true,
		}, &col)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		columns[name] = col.ID
	}
	var occurrences []struct {
		ID          string `json:"id"`
		BaseTableID string `json:"base_table_id"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", owner, nil, &occurrences).Code)
	occurrence := map[string]string{}
	for _, o := range occurrences {
		for name, id := range tableIDs {
			if o.BaseTableID == id {
				occurrence[name] = o.ID
			}
		}
	}
	var rel struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/relationships", owner, map[string]interface{}{
		"name": "teams_members", "operator": "=", "allow_creation": true,
		"left_occurrence_id": occurrence["teams"], "left_column_id": columns["teams"],
		"right_occurrence_id": occurrence["members"], "right_column_id": columns["members"],
	}, &rel)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var team, member map[string]interface{}
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/teams", owner, map[string]interface{}{"code": "T-1"}, &team).Code)
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/members", owner, map[string]interface{}{"code": "T-1"}, &member).Code)
	fromTeam := "/api/v1/data/teams/" + fmt.Sprint(team["id"]) + "/related?relationship=" + rel.ID

	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var users []struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/security/users", owner, nil, &users).Code)
	carol := ""
	for _, u := range users {
		if u.Username == "carol" {
			carol = u.ID
		}
	}
	grant := func(create bool) {
		t.Helper()
		rec := h.do(http.MethodPut, "/api/v1/security/users/"+carol+"/table-permissions", owner, map[string]interface{}{
			"permissions": []map[string]interface{}{{
				"table_name": "members", "access_level": "read_write",
				"can_create": create, "can_edit": true, "can_delete": false,
			}},
		}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	members := func() int {
		t.Helper()
		var rows []map[string]interface{}
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/members", owner, nil, &rows).Code)
		return len(rows)
	}

	t.Run("edit without create creates nothing", func(t *testing.T) {
		grant(false)
		carolToken := h.login(db, "carol", "carol-secret-pw")
		rec := h.do(http.MethodPost, fromTeam, carolToken, map[string]interface{}{}, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "create")
		assert.Equal(t, 1, members())

		grant(true)
		carolToken = h.login(db, "carol", "carol-secret-pw")
		var row map[string]interface{}
		rec = h.do(http.MethodPost, fromTeam, carolToken, map[string]interface{}{}, &row)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Equal(t, "T-1", row["code"])
		assert.Equal(t, 2, members())
	})

	t.Run("a member creates no team", func(t *testing.T) {
		rec := h.do(http.MethodPost, "/api/v1/data/members/"+fmt.Sprint(member["id"])+"/related?relationship="+rel.ID,
			owner, map[string]interface{}{}, nil)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "create the record from")
		var rows []map[string]interface{}
		require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/teams", owner, nil, &rows).Code)
		assert.Len(t, rows, 1)
	})
}
