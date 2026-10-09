package repository

import (
	"thyris-sz/internal/database"
	"thyris-sz/internal/models"
)

// CreateAuditLog persists one audit record.
func CreateAuditLog(entry *models.AuditLog) error {
	return database.DB.Create(entry).Error
}

// AuditLogFilter narrows ListAuditLogs. The zero value matches everything.
type AuditLogFilter struct {
	UserID           *uint  // only records of this registered user
	UnregisteredOnly bool   // only records whose email matched no user
	Status           string // "success", "failure", or "" for any
}

// ListAuditLogs returns the most recent audit records matching the filter,
// newest first. Filtering happens in SQL (not in the client) so a rarely
// active user is never hidden by the row limit.
func ListAuditLogs(filter AuditLogFilter, limit int) ([]models.AuditLog, error) {
	query := database.DB.Order("created_at DESC").Limit(limit)

	if filter.UnregisteredOnly {
		query = query.Where("user_id IS NULL")
	} else if filter.UserID != nil {
		query = query.Where("user_id = ?", *filter.UserID)
	}

	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}

	var logs []models.AuditLog
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}
