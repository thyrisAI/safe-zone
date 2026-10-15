package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
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

	// userSessionsKeyPrefix names a Redis SET per user that lists the ids of
	// that user's live sessions. It is the "register" that lets us find and
	// close every session of one user, for example after a password change.
	userSessionsKeyPrefix = "user_sessions:"
)

// UserSessionsKey returns the Redis key of the set holding a user's
// session ids.
func UserSessionsKey(userID uint) string {
	return userSessionsKeyPrefix + strconv.FormatUint(uint64(userID), 10)
}

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

	// Write the id into the user's register. The register lives as long as
	// the newest session, so it always outlives every session listed in it.
	userKey := UserSessionsKey(s.UserID)
	pipe := cache.RDB.TxPipeline()
	pipe.SAdd(ctx, userKey, id)
	pipe.Expire(ctx, userKey, SessionTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		// A session that cannot be revoked later is worse than a failed
		// sign-in, so remove it and report the error.
		_ = cache.RDB.Del(ctx, sessionKeyPrefix+id).Err()
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

// DeleteSession removes a session from Redis (logout) and from its user's
// register.
func DeleteSession(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}

	// Read the owner first: once the session is gone we cannot tell whose
	// register it belongs to. A session that is already gone or unreadable
	// simply has no register entry to clean.
	session, getErr := GetSession(ctx, id)

	if err := cache.RDB.Del(ctx, sessionKeyPrefix+id).Err(); err != nil {
		return err
	}

	if getErr == nil {
		return cache.RDB.SRem(ctx, UserSessionsKey(session.UserID), id).Err()
	}
	return nil
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

// OtherSessionIDs returns ids without keepID, in the same order. It holds
// the one rule of "close the other sessions": the session making the request
// stays, everything else goes. An empty keepID keeps nothing.
func OtherSessionIDs(ids []string, keepID string) []string {
	others := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != keepID {
			others = append(others, id)
		}
	}
	return others
}

// RevokeOtherSessions closes every live session of the user except keepID
// (normally the session that made the request), and returns how many live
// sessions it closed.
//
// It reads the user's register, deletes the matching session records and
// removes those ids from the register, all in one transaction. Ids in the
// register whose session already expired are cleaned up too but not counted.
func RevokeOtherSessions(ctx context.Context, userID uint, keepID string) (int, error) {
	userKey := UserSessionsKey(userID)

	ids, err := cache.RDB.SMembers(ctx, userKey).Result()
	if err != nil {
		return 0, err
	}

	others := OtherSessionIDs(ids, keepID)
	if len(others) == 0 {
		return 0, nil
	}

	sessionKeys := make([]string, len(others))
	members := make([]interface{}, len(others))
	for i, id := range others {
		sessionKeys[i] = sessionKeyPrefix + id
		members[i] = id
	}

	pipe := cache.RDB.TxPipeline()
	deleted := pipe.Del(ctx, sessionKeys...)
	pipe.SRem(ctx, userKey, members...)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	return int(deleted.Val()), nil
}

// RevokeAllSessions closes every live session of the user, including the
// one making the request, and returns how many live sessions it closed.
// It is used when an account is deleted.
func RevokeAllSessions(ctx context.Context, userID uint) (int, error) {
	closed, err := RevokeOtherSessions(ctx, userID, "")
	if err != nil {
		return 0, err
	}
	if err := cache.RDB.Del(ctx, UserSessionsKey(userID)).Err(); err != nil {
		return closed, err
	}
	return closed, nil
}
