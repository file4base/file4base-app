package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/clientip"
	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// SecurityHandler serves authentication (login, logout, session) and the
// management of user accounts and layout/table permissions.
type SecurityHandler struct {
	dbMgr        *dbal.MultiDatabaseManager
	sessions     *auth.Store
	controlStore *control.Store
	throttle     *auth.LoginThrottle
	// clientIP resolves the caller's address for the audit log, session
	// metadata and the login-throttle key, honouring forwarding headers only
	// from trusted proxies. A nil resolver falls back to the direct peer.
	clientIP *clientip.Resolver
	// superOwnerSeed bootstraps super-ownership for accounts named by
	// FILE4BASE_SUPER_OWNER. Keys are "database\x00username", both folded to
	// lower case. A seeded account signs in as a super-owner even before its
	// stored super_owner flag is set, so the first super-owner can exist
	// without another one to grant it.
	superOwnerSeed map[string]bool
}

// ConfigureSuperOwner records the FILE4BASE_SUPER_OWNER bootstrap seeds. Each
// entry is "database:username"; blank or malformed entries are ignored.
func (h *SecurityHandler) ConfigureSuperOwner(entries []string) {
	seed := make(map[string]bool, len(entries))
	for _, raw := range entries {
		db, user, ok := strings.Cut(raw, ":")
		db, user = strings.TrimSpace(db), strings.TrimSpace(user)
		if !ok || db == "" || user == "" {
			continue
		}
		seed[superOwnerSeedKey(db, user)] = true
	}
	h.superOwnerSeed = seed
}

func superOwnerSeedKey(database, username string) string {
	return strings.ToLower(strings.TrimSpace(database)) + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

// isSeededSuperOwner reports whether an account is a bootstrap super-owner.
func (h *SecurityHandler) isSeededSuperOwner(database, username string) bool {
	return h.superOwnerSeed[superOwnerSeedKey(database, username)]
}

// DefaultLoginThrottle locks a database+username+client out for 15 minutes
// after 10 failed password checks in a row (a 15-minute window forgets
// earlier failures). FILE4BASE_LOGIN_MAX_ATTEMPTS=0 disables it.
func DefaultLoginThrottle() *auth.LoginThrottle {
	max := envInt("FILE4BASE_LOGIN_MAX_ATTEMPTS", 10)
	window := time.Duration(envInt("FILE4BASE_LOGIN_WINDOW_SECONDS", 900)) * time.Second
	lockout := time.Duration(envInt("FILE4BASE_LOGIN_LOCKOUT_SECONDS", 900)) * time.Second
	return auth.NewLoginThrottle(max, window, lockout)
}

func envInt(name string, def int) int {
	if raw := strings.TrimSpace(os.Getenv(name)); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 0 {
			return v
		}
	}
	return def
}

// loginThrottleKey identifies who is trying to authenticate: the account
// (database + username) and where from (client address). Keying on both
// means one attacker cannot lock a victim out of their own account from
// elsewhere, while still capping guesses per account and per source.
func loginThrottleKey(database, username string, r *http.Request, resolver *clientip.Resolver) string {
	ip, _, _ := extractClientSessionMeta(r, resolver)
	return strings.ToLower(strings.TrimSpace(database)) + "\x00" +
		strings.ToLower(strings.TrimSpace(username)) + "\x00" + ip
}

// writeLockedOut refuses an attempt that is currently locked out.
func writeLockedOut(w http.ResponseWriter, r *http.Request, retryAfter time.Duration) {
	secs := int(retryAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	telemetry.WriteProblem(w, r, http.StatusTooManyRequests, "Too Many Attempts",
		fmt.Sprintf("too many failed attempts; try again in %d seconds", secs))
}

func NewSecurityHandler(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store) *SecurityHandler {
	return &SecurityHandler{
		dbMgr:    dbMgr,
		sessions: sessions,
	}
}

func NewSecurityHandlerWithControl(dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store, controlStore *control.Store) *SecurityHandler {
	return &SecurityHandler{
		dbMgr:        dbMgr,
		sessions:     sessions,
		controlStore: controlStore,
	}
}

func (h *SecurityHandler) syncProjectSecurity(ctx context.Context, dbName string, svc *schema.Service) {
	if h.controlStore == nil || svc == nil || strings.TrimSpace(dbName) == "" {
		return
	}
	if meta, err := svc.GetSolutionMeta(ctx, dbName); err == nil {
		_ = h.controlStore.UpsertProject(ctx, control.ProjectRecord{
			Database:     dbName,
			Name:         meta.SolutionName,
			Engine:       string(h.dbMgr.Engine()),
			DesignLocked: meta.DesignLocked,
		})
	}
	users, err := svc.ListUsers(ctx)
	if err != nil {
		return
	}
	uRecs := make([]control.UserRecord, 0, len(users))
	for _, u := range users {
		uRecs = append(uRecs, control.UserRecord{
			ID:       u.ID,
			Username: u.Username,
			Role:     u.Role,
			IsActive: u.IsActive,
		})
		if lPerms, err := svc.GetUserPermissions(ctx, u.ID); err == nil {
			lpRecs := make([]control.LayoutPermissionRecord, 0, len(lPerms))
			for _, lp := range lPerms {
				lpRecs = append(lpRecs, control.LayoutPermissionRecord{
					UserID:      u.ID,
					LayoutID:    lp.LayoutID,
					LayoutName:  lp.LayoutName,
					AccessLevel: lp.AccessLevel,
				})
			}
			_ = h.controlStore.SetLayoutPermissions(ctx, dbName, u.ID, lpRecs)
		}
		if tPerms, err := svc.GetUserTablePermissions(ctx, u.ID); err == nil {
			tpRecs := make([]control.TablePermissionRecord, 0, len(tPerms))
			for _, tp := range tPerms {
				tpRecs = append(tpRecs, control.TablePermissionRecord{
					UserID:          u.ID,
					TableName:       tp.TableName,
					AccessLevel:     tp.AccessLevel,
					CanCreate:       tp.CanCreate,
					CanEdit:         tp.CanEdit,
					CanDelete:       tp.CanDelete,
					CanModifySchema: tp.CanModifySchema,
				})
			}
			_ = h.controlStore.SetTablePermissions(ctx, dbName, u.ID, tpRecs)
		}
	}
	_ = h.controlStore.SyncUsers(ctx, dbName, uRecs)
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
		// Super-ownership is a server-wide privilege, so only a super-owner may
		// grant or revoke it (never a mere database owner or admin).
		r.With(RequireSuperOwner).Put("/users/{id}/super-owner", h.SetSuperOwner)
		r.With(RequireAdmin).Delete("/users/{id}", h.DeleteUser)
		r.Get("/users/{id}/permissions", h.GetUserPermissions) // admins, or the user themselves
		r.With(RequireAdmin).Put("/users/{id}/permissions", h.SetUserPermissions)
		r.Get("/users/{id}/table-permissions", h.GetUserTablePermissions) // admins, or the user themselves
		r.With(RequireAdmin).Put("/users/{id}/table-permissions", h.SetUserTablePermissions)
		r.Post("/users/{id}/mfa/setup", h.SetupUserMFA)
		r.Post("/users/{id}/mfa/enable", h.EnableUserMFA)
		r.Post("/users/{id}/mfa/disable", h.DisableUserMFA)
		r.With(RequireAdmin).Get("/privileges", h.GetRolePrivileges)
		// Granting a capability to a role is a change to the security
		// policy of the database, so only an owner may make it.
		r.With(RequireOwner).Put("/privileges", h.SetRolePrivileges)
	})
}

type LoginRequest struct {
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
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

	throttleKey := loginThrottleKey(dbName, req.Username, r, h.clientIP)
	if ok, retryAfter := h.throttle.Allow(throttleKey); !ok {
		writeLockedOut(w, r, retryAfter)
		return
	}

	user, err := svc.Authenticate(r.Context(), req.Username, req.Password, req.TOTPCode)
	if err != nil {
		// A missing second factor is not a wrong credential: the first factor
		// was right, so it does not count against the limit.
		if !errors.Is(err, schema.ErrMFARequired) {
			if locked, retryAfter := h.throttle.Fail(throttleKey); locked {
				writeLockedOut(w, r, retryAfter)
				return
			}
		}
		if errors.Is(err, schema.ErrMFARequired) {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"type":         "about:blank",
				"title":        "MFA Required",
				"status":       http.StatusUnauthorized,
				"detail":       err.Error(),
				"mfa_required": true,
			})
			return
		}
		telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Failed", err.Error())
		return
	}

	h.throttle.Succeed(throttleKey)
	h.syncProjectSecurity(r.Context(), dbName, svc)

	// Effective super-ownership combines the account's stored flag with the
	// bootstrap seed, so the response and the session agree on what the caller
	// may do before any grant has been persisted.
	user.SuperOwner = user.SuperOwner || h.isSeededSuperOwner(dbName, user.Username)

	token, sess, err := h.sessions.Create(user.ID, user.Username, user.Role, dbName, user.Stamp, user.SuperOwner)
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed creating session"))
		return
	}
	clientIP, webContainer, userAgent := extractClientSessionMeta(r, h.clientIP)
	h.sessions.TouchMeta(token, clientIP, webContainer, userAgent)

	// The capabilities of the account's role travel with the sign-in so a
	// client can tell a user what they may do before they try it. They are
	// a courtesy: the server enforces them on every call (#39).
	capabilities, err := svc.CapabilitiesOf(r.Context(), user.Role)
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed reading role privileges"))
		return
	}

	resp := map[string]interface{}{
		"status":       "ok",
		"database":     dbName,
		"user":         user,
		"token":        token,
		"token_type":   "Bearer",
		"capabilities": capabilities,
		"expires_at":   sess.ExpiresAt.UTC().Format(time.RFC3339),
	}
	if h.controlStore != nil {
		if proj, err := h.controlStore.GetProject(r.Context(), dbName); err == nil && proj != nil {
			if proj.Name != "" {
				resp["solution_name"] = proj.Name
			}
			if len(proj.FileOptions) > 0 {
				resp["file_options"] = proj.FileOptions
			}
			if len(proj.PageSetup) > 0 {
				resp["page_setup"] = proj.PageSetup
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// Logout revokes the caller's session.
func (h *SecurityHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.sessions.Revoke(auth.TokenFromContext(r.Context()))
	w.WriteHeader(http.StatusNoContent)
}

// CurrentSession returns the user and database of the caller's session.
func (h *SecurityHandler) CurrentSession(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	svc := schemaService(r)
	perms, err := svc.GetUserPermissions(r.Context(), sess.UserID)
	if err != nil {
		perms = make([]schema.UserLayoutPermission, 0)
	}
	tablePerms, err := svc.GetUserTablePermissions(r.Context(), sess.UserID)
	if err != nil {
		tablePerms = make([]schema.UserTablePermission, 0)
	}

	capabilities, err := svc.CapabilitiesOf(r.Context(), sess.Role)
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"database":     sess.Database,
		"capabilities": capabilities,
		"user": schema.AuthUser{
			ID:               sess.UserID,
			Username:         sess.Username,
			Role:             sess.Role,
			IsActive:         true,
			SuperOwner:       sess.SuperOwner,
			Permissions:      perms,
			TablePermissions: tablePerms,
		},
		"expires_at": sess.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *SecurityHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	svc := schemaService(r)
	users, err := svc.ListUsers(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	h.syncProjectSecurity(r.Context(), currentSession(r).Database, svc)

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
	h.syncProjectSecurity(r.Context(), currentSession(r).Database, svc)

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
	h.syncProjectSecurity(r.Context(), sess.Database, svc)

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

type SetSuperOwnerRequest struct {
	SuperOwner bool `json:"super_owner"`
}

// SetSuperOwner grants or revokes the super-owner privilege of an account in
// the caller's database. Only a super-owner may call it (RequireSuperOwner),
// the privilege can only sit on an owner account, and a super-owner may not
// strip their own privilege (so the population cannot lock itself out by
// accident). The target's other sessions are revoked so the change takes hold
// at once.
func (h *SecurityHandler) SetSuperOwner(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req SetSuperOwnerRequest
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
	if req.SuperOwner && target.Role != auth.RoleOwner {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Owner Required",
			"super-owner can only be granted to an owner account")
		return
	}
	if sess.UserID == target.ID && !req.SuperOwner {
		writeForbidden(w, r, "you cannot remove your own super-owner privilege; ask another super-owner")
		return
	}

	if err := svc.SetSuperOwner(r.Context(), id, req.SuperOwner); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Super-Owner Update Error", err.Error())
		return
	}
	h.syncProjectSecurity(r.Context(), sess.Database, svc)
	// The target's privilege changed; drop their existing sessions so it is
	// re-evaluated on the next sign-in.
	h.sessions.RevokeUser(sess.Database, id, "")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          id,
		"super_owner": req.SuperOwner,
		"status":      "updated",
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
	h.syncProjectSecurity(r.Context(), sess.Database, svc)

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
	h.syncProjectSecurity(r.Context(), currentSession(r).Database, svc)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *SecurityHandler) GetUserTablePermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess := currentSession(r)
	if !sess.IsAdmin() && sess.UserID != id {
		writeForbidden(w, r, "you can only read your own permissions")
		return
	}

	perms, err := schemaService(r).GetUserTablePermissions(r.Context(), id)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Table Permissions Not Found", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(perms)
}

type SetTablePermissionsRequest struct {
	Permissions []schema.UserTablePermission `json:"permissions"`
}

func (h *SecurityHandler) SetUserTablePermissions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req SetTablePermissionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}

	svc := schemaService(r)
	if _, err := svc.GetUser(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Not Found", err.Error())
		return
	}

	if err := svc.SetUserTablePermissions(r.Context(), id, req.Permissions); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Table Permission Update Error", err.Error())
		return
	}
	h.syncProjectSecurity(r.Context(), currentSession(r).Database, svc)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

type rolePrivilegesRequest struct {
	Privileges []schema.RolePrivileges `json:"privileges"`
}

// GetRolePrivileges returns the capabilities each account role holds (#39).
func (h *SecurityHandler) GetRolePrivileges(w http.ResponseWriter, r *http.Request) {
	privileges, err := schemaService(r).ListRolePrivileges(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"capabilities": schema.Capabilities(),
		"privileges":   privileges,
	})
}

// SetRolePrivileges replaces the capabilities of the roles in the request.
// The owner's entry is stored fully granted whatever it says: an owner must
// not be able to lock themselves out of their own database.
func (h *SecurityHandler) SetRolePrivileges(w http.ResponseWriter, r *http.Request) {
	var req rolePrivilegesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid Request", "the request body is not valid JSON")
		return
	}
	if err := schemaService(r).SetRolePrivileges(r.Context(), req.Privileges); err != nil {
		if errors.Is(err, schema.ErrUnknownRole) {
			telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Unknown Role", err.Error())
			return
		}
		telemetry.WriteInternalError(w, r, err)
		return
	}
	h.GetRolePrivileges(w, r)
}

func (h *SecurityHandler) canManageUserMFA(w http.ResponseWriter, r *http.Request, targetID string) (*schema.UserMetadata, bool) {
	sess := currentSession(r)
	svc := schemaService(r)
	target, err := svc.GetUser(r.Context(), targetID)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "User Not Found", err.Error())
		return nil, false
	}
	isSelf := sess.UserID == target.ID
	switch {
	case sess.IsOwner():
		return target, true
	case sess.IsAdmin():
		if target.Role == auth.RoleOwner {
			writeForbidden(w, r, "only an owner can manage owner accounts")
			return nil, false
		}
		return target, true
	case isSelf:
		return target, true
	default:
		writeForbidden(w, r, "your role does not allow this operation")
		return nil, false
	}
}

// SetupUserMFA generates a TOTP secret and otpauth:// URI for Google Authenticator.
func (h *SecurityHandler) SetupUserMFA(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.canManageUserMFA(w, r, id); !ok {
		return
	}
	svc := schemaService(r)
	issuer := "File4Base (" + currentSession(r).Database + ")"
	resp, err := svc.SetupUserMFA(r.Context(), id, issuer)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "MFA Setup Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

type enableMFARequest struct {
	Code   string `json:"code"`
	Secret string `json:"secret,omitempty"`
}

// EnableUserMFA verifies a 6-digit TOTP code and enables MFA on the account.
func (h *SecurityHandler) EnableUserMFA(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.canManageUserMFA(w, r, id); !ok {
		return
	}
	var req enableMFARequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	svc := schemaService(r)
	if err := svc.EnableUserMFA(r.Context(), id, req.Code, req.Secret); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "MFA Verification Failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          id,
		"mfa_enabled": true,
		"status":      "enabled",
	})
}

// DisableUserMFA turns off MFA and clears the TOTP secret on the account.
func (h *SecurityHandler) DisableUserMFA(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.canManageUserMFA(w, r, id); !ok {
		return
	}
	svc := schemaService(r)
	if err := svc.DisableUserMFA(r.Context(), id); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "MFA Disable Error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          id,
		"mfa_enabled": false,
		"status":      "disabled",
	})
}
