package unit

import (
	"testing"
	"time"

	"thyris-sz/internal/audit"
)

func TestThrottle_BlocksRepeatWithinWindow(t *testing.T) {
	th := audit.NewThrottle(time.Minute, 10)
	now := time.Now()

	if !th.Allow("a", now) {
		t.Fatal("first event should be allowed")
	}
	if th.Allow("a", now.Add(30*time.Second)) {
		t.Fatal("repeat inside the window should be blocked")
	}
}

func TestThrottle_AllowsAfterWindow(t *testing.T) {
	th := audit.NewThrottle(time.Minute, 10)
	now := time.Now()

	th.Allow("a", now)
	if !th.Allow("a", now.Add(61*time.Second)) {
		t.Fatal("event after the window should be allowed")
	}
}

func TestThrottle_KeysAreIndependent(t *testing.T) {
	th := audit.NewThrottle(time.Minute, 10)
	now := time.Now()

	th.Allow("a", now)
	if !th.Allow("b", now) {
		t.Fatal("a different key should not be throttled")
	}
}

func TestThrottle_StaysBounded(t *testing.T) {
	th := audit.NewThrottle(time.Minute, 2)
	now := time.Now()

	th.Allow("a", now)
	th.Allow("b", now)
	// Third key hits the limit and resets the state, so "a" is new again.
	th.Allow("c", now)
	if !th.Allow("a", now) {
		t.Fatal("state should have been cleared when the key limit was reached")
	}
}
