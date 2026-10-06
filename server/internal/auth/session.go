// Package auth provides server-side session management for the File4Base API.
//
// Sessions are opaque bearer tokens issued by POST /api/v1/auth/login. Each
// session is bound to exactly one database and one user, so the database a
// request operates on is always derived from the caller's session and never
// from shared server state.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Role names stored in sys_users.role.
const (
	RoleOwner = "owner"
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// DefaultSessionTTL is the idle lifetime of a session when none is configured.
const DefaultSessionTTL = 12 * time.Hour

// Session describes an authenticated caller.
type Session struct {
	UserID    string
	Username  string
	Role      string
	Database  string
	CreatedAt time.Time
	ExpiresAt time.Time
	// Stamp is the account state the session was opened with. Requests are
	// rejected once the account's current stamp differs (password, role or
	// active state changed, account deleted), even for a session created
	// after a revocation by a sign-in that was already in flight.
	Stamp string
}

// IsOwner reports whether the session belongs to a database owner.
func (s Session) IsOwner() bool { return s.Role == RoleOwner }

// IsAdmin reports whether the session has administrative rights (owner or admin).
func (s Session) IsAdmin() bool { return s.Role == RoleOwner || s.Role == RoleAdmin }

// Store is an in-memory session registry safe for concurrent use.
//
// Only the SHA-256 digest of each token is kept, so a memory dump of the
// store does not reveal usable bearer tokens. Sessions do not survive a
// server restart; clients simply sign in again.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
	ttl      time.Duration
	now      func() time.Time
}

// NewStore creates a session store. A non-positive ttl selects DefaultSessionTTL.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &Store{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		now:      time.Now,
	}
}

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create registers a new session and returns its bearer token.
func (s *Store) Create(userID, username, role, database, stamp string) (string, Session, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", Session{}, err
	}
	token := hex.EncodeToString(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.pruneLocked(now)

	sess := &Session{
		UserID:    userID,
		Username:  username,
		Role:      role,
		Database:  database,
		Stamp:     stamp,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}
	s.sessions[digest(token)] = sess
	return token, *sess, nil
}

// Get returns the session for a token and extends its idle lifetime.
func (s *Store) Get(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	key := digest(token)

	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[key]
	if !ok {
		return Session{}, false
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) {
		delete(s.sessions, key)
		return Session{}, false
	}
	sess.ExpiresAt = now.Add(s.ttl)
	return *sess, true
}

// Restamp records a new account stamp for one session, used when a user
// changes their own password and keeps the session they changed it from.
func (s *Store) Restamp(token, stamp string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[digest(token)]; ok {
		sess.Stamp = stamp
	}
}

// Revoke invalidates a single token.
func (s *Store) Revoke(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, digest(token))
}

// RevokeUser invalidates every session of a user in a database, optionally
// keeping the session identified by exceptToken (for example the caller's own
// session after changing their password).
func (s *Store) RevokeUser(database, userID, exceptToken string) {
	keep := ""
	if exceptToken != "" {
		keep = digest(exceptToken)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sess := range s.sessions {
		if key != keep && sess.Database == database && sess.UserID == userID {
			delete(s.sessions, key)
		}
	}
}

// RevokeDatabase invalidates every session bound to a database.
func (s *Store) RevokeDatabase(database string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sess := range s.sessions {
		if sess.Database == database {
			delete(s.sessions, key)
		}
	}
}

// Len returns the number of live sessions.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now())
	return len(s.sessions)
}

func (s *Store) pruneLocked(now time.Time) {
	for key, sess := range s.sessions {
		if !now.Before(sess.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
}

type contextKey int

const (
	sessionKey contextKey = iota
	tokenKey
)

// WithSession returns a context carrying the authenticated session.
func WithSession(ctx context.Context, sess Session) context.Context {
	return context.WithValue(ctx, sessionKey, sess)
}

// FromContext returns the authenticated session stored in ctx, if any.
func FromContext(ctx context.Context) (Session, bool) {
	sess, ok := ctx.Value(sessionKey).(Session)
	return sess, ok
}

// WithToken returns a context carrying the raw bearer token of the request.
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

// TokenFromContext returns the raw bearer token of the request, if any.
func TokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(tokenKey).(string)
	return token
}
