package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
)

// auditLogLimit caps how many records one request can return. No
// pagination yet: filters narrow the result instead.
const auditLogLimit = 100

// ListAuditLogs returns the most recent audit records, newest first.
// Access control (admin session only) is enforced by the route middleware.
//
// Optional query parameters:
//
//	user_id=<number>  only that registered user's records
//	user_id=none      only records whose email matched no user
//	status=success|failure
func ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	filter := repository.AuditLogFilter{}
	query := r.URL.Query()

	switch raw := query.Get("user_id"); raw {
	case "":
		// no user filter
	case "none":
		filter.UnregisteredOnly = true
	default:
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}
		userID := uint(id)
		filter.UserID = &userID
	}

	switch status := query.Get("status"); status {
	case "", models.AuditStatusSuccess, models.AuditStatusFailure:
		filter.Status = status
	default:
		http.Error(w, "Invalid status", http.StatusBadRequest)
		return
	}

	logs, err := repository.ListAuditLogs(filter, auditLogLimit)
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
