package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"

	"gorm.io/gorm"
)

// CreateValidator handles the creation of a new format validator
func CreateValidator(w http.ResponseWriter, r *http.Request) {
	var validator models.FormatValidator
	if err := json.NewDecoder(r.Body).Decode(&validator); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := repository.CreateFormatValidator(&validator); err != nil {
		http.Error(w, "Failed to create validator: "+err.Error(), http.StatusInternalServerError)
		return
	}

	recordManagement(r, models.AuditActionGuardrailCreated, fmt.Sprintf("%s (#%d)", validator.Name, validator.ID))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(validator)
}

// ListValidators returns all format validators
func ListValidators(w http.ResponseWriter, r *http.Request) {
	validators, err := repository.ListFormatValidators()
	if err != nil {
		http.Error(w, "Failed to list validators", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validators)
}

// DeleteValidator removes a validator by ID
func DeleteValidator(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := repository.DeleteFormatValidator(uint(id)); err != nil {
		http.Error(w, "Failed to delete validator", http.StatusInternalServerError)
		return
	}

	recordManagement(r, models.AuditActionGuardrailDeleted, fmt.Sprintf("#%d", id))

	w.WriteHeader(http.StatusNoContent)
}

// UpdateValidatorActive switches a guardrail on or off without deleting it.
// PATCH /validators/{id}  body: {"is_active": true|false}
func UpdateValidatorActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
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

	validator, err := repository.SetFormatValidatorActive(uint(id), *req.IsActive)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "Guardrail not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to update guardrail", http.StatusInternalServerError)
		return
	}

	action := models.AuditActionGuardrailDisabled
	if *req.IsActive {
		action = models.AuditActionGuardrailEnabled
	}
	recordManagement(r, action, fmt.Sprintf("%s (#%d)", validator.Name, validator.ID))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validator)
}
