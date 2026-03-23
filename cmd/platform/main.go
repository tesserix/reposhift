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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tesserix/reposhift/internal/platform"
	"github.com/tesserix/reposhift/internal/platform/auth"
	"github.com/tesserix/reposhift/internal/platform/config"
	"github.com/tesserix/reposhift/internal/platform/migration"
	"github.com/tesserix/reposhift/internal/platform/secrets"
	"github.com/tesserix/reposhift/internal/platform/tenant"
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
	slog.Info("configuration loaded", "mode", cfg.Mode, "port", cfg.Port)

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
	tenantStore := tenant.NewTenantStore(pool)
	migrationStore := migration.NewMigrationStore(pool)

	// Create secrets provider based on deployment mode.
	var secretsProvider secrets.SecretsProvider
	if cfg.IsSaaS() {
		dbProvider, err := secrets.NewDBProvider(pool, cfg.EncryptionKey)
		if err != nil {
			return fmt.Errorf("create DB secrets provider: %w", err)
		}
		secretsProvider = dbProvider
		slog.Info("secrets provider initialized", "type", "db")
	} else {
		k8sProvider, err := secrets.NewK8sProvider(cfg.K8sNamespace)
		if err != nil {
			slog.Warn("K8s secrets provider unavailable, falling back to DB provider", "error", err)
			// Fall back to DB provider if K8s is not available (e.g., local dev).
			if cfg.EncryptionKey != "" {
				dbProvider, dbErr := secrets.NewDBProvider(pool, cfg.EncryptionKey)
				if dbErr != nil {
					return fmt.Errorf("create fallback DB secrets provider: %w", dbErr)
				}
				secretsProvider = dbProvider
			} else {
				return fmt.Errorf("self-hosted mode requires either K8s cluster access or ENCRYPTION_KEY for DB fallback: %w", err)
			}
		} else {
			secretsProvider = k8sProvider
			slog.Info("secrets provider initialized", "type", "k8s", "namespace", cfg.K8sNamespace)
		}
	}

	// Create GitHub OAuth client (if configured).
	var githubOAuth *auth.GitHubOAuth
	if cfg.GitHub.ClientID != "" && cfg.GitHub.ClientSecret != "" {
		githubOAuth = auth.NewGitHubOAuth(cfg.GitHub)
		slog.Info("GitHub OAuth configured")
	}

	// Create the migration orchestrator. Pass nil for K8s client; the
	// orchestrator handles nil gracefully for out-of-cluster operation.
	orchestrator := migration.NewOrchestrator(migrationStore, secretsProvider, nil, cfg.K8sNamespace)

	// In self-hosted mode, ensure a default tenant and admin user exist.
	var defaultTenantID, defaultAdminUserID string
	if cfg.IsSelfHosted() {
		var err error
		defaultTenantID, defaultAdminUserID, err = ensureDefaultTenant(ctx, tenantStore, cfg)
		if err != nil {
			return fmt.Errorf("ensure default tenant: %w", err)
		}
	}

	// Build the platform server and router.
	srv := platform.NewPlatformServer(cfg, tenantStore, migrationStore, secretsProvider, orchestrator, githubOAuth, defaultTenantID, defaultAdminUserID)
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

// ensureDefaultTenant creates the "default" tenant and an "admin" user if they
// do not already exist. This is used in self-hosted mode so that the instance
// is immediately usable without requiring GitHub OAuth sign-up.
// Returns the tenant ID and admin user ID for use by the admin token middleware.
func ensureDefaultTenant(ctx context.Context, store *tenant.TenantStore, cfg *config.PlatformConfig) (string, string, error) {
	const defaultTenantSlug = "default"

	// Check if the default tenant already exists.
	if existing, err := store.GetTenantBySlug(ctx, defaultTenantSlug); err == nil {
		slog.Info("default tenant already exists", "tenantId", existing.ID)
		// Look up the admin user to return its ID.
		members, mErr := store.GetTenantMembers(ctx, existing.ID)
		if mErr != nil {
			return "", "", fmt.Errorf("get default tenant members: %w", mErr)
		}
		for _, m := range members {
			if m.Role == tenant.RoleOwner {
				return existing.ID, m.UserID, nil
			}
		}
		return existing.ID, "", nil
	}

	tenantID := uuid.New().String()
	t := &tenant.Tenant{
		ID:           tenantID,
		Name:         "Default Workspace",
		Slug:         defaultTenantSlug,
		Plan:         "selfhosted",
		Mode:         config.ModeSelfHosted,
		K8sNamespace: cfg.K8sNamespace,
		Settings:     map[string]any{},
	}
	if err := store.CreateTenant(ctx, t); err != nil {
		return "", "", fmt.Errorf("create default tenant: %w", err)
	}
	slog.Info("default tenant created", "tenantId", tenantID)

	// Create the admin user (no GitHub identity).
	adminUserID := uuid.New().String()
	adminUser, err := store.UpsertUser(ctx, &tenant.User{
		ID:          adminUserID,
		GitHubID:    nil,
		GitHubLogin: "admin",
		GitHubEmail: "admin@localhost",
		Name:        "Admin",
	})
	if err != nil {
		return "", "", fmt.Errorf("create admin user: %w", err)
	}
	slog.Info("admin user created", "userId", adminUser.ID)

	// Add the admin user as owner of the default tenant.
	if err := store.AddMember(ctx, tenantID, adminUser.ID, tenant.RoleOwner); err != nil {
		return "", "", fmt.Errorf("add admin to default tenant: %w", err)
	}
	slog.Info("admin user added to default tenant as owner")

	return tenantID, adminUser.ID, nil
}
