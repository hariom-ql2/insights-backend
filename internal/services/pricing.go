package services

import (
	"errors"
	"fmt"
	"strings"

	"github.com/frontinsight/backend/internal/models"
	"gorm.io/gorm"
)

// GetPriceForWebsite retrieves the price for a website from site_to_price_mapping table
// Tries to match by code first, then falls back to name
func GetPriceForWebsite(websiteName string, db *gorm.DB) (float64, error) {
	var mapping models.SiteToPriceMapping

	// Normalize website name for matching
	normalizedName := strings.ToUpper(strings.TrimSpace(websiteName))

	// Try to match by code first
	// Convert website name to code using the same logic as toScriptCode
	code := toScriptCodeFromName(normalizedName)

	err := db.Where("code = ?", code).First(&mapping).Error
	if err == nil {
		return mapping.Price, nil
	}
	// If record not found by code, that's expected - we'll try name lookup next
	// Only proceed if it's a "record not found" error, otherwise return the error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, fmt.Errorf("failed to lookup price by code for website %s: %v", websiteName, err)
	}

	// If not found by code, try to match by name
	err = db.Where("UPPER(TRIM(name)) = ?", normalizedName).First(&mapping).Error
	if err == nil {
		return mapping.Price, nil
	}

	// If still not found, return error
	// Only return error if both code and name lookups failed
	return 0, fmt.Errorf("price not found for website: %s (tried code: %s and name: %s)", websiteName, code, normalizedName)
}

// CalculateSearchAmount calculates the total amount for a search based on search items
// Sums prices for all search items (one price per website used)
func CalculateSearchAmount(searchItems []models.SearchItem, db *gorm.DB) (float64, error) {
	if len(searchItems) == 0 {
		return 0, nil
	}

	// Track unique websites to sum prices only once per website
	websitePrices := make(map[string]float64)
	totalAmount := 0.0

	for _, item := range searchItems {
		website := item.Website

		// If we've already calculated price for this website, skip
		if _, exists := websitePrices[website]; exists {
			continue
		}

		// Get price for this website
		price, err := GetPriceForWebsite(website, db)
		if err != nil {
			return 0, fmt.Errorf("failed to get price for website %s: %v", website, err)
		}

		websitePrices[website] = price
		totalAmount += price
	}

	return totalAmount, nil
}

// CalculateSearchAmountFromJobs calculates the total amount for a search based on job data
// This is used when we have job data but not yet search items
func CalculateSearchAmountFromJobs(jobs []models.JobData, db *gorm.DB) (float64, error) {
	if len(jobs) == 0 {
		return 0, nil
	}

	// Track unique websites to sum prices only once per website
	websitePrices := make(map[string]float64)
	totalAmount := 0.0

	for _, job := range jobs {
		website := job.Website.Name

		// If we've already calculated price for this website, skip
		if _, exists := websitePrices[website]; exists {
			continue
		}

		// Get price for this website
		price, err := GetPriceForWebsite(website, db)
		if err != nil {
			return 0, fmt.Errorf("failed to get price for website %s: %v", website, err)
		}

		websitePrices[website] = price
		totalAmount += price
	}

	return totalAmount, nil
}

// toScriptCodeFromName converts a website name to its script code
// This is a helper function similar to toScriptCode in handlers_collections_searches.go
func toScriptCodeFromName(name string) string {
	siteFullNameToCode := map[string]string{
		"EXPEDIA":      "EXP",
		"PRICELINE":    "PL",
		"MARRIOTT":     "MC",
		"CHOICEHOTELS": "CH",
		"BESTWESTERN":  "BW",
		"REDROOF":      "RR",
		"ACCORHOTELS":  "RT",
	}

	u := strings.ToUpper(strings.TrimSpace(name))
	if code, ok := siteFullNameToCode[u]; ok {
		return code
	}
	return name
}
