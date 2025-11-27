package server

import (
	"time"

	"github.com/frontinsight/backend/internal/models"
)

// GetFailedAttemptsCount returns the number of failed login attempts for an email/IP in the last hour
func (s *Server) GetFailedAttemptsCount(email, ipAddress string) int64 {
	var count int64
	oneHourAgo := time.Now().Add(-1 * time.Hour)
	s.DB.Model(&models.LoginAttempt{}).
		Where("email = ? AND ip_address = ? AND success = false AND created_at > ?", email, ipAddress, oneHourAgo).
		Count(&count)
	return count
}

// RecordLoginAttempt records a login attempt
func (s *Server) RecordLoginAttempt(email, ipAddress string, success bool) {
	attempt := models.LoginAttempt{
		Email:     email,
		IPAddress: ipAddress,
		Success:   success,
	}
	s.DB.Create(&attempt)
}

// CleanupOldAttempts removes login attempts older than 24 hours
func (s *Server) CleanupOldAttempts() {
	oneDayAgo := time.Now().Add(-24 * time.Hour)
	s.DB.Where("created_at < ?", oneDayAgo).Delete(&models.LoginAttempt{})
}
