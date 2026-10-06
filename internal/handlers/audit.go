package handlers

import (
	"encoding/json"
	"net/http"

	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
)

// auditLogLimit caps how many records one request can return. MVP: no
// pagination or filters yet.
const auditLogLimit = 100

// ListAuditLogs returns the most recent audit records, newest first.
// Access control (admin session only) is enforced by the route middleware.
func ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logs, err := repository.ListAuditLogs(auditLogLimit)
	if err != nil {
		http.Error(w, "Failed to list audit logs", http.StatusInternalServerError)
		return
	}

	// Always answer with a JSON array: "null" would break the frontend.
	if logs == nil {
		logs = []models.AuditLog{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}