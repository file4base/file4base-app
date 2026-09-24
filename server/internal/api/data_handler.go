package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

type DataHandler struct {
	svc *data.Service
}

func NewDataHandler(svc *data.Service) *DataHandler {
	return &DataHandler{svc: svc}
}

func (h *DataHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/data/{table}", func(r chi.Router) {
		r.Get("/", h.ListRows)
		r.Post("/", h.InsertRow)
		r.Post("/find", h.FindRows)
		r.Get("/{id}", h.GetRow)
		r.Put("/{id}", h.UpdateRow)
		r.Delete("/{id}", h.DeleteRow)
	})
}

func (h *DataHandler) ListRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	sortBy := r.URL.Query().Get("sort_by")
	sortAsc := r.URL.Query().Get("sort_asc") != "false"

	rows, err := h.svc.ListRows(r.Context(), table, data.QueryOptions{
		Limit:   limit,
		Offset:  offset,
		SortBy:  sortBy,
		SortAsc: sortAsc,
	})
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

func (h *DataHandler) InsertRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	var record map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	inserted, err := h.svc.InsertRow(r.Context(), table, record)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Data Insert Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(inserted)
}

func (h *DataHandler) GetRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	row, err := h.svc.GetRow(r.Context(), table, id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Record Not Found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(row)
}

func (h *DataHandler) UpdateRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	updated, err := h.svc.UpdateRow(r.Context(), table, id, updates)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Data Update Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

func (h *DataHandler) DeleteRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteRow(r.Context(), table, id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Data Delete Error", err.Error())
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
	var body FindRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	results, err := h.svc.ExecuteFind(r.Context(), table, body.Requests, body.Options)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Find Query Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}
