package auth

import (
	"testing"
	"time"
)

func TestLoginThrottle(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	th := NewLoginThrottle(3, time.Minute, 5*time.Minute)
	th.nowFn = func() time.Time { return now }

	// Two failures: still allowed.
	for i := 0; i < 2; i++ {
		if ok, _ := th.Allow("k"); !ok {
			t.Fatalf("attempt %d should be allowed", i)
		}
		th.Fail("k")
	}
	// Third failure trips the lockout.
	if ok, _ := th.Allow("k"); !ok {
		t.Fatal("third attempt should still be allowed before the failure")
	}
	if locked, retry := th.Fail("k"); !locked || retry != 5*time.Minute {
		t.Fatalf("expected lockout for 5m, got locked=%v retry=%v", locked, retry)
	}
	if ok, retry := th.Allow("k"); ok || retry <= 0 {
		t.Fatalf("should be locked out, got ok=%v retry=%v", ok, retry)
	}
	// A different key is unaffected.
	if ok, _ := th.Allow("other"); !ok {
		t.Fatal("another key must not be locked out")
	}
	// After the lockout passes, attempts are allowed again.
	now = now.Add(5*time.Minute + time.Second)
	if ok, _ := th.Allow("k"); !ok {
		t.Fatal("lockout should have expired")
	}

	// A success clears the counter.
	th2 := NewLoginThrottle(3, time.Minute, time.Minute)
	th2.Fail("a")
	th2.Fail("a")
	th2.Succeed("a")
	if locked, _ := th2.Fail("a"); locked {
		t.Fatal("success should have reset the counter")
	}

	// max <= 0 disables it.
	off := NewLoginThrottle(0, time.Minute, time.Minute)
	for i := 0; i < 100; i++ {
		off.Fail("x")
	}
	if ok, _ := off.Allow("x"); !ok {
		t.Fatal("a disabled throttle allows everything")
	}
}
