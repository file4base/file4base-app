package auth

import (
	"sync"
	"time"
)

// LoginThrottle slows down password guessing. It counts the failed attempts
// against each key (a database + username + client address) and, once too
// many have failed in a row, refuses further attempts for a lockout window.
// A successful attempt clears the counter.
//
// It is in-memory and per-process: it does not survive a restart and is not
// shared between replicas, which is enough to blunt online guessing without a
// store. Keys are swept once they are usable again, so the map cannot grow
// without bound.
type LoginThrottle struct {
	max      int
	window   time.Duration
	lockout  time.Duration
	nowFn    func() time.Time
	mu       sync.Mutex
	attempts map[string]*attemptRecord
}

type attemptRecord struct {
	count       int
	firstFailed time.Time
	lockedUntil time.Time
}

// NewLoginThrottle allows max failures in a row before locking a key out for
// lockout; a gap of window with no failure forgets the earlier ones. max <= 0
// disables it (every attempt is allowed).
func NewLoginThrottle(max int, window, lockout time.Duration) *LoginThrottle {
	return &LoginThrottle{
		max:      max,
		window:   window,
		lockout:  lockout,
		nowFn:    time.Now,
		attempts: make(map[string]*attemptRecord),
	}
}

// Allow reports whether an attempt on key may proceed, and for how long it is
// locked out otherwise.
func (t *LoginThrottle) Allow(key string) (allowed bool, retryAfter time.Duration) {
	if t == nil || t.max <= 0 {
		return true, 0
	}
	now := t.nowFn()
	t.mu.Lock()
	defer t.mu.Unlock()
	rec := t.attempts[key]
	if rec == nil {
		return true, 0
	}
	if !rec.lockedUntil.IsZero() && now.Before(rec.lockedUntil) {
		return false, rec.lockedUntil.Sub(now)
	}
	// The window since the first failure has passed with no lockout: forget it.
	if rec.lockedUntil.IsZero() && now.Sub(rec.firstFailed) >= t.window {
		delete(t.attempts, key)
	}
	return true, 0
}

// Fail records a failed attempt on key and returns whether it is now locked
// out and for how long.
func (t *LoginThrottle) Fail(key string) (locked bool, retryAfter time.Duration) {
	if t == nil || t.max <= 0 {
		return false, 0
	}
	now := t.nowFn()
	t.mu.Lock()
	defer t.mu.Unlock()
	rec := t.attempts[key]
	if rec == nil || (rec.lockedUntil.IsZero() && now.Sub(rec.firstFailed) >= t.window) {
		rec = &attemptRecord{firstFailed: now}
		t.attempts[key] = rec
	}
	rec.count++
	if rec.count >= t.max {
		rec.lockedUntil = now.Add(t.lockout)
		return true, t.lockout
	}
	return false, 0
}

// Succeed clears the counter for key after a successful attempt.
func (t *LoginThrottle) Succeed(key string) {
	if t == nil || t.max <= 0 {
		return
	}
	t.mu.Lock()
	delete(t.attempts, key)
	t.mu.Unlock()
}
