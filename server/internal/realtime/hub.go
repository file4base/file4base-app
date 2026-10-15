// Package realtime pushes "something changed" notifications to the clients of
// a database, so every open window reloads the layouts and records it is
// showing instead of going stale. It carries no record data: an event names
// what changed (a table, or a kind of schema object) and each client reloads
// what it needs. That keeps the server from having to merge concurrent edits.
package realtime

import (
	"sync"
	"time"
)

// Event is one change notification. Database scopes it (a client only ever
// receives events for the database it signed in to). Origin is the id of the
// session that caused the change, so that client can ignore its own event.
type Event struct {
	Type     string `json:"type"`     // "data" or "schema"
	Resource string `json:"resource"` // table name (data), or object kind (schema)
	Database string `json:"database"`
	Origin   string `json:"origin,omitempty"`
	At       int64  `json:"at"`
}

// Hub fans events out to the subscribers of each database. It is in-memory and
// per-process: fine for a single API instance. Behind several replicas, a
// shared bus (Postgres LISTEN/NOTIFY) would replace the direct fan-out.
type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[*Subscription]struct{} // keyed by lower-case database
}

// Subscription is one connected client's queue of events.
type Subscription struct {
	Events   chan Event
	database string
	hub      *Hub
}

const subscriptionBuffer = 64

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[*Subscription]struct{})}
}

func key(database string) string {
	return normalize(database)
}

// Subscribe registers a client for a database's events.
func (h *Hub) Subscribe(database string) *Subscription {
	sub := &Subscription{Events: make(chan Event, subscriptionBuffer), database: key(database), hub: h}
	h.mu.Lock()
	if h.subs[sub.database] == nil {
		h.subs[sub.database] = make(map[*Subscription]struct{})
	}
	h.subs[sub.database][sub] = struct{}{}
	h.mu.Unlock()
	return sub
}

// Close removes a subscription and releases its queue.
func (s *Subscription) Close() {
	if s == nil || s.hub == nil {
		return
	}
	s.hub.mu.Lock()
	if set := s.hub.subs[s.database]; set != nil {
		delete(set, s)
		if len(set) == 0 {
			delete(s.hub.subs, s.database)
		}
	}
	s.hub.mu.Unlock()
	s.hub = nil
}

// Publish delivers an event to every subscriber of its database. A subscriber
// whose queue is full is skipped (it will reload on its next event or on
// reconnect); the publish never blocks the request that triggered it.
func (h *Hub) Publish(ev Event) {
	if ev.At == 0 {
		ev.At = time.Now().UnixMilli()
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs[key(ev.Database)] {
		select {
		case sub.Events <- ev:
		default:
		}
	}
}

// Subscribers reports how many clients listen to a database (for tests).
func (h *Hub) Subscribers(database string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[key(database)])
}
