package models

import "gorm.io/gorm"

// User represents a dashboard account that can authenticate through the
// session-based login flow (see internal/auth/session.go and
// internal/handlers/auth.go). This is intentionally separate from the
// token-based machine/API auth in internal/auth/auth.go -- that system
// authenticates API callers (services, scripts) via static tokens and
// permission strings, while User authenticates a human logging into the
// dashboard in a browser.
type User struct {
	gorm.Model
	Email        string `gorm:"uniqueIndex;not null" json:"email"`
	PasswordHash string `gorm:"not null" json:"-"`
	Role         string `gorm:"default:'admin'" json:"role"`
	IsActive     bool   `gorm:"default:true" json:"is_active"`
}
