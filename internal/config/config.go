package config

import (
	"fmt"
	"os"
	"time"
)

type AppConfig struct {
	Port string

	DatabaseURL string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string

	RedisURL string

	RunDBHost string
	RunDBPort string
	RunDBUser string
	RunDBPass string
	RunDBName string

	FCDBHost string
	FCDBPort string
	FCDBUser string
	FCDBPass string
	FCDBName string

	SFTPHost string
	SFTPPort int
	SFTPUser string
	SFTPPass string

	QL2Username string
	QL2Password string
	QL2StartJob string

	QL2Username_1 string
	QL2Password_1 string

	QL2WebhookAPIKey string

	JWTSecret string
	JWTExpiry time.Duration

	// Development settings
	DevMode bool

	PoolSize        int
	PoolRecycle     time.Duration
	PoolPrePing     bool
	ConnectTimeout  time.Duration
	ApplicationName string
}

func Load() AppConfig {
	cfg := AppConfig{}
	cfg.Port = getenv("PORT", "5001")
	cfg.DatabaseURL = getenv("DATABASE_URL", defaultPgURL())

	cfg.SMTPHost = getenv("SMTP_HOST", "smtp.ql2.com")
	cfg.SMTPPort = getenvInt("SMTP_PORT", 25)
	cfg.SMTPUser = getenv("SMTP_USER", "hariom_yadav@ql2.com")
	cfg.SMTPPass = getenv("SMTP_PASS", "ql2_smtp_pass")

	cfg.RedisURL = getenv("REDIS_URL", "redis://localhost:6379/0")

	cfg.RunDBHost = getenv("RUN_DB_HOST", "db.ql2.com")
	cfg.RunDBPort = getenv("RUN_DB_PORT", "5432")
	cfg.RunDBUser = getenv("RUN_DB_USER", "read_client")
	cfg.RunDBPass = getenv("RUN_DB_PASSWORD", "r_client_p")
	cfg.RunDBName = getenv("RUN_DB_NAME", "caesius")

	cfg.FCDBHost = getenv("FC_DB_HOST", "farecache.ql2.com")
	cfg.FCDBPort = getenv("FC_DB_PORT", "5432")
	cfg.FCDBUser = getenv("FC_DB_USER", "farecache")
	cfg.FCDBPass = getenv("FC_DB_PASSWORD", "cachefare")
	cfg.FCDBName = getenv("FC_DB_NAME", "farecache")

	cfg.SFTPHost = getenv("SFTP_HOST", "ftp2.ql2.com")
	cfg.SFTPPort = getenvInt("SFTP_PORT", 22)
	cfg.SFTPUser = getenv("SFTP_USER", "y_dream")
	cfg.SFTPPass = getenv("SFTP_PASS", "y!dre@ml$0707")

	cfg.QL2Username = getenv("QL2_USERNAME", "y_dream")
	cfg.QL2Password = getenv("QL2_PASSWORD", "Ql2india@009")
	cfg.QL2StartJob = getenv("QL2_START_JOB", "y")

	cfg.QL2Username_1 = getenv("QL2_USERNAME_1", "hariom_yadav")
	cfg.QL2Password_1 = getenv("QL2_PASSWORD_1", "Hariom@2524")

	cfg.QL2WebhookAPIKey = getenv("QL2_WEBHOOK_API_KEY", "ql2-webhook-api-key-change-in-production")

	cfg.JWTSecret = getenv("JWT_SECRET", "your-super-secret-jwt-key-change-this-in-production")
	cfg.JWTExpiry = time.Duration(getenvInt("JWT_EXPIRY_HOURS", 24)) * time.Hour

	cfg.DevMode = getenv("DEV_MODE", "true") == "false"

	// Optimize pool size for better performance with remote database
	// Increased from 10 to 25 to handle concurrent requests better
	cfg.PoolSize = getenvInt("DB_POOL_SIZE", 25)
	cfg.PoolRecycle = time.Duration(getenvInt("DB_POOL_RECYCLE_SECONDS", 300)) * time.Second // Increased to 5 minutes
	cfg.PoolPrePing = getenv("DB_POOL_PREPING", "true") == "true"
	// Reduced connect timeout for faster failure detection
	cfg.ConnectTimeout = time.Duration(getenvInt("DB_CONNECT_TIMEOUT_SECONDS", 10)) * time.Second
	cfg.ApplicationName = getenv("DB_APPLICATION_NAME", "front_insight_app")
	return cfg
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		var n int
		_, _ = fmt.Sscanf(v, "%d", &n)
		if n != 0 {
			return n
		}
	}
	return def
}

func defaultPgURL() string {
	user := getenv("POSTGRES_USER", "postgres_user")
	pass := getenv("POSTGRES_PASSWORD", "postgres_pass")
	host := getenv("POSTGRES_HOST", "localhost")
	port := getenv("POSTGRES_PORT", "5432")
	db := getenv("POSTGRES_DB", "postgres")
	return "postgresql://" + user + ":" + pass + "@" + host + ":" + port + "/" + db
}

// func defaultPgURL() string {
// 	user := getenv("POSTGRES_USER", "postgres")
// 	pass := getenv("POSTGRES_PASSWORD", "dank!f0r3st")
// 	host := getenv("POSTGRES_HOST", "10.0.3.230")
// 	port := getenv("POSTGRES_PORT", "5432")
// 	db := getenv("POSTGRES_DB", "postgres")
// 	return "postgresql://" + user + ":" + pass + "@" + host + ":" + port + "/" + db
// }
