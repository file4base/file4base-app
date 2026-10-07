package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// SchemaHandler serves the schema catalog (tables, occurrences, relationships,
// layouts and scripts) of the database the caller is signed in to.
//
// Reading the catalog is open to every signed-in user. Changing it requires
// the owner or admin role, with one exception: a regular user may save a
// layout on which they hold read_write access.
type SchemaHandler struct{}

func NewSchemaHandler() *SchemaHandler {
	return &SchemaHandler{}
}

// rootPath returns the collection path of a route group: the prefix itself
// inside /api/v1/schemas ("/layouts"), or "/" for a top-level alias.
func rootPath(prefix string) string {
	if prefix == "" {
		return "/"
	}
	return prefix
}

func (h *SchemaHandler) RegisterRoutes(r chi.Router) {
	tables := func(r chi.Router) {
		r.Get("/tables", h.ListTables)
		r.With(RequireAdmin).Post("/tables", h.CreateTable)
		r.With(RequireAdmin).Delete("/tables/{id}", h.DeleteTable)
		r.With(RequireAdmin).Put("/tables/{id}/rename", h.RenameTable)
		r.With(RequireAdmin).Post("/tables/{id}/duplicate", h.DuplicateTable)
		r.With(RequireAdmin).Post("/tables/{id}/truncate", h.TruncateTable)
		r.With(RequireAdmin).Post("/tables/{id}/columns", h.AddColumn)
		r.With(RequireAdmin).Put("/tables/{id}/columns/{columnId}", h.UpdateColumn)
		r.With(RequireAdmin).Delete("/tables/{id}/columns/{columnId}", h.DeleteColumn)
	}
	occurrences := func(prefix string) func(r chi.Router) {
		root := rootPath(prefix)
		return func(r chi.Router) {
			r.Get(root, h.ListOccurrences)
			r.With(RequireAdmin).Post(root, h.CreateOccurrence)
			r.With(RequireAdmin).Put(prefix+"/{id}", h.UpdateOccurrence)
			r.With(RequireAdmin).Delete(prefix+"/{id}", h.DeleteOccurrence)
		}
	}
	relationships := func(prefix string) func(r chi.Router) {
		root := rootPath(prefix)
		return func(r chi.Router) {
			r.Get(root, h.ListRelationships)
			r.With(RequireAdmin).Post(root, h.CreateRelationship)
			r.With(RequireAdmin).Put(prefix+"/{id}", h.UpdateRelationship)
			r.With(RequireAdmin).Delete(prefix+"/{id}", h.DeleteRelationship)
		}
	}
	layouts := func(prefix string) func(r chi.Router) {
		root := rootPath(prefix)
		return func(r chi.Router) {
			r.Get(root, h.ListLayouts)
			r.With(RequireAdmin).Post(root, h.CreateLayout)
			r.Get(prefix+"/{id}", h.GetLayout)
			r.Put(prefix+"/{id}", h.UpdateLayout) // per-layout check inside the handler
			r.With(RequireAdmin).Delete(prefix+"/{id}", h.DeleteLayout)
		}
	}
	scripts := func(prefix string) func(r chi.Router) {
		root := rootPath(prefix)
		return func(r chi.Router) {
			r.Get(root, h.ListScripts)
			r.With(RequireAdmin).Post(root, h.CreateScript)
			r.Get(prefix+"/{id}", h.GetScript)
			r.With(RequireAdmin).Put(prefix+"/{id}", h.UpdateScript)
			r.With(RequireAdmin).Delete(prefix+"/{id}", h.DeleteScript)
			r.With(RequireAdmin).Post(prefix+"/{id}/duplicate", h.DuplicateScript)
		}
	}

	r.Route("/api/v1/schemas", func(r chi.Router) {
		r.Use(RequireSession)
		tables(r)
		occurrences("/occurrences")(r)
		relationships("/relationships")(r)
		layouts("/layouts")(r)
		scripts("/scripts")(r)
	})

	// Top-level aliases kept for backwards compatibility
	r.Route("/api/v1/occurrences", func(r chi.Router) {
		r.Use(RequireSession)
		occurrences("")(r)
	})
	r.Route("/api/v1/relationships", func(r chi.Router) {
		r.Use(RequireSession)
		relationships("")(r)
	})
	r.Route("/api/v1/layouts", func(r chi.Router) {
		r.Use(RequireSession)
		layouts("")(r)
	})
	r.Route("/api/v1/scripts", func(r chi.Router) {
		r.Use(RequireSession)
		scripts("")(r)
	})
}

func (h *SchemaHandler) ListTables(w http.ResponseWriter, r *http.Request) {
	tables, err := schemaService(r).ListTables(r.Context())
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

	tbl, err := schemaService(r).CreateTable(r.Context(), req.DisplayName, req.CustomName)
	if err != nil {
		writeSchemaError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(tbl)
}

func (h *SchemaHandler) DeleteTable(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	if tableID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id is required")
		return
	}
	if err := schemaService(r).DeleteTable(r.Context(), tableID); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type RenameTableRequest struct {
	DisplayName string `json:"display_name"`
}

func (h *SchemaHandler) RenameTable(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	if tableID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id is required")
		return
	}
	var req RenameTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if req.DisplayName == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "display_name is required")
		return
	}
	tbl, err := schemaService(r).RenameTable(r.Context(), tableID, req.DisplayName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tbl)
}

func (h *SchemaHandler) DuplicateTable(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	if tableID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id is required")
		return
	}
	tbl, err := schemaService(r).DuplicateTable(r.Context(), tableID)
	if err != nil {
		writeSchemaError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(tbl)
}

func (h *SchemaHandler) TruncateTable(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	if tableID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id is required")
		return
	}
	if err := schemaService(r).TruncateTable(r.Context(), tableID); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

	col, err := schemaService(r).AddColumn(r.Context(), tableID, schema.ColumnMetadata{
		Name:               req.Name,
		DisplayName:        req.DisplayName,
		FieldType:          req.FieldType,
		IsNullable:         req.IsNullable,
		DefaultValue:       req.DefaultValue,
		CalculationFormula: req.CalculationFormula,
		ValidationRules:    req.ValidationRules,
	})
	if err != nil {
		writeSchemaError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(col)
}

// writeSchemaError answers 422 for invalid names or field options, 409 for a
// table name already in use and 400 otherwise.
func writeSchemaError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, schema.ErrInvalidFieldOptions):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Field Options", err.Error())
		return
	case errors.Is(err, dbal.ErrInvalidIdentifier):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Name", err.Error())
		return
	case errors.Is(err, schema.ErrTableExists):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Table Already Exists", err.Error())
		return
	}
	telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
}

type UpdateColumnRequest struct {
	DisplayName        string  `json:"display_name"`
	DefaultValue       *string `json:"default_value"`
	CalculationFormula *string `json:"calculation_formula"`
	ValidationRules    *string `json:"validation_rules"`
}

func (h *SchemaHandler) UpdateColumn(w http.ResponseWriter, r *http.Request) {
	tableID := chi.URLParam(r, "id")
	columnID := chi.URLParam(r, "columnId")
	if tableID == "" || columnID == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "table id and column id are required")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Request", "Failed to read request body")
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	var req UpdateColumnRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	opts := schema.UpdateColumnOptions{
		DisplayName: req.DisplayName,
	}
	if _, ok := raw["default_value"]; ok {
		opts.UpdateDefaultValue = true
		opts.DefaultValue = req.DefaultValue
	}
	if _, ok := raw["calculation_formula"]; ok {
		opts.UpdateCalculation = true
		opts.CalculationFormula = req.CalculationFormula
	}
	if _, ok := raw["validation_rules"]; ok {
		opts.UpdateValidation = true
		opts.ValidationRules = req.ValidationRules
	}

	col, err := schemaService(r).UpdateColumn(r.Context(), tableID, columnID, opts)
	if err != nil {
		writeSchemaError(w, r, err)
		return
	}

	// A new formula changes what every existing record should show, so they
	// are recomputed rather than keeping the old answer until each is edited
	// (#30). A record the new formula cannot be computed for is reported.
	if opts.UpdateCalculation && col.FieldType == dbal.FieldTypeCalculation {
		driver, _ := dbal.DriverFromContext(r.Context())
		tableName, nameErr := schemaService(r).TableNameByID(r.Context(), tableID)
		if nameErr == nil && driver != nil {
			if _, recalcErr := data.NewService(driver).RecalculateTable(r.Context(), tableName); recalcErr != nil {
				writeDataError(w, r, http.StatusUnprocessableEntity, "Calculation Error", recalcErr)
				return
			}
		}
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

	if err := schemaService(r).DeleteColumn(r.Context(), tableID, columnID); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) ListOccurrences(w http.ResponseWriter, r *http.Request) {
	occurrences, err := schemaService(r).ListTableOccurrences(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(occurrences)
}

func (h *SchemaHandler) CreateOccurrence(w http.ResponseWriter, r *http.Request) {
	var input schema.CreateOccurrenceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if input.BaseTableID == "" || input.Name == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "base_table_id and name are required")
		return
	}

	occ, err := schemaService(r).CreateTableOccurrence(r.Context(), input)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(occ)
}

func (h *SchemaHandler) UpdateOccurrence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "occurrence id is required")
		return
	}

	var input schema.UpdateOccurrenceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	occ, err := schemaService(r).UpdateTableOccurrence(r.Context(), id, input)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(occ)
}

func (h *SchemaHandler) DeleteOccurrence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "occurrence id is required")
		return
	}

	if err := schemaService(r).DeleteTableOccurrence(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) ListRelationships(w http.ResponseWriter, r *http.Request) {
	relationships, err := schemaService(r).ListRelationships(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(relationships)
}

func (h *SchemaHandler) CreateRelationship(w http.ResponseWriter, r *http.Request) {
	var input schema.CreateRelationshipInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if input.LeftOccurrenceID == "" || input.RightOccurrenceID == "" || input.LeftColumnID == "" || input.RightColumnID == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "left and right occurrences and columns are required")
		return
	}

	rel, err := schemaService(r).CreateRelationship(r.Context(), input)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(rel)
}

func (h *SchemaHandler) UpdateRelationship(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "relationship id is required")
		return
	}

	var input schema.UpdateRelationshipInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	rel, err := schemaService(r).UpdateRelationship(r.Context(), id, input)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rel)
}

func (h *SchemaHandler) DeleteRelationship(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Bad Request", "relationship id is required")
		return
	}

	if err := schemaService(r).DeleteRelationship(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) ListLayouts(w http.ResponseWriter, r *http.Request) {
	svc := schemaService(r)
	layouts, err := svc.ListLayouts(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	// Regular users do not get layouts they were explicitly denied
	if sess := currentSession(r); !sess.IsAdmin() {
		perms, err := svc.GetUserPermissions(r.Context(), sess.UserID)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return
		}
		denied := make(map[string]struct{})
		for _, p := range perms {
			if p.AccessLevel == schema.AccessNone {
				denied[p.LayoutID] = struct{}{}
			}
		}
		visible := layouts[:0]
		for _, l := range layouts {
			if _, hidden := denied[l.ID]; !hidden {
				visible = append(visible, l)
			}
		}
		layouts = visible
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

	layout, err := schemaService(r).CreateLayout(r.Context(), req.Name, req.TableOccurrenceID, req.Definition)
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
	svc := schemaService(r)
	if sess := currentSession(r); !sess.IsAdmin() {
		level, err := svc.LayoutAccess(r.Context(), sess.UserID, id)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return
		}
		if level == schema.AccessNone {
			writeForbidden(w, r, "you do not have access to this layout")
			return
		}
	}
	layout, err := svc.GetLayout(r.Context(), id)
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

	svc := schemaService(r)
	if sess := currentSession(r); !sess.IsAdmin() {
		level, err := svc.LayoutAccess(r.Context(), sess.UserID, id)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return
		}
		if level != schema.AccessReadWrite {
			writeForbidden(w, r, "you do not have write access to this layout")
			return
		}
	}

	layout, err := svc.UpdateLayout(r.Context(), id, req.Name, req.Definition)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Schema Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(layout)
}

func (h *SchemaHandler) DeleteLayout(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := schemaService(r).DeleteLayout(r.Context(), id); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) ListScripts(w http.ResponseWriter, r *http.Request) {
	scripts, err := schemaService(r).ListScripts(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(scripts)
}

func (h *SchemaHandler) GetScript(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	script, err := schemaService(r).GetScript(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Script Not Found", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(script)
}

type CreateScriptRequest struct {
	Name         string                      `json:"name"`
	ContextTable string                      `json:"context_table,omitempty"`
	FolderID     *string                     `json:"folder_id,omitempty"`
	IsActive     *bool                       `json:"is_active,omitempty"`
	Steps        []schema.ScriptStepMetadata `json:"steps,omitempty"`
}

func (h *SchemaHandler) CreateScript(w http.ResponseWriter, r *http.Request) {
	var req CreateScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if req.Name == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "name is required")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	script, err := schemaService(r).CreateScript(r.Context(), req.Name, req.ContextTable, req.FolderID, active, req.Steps)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Script Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(script)
}

type UpdateScriptRequest struct {
	Name         string                      `json:"name"`
	ContextTable string                      `json:"context_table,omitempty"`
	FolderID     *string                     `json:"folder_id,omitempty"`
	IsActive     *bool                       `json:"is_active,omitempty"`
	Steps        []schema.ScriptStepMetadata `json:"steps,omitempty"`
}

func (h *SchemaHandler) UpdateScript(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req UpdateScriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if req.Name == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "name is required")
		return
	}
	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}
	script, err := schemaService(r).UpdateScript(r.Context(), id, req.Name, req.ContextTable, req.FolderID, active, req.Steps)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Script Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(script)
}

func (h *SchemaHandler) DeleteScript(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := schemaService(r).DeleteScript(r.Context(), id); err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SchemaHandler) DuplicateScript(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	script, err := schemaService(r).DuplicateScript(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Duplicate Script Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(script)
}
