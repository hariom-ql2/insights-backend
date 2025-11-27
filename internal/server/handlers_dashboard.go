package server

import (
	"net/http"
	"sort"
	"strings"

	"github.com/frontinsight/backend/internal/models"
	"github.com/labstack/echo/v4"
)

// DashboardStats godoc
// @Summary Get user dashboard statistics
// @Description Retrieve dashboard statistics for the authenticated user including search counts and chart data
// @Tags Dashboard
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Dashboard statistics"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /dashboard/stats [get]
func (s *Server) DashboardStats(c echo.Context) error {
	user := c.Get("user").(*models.User)
	userIDStr := user.Email

	// Get search counts by status
	var activeCount int64
	var completedCount int64
	var scheduledCount int64

	s.DB.Model(&models.Search{}).Where("user_id = ? AND status = ?", userIDStr, "Executing").Count(&activeCount)
	s.DB.Model(&models.Search{}).Where("user_id = ? AND status = ?", userIDStr, "Completed").Count(&completedCount)
	s.DB.Model(&models.Schedule{}).Where("user_id = ? AND is_active = ?", userIDStr, true).Count(&scheduledCount)

	// Multi-site analysis - get counts by website from search items
	// Only show websites that are found in search_items and are in the standard sites list
	type SiteCount struct {
		Website string
		Count   int64
	}
	var siteCounts []SiteCount

	err := s.DB.Model(&models.SearchItem{}).
		Select("search_items.website, COUNT(*) as count").
		Joins("JOIN searches ON search_items.search_id = searches.id").
		Where("searches.user_id = ?", userIDStr).
		Group("search_items.website").
		Order("count DESC").
		Scan(&siteCounts).Error

	if err != nil {
		// Log error but continue with empty data
		c.Logger().Errorf("Error fetching site counts: %v", err)
	} else {
		// Debug: Log what websites were found
		if len(siteCounts) > 0 {
			c.Logger().Infof("Found %d unique websites for user %s", len(siteCounts), userIDStr)
			for _, sc := range siteCounts {
				c.Logger().Infof("  Website: %s, Count: %d", sc.Website, sc.Count)
			}
		} else {
			c.Logger().Infof("No search items found for user %s", userIDStr)
		}
	}

	// Standard sites list with multiple possible name variations
	// Map of normalized keys to display names
	standardSitesMap := map[string]string{
		// Booking.com variations
		"booking.com": "Booking.com",
		"booking":     "Booking.com",
		"bookingcom":  "Booking.com",
		// Trivago variations
		"trivago": "Trivago",
		// Expedia variations
		"expedia": "Expedia",
		// Agoda variations
		"agoda": "Agoda",
		// Hotels.com variations
		"hotels.com": "Hotels.com",
		"hotels":     "Hotels.com",
		"hotelscom":  "Hotels.com",
		// TripAdvisor variations
		"tripadvisor":  "TripAdvisor",
		"trip advisor": "TripAdvisor",
		"trip-advisor": "TripAdvisor",
	}

	// Normalize website name for matching (more flexible)
	normalizeWebsite := func(name string) string {
		normalized := strings.ToLower(strings.TrimSpace(name))
		// Remove spaces and hyphens for matching
		normalized = strings.ReplaceAll(normalized, " ", "")
		normalized = strings.ReplaceAll(normalized, "-", "")
		normalized = strings.ReplaceAll(normalized, "_", "")
		return normalized
	}

	// Also create a map that includes the normalized version with dots removed
	normalizedSites := make(map[string]string)
	for key, value := range standardSitesMap {
		normalizedSites[normalizeWebsite(key)] = value
		// Also add version without dots
		noDots := strings.ReplaceAll(normalizeWebsite(key), ".", "")
		if noDots != normalizeWebsite(key) {
			normalizedSites[noDots] = value
		}
	}

	// Find total count and max count for normalization (only from standard sites)
	var maxCount int64
	var totalCount int64
	siteCountMap := make(map[string]int64)   // Map display name to aggregated count
	unmatchedSites := make(map[string]int64) // Track unmatched sites for debugging

	for _, sc := range siteCounts {
		normalized := normalizeWebsite(sc.Website)
		matched := false

		// Try exact normalized match first
		if displayName, exists := normalizedSites[normalized]; exists {
			siteCountMap[displayName] += sc.Count
			if siteCountMap[displayName] > maxCount {
				maxCount = siteCountMap[displayName]
			}
			totalCount += sc.Count
			c.Logger().Infof("Matched website: '%s' -> '%s' (count: %d)", sc.Website, displayName, sc.Count)
			matched = true
		} else {
			// Try without dots
			noDots := strings.ReplaceAll(normalized, ".", "")
			if displayName, exists := normalizedSites[noDots]; exists {
				siteCountMap[displayName] += sc.Count
				if siteCountMap[displayName] > maxCount {
					maxCount = siteCountMap[displayName]
				}
				totalCount += sc.Count
				c.Logger().Infof("Matched website (no dots): '%s' -> '%s' (count: %d)", sc.Website, displayName, sc.Count)
				matched = true
			}
		}

		if !matched {
			// Try fuzzy matching - check if normalized contains any standard site key
			for normalizedKey, displayName := range normalizedSites {
				// Only match if the key is a significant part of the website name (at least 4 chars)
				if len(normalizedKey) >= 4 && (strings.Contains(normalized, normalizedKey) || strings.Contains(normalizedKey, normalized)) {
					siteCountMap[displayName] += sc.Count
					if siteCountMap[displayName] > maxCount {
						maxCount = siteCountMap[displayName]
					}
					totalCount += sc.Count
					c.Logger().Infof("Matched website (fuzzy): '%s' -> '%s' (count: %d)", sc.Website, displayName, sc.Count)
					matched = true
					break
				}
			}
		}

		if !matched {
			// Track unmatched websites for debugging
			unmatchedSites[sc.Website] = sc.Count
		}
	}

	// Log unmatched websites
	if len(unmatchedSites) > 0 {
		c.Logger().Warnf("Unmatched websites found for user %s:", userIDStr)
		for site, count := range unmatchedSites {
			c.Logger().Warnf("  '%s' (count: %d, normalized: '%s')", site, count, normalizeWebsite(site))
		}
	}

	// Standard 6 websites in priority order (top 6)
	standardTop6 := []string{
		"Booking.com",
		"Trivago",
		"Expedia",
		"Agoda",
		"Hotels.com",
		"TripAdvisor",
	}

	// Build multi-site data with websites that have data, sorted by count descending
	multiSiteData := make([]map[string]any, 0)
	for displayName, count := range siteCountMap {
		var value int
		// Calculate percentage based on total usage (not max), so all sites show relative to total
		if totalCount > 0 {
			value = int((float64(count) / float64(totalCount)) * 100)
		} else {
			value = 0
		}
		multiSiteData = append(multiSiteData, map[string]any{
			"name":  displayName,
			"value": value,
			"count": count, // Store count for sorting and display
		})
	}

	// Sort by count descending (websites with data first)
	sort.Slice(multiSiteData, func(i, j int) bool {
		return multiSiteData[i]["count"].(int64) > multiSiteData[j]["count"].(int64)
	})

	// Create a map of sites that have data
	sitesWithData := make(map[string]bool)
	for _, site := range multiSiteData {
		sitesWithData[site["name"].(string)] = true
	}

	// Build final list: top 5 websites
	// First add sites with data (sorted by count), then fill with zero-usage sites
	finalMultiSiteData := make([]map[string]any, 0)

	// Add sites with data first (up to 6, sorted by count descending)
	for _, site := range multiSiteData {
		if len(finalMultiSiteData) >= 6 {
			break
		}
		finalMultiSiteData = append(finalMultiSiteData, map[string]any{
			"name":  site["name"],
			"value": site["value"],
			"count": site["count"], // Include count for chart display
		})
	}

	// Fill remaining slots with zero-usage sites from standardTop6
	// Only add sites that aren't already in the list
	for _, siteName := range standardTop6 {
		if len(finalMultiSiteData) >= 6 {
			break
		}
		// Check if this site is already in the list
		alreadyAdded := false
		for _, existing := range finalMultiSiteData {
			if existing["name"].(string) == siteName {
				alreadyAdded = true
				break
			}
		}
		if !alreadyAdded {
			finalMultiSiteData = append(finalMultiSiteData, map[string]any{
				"name":  siteName,
				"value": 0,
				"count": int64(0), // Include count for consistency
			})
		}
	}

	// Ensure we always have exactly 6 sites (should always be true, but safety check)
	if len(finalMultiSiteData) > 6 {
		finalMultiSiteData = finalMultiSiteData[:6]
	} else if len(finalMultiSiteData) < 6 {
		// This shouldn't happen, but fill with remaining standard sites if needed
		for _, siteName := range standardTop6 {
			if len(finalMultiSiteData) >= 6 {
				break
			}
			alreadyAdded := false
			for _, existing := range finalMultiSiteData {
				if existing["name"].(string) == siteName {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				finalMultiSiteData = append(finalMultiSiteData, map[string]any{
					"name":  siteName,
					"value": 0,
					"count": int64(0), // Include count for consistency
				})
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"active":        int(activeCount),
			"completed":     int(completedCount),
			"scheduled":     int(scheduledCount),
			"multiSiteData": finalMultiSiteData,
		},
	})
}
