package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// AuthMiddleware resolves the caller's session from the Authorization header
// and binds the request to the session's database.
type AuthMiddleware struct {
	sessions *auth.Store
	dbMgr    *dbal.MultiDatabaseManager
}

// NewAuthMiddleware creates the session-resolving middleware.
func NewAuthMiddleware(sessions *auth.Store, dbMgr *dbal.MultiDatabaseManager) *AuthMiddleware {
	return &AuthMiddleware{sessions: sessions, dbMgr: dbMgr}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// requestedDatabase returns the database a client explicitly named on the request, if any.
func requestedDatabase(r *http.Request) string {
	if name := strings.TrimSpace(r.URL.Query().Get("database")); name != "" {
		return name
	}
	return strings.TrimSpace(r.Header.Get("X-Database-Name"))
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, detail string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="file4base"`)
	telemetry.WriteProblem(w, r, http.StatusUnauthorized, "Authentication Required", detail)
}

// attach loads the session for the request's bearer token into the context.
// It returns the request unchanged (ok=false) when there is no valid session.
func (m *AuthMiddleware) attach(w http.ResponseWriter, r *http.Request) (*http.Request, bool, bool) {
	token := bearerToken(r)
	if token == "" {
		return r, false, false
	}
	sess, ok := m.sessions.Get(token)
	if !ok {
		return r, false, false
	}

	driver, err := m.dbMgr.DriverFor(r.Context(), sess.Database)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable",
			"the database bound to this session is not reachable")
		return r, false, true
	}

	// The account must still be in the state the session was opened with:
	// this rejects sessions of deleted, disabled, demoted or re-passworded
	// accounts, including one created by a sign-in that raced a revocation.
	stamp, err := schema.NewService(driver).CurrentAccountStamp(r.Context(), sess.UserID)
	if err != nil && !errors.Is(err, schema.ErrAccountNotFound) {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable",
			"the account of this session could not be checked")
		return r, false, true
	}
	if err != nil || stamp != sess.Stamp {
		m.sessions.Revoke(token)
		return r, false, false
	}

	ctx := auth.WithSession(r.Context(), sess)
	ctx = auth.WithToken(ctx, token)
	ctx = dbal.WithDriver(ctx, driver)
	return r.WithContext(ctx), true, false
}

// Authenticate rejects requests without a valid session. On success the
// request context carries the session and the driver of the session's
// database; a request can only ever reach the database it signed in to.
func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed, ok, handled := m.attach(w, r)
		if handled {
			return
		}
		if !ok {
			writeUnauthorized(w, r, "a valid session token is required; sign in via POST /api/v1/auth/login")
			return
		}

		sess, _ := auth.FromContext(authed.Context())
		if requested := requestedDatabase(authed); requested != "" && !strings.EqualFold(requested, sess.Database) {
			telemetry.WriteProblem(w, r, http.StatusForbidden, "Forbidden",
				"this session is bound to another database; sign in to the requested database first")
			return
		}

		next.ServeHTTP(w, authed)
	})
}

// Optional attaches the session when a valid token is present and lets
// anonymous requests through. Used by the few public endpoints whose response
// is merely enriched for signed-in callers.
func (m *AuthMiddleware) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authed, _, handled := m.attach(w, r)
		if handled {
			return
		}
		next.ServeHTTP(w, authed)
	})
}

// RequireSession is the deny-by-default guard every protected route group
// installs: without a session and a request-scoped database driver in the
// context (both set by AuthMiddleware.Authenticate) the request is rejected.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.FromContext(r.Context()); !ok {
			writeUnauthorized(w, r, "a valid session token is required; sign in via POST /api/v1/auth/login")
			return
		}
		if _, ok := dbal.DriverFromContext(r.Context()); !ok {
			writeUnauthorized(w, r, "the session is not bound to a database")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole only lets sessions holding one of the given roles through.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := auth.FromContext(r.Context())
			if !ok {
				writeUnauthorized(w, r, "a valid session token is required; sign in via POST /api/v1/auth/login")
				return
			}
			for _, role := range roles {
				if sess.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeForbidden(w, r, "your role does not allow this operation")
		})
	}
}

// RequireAdmin lets owners and admins through.
func RequireAdmin(next http.Handler) http.Handler {
	return RequireRole(auth.RoleOwner, auth.RoleAdmin)(next)
}

// RequireOwner lets only owners through.
func RequireOwner(next http.Handler) http.Handler {
	return RequireRole(auth.RoleOwner)(next)
}

func writeForbidden(w http.ResponseWriter, r *http.Request, detail string) {
	telemetry.WriteProblem(w, r, http.StatusForbidden, "Forbidden", detail)
}

// schemaService returns a schema service bound to the request's database.
// Callers sit behind RequireSession, which guarantees the driver is present.
func schemaService(r *http.Request) *schema.Service {
	driver, _ := dbal.DriverFromContext(r.Context())
	return schema.NewService(driver)
}

// currentSession returns the session of a request that passed RequireSession.
func currentSession(r *http.Request) auth.Session {
	sess, _ := auth.FromContext(r.Context())
	return sess
}
