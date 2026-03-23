package api

// CORS configuration updated
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tesserix/reposhift/internal/services"
	wsmanager "github.com/tesserix/reposhift/internal/websocket"
	"github.com/civica/global-platform-hub/packages/go-common/middleware"
)

// Prometheus metrics
var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)

	activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections",
			Help: "Number of active connections",
		},
	)

	migrationJobsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "migration_jobs_total",
			Help: "Total number of migration jobs",
		},
		[]string{"type", "status"},
	)

	migrationResourcesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "migration_resources_total",
			Help: "Total number of migration resources",
		},
		[]string{"type", "status"},
	)
)

func init() {
	// Register metrics with Prometheus
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpRequestDuration)
	prometheus.MustRegister(activeConnections)
	prometheus.MustRegister(migrationJobsTotal)
	prometheus.MustRegister(migrationResourcesTotal)
}

// Server represents the HTTP API server
type Server struct {
	client             client.Client
	azureDevOpsService *services.AzureDevOpsService
	githubService      *services.GitHubService
	migrationService   *services.MigrationService
	pipelineService    *services.PipelineConversionService
	websocketManager   *wsmanager.Manager
	sseHub             *SSEHub // Server-Sent Events hub for real-time updates
	middleware         *Middleware
	upgrader           websocket.Upgrader
	server             *http.Server
	shutdownTimeout    time.Duration
	startTime          time.Time
	requestCount       int64
	requestCountMutex  sync.RWMutex
}

// ServerConfig represents server configuration
type ServerConfig struct {
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxHeaderBytes  int
}

// NewServer creates a new API server instance
func NewServer(
	client client.Client,
	azureDevOpsService *services.AzureDevOpsService,
	githubService *services.GitHubService,
	migrationService *services.MigrationService,
	pipelineService *services.PipelineConversionService,
	websocketManager *wsmanager.Manager,
) *Server {
	return &Server{
		client:             client,
		azureDevOpsService: azureDevOpsService,
		githubService:      githubService,
		migrationService:   migrationService,
		pipelineService:    pipelineService,
		websocketManager:   websocketManager,
		sseHub:             NewSSEHub(), // Initialize SSE hub
		middleware:         NewMiddleware(),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow connections from any origin in development
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		shutdownTimeout: 30 * time.Second,
		startTime:       time.Now(),
	}
}

// ginAdapter wraps a standard http.Handler to work with Gin
// It extracts Gin path parameters and makes them available via request context
func ginAdapter(handler func(http.ResponseWriter, *http.Request)) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract Gin path parameters and add to request context
		ctx := c.Request.Context()
		for _, param := range c.Params {
			ctx = context.WithValue(ctx, "gin_param_"+param.Key, param.Value)
		}
		// Create new request with updated context
		req := c.Request.WithContext(ctx)
		handler(c.Writer, req)
	}
}

// getPathParam extracts path parameter from Gin context stored in request
// This replaces mux.Vars(r)["key"]
func getPathParam(r *http.Request, key string) string {
	if val := r.Context().Value("gin_param_" + key); val != nil {
		return val.(string)
	}
	return ""
}

// ginMetricsMiddleware wraps the prometheus metrics middleware for Gin
func (s *Server) ginMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Increment active connections
		activeConnections.Inc()
		defer activeConnections.Dec()

		// Increment request count
		s.requestCountMutex.Lock()
		s.requestCount++
		s.requestCountMutex.Unlock()

		// Track request duration
		start := time.Now()

		// Process request
		c.Next()

		// Record metrics
		duration := time.Since(start)
		path := getMetricPath(c.Request.URL.Path)

		httpRequestsTotal.WithLabelValues(c.Request.Method, path, strconv.Itoa(c.Writer.Status())).Inc()
		httpRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration.Seconds())

		// Record migration metrics if applicable
		if strings.HasPrefix(c.Request.URL.Path, "/api/v1/migrations") && c.Request.Method == "POST" {
			migrationJobsTotal.WithLabelValues("all", "created").Inc()
		}
	}
}

// SetupRoutes configures all HTTP routes with comprehensive middleware
func (s *Server) SetupRoutes() http.Handler {
	r := gin.New()

	// Configure CORS using go-common middleware
	// Using specific origins (like applications-hub-service) to allow credentials
	corsConfig := middleware.CORSConfig{
		AllowedOrigins: []string{
			"https://hub.civica.tech",
			"https://dev.hub.civica.tech",
			"https://ado-git-migration-api.hub.civica.tech",
			"https://ado-git-migration-dev.hub.civica.tech",
			"http://localhost:3000",
			"http://localhost:3001",
			"http://localhost:8080",
			"http://localhost:3005",
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowedHeaders: []string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			"X-Requested-With",
			"X-API-Key",
			"X-ADO-PAT",
			"X-GitHub-Token",
		},
		ExposedHeaders: []string{
			"X-Total-Count",
			"X-Rate-Limit-Remaining",
			"X-Rate-Limit-Reset",
			"Retry-After",
			"X-Page",
			"X-Limit",
			"X-Total",
			"X-Total-Pages",
			"X-Has-Next",
			"X-Has-Previous",
		},
		AllowCredentials: true,
		MaxAge:           43200, // 12 hours (matches applications-hub)
	}

	// Apply global middleware (CORS must be first)
	r.Use(middleware.CORS(corsConfig))
	r.Use(gin.Recovery())
	r.Use(s.ginMetricsMiddleware())

	// Handle all OPTIONS requests globally (for CORS preflight)
	r.OPTIONS("/*path", func(c *gin.Context) {
		// CORS middleware already set headers, just return 204
		c.Status(http.StatusNoContent)
	})

	// TODO: Convert custom middleware to Gin-compatible format
	// For now, relying on Gin's built-in middleware and CORS from go-common

	// API v1 group
	api := r.Group("/api/v1")
	// TODO: Add authentication and validation middleware for Gin
	{
		// Discovery endpoints
		discovery := api.Group("/discovery")
		{
			discovery.GET("/organizations", ginAdapter(s.handleDiscoverOrganizations))
			discovery.GET("/projects", ginAdapter(s.handleDiscoverProjects))
			discovery.GET("/repositories", ginAdapter(s.handleDiscoverRepositories))
			discovery.GET("/workitems", ginAdapter(s.handleDiscoverWorkItems))
			discovery.GET("/pipelines", ginAdapter(s.handleDiscoverPipelines))
			discovery.GET("/builds", ginAdapter(s.handleDiscoverBuilds))
			discovery.GET("/releases", ginAdapter(s.handleDiscoverReleases))
			discovery.GET("/teams", ginAdapter(s.handleDiscoverTeams))
			discovery.GET("/users", ginAdapter(s.handleDiscoverUsers))

			// Discovery resource management
			discovery.GET("", ginAdapter(s.handleListDiscoveries))
			discovery.POST("", ginAdapter(s.handleCreateDiscovery))
			discovery.GET("/:id", ginAdapter(s.handleGetDiscovery))
			discovery.PUT("/:id", ginAdapter(s.handleUpdateDiscovery))
			discovery.DELETE("/:id", ginAdapter(s.handleDeleteDiscovery))
			discovery.GET("/:id/status", ginAdapter(s.handleGetDiscoveryStatus))
			discovery.GET("/:id/results", ginAdapter(s.handleGetDiscoveryResults))
		}

		// ADO-specific endpoints for MFE
		ado := api.Group("/ado")
		{
			ado.POST("/repositories", ginAdapter(s.handleFetchADORepositories))
			ado.GET("/secrets", ginAdapter(s.handleListADOSecrets))
			ado.GET("/secrets/check-by-label", ginAdapter(s.handleCheckSecretByLabel))
			ado.POST("/secrets/create", ginAdapter(s.handleCreateADOSecret))
			ado.POST("/workitems/metadata", ginAdapter(s.handleGetWorkItemMetadata))
		}

		// Migration endpoints
		migrations := api.Group("/migrations")
		{
			migrations.GET("", ginAdapter(s.handleListMigrations))
			migrations.POST("", ginAdapter(s.handleCreateMigration))
			migrations.GET("/:id", ginAdapter(s.handleGetMigration))
			migrations.PUT("/:id", ginAdapter(s.handleUpdateMigration))
			migrations.DELETE("/:id", ginAdapter(s.handleDeleteMigration))
			migrations.GET("/:id/status", ginAdapter(s.handleGetMigrationStatus))
			migrations.GET("/:id/progress", ginAdapter(s.handleGetMigrationProgress))
			migrations.GET("/:id/logs", ginAdapter(s.handleGetMigrationLogs))
			migrations.POST("/:id/pause", ginAdapter(s.handlePauseMigration))
			migrations.POST("/:id/resume", ginAdapter(s.handleResumeMigration))
			migrations.POST("/:id/cancel", ginAdapter(s.handleCancelMigration))
			migrations.POST("/:id/retry", ginAdapter(s.handleRetryMigration))
			migrations.POST("/:id/validate", ginAdapter(s.handleValidateMigration))
		}

		// MonoRepo migration endpoints
		monorepo := api.Group("/monorepo-migrations")
		{
			monorepo.GET("", ginAdapter(s.handleListMonoRepoMigrations))
			monorepo.POST("", ginAdapter(s.handleCreateMonoRepoMigration))
			monorepo.GET("/:id", ginAdapter(s.handleGetMonoRepoMigration))
			monorepo.PUT("/:id", ginAdapter(s.handleUpdateMonoRepoMigration))
			monorepo.DELETE("/:id", ginAdapter(s.handleDeleteMonoRepoMigration))
			monorepo.GET("/:id/status", ginAdapter(s.handleGetMonoRepoMigrationStatus))
			monorepo.GET("/:id/progress", ginAdapter(s.handleGetMonoRepoMigrationProgress))
			monorepo.GET("/:id/logs", ginAdapter(s.handleGetMonoRepoMigrationLogs))
			monorepo.POST("/:id/pause", ginAdapter(s.handlePauseMonoRepoMigration))
			monorepo.POST("/:id/resume", ginAdapter(s.handleResumeMonoRepoMigration))
			monorepo.POST("/:id/cancel", ginAdapter(s.handleCancelMonoRepoMigration))
			monorepo.POST("/:id/retry", ginAdapter(s.handleRetryMonoRepoMigration))
		}

		// Pipeline conversion endpoints
		pipelines := api.Group("/pipelines")
		{
			pipelines.GET("", ginAdapter(s.handleListPipelineConversions))
			pipelines.POST("", ginAdapter(s.handleCreatePipelineConversion))
			pipelines.GET("/:id", ginAdapter(s.handleGetPipelineConversion))
			pipelines.PUT("/:id", ginAdapter(s.handleUpdatePipelineConversion))
			pipelines.DELETE("/:id", ginAdapter(s.handleDeletePipelineConversion))
			pipelines.GET("/:id/status", ginAdapter(s.handleGetPipelineConversionStatus))
			pipelines.GET("/:id/preview", ginAdapter(s.handlePreviewPipelineConversion))
			pipelines.POST("/:id/validate", ginAdapter(s.handleValidatePipelineConversion))
			pipelines.GET("/:id/download", ginAdapter(s.handleDownloadConvertedWorkflows))

			// Pipeline analysis endpoints
			pipelines.GET("/analyze", ginAdapter(s.handleAnalyzePipeline))
			pipelines.GET("/templates", ginAdapter(s.handleGetConversionTemplates))
			pipelines.GET("/templates/:type", ginAdapter(s.handleGetConversionTemplates))
			pipelines.GET("/mappings", ginAdapter(s.handleGetTaskMappings))
			pipelines.GET("/mappings/:task_type", ginAdapter(s.handleGetTaskMappings))
		}

		// Validation endpoints
		validation := api.Group("/validation")
		{
			validation.POST("/credentials/ado", ginAdapter(s.handleValidateAdoCredentials))
			validation.POST("/credentials/github", ginAdapter(s.handleValidateGitHubCredentials))
			validation.POST("/permissions/ado", ginAdapter(s.handleValidateAdoPermissions))
			validation.POST("/permissions/github", ginAdapter(s.handleValidateGitHubPermissions))
			validation.POST("/migration", ginAdapter(s.handleValidateMigrationConfig))
			validation.POST("/pipeline", ginAdapter(s.handleValidatePipelineConfig))
			validation.POST("/repository", ginAdapter(s.handleValidateGitHubRepository))
		}

		// Statistics and reporting endpoints
		stats := api.Group("/statistics")
		{
			stats.GET("/overview", ginAdapter(s.handleGetOverviewStatistics))
			stats.GET("/migrations", ginAdapter(s.handleGetMigrationStatistics))
			stats.GET("/pipelines", ginAdapter(s.handleGetPipelineStatistics))
			stats.GET("/usage", ginAdapter(s.handleGetUsageStatistics))
			stats.GET("/performance", ginAdapter(s.handleGetPerformanceMetrics))
		}

		// Utility endpoints
		utils := api.Group("/utils")
		{
			utils.GET("/health", ginAdapter(s.handleHealthCheck))
			utils.GET("/ready", ginAdapter(s.handleReadinessCheck))
			utils.GET("/version", ginAdapter(s.handleGetVersion))
			utils.GET("/config", ginAdapter(s.handleGetConfig))
			utils.GET("/templates", ginAdapter(s.handleGetTemplates))
			utils.GET("/templates/:type", ginAdapter(s.handleGetTemplate))
		}

		// Server-Sent Events (SSE) endpoint for real-time updates
		api.GET("/events", ginAdapter(s.handleSSE))
	}

	// Metrics endpoint (no auth required)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// WebSocket endpoint (no middleware for WebSocket upgrades - use raw http handler)
	r.GET("/ws/migrations", ginAdapter(s.handleWebSocket))

	// Swagger UI setup
	s.setupSwaggerUIGin(r)

	// Serve OpenAPI documentation
	r.Static("/docs", "./docs/")

	// 404 handler
	r.NoRoute(ginAdapter(s.handleNotFound))

	// 405 handler is handled by Gin automatically

	return r
}

// Start starts the HTTP server
func (s *Server) Start(addr string, config *ServerConfig) error {
	if config == nil {
		config = &ServerConfig{
			ShutdownTimeout: 30 * time.Second,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     120 * time.Second,
			MaxHeaderBytes:  1 << 20, // 1MB
		}
	}

	s.shutdownTimeout = config.ShutdownTimeout

	// Create server
	s.server = &http.Server{
		Addr:           addr,
		Handler:        s.SetupRoutes(),
		ReadTimeout:    config.ReadTimeout,
		WriteTimeout:   config.WriteTimeout,
		IdleTimeout:    config.IdleTimeout,
		MaxHeaderBytes: config.MaxHeaderBytes,
	}

	// Channel to listen for errors coming from the listener
	serverErrors := make(chan error, 1)

	// Start the server
	go func() {
		log.Log.Info("Starting HTTP server", "address", addr)
		serverErrors <- s.server.ListenAndServe()
	}()

	// Channel to listen for interrupt signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block until an error or interrupt occurs
	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)

	case sig := <-shutdown:
		log.Log.Info("Shutdown signal received", "signal", sig)

		// Give outstanding requests a deadline for completion
		ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()

		// Gracefully shutdown the server
		if err := s.server.Shutdown(ctx); err != nil {
			// Error from closing listeners, or context timeout
			s.server.Close()
			return fmt.Errorf("could not gracefully shutdown the server: %w", err)
		}
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// Helper functions
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Log.Error(err, "Failed to encode JSON response")
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	errorResponse := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"status":    status,
	}

	s.writeJSON(w, status, errorResponse)
}

func (s *Server) getQueryParam(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

func (s *Server) getQueryParamInt(r *http.Request, key string, defaultValue int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return intValue
}

func (s *Server) getQueryParamBool(r *http.Request, key string, defaultValue bool) bool {
	value := r.URL.Query().Get(key)
	if value == "" {
		return defaultValue
	}

	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}

	return boolValue
}

// Additional helper methods for enhanced functionality

func (s *Server) validateRequestSize(r *http.Request, maxSize int64) error {
	if r.ContentLength > maxSize {
		return fmt.Errorf("request body too large: %d bytes (max: %d)", r.ContentLength, maxSize)
	}
	return nil
}

func (s *Server) extractPaginationParams(r *http.Request) (int, int) {
	page := s.getQueryParamInt(r, "page", 1)
	if page < 1 {
		page = 1
	}

	limit := s.getQueryParamInt(r, "limit", 50)
	if limit < 1 {
		limit = 50
	} else if limit > 1000 {
		limit = 1000
	}

	return page, limit
}

func (s *Server) addPaginationHeaders(w http.ResponseWriter, page, limit, total int) {
	w.Header().Set("X-Page", strconv.Itoa(page))
	w.Header().Set("X-Limit", strconv.Itoa(limit))
	w.Header().Set("X-Total", strconv.Itoa(total))

	totalPages := (total + limit - 1) / limit
	w.Header().Set("X-Total-Pages", strconv.Itoa(totalPages))

	if page < totalPages {
		w.Header().Set("X-Has-Next", "true")
	}
	if page > 1 {
		w.Header().Set("X-Has-Previous", "true")
	}
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	// CORS headers are now handled by the global CORS middleware
	s.writeError(w, http.StatusNotFound, "Endpoint not found")
}

func (s *Server) handleMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	// CORS headers are now handled by the global CORS middleware
	s.writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// getMetricPath normalizes URL paths for metrics to avoid cardinality explosion
func getMetricPath(path string) string {
	// Replace IDs in paths with placeholders
	parts := strings.Split(path, "/")
	for i, part := range parts {
		// Skip empty parts and API version
		if part == "" || part == "api" || part == "v1" {
			continue
		}

		// Check if this part is likely an ID (UUID, numeric ID, etc.)
		if i > 0 && (isUUID(part) || isNumericID(part)) {
			parts[i] = ":id"
		}
	}

	return strings.Join(parts, "/")
}

// isUUID checks if a string looks like a UUID
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	// Simple check for UUID format (8-4-4-4-12)
	parts := strings.Split(s, "-")
	return len(parts) == 5 && len(parts[0]) == 8 && len(parts[1]) == 4 &&
		len(parts[2]) == 4 && len(parts[3]) == 4 && len(parts[4]) == 12
}

// isNumericID checks if a string is a numeric ID
func isNumericID(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}
