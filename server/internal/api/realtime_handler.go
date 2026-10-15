package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/file4base/file4base-app/server/internal/auth"
	"github.com/file4base/file4base-app/server/internal/realtime"
)

// RealtimeHandler serves the change-notification WebSocket. A client connects,
// sends its bearer token as the first message, and then receives an event
// whenever the records or schema of its database change in another session.
type RealtimeHandler struct {
	hub      *realtime.Hub
	sessions *auth.Store
}

func NewRealtimeHandler(hub *realtime.Hub, sessions *auth.Store) *RealtimeHandler {
	return &RealtimeHandler{hub: hub, sessions: sessions}
}

// originID is a stable, non-reversible id for a session's token. The same
// token identifies a client on both the WebSocket and its REST calls, so a
// client can ignore the events caused by its own changes.
func originID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:16]
}

type realtimeAuth struct {
	Token string `json:"token"`
}

type realtimeWelcome struct {
	Type     string `json:"type"`   // "welcome"
	Origin   string `json:"origin"` // this client's own origin id
	Database string `json:"database"`
}

// ServeWS upgrades the request and runs the client until it disconnects.
//
// Authentication is the first message (a browser cannot set an Authorization
// header on a WebSocket), not an ambient cookie, so there is no cross-site
// WebSocket hijacking vector and the origin is not checked here.
func (h *RealtimeHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()

	// First message within a short deadline: the bearer token.
	authCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	var creds realtimeAuth
	if err := wsjson.Read(authCtx, c, &creds); err != nil {
		cancel()
		_ = c.Close(websocket.StatusPolicyViolation, "expected an auth message")
		return
	}
	cancel()

	sess, ok := h.sessions.Get(strings.TrimSpace(creds.Token))
	if !ok {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid or expired token")
		return
	}

	sub := h.hub.Subscribe(sess.Database)
	defer sub.Close()

	ctx := r.Context()
	if err := wsjson.Write(ctx, c, realtimeWelcome{Type: "welcome", Origin: originID(creds.Token), Database: sess.Database}); err != nil {
		return
	}

	// Drain client reads (pings/closes) so the connection stays healthy; a
	// read error means the client went away.
	readErr := make(chan struct{})
	go func() {
		defer close(readErr)
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()

	// Keepalive: a ping well within nginx's proxy read timeout keeps an idle
	// connection from being closed by a proxy in front of the server.
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-readErr:
			return
		case <-ping.C:
			pingCtx, pc := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pingCtx)
			pc()
			if err != nil {
				return
			}
		case ev := <-sub.Events:
			writeCtx, wc := context.WithTimeout(ctx, 10*time.Second)
			err := wsjson.Write(writeCtx, c, ev)
			wc()
			if err != nil {
				return
			}
		}
	}
}

// emitChangeEvents publishes a realtime change notification after a successful
// data or schema mutation, so the other clients of the database reload. It is
// a coarse, robust signal (no record data), derived from the request path.
func emitChangeEvents(hub *realtime.Hub) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.status < 200 || rec.status >= 300 {
				return
			}
			ev, ok := changeEventFor(r)
			if !ok {
				return
			}
			sess, ok := auth.FromContext(r.Context())
			if !ok {
				return
			}
			ev.Database = sess.Database
			ev.Origin = originID(auth.TokenFromContext(r.Context()))
			hub.Publish(ev)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

// Flush/Unwrap pass-throughs keep streaming and middleware chains working.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// changeEventFor decides whether a request changed data or schema, and names
// the affected resource, from its method and path. Read-only POSTs (find,
// summary, import preview) and non-content routes produce no event.
func changeEventFor(r *http.Request) (realtime.Event, bool) {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return realtime.Event{}, false
	}
	path := r.URL.Path
	lower := strings.ToLower(path)

	// Data routes: /api/v1/data/{table}[/...]
	if strings.HasPrefix(lower, "/api/v1/data/") {
		if strings.Contains(lower, "/find") || strings.Contains(lower, "/summary") || strings.Contains(lower, "/import/preview") {
			return realtime.Event{}, false
		}
		rest := strings.TrimPrefix(path, "/api/v1/data/")
		table := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			table = rest[:i]
		}
		if table == "" {
			return realtime.Event{}, false
		}
		return realtime.Event{Type: "data", Resource: table}, true
	}

	// Schema / solution routes: a design change. Reload layouts and tables.
	schemaPrefixes := []string{
		"/api/v1/schemas", "/api/v1/layouts", "/api/v1/tables", "/api/v1/relationships",
		"/api/v1/occurrences", "/api/v1/value-lists", "/api/v1/scripts", "/api/v1/menu-sets",
		"/api/v1/data-sources", "/api/v1/saved-finds",
	}
	for _, p := range schemaPrefixes {
		if strings.HasPrefix(lower, p) {
			return realtime.Event{Type: "schema", Resource: strings.TrimPrefix(p, "/api/v1/")}, true
		}
	}
	if strings.HasPrefix(lower, "/api/v1/solutions/import") {
		return realtime.Event{Type: "schema", Resource: "solution"}, true
	}
	return realtime.Event{}, false
}
