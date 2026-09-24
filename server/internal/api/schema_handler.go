package api

import (
	"encoding/json"
	"net/http"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

type SchemaHandler struct {
	svc *schema.Service
}

func NewSchemaHandler(svc *schema.Service) *SchemaHandler {
	return &SchemaHandler{svc: svc}
}

func (h *SchemaHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/schemas", func(r chi.Router) {
		r.Get("/tables", h.ListTables)
		r.Post("/tables", h.CreateTable)
		r.Post("/tables/{id}/columns", h.AddColumn)
		r.Put("/tables/{id}/columns/{columnId}", h.UpdateColumn)
		r.Delete("/tables/{id}/columns/{columnId}", h.DeleteColumn)
		r.Get("/occurrences", h.ListOccurrences)
		r.Get("/layouts", h.ListLayouts)
		r.Post("/layouts", h.CreateLayout)
		r.Get("/layouts/{id}", h.GetLayout)
		r.Put("/layouts/{id}", h.UpdateLayout)
		r.Delete("/layouts/{id}", h.DeleteLayout)
	})

	// Also support top-level /api/v1/layouts
	r.Route("/api/v1/layouts", func(r chi.Router) {
		r.Get("/", h.ListLayouts)
		r.Post("/", h.CreateLayout)
		r.Get("/{id}", h.GetLayout)
		r.Put("/{id}", h.UpdateLayout)
		r.Delete("/{id}", h.DeleteLayout)
	})
}

func (h *SchemaHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	tables, err := h.svc.ListTables(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tables)
}

type CreateTableRequest struct {
	DisplayName string `json:"display_name"`
	CustomName  string `json:"custom_name,omitempty"`
}

func (h *SchemaHandler) CreateTable(w http.ResponseWriter, r *http.Request) {
	var req CreateTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if req.DisplayName == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "display_name is required")
		return
	}

	tbl, err := h.svc.CreateTable(r.Context(), req.DisplayName, req.CustomName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(tbl)
}

type AddColumnRequest struct {
	Name               string                 `json:"name"`
	DisplayName        string                 `json:"display_name"`
	FieldType          dbal.AgnosticFieldType `json:"field_type"`
	IsNullable         bool                   `json:"is_nullable"`
	DefaultValue       *string                `json:"default_value,omitempty"`
	CalculationFormula *string                `json:"calculation_formula,omitempty"`
	ValidationRules    *string                `json:"validation_rules,omitempty"`
}

func (h *SchemaHandler) AddColumn(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	if tableID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id parameter is required")
		return
	}

	var req AddColumnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if req.Name == "" || req.DisplayName == "" || req.FieldType == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "name, display_name and field_type are required")
		return
	}

	col, err := h.svc.AddColumn(r.Context(), tableID, schema.ColumnMetadata{
		Name:               req.Name,
		DisplayName:        req.DisplayName,
		FieldType:          req.FieldType,
		IsNullable:         req.IsNullable,
		DefaultValue:       req.DefaultValue,
		CalculationFormula: req.CalculationFormula,
		ValidationRules:    req.ValidationRules,
	})
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(col)
}

type UpdateColumnRequest struct {
	DisplayName string `json:"display_name"`
}

func (h *SchemaHandler) UpdateColumn(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	columnID := chi.URLParam(r, "columnId")
	if tableID == "" || columnID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id and column id are required")
		return
	}

	var req UpdateColumnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	col, err := h.svc.UpdateColumn(r.Context(), tableID, columnID, req.DisplayName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(col)
}

func (h *SchemaHandler) DeleteColumn(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	columnID := chi.URLParam(r, "columnId")
	if tableID == "" || columnID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id and column id are required")
		return
	}

	if err := h.svc.DeleteColumn(r.Context(), tableID, columnID); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) ListOccurrences(w http.ResponseWriter, r *http.Request) {
	occurrences, err := h.svc.ListTableOccurrences(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(occurrences)
}

func (h *SchemaHandler) ListLayouts(w http.ResponseWriter, r *http.Request) {
	layouts, err := h.svc.ListLayouts(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(layouts)
}

type CreateLayoutRequest struct {
	Name              string          `json:"name"`
	TableOccurrenceID string          `json:"table_occurrence_id"`
	Definition        json.RawMessage `json:"definition"`
}

func (h *SchemaHandler) CreateLayout(w http.ResponseWriter, r *http.Request) {
	var req CreateLayoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if req.Name == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "name is required")
		return
	}

	layout, err := h.svc.CreateLayout(r.Context(), req.Name, req.TableOccurrenceID, req.Definition)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(layout)
}

func (h *SchemaHandler) GetLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	layout, err := h.svc.GetLayout(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Layout Not Found", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(layout)
}

type UpdateLayoutRequest struct {
	Name       string          `json:"name"`
	Definition json.RawMessage `json:"definition"`
}

func (h *SchemaHandler) UpdateLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req UpdateLayoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	layout, err := h.svc.UpdateLayout(r.Context(), id, req.Name, req.Definition)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(layout)
}

func (h *SchemaHandler) DeleteLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.DeleteLayout(r.Context(), id); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
