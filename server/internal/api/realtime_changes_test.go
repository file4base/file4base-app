package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/realtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drain returns the events a subscription holds, by database and resource.
func drain(sub *realtime.Subscription) []string {
	out := []string{}
	for {
		select {
		case ev := <-sub.Events:
			out = append(out, ev.Database+"/"+ev.Resource)
		default:
			return out
		}
	}
}

// A write through an external occurrence is told to the solution it changed,
// and a cascade's tables to theirs, even when the request failed after
// another solution committed (#107).
func TestEmitChangeEvents_TellsEverySolutionAWriteChanged(t *testing.T) {
	hub := realtime.NewHub()
	own, peer := hub.Subscribe("orders_db"), hub.Subscribe("peer_db")
	defer own.Close()
	defer peer.Close()

	serve := func(method, path string, status int, work func(r *http.Request)) {
		h := emitChangeEvents(hub)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			work(r)
			w.WriteHeader(status)
		}))
		r := httptest.NewRequest(method, path, nil)
		r = r.WithContext(auth.WithSession(r.Context(), auth.Session{Database: "orders_db"}))
		h.ServeHTTP(httptest.NewRecorder(), r)
	}

	// Editing a record of Peer's customers from a layout of Orders.
	serve(http.MethodPut, "/api/v1/occurrences/o1/records/5", http.StatusOK, func(r *http.Request) {
		noteChangedTable(r.Context(), "peer_db", "customers")
	})
	assert.Equal(t, []string{"peer_db/customers"}, drain(peer))
	assert.Empty(t, drain(own))

	// A local occurrence's write is told under its table.
	serve(http.MethodPut, "/api/v1/occurrences/o2/records/5", http.StatusOK, func(r *http.Request) {
		noteChangedTable(r.Context(), "", "orders")
	})
	assert.Equal(t, []string{"orders_db/orders"}, drain(own))

	// A refused write changes nothing.
	serve(http.MethodPut, "/api/v1/occurrences/o1/records/5", http.StatusForbidden, func(r *http.Request) {
		noteChangedTable(r.Context(), "peer_db", "customers")
	})
	assert.Empty(t, drain(peer))

	// A cascade that deleted Peer's customers, then failed in Orders: the
	// customers are gone and told, the orders are not. The data service
	// reports what it committed through the observer the middleware gave it.
	serve(http.MethodDelete, "/api/v1/data/orders/9", http.StatusInternalServerError, func(r *http.Request) {
		r.Context().Value(changedTableKey{}).(*changedTables).add("peer_db", "customers")
	})
	assert.Equal(t, []string{"peer_db/customers"}, drain(peer))
	assert.Empty(t, drain(own))
}

func TestWriteDataError_PartialDelete(t *testing.T) {
	rec := httptest.NewRecorder()
	err := fmt.Errorf("deleting: %w", &data.PartialDeleteError{Committed: []string{"peer_db"}, Database: "orders_db"})
	writeDataError(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/data/orders/9", nil), http.StatusInternalServerError, "Delete Failed", err)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var body struct {
		Code      string   `json:"code"`
		Partial   bool     `json:"partial"`
		Committed []string `json:"committed"`
		Database  string   `json:"database"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "partial_delete", body.Code)
	assert.True(t, body.Partial)
	assert.Equal(t, []string{"peer_db"}, body.Committed)
	assert.Equal(t, "orders_db", body.Database)
}
