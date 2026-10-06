package repository

import (
	"thyris-sz/internal/database"
	"thyris-sz/internal/models"
)

// CreateAuditLog persists one audit record.
func CreateAuditLog(entry *models.AuditLog) error {
	return database.DB.Create(entry).Error
}

// ListAuditLogs returns the most recent audit records, newest first.
func ListAuditLogs(limit int) ([]models.AuditLog, error) {
	var logs []models.AuditLog
	result := database.DB.Order("created_at DESC").Limit(limit).Find(&logs)
	if result.Error != nil {
		return nil, result.Error
	}
	return logs, nil
}