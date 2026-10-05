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

// ListUsers returns all users, ordered by creation date.
func ListUsers() ([]models.User, error) {
	var users []models.User
	result := database.DB.Order("created_at").Find(&users)
	if result.Error != nil {
		return nil, result.Error
	}
	return users, nil
}