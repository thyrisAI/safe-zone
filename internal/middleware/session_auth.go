package middleware

import (
	"context"
	"net/http"

	"thyris-sz/internal/auth"
)

type sessionContextKey string

const currentSessionKey sessionContextKey = "dashboard_session"

// RequireSession protects a route so only requests carrying a valid
// dashboard session cookie (set by POST /auth/login) can proceed. This is
// for human users logging into the dashboard through a browser -- unlike
// the token-based auth.RequirePermission, which is for API/service callers.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		ctx := context.WithValue(r.Context(), currentSessionKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// SessionFromContext retrieves the session RequireSession put in context,
// if any. Handlers can use this to know which user is making the request.
func SessionFromContext(ctx context.Context) (*auth.Session, bool) {
	v := ctx.Value(currentSessionKey)
	if v == nil {
		return nil, false
	}
	s, ok := v.(*auth.Session)
	return s, ok
}