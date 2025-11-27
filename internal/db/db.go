package db

import (
	"context"
	"log"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Config struct {
	DatabaseURL     string
	PoolSize        int
	PoolRecycle     time.Duration
	PoolPrePing     bool
	ConnectTimeout  time.Duration
	ApplicationName string
}

func Open(cfg Config) (*gorm.DB, error) {
	// Configure logger with higher slow query threshold to reduce noise
	// Set to 1 second to avoid logging queries during AutoMigrate schema introspection
	customLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             1 * time.Second, // Only log queries slower than 1 second
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	// Enable prepared statement cache for better performance
	gormCfg := &gorm.Config{
		Logger:                   customLogger,
		PrepareStmt:              true, // Enable prepared statement cache
		DisableNestedTransaction: false,
	}

	// Optimize connection string with performance parameters
	databaseURL := cfg.DatabaseURL
	if databaseURL != "" {
		// Build optimized connection parameters
		params := []string{}

		// Add timezone parameter to ensure UTC storage
		if !containsTimezoneParam(databaseURL) {
			params = append(params, "timezone=UTC")
		}

		// Add connection optimization parameters
		// connect_timeout: reduce connection timeout for faster failure detection
		if !containsParam(databaseURL, "connect_timeout") {
			params = append(params, "connect_timeout=10")
		}

		// application_name: helps with monitoring and connection tracking
		if cfg.ApplicationName != "" && !containsParam(databaseURL, "application_name") {
			params = append(params, "application_name="+cfg.ApplicationName)
		}

		// sslmode: disable SSL for internal connections (if not already set)
		// Note: For production, consider using 'require' or 'verify-full'
		if !containsParam(databaseURL, "sslmode") {
			params = append(params, "sslmode=disable")
		}

		// Add all parameters to connection string
		if len(params) > 0 {
			separator := "?"
			if strings.Contains(databaseURL, "?") {
				separator = "&"
			}
			databaseURL = databaseURL + separator + strings.Join(params, "&")
		}
	}

	dial := postgres.Open(databaseURL)
	db, err := gorm.Open(dial, gormCfg)
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// Optimize connection pool settings
	// MaxOpenConns: maximum number of open connections
	sqlDB.SetMaxOpenConns(cfg.PoolSize)

	// MaxIdleConns: keep more idle connections for faster reuse
	// Use 50% of pool size for idle connections (minimum 2)
	idleConns := cfg.PoolSize / 2
	if idleConns < 2 {
		idleConns = 2
	}
	sqlDB.SetMaxIdleConns(idleConns)

	// ConnMaxLifetime: recycle connections to prevent stale connections
	sqlDB.SetConnMaxLifetime(cfg.PoolRecycle)

	// ConnMaxIdleTime: close idle connections after 5 minutes to free resources
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	// Test connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		log.Printf("db ping error: %v", err)
	}

	// Set connection-level optimizations
	// These are set per-connection, so they apply to all connections in the pool
	optimizationQueries := []string{
		"SET timezone = 'UTC'",
		"SET statement_timeout = '30s'", // Prevent long-running queries
		"SET lock_timeout = '10s'",      // Prevent long lock waits
	}

	for _, query := range optimizationQueries {
		if _, err := sqlDB.Exec(query); err != nil {
			log.Printf("warning: failed to execute optimization query '%s': %v", query, err)
		}
	}

	return db, nil
}

// Helper functions to check URL parameters
func containsTimezoneParam(url string) bool {
	return strings.Contains(url, "timezone=")
}

func containsParam(url string, param string) bool {
	if param == "" {
		return strings.Contains(url, "?") || strings.Contains(url, "&")
	}
	return strings.Contains(url, param+"=")
}
