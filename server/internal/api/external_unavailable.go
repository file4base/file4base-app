package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// externalUnavailableProblem is the 424 a request gets when it reaches an
// occurrence of another solution's table whose records cannot be read: the
// problem details, with the file reference, the solution and the reason.
type externalUnavailableProblem struct {
	telemetry.ProblemDetails
	Code      string `json:"code"`
	Reference string `json:"reference"`
	Database  string `json:"database"`
	Reason    string `json:"reason"`
}

// writeExternalUnavailable answers 424 Failed Dependency for a read through
// an external table occurrence (#107). Nothing of this solution is read in
// its place.
func writeExternalUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	prob := externalUnavailableProblem{
		ProblemDetails: telemetry.ProblemDetails{
			Type:     "https://file4base.org/errors/external-unavailable",
			Title:    "External Table Unavailable",
			Status:   http.StatusFailedDependency,
			Detail:   err.Error(),
			Instance: r.URL.Path,
			TraceID:  telemetry.TraceIDFromContext(r.Context()),
		},
		Code:   "external_unavailable",
		Reason: data.ExternalMissing,
	}
	var e *data.ExternalUnavailableError
	if errors.As(err, &e) {
		prob.Reference, prob.Database = e.Reference, e.Database
		if e.Reason != "" {
			prob.Reason = e.Reason
		}
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(http.StatusFailedDependency)
	_ = json.NewEncoder(w).Encode(prob)
}

// partialDeleteProblem is the 500 a cascading delete answers when it deleted
// related records in another solution and then failed in this one (#107):
// partial is true, and committed names the solutions already changed.
type partialDeleteProblem struct {
	telemetry.ProblemDetails
	Code      string   `json:"code"`
	Partial   bool     `json:"partial"`
	Committed []string `json:"committed"`
	Database  string   `json:"database"`
}

func writePartialDelete(w http.ResponseWriter, r *http.Request, err error) {
	prob := partialDeleteProblem{
		ProblemDetails: telemetry.ProblemDetails{
			Type:     "https://file4base.org/errors/partial-delete",
			Title:    "Delete Partly Done",
			Status:   http.StatusInternalServerError,
			Detail:   err.Error(),
			Instance: r.URL.Path,
			TraceID:  telemetry.TraceIDFromContext(r.Context()),
		},
		Code:      "partial_delete",
		Partial:   true,
		Committed: []string{},
	}
	var e *data.PartialDeleteError
	if errors.As(err, &e) {
		prob.Committed, prob.Database = e.Committed, e.Database
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(prob)
}
