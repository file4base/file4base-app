package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/file4base/file4base-app/server/internal/validation"
	"github.com/go-chi/chi/v5"
)

// DataHandler serves record CRUD and Find Mode queries on the user tables of
// the database the caller is signed in to.
//
// Only tables registered in the system catalog are reachable; sys_* tables are
// not. Owners and admins have full access. For regular users the access level
// is derived from their layout permissions (see schema.Service.TableAccess).
type DataHandler struct{}

func NewDataHandler() *DataHandler {
	return &DataHandler{}
}

func (h *DataHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/data/{table}", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/", h.ListRows)
		r.Post("/", h.InsertRow)
		r.Post("/find", h.FindRows)
		r.Get("/{id}", h.GetRow)
		r.Put("/{id}", h.UpdateRow)
		r.Delete("/{id}", h.DeleteRow)
	})
}

// authorize resolves the data service for the request and enforces the
// caller's access level on the table. It writes the error response itself
// and returns ok=false when the request must not proceed.
func (h *DataHandler) authorize(w http.ResponseWriter, r *http.Request, table string, write bool) (*data.Service, bool) {
	driver, _ := dbal.DriverFromContext(r.Context())
	sess := currentSession(r)

	if !sess.IsAdmin() {
		level, err := schema.NewService(driver).TableAccess(r.Context(), sess.UserID, table)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return nil, false
		}
		if level == schema.AccessNone {
			writeForbidden(w, r, "you do not have access to this table")
			return nil, false
		}
		if write && level != schema.AccessReadWrite {
			writeForbidden(w, r, "you have read-only access to this table")
			return nil, false
		}
	}

	return data.NewService(driver), true
}

// writeDataError maps data-layer errors to RFC 9457 responses.
func writeDataError(w http.ResponseWriter, r *http.Request, fallbackStatus int, title string, err error) {
	switch {
	case errors.Is(err, data.ErrTableNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Table Not Found", err.Error())
	case errors.Is(err, data.ErrUnknownField):
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Unknown Field", err.Error())
	case errors.Is(err, validation.ErrValidation):
		var ve *validation.Error
		errors.As(err, &ve)
		telemetry.WriteValidationProblem(w, r, ve.Message, []telemetry.InvalidParam{{Name: ve.Field, Reason: ve.Rule}})
	case errors.Is(err, data.ErrInvalidValue):
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Value", err.Error())
	default:
		telemetry.WriteProblem(w, r, fallbackStatus, title, err.Error())
	}
}

func (h *DataHandler) ListRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	sortBy := r.URL.Query().Get("sort_by")
	sortAsc := r.URL.Query().Get("sort_asc") != "false"

	rows, err := svc.ListRows(r.Context(), table, data.QueryOptions{
		Limit:   limit,
		Offset:  offset,
		SortBy:  sortBy,
		SortAsc: sortAsc,
	})
	if err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (h *DataHandler) InsertRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	svc, ok := h.authorize(w, r, table, true)
	if !ok {
		return
	}

	record, err := decodeRecord(r)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	inserted, err := svc.InsertRow(r.Context(), table, record)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Insert Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(inserted)
}

func (h *DataHandler) GetRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	row, err := svc.GetRow(r.Context(), table, id)
	if err != nil {
		writeDataError(w, r, http.StatusNotFound, "Record Not Found", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(row)
}

func (h *DataHandler) UpdateRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")
	svc, ok := h.authorize(w, r, table, true)
	if !ok {
		return
	}

	updates, err := decodeRecord(r)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	updated, err := svc.UpdateRow(r.Context(), table, id, updates)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Update Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *DataHandler) DeleteRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")
	svc, ok := h.authorize(w, r, table, true)
	if !ok {
		return
	}

	if err := svc.DeleteRow(r.Context(), table, id); err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Delete Error", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type FindRequestBody struct {
	Requests []data.FindRequest `json:"requests"`
	Options  data.QueryOptions  `json:"options"`
}

func (h *DataHandler) FindRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body FindRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	results, err := svc.ExecuteFind(r.Context(), table, body.Requests, body.Options)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Find Query Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

// decodeRecord decodes a record body keeping every JSON number exactly as
// written: numbers are passed to the database as their decimal text, never
// through float64, so NUMBER fields do not lose precision (issue #18).
func decodeRecord(r *http.Request) (map[string]interface{}, error) {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var record map[string]interface{}
	if err := dec.Decode(&record); err != nil {
		return nil, err
	}
	for k, v := range record {
		if n, ok := v.(json.Number); ok {
			record[k] = n.String()
		}
	}
	return record, nil
}
