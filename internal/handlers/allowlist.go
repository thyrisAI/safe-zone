package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/database"
	"thyris-sz/internal/models"
)

func CreateAllowlistItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var item models.AllowlistItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if result := database.DB.Create(&item); result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	cache.ClearCache(cache.KeyAllowlist)

	// Only the entry number is logged: the value itself may be personal data.
	recordManagement(r, models.AuditActionAllowlistAdded, fmt.Sprintf("allowlist entry #%d", item.ID))

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

func ListAllowlistItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var items []models.AllowlistItem
	if result := database.DB.Find(&items); result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func DeleteAllowlistItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	result := database.DB.Delete(&models.AllowlistItem{}, id)
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	cache.ClearCache(cache.KeyAllowlist)

	// Deleting an id that does not exist changes nothing, so it is not logged.
	if result.RowsAffected > 0 {
		recordManagement(r, models.AuditActionAllowlistRemoved, fmt.Sprintf("allowlist entry #%d", id))
	}

	w.WriteHeader(http.StatusNoContent)
}
