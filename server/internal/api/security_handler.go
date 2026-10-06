package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// SecurityHandler serves authentication (login, logout, session) and the
// management of user accounts and layout permissions.
type SecurityHandler struct {
	dbMgr    *dbal.MultiDatabaseManager
	sessions *auth.Store
}

func NewSecurityHandler(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store) *SecurityHandler {
	return &SecurityHandler{
		dbMgr:    dbMgr,
		sessions: sessions,
	}
}

// RegisterPublicRoutes registers the endpoints reachable without a session.
func (h *SecurityHandler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/api/v1/auth/login", h.Login)
}

// RegisterRoutes registers the endpoints that require a session.
func (h *SecurityHandler) RegisterRoutes(r chi.Router) {
	r.With(RequireSession).Post("/api/v1/auth/logout", h.Logout)
	r.With(RequireSession).Get("/api/v1/auth/session", h.CurrentSession)

	r.Route("/api/v1/security", func(r chi.Router) {
		r.Use(RequireSession)
		r.With(RequireAdmin).Get("/users", h.ListUsers)
		r.With(RequireAdmin).Post("/users", h.CreateUser)
		r.Put("/users/{id}", h.UpdateUser) // admins, or a user changing their own password
		r.With(RequireAdmin).Delete("/users/{id}", h.DeleteUser)
		r.Get("/users/{id}/permissions", h.GetUserPermissions) // admins, or the user themselves
		r.With(RequireAdmin).Put("/users/{id}/permissions", h.SetUserPermissions)
	})
}

type LoginRequest struct {
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login authenticates a user against one database and opens a session bound
// to that database. It changes no server-wide state.
func (h *SecurityHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	dbName := strings.TrimSpace(req.Database)
	if dbName == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "database is required")
		return
	}

	canonical, exists, err := h.dbMgr.LookupDatabase(r.Context(), dbName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable", "the database server is not reachable")
		return
	}
	if !exists {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Database Not Found", "database '"+dbName+"' does not exist on this server")
		return
	}
	dbName = canonical

	driver, err := h.dbMgr.DriverFor(r.Context(), dbName)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable", "database '"+dbName+"' is not reachable")
		return
	}

	svc := schema.NewService(driver)
	// Signing in never initializes a database: only databases that already
	// carry the File4Base catalog accept logins. For those, pending catalog
	// migrations are applied (idempotent, creates no accounts).
	if !svc.HasSystemCatalog(r.Context()) {
		telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Failed", schema.ErrInvalidCredentials.Error())
		return
	}
	if err := svc.EnsureSystemTables(r.Context()); err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed preparing the system catalog"))
		return
	}

	user, err := svc.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Failed", err.Error())
		return
	}

	token, sess, err := h.sessions.Create(user.ID, user.Username, user.Role, dbName, user.Stamp)
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed creating session"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"database":   dbName,
		"user":       user,
		"token":      token,
		"token_type": "Bearer",
		"expires_at": sess.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// Logout revokes the caller's session.
func (h *SecurityHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.sessions.Revoke(auth.TokenFromContext(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

// CurrentSession returns the user and database of the caller's session.
func (h *SecurityHandler) CurrentSession(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	perms, err := schemaService(r).GetUserPermissions(r.Context(), sess.UserID)
	if err != nil {
		perms = make([]schema.UserLayoutPermission, 0)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database": sess.Database,
		"user": schema.AuthUser{
			ID:          sess.UserID,
			Username:    sess.Username,
			Role:        sess.Role,
			IsActive:    true,
			Permissions: perms,
		},
		"expires_at": sess.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *SecurityHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := schemaService(r).ListUsers(r.Context())
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
	IsActive *bool  `json:"is_active,omitempty"`
}

func (h *SecurityHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	// Only an owner can mint another owner
	if req.Role == auth.RoleOwner && !currentSession(r).IsOwner() {
		writeForbidden(w, r, "only an owner can create owner accounts")
		return
	}

	svc := schemaService(r)
	var user *schema.UserMetadata
	var err error
	if req.IsActive != nil {
		user, err = svc.CreateUser(r.Context(), req.Username, req.Password, req.Role, *req.IsActive)
	} else {
		user, err = svc.CreateUser(r.Context(), req.Username, req.Password, req.Role)
	}
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
	IsActive *bool  `json:"is_active,omitempty"`
}

func (h *SecurityHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	sess := currentSession(r)
	svc := schemaService(r)

	target, err := svc.GetUser(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Not Found", err.Error())
		return
	}
	if req.Role == "" {
		req.Role = target.Role
	}
	isSelf := sess.UserID == target.ID

	switch {
	case sess.IsOwner():
		// Owners manage every account.
	case sess.IsAdmin():
		// Admins manage admins and users, but can neither touch owner
		// accounts nor promote anyone to owner.
		if target.Role == auth.RoleOwner || req.Role == auth.RoleOwner {
			writeForbidden(w, r, "only an owner can manage owner accounts")
			return
		}
	case isSelf:
		// Regular users may only change their own password.
		if req.Role != target.Role || req.IsActive != nil {
			writeForbidden(w, r, "you can only change your own password")
			return
		}
	default:
		writeForbidden(w, r, "your role does not allow this operation")
		return
	}

	if err := svc.UpdateUser(r.Context(), id, req.Password, req.Role, req.IsActive); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "User Update Error", err.Error())
		return
	}

	// Credentials, role or status changed: the user's other sessions must not
	// outlive the change. The caller keeps their own session when editing themselves.
	deactivated := req.IsActive != nil && !*req.IsActive
	if req.Password != "" || req.Role != target.Role || deactivated {
		keep := ""
		if isSelf && !deactivated && req.Role == target.Role {
			keep = auth.TokenFromContext(r.Context())
		}
		h.sessions.RevokeUser(sess.Database, id, keep)
		if keep != "" {
			// The session the user changed their password from stays valid.
			if stamp, err := svc.CurrentAccountStamp(r.Context(), id); err == nil {
				h.sessions.Restamp(keep, stamp)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":     id,
		"status": "updated",
	})
}

func (h *SecurityHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)
	svc := schemaService(r)

	target, err := svc.GetUser(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Not Found", err.Error())
		return
	}
	if target.ID == sess.UserID {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "User Deletion Error", "you cannot delete your own account")
		return
	}
	if target.Role == auth.RoleOwner && !sess.IsOwner() {
		writeForbidden(w, r, "only an owner can manage owner accounts")
		return
	}

	if err := svc.DeleteUser(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "User Deletion Error", err.Error())
		return
	}
	h.sessions.RevokeUser(sess.Database, id, "")

	w.WriteHeader(http.StatusNoContent)
}

func (h *SecurityHandler) GetUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)
	if !sess.IsAdmin() && sess.UserID != id {
		writeForbidden(w, r, "you can only read your own permissions")
		return
	}

	perms, err := schemaService(r).GetUserPermissions(r.Context(), id)
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

	svc := schemaService(r)
	if _, err := svc.GetUser(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Not Found", err.Error())
		return
	}

	if err := svc.SetUserPermissions(r.Context(), id, req.Permissions); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Permission Update Error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}
