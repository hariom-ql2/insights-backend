package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// Health godoc
// @Summary Health check
// @Description Check the health status of the API and its dependencies
// @Tags System
// @Produce json
// @Success 200 {object} map[string]interface{} "Health status"
// @Router /health [get]
func (s *Server) Health(c echo.Context) error {
	status := map[string]any{
		"success": true,
		"status":  "ok",
		"checks":  map[string]any{},
	}
	checks := status["checks"].(map[string]any)
	// Main DB
	if sqlDB, err := s.DB.DB(); err == nil {
		if err := sqlDB.Ping(); err != nil {
			checks["database"] = map[string]any{"ok": false, "error": err.Error()}
			status["status"] = "degraded"
		} else {
			checks["database"] = map[string]any{"ok": true}
		}
	} else {
		checks["database"] = map[string]any{"ok": false, "error": "db handle unavailable"}
		status["status"] = "degraded"
	}
	// Run-table (best-effort)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runDSN := ""
	if s.Cfg.RunDBHost != "" {
		runDSN = "host=" + s.Cfg.RunDBHost + " port=" + s.Cfg.RunDBPort + " user=" + s.Cfg.RunDBUser + " password=" + s.Cfg.RunDBPass + " dbname=" + s.Cfg.RunDBName + " sslmode=disable"
		if pool, err := pgxpool.New(ctx, runDSN); err == nil {
			if err := pool.Ping(ctx); err != nil {
				checks["run_table"] = map[string]any{"ok": false, "error": err.Error()}
				status["status"] = "degraded"
			} else {
				checks["run_table"] = map[string]any{"ok": true}
			}
			pool.Close()
		} else {
			checks["run_table"] = map[string]any{"ok": false}
		}
	}
	// Farecache (best-effort)
	fcDSN := ""
	if s.Cfg.FCDBHost != "" {
		fcDSN = "host=" + s.Cfg.FCDBHost + " port=" + s.Cfg.FCDBPort + " user=" + s.Cfg.FCDBUser + " password=" + s.Cfg.FCDBPass + " dbname=" + s.Cfg.FCDBName + " sslmode=disable"
		if pool, err := pgxpool.New(ctx, fcDSN); err == nil {
			if err := pool.Ping(ctx); err != nil {
				checks["farecache"] = map[string]any{"ok": false, "error": err.Error()}
				status["status"] = "degraded"
			} else {
				checks["farecache"] = map[string]any{"ok": true}
			}
			pool.Close()
		} else {
			checks["farecache"] = map[string]any{"ok": false}
		}
	}
	return c.JSON(http.StatusOK, status)
}
