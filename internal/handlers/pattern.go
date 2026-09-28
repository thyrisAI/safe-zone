package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/database"
	"thyris-sz/internal/models"
)

func CreatePattern(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var pattern models.Pattern
	if err := json.NewDecoder(r.Body).Decode(&pattern); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if result := database.DB.Create(&pattern); result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	cache.ClearCache(cache.KeyPatterns)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(pattern)
}

func ListPatterns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var patterns []models.Pattern
	if result := database.DB.Find(&patterns); result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(patterns)
}

func DeletePattern(w http.ResponseWriter, r *http.Request) {
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

	if result := database.DB.Delete(&models.Pattern{}, id); result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// Invalidate cache
	cache.ClearCache(cache.KeyPatterns)

	w.WriteHeader(http.StatusNoContent)
}


// UpdatePatternActive toggles only the IsActive flag of a pattern.
// PATCH /patterns/{id}  body: {"is_active": true|false}
func UpdatePatternActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	var req struct {
		IsActive *bool `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.IsActive == nil {
		http.Error(w, "is_active is required", http.StatusBadRequest)
		return
	}

	var pattern models.Pattern
	if result := database.DB.First(&pattern, id); result.Error != nil {
		http.Error(w, "Pattern not found", http.StatusNotFound)
		return
	}

	if result := database.DB.Model(&pattern).Update("is_active", *req.IsActive); result.Error != nil {
		http.Error(w, "Failed to update pattern", http.StatusInternalServerError)
		return
	}

	// Invalidate cache so the change takes effect immediately
	cache.ClearCache(cache.KeyPatterns)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pattern)
}