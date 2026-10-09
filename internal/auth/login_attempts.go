package auth

import (
	"context"
	"strings"
	"time"
)

// Failed sign-ins are counted twice: per email and per IP address.
// The email counter stops guessing against one account. The IP counter
// stops one machine from trying many different emails, which would never
// fill any single email counter.
const MaxLoginAttemptsPerEmail = 5

const MaxLoginAttemptsPerIP = 20

const LoginAttemptWindow = 15 * time.Minute

// maxLoginIDLength keeps Redis keys small even if someone submits a huge
// "email".
const maxLoginIDLength = 254

var loginEmailLimiter = NewAttemptLimiter("login_fail:email:", MaxLoginAttemptsPerEmail, LoginAttemptWindow)

var loginIPLimiter = NewAttemptLimiter("login_fail:ip:", MaxLoginAttemptsPerIP, LoginAttemptWindow)

// NormalizeLoginEmail makes "Admin@Example.com " and "admin@example.com"
// share one counter, and bounds the key length.
func NormalizeLoginEmail(email string) string {
	id := strings.ToLower(strings.TrimSpace(email))
	if len(id) > maxLoginIDLength {
		id = id[:maxLoginIDLength]
	}
	return id
}

// LoginBlocked reports whether a sign-in attempt must be refused because
// the email or the IP made too many recent failures, and for how long.
//
// The email does not have to belong to a real user: every email is
// counted the same way, so the answer never reveals which accounts exist.
func LoginBlocked(ctx context.Context, email, ip string) (bool, time.Duration, error) {
	emailBlocked, emailWait, err := loginEmailLimiter.Exceeded(ctx, NormalizeLoginEmail(email))
	if err != nil {
		return false, 0, err
	}
	ipBlocked, ipWait, err := loginIPLimiter.Exceeded(ctx, ip)
	if err != nil {
		return false, 0, err
	}

	if !emailBlocked && !ipBlocked {
		return false, 0, nil
	}
	if emailWait > ipWait {
		return true, emailWait, nil
	}
	return true, ipWait, nil
}

// RecordLoginFailure counts one failed sign-in against both the email and
// the IP address. Both counters are always attempted.
func RecordLoginFailure(ctx context.Context, email, ip string) error {
	emailErr := loginEmailLimiter.RecordFailure(ctx, NormalizeLoginEmail(email))
	ipErr := loginIPLimiter.RecordFailure(ctx, ip)
	if emailErr != nil {
		return emailErr
	}
	return ipErr
}

// ClearLoginFailures resets the email counter after a successful sign-in.
// The IP counter is left alone: one success must not erase failures made
// against other accounts from the same address.
func ClearLoginFailures(ctx context.Context, email string) error {
	return loginEmailLimiter.Clear(ctx, NormalizeLoginEmail(email))
}
