package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChangeEventFor(t *testing.T) {
	cases := []struct {
		method, path      string
		wantOK            bool
		wantType, wantRes string
	}{
		{http.MethodPost, "/api/v1/data/customers", true, "data", "customers"},
		{http.MethodPut, "/api/v1/data/customers/123", true, "data", "customers"},
		{http.MethodDelete, "/api/v1/data/orders/9", true, "data", "orders"},
		{http.MethodPost, "/api/v1/data/customers/import", true, "data", "customers"},
		{http.MethodPost, "/api/v1/data/customers/find", false, "", ""},
		{http.MethodPost, "/api/v1/data/customers/summary", false, "", ""},
		{http.MethodPost, "/api/v1/data/customers/import/preview", false, "", ""},
		{http.MethodGet, "/api/v1/data/customers", false, "", ""},
		{http.MethodPost, "/api/v1/schemas/layouts", true, "schema", "schemas"},
		{http.MethodPut, "/api/v1/layouts/abc", true, "schema", "layouts"},
		{http.MethodPost, "/api/v1/scripts", true, "schema", "scripts"},
		{http.MethodPost, "/api/v1/solutions/import", true, "schema", "solution"},
		{http.MethodPost, "/api/v1/data/customers/related", false, "", ""},
		{http.MethodPost, "/api/v1/data/customers/7/related", true, "data", "customers"},
		{http.MethodPost, "/api/v1/auth/login", false, "", ""},
		{http.MethodDelete, "/api/v1/server/backups/x", false, "", ""},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		ev, ok := changeEventFor(r, "")
		if ok != c.wantOK {
			t.Errorf("%s %s: ok=%v want %v", c.method, c.path, ok, c.wantOK)
			continue
		}
		if ok && (ev.Type != c.wantType || ev.Resource != c.wantRes) {
			t.Errorf("%s %s: got %s/%s want %s/%s", c.method, c.path, ev.Type, ev.Resource, c.wantType, c.wantRes)
		}
	}
}

// The record routes of an occurrence name the occurrence, not the table: a
// write reports the table the route resolved, and a read nothing.
func TestChangeEventFor_OccurrenceRecords(t *testing.T) {
	cases := []struct {
		method, path, table string
		wantOK              bool
	}{
		{http.MethodPost, "/api/v1/occurrences/o1/records", "customers", true},
		{http.MethodPut, "/api/v1/occurrences/o1/records/5", "customers", true},
		{http.MethodPost, "/api/v1/occurrences/o1/records/find", "", false},
		{http.MethodPost, "/api/v1/occurrences/o1/records/related", "", false},
		{http.MethodDelete, "/api/v1/occurrences/o1", "", true},
	}
	for _, c := range cases {
		ev, ok := changeEventFor(httptest.NewRequest(c.method, c.path, nil), c.table)
		if ok != c.wantOK {
			t.Errorf("%s %s: ok=%v want %v", c.method, c.path, ok, c.wantOK)
			continue
		}
		if ok && c.table != "" && (ev.Type != "data" || ev.Resource != c.table) {
			t.Errorf("%s %s: got %s/%s want data/%s", c.method, c.path, ev.Type, ev.Resource, c.table)
		}
	}
}
