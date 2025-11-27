package services

import (
	"context"
	"fmt"
	"time"

	"github.com/frontinsight/backend/internal/config"
	"github.com/frontinsight/backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"gorm.io/gorm"
)

// CheckBalanceAndFreeze checks if user has sufficient balance and freezes the amount
// Returns error if balance is insufficient
func CheckBalanceAndFreeze(userID string, amount float64, searchID uint, db *gorm.DB) error {
	if amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}

	// Get user
	var user models.User
	if err := db.Where("email = ?", userID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found: %v", err)
	}

	// Check if user has sufficient balance
	// Balance must be >= (current frozen_amount + new amount)
	// requiredBalance := user.FrozenAmount + amount
	requiredBalance := amount
	if user.Balance < requiredBalance {
		return fmt.Errorf("insufficient balance: required %.2f, available %.2f", requiredBalance, user.Balance)
	}

	// Start transaction
	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Update user: add to frozen_amount, deduct from balance
	user.FrozenAmount += amount
	user.Balance -= amount

	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update user balance: %v", err)
	}

	// Update search's frozen_amount
	var search models.Search
	if err := tx.First(&search, searchID).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("search not found: %v", err)
	}

	search.FrozenAmount = amount
	if err := tx.Save(&search).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update search frozen_amount: %v", err)
	}

	// Create transaction record
	description := fmt.Sprintf("Frozen amount for search #%d", searchID)
	transaction := models.Transaction{
		UserID:        userID,
		SearchID:      &searchID,
		TxnType:       "freeze",
		Amount:        amount,
		BalanceBefore: user.Balance + amount, // Balance before freeze
		BalanceAfter:  user.Balance,          // Balance after freeze
		Description:   &description,
		Status:        "completed",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create transaction record: %v", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

// CalculateDeductedAmount calculates the deducted amount from run_billing_summary
// Queries run_billing_summary by run_id and calculates: billing_inputs * price for each row
func CalculateDeductedAmount(runID int64, db *gorm.DB, cfg config.AppConfig) (float64, error) {
	// Connect to RunDB
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.RunDBHost, cfg.RunDBPort, cfg.RunDBUser, cfg.RunDBPass, cfg.RunDBName)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return 0, fmt.Errorf("failed to connect to RunDB: %v", err)
	}
	defer pool.Close()

	// Query run_billing_summary
	rows, err := pool.Query(ctx, "SELECT script, billing_inputs FROM run_billing_summary WHERE run_id = $1", runID)
	if err != nil {
		return 0, fmt.Errorf("failed to query run_billing_summary: %v", err)
	}
	defer rows.Close()

	totalDeducted := 0.0

	for rows.Next() {
		var script string
		var billingInputs int

		if err := rows.Scan(&script, &billingInputs); err != nil {
			return 0, fmt.Errorf("failed to scan row: %v", err)
		}

		// Get price from site_to_price_mapping using script
		// Try code first, then name
		price, err := GetPriceForWebsite(script, db)
		if err != nil {
			// If price not found, skip this row (log warning but continue)
			fmt.Printf("Warning: price not found for script %s, skipping\n", script)
			continue
		}

		// Calculate: billing_inputs * price
		deductedForRow := float64(billingInputs) * price
		totalDeducted += deductedForRow
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating rows: %v", err)
	}

	return totalDeducted, nil
}

// ProcessSearchCompletion processes a completed search
// Calculates deducted_amount, refunded_amount, and updates user balance
func ProcessSearchCompletion(search *models.Search, db *gorm.DB, cfg config.AppConfig) error {
	if search.FrozenAmount <= 0 {
		// Nothing to process if no frozen amount
		return nil
	}

	if search.RunID == nil {
		// If no run_id, we can't calculate deducted amount
		// Refund full frozen amount
		return ProcessSearchFailure(search, db)
	}

	// Calculate deducted amount from run_billing_summary
	deductedAmount, err := CalculateDeductedAmount(*search.RunID, db, cfg)
	if err != nil {
		// If calculation fails, refund full frozen amount
		fmt.Printf("Warning: failed to calculate deducted amount for search %d: %v, refunding full amount\n", search.ID, err)
		return ProcessSearchFailure(search, db)
	}

	// Calculate refunded amount
	refundedAmount := search.FrozenAmount - deductedAmount

	// Get user
	var user models.User
	if err := db.Where("email = ?", search.UserID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found: %v", err)
	}

	// Capture initial balance before processing
	initialBalance := user.Balance

	// Start transaction
	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Unfreeze: reduce user's frozen_amount by search's frozen_amount
	user.FrozenAmount -= search.FrozenAmount
	if user.FrozenAmount < 0 {
		user.FrozenAmount = 0 // Prevent negative frozen amount
	}

	// Refund: add refunded_amount to user's balance
	user.Balance += refundedAmount

	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update user balance: %v", err)
	}

	// Create transaction records
	searchID := search.ID

	// Unfreeze transaction (balance doesn't change, only frozen_amount changes)
	unfreezeDesc := fmt.Sprintf("Unfreeze amount for completed search #%d", searchID)
	unfreezeTxn := models.Transaction{
		UserID:        search.UserID,
		SearchID:      &searchID,
		TxnType:       "unfreeze",
		Amount:        search.FrozenAmount,
		BalanceBefore: initialBalance,
		BalanceAfter:  initialBalance, // Balance doesn't change on unfreeze
		Description:   &unfreezeDesc,
		Status:        "completed",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := tx.Create(&unfreezeTxn).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create unfreeze transaction: %v", err)
	}

	// Refund transaction
	if refundedAmount > 0 {
		refundDesc := fmt.Sprintf("Refund for completed search #%d (deducted: %.2f)", searchID, deductedAmount)
		refundTxn := models.Transaction{
			UserID:        search.UserID,
			SearchID:      &searchID,
			TxnType:       "refund",
			Amount:        refundedAmount,
			BalanceBefore: initialBalance,
			BalanceAfter:  user.Balance,
			Description:   &refundDesc,
			Status:        "completed",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := tx.Create(&refundTxn).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create refund transaction: %v", err)
		}
	}

	// Debit transaction (for deducted amount - this is just a record, balance already reflects the deduction)
	if deductedAmount > 0 {
		debitDesc := fmt.Sprintf("Deduction for completed search #%d", searchID)
		debitTxn := models.Transaction{
			UserID:        search.UserID,
			SearchID:      &searchID,
			TxnType:       "debit",
			Amount:        deductedAmount,
			BalanceBefore: user.Balance,
			BalanceAfter:  user.Balance, // Balance already reflects the deduction (frozen - refunded = deducted)
			Description:   &debitDesc,
			Status:        "completed",
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		if err := tx.Create(&debitTxn).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to create debit transaction: %v", err)
		}
	}

	// Reset search's frozen_amount
	search.FrozenAmount = 0
	if err := tx.Save(search).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update search: %v", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}

// ProcessSearchFailure processes a failed or aborted search
// Refunds the full frozen_amount
func ProcessSearchFailure(search *models.Search, db *gorm.DB) error {
	if search.FrozenAmount <= 0 {
		// Nothing to process if no frozen amount
		return nil
	}

	// Get user
	var user models.User
	if err := db.Where("email = ?", search.UserID).First(&user).Error; err != nil {
		return fmt.Errorf("user not found: %v", err)
	}

	// Capture initial balance before processing
	initialBalance := user.Balance

	// Start transaction
	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Unfreeze: reduce user's frozen_amount by search's frozen_amount
	user.FrozenAmount -= search.FrozenAmount
	if user.FrozenAmount < 0 {
		user.FrozenAmount = 0 // Prevent negative frozen amount
	}

	// Refund: add full frozen_amount to user's balance
	user.Balance += search.FrozenAmount

	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update user balance: %v", err)
	}

	// Create transaction records
	searchID := search.ID

	// Unfreeze transaction (balance doesn't change, only frozen_amount changes)
	unfreezeDesc := fmt.Sprintf("Unfreeze amount for failed/aborted search #%d", searchID)
	unfreezeTxn := models.Transaction{
		UserID:        search.UserID,
		SearchID:      &searchID,
		TxnType:       "unfreeze",
		Amount:        search.FrozenAmount,
		BalanceBefore: initialBalance,
		BalanceAfter:  initialBalance, // Balance doesn't change on unfreeze
		Description:   &unfreezeDesc,
		Status:        "completed",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := tx.Create(&unfreezeTxn).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create unfreeze transaction: %v", err)
	}

	// Refund transaction
	refundDesc := fmt.Sprintf("Full refund for failed/aborted search #%d", searchID)
	refundTxn := models.Transaction{
		UserID:        search.UserID,
		SearchID:      &searchID,
		TxnType:       "refund",
		Amount:        search.FrozenAmount,
		BalanceBefore: initialBalance,
		BalanceAfter:  user.Balance,
		Description:   &refundDesc,
		Status:        "completed",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := tx.Create(&refundTxn).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to create refund transaction: %v", err)
	}

	// Reset search's frozen_amount
	search.FrozenAmount = 0
	if err := tx.Save(search).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update search: %v", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
}
