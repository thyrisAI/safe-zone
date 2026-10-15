package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
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

// DeleteUser lets an admin delete a dashboard account for good.
// Protected by middleware.RequireAdminSession.
//
// Two rules keep the dashboard manageable: an admin cannot delete their own
// account, and the last active admin cannot be deleted. The deleted user's
// open sessions are closed, and the action is written to the audit log with
// the account's email (audit rows are never deleted).
func DeleteUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	id64, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil || id64 == 0 {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	id := uint(id64)

	if id == actor.UserID {
		http.Error(w, "you cannot delete your own account", http.StatusBadRequest)
		return
	}

	user, err := repository.GetUserByID(id)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	if user.Role == "admin" && user.IsActive {
		admins, err := repository.CountActiveAdmins()
		if err != nil {
			http.Error(w, "Failed to delete user", http.StatusInternalServerError)
			return
		}
		if admins <= 1 {
			http.Error(w, "the last active admin cannot be deleted", http.StatusConflict)
			return
		}
	}

	if err := repository.DeleteUser(id); err != nil {
		http.Error(w, "Failed to delete user", http.StatusInternalServerError)
		return
	}

	// The account is already gone, so a Redis problem must not turn this
	// into an error. Log it; the sessions expire on their own.
	closed, err := auth.RevokeAllSessions(r.Context(), id)
	if err != nil {
		log.Printf("deleted user %d but could not close their sessions: %v", id, err)
	}

	actorID := actor.UserID
	audit.RecordWithDetails(r, &actorID, actor.Email,
		models.AuditActionUserDeleted, models.AuditStatusSuccess,
		fmt.Sprintf("%s (%s), %d sessions closed", user.Email, user.Role, closed))

	w.WriteHeader(http.StatusNoContent)
}
