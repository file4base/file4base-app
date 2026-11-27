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
// Records of another solution are written the same way: created, edited and
// deleted by that solution's data service as that account, which needs that
// account's privilege, so its validation, auto-enter values and stored
// calculations apply, and its own cascades. Finding through a relationship
// into another solution is refused with 424 until it exists.

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
		// Evaluated here, the rows of another solution read there.
		r.Post("/evaluate", h.EvaluateOccurrenceExpression)
		// Auto-enter options apply in the solution the records are in, as
		// that solution's account: serial numbers and stamps are its own.
		write.Post("/relookup", h.RelookupRows)
		read.Get("/serials/{field}", h.GetNextSerialValue)
		write.Put("/serials/{field}", h.SetNextSerialValue)
		write.Post("/{id}/duplicate", h.DuplicateRow)
		read.Get("/{id}", h.GetRow)
		write.Put("/{id}", h.UpdateRow)
		write.Patch("/{id}", h.UpdateRow)
		write.Delete("/{id}", h.DeleteRow)
		r.Get("/{id}/related", h.ListOccurrenceRelatedRows)
		r.Post("/{id}/related", h.CreateOccurrenceRelatedRow)
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
// occurrence. The records of another solution's table are read and written
// with that solution's session and driver in place of this one's, so the
// handler and its access checks apply unchanged, as that account. A write
// names the table it changes, and the solution, for its change event.
func (h *DataHandler) inOccurrence(write bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin, end, ok := h.openOccurrence(w, r)
			if !ok {
				return
			}
			if write {
				database := ""
				if origin.External != nil {
					database = origin.External.Database
				}
				noteChangedTable(r.Context(), database, origin.Table)
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

// CreateOccurrenceRelatedRow is CreateRelatedRow for a record of the
// occurrence, through a relationship of this solution's graph; a related
// table of another solution is written there, as the group's account.
//
//	POST /api/v1/occurrences/{occurrence}/records/{id}/related?relationship=<id>&occurrence=<id>
func (h *DataHandler) CreateOccurrenceRelatedRow(w http.ResponseWriter, r *http.Request) {
	if svc, from, ok := h.relatedOrigin(w, r); ok {
		h.createRelated(w, r, svc, from)
	}
}

// occurrenceTable is a table of an occurrence with what the session may do
// with its records: for another solution's, what the group's account there
// may do, which is what a client enables record commands by (#107).
type occurrenceTable struct {
	*schema.TableMetadata
	Privileges RecordPrivileges `json:"privileges"`
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
	sess := currentSession(r)
	if end != nil {
		driver, sess = end.driver, end.session
	} else if _, ok := h.authorize(w, r, origin.Table, false); !ok {
		return
	}
	table, found, err := schema.NewService(driver).TableByName(r.Context(), origin.Table)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	if found {
		privileges, err := sessionPrivileges(r.Context(), driver, sess, origin.Table)
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(occurrenceTable{TableMetadata: table, Privileges: privileges})
		return
	}
	err = data.ErrTableNotFound
	if origin.External != nil {
		err = origin.External.Wrap(err)
	}
	writeDataError(w, r, http.StatusNotFound, "Table Not Found", err)
}
