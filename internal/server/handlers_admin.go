package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/frontinsight/backend/internal/models"
	"github.com/labstack/echo/v4"
)

// AdminDashboard godoc
// @Summary Get admin dashboard statistics
// @Description Retrieve comprehensive statistics for the admin dashboard
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Success 200 {object} models.AdminDashboardStats "Dashboard statistics"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/dashboard [get]
func (s *Server) AdminDashboard(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Log admin activity
	s.logAdminActivity(user.ID, "view", "dashboard", nil, "", c)

	var stats models.AdminDashboardStats

	// Get total users
	var totalUsers int64
	s.DB.Model(&models.User{}).Count(&totalUsers)
	stats.TotalUsers = int(totalUsers)

	// Get active users (logged in within last 30 days)
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	var activeUsers int64
	s.DB.Model(&models.User{}).Where("last_login_at > ?", thirtyDaysAgo).Count(&activeUsers)
	stats.ActiveUsers = int(activeUsers)

	// Get new users today
	today := time.Now().Truncate(24 * time.Hour)
	var newUsersToday int64
	s.DB.Model(&models.User{}).Where("created_at >= ?", today).Count(&newUsersToday)
	stats.NewUsersToday = int(newUsersToday)

	// Get total searches
	var totalSearches int64
	s.DB.Model(&models.Search{}).Count(&totalSearches)
	stats.TotalSearches = int(totalSearches)

	// Get total collections
	var totalCollections int64
	s.DB.Model(&models.Collection{}).Count(&totalCollections)
	stats.TotalCollections = int(totalCollections)

	// Get total schedules
	var totalSchedules int64
	s.DB.Model(&models.Schedule{}).Count(&totalSchedules)
	stats.TotalSchedules = int(totalSchedules)

	// Get failed jobs
	var failedJobs int64
	s.DB.Model(&models.Search{}).Where("status = ?", "Failed").Count(&failedJobs)
	stats.FailedJobs = int(failedJobs)

	// Get completed jobs
	var completedJobs int64
	s.DB.Model(&models.Search{}).Where("status = ?", "Completed").Count(&completedJobs)
	stats.CompletedJobs = int(completedJobs)

	// Get total revenue (from payment orders)
	s.DB.Model(&models.PaymentOrder{}).Select("COALESCE(SUM(amount), 0)").Where("status = ?", "completed").Scan(&stats.Revenue)

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data":    stats,
	})
}

// AdminUsers godoc
// @Summary Get all users with statistics
// @Description Retrieve all users with their activity statistics
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param search query string false "Search by name or email"
// @Param role query string false "Filter by role"
// @Param verified query bool false "Filter by verification status"
// @Success 200 {object} map[string]interface{} "List of users with statistics"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/users [get]
func (s *Server) AdminUsers(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Log admin activity
	s.logAdminActivity(user.ID, "view", "users", nil, "", c)

	// Parse query parameters
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	search := c.QueryParam("search")
	role := c.QueryParam("role")
	verified := c.QueryParam("verified")

	offset := (page - 1) * limit

	// Build query
	query := s.DB.Model(&models.User{})

	if search != "" {
		query = query.Where("name ILIKE ? OR email ILIKE ?", "%"+search+"%", "%"+search+"%")
	}

	if role != "" {
		query = query.Where("role = ?", role)
	}

	if verified != "" {
		if verified == "true" {
			query = query.Where("is_verified = ?", true)
		} else if verified == "false" {
			query = query.Where("is_verified = ?", false)
		}
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Get users with statistics
	var users []models.UserWithStats
	err := query.Select(`
		users.*,
		(SELECT COUNT(*) FROM searches WHERE searches.user_id = users.email) as searches_count,
		(SELECT COUNT(*) FROM collections WHERE collections.user_id = users.email) as collections_count,
		(SELECT COUNT(*) FROM schedules WHERE schedules.user_id = users.email) as schedules_count,
		(SELECT MAX(last_login_at) FROM users WHERE users.email = users.email) as last_activity_at
	`).Offset(offset).Limit(limit).Order("created_at DESC").Scan(&users).Error

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to fetch users",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"users": users,
			"pagination": map[string]any{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		},
	})
}

// AdminUserDetails godoc
// @Summary Get detailed information about a specific user
// @Description Retrieve comprehensive details about a user including all their activities
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param id path int true "User ID"
// @Success 200 {object} map[string]interface{} "User details with activities"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/users/{id} [get]
func (s *Server) AdminUserDetails(c echo.Context) error {
	user := c.Get("user").(*models.User)
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Invalid user ID",
		})
	}

	// Get user details
	var targetUser models.User
	if err := s.DB.First(&targetUser, userID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{
			"success": false,
			"message": "User not found",
		})
	}

	// Log admin activity
	s.logAdminActivity(user.ID, "view", "user", &targetUser.ID, fmt.Sprintf("Viewed user: %s", targetUser.Email), c)

	// Get user statistics
	var stats struct {
		SearchesCount    int `json:"searches_count"`
		CollectionsCount int `json:"collections_count"`
		SchedulesCount   int `json:"schedules_count"`
		PaymentOrders    int `json:"payment_orders"`
	}

	var searchesCount int64
	s.DB.Model(&models.Search{}).Where("user_id = ?", targetUser.Email).Count(&searchesCount)
	stats.SearchesCount = int(searchesCount)

	var collectionsCount int64
	s.DB.Model(&models.Collection{}).Where("user_id = ?", targetUser.Email).Count(&collectionsCount)
	stats.CollectionsCount = int(collectionsCount)

	var schedulesCount int64
	s.DB.Model(&models.Schedule{}).Where("user_id = ?", targetUser.Email).Count(&schedulesCount)
	stats.SchedulesCount = int(schedulesCount)

	var paymentOrders int64
	s.DB.Model(&models.PaymentOrder{}).Where("user_id = ?", targetUser.Email).Count(&paymentOrders)
	stats.PaymentOrders = int(paymentOrders)

	// Get recent searches
	var recentSearches []models.Search
	s.DB.Where("user_id = ?", targetUser.Email).Order("created_at DESC").Limit(10).Find(&recentSearches)

	// Get recent collections
	var recentCollections []models.Collection
	s.DB.Where("user_id = ?", targetUser.Email).Order("created_at DESC").Limit(10).Find(&recentCollections)

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"user":               targetUser,
			"stats":              stats,
			"recent_searches":    recentSearches,
			"recent_collections": recentCollections,
		},
	})
}

// AdminUpdateUser godoc
// @Summary Update user information
// @Description Update user details including role and verification status
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path int true "User ID"
// @Param request body map[string]interface{} true "User update data"
// @Success 200 {object} map[string]interface{} "User updated successfully"
// @Failure 400 {object} map[string]string "Bad request"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "User not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/users/{id} [put]
func (s *Server) AdminUpdateUser(c echo.Context) error {
	adminUser := c.Get("user").(*models.User)
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Invalid user ID",
		})
	}

	// Get user details
	var targetUser models.User
	if err := s.DB.First(&targetUser, userID).Error; err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{
			"success": false,
			"message": "User not found",
		})
	}

	// Parse update data
	var updateData map[string]interface{}
	if err := c.Bind(&updateData); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"message": "Invalid request data",
		})
	}

	// Track changes for audit log
	changes := make(map[string]interface{})

	// Update allowed fields
	if name, ok := updateData["name"].(string); ok && name != "" {
		changes["name"] = map[string]interface{}{"old": targetUser.Name, "new": name}
		targetUser.Name = name
	}

	if role, ok := updateData["role"].(string); ok && role != "" {
		// Only super_admin can change roles
		if adminUser.Role != "super_admin" {
			return c.JSON(http.StatusForbidden, map[string]any{
				"success": false,
				"message": "Only super admin can change user roles",
			})
		}
		changes["role"] = map[string]interface{}{"old": targetUser.Role, "new": role}
		targetUser.Role = role
	}

	if isVerified, ok := updateData["is_verified"].(bool); ok {
		changes["is_verified"] = map[string]interface{}{"old": targetUser.IsVerified, "new": isVerified}
		targetUser.IsVerified = isVerified
	}

	// Save changes
	if err := s.DB.Save(&targetUser).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to update user",
		})
	}

	// Log admin activity
	details := fmt.Sprintf("Updated user: %s, changes: %+v", targetUser.Email, changes)
	s.logAdminActivity(adminUser.ID, "update", "user", &targetUser.ID, details, c)

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": "User updated successfully",
		"data":    targetUser,
	})
}

// AdminSearches godoc
// @Summary Get all searches with user information
// @Description Retrieve all searches across all users with pagination and filtering
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param status query string false "Filter by status"
// @Param user_id query string false "Filter by user email"
// @Param scheduled query bool false "Filter by scheduled status"
// @Success 200 {object} map[string]interface{} "List of searches with user information"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/searches [get]
func (s *Server) AdminSearches(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Log admin activity
	s.logAdminActivity(user.ID, "view", "searches", nil, "", c)

	// Parse query parameters
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	status := c.QueryParam("status")
	userID := c.QueryParam("user_id")
	scheduled := c.QueryParam("scheduled")

	offset := (page - 1) * limit

	// Build query
	query := s.DB.Table("searches").
		Select("searches.*, users.name as user_name, users.email as user_email").
		Joins("LEFT JOIN users ON searches.user_id = users.email")

	if status != "" {
		query = query.Where("searches.status = ?", status)
	}

	if userID != "" {
		query = query.Where("searches.user_id = ?", userID)
	}

	if scheduled != "" {
		if scheduled == "true" {
			query = query.Where("searches.scheduled = ?", true)
		} else if scheduled == "false" {
			query = query.Where("searches.scheduled = ?", false)
		}
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Get searches
	var searches []models.SearchWithUser
	err := query.Offset(offset).Limit(limit).Order("searches.created_at DESC").Scan(&searches).Error

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to fetch searches",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"searches": searches,
			"pagination": map[string]any{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		},
	})
}

// AdminCollections godoc
// @Summary Get all collections with user information
// @Description Retrieve all collections across all users with pagination and filtering
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param status query string false "Filter by status"
// @Param user_id query string false "Filter by user email"
// @Success 200 {object} map[string]interface{} "List of collections with user information"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/collections [get]
func (s *Server) AdminCollections(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Log admin activity
	s.logAdminActivity(user.ID, "view", "collections", nil, "", c)

	// Parse query parameters
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	status := c.QueryParam("status")
	userID := c.QueryParam("user_id")

	offset := (page - 1) * limit

	// Build query
	query := s.DB.Table("collections").
		Select("collections.*, users.name as user_name, users.email as user_email").
		Joins("LEFT JOIN users ON collections.user_id = users.email")

	if status != "" {
		query = query.Where("collections.status = ?", status)
	}

	if userID != "" {
		query = query.Where("collections.user_id = ?", userID)
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Get collections
	var collections []models.CollectionWithUser
	err := query.Offset(offset).Limit(limit).Order("collections.created_at DESC").Scan(&collections).Error

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to fetch collections",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"collections": collections,
			"pagination": map[string]any{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		},
	})
}

// AdminActivities godoc
// @Summary Get admin activity log
// @Description Retrieve audit log of admin activities
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(20)
// @Param admin_id query int false "Filter by admin ID"
// @Param action query string false "Filter by action"
// @Param resource query string false "Filter by resource"
// @Success 200 {object} map[string]interface{} "List of admin activities"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /admin/activities [get]
func (s *Server) AdminActivities(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Log admin activity
	s.logAdminActivity(user.ID, "view", "activities", nil, "", c)

	// Parse query parameters
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	adminID := c.QueryParam("admin_id")
	action := c.QueryParam("action")
	resource := c.QueryParam("resource")

	offset := (page - 1) * limit

	// Build query
	query := s.DB.Table("admin_activities").
		Select("admin_activities.*, users.name as admin_name, users.email as admin_email").
		Joins("LEFT JOIN users ON admin_activities.admin_id = users.id")

	if adminID != "" {
		query = query.Where("admin_activities.admin_id = ?", adminID)
	}

	if action != "" {
		query = query.Where("admin_activities.action = ?", action)
	}

	if resource != "" {
		query = query.Where("admin_activities.resource = ?", resource)
	}

	// Get total count
	var total int64
	query.Count(&total)

	// Get activities
	var activities []struct {
		models.AdminActivity
		AdminName  string `json:"admin_name"`
		AdminEmail string `json:"admin_email"`
	}

	err := query.Offset(offset).Limit(limit).Order("admin_activities.created_at DESC").Scan(&activities).Error

	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to fetch activities",
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"activities": activities,
			"pagination": map[string]any{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		},
	})
}

// AdminSchedules handles GET /admin/schedules
func (s *Server) AdminSchedules(c echo.Context) error {
	// Log admin activity
	user := c.Get("user").(*models.User)
	s.logAdminActivity(user.ID, "view", "schedules", nil, "", c)

	// Parse query parameters
	page := 1
	limit := 20
	if p := c.QueryParam("page"); p != "" {
		if parsedPage, err := strconv.Atoi(p); err == nil && parsedPage > 0 {
			page = parsedPage
		}
	}
	if l := c.QueryParam("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}

	// Simple count query first
	var total int64
	if err := s.DB.Raw("SELECT COUNT(*) FROM schedules").Scan(&total).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to count schedules: " + err.Error(),
		})
	}

	// Simple data query
	var schedules []map[string]any
	offset := (page - 1) * limit
	if err := s.DB.Raw("SELECT * FROM schedules ORDER BY created_at DESC LIMIT ? OFFSET ?", limit, offset).Scan(&schedules).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": "Failed to fetch schedules: " + err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"schedules": schedules,
			"pagination": map[string]any{
				"page":       page,
				"limit":      limit,
				"total":      total,
				"totalPages": (total + int64(limit) - 1) / int64(limit),
			},
		},
	})
}
