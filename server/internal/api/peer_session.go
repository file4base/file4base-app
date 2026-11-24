package api

import (
	"net/http"
	"strings"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/schema"
	"github.com/file4base/file4base-app/server/internal/telemetry"
)

// peerPurpose words the refusals of resolvePeerSession for the request that
// reaches another solution.
type peerPurpose struct {
	what           string // "the destination of this transfer"
	unavailable    string // problem title when the server cannot authorize it
	unavailableWhy string
}

// resolvePeerSession authorizes a request that reaches another solution from
// that solution's own session token (#96, #107). The token has to be a live
// session of that very database, whose account is still in the state it
// signed in with: a session of any other solution is refused, whatever role
// it holds, and the server never derives authority for one solution from a
// session opened on another.
func resolvePeerSession(
	w http.ResponseWriter, r *http.Request, dbMgr *dbal.MultiDatabaseManager, sessions *auth.Store,
	token, database string, purpose peerPurpose,
) (transferEnd, bool) {
	var end transferEnd
	if token == "" {
		telemetry.WriteProblem(w, r, http.StatusBadRequest, "Peer Token Required",
			purpose.what+" is the solution "+database+
				", so the request needs a peer_token: a session token obtained by signing in to it")
		return end, false
	}
	if sessions == nil || dbMgr == nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, purpose.unavailable, purpose.unavailableWhy)
		return end, false
	}

	sess, ok := sessions.Get(token)
	if !ok {
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Peer Token Rejected",
			"the peer_token is not a valid session; sign in to "+database+" again")
		return end, false
	}
	if !strings.EqualFold(sess.Database, database) {
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Peer Token Rejected",
			"the peer_token is a session of another solution; "+purpose.what+" is "+database)
		return end, false
	}

	driver, err := dbMgr.DriverFor(r.Context(), sess.Database)
	if err != nil {
		telemetry.WriteProblem(w, r, http.StatusServiceUnavailable, "Database Unavailable",
			"the solution "+database+" is not reachable")
		return end, false
	}

	// The peer account must still be in the state its session was opened
	// with, the same check every authenticated request makes. Without it a
	// revoked or demoted account could still act through another solution.
	stamp, err := schema.NewService(driver).CurrentAccountStamp(r.Context(), sess.UserID)
	if err != nil || stamp != sess.Stamp {
		sessions.Revoke(token)
		telemetry.WriteProblem(w, r, http.StatusForbidden, "Peer Token Rejected",
			"the account of the peer_token is no longer in the state it signed in with; "+
				"sign in to "+database+" again")
		return end, false
	}

	return transferEnd{database: sess.Database, driver: driver, session: sess}, true
}
