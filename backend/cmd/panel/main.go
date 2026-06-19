package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alex4096/server-dashboard/backend/internal/api"
	"github.com/alex4096/server-dashboard/backend/internal/app"
	"github.com/alex4096/server-dashboard/backend/internal/audit"
	"github.com/alex4096/server-dashboard/backend/internal/auth"
	"github.com/alex4096/server-dashboard/backend/internal/configmgr"
	"github.com/alex4096/server-dashboard/backend/internal/logs"
	"github.com/alex4096/server-dashboard/backend/internal/metrics"
	"github.com/alex4096/server-dashboard/backend/internal/server"
	"github.com/alex4096/server-dashboard/backend/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	cfg, err := app.LoadConfig()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		slog.Error("database error", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	now := time.Now().UTC()
	_, err = db.Exec(`INSERT INTO servers(id,name,root_dir,executable_path,working_dir,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,root_dir=excluded.root_dir,executable_path=excluded.executable_path,working_dir=excluded.working_dir,updated_at=excluded.updated_at`, cfg.ServerID, cfg.ServerName, cfg.RootDir, cfg.ExecutablePath, cfg.WorkingDir, now, now)
	if err != nil {
		slog.Error("server record error", "error", err)
		os.Exit(1)
	}
	aud := audit.New(db)
	authService := auth.New(db, time.Duration(cfg.SessionTTLHours)*time.Hour, cfg.AuthEnabled)
	if err := authService.Bootstrap(context.Background(), cfg.AdminUsername, cfg.AdminPassword); err != nil {
		slog.Error("authentication initialization error", "error", err)
		os.Exit(1)
	}
	logManager := logs.New(db, cfg.ServerID)
	defer logManager.Close()
	serverManager := server.New(db, aud, logManager, cfg.ServerID, cfg.ExecutablePath, cfg.WorkingDir)
	configManager := configmgr.New(cfg.RootDir, cfg.ServerID, db, aud)
	metricCollector := metrics.New(serverManager, cfg.RootDir)
	router := api.New(serverManager, logManager, configManager, metricCollector, aud, authService, cfg.ServerID)
	httpServer := &http.Server{Addr: cfg.Addr, Handler: router.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		slog.Info("panel listening", "address", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server failed", "error", err)
			stop()
		}
	}()
	_ = logManager.Cleanup(ctx, cfg.LogRetentionDays, cfg.LogMaxRows)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = logManager.Cleanup(context.Background(), cfg.LogRetentionDays, cfg.LogMaxRows)
			case <-ctx.Done():
				return
			}
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_ = serverManager.Shutdown(shutdownCtx)
	_ = httpServer.Shutdown(shutdownCtx)
}
