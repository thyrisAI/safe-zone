package auth

import (
	"context"
	"strconv"
	"time"
)

const MaxPasswordAttempts = 5

// PasswordAttemptWindow is how long wrong current-password guesses are
// remembered. Without a limit, a stolen session could be used to guess the
// password.
const PasswordAttemptWindow = 15 * time.Minute

var passwordLimiter = NewAttemptLimiter("pwchange_fail:", MaxPasswordAttempts, PasswordAttemptWindow)

func passwordAttemptID(userID uint) string {
	return strconv.FormatUint(uint64(userID), 10)
}

// PasswordAttemptsExceeded reports whether the user is currently blocked
// from trying the current password again.
func PasswordAttemptsExceeded(ctx context.Context, userID uint) (bool, error) {
	exceeded, _, err := passwordLimiter.Exceeded(ctx, passwordAttemptID(userID))
	return exceeded, err
}

// RecordPasswordFailure counts one wrong guess. The window starts at the
// first failure and the counter expires on its own.
func RecordPasswordFailure(ctx context.Context, userID uint) error {
	return passwordLimiter.RecordFailure(ctx, passwordAttemptID(userID))
}

// ClearPasswordFailures resets the counter after a successful change.
func ClearPasswordFailures(ctx context.Context, userID uint) error {
	return passwordLimiter.Clear(ctx, passwordAttemptID(userID))
}
