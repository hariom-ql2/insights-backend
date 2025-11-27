package server

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	jwtTokenURL = "http://error-monitoring-prod.us-west-2.elasticbeanstalk.com/error/monitor/getJwtGenerationToken"
)

// fetchJWTToken fetches the JWT token from the external endpoint
func (s *Server) fetchJWTToken() (string, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, err := http.NewRequest("GET", jwtTokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create JWT token request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("JWT token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to retrieve JWT token with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read the token (API returns token as plain text)
	tokenBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read JWT token: %w", err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return "", fmt.Errorf("JWT token is empty")
	}

	return token, nil
}

// GetCompetitorRateTrackerDashboard godoc
// @Summary Get Competitor Rate Tracker Dashboard JWT token and URL
// @Description Fetches JWT token and returns Tableau dashboard URL for competitor rate tracker
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "JWT token and Tableau URL"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /reports/competitor-rate-tracker [get]
func (s *Server) GetCompetitorRateTrackerDashboard(c echo.Context) error {
	token, err := s.fetchJWTToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"url":     "https://us-west-2b.online.tableau.com/#/site/ql2/views/competitor_rate_tracker/CompetitorRateTracker?:iid=1",
	})
}

// GetMarketViewDashboard godoc
// @Summary Get Market View Dashboard JWT token and URL
// @Description Fetches JWT token and returns Tableau dashboard URL for market view
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "JWT token and Tableau URL"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /reports/market-view [get]
func (s *Server) GetMarketViewDashboard(c echo.Context) error {
	token, err := s.fetchJWTToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"url":     "https://us-west-2b.online.tableau.com/#/site/ql2/views/market_view/MarketView?:iid=1",
	})
}

// GetStarRatingTrendDashboard godoc
// @Summary Get Star Rating Trend Dashboard JWT token and URL
// @Description Fetches JWT token and returns Tableau dashboard URL for star rating trend
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "JWT token and Tableau URL"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /reports/star-rating-trend [get]
func (s *Server) GetStarRatingTrendDashboard(c echo.Context) error {
	token, err := s.fetchJWTToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"url":     "https://us-west-2b.online.tableau.com/#/site/ql2/views/star_rating_trend/StarRating?:iid=1",
	})
}

// GetPriceSuggestionDashboard godoc
// @Summary Get Price Suggestion Dashboard JWT token and URL
// @Description Fetches JWT token and returns Tableau dashboard URL for price suggestion
// @Tags Reports
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "JWT token and Tableau URL"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /reports/price-suggestion [get]
func (s *Server) GetPriceSuggestionDashboard(c echo.Context) error {
	token, err := s.fetchJWTToken()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{
			"success": false,
			"message": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"token":   token,
		"url":     "https://us-west-2b.online.tableau.com/#/site/ql2/views/price_suggestion/price_suggestion?:iid=1",
	})
}
