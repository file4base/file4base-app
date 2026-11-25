package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// Auto-enter field options on the data routes (docs/specs/auto_enter_options.md).

// EntryHeader marks a write as a script step's rather than data entry: a
// script may set a field whose options prohibit modification during data
// entry. It decides nothing about privileges.
const EntryHeader = "X-File4Base-Entry"

// ValidationHeader set to "override" sends again a write whose values break
// validation rules that allow the person to override them, as FileMaker's
// "Allow user to override during data entry": the person was told, and
// chose to keep the values. The rules that do not allow it still apply.
const ValidationHeader = "X-File4Base-Validation"

// entryContext is the request's context, marked as a script's when the
// request says it is one, and as overriding validation when it says so.
func entryContext(r *http.Request) context.Context {
	ctx := r.Context()
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(EntryHeader)), "script") {
		ctx = data.AsScript(ctx)
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(ValidationHeader)), "override") {
		ctx = data.WithValidationOverride(ctx)
	}
	return ctx
}

// DuplicateRow creates a copy of a record, as FileMaker's Duplicate Record.
func (h *DataHandler) DuplicateRow(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorizeAction(w, r, table, true, data.ActionCreate)
	if !ok {
		return
	}
	inserted, err := svc.DuplicateRow(r.Context(), table, chi.URLParam(r, "id"))
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Data Insert Error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(inserted)
}

// NextSerialValue is the next value of a serial number field.
type NextSerialValue struct {
	Field     string `json:"field"`
	NextValue string `json:"next_value"`
}

// GetNextSerialValue answers the value the next record gets in a serial
// number field: FileMaker's GetNextSerialValue.
func (h *DataHandler) GetNextSerialValue(w http.ResponseWriter, r *http.Request) {
	table, field := requestTable(r), chi.URLParam(r, "field")
	svc, ok := h.authorize(w, r, table, false)
	if !ok {
		return
	}
	next, err := svc.NextSerialValue(r.Context(), table, field)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Serial Number Error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(NextSerialValue{Field: field, NextValue: next})
}

// SetNextSerialValue sets the value the next record gets in a serial number
// field: FileMaker's Set Next Serial Value. It needs the edit privilege.
func (h *DataHandler) SetNextSerialValue(w http.ResponseWriter, r *http.Request) {
	table, field := requestTable(r), chi.URLParam(r, "field")
	svc, ok := h.authorizeAction(w, r, table, true, data.ActionEdit)
	if !ok {
		return
	}
	// The value is a number or a text, such as "INV0001".
	var body struct {
		NextValue interface{} `json:"next_value"`
	}
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	err := dec.Decode(&body)
	text, isText := body.NextValue.(string)
	if number, isNumber := body.NextValue.(json.Number); isNumber {
		text, isText = number.String(), true
	}
	if err != nil || !isText || strings.TrimSpace(text) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "the body must be {\"next_value\": a number or a text}")
		return
	}
	next, err := svc.SetNextSerialValue(r.Context(), table, field, text)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Serial Number Error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(NextSerialValue{Field: field, NextValue: next})
}

// maxRelookupRecords bounds one Relookup Field Contents request.
const maxRelookupRecords = 10000

// RelookupBody is a Relookup Field Contents request: the match field the
// lookups start from, and the records of the found set.
type RelookupBody struct {
	Field     string   `json:"field"`
	RecordIDs []string `json:"record_ids"`
}

// RelookupResult is what a relookup changed: how many records, and each of
// them as it is now, so a client updates its found set in place.
type RelookupResult struct {
	Updated int                      `json:"updated"`
	Records []map[string]interface{} `json:"records"`
}

// RelookupRows copies again the looked-up values of the found set:
// FileMaker's Relookup Field Contents. It needs the edit privilege.
func (h *DataHandler) RelookupRows(w http.ResponseWriter, r *http.Request) {
	table := requestTable(r)
	svc, ok := h.authorizeAction(w, r, table, true, data.ActionEdit)
	if !ok {
		return
	}
	var body RelookupBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Field) == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"the body must name the match field and the records: {\"field\": …, \"record_ids\": […]}")
		return
	}
	if len(body.RecordIDs) > maxRelookupRecords {
		telemetry.WriteProblem(w, r, http.StatusRequestEntityTooLarge, "Too Many Records",
			"a relookup takes at most 10000 records at a time")
		return
	}
	changed, err := svc.Relookup(r.Context(), table, body.Field, body.RecordIDs)
	if err != nil {
		writeDataError(w, r, http.StatusBadRequest, "Relookup Error", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RelookupResult{Updated: len(changed), Records: changed})
}
