package model

import "time"

type User struct {
    ID        string    `gorm:"primaryKey"`
    Email     string    `gorm:"uniqueIndex;not null"`
    Phone     string    `gorm:"uniqueIndex;not null"`
    Password  string    `gorm:"not null"`
    Role      string    `gorm:"default:'rider'"`
    RefreshToken string
    CreatedAt time.Time
    UpdatedAt time.Time
}