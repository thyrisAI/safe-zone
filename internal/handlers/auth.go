package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"thyris-sz/internal/audit"
	"thyris-sz/internal/auth"
	"thyris-sz/internal/config"
	"thyris-sz/internal/middleware"
	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Login authenticates a dashboard user with email+password and, on
// success, starts a session and sets it as an HttpOnly cookie.
func Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	// Always the same error for "no such user" and "wrong password" --
	// distinguishing them would let an attacker enumerate valid emails.
	unauthorized := func() {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
	}

	clientIP := audit.ClientIP(r)

	// failed counts the failure, records it in the audit log, then answers
	// with the same generic error. userID is nil when the email matches no
	// user.
	failed := func(userID *uint) {
		// Counting is best effort: a Redis problem must not turn a normal
		// "wrong password" answer into an error.
		if err := auth.RecordLoginFailure(r.Context(), req.Email, clientIP); err != nil {
			log.Printf("login rate limit: could not record failure: %v", err)
		}
		audit.Record(r, userID, req.Email, models.AuditActionLogin, models.AuditStatusFailure)
		unauthorized()
	}

	// Empty credentials carry no identity, so they are rejected without an
	// audit row (otherwise anyone could fill the table with empty requests).
	if req.Email == "" || req.Password == "" {
		unauthorized()
		return
	}

	// Refuse before touching the database or checking the password, so a
	// blocked client learns nothing about whether the account exists.
	// If Redis is down the check is skipped (fail open): signing in must
	// keep working even when the counter cannot.
	blocked, retryAfter, err := auth.LoginBlocked(r.Context(), req.Email, clientIP)
	if err != nil {
		log.Printf("login rate limit: check skipped: %v", err)
	} else if blocked {
		audit.RecordLoginBlocked(r, req.Email)
		w.Header().Set("Retry-After", strconv.Itoa(auth.RetryAfterSeconds(retryAfter)))
		http.Error(w, "Too many attempts. Try again later.", http.StatusTooManyRequests)
		return
	}

	user, err := repository.GetUserByEmail(req.Email)
	if err != nil {
		failed(nil)
		return
	}

	userID := user.ID

	if !user.IsActive {
		failed(&userID)
		return
	}

	if !auth.VerifyPassword(req.Password, user.PasswordHash) {
		failed(&userID)
		return
	}

	sessionID, err := auth.CreateSession(r.Context(), auth.Session{
		UserID: user.ID,
		Email:  user.Email,
		Role:   user.Role,
	})
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	setSessionCookie(w, sessionID)
	if err := auth.ClearLoginFailures(r.Context(), req.Email); err != nil {
		log.Printf("login rate limit: could not reset counter: %v", err)
	}
	audit.Record(r, &userID, user.Email, models.AuditActionLogin, models.AuditStatusSuccess)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse{Email: user.Email, Role: user.Role})
}

// Logout ends the current session, if any, and clears the cookie.
func Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		// Read the session first: once it is deleted we can no longer tell
		// who was logging out.
		session, sessionErr := auth.GetSession(r.Context(), cookie.Value)
		_ = auth.DeleteSession(r.Context(), cookie.Value)

		if sessionErr == nil {
			userID := session.UserID
			audit.Record(r, &userID, session.Email, models.AuditActionLogout, models.AuditStatusSuccess)
		}
	}

	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the currently logged-in user based on the session cookie.
// 401 if there is no valid session.
func Me(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	session, err := auth.GetSession(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userResponse{Email: session.Email, Role: session.Role})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword lets the signed-in user replace their own password. The
// user comes from the session, never from the request body. Any role may
// call it (route middleware: RequireSession).
//
// Responses: 204 on success, 400 policy violation or bad body, 401 no
// valid session, 403 wrong current password, 429 too many wrong guesses.
func ChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.CurrentPassword == "" {
		http.Error(w, "Current password is required", http.StatusBadRequest)
		return
	}
	if err := auth.ValidateNewPassword(req.CurrentPassword, req.NewPassword); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := repository.GetUserByID(session.UserID)
	if err != nil || !user.IsActive {
		// The account was removed or disabled after this session started.
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	userID := user.ID

	blocked, err := auth.PasswordAttemptsExceeded(r.Context(), userID)
	if err != nil {
		http.Error(w, "Failed to change password", http.StatusInternalServerError)
		return
	}
	if blocked {
		http.Error(w, "Too many incorrect attempts. Try again later.", http.StatusTooManyRequests)
		return
	}

	if !auth.VerifyPassword(req.CurrentPassword, user.PasswordHash) {
		// Both writes are best-effort bookkeeping; the answer is the same.
		_ = auth.RecordPasswordFailure(r.Context(), userID)
		audit.Record(r, &userID, user.Email, models.AuditActionPasswordChange, models.AuditStatusFailure)
		http.Error(w, "Current password is incorrect", http.StatusForbidden)
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, "Failed to change password", http.StatusInternalServerError)
		return
	}

	if err := repository.UpdateUserPasswordHash(userID, hash); err != nil {
		http.Error(w, "Failed to change password", http.StatusInternalServerError)
		return
	}

	_ = auth.ClearPasswordFailures(r.Context(), userID)
	audit.Record(r, &userID, user.Email, models.AuditActionPasswordChange, models.AuditStatusSuccess)
	w.WriteHeader(http.StatusNoContent)
}

func setSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   config.AppConfig.AppMode == "PROD",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(auth.SessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   config.AppConfig.AppMode == "PROD",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}