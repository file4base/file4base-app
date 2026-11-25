package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/data"
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

// groupPeers opens the other solutions of a request's session group (#107)
// for the reads that go through external table occurrences. Each solution
// is read as the account the group holds there, with that account's
// privileges: a session of this solution never reads another one, whatever
// role it holds. What it opens is kept for the request only.
type groupPeers struct {
	sessions *auth.Store
	dbMgr    *dbal.MultiDatabaseManager
	group    string

	mu   sync.Mutex
	ends map[string]*peerEnd
}

// peerEnd is the session a group holds in a solution and that solution's
// driver.
type peerEnd struct {
	driver  dbal.DatabaseDriver
	session auth.Session
}

func newGroupPeers(sessions *auth.Store, dbMgr *dbal.MultiDatabaseManager, group string) *groupPeers {
	return &groupPeers{sessions: sessions, dbMgr: dbMgr, group: group, ends: map[string]*peerEnd{}}
}

// end is the group's session in a solution, checked as every authenticated
// request checks its own: a member whose account changed since it signed in
// is revoked and reads as not signed in.
func (p *groupPeers) end(ctx context.Context, database string) (*peerEnd, error) {
	key := strings.ToLower(database)
	p.mu.Lock()
	defer p.mu.Unlock()
	if end, ok := p.ends[key]; ok {
		return end, nil
	}
	unavailable := func(reason string) error {
		return &data.ExternalUnavailableError{Database: database, Reason: reason}
	}
	if p.sessions == nil || p.dbMgr == nil {
		return nil, unavailable(data.ExternalMissing)
	}
	sess, ok := p.sessions.Member(p.group, database)
	if !ok {
		return nil, unavailable(data.ExternalNotSignedIn)
	}
	driver, err := p.dbMgr.DriverFor(ctx, sess.Database)
	if err != nil {
		return nil, unavailable(data.ExternalMissing)
	}
	stamp, err := schema.NewService(driver).CurrentAccountStamp(ctx, sess.UserID)
	if err != nil && !errors.Is(err, schema.ErrAccountNotFound) {
		return nil, fmt.Errorf("the account signed in to %s could not be checked: %w", database, err)
	}
	if err != nil || stamp != sess.Stamp {
		p.sessions.Unlink(p.group, sess.Database)
		return nil, unavailable(data.ExternalNotSignedIn)
	}
	end := &peerEnd{driver: driver, session: sess}
	p.ends[key] = end
	return end, nil
}

// Data implements data.Peers.
func (p *groupPeers) Data(ctx context.Context, database string) (*data.Service, error) {
	end, err := p.end(ctx, database)
	if err != nil {
		return nil, err
	}
	return dataServiceAs(end.driver, end.session), nil
}

type peersKey struct{}

func withPeers(ctx context.Context, peers *groupPeers) context.Context {
	return context.WithValue(ctx, peersKey{}, peers)
}

// peersOf is the request's session group, set by AuthMiddleware.
func peersOf(r *http.Request) *groupPeers {
	peers, _ := r.Context().Value(peersKey{}).(*groupPeers)
	return peers
}
