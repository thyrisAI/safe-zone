package auth

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"thyris-sz/internal/cache"
)

const (
	// MaxPasswordAttempts is how many wrong current-password guesses one
	// user may make inside PasswordAttemptWindow before being blocked.
	// Without this, a stolen session could be used to guess the password.
	MaxPasswordAttempts   = 5
	PasswordAttemptWindow = 15 * time.Minute

	passwordAttemptKeyPrefix = "pwchange_fail:"
)

func passwordAttemptKey(userID uint) string {
	return passwordAttemptKeyPrefix + strconv.FormatUint(uint64(userID), 10)
}

// PasswordAttemptsExceeded reports whether the user is currently blocked
// from trying the current password again.
func PasswordAttemptsExceeded(ctx context.Context, userID uint) (bool, error) {
	n, err := cache.RDB.Get(ctx, passwordAttemptKey(userID)).Int()
	if err != nil {
		// A missing key means no recent failures.
		if errors.Is(err, redis.Nil) {
			return false, nil
		}
		return false, err
	}
	return n >= MaxPasswordAttempts, nil
}

// RecordPasswordFailure counts one wrong guess. The window starts at the
// first failure and the key expires on its own.
func RecordPasswordFailure(ctx context.Context, userID uint) error {
	key := passwordAttemptKey(userID)
	n, err := cache.RDB.Incr(ctx, key).Result()
	if err != nil {
		return err
	}
	if n == 1 {
		return cache.RDB.Expire(ctx, key, PasswordAttemptWindow).Err()
	}
	return nil
}

// ClearPasswordFailures resets the counter after a successful change.
func ClearPasswordFailures(ctx context.Context, userID uint) error {
	return cache.RDB.Del(ctx, passwordAttemptKey(userID)).Err()
}
