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
	"errors"
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
	UserID   string
	Username string
	Role     string
	Database string
	// SuperOwner marks an account with full, server-wide control. It is a
	// privilege on top of an owner role and is independent of the database the
	// session signed in to: a super-owner reaches every Server Tool of every
	// database. The effective value is computed at sign-in from the account's
	// stored super_owner flag and the FILE4BASE_SUPER_OWNER bootstrap seed.
	SuperOwner    bool
	CreatedAt     time.Time
	LastSeenAt    time.Time
	ExpiresAt     time.Time
	ClientIP      string
	WebContainer  string
	UserAgent     string
	RequestsCount int64
	// Stamp is the account state the session was opened with. Requests are
	// rejected once the account's current stamp differs (password, role or
	// active state changed, account deleted), even for a session created
	// after a revocation by a sign-in that was already in flight.
	Stamp string
	// Group identifies the session group (#107): the sign-in that opened it
	// and the sessions linked to it in the solutions that one reads through
	// external table occurrences. Each linked session is an ordinary
	// session of its own account in its own solution; revoking the sign-in
	// that started the group revokes all of them.
	Group string
	// ServerAccount names the server account the session was opened with, or
	// is empty for a session of a database account. Such a session is checked
	// against the server account's state rather than the database's row.
	ServerAccount string
	// Linked marks a session opened in a solution of another session's
	// group, rather than by a sign-in of its own.
	Linked bool
}

// IsOwner reports whether the session belongs to a database owner.
func (s Session) IsOwner() bool { return s.Role == RoleOwner }

// IsSuperOwner reports whether the session holds the server-wide super-owner
// privilege.
func (s Session) IsSuperOwner() bool { return s.SuperOwner }

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

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// Create registers a new session and returns its bearer token. The session
// starts a session group of its own.
func (s *Store) Create(userID, username, role, database, stamp string, superOwner bool) (string, Session, error) {
	token, err := newToken()
	if err != nil {
		return "", Session{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.pruneLocked(now)

	sess := s.newSessionLocked(now, userID, username, role, database, stamp, superOwner)
	sess.Group = digest(token)
	s.sessions[digest(token)] = sess
	return token, *sess, nil
}

// CreateForServerAccount registers a session opened by a server account. The
// session carries the account's name, so each request checks the account
// itself: a disabled account, a changed password or role, or one that no
// longer reaches the database ends the session.
func (s *Store) CreateForServerAccount(userID, username, role, database, stamp string, superOwner bool, account string) (string, Session, error) {
	token, sess, err := s.Create(userID, username, role, database, stamp, superOwner)
	if err != nil {
		return "", Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.sessions[digest(token)]; ok {
		stored.ServerAccount = account
	}
	sess.ServerAccount = account
	return token, sess, nil
}

func (s *Store) newSessionLocked(now time.Time, userID, username, role, database, stamp string, superOwner bool) *Session {
	return &Session{
		UserID:        userID,
		Username:      username,
		Role:          role,
		Database:      database,
		SuperOwner:    superOwner,
		Stamp:         stamp,
		CreatedAt:     now,
		LastSeenAt:    now,
		ExpiresAt:     now.Add(s.ttl),
		RequestsCount: 1,
	}
}

// ErrGroupClosed is returned when a session is linked to a group whose
// sign-in is no longer valid.
var ErrGroupClosed = errors.New("the session that started this group is no longer valid")

// ErrLinkToRoot is returned when a session is linked in the solution the
// group's own sign-in is bound to: that session already covers it.
var ErrLinkToRoot = errors.New("the group is already signed in to this solution")

// Link opens a session of an account of another solution in a session group
// (#107), replacing the one the group held there, and returns its bearer
// token. The session has the expiry rules of any other and is revoked with
// the sign-in that started the group.
func (s *Store) Link(group, userID, username, role, database, stamp string, superOwner bool) (string, Session, error) {
	token, err := newToken()
	if err != nil {
		return "", Session{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.pruneLocked(now)

	root, ok := s.sessions[group]
	if !ok || root.Group != group {
		return "", Session{}, ErrGroupClosed
	}
	if root.Database == database {
		return "", Session{}, ErrLinkToRoot
	}
	for key, sess := range s.sessions {
		if sess.Linked && sess.Group == group && sess.Database == database {
			delete(s.sessions, key)
		}
	}
	sess := s.newSessionLocked(now, userID, username, role, database, stamp, superOwner)
	sess.Group, sess.Linked = group, true
	s.sessions[digest(token)] = sess
	return token, *sess, nil
}

// Member returns the session a group holds in a solution — the group's own
// sign-in when it is bound to that solution, which is how a cycle of
// solutions reading each other resolves — and extends its idle lifetime as
// using it directly would. An expired session is dropped, never renewed.
func (s *Store) Member(group, database string) (Session, bool) {
	if group == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	for key, sess := range s.sessions {
		if sess.Group != group || sess.Database != database || (!sess.Linked && key != group) {
			continue
		}
		if !now.Before(sess.ExpiresAt) {
			s.deleteLocked(key)
			return Session{}, false
		}
		sess.LastSeenAt = now
		sess.ExpiresAt = now.Add(s.ttl)
		return *sess, true
	}
	return Session{}, false
}

// Members returns the sessions linked to a group, the sign-in that started
// it excluded.
func (s *Store) Members(group string) []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(s.now())
	var out []Session
	for _, sess := range s.sessions {
		if sess.Linked && sess.Group == group {
			out = append(out, *sess)
		}
	}
	return out
}

// Root returns the sign-in that started a group, without extending it.
func (s *Store) Root(group string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[group]
	if !ok || sess.Linked || !s.now().Before(sess.ExpiresAt) {
		return Session{}, false
	}
	return *sess, true
}

// Unlink revokes the session a group holds in a solution. The group's own
// sign-in is not unlinked: it is revoked by signing out.
func (s *Store) Unlink(group, database string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for key, sess := range s.sessions {
		if sess.Linked && sess.Group == group && sess.Database == database {
			delete(s.sessions, key)
			found = true
		}
	}
	return found
}

// deleteLocked removes a session, and with the sign-in that started a group
// every session linked to it.
func (s *Store) deleteLocked(key string) {
	sess, ok := s.sessions[key]
	if !ok {
		return
	}
	delete(s.sessions, key)
	if sess.Linked || sess.Group != key {
		return
	}
	for other, member := range s.sessions {
		if member.Linked && member.Group == key {
			delete(s.sessions, other)
		}
	}
}

// TouchMeta records client origin metadata (IP, web container hostname, user-agent)
// and increments the request counter for the session.
func (s *Store) TouchMeta(token, clientIP, webContainer, userAgent string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[digest(token)]
	if !ok {
		return
	}
	sess.LastSeenAt = s.now()
	sess.RequestsCount++
	if clientIP != "" {
		sess.ClientIP = clientIP
	}
	if webContainer != "" {
		sess.WebContainer = webContainer
	}
	if userAgent != "" {
		sess.UserAgent = userAgent
	}
}

// Get returns the session for a token and extends its idle lifetime. A
// linked session lives no longer than the sign-in of its group: once that
// one has expired or been revoked, the linked token is refused and dropped.
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
		s.deleteLocked(key)
		return Session{}, false
	}
	if sess.Linked {
		if root, ok := s.sessions[sess.Group]; !ok || !now.Before(root.ExpiresAt) {
			s.deleteLocked(sess.Group)
			s.deleteLocked(key)
			return Session{}, false
		}
	}
	sess.LastSeenAt = now
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

// Revoke invalidates a single token, and every session linked to it when it
// started a session group.
func (s *Store) Revoke(token string) {
	if token == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteLocked(digest(token))
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
			s.deleteLocked(key)
		}
	}
}

// RevokeDatabase invalidates every session bound to a database.
func (s *Store) RevokeDatabase(database string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sess := range s.sessions {
		if sess.Database == database {
			s.deleteLocked(key)
		}
	}
}

// SessionInfo is a safe, serializable view of an active session (never exposes raw tokens).
type SessionInfo struct {
	SessionID     string `json:"session_id"`
	UserID        string `json:"user_id"`
	Username      string `json:"username"`
	Role          string `json:"role"`
	Database      string `json:"database"`
	SuperOwner    bool   `json:"super_owner"`
	ClientIP      string `json:"client_ip"`
	WebContainer  string `json:"web_container"`
	UserAgent     string `json:"user_agent"`
	RequestsCount int64  `json:"requests_count"`
	CreatedAt     string `json:"created_at"`
	LastSeenAt    string `json:"last_seen_at"`
	ExpiresAt     string `json:"expires_at"`
	IsCurrent     bool   `json:"is_current"`
}

// ListSessions returns all active, non-expired sessions in the store.
func (s *Store) ListSessions(currentToken string) []SessionInfo {
	currentKey := ""
	if currentToken != "" {
		currentKey = digest(currentToken)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.pruneLocked(now)
	out := make([]SessionInfo, 0, len(s.sessions))
	for key, sess := range s.sessions {
		lastSeen := sess.LastSeenAt
		if lastSeen.IsZero() {
			lastSeen = sess.CreatedAt
		}
		out = append(out, SessionInfo{
			SessionID:     key[:16],
			UserID:        sess.UserID,
			Username:      sess.Username,
			Role:          sess.Role,
			Database:      sess.Database,
			SuperOwner:    sess.SuperOwner,
			ClientIP:      sess.ClientIP,
			WebContainer:  sess.WebContainer,
			UserAgent:     sess.UserAgent,
			RequestsCount: sess.RequestsCount,
			CreatedAt:     sess.CreatedAt.UTC().Format(time.RFC3339),
			LastSeenAt:    lastSeen.UTC().Format(time.RFC3339),
			ExpiresAt:     sess.ExpiresAt.UTC().Format(time.RFC3339),
			IsCurrent:     key == currentKey,
		})
	}
	return out
}

// RevokeByID revokes a session matching the full or 16-char prefix session digest.
func (s *Store) RevokeByID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.sessions {
		if key == sessionID || (len(sessionID) >= 8 && len(key) >= len(sessionID) && key[:len(sessionID)] == sessionID) {
			s.deleteLocked(key)
			return true
		}
	}
	return false
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
			s.deleteLocked(key)
		}
	}
	// A session linked to a group whose sign-in is gone goes with it.
	for key, sess := range s.sessions {
		if sess.Linked && s.sessions[sess.Group] == nil {
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
