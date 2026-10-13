package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/designreport"
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
	// ControlStore persists project ↔ database mappings, options, and user
	// table/layout permissions in SQLite.
	ControlStore *control.Store
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

func (h *SolutionHandler) syncProjectRecord(ctx context.Context, dbName string, svc *schema.Service, fileOpts, pageSetup map[string]interface{}) {
	if h.opts.ControlStore == nil || strings.TrimSpace(dbName) == "" {
		return
	}
	projName := dbName
	locked := false
	if svc != nil {
		if meta, err := svc.GetSolutionMeta(ctx, dbName); err == nil {
			if meta.SolutionName != "" {
				projName = meta.SolutionName
			}
			locked = meta.DesignLocked
		}
	}
	_ = h.opts.ControlStore.UpsertProject(ctx, control.ProjectRecord{
		Database:     dbName,
		Name:         projName,
		Engine:       string(h.dbMgr.Engine()),
		DesignLocked: locked,
		FileOptions:  fileOpts,
		PageSetup:    pageSetup,
	})
}

// RegisterPublicRoutes registers the endpoints reachable without a session.
// The router passed in should apply AuthMiddleware.Optional so that signed-in
// callers are recognised.
func (h *SolutionHandler) RegisterPublicRoutes(r chi.Router) {
	r.Get("/api/v1/databases", h.ListDatabases)
	r.Get("/api/v1/databases/", h.ListDatabases)
	r.Get("/api/v1/projects", h.ListProjects)
	r.Get("/api/v1/projects/", h.ListProjects)
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
		r.Get("/meta", h.GetSolutionMeta)
		r.Get("/options", h.GetSolutionOptions)
		r.With(RequireAdmin).Put("/options", h.UpdateSolutionOptions)
		r.With(RequireAdmin).Get("/export", h.ExportSolution)
		r.With(RequireAdmin).Post("/export", h.ExportSolution)
		r.With(RequireOwner, RequireDesignUnlocked).Post("/import", h.ImportSolution)
		r.With(RequireOwner).Post("/clone", h.CloneSolution)
		r.With(RequireOwner, RequireDesignUnlocked).Post("/rename", h.RenameSolution)
		r.With(RequireOwner).Post("/lock-design", h.LockSolutionDesign)
		// Copy and paste structure between solutions (#54).
		r.With(RequireAdmin).Post("/clipboard/copy", h.CopyStructure)
		r.With(RequireAdmin).Post("/clipboard/preview", h.PreviewClipboard)
		r.With(RequireAdmin, RequireDesignUnlocked).Post("/clipboard/paste", h.PasteClipboard)
		// The design as a report to read, or as text for version control
		// (#49). It carries no records and no secrets.
		r.With(RequireAdmin).Get("/design-report", h.DesignReport)
		// The data file carries every record, so it needs the same bulk
		// capability as a record export or import (#39).
		r.With(RequireAdmin, RequireCapability(schema.CapabilityBulkExport)).Get("/export-data", h.ExportDatabaseData)
		r.With(RequireAdmin, RequireCapability(schema.CapabilityBulkImport)).Post("/import-data", h.ImportDatabaseData)
	})
}

// sessionDatabase returns the database of the caller's session, or "" for anonymous callers.
func sessionDatabase(r *http.Request) string {
	if sess, ok := auth.FromContext(r.Context()); ok {
		return sess.Database
	}
	return ""
}

func (h *SolutionHandler) collectProjects(ctx context.Context, databases []string) []control.ProjectRecord {
	projects := make([]control.ProjectRecord, 0, len(databases))
	for _, dbName := range databases {
		if h.opts.ControlStore != nil {
			if proj, err := h.opts.ControlStore.GetProject(ctx, dbName); err == nil && proj != nil {
				projects = append(projects, *proj)
				continue
			}
		}
		rec := control.ProjectRecord{
			Database: dbName,
			Name:     dbName,
			Engine:   string(h.dbMgr.Engine()),
		}
		if driver, err := h.dbMgr.DriverFor(ctx, dbName); err == nil {
			svc := schema.NewService(driver)
			if svc.HasSystemCatalog(ctx) {
				if meta, err := svc.GetSolutionMeta(ctx, dbName); err == nil {
					if meta.SolutionName != "" {
						rec.Name = meta.SolutionName
					}
					rec.DesignLocked = meta.DesignLocked
				}
			}
		}
		if h.opts.ControlStore != nil {
			_ = h.opts.ControlStore.UpsertProject(ctx, rec)
		}
		projects = append(projects, rec)
	}
	return projects
}

// ListDatabases lists the solution databases available on the server along with
// their registered project metadata from the API control store.
func (h *SolutionHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	databases, err := h.dbMgr.ListDatabases(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed listing databases: %w", err))
		return
	}
	projects := h.collectProjects(r.Context(), databases)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"databases": databases,
		"projects":  projects,
		"active":    sessionDatabase(r),
	})
}

// ListProjects returns the project ↔ database registry stored in the API's SQLite control store.
func (h *SolutionHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	h.ListDatabases(w, r)
}

type CreateDatabaseRequest struct {
	Database     string                 `json:"database"`
	SolutionName string                 `json:"solution_name,omitempty"`
	User         string                 `json:"user,omitempty"`
	Password     string                 `json:"password,omitempty"`
	FileOptions  map[string]interface{} `json:"file_options,omitempty"`
	PageSetup    map[string]interface{} `json:"page_setup,omitempty"`
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
	var svc *schema.Service
	driver, err := h.dbMgr.DriverFor(r.Context(), dbName)
	if err == nil {
		svc = schema.NewService(driver)
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

	projName := strings.TrimSpace(req.SolutionName)
	if projName == "" {
		projName = dbName
	}
	if svc != nil && projName != dbName {
		_, _ = svc.RenameSolution(r.Context(), dbName, projName)
	}
	if h.opts.ControlStore != nil {
		_ = h.opts.ControlStore.UpsertProject(r.Context(), control.ProjectRecord{
			Database:    dbName,
			Name:        projName,
			Engine:      string(h.dbMgr.Engine()),
			FileOptions: req.FileOptions,
			PageSetup:   req.PageSetup,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database":      dbName,
		"solution_name": projName,
		"status":        "created",
		"active":        sessionDatabase(r),
		"owner_user":    ownerUser,
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
	if h.opts.ControlStore != nil {
		_ = h.opts.ControlStore.DeleteProject(r.Context(), name)
	}

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
	sess := currentSession(r)
	svc := schemaService(r)
	if solutionName == "" {
		if meta, err := svc.GetSolutionMeta(r.Context(), sess.Database); err == nil && meta.SolutionName != "" {
			solutionName = meta.SolutionName
		} else {
			solutionName = "file4base_solution"
		}
	}

	fileOpts := req.FileOptions
	pageSetup := req.PageSetup
	if h.opts.ControlStore != nil {
		if proj, err := h.opts.ControlStore.GetProject(r.Context(), sess.Database); err == nil && proj != nil {
			if len(fileOpts) == 0 && len(proj.FileOptions) > 0 {
				fileOpts = proj.FileOptions
			}
			if len(pageSetup) == 0 && len(proj.PageSetup) > 0 {
				pageSetup = proj.PageSetup
			}
		}
		h.syncProjectRecord(r.Context(), sess.Database, svc, fileOpts, pageSetup)
	}

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
		FileOptions: fileOpts,
		PageSetup:   pageSetup,
	}

	bytes, err := svc.ExportSolution(r.Context(), opts)
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

	svc := schemaService(r)
	report, err := svc.ImportSolution(r.Context(), body)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Solution Import Error", err.Error())
		return
	}

	dbName := sessionDatabase(r)
	if strings.TrimSpace(report.SolutionName) != "" {
		if prev, err := svc.GetSolutionMeta(r.Context(), dbName); err == nil && !prev.DesignLocked {
			_, _ = svc.RenameSolution(r.Context(), prev.SolutionName, report.SolutionName)
		}
	}
	h.syncProjectRecord(r.Context(), dbName, svc, report.FileOptions, report.PageSetup)

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

// DesignReport answers what the solution is made of (#49).
//
//	GET /api/v1/solutions/design-report?format=html|xml|json&solution=<name>
//
// HTML is the report to read; XML and JSON are the design as text, for
// processing and for version control, and leave the timestamp out so that
// writing the same design twice produces no diff.
func (h *SolutionHandler) DesignReport(w http.ResponseWriter, r *http.Request) {
	format := designreport.Format(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))))
	if format == "" {
		format = designreport.FormatHTML
	}
	if !format.Known() {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Unknown Format",
			"a design report is written as html, xml or json")
		return
	}

	solution := strings.TrimSpace(r.URL.Query().Get("solution"))
	if solution == "" {
		if meta, err := schemaService(r).GetSolutionMeta(r.Context(), sessionDatabase(r)); err == nil && meta.SolutionName != "" {
			solution = meta.SolutionName
		} else {
			solution = sessionDatabase(r)
		}
	}

	report, err := schemaService(r).DesignReport(r.Context(), schema.DesignReportOptions{
		SolutionName: solution,
		Database:     sessionDatabase(r),
		Timestamped:  format == designreport.FormatHTML,
	})
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	document, err := designreport.Write(report, format)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", format.ContentType())
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", designreport.FileName(report.Solution, format)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(document)
}

// GetSolutionMeta returns the solution's logical name and whether its design is
// permanently locked (#53).
func (h *SolutionHandler) GetSolutionMeta(w http.ResponseWriter, r *http.Request) {
	dbName := sessionDatabase(r)
	svc := schemaService(r)
	meta, err := svc.GetSolutionMeta(r.Context(), dbName)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	var fileOpts, pageSetup map[string]interface{}
	if h.opts.ControlStore != nil {
		if proj, err := h.opts.ControlStore.GetProject(r.Context(), dbName); err == nil && proj != nil {
			fileOpts = proj.FileOptions
			pageSetup = proj.PageSetup
		} else {
			h.syncProjectRecord(r.Context(), dbName, svc, nil, nil)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Database    string                 `json:"database"`
		FileOptions map[string]interface{} `json:"file_options,omitempty"`
		PageSetup   map[string]interface{} `json:"page_setup,omitempty"`
		schema.SolutionMeta
	}{
		Database:     dbName,
		FileOptions:  fileOpts,
		PageSetup:    pageSetup,
		SolutionMeta: meta,
	})
}

// SolutionOptionsPayload describes the project options stored in the API's
// SQLite control store.
type SolutionOptionsPayload struct {
	SolutionName string                 `json:"solution_name,omitempty"`
	FileOptions  map[string]interface{} `json:"file_options,omitempty"`
	PageSetup    map[string]interface{} `json:"page_setup,omitempty"`
}

// GetSolutionOptions returns the project options (solution_name, file_options,
// page_setup) stored in the API's SQLite control store for the active database.
func (h *SolutionHandler) GetSolutionOptions(w http.ResponseWriter, r *http.Request) {
	dbName := sessionDatabase(r)
	svc := schemaService(r)
	meta, _ := svc.GetSolutionMeta(r.Context(), dbName)
	var fileOpts, pageSetup map[string]interface{}
	if h.opts.ControlStore != nil {
		if proj, err := h.opts.ControlStore.GetProject(r.Context(), dbName); err == nil && proj != nil {
			if proj.Name != "" && meta.SolutionName == "" {
				meta.SolutionName = proj.Name
			}
			fileOpts = proj.FileOptions
			pageSetup = proj.PageSetup
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database":      dbName,
		"solution_name": meta.SolutionName,
		"design_locked": meta.DesignLocked,
		"file_options":  fileOpts,
		"page_setup":    pageSetup,
	})
}

// UpdateSolutionOptions persists updated project options (file_options,
// page_setup, and optional solution_name) in the API's SQLite control store.
func (h *SolutionHandler) UpdateSolutionOptions(w http.ResponseWriter, r *http.Request) {
	var req SolutionOptionsPayload
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	dbName := sessionDatabase(r)
	svc := schemaService(r)
	if strings.TrimSpace(req.SolutionName) != "" {
		if prev, err := svc.GetSolutionMeta(r.Context(), dbName); err == nil && !prev.DesignLocked {
			_, _ = svc.RenameSolution(r.Context(), prev.SolutionName, req.SolutionName)
		}
	}
	h.syncProjectRecord(r.Context(), dbName, svc, req.FileOptions, req.PageSetup)
	h.GetSolutionOptions(w, r)
}

// CloneSolutionRequest describes a server-side clone without records (#53).
type CloneSolutionRequest struct {
	TargetDatabase string `json:"target_database"`
	SolutionName   string `json:"solution_name,omitempty"`
	LockDesign     bool   `json:"lock_design,omitempty"`
	OwnerUser      string `json:"owner_user,omitempty"`
	OwnerPassword  string `json:"owner_password,omitempty"`
}

// CloneSolution creates a new physical database on the server containing the
// entire design of the current solution and zero user records (#53).
// Optionally, the new solution's design is permanently locked on creation.
func (h *SolutionHandler) CloneSolution(w http.ResponseWriter, r *http.Request) {
	var req CloneSolutionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	targetDB := strings.ToLower(strings.TrimSpace(req.TargetDatabase))
	if targetDB == "" {
		telemetry.WriteValidationProblem(w, r, "A target database name is required", []telemetry.InvalidParam{
			{Name: "target_database", Reason: "target_database cannot be empty"},
		})
		return
	}

	sess := currentSession(r)
	if strings.EqualFold(targetDB, sess.Database) {
		telemetry.WriteProblem(w, r, http.StatusConflict, "Database Already Exists",
			"target_database must be a new database name, not the active database")
		return
	}

	ownerUser := strings.ToLower(strings.TrimSpace(req.OwnerUser))
	if ownerUser != "" || strings.TrimSpace(req.OwnerPassword) != "" {
		var invalid []telemetry.InvalidParam
		if ownerUser == "" {
			invalid = append(invalid, telemetry.InvalidParam{Name: "owner_user", Reason: "an owner username is required when owner_password is set"})
		}
		if reason := ownerPasswordProblem(req.OwnerPassword, ownerUser, targetDB); reason != "" {
			invalid = append(invalid, telemetry.InvalidParam{Name: "owner_password", Reason: reason})
		}
		if len(invalid) > 0 {
			telemetry.WriteValidationProblem(w, r, "Valid owner credentials are required", invalid)
			return
		}
	}

	solutionName := strings.TrimSpace(req.SolutionName)
	if solutionName == "" {
		solutionName = targetDB
	}

	// 1. Export the design bundle from the source database (zero records).
	srcSvc := schemaService(r)
	bundleBytes, err := srcSvc.ExportSolution(r.Context(), schema.ExportOptions{
		SolutionName: solutionName,
		Connection: schema.BundleConnection{
			Engine:   string(h.dbMgr.Engine()),
			Host:     "localhost",
			Database: targetDB,
			User:     sess.Username,
			SSLMode:  "disable",
		},
	})
	if err != nil {
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed exporting source solution design: %w", err))
		return
	}

	// 2. Create the target physical database.
	if err := h.dbMgr.CreateDatabase(r.Context(), targetDB); err != nil {
		if errors.Is(err, dbal.ErrDatabaseExists) {
			telemetry.WriteProblem(w, r, http.StatusConflict, "Database Already Exists",
				"database '"+targetDB+"' already exists; choose a new database name")
			return
		}
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Database Creation Error", err.Error())
		return
	}

	rollbackClone := func(cause error) {
		_ = h.dbMgr.DropDatabase(r.Context(), targetDB)
		telemetry.WriteInternalError(w, r, fmt.Errorf("failed cloning solution into %q: %w", targetDB, cause))
	}

	dstDriver, err := h.dbMgr.DriverFor(r.Context(), targetDB)
	if err != nil {
		rollbackClone(err)
		return
	}
	dstSvc := schema.NewService(dstDriver)

	// 3. Initialize system tables and provision the owner account on the clone.
	if ownerUser != "" {
		if err := dstSvc.EnsureSystemTablesWithCredentials(r.Context(), ownerUser, req.OwnerPassword); err != nil {
			rollbackClone(err)
			return
		}
	} else {
		if err := dstSvc.EnsureSystemTablesWithCredentials(r.Context(), "", ""); err != nil {
			rollbackClone(err)
			return
		}
		if err := srcSvc.CopyOwnerAccount(r.Context(), dstSvc, sess.UserID); err != nil {
			rollbackClone(err)
			return
		}
		ownerUser = sess.Username
	}

	// 4. Import the design bundle into the target database (no records).
	report, err := dstSvc.ImportSolution(r.Context(), bundleBytes)
	if err != nil {
		rollbackClone(err)
		return
	}

	// 5. Set the solution name (and rewrite references if renamed from source).
	srcMeta, _ := srcSvc.GetSolutionMeta(r.Context(), sess.Database)
	if _, err := dstSvc.RenameSolution(r.Context(), srcMeta.SolutionName, solutionName); err != nil {
		rollbackClone(err)
		return
	}

	// 6. Optionally lock the clone's design permanently.
	if req.LockDesign {
		if _, err := dstSvc.LockDesign(r.Context(), solutionName); err != nil {
			rollbackClone(err)
			return
		}
	}
	h.syncProjectRecord(r.Context(), targetDB, dstSvc, report.FileOptions, report.PageSetup)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(struct {
		Status       string `json:"status"`
		Database     string `json:"database"`
		SolutionName string `json:"solution_name"`
		DesignLocked bool   `json:"design_locked"`
		OwnerUser    string `json:"owner_user"`
		*schema.ImportReport
	}{
		Status:       "cloned",
		Database:     targetDB,
		SolutionName: solutionName,
		DesignLocked: req.LockDesign,
		OwnerUser:    ownerUser,
		ImportReport: report,
	})
}

// RenameSolutionRequest is the JSON body of POST /api/v1/solutions/rename (#53).
type RenameSolutionRequest struct {
	SolutionName string `json:"solution_name"`
}

// RenameSolution updates the solution's logical name and rewrites any layout
// or script references that named the previous solution name (#53).
func (h *SolutionHandler) RenameSolution(w http.ResponseWriter, r *http.Request) {
	var req RenameSolutionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	dbName := sessionDatabase(r)
	svc := schemaService(r)
	prev, err := svc.GetSolutionMeta(r.Context(), dbName)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	meta, err := svc.RenameSolution(r.Context(), prev.SolutionName, req.SolutionName)
	if err != nil {
		switch {
		case errors.Is(err, schema.ErrDesignLocked):
			writeForbidden(w, r, err.Error())
		case errors.Is(err, schema.ErrInvalidSolutionName):
			telemetry.WriteValidationProblem(w, r, "A valid solution name is required", []telemetry.InvalidParam{
				{Name: "solution_name", Reason: err.Error()},
			})
		default:
			telemetry.WriteInternalError(w, r, err)
		}
		return
	}
	h.syncProjectRecord(r.Context(), dbName, svc, nil, nil)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Database string `json:"database"`
		schema.SolutionMeta
	}{
		Database:     dbName,
		SolutionMeta: meta,
	})
}

// LockDesignRequest is the JSON body of POST /api/v1/solutions/lock-design (#53).
type LockDesignRequest struct {
	Confirm bool `json:"confirm"`
}

// LockSolutionDesign permanently locks the design of the active solution (#53).
func (h *SolutionHandler) LockSolutionDesign(w http.ResponseWriter, r *http.Request) {
	var req LockDesignRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	if !req.Confirm {
		telemetry.WriteValidationProblem(w, r, "Locking a solution's design is permanent and requires explicit confirmation", []telemetry.InvalidParam{
			{Name: "confirm", Reason: "confirm must be true to permanently lock the design"},
		})
		return
	}

	dbName := sessionDatabase(r)
	svc := schemaService(r)
	meta, err := svc.LockDesign(r.Context(), dbName)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	h.syncProjectRecord(r.Context(), dbName, svc, nil, nil)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Database string `json:"database"`
		schema.SolutionMeta
	}{
		Database:     dbName,
		SolutionMeta: meta,
	})
}

// CopyStructure produces a reviewable JSON clipboard envelope for a table,
// fields, layout, or script (#54).
func (h *SolutionHandler) CopyStructure(w http.ResponseWriter, r *http.Request) {
	var in schema.CopyStructureInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	env, err := schemaService(r).CopyStructure(r.Context(), in)
	if err != nil {
		if errors.Is(err, schema.ErrInvalidClipboard) {
			telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Clipboard Request", err.Error())
			return
		}
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Not Found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(env)
}

// PreviewClipboard inspects a clipboard envelope against the active database
// and reports any name collisions and missing dependencies before pasting (#54).
func (h *SolutionHandler) PreviewClipboard(w http.ResponseWriter, r *http.Request) {
	var in schema.PreviewClipboardInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	preview, err := schemaService(r).PreviewClipboard(r.Context(), in)
	if err != nil {
		if errors.Is(err, schema.ErrInvalidClipboard) {
			telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Clipboard Envelope", err.Error())
			return
		}
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Clipboard Preview Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(preview)
}

// PasteClipboard pastes a copied table, fields, layout, or script into the
// active database (#54).
func (h *SolutionHandler) PasteClipboard(w http.ResponseWriter, r *http.Request) {
	var in schema.PasteClipboardInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&in); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	report, err := schemaService(r).PasteClipboard(r.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, schema.ErrInvalidClipboard):
			telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Invalid Clipboard Envelope", err.Error())
		case errors.Is(err, schema.ErrDesignLocked):
			writeForbidden(w, r, err.Error())
		default:
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Clipboard Paste Error", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(report)
}

