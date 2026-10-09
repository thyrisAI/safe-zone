package auth

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"thyris-sz/internal/cache"
)

// ErrLimiterUnavailable is returned when Redis is not reachable. Callers
// decide whether to fail open (login) or closed.
var ErrLimiterUnavailable = errors.New("attempt limiter unavailable")

// recordFailureScript increments the counter and makes sure it always has
// an expiry. Doing both in one script means a crash between "count" and
// "set expiry" can never leave a counter that blocks someone forever.
var recordFailureScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 or redis.call('PTTL', KEYS[1]) < 0 then
	redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return n
`)

// AttemptLimiter counts failed attempts per identifier inside a time
// window, backed by Redis. After max failures the identifier is blocked
// until its counter expires on its own.
type AttemptLimiter struct {
	prefix string
	max    int
	window time.Duration
}

// NewAttemptLimiter creates a limiter. prefix keeps the Redis keys of
// different limiters apart (for example "login_fail:ip:").
func NewAttemptLimiter(prefix string, max int, window time.Duration) *AttemptLimiter {
	return &AttemptLimiter{prefix: prefix, max: max, window: window}
}

// Key returns the Redis key used for an identifier.
func (l *AttemptLimiter) Key(id string) string {
	return l.prefix + id
}

// Exceeded reports whether id is currently blocked and, if so, how long
// until the block ends.
func (l *AttemptLimiter) Exceeded(ctx context.Context, id string) (bool, time.Duration, error) {
	if cache.RDB == nil {
		return false, 0, ErrLimiterUnavailable
	}

	key := l.Key(id)
	n, err := cache.RDB.Get(ctx, key).Int()
	if err != nil {
		// A missing key means no recent failures.
		if errors.Is(err, redis.Nil) {
			return false, 0, nil
		}
		return false, 0, err
	}
	if n < l.max {
		return false, 0, nil
	}

	ttl, err := cache.RDB.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		// Unknown remaining time: be conservative and report a full window.
		ttl = l.window
	}
	return true, ttl, nil
}

// RecordFailure counts one failed attempt for id.
func (l *AttemptLimiter) RecordFailure(ctx context.Context, id string) error {
	if cache.RDB == nil {
		return ErrLimiterUnavailable
	}
	return recordFailureScript.Run(ctx, cache.RDB, []string{l.Key(id)}, l.window.Milliseconds()).Err()
}

// Clear forgets all failures for id (after a success).
func (l *AttemptLimiter) Clear(ctx context.Context, id string) error {
	if cache.RDB == nil {
		return ErrLimiterUnavailable
	}
	return cache.RDB.Del(ctx, l.Key(id)).Err()
}

// RetryAfterSeconds converts a block duration into the whole seconds a
// Retry-After header expects. It rounds up and is never less than 1.
func RetryAfterSeconds(d time.Duration) int {
	seconds := int((d + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
