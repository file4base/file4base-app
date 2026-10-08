package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dataio"
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
		r.Post("/summary", h.SummarizeRows)
		r.Post("/import/preview", h.PreviewImport)
		r.Post("/import", h.ImportRows)
		r.Post("/export", h.ExportRows)
		r.Get("/{id}", h.GetRow)
		r.Get("/{id}/related", h.ListRelatedRows)
		r.Post("/{id}/related", h.CreateRelatedRow)
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
	case errors.Is(err, dataio.ErrSourceTooLarge):
		telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Source File Too Large", err.Error())
	case errors.Is(err, dataio.ErrInvalidSource):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Source File", err.Error())
	case errors.Is(err, data.ErrImport):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Import Failed", err.Error())
	case errors.Is(err, data.ErrInvalidSummary):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Summary Field", err.Error())
	case errors.Is(err, data.ErrRelationshipNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Relationship Not Found", err.Error())
	case errors.Is(err, data.ErrCreationNotAllowed):
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Creation Not Allowed", err.Error())
	case errors.Is(err, data.ErrNoMatchValue):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "No Match Value", err.Error())
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

// parseSortOrder reads the `sort` query parameter, which may be repeated or
// comma-separated and orders by each field in turn. A leading "-" sorts that
// field descending:
//
//	?sort=company&sort=-fee_paid
//	?sort=company,-fee_paid
//
// It supersedes sort_by/sort_asc, which stay for callers that only ever sort by
// one field.
func parseSortOrder(values []string) []data.SortField {
	var order []data.SortField
	for _, value := range values {
		order = append(order, data.ParseSortSpec(value)...)
	}
	return order
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
		Sort:    parseSortOrder(r.URL.Query()["sort"]),
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

// ImportBody is what the import endpoints take: the file itself, base64 so it
// travels in JSON beside the settings, and how to read it.
type ImportBody struct {
	FileName string `json:"file_name"`

	// The file, base64 encoded (standard alphabet, padding optional).
	Content string `json:"content"`

	Format        string `json:"format"`
	HasHeader     bool   `json:"has_header"`
	Delimiter     string `json:"delimiter"`
	Sheet         string `json:"sheet"`
	RecordElement string `json:"record_element"`

	// Only used by the import itself, not by the preview.
	Options data.ImportOptions `json:"options"`
}

// parseSource decodes and reads the file an import body carries.
func parseSource(body ImportBody) (*dataio.Table, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body.Content))
	if err != nil {
		// A client that left the padding off still gets read.
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(body.Content))
		if err != nil {
			return nil, fmt.Errorf("%w: the file could not be decoded: %v", dataio.ErrInvalidSource, err)
		}
	}

	format := dataio.Format(strings.ToLower(strings.TrimSpace(body.Format)))
	if format == "" {
		format = dataio.FormatFromName(body.FileName)
	}

	return dataio.Parse(raw, dataio.Options{
		Format:        format,
		HasHeader:     body.HasHeader,
		Delimiter:     body.Delimiter,
		Sheet:         body.Sheet,
		RecordElement: body.RecordElement,
	})
}

// PreviewImport reads a file and reports what is in it, without writing
// anything (#38).
//
//	POST /api/v1/data/{table}/import/preview
//
// It answers the columns the file has, the sheets a workbook holds, and the
// first rows, so the import dialog can show what is about to be brought in
// and match the columns to the fields.
func (h *DataHandler) PreviewImport(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	// Previewing writes nothing, but it reads a file into a table the caller
	// must be allowed to write to, so it asks for write access.
	if _, ok := h.authorize(w, r, table, true); !ok {
		return
	}

	var body ImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	source, err := parseSource(body)
	if err != nil {
		writeDataError(w, r, http.StatusUnprocessableEntity, "Invalid Source File", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"columns":   source.Columns,
		"sheets":    source.Sheets,
		"rows":      source.Sample(20),
		"row_count": len(source.Rows),
		"truncated": source.Truncated,
	})
}

// ImportRows brings the records of a file into a table (#38).
//
//	POST /api/v1/data/{table}/import
//
// It is one transaction: the first row that cannot be read or stored takes the
// whole file with it, and the answer says which row it was.
func (h *DataHandler) ImportRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	svc, ok := h.authorize(w, r, table, true)
	if !ok {
		return
	}

	var body ImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	source, err := parseSource(body)
	if err != nil {
		writeDataError(w, r, http.StatusUnprocessableEntity, "Invalid Source File", err)
		return
	}

	report, err := svc.ImportRecords(r.Context(), table, source, body.Options)
	if err != nil {
		writeDataError(w, r, http.StatusUnprocessableEntity, "Import Failed", err)
		return
	}

	report.Rows = len(source.Rows)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"rows":      report.Rows,
		"added":     report.Added,
		"updated":   report.Updated,
		"skipped":   report.Skipped,
		"fields":    report.Fields,
		"truncated": source.Truncated,
	})
}

// ExportBody is what the export endpoint takes.
type ExportBody struct {
	Format string `json:"format"`

	// The sheet name of a workbook, and the stem of the suggested file name.
	Name string `json:"name"`

	data.ExportOptions
}

// ExportRows writes a found set out as CSV, tab-separated text or a workbook
// (#38).
//
//	POST /api/v1/data/{table}/export
//
// The body takes the same find requests a find does, so what is exported is
// what was found.
func (h *DataHandler) ExportRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body ExportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	format := dataio.Format(strings.ToLower(strings.TrimSpace(body.Format)))
	if format == "" {
		format = dataio.FormatCSV
	}
	if format == dataio.FormatXML {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Format Not Supported",
			"records are exported as csv, tsv or xlsx; XML is read but not written")
		return
	}

	source, err := svc.ExportRecords(r.Context(), table, body.ExportOptions)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Export Failed", err)
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = table
	}

	var out []byte
	switch format {
	case dataio.FormatXLSX:
		out, err = dataio.WriteXLSX(source, name)
	case dataio.FormatTSV:
		out, err = dataio.WriteDelimited(source, '\t', false)
	default:
		// With the byte order mark, so Excel opens it as UTF-8 and accented
		// names survive the round trip.
		out, err = dataio.WriteDelimited(source, ',', true)
	}
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", dataio.ContentType(format))
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", name+dataio.Extension(format)))
	w.Header().Set("X-File4Base-Rows", fmt.Sprint(len(source.Rows)))
	_, _ = w.Write(out)
}

// SummarizeRows works out the figures of a report: what each summary field
// comes to over the found set, and over each group of it (#32).
//
//	POST /api/v1/data/{table}/summary
//
// The found set is given as find requests, exactly as `POST .../find` takes
// them, so a report totals the records the find returned. An empty `requests`
// summarizes every record of the table.
func (h *DataHandler) SummarizeRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	var body data.SummaryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	result, err := svc.Summarize(r.Context(), table, body)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Summary Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// ListRelatedRows returns the records of a related table that match the record
// in hand: what a portal shows, and what a related field on a layout reads its
// value from (#36).
//
//	GET /api/v1/data/{table}/{id}/related?relationship=<id>&occurrence=<id>
//
// `occurrence` names the side whose records are wanted. It may be left out
// unless the relationship joins a table to itself, where there is no other
// side to infer.
func (h *DataHandler) ListRelatedRows(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	relationship := r.URL.Query().Get("relationship")
	if strings.TrimSpace(relationship) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Missing Relationship",
			"the relationship to follow must be given as ?relationship=<id>")
		return
	}
	occurrence := r.URL.Query().Get("occurrence")

	// The caller needs access to the table the records come from as well.
	if !h.authorizeRelated(w, r, svc, table, relationship, occurrence, false) {
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	rows, err := svc.RelatedRows(r.Context(), table, id, relationship, occurrence, data.QueryOptions{
		Limit:  limit,
		Offset: offset,
		Sort:   parseSortOrder(r.URL.Query()["sort"]),
	})
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rows)
}

// CreateRelatedRow creates a record in a related table and fills its match
// field, which is what typing into the last row of a portal does.
//
//	POST /api/v1/data/{table}/{id}/related?relationship=<id>&occurrence=<id>
//
// It is refused with 403 unless the relationship allows creation.
func (h *DataHandler) CreateRelatedRow(w http.ResponseWriter, r *http.Request) {
	table := chi.URLParam(r, "table")
	id := chi.URLParam(r, "id")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}

	relationship := r.URL.Query().Get("relationship")
	if strings.TrimSpace(relationship) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Missing Relationship",
			"the relationship to follow must be given as ?relationship=<id>")
		return
	}
	occurrence := r.URL.Query().Get("occurrence")

	// The record is written to the related table, so that is where write
	// access has to hold.
	if !h.authorizeRelated(w, r, svc, table, relationship, occurrence, true) {
		return
	}

	record, err := decodeRecord(r)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	inserted, err := svc.CreateRelatedRow(r.Context(), table, id, relationship, occurrence, record)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Insert Error", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(inserted)
}

// authorizeRelated enforces the caller's access level on the table at the far
// end of a relationship. It writes the error response itself and returns false
// when the request must not proceed.
func (h *DataHandler) authorizeRelated(
	w http.ResponseWriter, r *http.Request, svc *data.Service,
	table, relationship, occurrence string, write bool,
) bool {
	rel, err := svc.Relationship(r.Context(), relationship)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return false
	}
	_, target, err := rel.Sides(table, occurrence)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Related Records Error", err)
		return false
	}
	_, ok := h.authorize(w, r, target.TableName, write)
	return ok
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
