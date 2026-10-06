package models

import "time"

// Audit actions and statuses. Kept as constants so a typo can never
// create an inconsistent value in the table.
const (
	AuditActionLogin          = "login"
	AuditActionLogout         = "logout"
	AuditActionPasswordChange = "password_change"

	AuditStatusSuccess = "success"
	AuditStatusFailure = "failure"
)

// AuditLog is an append-only record of a security-relevant user action.
// It deliberately does not use gorm.Model: audit rows must never be
// soft-deleted or updated. Never store passwords or request bodies here.
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     *uint     `gorm:"index" json:"user_id,omitempty"` // nil when the email matches no user
	ActorEmail string    `gorm:"size:254;index;not null" json:"actor_email"`
	Action     string    `gorm:"size:50;not null" json:"action"`
	Status     string    `gorm:"size:20;not null" json:"status"`
	IPAddress  string    `gorm:"size:45" json:"ip_address"` // 45 = longest IPv6 text form
	CreatedAt  time.Time `gorm:"index;not null" json:"created_at"`
}