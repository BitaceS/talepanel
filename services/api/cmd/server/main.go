package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyraxo/talepanel/api/internal/config"
	"github.com/tyraxo/talepanel/api/internal/db"
	"github.com/tyraxo/talepanel/api/internal/router"
)

func main() {
	// ── Configuration ──────────────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: configuration error: %v\n", err)
		os.Exit(1)
	}

	// Force Gin into release mode in production builds.  This must happen
	// BEFORE any gin function is called (even gin.New) — otherwise Gin emits
	// its debug banner and the mode switch arrives too late.
	if !cfg.IsDevelopment() {
		gin.SetMode(gin.ReleaseMode)
	}

	// ── Logger ─────────────────────────────────────────────────────────────────
	log, err := buildLogger(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: could not initialise logger: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = log.Sync() }()

	log.Info("TalePanel API starting",
		zap.String("env", cfg.Env),
		zap.Int("port", cfg.ServerPort),
	)

	// ── Database ───────────────────────────────────────────────────────────────
	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startCancel()

	pool, err := db.NewPool(startCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("could not connect to PostgreSQL", zap.Error(err))
	}
	defer pool.Close()
	log.Info("PostgreSQL connection pool established")

	// ── Redis ──────────────────────────────────────────────────────────────────
	rdb, err := db.NewClient(cfg.RedisURL)
	if err != nil {
		log.Fatal("could not connect to Redis", zap.Error(err))
	}
	defer func() { _ = rdb.Close() }()
	log.Info("Redis connection established")

	// ── Migrations check ───────────────────────────────────────────────────────
	// Verifies the database schema has been applied before accepting traffic.
	// Run a proper migration tool (golang-migrate, goose, etc.) as a separate
	// step in your deployment pipeline before starting this process.
	if err := checkSchema(startCtx, pool, log); err != nil {
		log.Fatal("schema check failed — run migrations before starting", zap.Error(err))
	}

	// ── Router ─────────────────────────────────────────────────────────────────
	r := router.SetupRouter(cfg, pool, rdb, log)

	// ── HTTP Server ────────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.ServerPort),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine so the main goroutine can handle signals.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("HTTP server listening", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// ── Graceful Shutdown ──────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-serverErr:
		log.Error("server error", zap.Error(err))
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	log.Info("shutting down HTTP server gracefully...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("forced shutdown", zap.Error(err))
	} else {
		log.Info("server stopped cleanly")
	}
}

// buildLogger constructs a zap.Logger appropriate for the current environment.
func buildLogger(cfg *config.Config) (*zap.Logger, error) {
	var level zapcore.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = zapcore.InfoLevel
	}

	var zapCfg zap.Config
	if cfg.IsDevelopment() {
		zapCfg = zap.NewDevelopmentConfig()
		zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		zapCfg = zap.NewProductionConfig()
	}
	zapCfg.Level = zap.NewAtomicLevelAt(level)

	return zapCfg.Build()
}

// checkSchema verifies that critical tables exist as a lightweight startup
// sanity check.  It is NOT a replacement for a proper migration tool.
func checkSchema(ctx context.Context, pool *pgxpool.Pool, log *zap.Logger) error {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name   = 'users'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("schema check query: %w", err)
	}
	if !exists {
		return fmt.Errorf("table 'users' not found — database migrations have not been applied")
	}

	log.Info("schema check passed")
	return nil
}
