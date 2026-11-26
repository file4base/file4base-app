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
