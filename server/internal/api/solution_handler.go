package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// maxImportBytes bounds the size of an uploaded solution or data file.
const maxImportBytes = 256 << 20 // 256 MiB

// SolutionHandlerOptions tunes the server-level database endpoints.
type SolutionHandlerOptions struct {
	// AllowPublicDatabaseCreation lets callers without a session create new
	// databases (the desktop "New Database" flow happens before sign-in).
	// When false, only a signed-in owner can create databases.
	AllowPublicDatabaseCreation bool
}

// SolutionHandler serves server-level database management and the
// import/export of solution and data files.
type SolutionHandler struct {
	dbMgr    *dbal.MultiDatabaseManager
	sessions *auth.Store
	opts     SolutionHandlerOptions
}

func NewSolutionHandler(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store, opts SolutionHandlerOptions) *SolutionHandler {
	return &SolutionHandler{
		dbMgr:    dbMgr,
		sessions: sessions,
		opts:     opts,
	}
}

// RegisterPublicRoutes registers the endpoints reachable without a session.
// The router passed in should apply AuthMiddleware.Optional so that signed-in
// callers are recognised.
func (h *SolutionHandler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/api/v1/databases", h.ListDatabases)
	r.Get("/api/v1/databases/", h.ListDatabases)
	r.Post("/api/v1/databases", h.CreateDatabase)
	r.Post("/api/v1/databases/", h.CreateDatabase)
	r.Post("/api/v1/databases/switch", h.SwitchDatabase)
}

// RegisterRoutes registers the endpoints that require a session.
func (h *SolutionHandler) RegisterRoutes(r chi.Router) {
	r.With(RequireSession).Delete("/api/v1/databases/{name}", h.DeleteDatabase)

	// Solution and data packaging endpoints
	r.Route("/api/v1/solutions", func(r chi.Router) {
		r.Use(RequireSession)
		r.With(RequireAdmin).Get("/export", h.ExportSolution)
		r.With(RequireAdmin).Post("/export", h.ExportSolution)
		r.With(RequireOwner).Post("/import", h.ImportSolution)
		r.With(RequireAdmin).Get("/export-data", h.ExportDatabaseData)
		r.With(RequireAdmin).Post("/import-data", h.ImportDatabaseData)
	})
}

// sessionDatabase returns the database of the caller's session, or "" for anonymous callers.
func sessionDatabase(r *http.Request) string {
	if sess, ok := auth.FromContext(r.Context()); ok {
		return sess.Database
	}
	return ""
}

// ListDatabases lists the solution databases available on the server.
// "active" is the database of the caller's own session (empty when signed out).
func (h *SolutionHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	databases, err := h.dbMgr.ListDatabases(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed listing databases: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"databases": databases,
		"active":    sessionDatabase(r),
	})
}

type CreateDatabaseRequest struct {
	Database string `json:"database"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

// CreateDatabase creates a new database and provisions its first owner account
// with the credentials supplied by the caller. It never touches an existing
// database: a name that is already taken yields 409 Conflict.
func (h *SolutionHandler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	if !h.opts.AllowPublicDatabaseCreation {
		sess, ok := auth.FromContext(r.Context())
		if !ok {
			writeUnauthorized(w, r, "creating databases requires a signed-in owner on this server")
			return
		}
		if !sess.IsOwner() {
			writeForbidden(w, r, "only an owner can create databases on this server")
			return
		}
	}

	var req CreateDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	dbName := strings.ToLower(strings.TrimSpace(req.Database))
	ownerUser := strings.ToLower(strings.TrimSpace(req.User))
	var invalid []telemetry.InvalidParam
	if dbName == "" {
		invalid = append(invalid, telemetry.InvalidParam{Name: "database", Reason: "database name cannot be empty"})
	}
	if ownerUser == "" {
		invalid = append(invalid, telemetry.InvalidParam{Name: "user", Reason: "an owner username is required"})
	}
	if reason := ownerPasswordProblem(req.Password, ownerUser, dbName); reason != "" {
		invalid = append(invalid, telemetry.InvalidParam{Name: "password", Reason: reason})
	}
	if len(invalid) > 0 {
		telemetry.WriteValidationProblem(w, r, "A database name and the credentials of its first owner are required", invalid)
		return
	}

	// 1. Create the physical database
	if err := h.dbMgr.CreateDatabase(r.Context(), dbName); err != nil {
		if errors.Is(err, dbal.ErrDatabaseExists) {
			telemetry.WriteProblem(w, r, http.StatusConflict, "Database Already Exists",
				"database '"+dbName+"' already exists; sign in with its credentials instead")
			return
		}
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Database Creation Error", err.Error())
		return
	}

	// 2. Initialize the system catalog with the owner account
	driver, err := h.dbMgr.DriverFor(r.Context(), dbName)
	if err == nil {
		svc := schema.NewService(driver)
		if err = svc.EnsureSystemTablesWithCredentials(r.Context(), ownerUser, req.Password); err == nil {
			var users int
			if users, err = svc.CountUsers(r.Context()); err == nil && users == 0 {
				err = errors.New("owner account was not provisioned")
			}
		}
	}
	if err != nil {
		// Do not leave behind a database nobody can sign in to
		_ = h.dbMgr.DropDatabase(r.Context(), dbName)
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed initializing system catalog on new database: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database":   dbName,
		"status":     "created",
		"active":     sessionDatabase(r),
		"owner_user": ownerUser,
	})
}

// MinOwnerPasswordLength is the shortest password accepted for the first
// owner of a new database.
const MinOwnerPasswordLength = 8

// ownerPasswordProblem returns why password cannot protect the first owner
// account of database dbName, or "" when it is acceptable. Database names are
// listed publicly, and the owner username is often a default, so neither may
// be reused as the password.
func ownerPasswordProblem(password, user, dbName string) string {
	p := strings.TrimSpace(password)
	switch {
	case p == "":
		return "an owner password is required"
	case len([]rune(p)) < MinOwnerPasswordLength:
		return fmt.Sprintf("the owner password must be at least %d characters long", MinOwnerPasswordLength)
	case strings.EqualFold(p, user) || strings.EqualFold(p, dbName):
		return "the owner password cannot be the username or the database name"
	}
	return ""
}

type SwitchDatabaseRequest struct {
	Database string `json:"database"`
}

// SwitchDatabase is kept for backwards compatibility. The server no longer
// has a global "active" database: every session is bound to the database it
// signed in to, so this endpoint only reports whether the database exists.
// To work on another database, sign in to it via POST /api/v1/auth/login.
func (h *SolutionHandler) SwitchDatabase(w http.ResponseWriter, r *http.Request) {
	var req SwitchDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	dbName := strings.TrimSpace(req.Database)
	if dbName == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "Database name cannot be empty")
		return
	}

	canonical, exists, err := h.dbMgr.LookupDatabase(r.Context(), dbName)
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed listing databases: %w", err))
		return
	}
	if !exists {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Database Not Found", "database '"+dbName+"' does not exist on this server")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Deprecation", "true")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database": canonical,
		"active":   sessionDatabase(r),
		"status":   "available",
	})
}

type DeleteDatabaseRequest struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// DeleteDatabase drops a physical database on the server.
//
// The caller must prove ownership of the database being dropped: either the
// session itself is an owner session on that database, or the request body
// carries the credentials of one of its owners.
func (h *SolutionHandler) DeleteDatabase(w http.ResponseWriter, r *http.Request) {
	requested := strings.TrimSpace(chi.URLParam(r, "name"))
	if requested == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Validation Failed", "Database name is required")
		return
	}

	name, exists, err := h.dbMgr.LookupDatabase(r.Context(), requested)
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed listing databases: %w", err))
		return
	}
	if !exists {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Database Not Found", "database '"+requested+"' does not exist on this server")
		return
	}

	sess := currentSession(r)
	authorized := sess.IsOwner() && sess.Database == name
	if !authorized {
		var creds DeleteDatabaseRequest
		if r.Body != nil {
			_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&creds)
		}
		if creds.Username != "" && creds.Password != "" {
			if driver, err := h.dbMgr.DriverFor(r.Context(), name); err == nil {
				svc := schema.NewService(driver)
				if svc.HasSystemCatalog(r.Context()) {
					if user, err := svc.Authenticate(r.Context(), creds.Username, creds.Password); err == nil && user.Role == auth.RoleOwner {
						authorized = true
					}
				}
			}
		}
	}
	if !authorized {
		writeForbidden(w, r, "dropping database '"+name+"' requires the credentials of one of its owners")
		return
	}

	if err := h.dbMgr.DropDatabase(r.Context(), name); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Database Deletion Error", err.Error())
		return
	}
	h.sessions.RevokeDatabase(name)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database": name,
		"status":   "deleted",
		"active":   sessionDatabase(r),
	})
}

// ExportSolutionRequest is the optional JSON body of POST /solutions/export:
// client settings stored in the file as they are.
type ExportSolutionRequest struct {
	SolutionName string                 `json:"solution_name"`
	FileOptions  map[string]interface{} `json:"file_options"`
	PageSetup    map[string]interface{} `json:"page_setup"`
}

// ExportSolution packages the design of the session's database (tables, field
// options, occurrences, relationships, layouts, scripts and accounts, never
// passwords) into a MessagePack solution file. GET takes the solution name in
// ?name=; POST takes ExportSolutionRequest.
func (h *SolutionHandler) ExportSolution(w http.ResponseWriter, r *http.Request) {
	var req ExportSolutionRequest
	if r.Method == http.MethodPost && r.ContentLength != 0 {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
			return
		}
	}
	solutionName := req.SolutionName
	if solutionName == "" {
		solutionName = r.URL.Query().Get("name")
	}
	if solutionName == "" {
		solutionName = "file4base_solution"
	}

	sess := currentSession(r)
	port := 5432
	if h.dbMgr.Engine() == dbal.EngineMariaDB {
		port = 3306
	}
	opts := schema.ExportOptions{
		SolutionName: solutionName,
		Connection: schema.BundleConnection{
			Engine:   string(h.dbMgr.Engine()),
			Host:     "localhost",
			Port:     port,
			Database: sess.Database,
			User:     sess.Username,
			SSLMode:  "disable",
		},
		FileOptions: req.FileOptions,
		PageSetup:   req.PageSetup,
	}

	bytes, err := schemaService(r).ExportSolution(r.Context(), opts)
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed exporting solution: %w", err))
		return
	}

	cleanFileName := sanitizeFileName(solutionName)
	if !strings.HasSuffix(cleanFileName, ".f4p") {
		cleanFileName += ".f4p"
	}

	w.Header().Set("Content-Type", "application/x-msgpack")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, cleanFileName))
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	_, _ = w.Write(bytes)
}

// sanitizeFileName keeps a download file name safe to embed in a header.
func sanitizeFileName(name string) string {
	var b strings.Builder
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteRune(c)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "file4base_solution"
	}
	return b.String()
}

// ImportSolution restores a solution from MessagePack bytes (.f4b) into the session's database
func (h *SolutionHandler) ImportSolution(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Read Error", "Failed reading request body")
		return
	}

	report, err := schemaService(r).ImportSolution(r.Context(), body)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Solution Import Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Status string `json:"status"`
		*schema.ImportReport
	}{"imported", report})
}

// ExportDatabaseData exports all rows from all user tables of the session's database into MessagePack (.f4data)
func (h *SolutionHandler) ExportDatabaseData(w http.ResponseWriter, r *http.Request) {
	dbName := currentSession(r).Database
	bytes, err := schemaService(r).ExportDatabaseData(r.Context(), dbName)
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed exporting database data: %w", err))
		return
	}

	fileName := fmt.Sprintf("%s.f4data", sanitizeFileName(dbName))
	w.Header().Set("Content-Type", "application/x-msgpack")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	_, _ = w.Write(bytes)
}

// ImportDatabaseData restores records into the session's database from MessagePack (.f4data)
func (h *SolutionHandler) ImportDatabaseData(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxImportBytes))
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Read Error", "Failed reading request body")
		return
	}

	report, err := schemaService(r).ImportDatabaseData(r.Context(), body)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Database Data Import Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Status       string `json:"status"`
		RecordsCount int    `json:"records_count"`
		*schema.DataImportReport
	}{"restored", report.RecordsInserted, report})
}
