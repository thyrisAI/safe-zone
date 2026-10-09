package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"thyris-sz/internal/audit"
	"thyris-sz/internal/auth"
	"thyris-sz/internal/middleware"
	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
)

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// CreateUser lets an admin create a new dashboard account (admin or
// viewer). Protected by middleware.RequireAdminSession -- only a session
// with role "admin" can reach this handler.
func CreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Role = strings.TrimSpace(strings.ToLower(req.Role))

	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	if req.Role != "admin" && req.Role != "viewer" {
		http.Error(w, "role must be 'admin' or 'viewer'", http.StatusBadRequest)
		return
	}

	if err := auth.ValidatePassword(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if _, err := repository.GetUserByEmail(req.Email); err == nil {
		http.Error(w, "a user with this email already exists", http.StatusConflict)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "Failed to process password", http.StatusInternalServerError)
		return
	}

	user := &models.User{
		Email:        req.Email,
		PasswordHash: hash,
		Role:         req.Role,
		IsActive:     true,
	}

	if err := repository.CreateUser(user); err != nil {
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	// Record which admin created which account. The actor comes from the
	// session RequireAdminSession verified, never from the request.
	if actor, ok := middleware.SessionFromContext(r.Context()); ok {
		actorID := actor.UserID
		audit.RecordWithDetails(r, &actorID, actor.Email,
			models.AuditActionUserCreated, models.AuditStatusSuccess,
			fmt.Sprintf("%s (%s)", user.Email, user.Role))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(userResponse{Email: user.Email, Role: user.Role})
}

// ListUsers returns all dashboard accounts. Protected by
// middleware.RequireAdminSession -- only an admin can see the user list.
func ListUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	users, err := repository.ListUsers()
	if err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}