package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"gorm.io/gorm"

	"github.com/frontinsight/backend/internal/models"
	"github.com/frontinsight/backend/internal/services"
)

// mapping similar to Python create_input_new.py / server.py
var siteFullNameToCode = map[string]string{
	"EXPEDIA":      "EXP",
	"PRICELINE":    "PL",
	"MARRIOTT":     "MC",
	"CHOICEHOTELS": "CH",
	"BESTWESTERN":  "BW",
	"REDROOF":      "RR",
	"ACCORHOTELS":  "RT",
}

func toScriptCode(name string) string {
	u := strings.ToUpper(strings.TrimSpace(name))
	if code, ok := siteFullNameToCode[u]; ok {
		return code
	}
	return name
}

// Use models.JobData instead of local jobData
type jobData = models.JobData

type saveMultiFormRequest struct {
	Jobs           []jobData `json:"jobs" binding:"required"`
	Action         string    `json:"action" example:"start" enums:"start,save,schedule"`
	ScheduleTS     *string   `json:"scheduleTs" example:"202509181400"`                    // YYYYMMDDHHMI format for IST timezone
	CollectionName *string   `json:"collection_name" example:"My Hotel Search Collection"` // User-provided collection name
}

// SaveMultiForm godoc
// @Summary Submit search jobs
// @Description Submit hotel search jobs with options to start immediately, save as collection, or schedule for later
// @Tags Searches
// @Accept json
// @Produce json
// @Param request body saveMultiFormRequest true "Search job data"
// @Success 200 {object} map[string]interface{} "Job submitted successfully"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /save-multi-form [post]
func (s *Server) SaveMultiForm(c echo.Context) error {
	var req saveMultiFormRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid payload"})
	}
	// Get user info from authenticated context
	user := c.Get("user").(*models.User)
	userIDStr := user.Email // Use email as UserID for foreign key constraint
	if len(req.Jobs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "No jobs provided"})
	}
	if req.Action == "" {
		req.Action = "start"
	}

	// Validate collection name (required)
	if req.CollectionName == nil || strings.TrimSpace(*req.CollectionName) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection name is required"})
	}

	collectionName := strings.TrimSpace(*req.CollectionName)
	if len(collectionName) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection name cannot be empty"})
	}
	if len(collectionName) > 255 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection name cannot exceed 255 characters"})
	}

	// Check for duplicate collection name for this user
	var existingCollection models.Collection
	if err := s.DB.Where("user_id = ? AND name = ?", userIDStr, collectionName).First(&existingCollection).Error; err == nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "A collection with this name already exists. Please choose a different name."})
	}

	if hasDuplicateJobs(req.Jobs) {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection contains duplicate jobs"})
	}
	isDuplicate := hasDuplicateCollection(s.DB, userIDStr, req.Jobs, 0)
	switch req.Action {
	case "save":
		if isDuplicate {
			return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "A collection with identical jobs already exists"})
		}
		col, err := s.createCollection(userIDStr, req.Jobs, "saved", nil, collectionName)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Collection saved successfully", "collection_id": col.ID})
	case "start":
		now := s.TimezoneService.GetCurrentUTC()
		fileTS := now.Format("20060102_150405")
		// Include collection name in job name: 'user-given name' + 'current name'
		safeCollectionName := safeUserId(collectionName)
		jobName := fmt.Sprintf("%s_collection_%s_%s", safeCollectionName, safeUserId(userIDStr), fileTS)

		// Calculate search amount before creating search
		searchAmount, err := services.CalculateSearchAmountFromJobs(req.Jobs, s.DB)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to calculate search amount: " + err.Error()})
		}

		// Create search entry to track the submission
		timestampStr := now.Format("02-01-2006 15:04:05")

		// Check if a search with this job name already exists to prevent duplicates
		var existingSearch models.Search
		if err := s.DB.Where("job_name = ?", jobName).First(&existingSearch).Error; err == nil {
			// Search already exists, skip creation to prevent duplicates
			return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Search with this job name already exists"})
		}

		// Create search with calculated amount
		search := models.Search{
			UserID:         userIDStr,
			JobName:        &jobName,
			CollectionName: &collectionName,
			Timestamp:      timestampStr,
			Status:         "Executing",
			Scheduled:      false,
			Amount:         searchAmount,
			FrozenAmount:   0.00, // Will be set when frozen
		}
		if err := s.DB.Create(&search).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to create search: " + err.Error()})
		}

		// Create search items
		for _, j := range req.Jobs {
			checkIn, _ := time.Parse("2006-01-02", j.CheckInDate)
			checkOut, _ := time.Parse("2006-01-02", j.CheckOutDate)
			// Ensure POS values are plain strings, not JSON-encoded
			posValues := make([]string, len(j.Website.POS))
			for i, pos := range j.Website.POS {
				posValues[i] = pos
			}
			_ = s.DB.Create(&models.SearchItem{
				SearchID:     search.ID,
				Location:     j.Location,
				CheckInDate:  checkIn,
				CheckOutDate: checkOut,
				Adults:       j.Adults,
				StarRating:   j.StarRating,
				Website:      j.Website.Name,
				POS:          pq.StringArray(posValues),
				Amount:       0.00, // Individual item amount not used
			}).Error
		}

		// Check balance and freeze amount before submitting to QL2
		if err := services.CheckBalanceAndFreeze(userIDStr, searchAmount, search.ID, s.DB); err != nil {
			// Delete the search if freeze fails
			s.DB.Delete(&search)
			return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
		}

		// Submit to QL2 (after successful freeze)
		err = s.submitCollectionToQL2(jobName, req.Jobs, userIDStr, collectionName)
		if err != nil {
			// If QL2 submission fails, we should unfreeze the amount
			// For now, just return error - the frozen amount will be handled when search is marked as failed
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to submit job: " + err.Error()})
		}

		if isDuplicate {
			_ = s.updateLastRunForDuplicate(userIDStr, req.Jobs)
		} else {
			_, _ = s.createCollection(userIDStr, req.Jobs, "submitted", ptr(now), collectionName)
		}
		return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Job started and collection recorded"})
	case "schedule":
		// Create collection first
		collection, err := s.createCollection(userIDStr, req.Jobs, "scheduled", nil, collectionName)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		}

		// Create schedule using the new scheduler system
		// Format: user-given collection name + date
		dateStr := s.TimezoneService.GetCurrentUTC().Format("2006-01-02 15:04")
		scheduleName := fmt.Sprintf("%s - %s", collectionName, dateStr)

		// Get user timezone
		userTimezone, err := s.TimezoneService.GetUserTimezone(userIDStr)
		if err != nil {
			userTimezone = "UTC"
		}

		// Parse schedule data from request
		var scheduleData interface{}
		var scheduleType string

		if req.ScheduleTS != nil && len(*req.ScheduleTS) == 12 {
			// Legacy format - treat as "once" schedule
			scheduleType = "once"
			// Parse YYYYMMDDHHMI format
			year := (*req.ScheduleTS)[0:4]
			month := (*req.ScheduleTS)[4:6]
			day := (*req.ScheduleTS)[6:8]
			hour := (*req.ScheduleTS)[8:10]
			minute := (*req.ScheduleTS)[10:12]

			// Create datetime string in user's timezone
			userTimeStr := fmt.Sprintf("%s-%s-%s %s:%s:00", year, month, day, hour, minute)

			// Parse in user's timezone and convert to UTC
			utcTime, err := s.TimezoneService.ParseUserTime(userTimeStr, userTimezone)
			if err != nil {
				return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid schedule time format"})
			}

			// Format as ISO string for storage
			dateTime := utcTime.Format(time.RFC3339)

			scheduleData = models.OnceScheduleData{
				DateTime: dateTime,
				Timezone: userTimezone,
			}
		} else {
			return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "scheduleTs required in YYYYMMDDHHMI format"})
		}

		// Create schedule
		schedule, err := s.SchedulerService.CreateSchedule(
			userIDStr,
			scheduleName,
			scheduleType,
			scheduleData,
			&collection.ID,
			nil,
			userTimezone,
		)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to create schedule: " + err.Error()})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"success":       true,
			"message":       "Job scheduled successfully",
			"schedule_id":   schedule.ID,
			"collection_id": collection.ID,
		})
	}
	return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid action"})
}

func (s *Server) updateLastRunForDuplicate(userID string, jobs []jobData) error {
	var cols []models.Collection
	if err := s.DB.Where("user_id = ?", userID).Find(&cols).Error; err != nil {
		return err
	}
	for _, col := range cols {
		var items []models.CollectionItem
		_ = s.DB.Where("collection_id = ?", col.ID).Find(&items).Error
		if collectionsIdentical(jobs, items) {
			now := s.TimezoneService.GetCurrentUTC()
			col.LastRunAt = &now
			col.UpdatedAt = now
			return s.DB.Save(&col).Error
		}
	}
	return nil
}

func (s *Server) createCollection(userID string, jobs []jobData, status string, lastRun *time.Time, collectionName string) (*models.Collection, error) {
	col := models.Collection{UserID: userID, Name: collectionName, Description: ptr(fmt.Sprintf("Collection with %d jobs", len(jobs))), Status: status, LastRunAt: lastRun}
	if err := s.DB.Create(&col).Error; err != nil {
		return nil, err
	}
	for _, j := range jobs {
		checkIn, _ := time.Parse("2006-01-02", j.CheckInDate)
		checkOut, _ := time.Parse("2006-01-02", j.CheckOutDate)
		// Ensure POS values are plain strings, not JSON-encoded
		posValues := make([]string, len(j.Website.POS))
		for i, pos := range j.Website.POS {
			posValues[i] = pos
		}
		item := models.CollectionItem{
			CollectionID: col.ID,
			Location:     j.Location,
			CheckInDate:  checkIn,
			CheckOutDate: checkOut,
			Adults:       j.Adults,
			StarRating:   j.StarRating,
			Website:      j.Website.Name,
			POS:          pq.StringArray(posValues),
		}
		_ = s.DB.Create(&item).Error
	}
	return &col, nil
}

// Use models.SubmitOption instead of local submitOption
type submitOption = models.SubmitOption

func (s *Server) submitCollectionToQL2(jobName string, jobs []jobData, userID string, collectionName string, opts ...submitOption) error {
	if s.Cfg.QL2Username == "" || s.Cfg.QL2Password == "" {
		return nil
	}
	var schedule string
	for _, o := range opts {
		o(&schedule)
	}
	lines := make([]string, 0)
	for _, j := range jobs {
		locParts := strings.Split(j.Location, ",")
		for i := range locParts {
			locParts[i] = strings.TrimSpace(locParts[i])
		}
		if len(locParts) < 2 {
			continue
		}
		city := locParts[0]
		country := locParts[len(locParts)-1]
		state := ""
		if len(locParts) == 3 {
			state = locParts[1]
		}
		ci := strings.ReplaceAll(j.CheckInDate, "-", "")
		co := strings.ReplaceAll(j.CheckOutDate, "-", "")

		scriptCode := toScriptCode(j.Website.Name)
		poses := j.Website.POS
		if len(poses) == 0 {
			poses = []string{""}
		}
		for _, pos := range poses {
			row := fmt.Sprintf("%s,%s,%s,%s,%s,%s,,,,,15,%s,25,A,userId=%s&jobname=%s&,1,,,0,%d,,,,%s,,,", scriptCode, city, state, country, ci, co, j.StarRating, userID, collectionName, j.Adults, pos)
			lines = append(lines, row)
		}
	}
	if len(lines) == 0 {
		return errors.New("no CSV rows generated for QL2 submission")
	}
	csv := strings.Join(lines, string(rune(10)))

	// save this csv to a file
	os.WriteFile("ql2_submission.csv", []byte(csv), 0644)
	fmt.Println("Saved CSV to ql2_submission.csv")

	// build endpoint: when scheduling, force startjob=n; else use configured start flag
	startFlag := s.Cfg.QL2StartJob
	if schedule != "" {
		startFlag = "n"
	}
	base := fmt.Sprintf(
		"http://client.ql2.com/submit?username=%s&password=%s&app=hotel&createorreplacejob=%s&startjob=%s&priority=high",
		url.QueryEscape(s.Cfg.QL2Username),
		url.QueryEscape(s.Cfg.QL2Password),
		url.QueryEscape(jobName),
		url.QueryEscape(startFlag),
	)
	if schedule != "" {
		base += "&setschedule=" + url.QueryEscape(schedule)
	}

	req, _ := http.NewRequest(http.MethodPost, base, strings.NewReader(csv))
	req.Header.Set("Content-Type", "text/plain")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("ql2 submit: status %d", resp.StatusCode)
	}

	// Note: Search entry creation is handled by the calling function
	// This function only handles QL2 submission

	return nil
}

// MyCollections godoc
// @Summary Get user's collections
// @Description Retrieve all collections for a specific user with optional filtering
// @Tags Collections
// @Produce json
// @Param userId query string true "User ID (email)"
// @Param location query string false "Filter by location"
// @Param website query string false "Filter by website"
// @Param checkInStart query string false "Check-in start date (YYYY-MM-DD)"
// @Param checkInEnd query string false "Check-in end date (YYYY-MM-DD)"
// @Param checkOutStart query string false "Check-out start date (YYYY-MM-DD)"
// @Param checkOutEnd query string false "Check-out end date (YYYY-MM-DD)"
// @Success 200 {object} map[string]interface{} "List of collections"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Router /my-collections [get]
func (s *Server) MyCollections(c echo.Context) error {
	// Get user info from authenticated context
	user := c.Get("user").(*models.User)
	userIDStr := user.Email // Use email as UserID for foreign key constraint
	locationFilter := strings.TrimSpace(c.QueryParam("location"))
	websiteFilter := strings.TrimSpace(c.QueryParam("website"))
	checkInStart := strings.TrimSpace(c.QueryParam("checkInStart"))
	checkOutStart := strings.TrimSpace(c.QueryParam("checkOutStart"))

	var collections []models.Collection
	if err := s.DB.Where("user_id = ?", userIDStr).Order("updated_at DESC").Find(&collections).Error; err != nil {
		return c.JSON(http.StatusOK, map[string]any{"success": true, "collections": []any{}})
	}

	if len(collections) == 0 {
		return c.JSON(http.StatusOK, map[string]any{"success": true, "collections": []any{}})
	}

	// Always return UTC timestamps - frontend will handle timezone conversion

	resp := make([]map[string]any, 0, len(collections))
	for _, col := range collections {
		var items []models.CollectionItem
		_ = s.DB.Where("collection_id = ?", col.ID).Find(&items).Error

		filtered := []models.CollectionItem{}
		for _, it := range items {
			// location filter
			if locationFilter != "" && !strings.Contains(strings.ToLower(it.Location), strings.ToLower(locationFilter)) {
				continue
			}
			// website filter
			if websiteFilter != "" {
				if !strings.Contains(strings.ToLower(it.Website), strings.ToLower(websiteFilter)) {
					continue
				}
			}
			// date filters - exact matching
			if checkInStart != "" {
				if t, err := time.Parse("2006-01-02", checkInStart); err == nil {
					itemDate := it.CheckInDate.Format("2006-01-02")
					filterDate := t.Format("2006-01-02")
					if itemDate != filterDate {
						continue
					}
				}
			}
			if checkOutStart != "" {
				if t, err := time.Parse("2006-01-02", checkOutStart); err == nil {
					itemDate := it.CheckOutDate.Format("2006-01-02")
					filterDate := t.Format("2006-01-02")
					if itemDate != filterDate {
						continue
					}
				}
			}
			filtered = append(filtered, it)
		}

		// Collection items will be returned in UTC - frontend will handle timezone conversion

		// If filters applied and no items left, skip
		if len(filtered) == 0 && (locationFilter != "" || websiteFilter != "" || checkInStart != "" || checkOutStart != "") {
			continue
		}

		colData := map[string]any{
			"id":             col.ID,
			"name":           col.Name,
			"description":    col.Description,
			"status":         col.Status,
			"scheduled_date": nil,
			"last_run_at":    toISO(col.LastRunAt),
			"created_at":     col.CreatedAt.Format(time.RFC3339),
			"updated_at":     col.UpdatedAt.Format(time.RFC3339),
			"search_count":   len(filtered),
			"searches":       []any{},
		}
		for _, it := range filtered {
			colData["searches"] = append(colData["searches"].([]any), map[string]any{
				"id":             it.ID,
				"location":       it.Location,
				"check_in_date":  it.CheckInDate.Format("2006-01-02"),
				"check_out_date": it.CheckOutDate.Format("2006-01-02"),
				"adults":         it.Adults,
				"star_rating":    it.StarRating,
				"website":        it.Website,
				"pos":            it.POS,
			})
		}
		resp = append(resp, colData)
	}

	return c.JSON(http.StatusOK, map[string]any{"success": true, "collections": resp})
}

func (s *Server) GetCollection(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var col models.Collection
	if err := s.DB.First(&col, id).Error; err != nil || col.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection not found"})
	}
	var items []models.CollectionItem
	_ = s.DB.Where("collection_id = ?", col.ID).Find(&items).Error
	resp := map[string]any{
		"id":             col.ID,
		"name":           col.Name,
		"description":    col.Description,
		"status":         col.Status,
		"scheduled_date": nil,
		"last_run_at":    toISO(col.LastRunAt),
		"created_at":     col.CreatedAt.Format(time.RFC3339),
		"updated_at":     col.UpdatedAt.Format(time.RFC3339),
		"search_count":   len(items),
		"searches":       []any{},
	}
	for _, it := range items {
		resp["searches"] = append(resp["searches"].([]any), map[string]any{
			"id":             it.ID,
			"location":       it.Location,
			"check_in_date":  it.CheckInDate.Format("2006-01-02"),
			"check_out_date": it.CheckOutDate.Format("2006-01-02"),
			"adults":         it.Adults,
			"star_rating":    it.StarRating,
			"website":        it.Website,
			"pos":            it.POS,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "collection": resp})
}

func (s *Server) DeleteCollection(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("collection_id = ?", id).Delete(&models.CollectionItem{}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
			return err
		}
		if err := tx.Delete(&models.Collection{}, id).Error; err != nil {
			c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
			return err
		}
		c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Collection deleted successfully"})
		return nil
	})
}

type updateCollectionRequest struct {
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	Jobs        []jobData `json:"jobs"`
}

func (s *Server) UpdateCollection(c echo.Context) error {
	// Get user from context first
	userInterface := c.Get("user")
	if userInterface == nil {
		return c.JSON(http.StatusUnauthorized, map[string]any{"success": false, "message": "User not authenticated"})
	}
	user := userInterface.(*models.User)
	userIDStr := user.Email

	id, _ := strconv.Atoi(c.Param("id"))
	var col models.Collection
	if err := s.DB.First(&col, id).Error; err != nil || col.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection not found"})
	}

	// Verify ownership
	if col.UserID != userIDStr {
		return c.JSON(http.StatusForbidden, map[string]any{"success": false, "message": "User not authorized to update this collection"})
	}

	var req updateCollectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid payload"})
	}
	if strings.TrimSpace(req.Name) == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection name is required"})
	}
	if len(req.Jobs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "At least one job is required"})
	}
	if hasDuplicateJobs(req.Jobs) {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection contains duplicate jobs"})
	}
	if hasDuplicateCollection(s.DB, col.UserID, req.Jobs, uint(id)) {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "A collection with identical jobs already exists"})
	}
	// Update collection
	col.Name = req.Name
	col.Description = req.Description
	col.UpdatedAt = s.TimezoneService.GetCurrentUTC()
	if err := s.DB.Save(&col).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
	}
	// Replace items
	if err := s.DB.Where("collection_id = ?", col.ID).Delete(&models.CollectionItem{}).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
	}
	for _, j := range req.Jobs {
		checkIn, _ := time.Parse("2006-01-02", j.CheckInDate)
		checkOut, _ := time.Parse("2006-01-02", j.CheckOutDate)
		// Ensure POS values are plain strings, not JSON-encoded
		posValues := make([]string, len(j.Website.POS))
		for i, pos := range j.Website.POS {
			posValues[i] = pos
		}
		item := models.CollectionItem{
			CollectionID: col.ID,
			Location:     j.Location,
			CheckInDate:  checkIn,
			CheckOutDate: checkOut,
			Adults:       j.Adults,
			StarRating:   j.StarRating,
			Website:      j.Website.Name,
			POS:          pq.StringArray(posValues),
		}
		_ = s.DB.Create(&item).Error
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Collection updated successfully", "collection_id": col.ID})
}

func (s *Server) SubmitCollection(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var col models.Collection
	if err := s.DB.First(&col, id).Error; err != nil || col.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection not found"})
	}
	// Update status/last run
	now := s.TimezoneService.GetCurrentUTC()
	col.Status = "submitted"
	col.LastRunAt = &now
	col.UpdatedAt = now
	_ = s.DB.Save(&col).Error

	// Create job name for submission - include collection name
	fileTimestamp := now.Format("20060102_150405")
	timestampStr := now.Format("02-01-2006 15:04:05")
	safeCollectionName := safeUserId(col.Name)
	jobName := fmt.Sprintf("%s_collection_%s_%s", safeCollectionName, safeUserId(col.UserID), fileTimestamp)

	// Load items before submission
	var items []models.CollectionItem
	if err := s.DB.Where("collection_id = ?", col.ID).Find(&items).Error; err != nil {
		items = []models.CollectionItem{}
	}

	// Convert items to jobs for price calculation
	jobs := toJobsFromItems(items)

	// Calculate search amount
	searchAmount, err := services.CalculateSearchAmountFromJobs(jobs, s.DB)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to calculate search amount: " + err.Error()})
	}

	// Create search entry to track the submission
	// Check if a search with this job name already exists to prevent duplicates
	var existingSearch models.Search
	if err := s.DB.Where("job_name = ?", jobName).First(&existingSearch).Error; err == nil {
		// Search already exists, skip creation to prevent duplicates
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Search with this job name already exists"})
	}

	// Create search with calculated amount
	collectionName := col.Name
	search := models.Search{
		UserID:         col.UserID,
		JobName:        &jobName,
		CollectionName: &collectionName,
		Timestamp:      timestampStr,
		Status:         "Executing",
		Scheduled:      false,
		Amount:         searchAmount,
		FrozenAmount:   0.00, // Will be set when frozen
	}
	if err := s.DB.Create(&search).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to create search: " + err.Error()})
	}

	// Create search items
	for _, it := range items {
		_ = s.DB.Create(&models.SearchItem{
			SearchID:     search.ID,
			Location:     it.Location,
			CheckInDate:  it.CheckInDate,
			CheckOutDate: it.CheckOutDate,
			Adults:       it.Adults,
			StarRating:   it.StarRating,
			Website:      it.Website,
			POS:          it.POS,
			Amount:       0.00, // Individual item amount not used
		}).Error
	}

	// Check balance and freeze amount before submitting to QL2
	if err := services.CheckBalanceAndFreeze(col.UserID, searchAmount, search.ID, s.DB); err != nil {
		// Delete the search if freeze fails
		s.DB.Delete(&search)
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": err.Error()})
	}

	// Submit entire collection as one job (best-effort)
	err = s.submitCollectionToQL2(jobName, jobs, col.UserID, col.Name)
	if err != nil {
		// If QL2 submission fails, we should unfreeze the amount
		// For now, just return error - the frozen amount will be handled when search is marked as failed
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to submit job: " + err.Error()})
	}
	// Removed automatic polling - users will manually refresh job status
	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": fmt.Sprintf("Collection submitted successfully with %d searches", len(items))})
}

func (s *Server) UpdateCollectionItem(c echo.Context) error {
	// Get user info from authenticated context
	userInterface := c.Get("user")
	if userInterface == nil {
		return c.JSON(http.StatusUnauthorized, map[string]any{"success": false, "message": "User not authenticated"})
	}
	user := userInterface.(*models.User)
	userIDStr := user.Email // Use email as UserID for foreign key constraint

	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		Location     string   `json:"location"`
		CheckInDate  string   `json:"check_in_date"`
		CheckOutDate string   `json:"check_out_date"`
		Adults       int      `json:"adults"`
		StarRating   string   `json:"star_rating"`
		Website      string   `json:"website"`
		POS          []string `json:"pos"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid payload"})
	}
	var item models.CollectionItem
	if err := s.DB.First(&item, id).Error; err != nil || item.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection item not found"})
	}

	// Verify that the collection item belongs to the authenticated user
	var collection models.Collection
	if err := s.DB.First(&collection, item.CollectionID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection not found"})
	}
	if collection.UserID != userIDStr {
		return c.JSON(http.StatusForbidden, map[string]any{"success": false, "message": "User not authorized to update this collection item"})
	}

	// Prepare updated values
	updatedLocation := body.Location
	if updatedLocation == "" {
		updatedLocation = item.Location
	}
	updatedCheckInDate := body.CheckInDate
	if updatedCheckInDate == "" {
		updatedCheckInDate = item.CheckInDate.Format("2006-01-02")
	}
	updatedCheckOutDate := body.CheckOutDate
	if updatedCheckOutDate == "" {
		updatedCheckOutDate = item.CheckOutDate.Format("2006-01-02")
	}
	updatedAdults := body.Adults
	if updatedAdults == 0 {
		updatedAdults = item.Adults
	}
	updatedStarRating := body.StarRating
	if updatedStarRating == "" {
		updatedStarRating = item.StarRating
	}
	updatedWebsite := body.Website
	if updatedWebsite == "" {
		updatedWebsite = item.Website
	}
	updatedPOS := body.POS
	if updatedPOS == nil {
		updatedPOS = []string(item.POS)
	}

	// Check for duplicates with other items in the same collection (excluding current item)
	var existingItems []models.CollectionItem
	if err := s.DB.Where("collection_id = ? AND id != ?", item.CollectionID, item.ID).Find(&existingItems).Error; err != nil {
		existingItems = []models.CollectionItem{}
	}

	for _, existingItem := range existingItems {
		if updatedLocation == existingItem.Location &&
			updatedCheckInDate == existingItem.CheckInDate.Format("2006-01-02") &&
			updatedCheckOutDate == existingItem.CheckOutDate.Format("2006-01-02") &&
			updatedAdults == existingItem.Adults &&
			updatedStarRating == existingItem.StarRating &&
			updatedWebsite == existingItem.Website {
			return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": fmt.Sprintf("Duplicate item found: %s", updatedLocation)})
		}
	}

	// Update item fields
	if body.Location != "" {
		item.Location = body.Location
	}
	if body.CheckInDate != "" {
		if t, err := time.Parse("2006-01-02", body.CheckInDate); err == nil {
			item.CheckInDate = t
		}
	}
	if body.CheckOutDate != "" {
		if t, err := time.Parse("2006-01-02", body.CheckOutDate); err == nil {
			item.CheckOutDate = t
		}
	}
	if body.Adults > 0 {
		item.Adults = body.Adults
	}
	if body.StarRating != "" {
		item.StarRating = body.StarRating
	}
	if body.Website != "" {
		item.Website = body.Website
	}
	if body.POS != nil {
		// Ensure POS values are plain strings, not JSON-encoded
		posValues := make([]string, len(body.POS))
		for i, pos := range body.POS {
			posValues[i] = pos
		}
		item.POS = pq.StringArray(posValues)
	}
	if err := s.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to update collection item"})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Collection item updated successfully"})
}

// DeleteCollectionItem godoc
// @Summary Delete a collection item
// @Description Delete a specific item from a collection
// @Tags Collections
// @Accept json
// @Produce json
// @Param id path int true "Collection Item ID"
// @Success 200 {object} map[string]interface{} "Item deleted successfully"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 404 {object} map[string]interface{} "Collection item not found"
// @Router /collection-item/:id [delete]
func (s *Server) DeleteCollectionItem(c echo.Context) error {
	// Get user info from authenticated context
	userInterface := c.Get("user")
	if userInterface == nil {
		return c.JSON(http.StatusUnauthorized, map[string]any{"success": false, "message": "User not authenticated"})
	}
	user := userInterface.(*models.User)
	userIDStr := user.Email

	id, _ := strconv.Atoi(c.Param("id"))
	var item models.CollectionItem
	if err := s.DB.First(&item, id).Error; err != nil || item.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection item not found"})
	}

	// Verify that the collection item belongs to the authenticated user
	var collection models.Collection
	if err := s.DB.First(&collection, item.CollectionID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection not found"})
	}
	if collection.UserID != userIDStr {
		return c.JSON(http.StatusForbidden, map[string]any{"success": false, "message": "User not authorized to delete this collection item"})
	}

	// Delete the item
	if err := s.DB.Delete(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to delete collection item"})
	}

	// Update collection description
	var remainingItems []models.CollectionItem
	s.DB.Where("collection_id = ?", collection.ID).Find(&remainingItems)
	collection.Description = ptr(fmt.Sprintf("Collection with %d jobs", len(remainingItems)))
	collection.UpdatedAt = s.TimezoneService.GetCurrentUTC()
	s.DB.Save(&collection)

	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Collection item deleted successfully"})
}

// AddCollectionItems godoc
// @Summary Add items to an existing collection
// @Description Add new search items to an existing collection without replacing existing items
// @Tags Collections
// @Accept json
// @Produce json
// @Param id path int true "Collection ID"
// @Param request body addCollectionItemsRequest true "Items to add"
// @Success 200 {object} map[string]interface{} "Items added successfully"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 404 {object} map[string]interface{} "Collection not found"
// @Router /collection/:id/items [post]
func (s *Server) AddCollectionItems(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var col models.Collection
	if err := s.DB.First(&col, id).Error; err != nil || col.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Collection not found"})
	}

	// Get user from context
	user := c.Get("user").(*models.User)
	userIDStr := user.Email

	// Verify ownership
	if col.UserID != userIDStr {
		return c.JSON(http.StatusForbidden, map[string]any{"success": false, "message": "User not authorized to modify this collection"})
	}

	var req struct {
		Jobs []jobData `json:"jobs"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid payload"})
	}

	if len(req.Jobs) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "At least one job is required"})
	}

	if hasDuplicateJobs(req.Jobs) {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Collection contains duplicate jobs"})
	}

	// Get existing items to check for duplicates
	var existingItems []models.CollectionItem
	if err := s.DB.Where("collection_id = ?", col.ID).Find(&existingItems).Error; err != nil {
		existingItems = []models.CollectionItem{}
	}

	// Check for duplicates with existing items
	for _, newJob := range req.Jobs {
		for _, existingItem := range existingItems {
			if newJob.Location == existingItem.Location &&
				newJob.CheckInDate == existingItem.CheckInDate.Format("2006-01-02") &&
				newJob.CheckOutDate == existingItem.CheckOutDate.Format("2006-01-02") &&
				newJob.Adults == existingItem.Adults &&
				newJob.StarRating == existingItem.StarRating &&
				newJob.Website.Name == existingItem.Website {
				return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": fmt.Sprintf("Duplicate item found: %s", newJob.Location)})
			}
		}
	}

	// Add new items
	addedCount := 0
	for _, j := range req.Jobs {
		checkIn, _ := time.Parse("2006-01-02", j.CheckInDate)
		checkOut, _ := time.Parse("2006-01-02", j.CheckOutDate)
		// Ensure POS values are plain strings, not JSON-encoded
		posValues := make([]string, len(j.Website.POS))
		for i, pos := range j.Website.POS {
			posValues[i] = pos
		}
		item := models.CollectionItem{
			CollectionID: col.ID,
			Location:     j.Location,
			CheckInDate:  checkIn,
			CheckOutDate: checkOut,
			Adults:       j.Adults,
			StarRating:   j.StarRating,
			Website:      j.Website.Name,
			POS:          pq.StringArray(posValues),
		}
		if err := s.DB.Create(&item).Error; err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("Failed to add item: %v", err)})
		}
		addedCount++
	}

	// Update collection description and updated_at
	col.Description = ptr(fmt.Sprintf("Collection with %d jobs", len(existingItems)+addedCount))
	col.UpdatedAt = s.TimezoneService.GetCurrentUTC()
	if err := s.DB.Save(&col).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to update collection"})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success":       true,
		"message":       fmt.Sprintf("Successfully added %d item(s) to collection", addedCount),
		"collection_id": col.ID,
	})
}

// MySearches godoc
// @Summary Get user's searches
// @Description Retrieve all searches for a specific user with optional filtering
// @Tags Searches
// @Produce json
// @Param userId query string true "User ID (email)"
// @Param scheduled query string false "Filter by scheduled status (true/false)"
// @Param location query string false "Filter by location"
// @Param website query string false "Filter by website"
// @Param checkInStart query string false "Check-in start date (YYYY-MM-DD)"
// @Param checkInEnd query string false "Check-in end date (YYYY-MM-DD)"
// @Param checkOutStart query string false "Check-out start date (YYYY-MM-DD)"
// @Param checkOutEnd query string false "Check-out end date (YYYY-MM-DD)"
// @Success 200 {object} map[string]interface{} "List of searches"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Router /my-searches [get]
func (s *Server) MySearches(c echo.Context) error {
	// Get user info from authenticated context
	user := c.Get("user").(*models.User)
	userIDStr := user.Email // Use email as UserID for foreign key constraint
	scheduledOnly := strings.TrimSpace(c.QueryParam("scheduled"))
	locationFilter := strings.TrimSpace(c.QueryParam("location"))
	websiteFilter := strings.TrimSpace(c.QueryParam("website"))
	checkInStart := strings.TrimSpace(c.QueryParam("checkInStart"))
	checkOutStart := strings.TrimSpace(c.QueryParam("checkOutStart"))

	q := s.DB.Where("user_id = ?", userIDStr)
	if scheduledOnly == "true" {
		q = q.Where("scheduled = ?", true)
	}
	if scheduledOnly == "false" {
		q = q.Where("scheduled = ?", false)
	}
	var searches []models.Search
	if err := q.Order("created_at DESC").Find(&searches).Error; err != nil {
		return c.JSON(http.StatusOK, map[string]any{"success": true, "searches": []any{}})
	}

	if len(searches) == 0 {
		return c.JSON(http.StatusOK, map[string]any{"success": true, "searches": []any{}})
	}

	// Always return UTC timestamps - frontend will handle timezone conversion
	result := []map[string]any{}
	for _, srec := range searches {
		var items []models.SearchItem
		_ = s.DB.Where("search_id = ?", srec.ID).Find(&items).Error
		filtered := []models.SearchItem{}
		for _, it := range items {
			if locationFilter != "" && !strings.Contains(strings.ToLower(it.Location), strings.ToLower(locationFilter)) {
				continue
			}
			if websiteFilter != "" {
				if !strings.Contains(strings.ToLower(it.Website), strings.ToLower(websiteFilter)) {
					continue
				}
			}
			// date filters - exact matching
			if checkInStart != "" {
				if t, err := time.Parse("2006-01-02", checkInStart); err == nil {
					itemDate := it.CheckInDate.Format("2006-01-02")
					filterDate := t.Format("2006-01-02")
					if itemDate != filterDate {
						continue
					}
				}
			}
			if checkOutStart != "" {
				if t, err := time.Parse("2006-01-02", checkOutStart); err == nil {
					itemDate := it.CheckOutDate.Format("2006-01-02")
					filterDate := t.Format("2006-01-02")
					if itemDate != filterDate {
						continue
					}
				}
			}
			filtered = append(filtered, it)
		}

		if len(filtered) == 0 && (locationFilter != "" || websiteFilter != "" || checkInStart != "" || checkOutStart != "") {
			continue
		}
		result = append(result, map[string]any{
			"serial":              len(result) + 1,
			"id":                  srec.ID,
			"job_name":            valOrEmpty(srec.JobName),
			"collection_name":     valOrEmpty(srec.CollectionName),
			"run_id":              srec.RunID,
			"timestamp":           srec.Timestamp,
			"status":              srec.Status,
			"output":              srec.OutputFile,
			"scheduled":           srec.Scheduled,
			"filtered_item_count": len(filtered),
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "searches": result})
}

func (s *Server) GetSearch(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var srec models.Search
	if err := s.DB.First(&srec, id).Error; err != nil || srec.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Search not found"})
	}
	var items []models.SearchItem
	_ = s.DB.Where("search_id = ?", srec.ID).Find(&items).Error
	resp := map[string]any{
		"id":           srec.ID,
		"user_id":      srec.UserID,
		"job_name":     valOrEmpty(srec.JobName),
		"run_id":       srec.RunID,
		"timestamp":    srec.Timestamp,
		"status":       srec.Status,
		"output_file":  srec.OutputFile,
		"created_at":   srec.CreatedAt.Format(time.RFC3339),
		"search_count": len(items),
		"search_items": []any{},
	}
	for _, it := range items {
		resp["search_items"] = append(resp["search_items"].([]any), map[string]any{
			"id":             it.ID,
			"location":       it.Location,
			"check_in_date":  it.CheckInDate.Format("2006-01-02"),
			"check_out_date": it.CheckOutDate.Format("2006-01-02"),
			"adults":         it.Adults,
			"star_rating":    it.StarRating,
			"website":        it.Website,
			"pos":            it.POS,
			"amount":         it.Amount,
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "search": resp})
}

func (s *Server) UpdateSearchItem(c echo.Context) error {
	id, _ := strconv.Atoi(c.Param("id"))
	var body struct {
		Location     string   `json:"location"`
		CheckInDate  string   `json:"check_in_date"`
		CheckOutDate string   `json:"check_out_date"`
		Adults       int      `json:"adults"`
		StarRating   string   `json:"star_rating"`
		Website      string   `json:"website"`
		POS          []string `json:"pos"`
		Amount       float64  `json:"amount"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid payload"})
	}
	var item models.SearchItem
	if err := s.DB.First(&item, id).Error; err != nil || item.ID == 0 {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": "Search item not found"})
	}
	if body.Location != "" {
		item.Location = body.Location
	}
	if body.CheckInDate != "" {
		if t, err := time.Parse("2006-01-02", body.CheckInDate); err == nil {
			item.CheckInDate = t
		}
	}
	if body.CheckOutDate != "" {
		if t, err := time.Parse("2006-01-02", body.CheckOutDate); err == nil {
			item.CheckOutDate = t
		}
	}
	if body.Adults > 0 {
		item.Adults = body.Adults
	}
	if body.StarRating != "" {
		item.StarRating = body.StarRating
	}
	if body.Website != "" {
		item.Website = body.Website
	}
	if body.POS != nil {
		item.POS = body.POS
	}
	if body.Amount > 0 {
		item.Amount = body.Amount
	}
	if err := s.DB.Save(&item).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to update search item"})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Search item updated successfully"})
}

// helpers
func hasDuplicateJobs(jobs []jobData) bool {
	type key struct {
		Location     string
		CheckInDate  string
		CheckOutDate string
		Adults       int
		StarRating   string
		WebsiteJSON  string
	}
	seen := map[key]struct{}{}
	for _, j := range jobs {
		websiteJSON, _ := json.Marshal(j.Website)
		k := key{j.Location, j.CheckInDate, j.CheckOutDate, j.Adults, j.StarRating, string(websiteJSON)}
		if _, ok := seen[k]; ok {
			return true
		}
		seen[k] = struct{}{}
	}
	return false
}

func hasDuplicateCollection(db *gorm.DB, userID string, jobs []jobData, excludeID uint) bool {
	if userID == "" {
		return false
	}
	var cols []models.Collection
	if err := db.Where("user_id = ?", userID).Find(&cols).Error; err != nil {
		return false
	}
	for _, col := range cols {
		if excludeID != 0 && col.ID == excludeID {
			continue
		}
		var items []models.CollectionItem
		_ = db.Where("collection_id = ?", col.ID).Find(&items).Error
		if len(items) != len(jobs) {
			continue
		}
		if collectionsIdentical(jobs, items) {
			return true
		}
	}
	return false
}

func collectionsIdentical(jobs []jobData, items []models.CollectionItem) bool {
	// build sets
	type key struct {
		Location     string
		CheckInDate  string
		CheckOutDate string
		Adults       int
		StarRating   string
		WebsiteJSON  string
	}
	setA := map[key]struct{}{}
	for _, it := range items {
		wj, _ := json.Marshal(models.WebsiteData{Name: it.Website, POS: it.POS})
		setA[key{it.Location, it.CheckInDate.Format("2006-01-02"), it.CheckOutDate.Format("2006-01-02"), it.Adults, it.StarRating, string(wj)}] = struct{}{}
	}
	setB := map[key]struct{}{}
	for _, j := range jobs {
		wj, _ := json.Marshal(j.Website)
		setB[key{j.Location, j.CheckInDate, j.CheckOutDate, j.Adults, j.StarRating, string(wj)}] = struct{}{}
	}
	if len(setA) != len(setB) {
		return false
	}
	for k := range setA {
		if _, ok := setB[k]; !ok {
			return false
		}
	}
	return true
}

func ptr[T any](v T) *T { return &v }

func safeUserId(u string) string {
	if u == "" {
		return "anon"
	}
	return strings.ReplaceAll(u, "@", "_")
}

func toISO(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}

func valOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func toJobsFromItems(items []models.CollectionItem) []jobData {
	jobs := make([]jobData, 0, len(items))
	for _, it := range items {
		jobs = append(jobs, jobData{
			Website:      models.WebsiteData{Name: it.Website, POS: it.POS},
			Location:     it.Location,
			CheckInDate:  it.CheckInDate.Format("2006-01-02"),
			CheckOutDate: it.CheckOutDate.Format("2006-01-02"),
			Adults:       it.Adults,
			StarRating:   it.StarRating,
		})
	}
	return jobs
}

// RefreshJobStatus godoc
// @Summary Refresh individual job status
// @Description Manually refresh the status of a specific job by checking the run table
// @Tags Searches
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "Search ID"
// @Success 200 {object} map[string]interface{} "Job status updated"
// @Failure 400 {object} simpleResponse
// @Failure 401 {object} simpleResponse
// @Failure 404 {object} simpleResponse
// @Router /refresh-job-status/{id} [post]
func (s *Server) RefreshJobStatus(c echo.Context) error {
	searchID := c.Param("id")
	if searchID == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Search ID is required"})
	}

	var search models.Search
	if err := s.DB.First(&search, searchID).Error; err != nil {
		return c.JSON(http.StatusNotFound, simpleResponse{Success: false, Message: "Search not found"})
	}

	// Check if job has a job name (run table ID)
	if search.JobName == nil || *search.JobName == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Job not yet started"})
	}

	// Check if job is already in terminal state
	if search.Status == "Completed" || search.Status == "Error occured" || search.Status == "Aborted" {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Job already completed",
			"status":  search.Status,
		})
	}

	// Check job status in run table
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		s.Cfg.RunDBHost, s.Cfg.RunDBPort, s.Cfg.RunDBUser, s.Cfg.RunDBPass, s.Cfg.RunDBName)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to connect to run table"})
	}
	defer pool.Close()
	var row pgx.Row
	if search.RunID != nil {
		row = pool.QueryRow(ctx, "SELECT status, upload_url, id, errors, raw_count FROM run WHERE id=$1 ORDER BY id DESC LIMIT 1", *search.RunID)
	} else {
		row = pool.QueryRow(ctx, "SELECT status, upload_url, id, errors, raw_count FROM run WHERE job_name=$1 ORDER BY id DESC LIMIT 1", *search.JobName)
	}

	var status int
	var uploadURL *string
	var runID int64
	var errors *string
	var rawCount *int64

	if err := row.Scan(&status, &uploadURL, &runID, &errors, &rawCount); err != nil {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"success": true,
			"message": "Job not found in run table yet",
			"status":  search.Status,
		})
	}

	// Capture old status before update
	oldStatus := search.Status

	// Update search status using helper function
	// Only process payment if status changed to terminal state
	processPayment := false
	terminalStates := map[int]bool{3: true, 4: true, 5: true}
	if terminalStates[status] {
		processPayment = true
	}

	if err := s.updateSearchStatusFromRunData(&search, status, runID, processPayment); err != nil {
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to update job status"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success":    true,
		"message":    "Job status updated successfully",
		"old_status": oldStatus,
		"new_status": search.Status,
		"status":     search.Status,
	})
}

// updateSearchStatusFromRunData is a helper function that updates search status and processes payment
// It's used by both RefreshJobStatus and QL2JobStatusWebhook
// Returns error only if status update fails, payment processing errors are logged but don't cause failure
func (s *Server) updateSearchStatusFromRunData(search *models.Search, status int, runID int64, processPayment bool) error {
	statusMap := map[int]string{
		0: "Initializing",
		1: "Executing",
		2: "Completing",
		3: "Completed",
		4: "Error occured",
		5: "Aborted",
	}

	oldStatus := search.Status
	if st, ok := statusMap[status]; ok {
		search.Status = st
	} else {
		search.Status = fmt.Sprintf("%d", status)
	}

	// Update run_id if provided
	if runID != 0 {
		search.RunID = &runID
	}

	// Mark output file if terminal state
	if status == 3 || status == 4 || status == 5 {
		if search.JobName != nil {
			search.OutputFile = search.JobName
		}
	}

	// Save search to get updated status before processing payment
	if err := s.DB.Save(search).Error; err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	// Process payment if status changed to terminal state and processPayment is true
	if processPayment && oldStatus != search.Status {
		terminalStates := map[string]bool{
			"Completed":     true,
			"Error occured": true,
			"Aborted":       true,
		}

		if terminalStates[search.Status] {
			// Reload search to get latest data
			if err := s.DB.First(search, search.ID).Error; err != nil {
				// Log error but don't fail - payment processing is best-effort
				fmt.Printf("Warning: failed to reload search %d for payment processing: %v\n", search.ID, err)
				return nil
			}

			// Process based on status
			switch search.Status {
			case "Completed":
				if err := services.ProcessSearchCompletion(search, s.DB, s.Cfg); err != nil {
					// Log error but don't fail - payment processing is best-effort
					fmt.Printf("Warning: failed to process search completion for search %d: %v\n", search.ID, err)
				}
			case "Error occured", "Aborted":
				if err := services.ProcessSearchFailure(search, s.DB); err != nil {
					// Log error but don't fail - payment processing is best-effort
					fmt.Printf("Warning: failed to process search failure for search %d: %v\n", search.ID, err)
				}
			}
		}
	}

	return nil
}

// QL2JobStatusWebhook godoc
// @Summary QL2 job status webhook
// @Description Webhook endpoint for QL2 to notify when jobs reach terminal states (Completed/Error/Aborted)
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param X-API-Key header string true "Webhook API Key"
// @Param request body object true "Job status payload" example({"job_name":"test_job_123","status":3,"run_id":12345})
// @Success 200 {object} simpleResponse "Status updated successfully"
// @Failure 400 {object} simpleResponse "Bad request"
// @Failure 401 {object} simpleResponse "Unauthorized"
// @Failure 404 {object} simpleResponse "Search not found"
// @Failure 500 {object} simpleResponse "Internal server error"
// @Router /webhooks/ql2-job-status [post]
func (s *Server) QL2JobStatusWebhook(c echo.Context) error {
	// Authenticate via API key
	apiKey := c.Request().Header.Get("X-API-Key")
	if apiKey == "" {
		return c.JSON(http.StatusUnauthorized, simpleResponse{Success: false, Message: "Missing API key"})
	}
	if apiKey != s.Cfg.QL2WebhookAPIKey {
		return c.JSON(http.StatusUnauthorized, simpleResponse{Success: false, Message: "Invalid API key"})
	}

	// Parse request payload
	var payload struct {
		JobName   string  `json:"job_name" binding:"required"`
		Status    int     `json:"status" binding:"required"`
		RunID     int64   `json:"run_id" binding:"required"`
		UploadURL *string `json:"upload_url,omitempty"`
		Errors    *string `json:"errors,omitempty"`
		RawCount  *int64  `json:"raw_count,omitempty"`
	}

	if err := c.Bind(&payload); err != nil {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "Invalid payload: " + err.Error()})
	}

	// Validate required fields
	if payload.JobName == "" {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "job_name is required"})
	}
	if payload.RunID == 0 {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "run_id is required and must be non-zero"})
	}

	// Validate status is terminal state only (3, 4, or 5)
	if payload.Status != 3 && payload.Status != 4 && payload.Status != 5 {
		return c.JSON(http.StatusBadRequest, simpleResponse{Success: false, Message: "status must be 3 (Completed), 4 (Error occured), or 5 (Aborted)"})
	}

	// Find search by job_name
	var search models.Search
	if err := s.DB.Where("job_name = ?", payload.JobName).First(&search).Error; err != nil {
		return c.JSON(http.StatusNotFound, simpleResponse{Success: false, Message: "Search not found for job_name: " + payload.JobName})
	}

	// Update search status using helper function
	// Always process payment since webhook is only called for terminal states
	if err := s.updateSearchStatusFromRunData(&search, payload.Status, payload.RunID, true); err != nil {
		// If status update failed, return error
		// Payment processing errors are logged inside the helper but don't cause failure
		return c.JSON(http.StatusInternalServerError, simpleResponse{Success: false, Message: "Failed to update job status"})
	}

	return c.JSON(http.StatusOK, simpleResponse{
		Success: true,
		Message: "Job status updated successfully",
	})
}
