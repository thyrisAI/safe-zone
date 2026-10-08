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

// CreateBlacklistItem adds a new value to the blocklist
func CreateBlacklistItem(w http.ResponseWriter, r *http.Request) {
	var item models.BlacklistItem
	if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if result := database.DB.Create(&item); result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	cache.ClearCache(cache.KeyBlocklist)

	// Only the entry number is logged: the value itself may be personal data.
	recordManagement(r, models.AuditActionBlacklistAdded, fmt.Sprintf("blacklist entry #%d", item.ID))

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(item)
}

// ListBlacklistItems returns all blocklist items
func ListBlacklistItems(w http.ResponseWriter, r *http.Request) {
	var items []models.BlacklistItem
	database.DB.Find(&items)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// DeleteBlacklistItem removes a value from the blocklist
func DeleteBlacklistItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	result := database.DB.Delete(&models.BlacklistItem{}, id)
	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	cache.ClearCache(cache.KeyBlocklist)

	// Deleting an id that does not exist changes nothing, so it is not logged.
	if result.RowsAffected > 0 {
		recordManagement(r, models.AuditActionBlacklistRemoved, fmt.Sprintf("blacklist entry #%d", id))
	}

	w.WriteHeader(http.StatusNoContent)
}
