package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

type SecurityHandler struct {
	dbMgr     *dbal.MultiDatabaseManager
	schemaSvc *schema.Service
}

func NewSecurityHandler(dbMgr *dbal.MultiDatabaseManager, schemaSvc *schema.Service) *SecurityHandler {
	return &SecurityHandler{
		dbMgr:     dbMgr,
		schemaSvc: schemaSvc,
	}
}

func (h *SecurityHandler) RegisterRoutes(r chi.Router) {
	// Authentication
	r.Post("/api/v1/auth/login", h.Login)

	// User security management
	r.Route("/api/v1/security", func(r chi.Router) {
		r.Get("/users", h.ListUsers)
		r.Post("/users", h.CreateUser)
		r.Put("/users/{id}", h.UpdateUser)
		r.Delete("/users/{id}", h.DeleteUser)
		r.Get("/users/{id}/permissions", h.GetUserPermissions)
		r.Put("/users/{id}/permissions", h.SetUserPermissions)
	})
}

type LoginRequest struct {
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *SecurityHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	// If database specified, switch to it and ensure system tables
	if req.Database != "" && req.Database != h.dbMgr.ActiveDatabase() {
		if _, err := h.dbMgr.SetActiveDatabase(r.Context(), req.Database); err != nil {
			telemetry.WriteProblem(w, r, http.StatusBadRequest, "Database Switch Error", fmt.Sprintf("database switch failed: %v", err))
			return
		}
		if err := h.schemaSvc.EnsureSystemTables(r.Context()); err != nil {
			telemetry.WriteInternalError(w, r, fmt.Errorf("failed ensuring system tables: %w", err))
			return
		}
	} else {
		_ = h.schemaSvc.EnsureSystemTables(r.Context())
	}

	user, err := h.schemaSvc.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"database": h.dbMgr.ActiveDatabase(),
		"user":     user,
	})
}

func (h *SecurityHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	_ = h.schemaSvc.EnsureSystemTables(r.Context())
	users, err := h.schemaSvc.ListUsers(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}

type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

func (h *SecurityHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	user, err := h.schemaSvc.CreateUser(r.Context(), req.Username, req.Password, req.Role)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "User Creation Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(user)
}

type UpdateUserRequest struct {
	Password string `json:"password,omitempty"`
	Role     string `json:"role"`
}

func (h *SecurityHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if err := h.schemaSvc.UpdateUser(r.Context(), id, req.Password, req.Role); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "User Update Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":     id,
		"status": "updated",
	})
}

func (h *SecurityHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.schemaSvc.DeleteUser(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "User Deletion Error", err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *SecurityHandler) GetUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	perms, err := h.schemaSvc.GetUserPermissions(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Permissions Not Found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(perms)
}

type SetPermissionsRequest struct {
	Permissions []schema.UserLayoutPermission `json:"permissions"`
}

func (h *SecurityHandler) SetUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req SetPermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	if err := h.schemaSvc.SetUserPermissions(r.Context(), id, req.Permissions); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Permission Update Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}
