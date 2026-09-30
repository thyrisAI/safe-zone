package middleware

import (
	"net/http"

	"thyris-sz/internal/auth"
)

// RequireReadAccess protects a read (GET) route so it accepts either:
//   - a valid API token with the given permission (CLI, scripts, SDK
//     integrations -- see pkg/tszclient-go and pkg/tsz-cli), or
//   - a valid dashboard session, regardless of role (both "admin" and
//     "viewer" can read).
//
// Used instead of auth.RequirePermission on the specific routes the
// dashboard frontend also calls directly (/patterns, /allowlist,
// /blacklist, /validators), so token-based and session-based callers
// keep working side by side.
func RequireReadAccess(tokenPermission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.CheckTokenPermission(r, tokenPermission) {
				next.ServeHTTP(w, r)
				return
			}

			if _, ok := sessionFromRequest(r); ok {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}
}

// RequireWriteAccess protects a write (POST/PATCH/DELETE) route the same
// way as RequireReadAccess, except a dashboard session must additionally
// have the "admin" role -- a "viewer" session is not enough to write.
func RequireWriteAccess(tokenPermission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.CheckTokenPermission(r, tokenPermission) {
				next.ServeHTTP(w, r)
				return
			}

			if session, ok := sessionFromRequest(r); ok && session.Role == "admin" {
				next.ServeHTTP(w, r)
				return
			}

			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		})
	}
}

// sessionFromRequest reads the session cookie, if any, and looks it up in
// Redis. Returns ok=false for any failure (no cookie, expired, invalid).
func sessionFromRequest(r *http.Request) (*auth.Session, bool) {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return nil, false
	}

	session, err := auth.GetSession(r.Context(), cookie.Value)
	if err != nil {
		return nil, false
	}

	return session, true
}