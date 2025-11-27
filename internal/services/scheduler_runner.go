package services

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/frontinsight/backend/internal/config"
	"github.com/frontinsight/backend/internal/models"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

type SchedulerRunner struct {
	db               *gorm.DB
	schedulerService *SchedulerService
	submitCollection func(string, []models.JobData, string, ...models.SubmitOption) error
	cfg              config.AppConfig
}

func NewSchedulerRunner(db *gorm.DB, schedulerService *SchedulerService, submitCollection func(string, []models.JobData, string, ...models.SubmitOption) error, cfg config.AppConfig) *SchedulerRunner {
	return &SchedulerRunner{
		db:               db,
		schedulerService: schedulerService,
		submitCollection: submitCollection,
		cfg:              cfg,
	}
}

// RunScheduler checks for due schedules and executes them
func (sr *SchedulerRunner) RunScheduler() error {
	// Get due schedules
	schedules, err := sr.schedulerService.GetDueSchedules()
	if err != nil {
		return fmt.Errorf("failed to get due schedules: %v", err)
	}

	log.Printf("Found %d due schedules", len(schedules))

	for _, schedule := range schedules {
		go sr.executeSchedule(schedule)
	}

	return nil
}

// executeSchedule executes a single schedule
func (sr *SchedulerRunner) executeSchedule(schedule models.Schedule) {
	log.Printf("Executing schedule %d: %s", schedule.ID, schedule.Name)

	// Record start of run
	err := sr.schedulerService.RecordScheduleRun(schedule.ID, "running", nil)
	if err != nil {
		log.Printf("Failed to record schedule run start: %v", err)
		return
	}

	var errorMsg *string
	defer func() {
		// Update run status
		status := "completed"
		if errorMsg != nil {
			status = "failed"
		}
		sr.schedulerService.RecordScheduleRun(schedule.ID, status, errorMsg)

		// Update schedule next run time
		if err := sr.schedulerService.UpdateScheduleNextRun(schedule.ID); err != nil {
			log.Printf("Failed to update schedule next run: %v", err)
		}
	}()

	// Execute the scheduled job
	if err := sr.executeScheduledJob(schedule); err != nil {
		errStr := err.Error()
		errorMsg = &errStr
		log.Printf("Failed to execute schedule %d: %v", schedule.ID, err)
		return
	}

	log.Printf("Successfully executed schedule %d: %s", schedule.ID, schedule.Name)
}

// executeScheduledJob executes the actual job based on schedule type
func (sr *SchedulerRunner) executeScheduledJob(schedule models.Schedule) error {
	// Get the collection or search data
	var collection *models.Collection
	var search *models.Search

	if schedule.CollectionID != nil {
		err := sr.db.Preload("CollectionItems").First(&collection, *schedule.CollectionID).Error
		if err != nil {
			return fmt.Errorf("failed to load collection: %v", err)
		}
	}

	if schedule.SearchID != nil {
		err := sr.db.Preload("Items").First(&search, *schedule.SearchID).Error
		if err != nil {
			return fmt.Errorf("failed to load search: %v", err)
		}
	}

	// Convert to job data format and submit to QL2
	if collection != nil {
		return sr.submitCollectionToQL2(collection, schedule.UserID)
	} else if search != nil {
		return sr.submitSearchToQL2(search)
	}

	return fmt.Errorf("no collection or search found for schedule")
}

// submitCollectionToQL2 submits a collection to QL2 using the actual submission function
func (sr *SchedulerRunner) submitCollectionToQL2(collection *models.Collection, userID string) error {
	log.Printf("Submitting collection %d to QL2", collection.ID)

	// Convert collection items to job data format
	var jobs []models.JobData
	for _, item := range collection.CollectionItems {
		job := models.JobData{
			Website:      models.WebsiteData{Name: item.Website, POS: []string(item.POS)},
			Location:     item.Location,
			CheckInDate:  item.CheckInDate.Format("2006-01-02"),
			CheckOutDate: item.CheckOutDate.Format("2006-01-02"),
			Adults:       item.Adults,
			StarRating:   item.StarRating,
		}
		jobs = append(jobs, job)
	}

	// Generate job name: 'user-given name' + 'scheduled' + 'userIDStr' + timestamp
	now := time.Now().UTC()
	safeCollectionName := strings.ReplaceAll(collection.Name, "@", "_")
	safeUserID := strings.ReplaceAll(userID, "@", "_")
	fileTS := now.Format("20060102_150405")
	jobName := fmt.Sprintf("%s_scheduled_%s_%s", safeCollectionName, safeUserID, fileTS)

	// Calculate search amount before creating search
	searchAmount, err := CalculateSearchAmountFromJobs(jobs, sr.db)
	if err != nil {
		return fmt.Errorf("failed to calculate search amount: %v", err)
	}

	// Create search entry to track the scheduled submission
	timestampStr := now.Format("02-01-2006 15:04:05")
	collectionName := collection.Name
	search := models.Search{
		UserID:         userID,
		JobName:        &jobName,
		CollectionName: &collectionName,
		Timestamp:      timestampStr,
		Status:         "Executing",
		Scheduled:      true,
		Amount:         searchAmount,
		FrozenAmount:   0.00, // Will be set when frozen
	}
	if err := sr.db.Create(&search).Error; err != nil {
		return fmt.Errorf("failed to create search: %v", err)
	}

	// Create search items for each job
	for _, job := range jobs {
		checkIn, _ := time.Parse("2006-01-02", job.CheckInDate)
		checkOut, _ := time.Parse("2006-01-02", job.CheckOutDate)
		// Ensure POS values are plain strings, not JSON-encoded
		posValues := make([]string, len(job.Website.POS))
		for i, pos := range job.Website.POS {
			posValues[i] = pos
		}
		_ = sr.db.Create(&models.SearchItem{
			SearchID:     search.ID,
			Location:     job.Location,
			CheckInDate:  checkIn,
			CheckOutDate: checkOut,
			Adults:       job.Adults,
			StarRating:   job.StarRating,
			Website:      job.Website.Name,
			POS:          pq.StringArray(posValues),
			Amount:       0.00, // Individual item amount not used
		}).Error
	}

	// Check balance and freeze amount before submitting to QL2
	if err := CheckBalanceAndFreeze(userID, searchAmount, search.ID, sr.db); err != nil {
		// Delete the search if freeze fails
		sr.db.Delete(&search)
		return fmt.Errorf("failed to freeze amount: %v", err)
	}

	// Submit to QL2 using the actual submission function
	err = sr.submitCollection(jobName, jobs, userID)
	if err != nil {
		// If QL2 submission fails, the frozen amount will be handled when search is marked as failed
		return fmt.Errorf("failed to submit collection to QL2: %v", err)
	}

	// Update collection status
	collection.Status = "running"
	nowUTC := time.Now().UTC()
	collection.LastRunAt = &nowUTC
	return sr.db.Save(collection).Error
}

// submitSearchToQL2 submits a search to QL2
func (sr *SchedulerRunner) submitSearchToQL2(search *models.Search) error {
	log.Printf("Submitting search %d to QL2", search.ID)

	// Convert search items to job data format
	var jobs []map[string]interface{}
	for _, item := range search.Items {
		job := map[string]interface{}{
			"website":      models.WebsiteData{Name: item.Website, POS: []string(item.POS)},
			"location":     item.Location,
			"checkInDate":  item.CheckInDate.Format("2006-01-02"),
			"checkOutDate": item.CheckOutDate.Format("2006-01-02"),
			"adults":       item.Adults,
			"starRating":   item.StarRating,
		}
		jobs = append(jobs, job)
	}

	// Create CSV data for QL2
	csvData := sr.createCSVData(jobs)

	// Here you would submit to QL2 - for now just log
	log.Printf("Would submit to QL2: %d jobs from search %d", len(jobs), search.ID)
	log.Printf("CSV Data: %s", csvData)

	// Update search status
	search.Status = "running"
	return sr.db.Save(search).Error
}

// createCSVData creates CSV data for QL2 submission
func (sr *SchedulerRunner) createCSVData(jobs []map[string]interface{}) string {
	// This is a simplified version - you would implement the full CSV creation logic
	// similar to the existing submitCollectionToQL2 function
	csvData := "SITECODE,COUNTRY,LOCATION,CHECKIN_DATE,CHECKOUT_DATE,HOTELNAME,SUPPLIER,RATENIGHTLY,RATEFINAL,CURRENCY,ROOM_INFORMATION,STARS,MESSAGE\n"

	for _, job := range jobs {
		website := job["website"].(models.WebsiteData)
		csvData += fmt.Sprintf("%s,%s,%s,%s,%s,,,,,,,,,\n",
			website.Name,
			"", // Country would be extracted from location
			job["location"],
			job["checkInDate"],
			job["checkOutDate"],
		)
	}

	return csvData
}

// StartScheduler starts the scheduler background process
func (sr *SchedulerRunner) StartScheduler() {
	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	defer ticker.Stop()

	log.Println("Scheduler started - checking for due schedules every minute")

	for {
		select {
		case <-ticker.C:
			if err := sr.RunScheduler(); err != nil {
				log.Printf("Scheduler error: %v", err)
			}
		}
	}
}
