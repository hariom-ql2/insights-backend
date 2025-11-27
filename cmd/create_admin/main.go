package main

import (
	"fmt"
	"log"
	"os"

	"github.com/frontinsight/backend/internal/config"
	"github.com/frontinsight/backend/internal/db"
	"github.com/frontinsight/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run create_admin.go <email> <password> <name> [role]")
		fmt.Println("Example: go run create_admin.go admin@example.com password123 Admin User")
		fmt.Println("Role can be: admin, super_admin (default: admin)")
		os.Exit(1)
	}

	email := os.Args[1]
	password := os.Args[2]
	name := os.Args[3]
	role := "admin"
	if len(os.Args) > 4 {
		role = os.Args[4]
	}

	// Validate role
	if role != "admin" && role != "super_admin" {
		fmt.Printf("Invalid role: %s. Must be 'admin' or 'super_admin'\n", role)
		os.Exit(1)
	}

	// Load configuration
	cfg := config.Load()

	// Connect to database
	gormDB, err := db.Open(db.Config{
		DatabaseURL:     cfg.DatabaseURL,
		PoolSize:        cfg.PoolSize,
		PoolRecycle:     cfg.PoolRecycle,
		PoolPrePing:     cfg.PoolPrePing,
		ConnectTimeout:  cfg.ConnectTimeout,
		ApplicationName: cfg.ApplicationName,
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Auto-migrate schema
	err = gormDB.AutoMigrate(&models.User{})
	if err != nil {
		log.Fatalf("Failed to migrate schema: %v", err)
	}

	// Check if user already exists
	var existingUser models.User
	result := gormDB.Where("email = ?", email).First(&existingUser)
	if result.Error == nil {
		fmt.Printf("User with email %s already exists\n", email)
		os.Exit(1)
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	// Create admin user
	adminUser := models.User{
		Email:      email,
		Name:       name,
		Password:   string(hashedPassword),
		Role:       role,
		IsVerified: true, // Auto-verify admin users
	}

	result = gormDB.Create(&adminUser)
	if result.Error != nil {
		log.Fatalf("Failed to create admin user: %v", result.Error)
	}

	fmt.Printf("✅ Admin user created successfully!\n")
	fmt.Printf("Email: %s\n", email)
	fmt.Printf("Name: %s\n", name)
	fmt.Printf("Role: %s\n", role)
	fmt.Printf("ID: %d\n", adminUser.ID)
	fmt.Printf("\nYou can now login with these credentials.\n")
}
