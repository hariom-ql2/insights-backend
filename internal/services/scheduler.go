package services

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/frontinsight/backend/internal/models"
	"gorm.io/gorm"
)

type SchedulerService struct {
	db              *gorm.DB
	timezoneService *TimezoneService
}

func NewSchedulerService(db *gorm.DB) *SchedulerService {
	return &SchedulerService{
		db:              db,
		timezoneService: NewTimezoneService(db),
	}
}

// CreateSchedule creates a new schedule
func (s *SchedulerService) CreateSchedule(userID, name, scheduleType string, scheduleData interface{}, collectionID, searchID *uint, userTimezone string) (*models.Schedule, error) {
	dataJSON, err := json.Marshal(scheduleData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schedule data: %v", err)
	}

	// Calculate next run time (converting from user timezone to UTC)
	nextRunAt, err := s.calculateNextRunTime(scheduleType, scheduleData, userTimezone)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate next run time: %v", err)
	}

	schedule := &models.Schedule{
		UserID:       userID,
		Name:         name,
		ScheduleType: scheduleType,
		ScheduleData: string(dataJSON),
		IsActive:     true,
		NextRunAt:    nextRunAt,
		CollectionID: collectionID,
		SearchID:     searchID,
	}

	if err := s.db.Create(schedule).Error; err != nil {
		return nil, fmt.Errorf("failed to create schedule: %v", err)
	}

	return schedule, nil
}

// GetSchedulesForUser returns all schedules for a user
func (s *SchedulerService) GetSchedulesForUser(userID string) ([]models.Schedule, error) {
	var schedules []models.Schedule
	err := s.db.Where("user_id = ? AND is_active = ?", userID, true).Order("next_run_at ASC").Find(&schedules).Error
	return schedules, err
}

// GetDueSchedules returns schedules that are due to run
// Optimized query: uses partial index and selects only necessary fields
func (s *SchedulerService) GetDueSchedules() ([]models.Schedule, error) {
	var schedules []models.Schedule
	nowUTC := s.timezoneService.GetCurrentUTC()
	// Use Select to limit fields - reduces data transfer and improves performance
	// The partial index idx_schedules_active_next_run optimizes this query
	err := s.db.Select("id", "user_id", "name", "schedule_type", "schedule_data", "collection_id", "search_id", "next_run_at").
		Where("is_active = ? AND next_run_at <= ?", true, nowUTC).
		Find(&schedules).Error
	return schedules, err
}

// UpdateScheduleNextRun updates the next run time for a schedule
func (s *SchedulerService) UpdateScheduleNextRun(scheduleID uint) error {
	var schedule models.Schedule
	if err := s.db.First(&schedule, scheduleID).Error; err != nil {
		return err
	}

	// Get user's timezone from the schedule data or use default
	userTimezone := "UTC" // Default fallback
	var scheduleData map[string]interface{}
	if err := json.Unmarshal([]byte(schedule.ScheduleData), &scheduleData); err == nil {
		if tz, ok := scheduleData["timezone"].(string); ok && tz != "" {
			userTimezone = tz
		}
	}

	// Calculate next run time
	nextRunAt, err := s.calculateNextRunTime(schedule.ScheduleType, scheduleData, userTimezone)
	if err != nil {
		return err
	}

	// For "once" schedules, deactivate after first run
	if schedule.ScheduleType == "once" {
		schedule.IsActive = false
		schedule.NextRunAt = nil
	} else {
		schedule.NextRunAt = nextRunAt
	}

	// Use UTC for LastRunAt
	nowUTC := s.timezoneService.GetCurrentUTC()
	schedule.LastRunAt = &nowUTC

	return s.db.Save(&schedule).Error
}

// RecordScheduleRun records a schedule run
func (s *SchedulerService) RecordScheduleRun(scheduleID uint, status string, errorMsg *string) error {
	nowUTC := s.timezoneService.GetCurrentUTC()
	run := &models.ScheduleRun{
		ScheduleID: scheduleID,
		Status:     status,
		StartedAt:  nowUTC,
	}

	if status == "completed" || status == "failed" {
		run.CompletedAt = &nowUTC
	}

	if errorMsg != nil {
		run.ErrorMsg = errorMsg
	}

	return s.db.Create(run).Error
}

// calculateNextRunTime calculates the next run time based on schedule type and data
func (s *SchedulerService) calculateNextRunTime(scheduleType string, scheduleData interface{}, userTimezone string) (*time.Time, error) {
	now := s.timezoneService.GetCurrentUTC()

	switch scheduleType {
	case "once":
		// Parse the map data to OnceScheduleData
		dataMap, ok := scheduleData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid schedule data format for once schedule")
		}

		// Use the timezone service to parse and convert to UTC
		nextRunUTC, err := s.timezoneService.ParseScheduleTime(dataMap, userTimezone)
		if err != nil {
			return nil, fmt.Errorf("failed to parse schedule time: %v", err)
		}

		return &nextRunUTC, nil

	case "daily":
		dataMap, ok := scheduleData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid schedule data format for daily schedule")
		}

		timeStr, ok := dataMap["time"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid time in daily schedule data")
		}

		return s.calculateDailyNextRun(timeStr, userTimezone, now)

	case "weekly":
		dataMap, ok := scheduleData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid schedule data format for weekly schedule")
		}

		dayOfWeekFloat, ok := dataMap["day_of_week"].(float64)
		if !ok {
			return nil, fmt.Errorf("missing or invalid day_of_week in weekly schedule data")
		}
		dayOfWeek := int(dayOfWeekFloat)

		timeStr, ok := dataMap["time"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid time in weekly schedule data")
		}

		return s.calculateWeeklyNextRun(dayOfWeek, timeStr, userTimezone, now)

	case "biweekly":
		dataMap, ok := scheduleData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid schedule data format for biweekly schedule")
		}

		dayOfWeekFloat, ok := dataMap["day_of_week"].(float64)
		if !ok {
			return nil, fmt.Errorf("missing or invalid day_of_week in biweekly schedule data")
		}
		dayOfWeek := int(dayOfWeekFloat)

		timeStr, ok := dataMap["time"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid time in biweekly schedule data")
		}

		startDate, ok := dataMap["start_date"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid start_date in biweekly schedule data")
		}

		return s.calculateBiweeklyNextRun(dayOfWeek, timeStr, userTimezone, startDate, now)

	case "monthly":
		dataMap, ok := scheduleData.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid schedule data format for monthly schedule")
		}

		dayOfMonthFloat, ok := dataMap["day_of_month"].(float64)
		if !ok {
			return nil, fmt.Errorf("missing or invalid day_of_month in monthly schedule data")
		}
		dayOfMonth := int(dayOfMonthFloat)

		timeStr, ok := dataMap["time"].(string)
		if !ok {
			return nil, fmt.Errorf("missing or invalid time in monthly schedule data")
		}

		return s.calculateMonthlyNextRun(dayOfMonth, timeStr, userTimezone, now)

	default:
		return nil, fmt.Errorf("unknown schedule type: %s", scheduleType)
	}
}

func (s *SchedulerService) calculateDailyNextRun(timeStr, userTimezone string, nowUTC time.Time) (*time.Time, error) {
	// Convert UTC now to user's timezone to calculate the target time
	userNow, err := s.timezoneService.ConvertFromUTC(nowUTC, userTimezone)
	if err != nil {
		return nil, err
	}

	// Parse time (HH:MM format) in user's timezone
	today := userNow.Format("2006-01-02")
	timeParts := fmt.Sprintf("%s %s", today, timeStr)

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return nil, err
	}

	targetTimeUser, err := time.ParseInLocation("2006-01-02 15:04", timeParts, loc)
	if err != nil {
		return nil, err
	}

	// If time has passed today, schedule for tomorrow
	if targetTimeUser.Before(userNow) {
		targetTimeUser = targetTimeUser.Add(24 * time.Hour)
	}

	// Convert back to UTC for storage
	targetTimeUTC, err := s.timezoneService.ConvertToUTC(targetTimeUser, userTimezone)
	if err != nil {
		return nil, err
	}

	return &targetTimeUTC, nil
}

func (s *SchedulerService) calculateWeeklyNextRun(dayOfWeek int, timeStr, userTimezone string, nowUTC time.Time) (*time.Time, error) {
	// Convert UTC now to user's timezone
	userNow, err := s.timezoneService.ConvertFromUTC(nowUTC, userTimezone)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return nil, err
	}

	currentWeekday := int(userNow.Weekday())

	// Calculate days until target weekday
	daysUntilTarget := (dayOfWeek - currentWeekday + 7) % 7
	if daysUntilTarget == 0 && userNow.Hour()*60+userNow.Minute() >= parseTimeToMinutes(timeStr) {
		daysUntilTarget = 7 // Next week
	}

	// Calculate target date
	targetDate := userNow.AddDate(0, 0, daysUntilTarget)
	timeParts := fmt.Sprintf("%s %s", targetDate.Format("2006-01-02"), timeStr)
	targetTimeUser, err := time.ParseInLocation("2006-01-02 15:04", timeParts, loc)
	if err != nil {
		return nil, err
	}

	// Convert back to UTC for storage
	targetTimeUTC, err := s.timezoneService.ConvertToUTC(targetTimeUser, userTimezone)
	if err != nil {
		return nil, err
	}

	return &targetTimeUTC, nil
}

func (s *SchedulerService) calculateBiweeklyNextRun(dayOfWeek int, timeStr, userTimezone, startDate string, nowUTC time.Time) (*time.Time, error) {
	// Convert UTC now to user's timezone
	userNow, err := s.timezoneService.ConvertFromUTC(nowUTC, userTimezone)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return nil, err
	}

	// Parse start date in user's timezone
	start, err := time.ParseInLocation("2006-01-02", startDate, loc)
	if err != nil {
		return nil, err
	}

	// Calculate weeks since start
	weeksSinceStart := int(userNow.Sub(start).Hours() / (24 * 7 * 2)) // Biweekly = 2 weeks

	// Calculate next occurrence
	nextStart := start.AddDate(0, 0, weeksSinceStart*14)
	nextTarget := nextStart.AddDate(0, 0, dayOfWeek-int(nextStart.Weekday()))

	// If we're past this week's occurrence, move to next biweekly cycle
	if nextTarget.Before(userNow) {
		nextTarget = nextTarget.AddDate(0, 0, 14)
	}

	timeParts := fmt.Sprintf("%s %s", nextTarget.Format("2006-01-02"), timeStr)
	targetTimeUser, err := time.ParseInLocation("2006-01-02 15:04", timeParts, loc)
	if err != nil {
		return nil, err
	}

	// Convert back to UTC for storage
	targetTimeUTC, err := s.timezoneService.ConvertToUTC(targetTimeUser, userTimezone)
	if err != nil {
		return nil, err
	}

	return &targetTimeUTC, nil
}

func (s *SchedulerService) calculateMonthlyNextRun(dayOfMonth int, timeStr, userTimezone string, nowUTC time.Time) (*time.Time, error) {
	// Convert UTC now to user's timezone
	userNow, err := s.timezoneService.ConvertFromUTC(nowUTC, userTimezone)
	if err != nil {
		return nil, err
	}

	loc, err := time.LoadLocation(userTimezone)
	if err != nil {
		return nil, err
	}

	// Calculate next month
	nextMonth := userNow.AddDate(0, 1, 0)

	// Adjust day if it doesn't exist in the target month
	year, month, _ := nextMonth.Date()
	targetDay := dayOfMonth
	if targetDay > 28 {
		// Handle months with fewer days
		daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
		if targetDay > daysInMonth {
			targetDay = daysInMonth
		}
	}

	timeParts := fmt.Sprintf("%04d-%02d-%02d %s", year, int(month), targetDay, timeStr)
	targetTimeUser, err := time.ParseInLocation("2006-01-02 15:04", timeParts, loc)
	if err != nil {
		return nil, err
	}

	// Convert back to UTC for storage
	targetTimeUTC, err := s.timezoneService.ConvertToUTC(targetTimeUser, userTimezone)
	if err != nil {
		return nil, err
	}

	return &targetTimeUTC, nil
}

// Helper function to parse time string to minutes
func parseTimeToMinutes(timeStr string) int {
	var hour, minute int
	fmt.Sscanf(timeStr, "%d:%d", &hour, &minute)
	return hour*60 + minute
}

// DeleteSchedule deactivates a schedule
func (s *SchedulerService) DeleteSchedule(scheduleID uint, userID string) error {
	return s.db.Model(&models.Schedule{}).Where("id = ? AND user_id = ?", scheduleID, userID).Update("is_active", false).Error
}
