package repository

import (
    "errors"

    "gorm.io/gorm"
    "github.com/uber-clone/auth-service/internal/model"
)

type AuthRepository struct {
    db *gorm.DB
}

func NewAuthRepository(db *gorm.DB) *AuthRepository {
    return &AuthRepository{db: db}
}

func (r *AuthRepository) CreateUser(user *model.User) error {
    return r.db.Create(user).Error
}

func (r *AuthRepository) GetUserByEmail(email string) (*model.User, error) {
    var user model.User
    err := r.db.Where("email = ?", email).First(&user).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, nil
    }
    return &user, err
}

func (r *AuthRepository) GetUserByID(id string) (*model.User, error) {
    var user model.User
    err := r.db.Where("id = ?", id).First(&user).Error
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, nil
    }
    return &user, err
}

func (r *AuthRepository) UpdateRefreshToken(userID, refreshToken string) error {
    return r.db.Model(&model.User{}).Where("id = ?", userID).Update("refresh_token", refreshToken).Error
}