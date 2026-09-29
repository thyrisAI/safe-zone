package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"thyris-sz/internal/cache"
)

const (
	// SessionCookieName is the cookie the browser stores the session ID in.
	SessionCookieName = "sz_session"
	// SessionTTL controls how long a session stays valid in Redis. After
	// this, the user is logged out automatically even without clicking
	// "logout" -- Redis expires the key on its own.
	SessionTTL = 24 * time.Hour

	sessionKeyPrefix = "session:"
)

// Session is what we store in Redis per logged-in user. Kept intentionally
// small -- anything else about the user (permissions, preferences) should
// be looked up from the DB by UserID when needed, not duplicated here.
type Session struct {
	UserID uint   `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
}

// CreateSession generates a new random session ID, stores the session data
// in Redis under that ID with a TTL, and returns the ID. The caller (the
// login handler, in step 5) is responsible for setting it as a cookie.
func CreateSession(ctx context.Context, s Session) (string, error) {
	id, err := generateSessionID()
	if err != nil {
		return "", err
	}

	data, err := json.Marshal(s)
	if err != nil {
		return "", err
	}

	if err := cache.RDB.Set(ctx, sessionKeyPrefix+id, data, SessionTTL).Err(); err != nil {
		return "", err
	}

	return id, nil
}

// GetSession looks up a session by ID. Returns an error if the ID is empty,
// missing from Redis, or expired -- callers should treat any error as
// "not authenticated".
func GetSession(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, errors.New("empty session id")
	}

	val, err := cache.RDB.Get(ctx, sessionKeyPrefix+id).Result()
	if err != nil {
		return nil, err
	}

	var s Session
	if err := json.Unmarshal([]byte(val), &s); err != nil {
		return nil, err
	}

	return &s, nil
}

// DeleteSession removes a session from Redis (logout).
func DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	return cache.RDB.Del(ctx, sessionKeyPrefix+id).Err()
}

// generateSessionID produces a cryptographically random, URL-safe session
// identifier. 32 random bytes = 256 bits of entropy, which is far beyond
// what's guessable/brute-forceable.
func generateSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}