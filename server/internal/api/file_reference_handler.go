package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// FileReferenceHandler serves the file references of the solution the caller
// is signed in to (#107): the named links from this solution's scripts to
// other solutions.
type FileReferenceHandler struct{}

func NewFileReferenceHandler() *FileReferenceHandler { return &FileReferenceHandler{} }

func (h *FileReferenceHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/file-references", func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/", h.List)
		r.With(RequireAdmin, RequireDesignUnlocked).Post("/", h.Create)
		r.Get("/{id}", h.Get)
		r.Get("/{id}/resolve", h.Resolve)
		r.With(RequireAdmin, RequireDesignUnlocked).Put("/{id}", h.Update)
		r.With(RequireAdmin, RequireDesignUnlocked).Delete("/{id}", h.Delete)
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
	case errors.Is(err, schema.ErrFileReferenceUnmapped):
		telemetry.WriteProblem(w, r, http.StatusConflict, "File Reference Not Mapped", err.Error())
	case errors.Is(err, schema.ErrInvalidFileReference):
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid File Reference", err.Error())
	default:
		writeSchemaError(w, r, err)
	}
}
