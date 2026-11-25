package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/control"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// isServerAccountName reports whether a sign-in name refers to a server
// account. A name with "@" is a directory account and never one of these.
func (h *SecurityHandler) isServerAccountName(ctx context.Context, username string) bool {
	if h.controlStore == nil || strings.Contains(username, "@") {
		return false
	}
	_, err := h.controlStore.GetServerAccount(ctx, username)
	return err == nil
}

// serverAccountReaches reports whether a server account may sign in to a database.
func serverAccountReaches(acc *control.ServerAccount, database string) bool {
	if acc.AllDatabases {
		return true
	}
	for _, db := range acc.Databases {
		if strings.EqualFold(db, database) {
			return true
		}
	}
	return false
}

// signInServerAccount signs a server account in to one database. The account
// is checked against the server, then a row standing for it is kept in the
// database, and the session is opened on that row. Every failure a caller
// could use to learn about other databases or accounts answers the same way
// as a wrong password.
func (h *SecurityHandler) signInServerAccount(w http.ResponseWriter, r *http.Request, dbName string, driver dbal.DatabaseDriver, svc *schema.Service, throttleKey, username, password string) {
	ctx := r.Context()

	acc, err := h.controlStore.AuthenticateServerAccount(ctx, username, password)
	if err != nil && !errors.Is(err, control.ErrInvalidCredentials) {
		telemetry.WriteInternalError(w, r, errors.New("failed checking the server account"))
		return
	}
	if err != nil || !serverAccountReaches(acc, dbName) {
		if locked, retryAfter := h.throttle.Fail(throttleKey); locked {
			writeLockedOut(w, r, retryAfter)
			return
		}
		telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Failed", control.ErrInvalidCredentials.Error())
		return
	}

	if driver.Dialect().Engine() != dbal.EnginePostgres {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed",
			"server accounts sign in to PostgreSQL databases only")
		return
	}

	h.throttle.Succeed(throttleKey)
	h.syncProjectSecurity(ctx, dbName, svc)

	userID, err := svc.EnsureServerAccountMirror(ctx, acc.Name, acc.Role, acc.SuperOwner)
	if errors.Is(err, schema.ErrServerAccountNameTaken) {
		telemetry.WriteProblem(w, r, http.StatusConflict, "Name Already Used",
			"this database already has a database account named '"+acc.Name+"'; rename one of them to sign in with the server account")
		return
	}
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed preparing the server account in this database"))
		return
	}

	stamp, err := h.controlStore.ServerAccountSessionStamp(ctx, acc.Name, dbName)
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed reading the server account"))
		return
	}
	token, sess, err := h.sessions.CreateForServerAccount(userID, acc.Name, acc.Role, dbName, stamp, acc.SuperOwner, acc.Name)
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed creating session"))
		return
	}
	clientIP, webContainer, userAgent := extractClientSessionMeta(r, h.clientIP)
	h.sessions.TouchMeta(token, clientIP, webContainer, userAgent)

	capabilities, err := svc.CapabilitiesOf(ctx, acc.Role)
	if err != nil {
		telemetry.WriteInternalError(w, r, errors.New("failed reading role privileges"))
		return
	}

	resp := map[string]interface{}{
		"status":   "ok",
		"database": dbName,
		"user": map[string]interface{}{
			"id":             userID,
			"username":       acc.Name,
			"role":           acc.Role,
			"super_owner":    acc.SuperOwner,
			"server_account": true,
		},
		"token":        token,
		"token_type":   "Bearer",
		"capabilities": capabilities,
		"expires_at":   sess.ExpiresAt.UTC().Format(time.RFC3339),
	}
	h.addSignInProjectFields(ctx, dbName, resp)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// addSignInProjectFields adds the solution's name, file options and page setup to a sign-in answer.
func (h *SecurityHandler) addSignInProjectFields(ctx context.Context, dbName string, resp map[string]interface{}) {
	if h.controlStore == nil {
		return
	}
	proj, err := h.controlStore.GetProject(ctx, dbName)
	if err != nil || proj == nil {
		return
	}
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
