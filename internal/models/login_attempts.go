package models

import (
	"time"
)

type LoginAttempt struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Email     string    `gorm:"not null;index" json:"email"`
	IPAddress string    `gorm:"not null;index" json:"ip_address"`
	Success   bool      `gorm:"not null" json:"success"`
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// These functions will be moved to the server package
