package auth

import (
	"errors"
	"unicode/utf8"
)

const (
	// MinPasswordLength is counted in characters (runes), not bytes, so a
	// password in any script is measured fairly. Length matters more than
	// forced character classes, so no composition rules are enforced.
	MinPasswordLength = 8
	// MaxPasswordLength bounds the work argon2 does for one request.
	MaxPasswordLength = 128
)

var (
	ErrPasswordTooShort  = errors.New("New password must be at least 8 characters")
	ErrPasswordTooLong   = errors.New("New password must be at most 128 characters")
	ErrPasswordUnchanged = errors.New("New password must be different from the current password")
)

// ValidateNewPassword checks a replacement password against the policy.
// It needs no database, so it is cheap to run before any lookup.
func ValidateNewPassword(current, next string) error {
	n := utf8.RuneCountInString(next)
	switch {
	case n < MinPasswordLength:
		return ErrPasswordTooShort
	case n > MaxPasswordLength:
		return ErrPasswordTooLong
	case next == current:
		return ErrPasswordUnchanged
	}
	return nil
}
