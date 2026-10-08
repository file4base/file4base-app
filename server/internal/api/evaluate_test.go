package api_test

import (
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type evaluationResult struct {
	Expression    string      `json:"expression"`
	Value         interface{} `json:"value"`
	Text          string      `json:"text"`
	ResultType    string      `json:"result_type"`
	Fields        []string    `json:"fields"`
	UnknownFields []string    `json:"unknown_fields"`
	Error         string      `json:"error"`
}

// Evaluating expressions against a record, with the engine the calculation
// fields use (#50) — what the Data Viewer runs on.
func TestAPI_EvaluateExpression(t *testing.T) {
	h := newAPIHarness(t, api.Options{AllowPublicDatabaseCreation: true})
	db := uniqueDB("f4b_eval")
	h.createDatabase(db, "alice", "alice-secret")
	owner := h.login(db, "alice", "alice-secret")

	var tbl struct {
		ID string `json:"id"`
	}
	rec := h.do(http.MethodPost, "/api/v1/schemas/tables", owner,
		map[string]string{"display_name": "Customers", "custom_name": "customers"}, &tbl)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	for _, col := range []struct{ name, kind string }{
		{"last_name", "TEXT"},
		{"city", "TEXT"},
		{"fee_paid", "NUMBER"},
	} {
		rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+tbl.ID+"/columns", owner,
			map[string]interface{}{"display_name": col.name, "name": col.name, "field_type": col.kind}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}
	var row map[string]interface{}
	rec = h.do(http.MethodPost, "/api/v1/data/customers", owner,
		map[string]interface{}{"last_name": "Durand", "city": "Paris", "fee_paid": 100}, &row)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	id, _ := row["id"].(string)
	require.NotEmpty(t, id)

	evaluate := func(body map[string]interface{}) []evaluationResult {
		var answer struct {
			Results []evaluationResult `json:"results"`
		}
		rec := h.do(http.MethodPost, "/api/v1/data/customers/evaluate", owner, body, &answer)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return answer.Results
	}

	// A formula over the record in hand, in the shape the result type asks
	// for: the viewer and a calculation field see the same answer.
	results := evaluate(map[string]interface{}{
		"expressions": []string{"fee_paid * 2", "UPPER(last_name) & \", \" & city"},
		"record_id":   id,
		"result_type": "Number",
	})
	require.Len(t, results, 2)
	assert.InDelta(t, 200, results[0].Value, 0.001)
	assert.Equal(t, "200", results[0].Text)
	assert.Equal(t, []string{"fee_paid"}, results[0].Fields)
	assert.Equal(t, "NUMBER", results[0].ResultType)

	// Text comes back as text when that is what is asked for.
	results = evaluate(map[string]interface{}{
		"expression":  "UPPER(last_name) & \", \" & city",
		"record_id":   id,
		"result_type": "Text",
	})
	require.Len(t, results, 1)
	assert.Equal(t, "DURAND, Paris", results[0].Text)
	assert.ElementsMatch(t, []string{"city", "last_name"}, results[0].Fields)

	// Without a record every field reads as empty, so a constant formula
	// still answers — which is how the viewer is used before a record is
	// chosen.
	results = evaluate(map[string]interface{}{"expression": "UPPER(\"ab\") & 1 + 1", "result_type": "Text"})
	require.Len(t, results, 1)
	assert.Empty(t, results[0].Error)

	// A field the table does not have reads as empty, as the engine says,
	// and the answer names it rather than being quietly wrong.
	results = evaluate(map[string]interface{}{
		"expression": "postcode & last_name", "record_id": id, "result_type": "Text",
	})
	require.Len(t, results, 1)
	assert.Equal(t, []string{"postcode"}, results[0].UnknownFields)
	assert.Equal(t, "Durand", results[0].Text)

	// One expression that does not parse does not blank the others: a
	// viewer shows a list, and a typo in one line is one line's problem.
	results = evaluate(map[string]interface{}{
		"expressions": []string{"fee_paid *", "fee_paid + 1"},
		"record_id":   id,
		"result_type": "Number",
	})
	require.Len(t, results, 2)
	assert.NotEmpty(t, results[0].Error)
	assert.Contains(t, results[0].Error, "position")
	assert.Empty(t, results[1].Error)
	assert.InDelta(t, 101, results[1].Value, 0.001)

	// What is about the request, rather than about one expression, is an
	// error on the request.
	rec = h.do(http.MethodPost, "/api/v1/data/customers/evaluate", owner,
		map[string]interface{}{"expression": "1", "record_id": "no-such-record"}, nil)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = h.do(http.MethodPost, "/api/v1/data/customers/evaluate", owner, map[string]interface{}{}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "1"
	}
	rec = h.do(http.MethodPost, "/api/v1/data/customers/evaluate", owner,
		map[string]interface{}{"expressions": tooMany}, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	// Evaluating reads the table, so it is bound by the table's access.
	rec = h.do(http.MethodPost, "/api/v1/security/users", owner,
		map[string]interface{}{"username": "carol", "password": "carol-secret-pw", "role": "user"}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var occurrences []struct {
		ID          string `json:"id"`
		BaseTableID string `json:"base_table_id"`
	}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/occurrences", owner, nil, &occurrences).Code)
	occurrence := ""
	for _, o := range occurrences {
		if o.BaseTableID == tbl.ID {
			occurrence = o.ID
		}
	}
	var layout struct {
		ID string `json:"id"`
	}
	rec = h.do(http.MethodPost, "/api/v1/schemas/layouts", owner,
		map[string]interface{}{"name": "Customers Form", "table_occurrence_id": occurrence}, &layout)
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
	rec = h.do(http.MethodPut, "/api/v1/security/users/"+carol+"/permissions", owner,
		map[string]interface{}{"permissions": []map[string]string{
			{"layout_id": layout.ID, "access_level": "none"},
		}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	carolToken := h.login(db, "carol", "carol-secret-pw")
	rec = h.do(http.MethodPost, "/api/v1/data/customers/evaluate", carolToken,
		map[string]interface{}{"expression": "last_name", "record_id": id}, nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
