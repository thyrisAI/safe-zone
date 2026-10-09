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
	ErrPasswordTooShort  = errors.New("Password must be at least 8 characters")
	ErrPasswordTooLong   = errors.New("Password must be at most 128 characters")
	ErrPasswordUnchanged = errors.New("New password must be different from the current password")
)

// ValidatePassword checks a password against the length policy. Used when
// an account is created and, through ValidateNewPassword, when it changes.
// It needs no database, so it is cheap to run before any lookup.
func ValidatePassword(password string) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < MinPasswordLength:
		return ErrPasswordTooShort
	case n > MaxPasswordLength:
		return ErrPasswordTooLong
	}
	return nil
}

// ValidateNewPassword checks a replacement password: the length policy,
// plus it must differ from the current one.
func ValidateNewPassword(current, next string) error {
	if err := ValidatePassword(next); err != nil {
		return err
	}
	if next == current {
		return ErrPasswordUnchanged
	}
	return nil
}
