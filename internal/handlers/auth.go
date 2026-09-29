package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"thyris-sz/internal/auth"
	"thyris-sz/internal/config"
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

	if req.Email == "" || req.Password == "" {
		unauthorized()
		return
	}

	user, err := repository.GetUserByEmail(req.Email)
	if err != nil {
		unauthorized()
		return
	}

	if !user.IsActive {
		unauthorized()
		return
	}

	if !auth.VerifyPassword(req.Password, user.PasswordHash) {
		unauthorized()
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
		_ = auth.DeleteSession(r.Context(), cookie.Value)
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