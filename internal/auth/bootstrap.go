package auth

import (
	"errors"
	"log"

	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"

	"gorm.io/gorm"
)

// EnsureAdminUser creates the first admin user from ADMIN_EMAIL/
// ADMIN_PASSWORD if one doesn't already exist. Called once at startup
// (see main.go). Idempotent: safe to run on every restart.
func EnsureAdminUser(email, password string) {
	if email == "" || password == "" {
		log.Println("[Auth] ADMIN_EMAIL/ADMIN_PASSWORD not set, skipping admin bootstrap")
		return
	}

	_, err := repository.GetUserByEmail(email)
	if err == nil {
		// Already exists, nothing to do.
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("[Auth] Failed to check for existing admin user: %v", err)
		return
	}

	hash, err := HashPassword(password)
	if err != nil {
		log.Printf("[Auth] Failed to hash admin password: %v", err)
		return
	}

	admin := &models.User{
		Email:        email,
		PasswordHash: hash,
		Role:         "admin",
		IsActive:     true,
	}

	if err := repository.CreateUser(admin); err != nil {
		log.Printf("[Auth] Failed to create admin user: %v", err)
		return
	}

	log.Printf("[Auth] Created initial admin user: %s", email)
}