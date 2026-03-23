package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tesserix/reposhift/internal/platform"
	"github.com/tesserix/reposhift/internal/platform/config"
	"github.com/tesserix/reposhift/internal/platform/migration"
	"github.com/tesserix/reposhift/internal/platform/secrets"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load configuration.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	slog.Info("configuration loaded", "port", cfg.Port)

	// Connect to PostgreSQL.
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("parse database URL: %w", err)
	}
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("create connection pool: %w", err)
	}
	defer pool.Close()

	// Verify database connectivity.
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	slog.Info("database connection established")

	// Create stores.
	migrationStore := migration.NewMigrationStore(pool)

	// Create K8s secrets provider with graceful fallback.
	var secretsProvider secrets.SecretsProvider
	k8sProvider, err := secrets.NewK8sProvider(cfg.K8sNamespace)
	if err != nil {
		slog.Warn("K8s secrets provider unavailable (not running in cluster), secrets API will be disabled", "error", err)
	} else {
		secretsProvider = k8sProvider
		slog.Info("secrets provider initialized", "type", "k8s", "namespace", cfg.K8sNamespace)
	}

	// Create the migration orchestrator. Pass nil for K8s client; the
	// orchestrator handles nil gracefully for out-of-cluster operation.
	orchestrator := migration.NewOrchestrator(migrationStore, secretsProvider, nil, cfg.K8sNamespace)

	// Build the platform server and router.
	srv := platform.NewPlatformServer(cfg, migrationStore, secretsProvider, orchestrator)
	router := srv.SetupRouter()

	// Start HTTP server.
	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("platform server starting", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	select {
	case sig := <-shutdown:
		slog.Info("shutdown signal received", "signal", sig.String())
	case err := <-errCh:
		return err
	}

	// Give in-flight requests up to 15 seconds to complete.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}

	slog.Info("platform server stopped gracefully")
	return nil
}
