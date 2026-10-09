package handlers

import (
	"net/http"

	"thyris-sz/internal/audit"
	"thyris-sz/internal/middleware"
	"thyris-sz/internal/models"
)

// recordManagement writes an audit row for a successful management action
// (pattern, allowlist, blacklist, guardrail changes).
//
// The actor is the signed-in admin when the route middleware put a session
// in the context. A call made with an API token has no session, so it is
// recorded as models.AuditActorAPIToken; the token itself is never stored.
//
// details must name the record (for example its id), never a value that
// could be personal data such as an allowlist entry.
func recordManagement(r *http.Request, action, details string) {
	if session, ok := middleware.SessionFromContext(r.Context()); ok {
		id := session.UserID
		audit.RecordWithDetails(r, &id, session.Email, action, models.AuditStatusSuccess, details)
		return
	}

	audit.RecordWithDetails(r, nil, models.AuditActorAPIToken, action, models.AuditStatusSuccess, details)
}
