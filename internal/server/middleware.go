package server

import (
	"net/http"
	"strings"

	"github.com/frontinsight/backend/internal/models"
	"github.com/frontinsight/backend/internal/utils"
	"github.com/labstack/echo/v4"
)

// JWTMiddleware validates JWT tokens and sets user context
func (s *Server) JWTMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Authorization header required",
				})
			}

			// Extract token from "Bearer <token>"
			tokenParts := strings.Split(authHeader, " ")
			if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Invalid authorization header format",
				})
			}

			tokenString := tokenParts[1]
			claims, err := utils.ValidateJWT(tokenString, s.Cfg.JWTSecret)
			if err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Invalid or expired token",
				})
			}

			// Check if user still exists and session is valid
			var user models.User
			if err := s.DB.Where("id = ? AND session_token = ?", claims.UserID, tokenString).First(&user).Error; err != nil {
				return c.JSON(http.StatusUnauthorized, map[string]interface{}{
					"success": false,
					"message": "Session expired or invalid",
				})
			}

			// Set user context
			c.Set("user", &user)
			c.Set("user_id", claims.UserID)
			c.Set("user_email", claims.Email)

			// Session tracking removed - no longer needed for polling

			return next(c)
		}
	}
}

// OptionalJWTMiddleware validates JWT tokens if present but doesn't require them
func (s *Server) OptionalJWTMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return next(c)
			}

			// Extract token from "Bearer <token>"
			tokenParts := strings.Split(authHeader, " ")
			if len(tokenParts) != 2 || tokenParts[0] != "Bearer" {
				return next(c)
			}

			tokenString := tokenParts[1]
			claims, err := utils.ValidateJWT(tokenString, s.Cfg.JWTSecret)
			if err != nil {
				return next(c)
			}

			// Check if user still exists and session is valid
			var user models.User
			if err := s.DB.Where("id = ? AND session_token = ?", claims.UserID, tokenString).First(&user).Error; err != nil {
				return next(c)
			}

			// Set user context
			c.Set("user", &user)
			c.Set("user_id", claims.UserID)
			c.Set("user_email", claims.Email)

			return next(c)
		}
	}
}
