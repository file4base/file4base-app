package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// Records of a table occurrence (#107).
//
// A layout is on an occurrence, and an occurrence of another solution's
// table has its records in that solution. These routes take the occurrence,
// which this solution's catalog resolves, and read its records where they
// are: those of a table of this solution as /api/v1/data/{table} does, those
// of another solution as the account the caller's session group holds there,
// with that account's privileges and never this one's. A relationship
// followed from such a record is this solution's: the relationship graph is
// the one of the solution the layout belongs to.
//
// Writing to another solution, and finding through a relationship into one,
// are refused with 424 until they exist.

type tableKey struct{}

type originKey struct{}

// requestTable is the table a data route acts on: the {table} of
// /api/v1/data, or the table of the occurrence of /api/v1/occurrences.
func requestTable(r *http.Request) string {
	if table, ok := r.Context().Value(tableKey{}).(string); ok {
		return table
	}
	return chi.URLParam(r, "table")
}

// externalOrigin is the external occurrence a data route reads, if any.
func externalOrigin(r *http.Request) *data.ExternalTarget {
	origin, _ := r.Context().Value(originKey{}).(data.Origin)
	return origin.External
}

// RegisterOccurrenceRoutes registers the record routes of table occurrences.
func (h *DataHandler) RegisterOccurrenceRoutes(r chi.Router) {
	r.Route("/api/v1/occurrences/{occurrence}/records", func(r chi.Router) {
		r.Use(RequireSession)
		read, write := r.With(h.inOccurrence(false)), r.With(h.inOccurrence(true))
		read.Get("/", h.ListRows)
		write.Post("/", h.InsertRow)
		read.Post("/find", h.FindRows)
		read.Post("/summary", h.SummarizeRows)
		r.Post("/related", h.ListOccurrenceRelatedRowsBatch)
		read.Get("/{id}", h.GetRow)
		write.Put("/{id}", h.UpdateRow)
		write.Delete("/{id}", h.DeleteRow)
		r.Get("/{id}/related", h.ListOccurrenceRelatedRows)
		write.Post("/{id}/related", h.CreateRelatedRow)
	})
	r.With(RequireSession).Get("/api/v1/occurrences/{occurrence}/table", h.OccurrenceTable)
}

// writeOccurrenceError answers an occurrence that could not be resolved.
func writeOccurrenceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, data.ErrOccurrenceNotFound) {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Table Occurrence Not Found", err.Error())
		return
	}
	writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
}

// openOccurrence resolves the request's occurrence. For an occurrence of
// another solution's table it also opens that solution as the session
// group's account there, answering 424 when it cannot be read, and returns
// that session and driver.
func (h *DataHandler) openOccurrence(w http.ResponseWriter, r *http.Request) (data.Origin, *peerEnd, bool) {
	svc := callerDataService(r)
	origin, err := svc.OccurrenceOrigin(r.Context(), chi.URLParam(r, "occurrence"))
	if err != nil {
		writeOccurrenceError(w, r, err)
		return origin, nil, false
	}
	if origin.External == nil {
		return origin, nil, true
	}
	if _, err := svc.Open(r.Context(), *origin.External); err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", err)
		return origin, nil, false
	}
	end, err := peersOf(r).end(r.Context(), origin.External.Database)
	if err != nil {
		writeDataError(w, r, http.StatusInternalServerError, "Internal Server Error", origin.External.Wrap(err))
		return origin, nil, false
	}
	return origin, end, true
}

// inOccurrence runs a /api/v1/data handler on the table of the request's
// occurrence. The records of another solution's table are read with that
// solution's session and driver in place of this one's, so the handler and
// its access checks apply unchanged, as that account; writing them is
// refused.
func (h *DataHandler) inOccurrence(write bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if write {
				origin, err := callerDataService(r).OccurrenceOrigin(r.Context(), chi.URLParam(r, "occurrence"))
				if err != nil {
					writeOccurrenceError(w, r, err)
					return
				}
				if origin.External != nil {
					writeExternalUnavailable(w, r, origin.External.Unsupported("writing records of another solution"))
					return
				}
				noteChangedTable(r.Context(), origin.Table)
				ctx := context.WithValue(r.Context(), tableKey{}, origin.Table)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			origin, end, ok := h.openOccurrence(w, r)
			if !ok {
				return
			}
			ctx := context.WithValue(r.Context(), originKey{}, origin)
			ctx = context.WithValue(ctx, tableKey{}, origin.Table)
			if end != nil {
				ctx = auth.WithSession(ctx, end.session)
				ctx = dbal.WithDriver(ctx, end.driver)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// refuseRelatedCriteria answers 424 for a find on a field of a related record
// from a record of another solution: the relationship is this solution's, and
// a find across solutions is not done yet.
func refuseRelatedCriteria(w http.ResponseWriter, r *http.Request, requests []data.FindRequest) bool {
	external := externalOrigin(r)
	if external == nil {
		return false
	}
	for _, req := range requests {
		for _, crit := range req.Criteria {
			if strings.TrimSpace(crit.RelationshipID) != "" {
				writeExternalUnavailable(w, r, external.Unsupported("a find on a field of a related record"))
				return true
			}
		}
	}
	return false
}

// relatedOrigin resolves the occurrence a related read starts from, checking
// the caller may read a table of this solution; another solution's table is
// checked by the data service, as the group's account there.
func (h *DataHandler) relatedOrigin(w http.ResponseWriter, r *http.Request) (*data.Service, data.Origin, bool) {
	svc := callerDataService(r)
	origin, err := svc.OccurrenceOrigin(r.Context(), chi.URLParam(r, "occurrence"))
	if err != nil {
		writeOccurrenceError(w, r, err)
		return nil, origin, false
	}
	if origin.External == nil {
		svc, ok := h.authorize(w, r, origin.Table, false)
		return svc, origin, ok
	}
	return svc, origin, true
}

// ListOccurrenceRelatedRows returns the records related to a record of the
// occurrence, through a relationship of this solution's graph.
//
//	GET /api/v1/occurrences/{occurrence}/records/{id}/related?relationship=<id>&occurrence=<id>
func (h *DataHandler) ListOccurrenceRelatedRows(w http.ResponseWriter, r *http.Request) {
	if svc, from, ok := h.relatedOrigin(w, r); ok {
		h.listRelated(w, r, svc, from)
	}
}

// ListOccurrenceRelatedRowsBatch is ListRelatedRowsBatch for records of the
// occurrence.
//
//	POST /api/v1/occurrences/{occurrence}/records/related
func (h *DataHandler) ListOccurrenceRelatedRowsBatch(w http.ResponseWriter, r *http.Request) {
	if svc, from, ok := h.relatedOrigin(w, r); ok {
		h.listRelatedBatch(w, r, svc, from)
	}
}

// OccurrenceTable describes the table of an occurrence: its fields, read from
// the catalog of the solution the table is in. A client lays out and
// formats the fields of another solution's table with it, since its own
// catalog does not have them.
//
//	GET /api/v1/occurrences/{occurrence}/table
func (h *DataHandler) OccurrenceTable(w http.ResponseWriter, r *http.Request) {
	origin, end, ok := h.openOccurrence(w, r)
	if !ok {
		return
	}
	driver, _ := dbal.DriverFromContext(r.Context())
	if end != nil {
		driver = end.driver
	} else if _, ok := h.authorize(w, r, origin.Table, false); !ok {
		return
	}
	tables, err := schema.NewService(driver).ListTables(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	for _, table := range tables {
		if table.Name == origin.Table {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(table)
			return
		}
	}
	err = data.ErrTableNotFound
	if origin.External != nil {
		err = origin.External.Wrap(err)
	}
	writeDataError(w, r, http.StatusNotFound, "Table Not Found", err)
}
