package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// FileReferenceHandler serves the file references of the solution the caller
// is signed in to (#107): the named links from this solution's scripts and
// external table occurrences to other solutions.
//
// Binding a reference reads the other solution's catalog, so it is authorized
// by a session of that solution (peer_token), as a transfer's far end is.
type FileReferenceHandler struct {
	dbMgr    *dbal.MultiDatabaseManager
	sessions *auth.Store
}

func NewFileReferenceHandler(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store) *FileReferenceHandler {
	return &FileReferenceHandler{dbMgr: dbMgr, sessions: sessions}
}

func (h *FileReferenceHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/file-references", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/", h.List)
		r.With(RequireAdmin, RequireDesignUnlocked).Post("/", h.Create)
		r.Get("/{id}", h.Get)
		r.Get("/{id}/resolve", h.Resolve)
		r.With(RequireAdmin, RequireDesignUnlocked).Put("/{id}", h.Update)
		r.With(RequireAdmin, RequireDesignUnlocked).Delete("/{id}", h.Delete)
		r.With(RequireAdmin, RequireDesignUnlocked).Post("/{id}/bind", h.Bind)
		// Verifying reads the design (layouts, fields) of both solutions:
		// a design operation, like binding.
		r.With(RequireAdmin).Post("/{id}/verify", h.Verify)
	})
}

func (h *FileReferenceHandler) List(w http.ResponseWriter, r *http.Request) {
	refs, err := schemaService(r).ListFileReferences(r.Context())
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(refs)
}

func (h *FileReferenceHandler) Get(w http.ResponseWriter, r *http.Request) {
	ref, err := resolveFileReference(r, chi.URLParam(r, "id"))
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ref)
}

// FileReferenceResolution says where a script step that names a reference
// goes, and from where.
type FileReferenceResolution struct {
	Name string `json:"name"`

	// Database is the solution the reference is mapped to.
	Database string `json:"database"`

	// CallingDatabase is the solution this session is signed in to: the one
	// whose script is reaching out. A script performed in the referenced
	// solution may need to reach back, and this is the name it reaches back
	// to.
	CallingDatabase string `json:"calling_database"`

	// Local is true when the reference names the calling solution itself.
	Local bool `json:"local"`
}

// Resolve answers where a reference goes. An unmapped reference is a 409: the
// reference exists, and the request is refused because of its state, not
// because of anything wrong with the request.
func (h *FileReferenceHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	ref, err := resolveFileReference(r, chi.URLParam(r, "id"))
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	if !ref.IsMapped() {
		writeFileReferenceError(w, r, fmt.Errorf(
			"%w: the file reference %q names no solution yet; map it to one before a script uses it",
			schema.ErrFileReferenceUnmapped, ref.Name))
		return
	}
	calling := currentSession(r).Database
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(FileReferenceResolution{
		Name:            ref.Name,
		Database:        ref.Database,
		CallingDatabase: calling,
		Local:           strings.EqualFold(ref.Database, calling),
	})
}

func (h *FileReferenceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in schema.FileReferenceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	ref, err := schemaService(r).CreateFileReference(r.Context(), in)
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(ref)
}

func (h *FileReferenceHandler) Update(w http.ResponseWriter, r *http.Request) {
	var in schema.FileReferenceInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	current, err := resolveFileReference(r, chi.URLParam(r, "id"))
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	ref, err := schemaService(r).UpdateFileReference(r.Context(), current.ID, in)
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ref)
}

func (h *FileReferenceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	current, err := resolveFileReference(r, chi.URLParam(r, "id"))
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	if err := schemaService(r).DeleteFileReference(r.Context(), current.ID); err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// BindFileReferenceRequest maps a reference to a solution and resolves the
// tables of the occurrences that go through it.
type BindFileReferenceRequest struct {
	// Database is the solution to bind to; empty keeps the one the
	// reference is mapped to.
	Database string `json:"database"`
	// PeerToken is a session of Database, of an owner or an administrator.
	PeerToken string `json:"peer_token"`
	// Tables names the table of an occurrence (by occurrence name) where
	// the fields used through it do not single one out.
	Tables map[string]string `json:"tables,omitempty"`
}

// Bind maps the reference to a solution, resolves the table of each
// occurrence that goes through it and answers the verification report. A
// table is never guessed: an occurrence that several tables, or none, could
// be stays unbound with its candidates listed.
func (h *FileReferenceHandler) Bind(w http.ResponseWriter, r *http.Request) {
	var in BindFileReferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
			"Request body contains invalid JSON format")
		return
	}
	ref, err := resolveFileReference(r, chi.URLParam(r, "id"))
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	database := strings.TrimSpace(in.Database)
	if database == "" {
		database = ref.Database
	}
	if database == "" || !dbal.IsValidDatabaseName(database) || dbal.IsProtectedDatabase(database) {
		writeFileReferenceError(w, r, fmt.Errorf("%w: name the solution to bind %q to", schema.ErrInvalidFileReference, ref.Name))
		return
	}
	peer, ok := h.peerCatalog(w, r, in.PeerToken, database, ref.Name)
	if !ok {
		return
	}
	// The mapping is saved with the tables, once every check passed: a bind
	// that fails leaves the reference, and the scripts it runs, as they were.
	report, err := schemaService(r).BindFileReference(r.Context(), ref.ID, database, peer, in.Tables)
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

// VerifyFileReferenceRequest carries the session that reads the solution a
// reference is mapped to.
type VerifyFileReferenceRequest struct {
	PeerToken string `json:"peer_token"`
}

// Verify reports, for each occurrence that goes through the reference,
// whether its table and fields are in the solution it is mapped to: unbound,
// bound or mismatch. Nothing is written. An unmapped reference needs no
// peer_token: every occurrence through it is unbound.
func (h *FileReferenceHandler) Verify(w http.ResponseWriter, r *http.Request) {
	var in VerifyFileReferenceRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON",
				"Request body contains invalid JSON format")
			return
		}
	}
	ref, err := resolveFileReference(r, chi.URLParam(r, "id"))
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	var peer *schema.Service
	if ref.IsMapped() {
		var ok bool
		if peer, ok = h.peerCatalog(w, r, in.PeerToken, ref.Database, ref.Name); !ok {
			return
		}
	}
	report, err := schemaService(r).VerifyFileReference(r.Context(), ref.ID, peer)
	if err != nil {
		writeFileReferenceError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

// peerCatalog authorizes reading the catalog of the solution a reference
// goes to: a session of that solution, of an owner or an administrator,
// since the verification discloses its tables and fields.
func (h *FileReferenceHandler) peerCatalog(w http.ResponseWriter, r *http.Request, token, database, reference string) (*schema.Service, bool) {
	end, ok := resolvePeerSession(w, r, h.dbMgr, h.sessions, strings.TrimSpace(token), database, peerPurpose{
		what:           "the solution of the file reference " + reference,
		unavailable:    "File References Unavailable",
		unavailableWhy: "this server cannot authorize a session of another solution",
	})
	if !ok {
		return nil, false
	}
	if end.session.Role != auth.RoleOwner && end.session.Role != auth.RoleAdmin {
		writeForbidden(w, r, "reading the tables of "+database+" needs a peer_token of an owner or an administrator")
		return nil, false
	}
	return schema.NewService(end.driver), true
}

// resolveFileReference reads a reference by id, or by name when the path is
// a name: a script step names the reference it uses.
func resolveFileReference(r *http.Request, idOrName string) (*schema.FileReference, error) {
	svc := schemaService(r)
	idOrName = strings.TrimSpace(idOrName)
	ref, err := svc.GetFileReference(r.Context(), idOrName)
	if err == nil {
		return ref, nil
	}
	if !errors.Is(err, schema.ErrFileReferenceNotFound) {
		return nil, err
	}
	return svc.GetFileReferenceByName(r.Context(), idOrName)
}

func writeFileReferenceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, schema.ErrFileReferenceNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "File Reference Not Found", err.Error())
	case errors.Is(err, schema.ErrFileReferenceExists):
		telemetry.WriteProblem(w, r, http.StatusConflict, "File Reference Already Exists", err.Error())
	case errors.Is(err, schema.ErrFileReferenceInUse):
		telemetry.WriteProblem(w, r, http.StatusConflict, "File Reference In Use", err.Error())
	case errors.Is(err, schema.ErrFileReferenceUnmapped):
		telemetry.WriteProblem(w, r, http.StatusConflict, "File Reference Not Mapped", err.Error())
	case errors.Is(err, schema.ErrInvalidFileReference):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid File Reference", err.Error())
	default:
		writeSchemaError(w, r, err)
	}
}
