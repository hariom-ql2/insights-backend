// @title Front Insight API
// @version 1.0
// @description A comprehensive hotel search and booking API with scheduled jobs and IST timezone support
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name MIT
// @license.url https://opensource.org/licenses/MIT

// @host localhost:5001
// @BasePath /
// @schemes http https

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

package main

import (
	"log"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"

	_ "github.com/frontinsight/backend/docs" // Import generated docs
	"github.com/frontinsight/backend/internal/config"
	"github.com/frontinsight/backend/internal/db"
	"github.com/frontinsight/backend/internal/server"
)

func main() {
	cfg := config.Load()

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.Logger())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))

	gormDB, err := db.Open(db.Config{
		DatabaseURL:     cfg.DatabaseURL,
		PoolSize:        cfg.PoolSize,
		PoolRecycle:     cfg.PoolRecycle,
		PoolPrePing:     cfg.PoolPrePing,
		ConnectTimeout:  cfg.ConnectTimeout,
		ApplicationName: cfg.ApplicationName,
	})
	if err != nil {
		log.Fatalf("db open error: %v", err)
	}

	_ = server.New(e, gormDB, cfg)

	// Add Swagger documentation endpoint
	e.GET("/swagger/*", echoSwagger.WrapHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = cfg.Port
	}
	e.Logger.Fatal(e.Start(":" + port))
}
