// Package audit records security-relevant user actions (login, logout, ...)
// into the audit_logs table. It must never store passwords or request bodies.
package audit

import (
	"log"
	"net"
	"net/http"

	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
)

const (
	maxEmailLength = 254 // matches the actor_email column size
	maxIPLength    = 45  // longest textual IPv6 address
)

// Truncate shortens s to at most max characters (not bytes), so a
// user-controlled value can never overflow a column or bloat the table.
func Truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

// ClientIP returns the caller's IP without the port. It uses the direct
// connection address only: X-Forwarded-For is client-controlled and is
// not trusted here (see V3.1 analysis notes).
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return Truncate(host, maxIPLength)
}

// Record writes one audit entry. It is best effort: if the write fails
// the error is logged and swallowed, so an audit problem can never block
// a login or logout. userID is nil when the email matches no user.
func Record(r *http.Request, userID *uint, email, action, status string) {
	entry := &models.AuditLog{
		UserID:     userID,
		ActorEmail: Truncate(email, maxEmailLength),
		Action:     action,
		Status:     status,
		IPAddress:  ClientIP(r),
	}

	if err := repository.CreateAuditLog(entry); err != nil {
		// Log only action/status/error, never the email: it is user
		// input and could be used to inject fake lines into the log.
		log.Printf("audit: failed to record %s/%s: %v", action, status, err)
	}
}