package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedactPath(t *testing.T) {
	for in, want := range map[string]string{
		"/fmi/data/v1/databases/Sales/sessions/0123abcd":   "/fmi/data/v1/databases/Sales/sessions/[redacted]",
		"/fmi/data/v1/databases/Sales/SESSIONS/tok/extra":  "/fmi/data/v1/databases/Sales/SESSIONS/[redacted]",
		"/FMI/DATA/v1/databases/Sales/sessions//tok":       "/FMI/DATA/v1/databases/Sales/sessions/[redacted]",
		"/fmi/data/v1/databases/Sales/sessions":            "/fmi/data/v1/databases/Sales/sessions",
		"/fmi/data/v1/databases/Sales/sessions/":           "/fmi/data/v1/databases/Sales/sessions/",
		"/fmi/data/v1/databases/Sales/layouts/L/records/1": "/fmi/data/v1/databases/Sales/layouts/L/records/1",
		"/api/v1/sessions/abc":                             "/api/v1/sessions/abc",
		"//fmi/data/v1/databases/Sales/sessions/tok":       "//fmi/data/v1/databases/Sales/sessions/[redacted]",
	} {
		if got := redactPath(in); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDataAPIRepliesCarryNoTraceHeaders(t *testing.T) {
	h := Middleware("test", "0", nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for path, traced := range map[string]bool{"/fmi/data/v1/productInfo": false, "/api/v1/ping": true} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if got := w.Header().Get("traceparent") != ""; got != traced {
			t.Errorf("%s: traceparent present %v, want %v", path, got, traced)
		}
	}
}
