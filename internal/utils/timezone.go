package utils

import (
	"time"
)

// UTCLocation returns the UTC timezone location
func UTCLocation() *time.Location {
	return time.UTC
}

// NowUTC returns the current time in UTC
func NowUTC() time.Time {
	return time.Now().UTC()
}

// ParseUTC parses a time string in UTC timezone
func ParseUTC(layout, value string) (time.Time, error) {
	return time.ParseInLocation(layout, value, time.UTC)
}

// FormatUTC formats a time to string in UTC timezone
func FormatUTC(t time.Time, layout string) string {
	return t.UTC().Format(layout)
}

// ToUTC converts any time to UTC
func ToUTC(t time.Time) time.Time {
	return t.UTC()
}

// ParseUserTimezone parses a time string in user's timezone and returns UTC
func ParseUserTimezone(timeStr, userTimezone string) (time.Time, error) {
	if userTimezone == "" {
		userTimezone = "UTC"
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return time.Time{}, err
	}

	// Parse the time string in the user's timezone
	userTime, err := time.ParseInLocation(time.RFC3339, timeStr, loc)
	if err != nil {
		// Try parsing as a simple date-time format
		userTime, err = time.ParseInLocation("2006-01-02 15:04:05", timeStr, loc)
		if err != nil {
			return time.Time{}, err
		}
	}

	// Convert to UTC
	return userTime.UTC(), nil
}

// FormatForUserTimezone formats a UTC time for display in user's timezone
func FormatForUserTimezone(utcTime time.Time, userTimezone string, format string) (string, error) {
	if userTimezone == "" {
		userTimezone = "UTC"
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return "", err
	}

	// Convert UTC time to user's timezone and format
	userTime := utcTime.In(loc)
	return userTime.Format(format), nil
}

// ISTLocation returns the Indian Standard Time timezone location
func ISTLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		// Fallback to UTC+5:30 if Asia/Kolkata is not available
		return time.FixedZone("IST", 5*60*60+30*60)
	}
	return loc
}

// NowIST returns the current time in Indian Standard Time
func NowIST() time.Time {
	return time.Now().In(ISTLocation())
}
