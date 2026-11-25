package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
	"github.com/go-chi/chi/v5"
)

// Session groups (#107).
//
// A solution reads the tables of the solutions its external occurrences are
// mapped to as an account of each, never as its own. Signing in to a
// solution tries the same credentials in each of those solutions, and in the
// ones they read in turn, once each: an account that has them gets a session
// of its own there, linked to the sign-in's session group. One that does not
// is left to the user, who signs in to it with POST /api/v1/auth/link. A
// failed link never fails the sign-in.
//
// Every attempt counts against the lockout of the account it is made on, so
// signing in to one solution cannot be used to guess another's passwords.

// The outcome of linking a solution.
const (
	linkLinked      = "linked"
	linkRefused     = "refused"
	linkMFARequired = "mfa_required"
	linkMissing     = "missing"
	linkThrottled   = "throttled"
)

// maxLinkedSolutions bounds the solutions one sign-in links, however many
// the solutions it reads depend on.
const maxLinkedSolutions = 16

// LinkedSolution is a solution a session group reads, and whether the group
// is signed in to it.
type LinkedSolution struct {
	Database string `json:"database"`
	// References are the file references of the solution reading it that
	// are mapped to it.
	References []string `json:"references,omitempty"`
	Status     string   `json:"status,omitempty"`
	Username   string   `json:"username,omitempty"`
	// Token is the bearer token of the linked session, which reads that
	// solution directly, as that account.
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// linkAttempt is the result of signing in to one solution for a group.
type linkAttempt struct {
	status     string
	retryAfter time.Duration
	err        error
	token      string
	session    auth.Session
	schema     *schema.Service
}

// openSolution resolves a solution by name and prepares its catalog as a
// sign-in does. It returns the canonical name, or a link status saying why
// it cannot be signed in to.
func (h *SecurityHandler) openSolution(ctx context.Context, name string) (string, *schema.Service, string) {
	canonical, exists, err := h.dbMgr.LookupDatabase(ctx, strings.TrimSpace(name))
	if err != nil || !exists {
		return name, nil, linkMissing
	}
	driver, err := h.dbMgr.DriverFor(ctx, canonical)
	if err != nil {
		return canonical, nil, linkMissing
	}
	svc := schema.NewService(driver)
	if !svc.HasSystemCatalog(ctx) || svc.EnsureSystemTables(ctx) != nil {
		return canonical, nil, linkMissing
	}
	return canonical, svc, ""
}

// link signs in to a solution for a group with the credentials given, under
// that account's own lockout, and links the session on success.
func (h *SecurityHandler) link(r *http.Request, group, database, username, password, totp string) linkAttempt {
	ctx := r.Context()
	database, svc, status := h.openSolution(ctx, database)
	if status != "" {
		return linkAttempt{status: status}
	}
	key := loginThrottleKey(database, username, r, h.clientIP)
	if ok, retryAfter := h.throttle.Allow(key); !ok {
		return linkAttempt{status: linkThrottled, retryAfter: retryAfter}
	}
	user, err := svc.Authenticate(ctx, username, password, totp)
	if errors.Is(err, schema.ErrMFARequired) {
		return linkAttempt{status: linkMFARequired, err: err}
	}
	if err != nil {
		if locked, retryAfter := h.throttle.Fail(key); locked {
			return linkAttempt{status: linkThrottled, retryAfter: retryAfter}
		}
		return linkAttempt{status: linkRefused, err: err}
	}
	h.throttle.Succeed(key)
	h.syncProjectSecurity(ctx, database, svc)
	// The account's own standing in that solution: nothing of the account
	// that started the group carries over.
	superOwner := user.SuperOwner || h.isSeededSuperOwner(database, user.Username) || h.isAdminDatabaseOwner(database, user.Role)
	token, sess, err := h.sessions.Link(group, user.ID, user.Username, user.Role, database, user.Stamp, superOwner)
	if err != nil {
		return linkAttempt{status: linkRefused, err: err}
	}
	clientIP, webContainer, userAgent := extractClientSessionMeta(r, h.clientIP)
	h.sessions.TouchMeta(token, clientIP, webContainer, userAgent)
	return linkAttempt{status: linkLinked, token: token, session: sess, schema: svc}
}

// linkDependencies links, after a sign-in to a solution, the solutions it
// reads and the ones those read in turn, each once, with the credentials of
// the sign-in. A solution is followed only once the group is signed in to
// it: the design of one it could not sign in to is not read.
func (h *SecurityHandler) linkDependencies(r *http.Request, group, database string, svc *schema.Service, username, password string) []LinkedSolution {
	visited := map[string]bool{strings.ToLower(database): true}
	queue := []*schema.Service{svc}
	var out []LinkedSolution
	for len(queue) > 0 && len(out) < maxLinkedSolutions {
		deps, err := queue[0].ExternalDependencies(r.Context())
		queue = queue[1:]
		if err != nil {
			continue
		}
		for _, dep := range deps {
			if visited[strings.ToLower(dep.Database)] || len(out) == maxLinkedSolutions {
				continue
			}
			visited[strings.ToLower(dep.Database)] = true
			attempt := h.link(r, group, dep.Database, username, password, "")
			linked := LinkedSolution{Database: dep.Database, References: dep.References, Status: attempt.status}
			if attempt.status == linkLinked {
				linked.Database, linked.Username, linked.Token = attempt.session.Database, attempt.session.Username, attempt.token
				linked.ExpiresAt = attempt.session.ExpiresAt.UTC().Format(time.RFC3339)
				queue = append(queue, attempt.schema)
			}
			out = append(out, linked)
		}
	}
	return out
}

// groupDependencies lists the solutions a session group's solutions read:
// the one it signed in to and every one linked since.
func (h *SecurityHandler) groupDependencies(ctx context.Context, sess auth.Session) map[string][]string {
	sessions := h.sessions.Members(sess.Group)
	if root, ok := h.sessions.Root(sess.Group); ok {
		sessions = append(sessions, root)
	}
	deps := map[string][]string{}
	for _, member := range sessions {
		driver, err := h.dbMgr.DriverFor(ctx, member.Database)
		if err != nil {
			continue
		}
		list, err := schema.NewService(driver).ExternalDependencies(ctx)
		if err != nil {
			continue
		}
		for _, dep := range list {
			key := strings.ToLower(dep.Database)
			deps[key] = append(deps[key], dep.References...)
		}
	}
	return deps
}

// LinkRequest signs in to a solution the caller's solution reads.
type LinkRequest struct {
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	TOTPCode string `json:"totp_code,omitempty"`
}

// Link signs the caller's session group in to a solution its solutions read,
// with credentials of that solution, replacing the session the group held
// there.
//
//	POST /api/v1/auth/link
func (h *SecurityHandler) Link(w http.ResponseWriter, r *http.Request) {
	var req LinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Invalid JSON", "Request body contains invalid JSON format")
		return
	}
	database := strings.TrimSpace(req.Database)
	if database == "" || strings.TrimSpace(req.Username) == "" {
		telemetry.WriteProblem(w, r, http.StatusUnprocessableEntity, "Validation Failed", "database and username are required")
		return
	}
	sess := currentSession(r)
	if strings.EqualFold(database, sess.Database) {
		telemetry.WriteProblem(w, r, http.StatusConflict, "Already Signed In", auth.ErrLinkToRoot.Error())
		return
	}
	references, uses := h.groupDependencies(r.Context(), sess)[strings.ToLower(database)]
	if !uses {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Not A Dependency",
			"no external table occurrence of this session's solutions is mapped to '"+database+"'")
		return
	}

	attempt := h.link(r, sess.Group, database, req.Username, req.Password, req.TOTPCode)
	switch attempt.status {
	case linkMissing:
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Database Not Found", "database '"+database+"' is not available on this server")
		return
	case linkThrottled:
		writeLockedOut(w, r, attempt.retryAfter)
		return
	case linkMFARequired:
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "about:blank", "title": "MFA Required", "status": http.StatusUnauthorized,
			"detail": attempt.err.Error(), "mfa_required": true,
		})
		return
	case linkRefused:
		if errors.Is(attempt.err, auth.ErrGroupClosed) || errors.Is(attempt.err, auth.ErrLinkToRoot) {
			telemetry.WriteProblem(w, r, http.StatusConflict, "Cannot Link", attempt.err.Error())
			return
		}
		telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Failed", attempt.err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(LinkedSolution{
		Database: attempt.session.Database, References: dedupe(references), Status: linkLinked,
		Username: attempt.session.Username, Token: attempt.token,
		ExpiresAt: attempt.session.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// Unlink signs the caller's session group out of a solution it linked.
//
//	DELETE /api/v1/auth/link/{database}
func (h *SecurityHandler) Unlink(w http.ResponseWriter, r *http.Request) {
	if !h.sessions.Unlink(currentSession(r).Group, chi.URLParam(r, "database")) {
		telemetry.WriteProblem(w, r, http.StatusNotFound, "Not Linked", "this session is not linked to that solution")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// linkedSolutions lists the sessions linked to a group, by database. Their
// tokens are not repeated: they were given when each was linked.
func (h *SecurityHandler) linkedSolutions(group string) []LinkedSolution {
	members := h.sessions.Members(group)
	out := make([]LinkedSolution, 0, len(members))
	for _, m := range members {
		out = append(out, LinkedSolution{
			Database: m.Database, Status: linkLinked, Username: m.Username,
			ExpiresAt: m.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Database < out[j].Database })
	return out
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	out := values[:0:0]
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
