package utils

import (
	"regexp"
	"strings"
	"unicode"
)

// ValidateEmail validates email format
func ValidateEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// ValidatePassword validates password strength
func ValidatePassword(password string) (bool, string) {
	if len(password) < 8 {
		return false, "Password must be at least 8 characters long"
	}

	if len(password) > 128 {
		return false, "Password must be less than 128 characters"
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsDigit(char):
			hasDigit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	if !hasUpper {
		return false, "Password must contain at least one uppercase letter"
	}
	if !hasLower {
		return false, "Password must contain at least one lowercase letter"
	}
	if !hasDigit {
		return false, "Password must contain at least one digit"
	}
	if !hasSpecial {
		return false, "Password must contain at least one special character"
	}

	return true, ""
}

// SanitizeString removes potentially dangerous characters
func SanitizeString(input string) string {
	// Remove null bytes and control characters
	input = strings.ReplaceAll(input, "\x00", "")
	input = strings.ReplaceAll(input, "\r", "")
	input = strings.ReplaceAll(input, "\n", "")
	input = strings.ReplaceAll(input, "\t", "")

	// Trim whitespace
	input = strings.TrimSpace(input)

	return input
}

// ValidateName validates name format
func ValidateName(name string) (bool, string) {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return false, "Name must be at least 2 characters long"
	}
	if len(name) > 100 {
		return false, "Name must be less than 100 characters"
	}

	// Check for valid characters (letters, spaces, hyphens, apostrophes)
	for _, char := range name {
		if !unicode.IsLetter(char) && char != ' ' && char != '-' && char != '\'' {
			return false, "Name contains invalid characters"
		}
	}

	return true, ""
}

// ValidateMobileNumber validates mobile number format
func ValidateMobileNumber(mobile string) (bool, string) {
	if mobile == "" {
		return true, "" // Optional field
	}

	// Remove all non-digit characters
	digits := regexp.MustCompile(`\D`).ReplaceAllString(mobile, "")

	if len(digits) < 10 || len(digits) > 15 {
		return false, "Mobile number must be between 10 and 15 digits"
	}

	return true, ""
}
