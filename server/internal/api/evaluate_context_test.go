package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/file4base/file4base-app/server/internal/api"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The rows of a portal of Peer's customers on an Orders layout are
// evaluated in the layout's record: they are read in Peer, as the account
// alice holds there, and a field of the layout reads the order (#222).
func TestAPI_EvaluateExternalPortalRowsInTheLayoutsRecord(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token

	var customers []map[string]interface{}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/customers", p.peerOwner, nil, &customers).Code)
	ids := map[string]string{}
	for _, c := range customers {
		ids[fmt.Sprint(c["full_name"])] = fmt.Sprint(c["id"])
	}
	require.Len(t, ids, 3)

	body := map[string]interface{}{
		"expressions": []string{"orders::region", `full_name & "/" & ${Order Customers}::full_name`,
			"Count ( ${Order Customers}::full_name )", "Self"},
		"record_ids":  []string{ids["Ada"], ids["Bob"]},
		"self":        []interface{}{nil, nil, nil, map[string]string{"field": "code"}},
		"context":     map[string]string{"occurrence": p.ordersOcc, "record_id": p.orderIDs["north"]},
		"result_type": "Text",
	}
	path := "/api/v1/occurrences/" + p.ext + "/records/evaluate"
	evaluate := func() (int, map[string][]evaluationResult) {
		var answer struct {
			Records map[string][]evaluationResult `json:"records"`
		}
		rec := h.do(http.MethodPost, path, token, body, &answer)
		return rec.Code, answer.Records
	}
	code, records := evaluate()
	require.Equal(t, http.StatusOK, code)
	texts := func(results []evaluationResult) []string {
		out := []string{}
		for _, r := range results {
			out = append(out, r.Text+r.Error)
		}
		return out
	}
	assert.Equal(t, []string{"north", "Ada/Ada", "1", "C-1"}, texts(records[ids["Ada"]]))
	assert.Equal(t, []string{"north", "Bob/Bob", "1", "C-2"}, texts(records[ids["Bob"]]))

	t.Run("a layout's record or occurrence that is not there", func(t *testing.T) {
		gone := map[string]interface{}{"expression": "full_name", "record_ids": []string{ids["Ada"]},
			"context": map[string]string{"occurrence": p.ordersOcc, "record_id": "no-such-order"}}
		rec := h.do(http.MethodPost, path, token, gone, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		gone["context"] = map[string]string{"occurrence": "no such occurrence", "record_id": p.orderIDs["north"]}
		rec = h.do(http.MethodPost, path, token, gone, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("on a table of this solution named as the other's", func(t *testing.T) {
		rec := h.do(http.MethodPost, "/api/v1/schemas/tables", p.owner,
			map[string]string{"display_name": "customers", "custom_name": "customers"}, nil)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		local := map[string]interface{}{"expression": "1", "record_ids": []string{ids["Ada"]}, "occurrence": p.ext,
			"context": map[string]string{"occurrence": p.ordersOcc, "record_id": p.orderIDs["north"]}}
		// By its id or its name, however spaced: the occurrence is resolved
		// once, where it is read.
		for _, named := range []string{p.ext, " Order Customers ", "order customers"} {
			local["occurrence"] = named
			rec = h.do(http.MethodPost, "/api/v1/data/customers/evaluate", p.owner, local, nil)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "%q: %s", named, rec.Body.String())
			assert.Contains(t, rec.Body.String(), "Occurrence of Another Solution", named)
		}
	})

	t.Run("without an active row, the first related", func(t *testing.T) {
		plain := map[string]interface{}{"expression": "${Order Customers}::full_name", "record_id": p.orderIDs["north"],
			"occurrence": p.ordersOcc, "result_type": "Text"}
		var first struct {
			Results []evaluationResult `json:"results"`
		}
		rec := h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, plain, &first)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, first.Results, 1)
		assert.Equal(t, "Ada", first.Results[0].Text)
	})

	t.Run("without the layout's record", func(t *testing.T) {
		rec := h.do(http.MethodPost, path, token, map[string]interface{}{"expression": "full_name", "record_id": ids["Ada"]}, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("no access in the other solution", func(t *testing.T) {
		p.setPeerAccess(t, schema.AccessNone)
		defer p.setPeerAccess(t, schema.AccessReadOnly)
		rec := h.do(http.MethodPost, path, token, body, nil)
		assertUnavailable(t, rec.Result(), p.peer, "no_access")
	})

	t.Run("not signed in to the other solution", func(t *testing.T) {
		rec := h.do(http.MethodDelete, "/api/v1/auth/link/"+p.peer, token, nil, nil)
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
		rec = h.do(http.MethodPost, path, token, body, nil)
		assertUnavailable(t, rec.Result(), p.peer, "not_signed_in")
	})
}

// A script run from a row of a portal of Peer's customers evaluates on the
// layout's order with that row active: a field of the portal's occurrence
// reads the row, read in Peer as the account alice holds there, and the
// order stays the current record (#237).
func TestAPI_EvaluateWithAnExternalActivePortalRow(t *testing.T) {
	p := newExternalPair(t, api.Options{}, "alice-secret")
	h := p.h
	token := p.signIn(t, p.orders, "alice-secret").Token

	var customers []map[string]interface{}
	require.Equal(t, http.StatusOK, h.do(http.MethodGet, "/api/v1/data/customers", p.peerOwner, nil, &customers).Code)
	ids := map[string]string{}
	for _, c := range customers {
		ids[fmt.Sprint(c["full_name"])] = fmt.Sprint(c["id"])
	}
	require.Len(t, ids, 3)
	// A second customer related to the north order, after Ada: the row
	// active is the one given, not the portal's first.
	var second map[string]interface{}
	rec := h.do(http.MethodPost, "/api/v1/data/customers", p.peerOwner,
		map[string]string{"code": "C-1", "region": "north", "full_name": "Ada Two"}, &second)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	ids["Ada Two"] = fmt.Sprint(second["id"])

	body := map[string]interface{}{
		"expressions": []string{"orders::region", "${Order Customers}::full_name", "region"},
		"record_id":   p.orderIDs["north"],
		"occurrence":  p.ordersOcc,
		"portal_row":  map[string]string{"occurrence": "Order Customers", "record_id": ids["Ada Two"]},
		"result_type": "Text",
	}
	var answer struct {
		Results []evaluationResult `json:"results"`
	}
	rec = h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, body, &answer)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := []string{}
	for _, r := range answer.Results {
		got = append(got, r.Text+r.Error)
	}
	assert.Equal(t, []string{"north", "Ada Two", "north"}, got)

	t.Run("without the layout's record", func(t *testing.T) {
		alone := map[string]interface{}{"expression": "1", "occurrence": p.ordersOcc,
			"portal_row": map[string]string{"occurrence": p.ext, "record_id": ids["Ada"]}}
		rec := h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, alone, nil)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("a row that is not there", func(t *testing.T) {
		gone := map[string]interface{}{"expression": "1", "record_id": p.orderIDs["north"], "occurrence": p.ordersOcc,
			"portal_row": map[string]string{"occurrence": p.ext, "record_id": "no-such-customer"}}
		rec := h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, gone, nil)
		assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	})

	t.Run("with a context or several records", func(t *testing.T) {
		for _, extra := range []map[string]interface{}{
			{"context": map[string]string{"occurrence": p.ordersOcc, "record_id": p.orderIDs["north"]}},
			{"record_ids": []string{p.orderIDs["north"]}},
		} {
			mixed := map[string]interface{}{"expression": "1", "occurrence": p.ordersOcc,
				"portal_row": map[string]string{"occurrence": p.ext, "record_id": ids["Ada"]}}
			for k, v := range extra {
				mixed[k] = v
			}
			rec := h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, mixed, nil)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		}
	})

	t.Run("no access in the other solution", func(t *testing.T) {
		p.setPeerAccess(t, schema.AccessNone)
		defer p.setPeerAccess(t, schema.AccessReadOnly)
		rec := h.do(http.MethodPost, "/api/v1/data/orders/evaluate", token, body, nil)
		assertUnavailable(t, rec.Result(), p.peer, "no_access")
	})
}
