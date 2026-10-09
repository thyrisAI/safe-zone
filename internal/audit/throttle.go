package audit

import (
	"sync"
	"time"
)

// Throttle lets an event through at most once per window for each key.
// It protects the audit table from being flooded by one repeated action
// (for example a script hammering a forbidden URL). State lives in memory
// and is bounded: when it holds maxKeys entries it is cleared.
type Throttle struct {
	mu      sync.Mutex
	window  time.Duration
	maxKeys int
	last    map[string]time.Time
}

// NewThrottle creates a Throttle with the given window and key limit.
func NewThrottle(window time.Duration, maxKeys int) *Throttle {
	return &Throttle{
		window:  window,
		maxKeys: maxKeys,
		last:    make(map[string]time.Time),
	}
}

// Allow reports whether the event for key should be recorded now.
func (t *Throttle) Allow(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if prev, ok := t.last[key]; ok && now.Sub(prev) < t.window {
		return false
	}

	if len(t.last) >= t.maxKeys {
		t.last = make(map[string]time.Time)
	}
	t.last[key] = now
	return true
}
