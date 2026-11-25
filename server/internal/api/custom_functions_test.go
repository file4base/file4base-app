package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Manage Custom Functions over the API: full access designs them, every
// account's formulas call them, and an account without full access does not
// see the ones kept for full access.
func TestAPI_CustomFunctions(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_cfuncs")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")
	rec := h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carl", "password": "carl-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	user := h.login(db, "carl", "carl-secret-pw")

	var tbl struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables", owner, map[string]string{"display_name": "Items", "custom_name": "items"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner,
		map[string]interface{}{"display_name": "Price", "name": "price", "field_type": "NUMBER"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var half, secret schema.CustomFunction
	rec = h.do(http.MethodPost, "/api/v1/custom-functions", owner,
		map[string]interface{}{"name": "Half", "parameters": []string{"x"}, "formula": "x / 2"}, &half)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/custom-functions", owner,
		map[string]interface{}{"name": "Margin", "formula": "0.3", "access": "full_access"}, &secret)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// Designing them is full access's.
	rec = h.do(http.MethodPost, "/api/v1/custom-functions", user, map[string]interface{}{"name": "Mine", "formula": "1"}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, http.StatusForbidden, h.do(http.MethodDelete, "/api/v1/custom-functions/"+half.ID, user, nil, nil).Code)
	rec = h.do(http.MethodPost, "/api/v1/custom-functions", owner, map[string]interface{}{"name": "Upper", "formula": "1"}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a built-in name")

	var listing struct {
		CustomFunctions []schema.CustomFunction `json:"custom_functions"`
	}
	rec = h.do(http.MethodGet, "/api/v1/custom-functions", owner, nil, &listing)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, listing.CustomFunctions, 2)
	rec = h.do(http.MethodGet, "/api/v1/custom-functions", user, nil, &listing)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, listing.CustomFunctions, 1)
	assert.Equal(t, "Half", listing.CustomFunctions[0].Name)

	// Every account's formulas call them, a script's with its variables.
	var rows map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/items", user, map[string]interface{}{"price": 10}, &rows)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var evaluated struct {
		Results []struct {
			Text      string            `json:"text"`
			Error     string            `json:"error"`
			Variables map[string]string `json:"variables"`
		} `json:"results"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/items/evaluate", user, map[string]interface{}{
		"record_id":   rows["id"],
		"expressions": []string{"Half ( price ) + $n", "Let ( $x = Margin ; Get ( ScriptParameter ) )"},
		"variables":   map[string]interface{}{"$n": "1"},
		"get":         map[string]interface{}{"ScriptParameter": "p"},
	}, &evaluated)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, evaluated.Results, 2)
	assert.Equal(t, "6", evaluated.Results[0].Text, evaluated.Results[0].Error)
	assert.Equal(t, "p", evaluated.Results[1].Text, evaluated.Results[1].Error)
	assert.Equal(t, map[string]string{"$x": "0.3"}, evaluated.Results[1].Variables)

	// The calculation editor's syntax check.
	var check schema.FormulaCheck
	rec = h.do(http.MethodPost, "/api/v1/formulas/check", user, map[string]interface{}{"formula": "Half ( 1 ; 2 )"}, &check)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, check.Valid)
	assert.NotEmpty(t, check.Message)

	// A function formulas call is not deleted without force; the conflict
	// lists them.
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner, map[string]interface{}{
		"display_name": "Halved", "name": "halved", "field_type": "CALCULATION", "is_nullable": true,
		"calculation_formula": `{"formula":"Half ( price )","result_type":"Number"}`}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var usages struct {
		Usages []schema.CustomFunctionUsage `json:"usages"`
	}
	rec = h.do(http.MethodGet, "/api/v1/custom-functions/"+half.ID+"/usages", owner, nil, &usages)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, usages.Usages, 1)
	assert.Equal(t, "items::halved", usages.Usages[0].Name)
	var conflict struct {
		Detail string                       `json:"detail"`
		Usages []schema.CustomFunctionUsage `json:"usages"`
	}
	rec = h.do(http.MethodDelete, "/api/v1/custom-functions/"+half.ID, owner, nil, &conflict)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Len(t, conflict.Usages, 1)

	// A rename rewrites the field's formula.
	rec = h.do(http.MethodPut, "/api/v1/custom-functions/"+half.ID, owner,
		map[string]interface{}{"name": "HalfOf", "parameters": []string{"x"}, "formula": "x / 2"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var tables []schema.TableMetadata
	rec = h.do(http.MethodGet, "/api/v1/schemas/tables", owner, nil, &tables)
	require.Equal(t, http.StatusOK, rec.Code)
	found := false
	for _, t2 := range tables {
		for _, c := range t2.Columns {
			if c.Name == "halved" {
				found = true
				assert.Contains(t, *c.CalculationFormula, "HalfOf ( price )")
			}
		}
	}
	assert.True(t, found)
	assert.Equal(t, http.StatusNoContent, h.do(http.MethodDelete, "/api/v1/custom-functions/"+half.ID+"?force=true", owner, nil, nil).Code)
}

// A validation the person may override answers 422 saying so, and the same
// write sent with X-File4Base-Validation: override is saved.
func TestAPI_ValidationOverride(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_override")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")
	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner, map[string]string{"display_name": "Codes", "custom_name": "codes"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner, map[string]interface{}{
		"display_name": "Code", "name": "code", "field_type": "TEXT",
		"validation_rules": `{"calculation_enabled":true,"calculation":"Length ( Self ) = 3","allow_override":true}`}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner, map[string]interface{}{
		"display_name": "Bad", "name": "bad", "field_type": "TEXT",
		"validation_rules": `{"calculation_enabled":true,"calculation":"Length ( "}`}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "a validation formula that does not read is refused")

	var problem struct {
		InvalidParams []struct {
			Name        string `json:"name"`
			Reason      string `json:"reason"`
			Overridable bool   `json:"overridable"`
		} `json:"invalid_params"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/codes", owner, map[string]string{"code": "ABCD"}, &problem)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Len(t, problem.InvalidParams, 1)
	assert.Equal(t, "calculation", problem.InvalidParams[0].Reason)
	assert.True(t, problem.InvalidParams[0].Overridable)

	raw, _ := json.Marshal(map[string]string{"code": "ABCD"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/data/codes", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+owner)
	req.Header.Set(api.ValidationHeader, "override")
	out := httptest.NewRecorder()
	h.router.ServeHTTP(out, req)
	assert.Equal(t, http.StatusCreated, out.Code, out.Body.String())
}
