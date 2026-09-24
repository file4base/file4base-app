package telemetry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProblemDetails represents an RFC 9457 compliant error response.
type ProblemDetails struct {
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail"`
	Instance      string         `json:"instance,omitempty"`
	InvalidParams []InvalidParam `json:"invalid_params,omitempty"`
	TraceID       string         `json:"trace_id,omitempty"`
}

// InvalidParam represents a single field validation error.
type InvalidParam struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// DefaultTypeForStatus returns a standard URI for the given status code.
func DefaultTypeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "https://file4base.org/errors/bad-request"
	case http.StatusUnauthorized:
		return "https://file4base.org/errors/unauthorized"
	case http.StatusForbidden:
		return "https://file4base.org/errors/forbidden"
	case http.StatusNotFound:
		return "https://file4base.org/errors/not-found"
	case http.StatusConflict:
		return "https://file4base.org/errors/conflict"
	case http.StatusUnprocessableEntity:
		return "https://file4base.org/errors/validation-failed"
	case http.StatusTooManyRequests:
		return "https://file4base.org/errors/rate-limit-exceeded"
	case http.StatusServiceUnavailable:
		return "https://file4base.org/errors/service-unavailable"
	default:
		return "https://file4base.org/errors/internal-error"
	}
}

// WriteProblem serializes and writes an RFC 9457 problem details JSON response.
func WriteProblem(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	traceID := ""
	if r != nil {
		traceID = TraceIDFromContext(r.Context())
	}

	instance := ""
	if r != nil {
		instance = r.URL.Path
	}

	prob := ProblemDetails{
		Type:     DefaultTypeForStatus(status),
		Title:    title,
		Status:   status,
		Detail:   strings.TrimSpace(detail),
		Instance: instance,
		TraceID:  traceID,
	}

	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(prob)
}

// WriteValidationProblem writes a 422 Unprocessable Content error with invalid parameters list.
func WriteValidationProblem(w http.ResponseWriter, r *http.Request, detail string, invalidParams []InvalidParam) {
	traceID := ""
	if r != nil {
		traceID = TraceIDFromContext(r.Context())
	}

	instance := ""
	if r != nil {
		instance = r.URL.Path
	}

	prob := ProblemDetails{
		Type:          "https://file4base.org/errors/validation-failed",
		Title:         "Validation Failed",
		Status:        http.StatusUnprocessableEntity,
		Detail:        detail,
		Instance:      instance,
		InvalidParams: invalidParams,
		TraceID:       traceID,
	}

	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(prob)
}

// WriteInternalError formats any Go error into a 500 ProblemDetails response.
func WriteInternalError(w http.ResponseWriter, r *http.Request, err error) {
	detail := "An unexpected server error occurred."
	if err != nil {
		detail = fmt.Sprintf("%v", err)
	}
	WriteProblem(w, r, http.StatusInternalServerError, "Internal Server Error", detail)
}
