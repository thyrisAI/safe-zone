package repository

import (
	"thyris-sz/internal/database"
	"thyris-sz/internal/models"
)

// GetUserByEmail looks up a user by email. Returns gorm.ErrRecordNotFound
// (wrapped) if no such user exists.
func GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	result := database.DB.Where("email = ?", email).First(&user)
	if result.Error != nil {
		return nil, result.Error
	}
	return &user, nil
}

// CreateUser persists a new user.
func CreateUser(user *models.User) error {
	return database.DB.Create(user).Error
}