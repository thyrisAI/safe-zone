package unit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"thyris-sz/internal/auth"
	"thyris-sz/internal/cache"
)

func TestNormalizeLoginEmail_SharesOneCounterAcrossSpellings(t *testing.T) {
	a := auth.NormalizeLoginEmail("  Admin@Example.COM ")
	b := auth.NormalizeLoginEmail("admin@example.com")
	if a != b {
		t.Fatalf("expected the same counter id, got %q and %q", a, b)
	}
}

func TestNormalizeLoginEmail_BoundsKeyLength(t *testing.T) {
	huge := strings.Repeat("a", 5000) + "@example.com"
	if got := len(auth.NormalizeLoginEmail(huge)); got > 254 {
		t.Fatalf("expected id of at most 254 bytes, got %d", got)
	}
}

func TestAttemptLimiter_KeysAreSeparatedByPrefix(t *testing.T) {
	one := auth.NewAttemptLimiter("one:", 5, time.Minute)
	two := auth.NewAttemptLimiter("two:", 5, time.Minute)

	if one.Key("x") == two.Key("x") {
		t.Fatal("limiters with different prefixes must not share keys")
	}
	if one.Key("x") != "one:x" {
		t.Fatalf("unexpected key %q", one.Key("x"))
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	cases := map[string]struct {
		in   time.Duration
		want int
	}{
		"zero is at least one second": {0, 1},
		"sub-second rounds up":        {200 * time.Millisecond, 1},
		"exact seconds stay":          {30 * time.Second, 30},
		"partial seconds round up":    {30*time.Second + time.Millisecond, 31},
		"full window":                 {15 * time.Minute, 900},
	}
	for name, tc := range cases {
		if got := auth.RetryAfterSeconds(tc.in); got != tc.want {
			t.Errorf("%s: got %d, want %d", name, got, tc.want)
		}
	}
}

// Without Redis the limiter must report an error (so the login handler can
// fail open) instead of panicking.
func TestLoginBlocked_WithoutRedisReturnsError(t *testing.T) {
	previous := cache.RDB
	cache.RDB = nil
	defer func() { cache.RDB = previous }()

	blocked, _, err := auth.LoginBlocked(context.Background(), "a@example.com", "10.0.0.1")
	if blocked {
		t.Fatal("must not block when the counter is unavailable")
	}
	if !errors.Is(err, auth.ErrLimiterUnavailable) {
		t.Fatalf("expected ErrLimiterUnavailable, got %v", err)
	}
}

func TestRecordLoginFailure_WithoutRedisReturnsError(t *testing.T) {
	previous := cache.RDB
	cache.RDB = nil
	defer func() { cache.RDB = previous }()

	err := auth.RecordLoginFailure(context.Background(), "a@example.com", "10.0.0.1")
	if !errors.Is(err, auth.ErrLimiterUnavailable) {
		t.Fatalf("expected ErrLimiterUnavailable, got %v", err)
	}
}
