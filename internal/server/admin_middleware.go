package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/frontinsight/backend/internal/models"
	"github.com/labstack/echo/v4"
)

// AdminMiddleware checks if the user has admin privileges
func (s *Server) AdminMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := c.Get("user").(*models.User)

			// Check if user has admin role
			if user.Role != "admin" && user.Role != "super_admin" {
				return c.JSON(http.StatusForbidden, map[string]any{
					"success": false,
					"message": "Admin access required",
				})
			}

			return next(c)
		}
	}
}

// SuperAdminMiddleware checks if the user has super admin privileges
func (s *Server) SuperAdminMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user := c.Get("user").(*models.User)

			// Check if user has super admin role
			if user.Role != "super_admin" {
				return c.JSON(http.StatusForbidden, map[string]any{
					"success": false,
					"message": "Super admin access required",
				})
			}

			return next(c)
		}
	}
}

// logAdminActivity logs admin activities for audit trail
func (s *Server) logAdminActivity(adminID uint, action, resource string, resourceID *uint, details string, c echo.Context) {
	ipAddress := c.Request().Header.Get("X-Forwarded-For")
	if ipAddress == "" {
		ipAddress = c.RealIP()
	}

	userAgent := c.Request().Header.Get("User-Agent")

	// Ensure details is valid JSON
	var detailsPtr *string
	if details == "" {
		detailsPtr = nil
	} else {
		detailsPtr = &details
	}

	activity := models.AdminActivity{
		AdminID:    adminID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    detailsPtr,
		IPAddress:  ipAddress,
		UserAgent:  userAgent,
	}

	// Log asynchronously to avoid blocking the main request
	go func() {
		if err := s.DB.Create(&activity).Error; err != nil {
			// Log the error but don't fail the main request
			fmt.Printf("Failed to log admin activity: %v\n", err)
		}
	}()
}

// getClientIP extracts the client IP address from the request
func (s *Server) getClientIP(c echo.Context) string {
	ip := c.Request().Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = c.Request().Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = c.RealIP()
	}

	// Handle comma-separated IPs (from proxies)
	if strings.Contains(ip, ",") {
		ips := strings.Split(ip, ",")
		ip = strings.TrimSpace(ips[0])
	}

	return ip
}
