package services

import (
	"fmt"
	"time"

	"github.com/frontinsight/backend/internal/models"
	"gorm.io/gorm"
)

// TimezoneService handles all timezone-related operations
type TimezoneService struct {
	db *gorm.DB
}

// NewTimezoneService creates a new timezone service
func NewTimezoneService(db *gorm.DB) *TimezoneService {
	return &TimezoneService{db: db}
}

// GetUserTimezone retrieves the user's timezone from the database
func (ts *TimezoneService) GetUserTimezone(userID string) (string, error) {
	var user models.User
	if err := ts.db.Where("email = ?", userID).First(&user).Error; err != nil {
		return "UTC", fmt.Errorf("user not found: %v", err)
	}

	if user.Timezone == nil || *user.Timezone == "" {
		return "UTC", nil
	}

	return *user.Timezone, nil
}

// ConvertToUTC converts a time from user's timezone to UTC
func (ts *TimezoneService) ConvertToUTC(userTime time.Time, userTimezone string) (time.Time, error) {
	if userTimezone == "" {
		userTimezone = "UTC"
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %s: %v", userTimezone, err)
	}

	// Convert the time to UTC
	return userTime.In(loc).UTC(), nil
}

// ConvertFromUTC converts a UTC time to user's timezone
func (ts *TimezoneService) ConvertFromUTC(utcTime time.Time, userTimezone string) (time.Time, error) {
	if userTimezone == "" {
		userTimezone = "UTC"
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %s: %v", userTimezone, err)
	}

	// Convert UTC time to user's timezone
	return utcTime.In(loc), nil
}

// ParseUserTime parses a time string in user's timezone and returns UTC
func (ts *TimezoneService) ParseUserTime(timeStr, userTimezone string) (time.Time, error) {
	if userTimezone == "" {
		userTimezone = "UTC"
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %s: %v", userTimezone, err)
	}

	// Parse the time string in the user's timezone
	userTime, err := time.ParseInLocation(time.RFC3339, timeStr, loc)
	if err != nil {
		// Try parsing as a simple date-time format
		userTime, err = time.ParseInLocation("2006-01-02 15:04:05", timeStr, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("failed to parse time %s: %v", timeStr, err)
		}
	}

	// Convert to UTC
	return userTime.UTC(), nil
}

// FormatForUser formats a UTC time for display in user's timezone
func (ts *TimezoneService) FormatForUser(utcTime time.Time, userTimezone string, format string) (string, error) {
	if userTimezone == "" {
		userTimezone = "UTC"
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return "", fmt.Errorf("invalid timezone %s: %v", userTimezone, err)
	}

	// Convert UTC time to user's timezone and format
	userTime := utcTime.In(loc)
	return userTime.Format(format), nil
}

// GetCurrentUTC returns the current time in UTC
func (ts *TimezoneService) GetCurrentUTC() time.Time {
	return time.Now().UTC()
}

// GetCurrentUserTime returns the current time in user's timezone
func (ts *TimezoneService) GetCurrentUserTime(userTimezone string) (time.Time, error) {
	return ts.ConvertFromUTC(ts.GetCurrentUTC(), userTimezone)
}

// ConvertCollectionToUserTimezone converts a collection's timestamps to user's timezone
func (ts *TimezoneService) ConvertCollectionToUserTimezone(collection *models.Collection, userTimezone string) error {
	// Convert CreatedAt
	if !collection.CreatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(collection.CreatedAt, userTimezone)
		if err != nil {
			return err
		}
		collection.CreatedAt = userTime
	}

	// Convert UpdatedAt
	if !collection.UpdatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(collection.UpdatedAt, userTimezone)
		if err != nil {
			return err
		}
		collection.UpdatedAt = userTime
	}

	// Convert LastRunAt (this is *time.Time)
	if collection.LastRunAt != nil {
		userTime, err := ts.ConvertFromUTC(*collection.LastRunAt, userTimezone)
		if err != nil {
			return err
		}
		collection.LastRunAt = &userTime
	}

	return nil
}

// ConvertCollectionItemToUserTimezone converts collection item timestamps to user's timezone
func (ts *TimezoneService) ConvertCollectionItemToUserTimezone(item *models.CollectionItem, userTimezone string) error {
	// Convert CreatedAt
	if !item.CreatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(item.CreatedAt, userTimezone)
		if err != nil {
			return err
		}
		item.CreatedAt = userTime
	}

	// Convert CheckInDate and CheckOutDate
	if !item.CheckInDate.IsZero() {
		userTime, err := ts.ConvertFromUTC(item.CheckInDate, userTimezone)
		if err != nil {
			return err
		}
		item.CheckInDate = userTime
	}

	if !item.CheckOutDate.IsZero() {
		userTime, err := ts.ConvertFromUTC(item.CheckOutDate, userTimezone)
		if err != nil {
			return err
		}
		item.CheckOutDate = userTime
	}

	return nil
}

// ConvertSearchToUserTimezone converts search timestamps to user's timezone
func (ts *TimezoneService) ConvertSearchToUserTimezone(search *models.Search, userTimezone string) error {
	// Convert CreatedAt
	if !search.CreatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(search.CreatedAt, userTimezone)
		if err != nil {
			return err
		}
		search.CreatedAt = userTime
	}

	// Convert Timestamp field (string format: DD-MM-YYYY HH:MM:SS)
	if search.Timestamp != "" {
		// Parse the timestamp string in DD-MM-YYYY HH:MM:SS format
		parsedTime, err := time.Parse("02-01-2006 15:04:05", search.Timestamp)
		if err != nil {
			// If parsing fails, try other common formats
			parsedTime, err = time.Parse("2006-01-02 15:04:05", search.Timestamp)
			if err != nil {
				// If still fails, try RFC3339
				parsedTime, err = time.Parse(time.RFC3339, search.Timestamp)
				if err != nil {
					return fmt.Errorf("failed to parse timestamp %s: %v", search.Timestamp, err)
				}
			}
		}

		// Convert to user's timezone and format back to DD-MM-YYYY HH:MM:SS
		userTime, err := ts.ConvertFromUTC(parsedTime, userTimezone)
		if err != nil {
			return err
		}
		search.Timestamp = userTime.Format("02-01-2006 15:04:05")
	}

	return nil
}

// ConvertSearchItemToUserTimezone converts search item timestamps to user's timezone
func (ts *TimezoneService) ConvertSearchItemToUserTimezone(item *models.SearchItem, userTimezone string) error {
	// Convert CreatedAt
	if !item.CreatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(item.CreatedAt, userTimezone)
		if err != nil {
			return err
		}
		item.CreatedAt = userTime
	}

	// Convert CheckInDate and CheckOutDate
	if !item.CheckInDate.IsZero() {
		userTime, err := ts.ConvertFromUTC(item.CheckInDate, userTimezone)
		if err != nil {
			return err
		}
		item.CheckInDate = userTime
	}

	if !item.CheckOutDate.IsZero() {
		userTime, err := ts.ConvertFromUTC(item.CheckOutDate, userTimezone)
		if err != nil {
			return err
		}
		item.CheckOutDate = userTime
	}

	return nil
}

// ConvertScheduleToUserTimezone converts schedule timestamps to user's timezone
func (ts *TimezoneService) ConvertScheduleToUserTimezone(schedule *models.Schedule, userTimezone string) error {
	// Convert CreatedAt
	if !schedule.CreatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(schedule.CreatedAt, userTimezone)
		if err != nil {
			return err
		}
		schedule.CreatedAt = userTime
	}

	// Convert UpdatedAt
	if !schedule.UpdatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(schedule.UpdatedAt, userTimezone)
		if err != nil {
			return err
		}
		schedule.UpdatedAt = userTime
	}

	// Convert NextRunAt (this is *time.Time)
	if schedule.NextRunAt != nil {
		userTime, err := ts.ConvertFromUTC(*schedule.NextRunAt, userTimezone)
		if err != nil {
			return err
		}
		schedule.NextRunAt = &userTime
	}

	// Convert LastRunAt (this is *time.Time)
	if schedule.LastRunAt != nil {
		userTime, err := ts.ConvertFromUTC(*schedule.LastRunAt, userTimezone)
		if err != nil {
			return err
		}
		schedule.LastRunAt = &userTime
	}

	return nil
}

// ConvertScheduleRunToUserTimezone converts schedule run timestamps to user's timezone
func (ts *TimezoneService) ConvertScheduleRunToUserTimezone(run *models.ScheduleRun, userTimezone string) error {
	// Convert CreatedAt
	if !run.CreatedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(run.CreatedAt, userTimezone)
		if err != nil {
			return err
		}
		run.CreatedAt = userTime
	}

	// Convert StartedAt (this is time.Time)
	if !run.StartedAt.IsZero() {
		userTime, err := ts.ConvertFromUTC(run.StartedAt, userTimezone)
		if err != nil {
			return err
		}
		run.StartedAt = userTime
	}

	// Convert CompletedAt (this is *time.Time)
	if run.CompletedAt != nil {
		userTime, err := ts.ConvertFromUTC(*run.CompletedAt, userTimezone)
		if err != nil {
			return err
		}
		run.CompletedAt = &userTime
	}

	return nil
}

// ParseScheduleTime parses schedule time from frontend and returns UTC
func (ts *TimezoneService) ParseScheduleTime(scheduleData map[string]interface{}, userTimezone string) (time.Time, error) {
	// Extract date_time from schedule data
	dateTimeStr, ok := scheduleData["date_time"].(string)
	if !ok {
		return time.Time{}, fmt.Errorf("missing or invalid date_time in schedule data")
	}

	// Parse the datetime string
	parsedTime, err := time.Parse(time.RFC3339, dateTimeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse date_time: %v", err)
	}

	// If the time is already in UTC (ends with Z), return as is
	if dateTimeStr[len(dateTimeStr)-1] == 'Z' {
		return parsedTime.UTC(), nil
	}

	// Otherwise, treat it as user's timezone and convert to UTC
	return ts.ConvertToUTC(parsedTime, userTimezone)
}

// FormatScheduleTime formats a UTC time for schedule display
func (ts *TimezoneService) FormatScheduleTime(utcTime time.Time, userTimezone string) (string, error) {
	return ts.FormatForUser(utcTime, userTimezone, time.RFC3339)
}

// GetTimezoneOffset returns the offset in hours for a given timezone
func (ts *TimezoneService) GetTimezoneOffset(timezone string) (int, error) {
	if timezone == "" {
		timezone = "UTC"
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return 0, fmt.Errorf("invalid timezone %s: %v", timezone, err)
	}

	now := time.Now()
	_, offset := now.In(loc).Zone()
	return offset / 3600, nil // Convert seconds to hours
}

// ValidateTimezone checks if a timezone is valid
func (ts *TimezoneService) ValidateTimezone(timezone string) error {
	if timezone == "" {
		return nil // Empty timezone is valid (will use default)
	}

	_, err := time.LoadLocation(timezone)
	return err
}

// GetCommonTimezones returns a list of common timezones
func (ts *TimezoneService) GetCommonTimezones() []string {
	return []string{
		"UTC",
		"America/New_York",
		"America/Chicago",
		"America/Denver",
		"America/Los_Angeles",
		"Europe/London",
		"Europe/Paris",
		"Europe/Berlin",
		"Europe/Rome",
		"Asia/Kolkata",
		"Asia/Shanghai",
		"Asia/Tokyo",
		"Asia/Dubai",
		"Australia/Sydney",
		"Australia/Melbourne",
		"Pacific/Auckland",
	}
}

// ConvertUserInputToUTC converts user input timestamps to UTC for database storage
func (ts *TimezoneService) ConvertUserInputToUTC(inputTime time.Time, userTimezone string) (time.Time, error) {
	return ts.ConvertToUTC(inputTime, userTimezone)
}

// ConvertDBTimestampToUser converts database UTC timestamps to user's timezone for display
func (ts *TimezoneService) ConvertDBTimestampToUser(dbTime time.Time, userTimezone string) (time.Time, error) {
	return ts.ConvertFromUTC(dbTime, userTimezone)
}

// FormatTimestampForAPI formats a timestamp for API response in user's timezone
func (ts *TimezoneService) FormatTimestampForAPI(utcTime time.Time, userTimezone string) (string, error) {
	return ts.FormatForUser(utcTime, userTimezone, time.RFC3339)
}

// ParseTimestampFromAPI parses a timestamp from API request and converts to UTC
func (ts *TimezoneService) ParseTimestampFromAPI(timeStr string, userTimezone string) (time.Time, error) {
	return ts.ParseUserTime(timeStr, userTimezone)
}
