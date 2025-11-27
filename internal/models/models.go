package models

import (
	"time"

	"github.com/lib/pq"
)

type User struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Email        string     `gorm:"unique;not null" json:"email"`
	Name         string     `gorm:"not null" json:"name"`
	Password     string     `gorm:"not null" json:"-"`
	IsVerified   bool       `gorm:"default:false" json:"is_verified"`
	Role         string     `gorm:"default:'user'" json:"role"` // user, admin, super_admin
	City         *string    `json:"city"`
	State        *string    `json:"state"`
	Country      *string    `json:"country"`
	MobileNumber *string    `gorm:"column:mobile_number" json:"mobile_number"`
	IPAddress    *string    `gorm:"column:ip_address" json:"ip_address"`
	Timezone     *string    `json:"timezone"`
	BusinessType *string    `gorm:"column:business_type" json:"business_type"`
	Company      *string    `json:"company"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at" json:"last_login_at"`
	SessionToken *string    `gorm:"column:session_token" json:"-"`
	Balance      float64    `gorm:"type:decimal(10,2);default:0.00;not null" json:"balance"`
	FrozenAmount float64    `gorm:"type:decimal(10,2);default:0.00;not null;column:frozen_amount" json:"frozen_amount"`
	CreatedAt    time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

type EmailVerification struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Email     string    `gorm:"not null" json:"email"`
	Code      string    `gorm:"size:6;not null" json:"code"`
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	IsUsed    bool      `gorm:"default:false" json:"is_used"`
}

type Search struct {
	ID            uint         `gorm:"primaryKey" json:"id"`
	UserID        string       `gorm:"not null;index" json:"user_id"`
	JobName       *string      `gorm:"column:job_name" json:"job_name"`
	CollectionName *string     `gorm:"column:collection_name" json:"collection_name"` // User-provided collection name for display
	RunID         *int64       `gorm:"column:run_id" json:"run_id"`
	Timestamp     string       `json:"timestamp"`
	Status        string       `gorm:"default:'Starting'" json:"status"`
	OutputFile    *string      `gorm:"column:output_file" json:"output_file"`
	Scheduled     bool         `gorm:"not null;default:false" json:"scheduled"`
	ScheduledAt   *time.Time   `gorm:"column:scheduled_at" json:"scheduled_at"`
	Amount        float64      `gorm:"type:decimal(10,2);default:0.00;not null" json:"amount"`
	FrozenAmount  float64      `gorm:"type:decimal(10,2);default:0.00;not null;column:frozen_amount" json:"frozen_amount"`
	CreatedAt     time.Time    `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	Items         []SearchItem `gorm:"foreignKey:SearchID" json:"search_items,omitempty"`
}

type Collection struct {
	ID              uint             `gorm:"primaryKey" json:"id"`
	UserID          string           `gorm:"not null;index" json:"user_id"`
	Name            string           `gorm:"not null;uniqueIndex:idx_user_collection_name" json:"name"` // User-provided name, unique per user
	Description     *string          `json:"description"`
	Status          string           `gorm:"default:'saved'" json:"status"`
	LastRunAt       *time.Time       `gorm:"column:last_run_at" json:"last_run_at"`
	CreatedAt       time.Time        `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt       time.Time        `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	CollectionItems []CollectionItem `gorm:"foreignKey:CollectionID" json:"collection_items,omitempty"`
}

type CollectionItem struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	CollectionID uint           `gorm:"not null;index" json:"collection_id"`
	Location     string         `gorm:"not null" json:"location"`
	CheckInDate  time.Time      `gorm:"type:date;not null" json:"check_in_date"`
	CheckOutDate time.Time      `gorm:"type:date;not null" json:"check_out_date"`
	Adults       int            `gorm:"not null" json:"adults"`
	StarRating   string         `gorm:"not null" json:"star_rating"`
	Website      string         `gorm:"not null" json:"website"`
	POS          pq.StringArray `gorm:"type:text[];not null" json:"pos"`
	CreatedAt    time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

type SearchItem struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	SearchID     uint           `gorm:"not null;index" json:"search_id"`
	Location     string         `gorm:"not null" json:"location"`
	CheckInDate  time.Time      `gorm:"type:date;not null" json:"check_in_date"`
	CheckOutDate time.Time      `gorm:"type:date;not null" json:"check_out_date"`
	Adults       int            `gorm:"not null" json:"adults"`
	StarRating   string         `gorm:"not null" json:"star_rating"`
	Website      string         `gorm:"not null" json:"website"`
	POS          pq.StringArray `gorm:"type:text[];not null" json:"pos"`
	Amount       float64        `gorm:"type:decimal(10,2);default:0.00;not null" json:"amount"`
	CreatedAt    time.Time      `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

type PaymentOrder struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Amount    int       `gorm:"not null" json:"amount"`
	UserID    string    `gorm:"not null" json:"user_id"`
	Status    string    `gorm:"default:'created'" json:"status"`
	CreatedAt time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

type Transaction struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	UserID        string    `gorm:"not null;index" json:"user_id"`
	SearchID      *uint     `json:"search_id"`
	TxnType       string    `gorm:"not null;index" json:"txn_type"` // debit, credit, refund, freeze, unfreeze
	Amount        float64   `gorm:"type:decimal(10,2);not null" json:"amount"`
	BalanceBefore float64   `gorm:"type:decimal(10,2);not null" json:"balance_before"`
	BalanceAfter  float64   `gorm:"type:decimal(10,2);not null" json:"balance_after"`
	Description   *string   `json:"description"`
	ReferenceID   *string   `gorm:"column:reference_id;index" json:"reference_id"`
	Status        string    `gorm:"default:'completed';index" json:"status"` // pending, completed, failed, cancelled
	Metadata      *string   `gorm:"type:jsonb" json:"metadata"`
	CreatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt     time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	// Foreign key relationships
	User   User    `gorm:"foreignKey:UserID;references:Email" json:"user,omitempty"`
	Search *Search `gorm:"foreignKey:SearchID" json:"search,omitempty"`
}

type Location struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"not null" json:"name"`
}

type Site struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Code string `gorm:"not null" json:"code"`
	Name string `gorm:"not null" json:"name"`
}

type POS struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"not null" json:"name"`
}

type SiteToPriceMapping struct {
	ID    uint    `gorm:"primaryKey" json:"id"`
	Code  string  `gorm:"not null;index" json:"code"`
	Name  string  `gorm:"not null;index" json:"name"`
	Price float64 `gorm:"type:decimal(10,2);not null" json:"price"`
}

// TableName specifies the table name for SiteToPriceMapping
func (SiteToPriceMapping) TableName() string {
	return "site_to_price_mapping"
}

type CustomerQuery struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	Name       string    `gorm:"not null" json:"name"`
	Email      string    `gorm:"not null" json:"email"`
	Phone      string    `gorm:"type:varchar(20)" json:"phone"`
	Company    string    `gorm:"type:varchar(255)" json:"company"`
	Subject    string    `gorm:"type:varchar(255)" json:"subject"`
	QueryType  string    `gorm:"type:varchar(50)" json:"query_type"` // e.g., "general", "support", "sales", "technical"
	Message    string    `gorm:"not null;type:text" json:"message"`
	CreatedAt  time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// Scheduler Models
type Schedule struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	UserID       string     `gorm:"index;not null" json:"user_id"`
	Name         string     `gorm:"not null" json:"name"`
	ScheduleType string     `gorm:"not null" json:"schedule_type"`            // once, daily, weekly, biweekly, monthly
	ScheduleData string     `gorm:"type:jsonb;not null" json:"schedule_data"` // Contains schedule configuration
	IsActive     bool       `gorm:"default:true" json:"is_active"`
	LastRunAt    *time.Time `gorm:"column:last_run_at" json:"last_run_at"`
	NextRunAt    *time.Time `gorm:"column:next_run_at" json:"next_run_at"`
	CreatedAt    time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
	CollectionID *uint      `json:"collection_id"` // Reference to collection if scheduled from collection
	SearchID     *uint      `json:"search_id"`     // Reference to search if scheduled from search
}

type ScheduleRun struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	ScheduleID  uint       `gorm:"not null;index" json:"schedule_id"`
	Status      string     `gorm:"not null" json:"status"` // running, completed, failed
	StartedAt   time.Time  `gorm:"not null" json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	ErrorMsg    *string    `json:"error_msg"`
	CreatedAt   time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// Schedule data structures for different types
type OnceScheduleData struct {
	DateTime string `json:"date_time"` // ISO format: 2025-01-15T17:00:00+05:30
	Timezone string `json:"timezone"`  // IST, UTC, etc.
}

type DailyScheduleData struct {
	Time     string `json:"time"`     // HH:MM format: 17:00
	Timezone string `json:"timezone"` // IST, UTC, etc.
}

type WeeklyScheduleData struct {
	DayOfWeek int    `json:"day_of_week"` // 0=Sunday, 1=Monday, etc.
	Time      string `json:"time"`        // HH:MM format: 17:00
	Timezone  string `json:"timezone"`    // IST, UTC, etc.
}

type BiweeklyScheduleData struct {
	DayOfWeek int    `json:"day_of_week"` // 0=Sunday, 1=Monday, etc.
	Time      string `json:"time"`        // HH:MM format: 17:00
	Timezone  string `json:"timezone"`    // IST, UTC, etc.
	StartDate string `json:"start_date"`  // YYYY-MM-DD format
}

type MonthlyScheduleData struct {
	DayOfMonth int    `json:"day_of_month"` // 1-31
	Time       string `json:"time"`         // HH:MM format: 17:00
	Timezone   string `json:"timezone"`     // IST, UTC, etc.
}

// WebsiteData represents website information for job submission
type WebsiteData struct {
	Name string   `json:"name"`
	POS  []string `json:"pos"`
}

// JobData represents a single job for submission
type JobData struct {
	Website      WebsiteData `json:"website"`
	Location     string      `json:"location"`
	CheckInDate  string      `json:"checkInDate"`
	CheckOutDate string      `json:"checkOutDate"`
	Adults       int         `json:"adults"`
	StarRating   string      `json:"starRating"`
}

// SubmitOption represents a submission option function
type SubmitOption func(*string)

// Admin Models
type AdminActivity struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	AdminID    uint      `gorm:"not null;index" json:"admin_id"`
	Action     string    `gorm:"not null" json:"action"`   // create, update, delete, view
	Resource   string    `gorm:"not null" json:"resource"` // user, search, collection, etc.
	ResourceID *uint     `json:"resource_id"`
	Details    *string   `gorm:"type:jsonb" json:"details"`
	IPAddress  string    `gorm:"not null" json:"ip_address"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

type SystemStats struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	TotalUsers       int       `json:"total_users"`
	ActiveUsers      int       `json:"active_users"`
	TotalSearches    int       `json:"total_searches"`
	TotalCollections int       `json:"total_collections"`
	TotalSchedules   int       `json:"total_schedules"`
	FailedJobs       int       `json:"failed_jobs"`
	CompletedJobs    int       `json:"completed_jobs"`
	CreatedAt        time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// Admin Dashboard Response Types
type AdminDashboardStats struct {
	TotalUsers       int `json:"total_users"`
	ActiveUsers      int `json:"active_users"`
	NewUsersToday    int `json:"new_users_today"`
	TotalSearches    int `json:"total_searches"`
	TotalCollections int `json:"total_collections"`
	TotalSchedules   int `json:"total_schedules"`
	FailedJobs       int `json:"failed_jobs"`
	CompletedJobs    int `json:"completed_jobs"`
	Revenue          int `json:"revenue"`
}

type UserWithStats struct {
	User
	SearchesCount    int        `json:"searches_count"`
	CollectionsCount int        `json:"collections_count"`
	SchedulesCount   int        `json:"schedules_count"`
	LastActivityAt   *time.Time `json:"last_activity_at"`
}

type SearchWithUser struct {
	Search
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
}

type CollectionWithUser struct {
	Collection
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
}
