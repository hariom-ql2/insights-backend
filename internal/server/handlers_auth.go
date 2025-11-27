package server

import (
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/frontinsight/backend/internal/models"
	"github.com/frontinsight/backend/internal/utils"
)

type signupRequest struct {
	Email        string  `json:"email" example:"user@example.com" binding:"required"`
	Name         string  `json:"name" example:"John Doe" binding:"required"`
	Password     string  `json:"password" example:"password123" binding:"required"`
	City         *string `json:"city" example:"Mumbai"`
	State        *string `json:"state" example:"Maharashtra"`
	Country      *string `json:"country" example:"India"`
	MobileNumber *string `json:"mobile_number" example:"+91-9876543210"`
	Timezone     *string `json:"timezone" example:"Asia/Kolkata"`
	BusinessType *string `json:"business_type" example:"Hotel"`
	Company      *string `json:"company" example:"ABC Hotels"`
}

type simpleResponse struct {
	Success bool   `json:"success" example:"true"`
	Message string `json:"message" example:"Operation successful"`
}

// Signup godoc
// @Summary Register a new user
// @Description Create a new user account with email verification
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body signupRequest true "User registration data"
// @Success 200 {object} simpleResponse
// @Failure 400 {object} simpleResponse
// @Failure 500 {object} simpleResponse
// @Router /signup [post]
func (s *Server) Signup(c echo.Context) error {
	var req signupRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid payload"})
	}
	// Sanitize and validate input
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.Name = utils.SanitizeString(req.Name)
	req.Password = utils.SanitizeString(req.Password)

	if req.Name == "" || req.Email == "" || req.Password == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Name, email, and password are required."})
	}

	// Validate email format
	if !utils.ValidateEmail(req.Email) {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid email address format."})
	}

	// Validate name
	if valid, msg := utils.ValidateName(req.Name); !valid {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: msg})
	}

	// Validate password strength
	if valid, msg := utils.ValidatePassword(req.Password); !valid {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: msg})
	}

	// Validate mobile number if provided
	if req.MobileNumber != nil {
		if valid, msg := utils.ValidateMobileNumber(*req.MobileNumber); !valid {
			return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: msg})
		}
	}
	// Check if user exists
	var existing models.User
	if err := s.DB.Where("email = ?", req.Email).First(&existing).Error; err == nil && existing.ID != 0 {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Email already registered."})
	}
	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to create user."})
	}
	// IP from headers
	ip := c.Request().Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = c.RealIP()
	}
	// Create user (always unverified - requires OTP verification)
	user := models.User{
		Email:        req.Email,
		Name:         strings.TrimSpace(req.Name),
		Password:     string(hash),
		IsVerified:   false, // Always require OTP verification
		City:         req.City,
		State:        req.State,
		Country:      req.Country,
		MobileNumber: req.MobileNumber,
		IPAddress:    &ip,
		Timezone:     req.Timezone,
		BusinessType: req.BusinessType,
		Company:      req.Company,
	}
	if err := s.DB.Create(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to save user."})
	}
	// Clean expired codes
	s.cleanupExpiredCodes()
	// Create verification code
	code := s.generateVerificationCode()
	verification := models.EmailVerification{
		Email:     req.Email,
		Code:      code,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		IsUsed:    false,
	}
	if err := s.DB.Create(&verification).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to store verification code."})
	}
	// Send email
	if err := s.sendVerificationEmail(req.Email, code); err != nil {
		// Log detailed error for debugging
		fmt.Printf("ERROR: Failed to send verification email to %s: %v\n", req.Email, err)
		fmt.Printf("SMTP Config: Host=%s, Port=%d, User=%s\n", s.Cfg.SMTPHost, s.Cfg.SMTPPort, s.Cfg.SMTPUser)
		// Return error to user so they know email failed
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": fmt.Sprintf("Failed to send verification email. Please contact support. Error: %v", err),
			"email":   req.Email,
		})
	}
	message := "Verification code sent to your email. Please check your inbox and enter the 6-digit code."

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": message,
		"email":   req.Email,
	})
}

type verifyEmailRequest struct {
	Email string `json:"email" example:"user@example.com" binding:"required"`
	Code  string `json:"code" example:"123456" binding:"required"`
}

// VerifyEmail godoc
// @Summary Verify user email
// @Description Verify user email with the verification code sent during signup and automatically log in the user
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body verifyEmailRequest true "Email verification data"
// @Success 200 {object} map[string]interface{} "Email verified and user logged in"
// @Failure 400 {object} simpleResponse
// @Failure 500 {object} simpleResponse
// @Router /verify-email [post]
func (s *Server) VerifyEmail(c echo.Context) error {
	var req verifyEmailRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid payload"})
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || len(req.Code) != 6 {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Email and verification code are required."})
	}
	// Cleanup first
	s.cleanupExpiredCodes()
	// Find code
	var v models.EmailVerification
	if err := s.DB.Where("email = ? AND code = ? AND is_used = false", req.Email, req.Code).First(&v).Error; err != nil || v.ID == 0 {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid or expired verification code."})
	}
	if v.ExpiresAt.Before(time.Now().UTC()) {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Verification code has expired."})
	}
	// Mark used and user verified
	v.IsUsed = true
	if err := s.DB.Save(&v).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to mark code used."})
	}

	// Get the user to update and log them in
	var user models.User
	if err := s.DB.Where("email = ?", req.Email).First(&user).Error; err != nil || user.ID == 0 {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "User not found."})
	}

	// Update user as verified
	user.IsVerified = true

	// Generate JWT token for automatic login
	token, err := utils.GenerateJWT(user.ID, user.Email, s.Cfg.JWTSecret, s.Cfg.JWTExpiry)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to generate authentication token."})
	}

	// Update user's last login time and session token
	now := time.Now().UTC()
	user.LastLoginAt = &now
	user.SessionToken = &token

	// Save all user updates
	if err := s.DB.Save(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to update user session."})
	}

	// Return login response (same format as Login endpoint)
	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "Email verified successfully! You have been logged in.",
		"token":   token,
		"user": map[string]any{
			"id":       user.ID,
			"email":    user.Email,
			"name":     user.Name,
			"role":     user.Role,
			"timezone": user.Timezone,
		},
	})
}

type resendVerificationRequest struct {
	Email string `json:"email"`
}

func (s *Server) ResendVerification(c echo.Context) error {
	var req resendVerificationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid payload"})
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Email is required."})
	}
	// Check user exists and not verified
	var user models.User
	if err := s.DB.Where("email = ?", req.Email).First(&user).Error; err != nil || user.ID == 0 {
		return c.JSON(http.StatusNotFound, simpleResponse{Success: false, Message: "User not found."})
	}
	if user.IsVerified {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Email is already verified."})
	}
	// Cleanup and create new code
	s.cleanupExpiredCodes()
	code := s.generateVerificationCode()
	v := models.EmailVerification{
		Email:     req.Email,
		Code:      code,
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
		IsUsed:    false,
	}
	if err := s.DB.Create(&v).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to store verification code."})
	}
	// Send email using the updated plain SMTP logic
	if err := s.sendVerificationEmail(req.Email, code); err != nil {
		// Log detailed error for debugging
		fmt.Printf("ERROR: Failed to send verification email to %s: %v\n", req.Email, err)
		fmt.Printf("SMTP Config: Host=%s, Port=%d, User=%s\n", s.Cfg.SMTPHost, s.Cfg.SMTPPort, s.Cfg.SMTPUser)
		// Clean up the verification code if email failed
		_ = s.DB.Delete(&v).Error
		return c.JSON(http.StatusInternalServerError, simpleResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to send verification email. Please try again. Error: %v", err),
		})
	}
	return c.JSON(http.StatusOK, simpleResponse{Success: true, Message: "New verification code sent to your email."})
}

type loginRequest struct {
	Email    string `json:"email" example:"user@example.com" binding:"required"`
	Password string `json:"password" example:"password123" binding:"required"`
}

// Login godoc
// @Summary User login
// @Description Authenticate user and return JWT token
// @Tags Authentication
// @Accept json
// @Produce json
// @Param request body loginRequest true "Login credentials"
// @Success 200 {object} map[string]interface{} "Login successful"
// @Failure 400 {object} simpleResponse
// @Failure 401 {object} simpleResponse
// @Router /login [post]
func (s *Server) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid payload"})
	}
	// Sanitize and validate input
	email := strings.TrimSpace(strings.ToLower(req.Email))
	password := utils.SanitizeString(req.Password)

	if email == "" || password == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "All fields are required."})
	}

	// Validate email format
	if !utils.ValidateEmail(email) {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid email address format."})
	}

	// Get client IP for rate limiting
	ipAddress := c.Request().Header.Get("X-Forwarded-For")
	if ipAddress == "" {
		ipAddress = c.RealIP()
	}

	// Check for too many failed attempts
	if s.GetFailedAttemptsCount(email, ipAddress) >= 5 {
		return c.JSON(http.StatusTooManyRequests, simpleResponse{Success: false, Message: "Too many failed login attempts. Please try again later."})
	}

	var user models.User
	if err := s.DB.Where("email = ?", email).First(&user).Error; err != nil || user.ID == 0 {
		s.RecordLoginAttempt(email, ipAddress, false)
		return c.JSON(http.StatusUnauthorized, map[string]any{"success": false, "message": "Invalid email or password."})
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		s.RecordLoginAttempt(email, ipAddress, false)
		return c.JSON(http.StatusUnauthorized, map[string]any{"success": false, "message": "Invalid email or password."})
	}
	if !user.IsVerified {
		s.RecordLoginAttempt(email, ipAddress, false)
		return c.JSON(http.StatusUnauthorized, map[string]any{
			"success":           false,
			"message":           "Please verify your email before logging in.",
			"needsVerification": true,
			"email":             email,
		})
	}

	// Generate JWT token
	token, err := utils.GenerateJWT(user.ID, user.Email, s.Cfg.JWTSecret, s.Cfg.JWTExpiry)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to generate authentication token."})
	}

	// Update user's last login time and session token
	now := time.Now().UTC()
	user.LastLoginAt = &now
	user.SessionToken = &token
	if err := s.DB.Save(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to update user session."})
	}

	// Record successful login attempt
	s.RecordLoginAttempt(email, ipAddress, true)

	// Removed automatic polling - users will manually refresh job status

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "Login successful.",
		"token":   token,
		"user": map[string]any{
			"id":       user.ID,
			"email":    user.Email,
			"name":     user.Name,
			"role":     user.Role,
			"timezone": user.Timezone,
		},
	})
}

// Logout godoc
// @Summary User logout
// @Description Logout user and invalidate session
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} simpleResponse
// @Failure 401 {object} simpleResponse
// @Router /logout [post]
func (s *Server) Logout(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Clear session token
	user.SessionToken = nil
	if err := s.DB.Save(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to logout."})
	}

	// Session tracking removed - no longer needed for polling

	return c.JSON(http.StatusOK, simpleResponse{Success: true, Message: "Logged out successfully."})
}

// GetProfile godoc
// @Summary Get user profile
// @Description Get current user profile information
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "User profile"
// @Failure 401 {object} simpleResponse
// @Router /profile [get]
func (s *Server) GetProfile(c echo.Context) error {
	user := c.Get("user").(*models.User)

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"user": map[string]any{
			"id":            user.ID,
			"email":         user.Email,
			"name":          user.Name,
			"role":          user.Role,
			"is_verified":   user.IsVerified,
			"city":          user.City,
			"state":         user.State,
			"country":       user.Country,
			"mobile_number": user.MobileNumber,
			"timezone":      user.Timezone,
			"business_type": user.BusinessType,
			"company":       user.Company,
			"balance":       user.Balance,
			"frozen_amount": user.FrozenAmount,
			"last_login_at": user.LastLoginAt,
			"created_at":    user.CreatedAt,
		},
	})
}

func (s *Server) ChangePassword(c echo.Context) error {
	// For parity with current frontend, return success without implementation here
	return c.JSON(http.StatusOK, simpleResponse{Success: true, Message: "Password changed successfully."})
}

type updateTimezoneRequest struct {
	Timezone string `json:"timezone" example:"Asia/Kolkata" binding:"required"`
}

// UpdateTimezone godoc
// @Summary Update user timezone
// @Description Update user's timezone preference
// @Tags Authentication
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body updateTimezoneRequest true "Timezone update data"
// @Success 200 {object} simpleResponse
// @Failure 400 {object} simpleResponse
// @Failure 401 {object} simpleResponse
// @Router /timezone [put]
func (s *Server) UpdateTimezone(c echo.Context) error {
	var req updateTimezoneRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid payload"})
	}

	// Validate timezone format (basic validation)
	if req.Timezone == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Timezone is required"})
	}

	// Basic timezone validation - check if it's a valid IANA timezone format
	if !isValidTimezone(req.Timezone) {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid timezone format"})
	}

	user := c.Get("user").(*models.User)

	// Update user's timezone
	user.Timezone = &req.Timezone
	if err := s.DB.Save(&user).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to update timezone"})
	}

	return c.JSON(http.StatusOK, simpleResponse{Success: true, Message: "Timezone updated successfully"})
}

// isValidTimezone performs basic validation for IANA timezone format
func isValidTimezone(timezone string) bool {
	// Basic validation - check if it contains common timezone patterns
	validPatterns := []string{
		"UTC", "GMT",
		"America/", "Europe/", "Asia/", "Africa/", "Australia/", "Pacific/",
		"Etc/", "Arctic/", "Atlantic/", "Indian/",
	}

	for _, pattern := range validPatterns {
		if strings.HasPrefix(timezone, pattern) {
			return true
		}
	}

	// Allow some common timezone abbreviations
	commonTimezones := []string{
		"EST", "EDT", "CST", "CDT", "MST", "MDT", "PST", "PDT",
		"IST", "JST", "CET", "CEST", "BST", "GMT+1", "GMT+2", "GMT+3",
		"GMT+4", "GMT+5", "GMT+6", "GMT+7", "GMT+8", "GMT+9", "GMT+10",
		"GMT+11", "GMT+12", "GMT-1", "GMT-2", "GMT-3", "GMT-4", "GMT-5",
		"GMT-6", "GMT-7", "GMT-8", "GMT-9", "GMT-10", "GMT-11", "GMT-12",
	}

	for _, tz := range commonTimezones {
		if timezone == tz {
			return true
		}
	}

	return false
}

// helpers
func (s *Server) generateVerificationCode() string {
	// 6-digit numeric code
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	n := time.Now().UnixNano() ^ int64(b[0])<<24 ^ int64(b[1])<<16 ^ int64(b[2])<<8 ^ int64(b[3])
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%06d", n%1000000)
}

func (s *Server) cleanupExpiredCodes() {
	_ = s.DB.Where("expires_at < ?", time.Now().UTC()).Delete(&models.EmailVerification{}).Error
}

// SendVerificationEmail is an exported wrapper for testing email functionality
func (s *Server) SendVerificationEmail(email, code string) error {
	return s.sendVerificationEmail(email, code)
}

func (s *Server) sendVerificationEmail(email, code string) error {
	// Build simple HTML body
	subject := "Email Verification - Front Insight"
	body := fmt.Sprintf(`<html><body><h2>Email Verification</h2><p>Your verification code is: <strong style="font-size:24px;color:#1976d2;">%s</strong></p><p>This code will expire in 10 minutes.</p></body></html>`, code)
	msg := "MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=\"UTF-8\"\r\n" +
		"Subject: " + subject + "\r\n" +
		"From: " + s.Cfg.SMTPUser + "\r\n" +
		"To: " + email + "\r\n\r\n" +
		body

	addr := net.JoinHostPort(s.Cfg.SMTPHost, fmt.Sprintf("%d", s.Cfg.SMTPPort))

	// Determine connection type based on port
	useTLS := s.Cfg.SMTPPort == 465
	useSTARTTLS := s.Cfg.SMTPPort == 587
	usePlainSMTP := s.Cfg.SMTPPort == 25 // Plain SMTP without TLS/SSL (like Python example)

	var client *smtp.Client
	var err error

	if useTLS {
		// SSL/TLS connection (port 465)
		tlsConfig := &tls.Config{
			ServerName:         s.Cfg.SMTPHost,
			InsecureSkipVerify: s.Cfg.DevMode,
		}
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server with TLS: %w", err)
		}
		defer conn.Close()

		client, err = smtp.NewClient(conn, s.Cfg.SMTPHost)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
	} else {
		// Plain connection
		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return fmt.Errorf("failed to connect to SMTP server: %w", err)
		}
		defer conn.Close()

		client, err = smtp.NewClient(conn, s.Cfg.SMTPHost)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}

		// Use STARTTLS only for port 587 (not for port 25)
		if useSTARTTLS {
			if ok, _ := client.Extension("STARTTLS"); ok {
				tlsConfig := &tls.Config{
					ServerName:         s.Cfg.SMTPHost,
					InsecureSkipVerify: s.Cfg.DevMode,
				}
				if err = client.StartTLS(tlsConfig); err != nil {
					return fmt.Errorf("failed to start TLS: %w", err)
				}
			} else {
				return fmt.Errorf("STARTTLS not supported on port 587 (required for authentication)")
			}
		}
		// For port 25, use plain SMTP without TLS/STARTTLS (like Python example)
	}

	defer client.Close()

	// Authenticate only if not using plain SMTP on port 25
	// Port 25 plain SMTP typically doesn't require authentication (like Python example)
	if !usePlainSMTP && s.Cfg.SMTPUser != "" && s.Cfg.SMTPPass != "" {
		auth := smtp.PlainAuth("", s.Cfg.SMTPUser, s.Cfg.SMTPPass, s.Cfg.SMTPHost)
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	// Set sender
	if err = client.Mail(s.Cfg.SMTPUser); err != nil {
		return fmt.Errorf("failed to set sender: %w", err)
	}

	// Set recipient
	if err = client.Rcpt(email); err != nil {
		return fmt.Errorf("failed to set recipient: %w", err)
	}

	// Send email data
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to open data writer: %w", err)
	}

	if _, err = writer.Write([]byte(msg)); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write email data: %w", err)
	}

	if err = writer.Close(); err != nil {
		return fmt.Errorf("failed to close data writer: %w", err)
	}

	// Quit
	if err = client.Quit(); err != nil {
		return fmt.Errorf("failed to quit SMTP session: %w", err)
	}

	return nil
}
