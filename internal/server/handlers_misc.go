package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/frontinsight/backend/internal/models"
)

// GetLocations godoc
// @Summary Get all locations
// @Description Retrieve a list of all available locations
// @Tags Reference Data
// @Produce json
// @Success 200 {object} map[string]interface{} "List of locations"
// @Router /locations [get]
func (s *Server) GetLocations(c echo.Context) error {
	var names []string
	rows, err := s.DB.Model(&models.Location{}).Order("name").Rows()
	if err != nil {
		return c.JSON(http.StatusOK, map[string]any{"locations": []string{}})
	}
	defer rows.Close()
	for rows.Next() {
		var l models.Location
		_ = s.DB.ScanRows(rows, &l)
		names = append(names, l.Name)
	}
	return c.JSON(http.StatusOK, map[string]any{"locations": names})
}

// GetSites godoc
// @Summary Get all sites
// @Description Retrieve a list of all available booking sites
// @Tags Reference Data
// @Produce json
// @Success 200 {object} map[string]interface{} "List of sites"
// @Router /sites [get]
func (s *Server) GetSites(c echo.Context) error {
	var names []string
	rows, err := s.DB.Model(&models.Site{}).Order("name").Rows()
	if err != nil {
		return c.JSON(http.StatusOK, map[string]any{"sites": []string{}})
	}
	defer rows.Close()
	for rows.Next() {
		var site models.Site
		_ = s.DB.ScanRows(rows, &site)
		names = append(names, site.Name)
	}
	return c.JSON(http.StatusOK, map[string]any{"sites": names})
}

// GetPOS godoc
// @Summary Get all POS systems
// @Description Retrieve a list of all available POS (Point of Sale) systems for a specific site
// @Tags Reference Data
// @Produce json
// @Param site_name query string true "Site name"
// @Success 200 {object} map[string]interface{} "List of POS systems"
// @Router /pos [get]
func (s *Server) GetPOS(c echo.Context) error {
	siteName := c.QueryParam("site_name")
	if siteName == "" {
		return c.JSON(http.StatusOK, map[string]any{"pos": []string{}})
	}

	var pos []string
	sql := `SELECT spm.pos_name FROM site_to_pos_mapping spm
	WHERE spm.site_name = ?
	ORDER BY spm.pos_name`
	rows, err := s.DB.Raw(sql, siteName).Rows()
	if err != nil {
		return c.JSON(http.StatusOK, map[string]any{"pos": []string{}})
	}
	defer rows.Close()
	for rows.Next() {
		var posName string
		_ = rows.Scan(&posName)
		pos = append(pos, posName)
	}
	return c.JSON(http.StatusOK, map[string]any{"pos": pos})
}

// ContactQuery godoc
// @Summary Submit contact query
// @Description Submit a customer contact query or support request
// @Tags Contact
// @Accept json
// @Produce json
// @Param request body object{name=string,email=string,phone=string,company=string,subject=string,query_type=string,message=string} true "Contact query data"
// @Success 200 {object} map[string]interface{} "Query submitted successfully"
// @Failure 400 {object} map[string]interface{} "Bad request"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Router /contact-query [post]
func (s *Server) ContactQuery(c echo.Context) error {
	var req struct {
		Name      string `json:"name" example:"John Doe" binding:"required"`
		Email     string `json:"email" example:"john@example.com" binding:"required"`
		Phone     string `json:"phone" example:"+1234567890"`
		Company   string `json:"company" example:"Acme Corp"`
		Subject   string `json:"subject" example:"Product Inquiry"`
		QueryType string `json:"query_type" example:"general"`
		Message   string `json:"message" example:"I need help with my booking" binding:"required"`
	}
	if err := c.Bind(&req); err != nil || req.Name == "" || req.Email == "" || req.Message == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Name, email, and message are required."})
	}

	// Validate query_type if provided
	validQueryTypes := map[string]bool{
		"general":   true,
		"support":   true,
		"sales":     true,
		"technical": true,
		"feedback":  true,
	}
	if req.QueryType != "" && !validQueryTypes[req.QueryType] {
		req.QueryType = "general" // Default to general if invalid
	}
	if req.QueryType == "" {
		req.QueryType = "general" // Default to general if not provided
	}

	q := models.CustomerQuery{
		Name:      req.Name,
		Email:     req.Email,
		Phone:     req.Phone,
		Company:   req.Company,
		Subject:   req.Subject,
		QueryType: req.QueryType,
		Message:   req.Message,
	}
	if err := s.DB.Create(&q).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]any{"success": true, "message": "Query submitted successfully."})
}

// GetWallet godoc
// @Summary Get user wallet information
// @Description Get user balance, frozen amount, and transaction history
// @Tags Wallet
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} map[string]interface{} "Wallet information"
// @Failure 401 {object} simpleResponse
// @Router /wallet [get]
func (s *Server) GetWallet(c echo.Context) error {
	user := c.Get("user").(*models.User)

	// Get transactions for the user
	var transactions []models.Transaction
	if err := s.DB.Where("user_id = ?", user.Email).
		Order("created_at DESC").
		Limit(100).
		Find(&transactions).Error; err != nil {
		transactions = []models.Transaction{}
	}

	// Format transactions for response
	formattedTransactions := make([]map[string]any, 0, len(transactions))
	for _, txn := range transactions {
		txnType := "debit"
		if txn.TxnType == "credit" || txn.TxnType == "refund" {
			txnType = "credit"
		}

		formattedTransactions = append(formattedTransactions, map[string]any{
			"id":          txn.ID,
			"type":        txnType,
			"amount":      txn.Amount,
			"description": txn.Description,
			"timestamp":   txn.CreatedAt,
			"status":      txn.Status,
		})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success":       true,
		"balance":       user.Balance,
		"frozen_amount": user.FrozenAmount,
		"transactions":  formattedTransactions,
	})
}

// AddMoneyToWallet godoc
// @Summary Add money to wallet (dummy implementation)
// @Description Add money to user wallet - dummy implementation that directly credits the account
// @Tags Wallet
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body object{amount=float64} true "Amount to add"
// @Success 200 {object} map[string]interface{} "Money added successfully"
// @Failure 400 {object} simpleResponse
// @Failure 401 {object} simpleResponse
// @Router /wallet/add-money [post]
func (s *Server) AddMoneyToWallet(c echo.Context) error {
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid request"})
	}

	if req.Amount <= 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Amount must be greater than zero"})
	}

	// Get user from authenticated context
	user := c.Get("user").(*models.User)

	// Start transaction
	tx := s.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Capture balance before update
	balanceBefore := user.Balance

	// Update user balance
	user.Balance += req.Amount
	if err := tx.Save(&user).Error; err != nil {
		tx.Rollback()
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to update balance: " + err.Error()})
	}

	// Create transaction record
	description := "Wallet top-up (dummy payment)"
	transaction := models.Transaction{
		UserID:        user.Email,
		TxnType:       "credit",
		Amount:        req.Amount,
		BalanceBefore: balanceBefore,
		BalanceAfter:  user.Balance,
		Description:   &description,
		Status:        "completed",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to create transaction record: " + err.Error()})
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": "Failed to commit transaction: " + err.Error()})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"message": fmt.Sprintf("Successfully added %s to wallet", formatCurrency(req.Amount)),
		"balance": user.Balance,
	})
}

// Helper function to format currency
func formatCurrency(amount float64) string {
	return fmt.Sprintf("$%.2f", amount)
}

func (s *Server) CreatePaymentOrder(c echo.Context) error {
	var req struct {
		Amount int `json:"amount"`
	}
	if err := c.Bind(&req); err != nil || req.Amount == 0 {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Amount is required."})
	}
	// Get user info from authenticated context
	user := c.Get("user").(*models.User)
	userIDStr := user.Email // Use email as UserID for foreign key constraint
	order := models.PaymentOrder{Amount: req.Amount, UserID: userIDStr, Status: "created"}
	if err := s.DB.Create(&order).Error; err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
	}
	paymentURL := fmt.Sprintf("https://payment-gateway.example.com/pay?order_id=%d", order.ID)
	return c.JSON(http.StatusOK, map[string]any{"success": true, "paymentUrl": paymentURL, "orderId": order.ID})
}

func (s *Server) DownloadSampleData(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Use pgx pool for farecache
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		s.Cfg.FCDBHost, s.Cfg.FCDBPort, s.Cfg.FCDBUser, s.Cfg.FCDBPass, s.Cfg.FCDBName)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
	}
	defer pool.Close()
	query := `WITH RankedEntries AS (
		SELECT raw_result, ROW_NUMBER() OVER(PARTITION BY search_key ORDER BY shop_date DESC) as rn
		FROM farecache
		WHERE search_key IN ('107_TRIVAGO','107_BOOKING','107_AGODA','107_XP')
	) SELECT raw_result FROM RankedEntries WHERE rn <= 50;`
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
	}
	defer rows.Close()
	columns := []string{"SITECODE", "COUNTRY", "LOCATION", "CHECKIN_DATE", "CHECKOUT_DATE", "HOTELNAME", "SUPPLIER", "RATENIGHTLY", "RATEFINAL", "CURRENCY", "ROOM_INFORMATION", "STARS", "MESSAGE"}

	// Get format parameter (default to csv)
	format := c.QueryParam("format")
	if format == "" {
		format = "csv"
	}

	// Collect all rows
	var allRows [][]string
	for rows.Next() {
		var raw sql.NullString
		_ = rows.Scan(&raw)
		var parts []string
		if raw.Valid {
			parts = stringsSplitPad(raw.String, '|', 13)
		} else {
			parts = make([]string, len(columns))
		}
		allRows = append(allRows, parts)
	}

	if format == "json" {
		// Convert to JSON format
		var jsonData []map[string]string
		for _, row := range allRows {
			rowMap := make(map[string]string)
			for i, col := range columns {
				if i < len(row) {
					rowMap[col] = row[i]
				} else {
					rowMap[col] = ""
				}
			}
			jsonData = append(jsonData, rowMap)
		}

		jsonBytes, err := json.MarshalIndent(jsonData, "", "  ")
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		}
		return c.Blob(http.StatusOK, "application/json", jsonBytes)
	}

	// Default to CSV format
	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)
	_ = w.Write(columns)
	for _, row := range allRows {
		_ = w.Write(row)
	}
	w.Flush()
	return c.Blob(http.StatusOK, "text/csv", buf.Bytes())
}

func (s *Server) DownloadFile(c echo.Context) error {
	filename := fmt.Sprintf("out_%s.csv", c.Param("job_name"))
	// Connect SFTP
	cfg := &ssh.ClientConfig{
		User:            s.Cfg.SFTPUser,
		Auth:            []ssh.AuthMethod{ssh.Password(s.Cfg.SFTPPass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", s.Cfg.SFTPHost, s.Cfg.SFTPPort)
	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("SFTP dial error: %v", err)})
	}
	defer conn.Close()
	sftpClient, err := sftp.NewClient(conn)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("SFTP client error: %v", err)})
	}
	defer sftpClient.Close()
	// Check file exists
	f, err := sftpClient.Open(filename)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": fmt.Sprintf("File %s not found on SFTP.", filename)})
	}
	defer f.Close()
	data := &bytes.Buffer{}
	if _, err := f.WriteTo(data); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("Read error: %v", err)})
	}
	return c.Blob(http.StatusOK, "text/csv", data.Bytes())
}

// DownloadFileByRunID godoc
// @Summary Download search output file by run ID
// @Description Download the output file for a search using the run ID from the QL2 system
// @Tags Files
// @Produce application/octet-stream
// @Security BearerAuth
// @Param run_id path string true "Run ID from QL2 system"
// @Param format query string false "File format: csv or json (default: csv)"
// @Success 200 {file} file "CSV or JSON file"
// @Failure 400 {object} simpleResponse
// @Failure 401 {object} simpleResponse
// @Failure 404 {object} simpleResponse
// @Failure 500 {object} simpleResponse
// @Router /download-by-run-id/{run_id} [get]
func (s *Server) DownloadFileByRunID(c echo.Context) error {
	runID := c.Param("run_id")
	if runID == "" {
		return c.JSON(http.StatusBadRequest, map[string]any{"success": false, "message": "Run ID is required"})
	}

	// Get format parameter (default to csv)
	format := c.QueryParam("format")
	if format == "" {
		format = "csv"
	}

	// Determine filename based on format
	var filename string
	if format == "json" {
		filename = fmt.Sprintf("out_%s.json", runID)
	} else {
		filename = fmt.Sprintf("out_%s.csv", runID)
	}

	// Connect SFTP
	cfg := &ssh.ClientConfig{
		User:            s.Cfg.SFTPUser,
		Auth:            []ssh.AuthMethod{ssh.Password(s.Cfg.SFTPPass)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	addr := fmt.Sprintf("%s:%d", s.Cfg.SFTPHost, s.Cfg.SFTPPort)
	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("SFTP dial error: %v", err)})
	}
	defer conn.Close()
	sftpClient, err := sftp.NewClient(conn)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("SFTP client error: %v", err)})
	}
	defer sftpClient.Close()

	// Check if JSON file exists first, otherwise fall back to CSV
	if format == "json" {
		f, err := sftpClient.Open(filename)
		if err != nil {
			// If JSON file doesn't exist, try to convert CSV to JSON
			csvFilename := fmt.Sprintf("out_%s.csv", runID)
			csvFile, csvErr := sftpClient.Open(csvFilename)
			if csvErr != nil {
				return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": fmt.Sprintf("File %s not found on SFTP.", csvFilename)})
			}
			defer csvFile.Close()

			// Read CSV and convert to JSON
			csvData := &bytes.Buffer{}
			if _, err := csvFile.WriteTo(csvData); err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("Read error: %v", err)})
			}

			// Parse CSV
			reader := csv.NewReader(csvData)
			records, err := reader.ReadAll()
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("CSV parse error: %v", err)})
			}

			if len(records) == 0 {
				return c.JSON(http.StatusOK, map[string]any{"success": true, "data": []interface{}{}})
			}

			// Convert to JSON
			headers := records[0]
			var jsonData []map[string]string
			for i := 1; i < len(records); i++ {
				rowMap := make(map[string]string)
				for j, header := range headers {
					if j < len(records[i]) {
						rowMap[header] = records[i][j]
					} else {
						rowMap[header] = ""
					}
				}
				jsonData = append(jsonData, rowMap)
			}

			jsonBytes, err := json.MarshalIndent(jsonData, "", "  ")
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("JSON marshal error: %v", err)})
			}
			return c.Blob(http.StatusOK, "application/json", jsonBytes)
		}
		defer f.Close()
		data := &bytes.Buffer{}
		if _, err := f.WriteTo(data); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("Read error: %v", err)})
		}
		return c.Blob(http.StatusOK, "application/json", data.Bytes())
	}

	// CSV format
	f, err := sftpClient.Open(filename)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]any{"success": false, "message": fmt.Sprintf("File %s not found on SFTP.", filename)})
	}
	defer f.Close()
	data := &bytes.Buffer{}
	if _, err := f.WriteTo(data); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]any{"success": false, "message": fmt.Sprintf("Read error: %v", err)})
	}
	return c.Blob(http.StatusOK, "text/csv", data.Bytes())
}

// helper: split and pad/truncate to n
func stringsSplitPad(s string, sep rune, n int) []string {
	parts := make([]string, 0, n)
	cur := ""
	for _, ch := range s {
		if ch == sep {
			parts = append(parts, cur)
			cur = ""
			continue
		}
		cur += string(ch)
	}
	parts = append(parts, cur)
	for len(parts) < n {
		parts = append(parts, "")
	}
	if len(parts) > n {
		parts = parts[:n]
	}
	return parts
}
