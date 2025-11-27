package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/frontinsight/backend/internal/models"
	"github.com/frontinsight/backend/internal/services"
	"github.com/labstack/echo/v4"
)

type SchedulerHandler struct {
	schedulerService *services.SchedulerService
	timezoneService  *services.TimezoneService
}

func NewSchedulerHandler(schedulerService *services.SchedulerService, timezoneService *services.TimezoneService) *SchedulerHandler {
	return &SchedulerHandler{
		schedulerService: schedulerService,
		timezoneService:  timezoneService,
	}
}

// CreateSchedule godoc
// @Summary Create a new schedule
// @Description Create a new schedule for running searches
// @Tags Scheduler
// @Accept json
// @Produce json
// @Param request body CreateScheduleRequest true "Schedule creation data"
// @Success 201 {object} models.Schedule "Schedule created successfully"
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /schedules [post]
func (h *SchedulerHandler) CreateSchedule(c echo.Context) error {
	var req CreateScheduleRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	// Validate request
	if err := h.validateScheduleRequest(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Get user from context
	user := c.Get("user").(*models.User)

	// Get user's timezone
	userTimezone := "UTC" // Default fallback
	if user.Timezone != nil && *user.Timezone != "" {
		userTimezone = *user.Timezone
	}

	// Create schedule
	schedule, err := h.schedulerService.CreateSchedule(
		user.Email,
		req.Name,
		req.ScheduleType,
		req.ScheduleData,
		req.CollectionID,
		req.SearchID,
		userTimezone,
	)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, map[string]any{"success": true, "data": schedule})
}

// GetSchedules godoc
// @Summary Get user schedules
// @Description Get all schedules for the authenticated user
// @Tags Scheduler
// @Produce json
// @Success 200 {array} models.Schedule "List of schedules"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /schedules [get]
func (h *SchedulerHandler) GetSchedules(c echo.Context) error {
	user := c.Get("user").(*models.User)

	fmt.Printf("Debug: Getting schedules for user: %s (ID: %d)\n", user.Email, user.ID)
	schedules, err := h.schedulerService.GetSchedulesForUser(user.Email)
	if err != nil {
		fmt.Printf("Error getting schedules: %v\n", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	fmt.Printf("Debug: Found %d schedules for user %s\n", len(schedules), user.Email)

	// Always return UTC timestamps - frontend will handle timezone conversion
	return c.JSON(http.StatusOK, map[string]any{"success": true, "data": schedules})
}

// DeleteSchedule godoc
// @Summary Delete a schedule
// @Description Delete (deactivate) a schedule
// @Tags Scheduler
// @Param id path int true "Schedule ID"
// @Success 200 {object} map[string]string "Schedule deleted successfully"
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /schedules/{id} [delete]
func (h *SchedulerHandler) DeleteSchedule(c echo.Context) error {
	scheduleIDStr := c.Param("id")
	scheduleID, err := strconv.ParseUint(scheduleIDStr, 10, 32)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid schedule ID"})
	}

	user := c.Get("user").(*models.User)

	err = h.schedulerService.DeleteSchedule(uint(scheduleID), user.Email)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Schedule deleted successfully"})
}

// Request structures
type CreateScheduleRequest struct {
	Name         string      `json:"name" binding:"required"`
	ScheduleType string      `json:"schedule_type" binding:"required"`
	ScheduleData interface{} `json:"schedule_data" binding:"required"`
	CollectionID *uint       `json:"collection_id"`
	SearchID     *uint       `json:"search_id"`
}

// validateScheduleRequest validates the schedule creation request
func (h *SchedulerHandler) validateScheduleRequest(req *CreateScheduleRequest) error {
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule name is required")
	}

	if req.ScheduleType == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule type is required")
	}

	// Validate schedule type and data
	switch req.ScheduleType {
	case "once":
		var data models.OnceScheduleData
		jsonData, err := json.Marshal(req.ScheduleData)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid schedule data")
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid once schedule data")
		}
		if data.DateTime == "" || data.Timezone == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "DateTime and Timezone are required for once schedule")
		}

	case "daily":
		var data models.DailyScheduleData
		jsonData, err := json.Marshal(req.ScheduleData)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid schedule data")
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid daily schedule data")
		}
		if data.Time == "" || data.Timezone == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Time and Timezone are required for daily schedule")
		}

	case "weekly":
		var data models.WeeklyScheduleData
		jsonData, err := json.Marshal(req.ScheduleData)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid schedule data")
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid weekly schedule data")
		}
		if data.DayOfWeek < 0 || data.DayOfWeek > 6 || data.Time == "" || data.Timezone == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Valid DayOfWeek (0-6), Time and Timezone are required for weekly schedule")
		}

	case "biweekly":
		var data models.BiweeklyScheduleData
		jsonData, err := json.Marshal(req.ScheduleData)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid schedule data")
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid biweekly schedule data")
		}
		if data.DayOfWeek < 0 || data.DayOfWeek > 6 || data.Time == "" || data.Timezone == "" || data.StartDate == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Valid DayOfWeek (0-6), Time, Timezone and StartDate are required for biweekly schedule")
		}

	case "monthly":
		var data models.MonthlyScheduleData
		jsonData, err := json.Marshal(req.ScheduleData)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid schedule data")
		}
		if err := json.Unmarshal(jsonData, &data); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid monthly schedule data")
		}
		if data.DayOfMonth < 1 || data.DayOfMonth > 31 || data.Time == "" || data.Timezone == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Valid DayOfMonth (1-31), Time and Timezone are required for monthly schedule")
		}

	default:
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid schedule type. Must be: once, daily, weekly, biweekly, or monthly")
	}

	return nil
}
