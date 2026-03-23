package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// Import all Kubernetes client auth plugins
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	"github.com/tesserix/reposhift/internal/api"
	"github.com/tesserix/reposhift/internal/cache"
	"github.com/tesserix/reposhift/internal/controller"
	"github.com/tesserix/reposhift/internal/services"
	"github.com/tesserix/reposhift/internal/websocket"
)

var (
	scheme    = runtime.NewScheme()
	setupLog  = ctrl.Log.WithName("setup")
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(migrationv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var httpAddr string
	var webhookAddr string
	var showVersion bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8082", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&httpAddr, "http-bind-address", ":8080", "The address the HTTP API server binds to.")
	flag.StringVar(&webhookAddr, "webhook-bind-address", ":9443", "The address the webhook server binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false, "If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.BoolVar(&showVersion, "version", false, "Show version information and exit")

	flag.Parse()

	// Show version information if requested
	if showVersion {
		fmt.Printf("ADO to Git Migration Operator\n")
		fmt.Printf("Version: %s\n", version)
		fmt.Printf("Build Date: %s\n", buildDate)
		fmt.Printf("Git Commit: %s\n", gitCommit)
		os.Exit(0)
	}

	// Initialize logger
	opts := zap.Options{
		Development: true, // Enable development mode for more verbose logging
	}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("Starting ADO to Git Migration Operator",
		"version", version,
		"buildDate", buildDate,
		"gitCommit", gitCommit)

	// Configure TLS
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		Port:    9443,
	})

	// Configure leader election
	leaderElectionID := "migration.ado-to-git-migration.io"
	leaderElectionNamespace := os.Getenv("POD_NAMESPACE")
	if leaderElectionNamespace == "" {
		leaderElectionNamespace = "default"
	}

	// Create manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:              webhookServer,
		HealthProbeBindAddress:     probeAddr,
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           leaderElectionID,
		LeaderElectionNamespace:    leaderElectionNamespace,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize services with proper cleanup routines
	azureDevOpsService := services.NewAzureDevOpsService()
	githubService := services.NewGitHubService()
	migrationService := services.NewMigrationService("/tmp/migrations") // Provide a work directory (or from env)
	pipelineService := services.NewPipelineConversionService()
	workItemService := services.NewWorkItemService()
	projectService := services.NewGitHubProjectService()

	// Initialize pipeline-to-workflow conversion services
	pipelineDiscoveryService := services.NewPipelineDiscoveryService(azureDevOpsService, mgr.GetClient())
	workflowConverter := services.NewWorkflowConverter()
	workflowsRepositoryManager := services.NewWorkflowsRepositoryManager(githubService)

	// Initialize migration cache if enabled
	var migrationCache *cache.MigrationCache
	if os.Getenv("MIGRATION_CACHE_ENABLED") == "true" {
		retentionHours := 48  // Default
		maxEntries := 1000    // Default
		cleanupInterval := 30 // Default (minutes)

		if val := os.Getenv("MIGRATION_CACHE_RETENTION_HOURS"); val != "" {
			if parsed, err := fmt.Sscanf(val, "%d", &retentionHours); err == nil && parsed == 1 {
				setupLog.Info("Using custom cache retention", "hours", retentionHours)
			}
		}
		if val := os.Getenv("MIGRATION_CACHE_MAX_ENTRIES"); val != "" {
			if parsed, err := fmt.Sscanf(val, "%d", &maxEntries); err == nil && parsed == 1 {
				setupLog.Info("Using custom cache max entries", "maxEntries", maxEntries)
			}
		}
		if val := os.Getenv("MIGRATION_CACHE_CLEANUP_INTERVAL_MINUTES"); val != "" {
			if parsed, err := fmt.Sscanf(val, "%d", &cleanupInterval); err == nil && parsed == 1 {
				setupLog.Info("Using custom cache cleanup interval", "minutes", cleanupInterval)
			}
		}

		migrationCache = cache.NewMigrationCache(retentionHours, maxEntries, cleanupInterval, setupLog)
		migrationCache.Start()
		setupLog.Info("Migration cache initialized and started",
			"retentionHours", retentionHours,
			"maxEntries", maxEntries,
			"cleanupIntervalMinutes", cleanupInterval)
	} else {
		setupLog.Info("Migration cache is disabled")
	}

	// Initialize and test PostgreSQL connection at startup
	if os.Getenv("POSTGRES_ENABLED") == "true" {
		setupLog.Info("PostgreSQL is enabled, testing connection...")
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dbCancel()

		dbService, err := services.NewDatabaseService(dbCtx)
		if err != nil {
			setupLog.Error(err, "Failed to connect to PostgreSQL database")
			setupLog.Info("Operator will continue without PostgreSQL tracking (duplicate prevention disabled)")
		} else {
			setupLog.Info("PostgreSQL connection successful - duplicate prevention enabled")
			// Close the test connection, services will create their own when needed
			dbService.Close()
		}
	} else {
		setupLog.Info("PostgreSQL is disabled - migrations will run without duplicate prevention")
	}

	// Set up periodic cleanup tasks
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			// Clean up caches
			azureDevOpsService.CleanupConnectionCache()
			azureDevOpsService.CleanupTokenCache()
			githubService.CleanupClientCache()

			// Log migration cache stats if enabled
			if migrationCache != nil {
				stats := migrationCache.Stats()
				setupLog.V(1).Info("Migration cache stats", "stats", stats)
			}
		}
	}()

	// Initialize WebSocket manager
	wsManager := websocket.NewManager()
	wsManager.Start()

	// Setup controllers
	if err = (&controller.AdoToGitMigrationReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		MigrationService: migrationService,
		GitHubService:    githubService,
		WebSocketManager: wsManager,
		Recorder:         mgr.GetEventRecorderFor("adotogitmigration-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AdoToGitMigration")
		os.Exit(1)
	}

	if err = (&controller.AdoDiscoveryReconciler{
		Client:             mgr.GetClient(),
		Scheme:             mgr.GetScheme(),
		AzureDevOpsService: azureDevOpsService,
		WebSocketManager:   wsManager,
		Recorder:           mgr.GetEventRecorderFor("adodiscovery-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AdoDiscovery")
		os.Exit(1)
	}

	if err = (&controller.PipelineToWorkflowReconciler{
		Client:                     mgr.GetClient(),
		Scheme:                     mgr.GetScheme(),
		PipelineService:            pipelineService,
		PipelineDiscoveryService:   pipelineDiscoveryService,
		WorkflowConverter:          workflowConverter,
		WorkflowsRepositoryManager: workflowsRepositoryManager,
		ADOService:                 azureDevOpsService,
		GitHubService:              githubService,
		WebSocketManager:           wsManager,
		Recorder:                   mgr.GetEventRecorderFor("pipelinetoworkflow-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PipelineToWorkflow")
		os.Exit(1)
	}

	if err = (&controller.WorkItemMigrationReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		WorkItemService: workItemService,
		ProjectService:  projectService,
		Recorder:        mgr.GetEventRecorderFor("workitemmigration-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WorkItemMigration")
		os.Exit(1)
	}

	if err = (&controller.GitHubProjectReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		ProjectService: projectService,
		Recorder:       mgr.GetEventRecorderFor("githubproject-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "GitHubProject")
		os.Exit(1)
	}

	if err = (&controller.MonoRepoMigrationReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		MigrationService: migrationService,
		GitHubService:    githubService,
		WebSocketManager: wsManager,
		Recorder:         mgr.GetEventRecorderFor("monorepomigration-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MonoRepoMigration")
		os.Exit(1)
	}

	// Setup health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Start HTTP API server
	apiServer := api.NewServer(mgr.GetClient(), azureDevOpsService, githubService, migrationService, pipelineService, wsManager)

	// Configure server with timeouts
	serverConfig := &api.ServerConfig{
		ShutdownTimeout: 30 * time.Second,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		MaxHeaderBytes:  1 << 20, // 1MB
	}

	// Start API server in a goroutine
	go func() {
		setupLog.Info("starting HTTP API server", "addr", httpAddr)
		if err := apiServer.Start(httpAddr, serverConfig); err != nil && err != http.ErrServerClosed {
			setupLog.Error(err, "failed to start HTTP API server")
			os.Exit(1)
		}
	}()

	// Set up graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start manager
	setupLog.Info("starting manager")
	go func() {
		if err := mgr.Start(ctx); err != nil {
			setupLog.Error(err, "problem running manager")
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	// Graceful shutdown
	setupLog.Info("shutting down")

	// Create a new context for shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown API server
	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		setupLog.Error(err, "failed to shutdown HTTP server gracefully")
	}

	// Wait for manager to shut down
	time.Sleep(2 * time.Second)
	setupLog.Info("shutdown complete")
}
