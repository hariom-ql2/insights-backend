package server

import (
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"gorm.io/gorm"

	"github.com/frontinsight/backend/internal/config"
	"github.com/frontinsight/backend/internal/models"
	"github.com/frontinsight/backend/internal/services"
)

type Server struct {
	DB               *gorm.DB
	Cfg              config.AppConfig
	SchedulerService *services.SchedulerService
	SchedulerRunner  *services.SchedulerRunner
	TimezoneService  *services.TimezoneService
}

func New(e *echo.Echo, db *gorm.DB, cfg config.AppConfig) *Server {
	// Auto-migrate schema
	_ = db.AutoMigrate(
		&models.User{},
		&models.EmailVerification{},
		&models.LoginAttempt{},
		&models.Search{},
		&models.SearchItem{},
		&models.Collection{},
		&models.CollectionItem{},
		&models.PaymentOrder{},
		&models.Transaction{},
		&models.Location{},
		&models.Site{},
		&models.POS{},
		&models.SiteToPriceMapping{},
		&models.CustomerQuery{},
		&models.Schedule{},
		&models.ScheduleRun{},
		&models.AdminActivity{},
		&models.SystemStats{},
	)

	// Create optimized index for scheduler queries
	// Partial index on next_run_at where is_active = true for faster lookups
	_ = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_schedules_active_next_run 
		ON schedules (next_run_at) 
		WHERE is_active = true
	`).Error

	// Initialize scheduler services
	schedulerService := services.NewSchedulerService(db)
	timezoneService := services.NewTimezoneService(db)
	schedulerRunner := services.NewSchedulerRunner(db, schedulerService, func(jobName string, jobs []models.JobData, userID string, opts ...models.SubmitOption) error {
		// Create a temporary server instance to access the submission method
		tempServer := &Server{DB: db, Cfg: cfg}
		// Look up the search record to get the original collection name (preserves special characters)
		var search models.Search
		collectionName := ""
		if err := db.Where("job_name = ? AND user_id = ?", jobName, userID).First(&search).Error; err == nil && search.CollectionName != nil {
			collectionName = *search.CollectionName
		} else {
			// Fallback: try to extract from jobName if search not found yet
			if parts := strings.SplitN(jobName, "_scheduled_", 2); len(parts) > 0 {
				collectionName = parts[0]
			}
		}
		return tempServer.submitCollectionToQL2(jobName, jobs, userID, collectionName, opts...)
	}, cfg)

	s := &Server{
		DB:               db,
		Cfg:              cfg,
		SchedulerService: schedulerService,
		SchedulerRunner:  schedulerRunner,
		TimezoneService:  timezoneService,
	}

	// Security middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.Secure())
	e.Use(middleware.RateLimiter(middleware.NewRateLimiterMemoryStore(20))) // 20 requests per second

	// Health
	e.GET("/health", s.Health)

	// Webhooks (public routes, authenticated via API key)
	e.POST("/webhooks/ql2-job-status", s.QL2JobStatusWebhook)

	// Auth (public routes)
	e.POST("/signup", s.Signup)
	e.POST("/verify-email", s.VerifyEmail)
	e.POST("/resend-verification", s.ResendVerification)
	e.POST("/login", s.Login)

	// Auth (protected routes)
	authGroup := e.Group("/auth")
	authGroup.Use(s.JWTMiddleware())
	authGroup.POST("/logout", s.Logout)
	authGroup.GET("/profile", s.GetProfile)
	authGroup.PUT("/timezone", s.UpdateTimezone)

	// change-password kept for parity
	e.POST("/change-password", s.ChangePassword)

	// Protected routes (require authentication)
	protectedGroup := e.Group("")
	protectedGroup.Use(s.JWTMiddleware())

	// Dashboard
	protectedGroup.GET("/dashboard/stats", s.DashboardStats)

	// Searches and collections
	protectedGroup.POST("/save-multi-form", s.SaveMultiForm)
	protectedGroup.GET("/my-searches", s.MySearches)
	protectedGroup.GET("/search/:id", s.GetSearch)
	protectedGroup.PUT("/search-item/:id", s.UpdateSearchItem)
	protectedGroup.POST("/refresh-job-status/:id", s.RefreshJobStatus)

	protectedGroup.GET("/my-collections", s.MyCollections)
	protectedGroup.GET("/collection/:id", s.GetCollection)
	protectedGroup.PUT("/collection/:id", s.UpdateCollection)
	protectedGroup.DELETE("/collection/:id", s.DeleteCollection)
	protectedGroup.POST("/collection/:id/submit", s.SubmitCollection)
	protectedGroup.POST("/collection/:id/items", s.AddCollectionItems)
	protectedGroup.PUT("/collection-item/:id", s.UpdateCollectionItem)
	protectedGroup.DELETE("/collection-item/:id", s.DeleteCollectionItem)

	// Reference
	e.GET("/locations", s.GetLocations)
	e.GET("/sites", s.GetSites)
	e.GET("/pos", s.GetPOS)

	// Contact & payments
	e.POST("/contact-query", s.ContactQuery)
	protectedGroup.GET("/wallet", s.GetWallet)
	protectedGroup.POST("/wallet/add-money", s.AddMoneyToWallet)
	protectedGroup.POST("/create-payment-order", s.CreatePaymentOrder)

	// Scheduler routes
	schedulerHandler := NewSchedulerHandler(schedulerService, timezoneService)
	protectedGroup.POST("/schedules", schedulerHandler.CreateSchedule)
	protectedGroup.GET("/schedules", schedulerHandler.GetSchedules)
	protectedGroup.DELETE("/schedules/:id", schedulerHandler.DeleteSchedule)

	// Admin routes (require admin authentication)
	adminGroup := e.Group("/admin")
	adminGroup.Use(s.JWTMiddleware())
	adminGroup.Use(s.AdminMiddleware())

	// Admin dashboard
	adminGroup.GET("/dashboard", s.AdminDashboard)

	// Admin user management
	adminGroup.GET("/users", s.AdminUsers)
	adminGroup.GET("/users/:id", s.AdminUserDetails)
	adminGroup.PUT("/users/:id", s.AdminUpdateUser)

	// Admin data management
	adminGroup.GET("/searches", s.AdminSearches)
	adminGroup.GET("/collections", s.AdminCollections)
	adminGroup.GET("/schedules", s.AdminSchedules)
	adminGroup.GET("/activities", s.AdminActivities)

	// Files
	e.GET("/download-sample-data", s.DownloadSampleData)
	protectedGroup.GET("/download/:timestamp/:job_name", s.DownloadFile)
	// Back-compat: frontend might call download-search-output
	protectedGroup.GET("/download-search-output/:timestamp/:job_name", s.DownloadFile)
	// New route using run_id
	protectedGroup.GET("/download-by-run-id/:run_id", s.DownloadFileByRunID)

	// Reports
	protectedGroup.GET("/reports/competitor-rate-tracker", s.GetCompetitorRateTrackerDashboard)
	protectedGroup.GET("/reports/market-view", s.GetMarketViewDashboard)
	protectedGroup.GET("/reports/star-rating-trend", s.GetStarRatingTrendDashboard)
	protectedGroup.GET("/reports/price-suggestion", s.GetPriceSuggestionDashboard)

	// Start scheduler runner
	go s.SchedulerRunner.StartScheduler()

	// Start cleanup job for old login attempts
	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Run every hour
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.CleanupOldAttempts()
			}
		}
	}()

	return s
}
