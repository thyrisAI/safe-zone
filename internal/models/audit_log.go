package models

import "time"

// Audit actions and statuses. Kept as constants so a typo can never
// create an inconsistent value in the table.
const (
	AuditActionLogin          = "login"
	AuditActionLogout         = "logout"
	AuditActionPasswordChange = "password_change"
	AuditActionUserCreated    = "user_created"

	// Management actions on the protection rules.
	AuditActionPatternCreated   = "pattern_created"
	AuditActionPatternDeleted   = "pattern_deleted"
	AuditActionPatternEnabled   = "pattern_enabled"
	AuditActionPatternDisabled  = "pattern_disabled"
	AuditActionAllowlistAdded   = "allowlist_added"
	AuditActionAllowlistRemoved = "allowlist_removed"
	AuditActionBlacklistAdded   = "blacklist_added"
	AuditActionBlacklistRemoved = "blacklist_removed"
	AuditActionGuardrailCreated = "guardrail_created"
	AuditActionGuardrailDeleted = "guardrail_deleted"

	// A guardrail switched off or on without being deleted.
	AuditActionGuardrailEnabled  = "guardrail_enabled"
	AuditActionGuardrailDisabled = "guardrail_disabled"

	// AuditActionAccessDenied records a signed-in user trying something
	// their role does not allow (HTTP 403).
	AuditActionAccessDenied = "access_denied"

	// AuditActorAPIToken is the actor name for a management action made with
	// an API token instead of a dashboard session. The token itself is never
	// stored. It is also what appears if AUTH_ENABLED=false, where no
	// identity exists at all.
	AuditActorAPIToken = "api-token"

	AuditStatusSuccess = "success"
	AuditStatusFailure = "failure"
)

// AuditLog is an append-only record of a security-relevant user action.
// It deliberately does not use gorm.Model: audit rows must never be
// soft-deleted or updated. Never store passwords or request bodies here.
type AuditLog struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	UserID     *uint  `gorm:"index" json:"user_id,omitempty"` // nil when the email matches no user
	ActorEmail string `gorm:"size:254;index;not null" json:"actor_email"`
	Action     string `gorm:"size:50;not null" json:"action"`
	Status     string `gorm:"size:20;not null" json:"status"`
	IPAddress  string `gorm:"size:45" json:"ip_address"` // 45 = longest IPv6 text form
	// Details names what the action was done to, e.g. the account an admin
	// created. Short free text; never a password or request body.
	Details   string    `gorm:"size:255" json:"details,omitempty"`
	CreatedAt time.Time `gorm:"index;not null" json:"created_at"`
}
