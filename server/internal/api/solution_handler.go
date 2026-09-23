package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/go-chi/chi/v5"
)

type SolutionHandler struct {
	dbMgr     *dbal.MultiDatabaseManager
	schemaSvc *schema.Service
}

func NewSolutionHandler(dbMgr *dbal.MultiDatabaseManager, schemaSvc *schema.Service) *SolutionHandler {
	return &SolutionHandler{
		dbMgr:     dbMgr,
		schemaSvc: schemaSvc,
	}
}

func (h *SolutionHandler) RegisterRoutes(r chi.Router) {
	// Database management endpoints
	r.Route("/api/v1/databases", func(r chi.Router) {
		r.Get("/", h.ListDatabases)
		r.Post("/", h.CreateDatabase)
		r.Post("/switch", h.SwitchDatabase)
	})

	// Solution and data packaging endpoints
	r.Route("/api/v1/solutions", func(r chi.Router) {
		r.Get("/export", h.ExportSolution)
		r.Post("/import", h.ImportSolution)
		r.Get("/export-data", h.ExportDatabaseData)
		r.Post("/import-data", h.ImportDatabaseData)
	})
}

// ListDatabases lists all available databases on the PostgreSQL server
func (h *SolutionHandler) ListDatabases(w http.ResponseWriter, r *http.Request) {
	databases, err := h.dbMgr.ListDatabases(r.Context())
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed listing databases: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"databases": databases,
		"active":    h.dbMgr.ActiveDatabase(),
	})
}

type CreateDatabaseRequest struct {
	Database string `json:"database"`
}

// CreateDatabase creates a new database on PostgreSQL and initializes system tables
func (h *SolutionHandler) CreateDatabase(w http.ResponseWriter, r *http.Request) {
	var req CreateDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	dbName := strings.ToLower(strings.TrimSpace(req.Database))
	if dbName == "" {
		http.Error(w, "Database name cannot be empty", http.StatusBadRequest)
		return
	}

	// 1. Create database in server
	if err := h.dbMgr.CreateDatabase(r.Context(), dbName); err != nil {
		http.Error(w, fmt.Sprintf("Failed creating database: %v", err), http.StatusBadRequest)
		return
	}

	// 2. Switch to it and ensure system tables
	driver, err := h.dbMgr.SetActiveDatabase(r.Context(), dbName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Database created but failed switching: %v", err), http.StatusInternalServerError)
		return
	}

	// Initialize system catalog on new DB
	tempSvc := schema.NewService(driver)
	if err := tempSvc.EnsureSystemTables(r.Context()); err != nil {
		http.Error(w, fmt.Sprintf("Failed initializing system catalog on new database: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database": dbName,
		"status":   "created",
		"active":   h.dbMgr.ActiveDatabase(),
	})
}

type SwitchDatabaseRequest struct {
	Database string `json:"database"`
}

// SwitchDatabase switches the active database context
func (h *SolutionHandler) SwitchDatabase(w http.ResponseWriter, r *http.Request) {
	var req SwitchDatabaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	dbName := strings.ToLower(strings.TrimSpace(req.Database))
	if dbName == "" {
		http.Error(w, "Database name cannot be empty", http.StatusBadRequest)
		return
	}

	driver, err := h.dbMgr.SetActiveDatabase(r.Context(), dbName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed switching database: %v", err), http.StatusBadRequest)
		return
	}

	// Ensure system catalog exists on target DB
	tempSvc := schema.NewService(driver)
	_ = tempSvc.EnsureSystemTables(r.Context())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"active": h.dbMgr.ActiveDatabase(),
		"status": "connected",
	})
}

// ExportSolution packages active layouts, schemas, TOs, users, and DB config into MessagePack (.f4b)
func (h *SolutionHandler) ExportSolution(w http.ResponseWriter, r *http.Request) {
	solutionName := r.URL.Query().Get("name")
	if solutionName == "" {
		solutionName = "file4base_solution"
	}

	activeDB := h.dbMgr.ActiveDatabase()
	host := r.URL.Query().Get("host")
	if host == "" {
		host = "localhost"
	}
	portStr := r.URL.Query().Get("port")
	port := 5432
	if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
		port = p
	}
	user := r.URL.Query().Get("user")
	if user == "" {
		user = "file4base"
	}
	password := r.URL.Query().Get("password")
	if password == "" {
		password = "dev_password"
	}

	dbConfig := schema.DatabaseConnectionConfig{
		Engine:   "postgres",
		Host:     host,
		Port:     port,
		Database: activeDB,
		User:     user,
		Password: password,
		SSLMode:  "disable",
	}

	bytes, err := h.schemaSvc.ExportSolution(r.Context(), solutionName, dbConfig)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed exporting solution: %v", err), http.StatusInternalServerError)
		return
	}

	cleanFileName := strings.ReplaceAll(solutionName, " ", "_")
	if !strings.HasSuffix(cleanFileName, ".f4b") {
		cleanFileName += ".f4b"
	}

	w.Header().Set("Content-Type", "application/x-msgpack")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, cleanFileName))
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	_, _ = w.Write(bytes)
}

// ImportSolution restores a solution from MessagePack bytes (.f4b)
func (h *SolutionHandler) ImportSolution(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed reading request body", http.StatusBadRequest)
		return
	}

	bundle, err := h.schemaSvc.ImportSolution(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed importing solution: %v", err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "imported",
		"solution_name": bundle.SolutionName,
		"tables_count":  len(bundle.Tables),
		"layouts_count": len(bundle.Layouts),
	})
}

// ExportDatabaseData exports all rows from all tables in the active database into MessagePack (.f4data)
func (h *SolutionHandler) ExportDatabaseData(w http.ResponseWriter, r *http.Request) {
	activeDB := h.dbMgr.ActiveDatabase()
	bytes, err := h.schemaSvc.ExportDatabaseData(r.Context(), activeDB)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed exporting database data: %v", err), http.StatusInternalServerError)
		return
	}

	fileName := fmt.Sprintf("%s.f4data", activeDB)
	w.Header().Set("Content-Type", "application/x-msgpack")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	w.Header().Set("Content-Length", strconv.Itoa(len(bytes)))
	_, _ = w.Write(bytes)
}

// ImportDatabaseData restores records into the active database from MessagePack (.f4data)
func (h *SolutionHandler) ImportDatabaseData(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed reading request body", http.StatusBadRequest)
		return
	}

	bundle, err := h.schemaSvc.ImportDatabaseData(r.Context(), body)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed importing database data: %v", err), http.StatusBadRequest)
		return
	}

	tablesRestored := len(bundle.TablesData)
	totalRecords := 0
	for _, rows := range bundle.TablesData {
		totalRecords += len(rows)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "restored",
		"database":        bundle.DatabaseName,
		"tables_restored": tablesRestored,
		"records_count":   totalRecords,
	})
}
