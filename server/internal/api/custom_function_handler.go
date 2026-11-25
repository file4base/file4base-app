package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// Manage Custom Functions (docs/specs/custom_functions.md).
//
//	GET    /api/v1/custom-functions               list (full-access-only functions for full access only)
//	POST   /api/v1/custom-functions               create
//	GET    /api/v1/custom-functions/{id}          one
//	PUT    /api/v1/custom-functions/{id}          change; a rename rewrites the formulas that call it
//	DELETE /api/v1/custom-functions/{id}          delete (?force=true with formulas calling it)
//	GET    /api/v1/custom-functions/{id}/usages   the formulas that call it
//	POST   /api/v1/formulas/check                 syntax check of a formula being written
//
// Reading the list is every signed-in user's, for the calculation editor;
// the rest is design, for full access (owner, admin).
func (h *SchemaHandler) registerCustomFunctionRoutes(r chi.Router) {
	r.Route("/api/v1/custom-functions", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/", h.ListCustomFunctions)
		r.With(RequireAdmin, RequireDesignUnlocked).Post("/", h.CreateCustomFunction)
		r.With(RequireAdmin).Get("/{id}", h.GetCustomFunction)
		r.With(RequireAdmin, RequireDesignUnlocked).Put("/{id}", h.UpdateCustomFunction)
		r.With(RequireAdmin, RequireDesignUnlocked).Delete("/{id}", h.DeleteCustomFunction)
		r.With(RequireAdmin).Get("/{id}/usages", h.CustomFunctionUsages)
	})
	r.Route("/api/v1/formulas", func(r chi.Router) {
		r.Use(RequireSession)
		r.Post("/check", h.CheckFormula)
	})
}

func isFullAccess(r *http.Request) bool {
	role := currentSession(r).Role
	return role == auth.RoleOwner || role == auth.RoleAdmin
}

func (h *SchemaHandler) ListCustomFunctions(w http.ResponseWriter, r *http.Request) {
	all, err := schemaService(r).ListCustomFunctions(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	full := isFullAccess(r)
	out := make([]schema.CustomFunction, 0, len(all))
	for _, fn := range all {
		if fn.Access == schema.CustomFunctionAccessFullAccess && !full {
			continue
		}
		out = append(out, fn)
	}
	writeCustomFunctionJSON(w, http.StatusOK, map[string]interface{}{"custom_functions": out})
}

func (h *SchemaHandler) GetCustomFunction(w http.ResponseWriter, r *http.Request) {
	fn, err := schemaService(r).GetCustomFunction(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeCustomFunctionError(w, r, err)
		return
	}
	writeCustomFunctionJSON(w, http.StatusOK, fn)
}

func (h *SchemaHandler) CreateCustomFunction(w http.ResponseWriter, r *http.Request) {
	var in schema.CustomFunctionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	fn, err := schemaService(r).CreateCustomFunction(r.Context(), in)
	if err != nil {
		writeCustomFunctionError(w, r, err)
		return
	}
	writeCustomFunctionJSON(w, http.StatusCreated, fn)
}

func (h *SchemaHandler) UpdateCustomFunction(w http.ResponseWriter, r *http.Request) {
	var in schema.CustomFunctionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	fn, err := schemaService(r).UpdateCustomFunction(r.Context(), chi.URLParam(r, "id"), in, forceParam(r))
	if err != nil {
		writeCustomFunctionError(w, r, err)
		return
	}
	writeCustomFunctionJSON(w, http.StatusOK, fn)
}

func (h *SchemaHandler) DeleteCustomFunction(w http.ResponseWriter, r *http.Request) {
	if err := schemaService(r).DeleteCustomFunction(r.Context(), chi.URLParam(r, "id"), forceParam(r)); err != nil {
		writeCustomFunctionError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) CustomFunctionUsages(w http.ResponseWriter, r *http.Request) {
	usages, err := schemaService(r).CustomFunctionUsages(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeCustomFunctionError(w, r, err)
		return
	}
	writeCustomFunctionJSON(w, http.StatusOK, map[string]interface{}{"usages": usages})
}

func (h *SchemaHandler) CheckFormula(w http.ResponseWriter, r *http.Request) {
	var in schema.FormulaCheckInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	check, err := schemaService(r).CheckFormula(r.Context(), in)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	writeCustomFunctionJSON(w, http.StatusOK, check)
}

func forceParam(r *http.Request) bool {
	v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("force")))
	return v == "1" || v == "true" || v == "yes"
}

// writeCustomFunctionError maps the custom function errors: a conflict lists
// the formulas it is about.
func writeCustomFunctionError(w http.ResponseWriter, r *http.Request, err error) {
	var conflict *schema.CustomFunctionConflict
	switch {
	case errors.As(err, &conflict):
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"type":     "https://file4base.org/errors/custom-function-in-use",
			"title":    "Custom Function In Use",
			"status":   http.StatusConflict,
			"detail":   conflict.Message,
			"instance": r.URL.Path,
			"usages":   conflict.Usages,
		})
	case errors.Is(err, schema.ErrCustomFunctionNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Not Found", err.Error())
	case errors.Is(err, schema.ErrCustomFunctionChanged):
		// Not a 409: saving with force would not help, saving again does.
		telemetry.WriteProblem(w, r, http.StatusPreconditionFailed, "Changed Meanwhile", err.Error())
	case errors.Is(err, schema.ErrInvalidCustomFunction):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Custom Function", err.Error())
	default:
		telemetry.WriteInternalError(w, r, err)
	}
}

func writeCustomFunctionJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
