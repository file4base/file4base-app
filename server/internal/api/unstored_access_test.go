package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unstored calculation reads related records as the caller: a user who
// may not read the related table gets <No Access>, never its values, from a
// record read and from the Data Viewer alike.
func TestAPI_UnstoredCalculationRespectsTableAccess(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_unstored_access")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	tableIDs, columns := map[string]string{}, map[string]string{}
	for _, name := range []string{"staff", "payroll"} {
		var tbl struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner, map[string]string{"display_name": name, "custom_name": name}, &tbl)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		tableIDs[name] = tbl.ID
	}
	addColumn := func(table, name, kind string, formula *string) {
		var col struct {
			ID string `json:"id"`
		}
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables/"+tableIDs[table]+"/columns", owner, map[string]interface{}{
			"name": name, "display_name": name, "field_type": kind, "is_nullable": true, "calculation_formula": formula,
		}, &col)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		columns[table+"."+name] = col.ID
	}
	addColumn("staff", "code", "TEXT", nil)
	addColumn("payroll", "code", "TEXT", nil)
	addColumn("payroll", "salary", "NUMBER", nil)

	var occurrences []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		BaseTableID string `json:"base_table_id"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", owner, nil, &occurrences).Code)
	occurrence := map[string]string{}
	payrollOccurrence := ""
	for _, o := range occurrences {
		for name, id := range tableIDs {
			if o.BaseTableID == id {
				occurrence[name] = o.ID
				if name == "payroll" {
					payrollOccurrence = o.Name
				}
			}
		}
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/relationships", owner, map[string]interface{}{
		"name": "staff_payroll", "operator": "=",
		"left_occurrence_id": occurrence["staff"], "left_column_id": columns["staff.code"],
		"right_occurrence_id": occurrence["payroll"], "right_column_id": columns["payroll.code"],
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	formula := fmt.Sprintf(`{"formula":"Sum ( %s::salary )","result_type":"Number","stored":false}`, payrollOccurrence)
	addColumn("staff", "pay", "CALCULATION", &formula)

	var person map[string]interface{}
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/staff", owner, map[string]interface{}{"code": "S-1"}, &person).Code)
	require.Equal(t, http.StatusCreated, h.do(http.MethodPost, "/api/v1/data/payroll", owner, map[string]interface{}{"code": "S-1", "salary": 4200}, nil).Code)
	id := fmt.Sprint(person["id"])

	var row map[string]interface{}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/staff/"+id, owner, nil, &row).Code)
	assert.Equal(t, "4200", fmt.Sprint(row["pay"]))

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
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+carol+"/table-permissions", owner, map[string]interface{}{
		"permissions": []map[string]interface{}{{"table_name": "payroll", "access_level": "none"}},
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	carolToken := h.login(db, "carol", "carol-secret-pw")

	row = nil
	rec = h.do(http.MethodGet, "/api/v1/data/staff/"+id, carolToken, nil, &row)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, row["pay"], "no payroll value reaches a user who may not read payroll")

	var answer struct {
		Results []evaluationResult `json:"results"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/staff/evaluate", carolToken, map[string]interface{}{
		"expression": fmt.Sprintf("Sum ( %s::salary )", payrollOccurrence), "record_id": id, "result_type": "Number",
	}, &answer)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, answer.Results, 1)
	assert.Nil(t, answer.Results[0].Value)
	assert.Contains(t, answer.Results[0].Error, "<No Access>")
}
