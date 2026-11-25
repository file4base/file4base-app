package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An unstored calculation of Orders reads Peer's customers, and a find on a
// field of Peer's customers finds Orders' orders, as the account the
// session group holds in Peer (#107): with its access, and nothing of Peer
// when the group holds no session there.
func TestAPI_ExternalCalculationsAndFinds(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token

	rec := h.do(http.MethodPut, "/api/v1/schemas/occurrences/"+p.ext, p.owner, map[string]string{"name": "order_customers"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var tables []schema.TableMetadata
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/schemas/tables", p.owner, nil, &tables).Code)
	ordersTable := ""
	for _, tbl := range tables {
		if tbl.Name == "orders" {
			ordersTable = tbl.ID
		}
	}
	require.NotEmpty(t, ordersTable)
	rec = h.do(http.MethodPost, "/api/v1/schemas/tables/"+ordersTable+"/columns", p.owner, map[string]interface{}{
		"name": "customer_names", "display_name": "customer_names", "field_type": "CALCULATION", "is_nullable": true,
		"calculation_formula": `{"formula":"List ( order_customers::full_name )","result_type":"Text","stored":false}`,
	}, nil)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	read := func() interface{} {
		t.Helper()
		var row map[string]interface{}
		rec := h.do(http.MethodGet, "/api/v1/data/orders/"+p.orderIDs["north"], token, nil, &row)
		require.Equal(t, http.StatusOK, rec.Code, "the record is read whatever the other solution answers: %s", rec.Body.String())
		return row["customer_names"]
	}
	evaluate := func() evaluationResult {
		t.Helper()
		var answer struct {
			Results []evaluationResult `json:"results"`
		}
		rec := h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, map[string]interface{}{
			"expression": "List ( order_customers::full_name )", "record_id": p.orderIDs["north"],
		}, &answer)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, answer.Results, 1)
		return answer.Results[0]
	}
	find := func() *http.Response {
		rec := h.do(http.MethodPost, "/api/v1/data/orders/find", token, map[string]interface{}{
			"requests": []map[string]interface{}{{"criteria": []map[string]string{
				{"field_name": "full_name", "operator": "=", "value": "Ada", "relationship_id": p.relationship}}}},
		}, nil)
		return rec.Result()
	}

	assert.Equal(t, "Ada", read())
	res := find()
	require.Equal(t, http.StatusOK, res.StatusCode)
	var found []map[string]interface{}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&found))
	require.Len(t, found, 1, "both criteria of the relationship hold: only the north order")
	assert.Equal(t, p.orderIDs["north"], fmt.Sprint(found[0]["id"]))

	t.Run("no access in the other solution", func(t *testing.T) {
		p.setPeerAccess(t, schema.AccessNone)
		defer p.setPeerAccess(t, schema.AccessReadOnly)
		assert.Nil(t, read())
		assert.Contains(t, evaluate().Error, "<No Access>")
		assertUnavailable(t, find(), p.peer, "no_access")
	})

	t.Run("not signed in to the other solution", func(t *testing.T) {
		rec := h.do(http.MethodDelete, "/api/v1/auth/link/"+p.peer, token, nil, nil)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		assert.Nil(t, read())
		assert.Contains(t, evaluate().Error, "sign in to "+p.peer)
		assertUnavailable(t, find(), p.peer, "not_signed_in")
	})
}
