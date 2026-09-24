package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTraceContextGeneration(t *testing.T) {
	traceID := GenerateTraceID()
	assert.Len(t, traceID, 32)

	spanID := GenerateSpanID()
	assert.Len(t, spanID, 16)

	header := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tID, sID, ok := ParseTraceparent(header)
	assert.True(t, ok)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", tID)
	assert.Equal(t, "00f067aa0ba902b7", sID)

	// Invalid header
	_, _, ok = ParseTraceparent("invalid-header")
	assert.False(t, ok)
}

func TestProblemDetails(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/schemas/tables/test", nil)
	ctx := ContextWithTrace(req.Context(), "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7")
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	WriteProblem(rr, req, http.StatusNotFound, "Table Not Found", "Table 'test' does not exist")

	assert.Equal(t, http.StatusNotFound, rr.Code)
	assert.Equal(t, "application/problem+json; charset=utf-8", rr.Header().Get("Content-Type"))

	var prob ProblemDetails
	err := json.Unmarshal(rr.Body.Bytes(), &prob)
	assert.NoError(t, err)
	assert.Equal(t, "Table Not Found", prob.Title)
	assert.Equal(t, "Table 'test' does not exist", prob.Detail)
	assert.Equal(t, "/api/v1/schemas/tables/test", prob.Instance)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", prob.TraceID)
}

func TestMiddleware(t *testing.T) {
	handler := Middleware("test-service", "1.0.0")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tID := TraceIDFromContext(r.Context())
		assert.NotEmpty(t, tID)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("traceparent"))
	assert.NotEmpty(t, rr.Header().Get("X-Trace-ID"))
	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "SAMEORIGIN", rr.Header().Get("X-Frame-Options"))
}
