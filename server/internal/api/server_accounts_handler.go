package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// maxServerAccountRequestBytes bounds one server-account request body.
const maxServerAccountRequestBytes = 1 << 16

// The server accounts API: identities kept at server level (see
// docs/api/API_REFERENCE.md, "Server accounts"). Every route needs a
// super-owner. A password is accepted on write and never returned.

type createServerAccountRequest struct {
	Name         string   `json:"name"`
	Password     string   `json:"password"`
	Role         string   `json:"role"`
	SuperOwner   bool     `json:"super_owner"`
	AllDatabases bool     `json:"all_databases"`
	Databases    []string `json:"databases"`
}

type setServerAccountActiveRequest struct {
	Active bool `json:"active"`
}

type setServerAccountScopeRequest struct {
	AllDatabases bool     `json:"all_databases"`
	Databases    []string `json:"databases"`
}

type setServerAccountRoleRequest struct {
	Role string `json:"role"`
}

type setServerAccountPasswordRequest struct {
	Password string `json:"password"`
}

// registerServerAccountRoutes mounts /api/v1/server/accounts (super-owners only).
func (h *ServerAdminHandler) registerServerAccountRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(RequireSuperOwner)
		r.Get("/accounts", h.ListServerAccounts)
		r.Post("/accounts", h.CreateServerAccount)
		r.Get("/accounts/{name}", h.GetServerAccount)
		r.Put("/accounts/{name}/active", h.SetServerAccountActive)
		r.Put("/accounts/{name}/scope", h.SetServerAccountScope)
		r.Put("/accounts/{name}/role", h.SetServerAccountRole)
		r.Put("/accounts/{name}/password", h.SetServerAccountPassword)
		r.Delete("/accounts/{name}", h.DeleteServerAccount)
	})
}

// ListServerAccounts answers every server account, ordered by name.
func (h *ServerAdminHandler) ListServerAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := h.controlStore.ListServerAccounts(r.Context())
	if err != nil {
		telemetry.WriteInternalError(w, r, err)
		return
	}
	if accounts == nil {
		accounts = []control.ServerAccount{}
	}
	writeJSON(w, http.StatusOK, accounts)
}

// GetServerAccount answers one server account.
func (h *ServerAdminHandler) GetServerAccount(w http.ResponseWriter, r *http.Request) {
	acc, err := h.controlStore.GetServerAccount(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, acc)
}

// CreateServerAccount adds a server account.
func (h *ServerAdminHandler) CreateServerAccount(w http.ResponseWriter, r *http.Request) {
	var req createServerAccountRequest
	if err := decodeServerAccountBody(w, r, &req); err != nil {
		telemetry.WriteValidationProblem(w, r, "the request body is not valid JSON", nil)
		return
	}
	if !h.checkServerAccountDatabases(w, r, req.Databases) {
		return
	}
	if err := h.controlStore.CreateServerAccount(r.Context(), req.Name, req.Password, req.Role,
		req.SuperOwner, req.AllDatabases, req.Databases); err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	sess := currentSession(r)
	h.recordAudit("server_accounts", "Create Server Account", sess.Username, sess.Database,
		strings.TrimSpace(req.Name), "success", serverAccountScopeText(req.AllDatabases, req.Databases, req.SuperOwner))
	h.writeServerAccount(w, r, req.Name, http.StatusCreated)
}

// SetServerAccountActive enables or disables an account.
func (h *ServerAdminHandler) SetServerAccountActive(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req setServerAccountActiveRequest
	if err := decodeServerAccountBody(w, r, &req); err != nil {
		telemetry.WriteValidationProblem(w, r, "the request body is not valid JSON", nil)
		return
	}
	if err := h.controlStore.SetServerAccountActive(r.Context(), name, req.Active); err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	sess := currentSession(r)
	action := "Disable Server Account"
	if req.Active {
		action = "Enable Server Account"
	}
	h.recordAudit("server_accounts", action, sess.Username, sess.Database, name, "success", "")
	h.writeServerAccount(w, r, name, http.StatusOK)
}

// SetServerAccountRole sets the role an account holds in the databases it
// reaches. It applies from the account's next sign-in in each database.
func (h *ServerAdminHandler) SetServerAccountRole(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req setServerAccountRoleRequest
	if err := decodeServerAccountBody(w, r, &req); err != nil {
		telemetry.WriteValidationProblem(w, r, "the request body is not valid JSON", nil)
		return
	}
	if err := h.controlStore.SetServerAccountRole(r.Context(), name, req.Role); err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	sess := currentSession(r)
	h.recordAudit("server_accounts", "Change Server Account Role", sess.Username, sess.Database,
		name, "success", "role "+req.Role)
	h.writeServerAccount(w, r, name, http.StatusOK)
}

// SetServerAccountScope replaces the databases an account reaches.
func (h *ServerAdminHandler) SetServerAccountScope(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req setServerAccountScopeRequest
	if err := decodeServerAccountBody(w, r, &req); err != nil {
		telemetry.WriteValidationProblem(w, r, "the request body is not valid JSON", nil)
		return
	}
	if !h.checkServerAccountDatabases(w, r, req.Databases) {
		return
	}
	if err := h.controlStore.SetServerAccountScope(r.Context(), name, req.AllDatabases, req.Databases); err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	sess := currentSession(r)
	h.recordAudit("server_accounts", "Change Server Account Scope", sess.Username, sess.Database,
		name, "success", serverAccountScopeText(req.AllDatabases, req.Databases, false))
	h.writeServerAccount(w, r, name, http.StatusOK)
}

// SetServerAccountPassword replaces an account's password. The password is
// never written to the audit log.
func (h *ServerAdminHandler) SetServerAccountPassword(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req setServerAccountPasswordRequest
	if err := decodeServerAccountBody(w, r, &req); err != nil {
		telemetry.WriteValidationProblem(w, r, "the request body is not valid JSON", nil)
		return
	}
	if err := h.controlStore.SetServerAccountPassword(r.Context(), name, req.Password); err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	sess := currentSession(r)
	h.recordAudit("server_accounts", "Set Server Account Password", sess.Username, sess.Database, name, "success", "")
	w.WriteHeader(http.StatusNoContent)
}

// DeleteServerAccount removes an account and the databases it reached.
func (h *ServerAdminHandler) DeleteServerAccount(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.controlStore.DeleteServerAccount(r.Context(), name); err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	sess := currentSession(r)
	h.recordAudit("server_accounts", "Delete Server Account", sess.Username, sess.Database, name, "success", "")
	w.WriteHeader(http.StatusNoContent)
}

// checkServerAccountDatabases answers 422 when a listed database is not a
// registered database, and reports whether the request may go on.
func (h *ServerAdminHandler) checkServerAccountDatabases(w http.ResponseWriter, r *http.Request, databases []string) bool {
	for _, db := range databases {
		name := strings.TrimSpace(db)
		if name == "" {
			continue
		}
		_, err := h.controlStore.GetProject(r.Context(), name)
		if errors.Is(err, control.ErrProjectNotFound) {
			telemetry.WriteValidationProblem(w, r, fmt.Sprintf("no database is named %q", name),
				[]telemetry.InvalidParam{{Name: "databases", Reason: "not a registered database"}})
			return false
		}
		if err != nil {
			telemetry.WriteInternalError(w, r, err)
			return false
		}
	}
	return true
}

// writeServerAccount answers the account named name, with the given status.
func (h *ServerAdminHandler) writeServerAccount(w http.ResponseWriter, r *http.Request, name string, status int) {
	acc, err := h.controlStore.GetServerAccount(r.Context(), name)
	if err != nil {
		writeServerAccountError(w, r, err)
		return
	}
	writeJSON(w, status, acc)
}

func decodeServerAccountBody(w http.ResponseWriter, r *http.Request, dst any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxServerAccountRequestBytes)).Decode(dst)
}

// writeServerAccountError maps a control-store error to its problem response.
func writeServerAccountError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, control.ErrServerAccountNotFound):
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Server account not found",
			"no server account has that name")
	case errors.Is(err, control.ErrServerAccountExists):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Name already in use",
			"a server account already has that name, ignoring case")
	case errors.Is(err, control.ErrLastSuperOwner):
		telemetry.WriteProblem(w, r, http.StatusConflict, "Last super-owner",
			control.ErrLastSuperOwner.Error())
	case errors.Is(err, control.ErrInvalidServerAccountName):
		telemetry.WriteValidationProblem(w, r, err.Error(),
			[]telemetry.InvalidParam{{Name: "name", Reason: "no @, no whitespace, not empty, at most 128 characters"}})
	case errors.Is(err, control.ErrInvalidServerAccount):
		telemetry.WriteValidationProblem(w, r, err.Error(), nil)
	default:
		telemetry.WriteInternalError(w, r, err)
	}
}

// serverAccountScopeText describes a scope for the audit log. It names
// databases, never a password.
func serverAccountScopeText(allDatabases bool, databases []string, superOwner bool) string {
	scope := "all databases"
	if !allDatabases {
		scope = "databases " + strings.Join(databases, ", ")
	}
	if superOwner {
		scope += ", super-owner"
	}
	return scope
}
