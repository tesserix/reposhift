package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Namespace for all custom metrics
const (
	MetricsNamespace = "ado_github_migration"
	MetricsSubsystem = "operator"
)

// Labels used across metrics
const (
	LabelMigrationType = "migration_type" // repository, workitem, pipeline, board
	LabelPhase         = "phase"          // Pending, Running, Completed, Failed
	LabelResourceType  = "resource_type"  // repository, issue, pipeline
	LabelSourceType    = "source_type"    // ADO work item type, pipeline type
	LabelTargetType    = "target_type"    // GitHub issue label, workflow type
	LabelStatus        = "status"         // success, failure, timeout
	LabelErrorType     = "error_type"     // retryable, terminal, validation
	LabelOperation     = "operation"      // clone, push, validate, etc.
	LabelService       = "service"        // github, azuredevops, kubernetes
	LabelEndpoint      = "endpoint"       // API endpoint
	LabelController    = "controller"     // controller name
	LabelNamespace     = "namespace"      // Kubernetes namespace
	LabelVisibility    = "visibility"     // public, private, internal
	LabelOrganization  = "organization"   // Source ADO org or target GitHub org
)

var (
	// ============================================================================
	// MIGRATION LIFECYCLE METRICS
	// ============================================================================

	// MigrationsTotal tracks total number of migrations by type and status
	MigrationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "migrations_total",
			Help:      "Total number of migrations initiated, labeled by type and final status",
		},
		[]string{LabelMigrationType, LabelStatus},
	)

	// MigrationsActive tracks currently active migrations by type and phase
	MigrationsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "migrations_active",
			Help:      "Number of currently active migrations, labeled by type and phase",
		},
		[]string{LabelMigrationType, LabelPhase},
	)

	// MigrationDuration tracks migration duration from start to completion
	MigrationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "migration_duration_seconds",
			Help:      "Duration of migrations from start to completion in seconds",
			Buckets:   []float64{30, 60, 120, 300, 600, 1800, 3600, 7200, 14400}, // 30s to 4h
		},
		[]string{LabelMigrationType, LabelStatus},
	)

	// MigrationPhaseTransitions tracks phase transitions
	MigrationPhaseTransitions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "migration_phase_transitions_total",
			Help:      "Total number of migration phase transitions",
		},
		[]string{LabelMigrationType, "from_phase", "to_phase"},
	)

	// ============================================================================
	// RESOURCE MIGRATION METRICS (Repositories, Work Items, Pipelines)
	// ============================================================================

	// RepositoriesMigrated tracks successfully migrated repositories
	RepositoriesMigrated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "repositories_migrated_total",
			Help:      "Total number of repositories successfully migrated",
		},
		[]string{LabelVisibility, LabelOrganization},
	)

	// RepositorySize tracks the size of migrated repositories
	RepositorySize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "repository_size_bytes",
			Help:      "Size of migrated repositories in bytes",
			Buckets:   prometheus.ExponentialBuckets(1024*1024, 10, 8), // 1MB to ~1TB
		},
		[]string{LabelVisibility},
	)

	// RepositoryCommits tracks number of commits migrated per repository
	RepositoryCommits = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "repository_commits_total",
			Help:      "Number of commits in migrated repositories",
			Buckets:   []float64{10, 50, 100, 500, 1000, 5000, 10000, 50000},
		},
		[]string{},
	)

	// RepositoryBranches tracks number of branches migrated per repository
	RepositoryBranches = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "repository_branches_total",
			Help:      "Number of branches in migrated repositories",
			Buckets:   []float64{1, 5, 10, 20, 50, 100, 200},
		},
		[]string{},
	)

	// RepositoryTags tracks number of tags migrated per repository
	RepositoryTags = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "repository_tags_total",
			Help:      "Number of tags in migrated repositories",
			Buckets:   []float64{0, 1, 5, 10, 25, 50, 100, 200},
		},
		[]string{},
	)

	// WorkItemsMigrated tracks successfully migrated work items
	WorkItemsMigrated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "work_items_migrated_total",
			Help:      "Total number of work items successfully migrated",
		},
		[]string{LabelSourceType, LabelTargetType},
	)

	// WorkItemComments tracks comments migrated
	WorkItemComments = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "work_item_comments_migrated_total",
			Help:      "Total number of work item comments migrated",
		},
		[]string{},
	)

	// WorkItemAttachments tracks attachments migrated
	WorkItemAttachments = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "work_item_attachments_migrated_total",
			Help:      "Total number of work item attachments migrated",
		},
		[]string{},
	)

	// DataTransferred tracks total data transferred
	DataTransferred = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "data_transferred_bytes_total",
			Help:      "Total bytes of data transferred during migrations",
		},
		[]string{LabelResourceType, "direction"}, // direction: upload, download
	)

	// ============================================================================
	// CONTROLLER PERFORMANCE METRICS
	// ============================================================================

	// ReconciliationDuration tracks time spent in reconciliation loops
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "reconciliation_duration_seconds",
			Help:      "Time spent in reconciliation loops in seconds",
			Buckets:   []float64{0.001, 0.01, 0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{LabelController, LabelPhase},
	)

	// ReconciliationsTotal tracks total number of reconciliations
	ReconciliationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "reconciliations_total",
			Help:      "Total number of reconciliation loops executed",
		},
		[]string{LabelController, LabelStatus},
	)

	// ReconciliationsSkipped tracks reconciliations skipped due to observedGeneration
	ReconciliationsSkipped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "reconciliations_skipped_total",
			Help:      "Total number of reconciliations skipped (no spec changes)",
		},
		[]string{LabelController},
	)

	// StatusUpdates tracks status update operations
	StatusUpdates = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "status_updates_total",
			Help:      "Total number of status update operations",
		},
		[]string{LabelController, LabelStatus},
	)

	// StatusUpdateConflicts tracks conflicts during status updates
	StatusUpdateConflicts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "status_update_conflicts_total",
			Help:      "Total number of conflicts during status updates",
		},
		[]string{LabelController},
	)

	// StatusUpdateRetries tracks status update retry attempts
	StatusUpdateRetries = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "status_update_retries",
			Help:      "Number of retries needed for successful status update",
			Buckets:   []float64{0, 1, 2, 3, 4, 5, 10},
		},
		[]string{LabelController},
	)

	// ============================================================================
	// API CLIENT METRICS
	// ============================================================================

	// APIRequestsTotal tracks API requests to external services
	APIRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "api_requests_total",
			Help:      "Total number of API requests to external services",
		},
		[]string{LabelService, LabelEndpoint, LabelStatus},
	)

	// APIRequestDuration tracks API request latency
	APIRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "api_request_duration_seconds",
			Help:      "Duration of API requests to external services in seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
		},
		[]string{LabelService, LabelEndpoint},
	)

	// APIRateLimitRemaining tracks remaining rate limit
	APIRateLimitRemaining = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "api_rate_limit_remaining",
			Help:      "Remaining API rate limit for external services",
		},
		[]string{LabelService},
	)

	// APIRateLimitHits tracks rate limit hits
	APIRateLimitHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "api_rate_limit_hits_total",
			Help:      "Total number of times rate limit was hit",
		},
		[]string{LabelService},
	)

	// KubernetesAPIRequests tracks requests to Kubernetes API
	KubernetesAPIRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "kubernetes_api_requests_total",
			Help:      "Total number of requests to Kubernetes API",
		},
		[]string{LabelOperation, LabelResourceType, LabelStatus},
	)

	// ============================================================================
	// ERROR AND RETRY METRICS
	// ============================================================================

	// ErrorsTotal tracks errors by type and operation
	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "errors_total",
			Help:      "Total number of errors encountered",
		},
		[]string{LabelErrorType, LabelOperation, LabelController},
	)

	// RetriesTotal tracks retry attempts
	RetriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "retries_total",
			Help:      "Total number of retry attempts",
		},
		[]string{LabelOperation, LabelStatus},
	)

	// RetryBackoff tracks backoff duration
	RetryBackoff = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "retry_backoff_seconds",
			Help:      "Duration of retry backoff in seconds",
			Buckets:   []float64{1, 2, 5, 10, 30, 60, 120, 300, 600},
		},
		[]string{LabelOperation},
	)

	// ValidationErrors tracks validation errors
	ValidationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "validation_errors_total",
			Help:      "Total number of validation errors",
		},
		[]string{"field", "validation_type"},
	)

	// ============================================================================
	// RESOURCE USAGE METRICS
	// ============================================================================

	// GoroutinesActive tracks active goroutines
	GoroutinesActive = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "goroutines_active",
			Help:      "Number of currently active goroutines in the operator",
		},
	)

	// MemoryUsage tracks memory usage
	MemoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "memory_usage_bytes",
			Help:      "Memory usage in bytes",
		},
		[]string{"type"}, // type: alloc, sys, heap
	)

	// CacheHits tracks cache hit ratio
	CacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "cache_hits_total",
			Help:      "Total number of cache hits",
		},
		[]string{"cache_type"}, // token, config, etc.
	)

	// CacheMisses tracks cache misses
	CacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "cache_misses_total",
			Help:      "Total number of cache misses",
		},
		[]string{"cache_type"},
	)

	// ============================================================================
	// BUSINESS METRICS
	// ============================================================================

	// UsersImpacted tracks number of unique users affected by migrations
	UsersImpacted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "users_impacted_total",
			Help:      "Total number of unique users impacted by migrations",
		},
		[]string{LabelOrganization},
	)

	// TeamsImpacted tracks number of teams affected
	TeamsImpacted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "teams_impacted_total",
			Help:      "Total number of teams impacted by migrations",
		},
		[]string{LabelOrganization},
	)

	// ProjectsMigrated tracks number of projects migrated
	ProjectsMigrated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "projects_migrated_total",
			Help:      "Total number of projects migrated",
		},
		[]string{LabelOrganization},
	)

	// MigrationCost tracks estimated cost of migrations (if applicable)
	MigrationCost = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "migration_cost_total",
			Help:      "Estimated total cost of migrations (in cost units)",
		},
		[]string{LabelMigrationType},
	)

	// ============================================================================
	// QUEUE AND WORKLOAD METRICS
	// ============================================================================

	// QueueDepth tracks work queue depth
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "queue_depth",
			Help:      "Current depth of work queues",
		},
		[]string{LabelController},
	)

	// QueueLatency tracks time items spend in queue
	QueueLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "queue_latency_seconds",
			Help:      "Time items spend in queue before processing",
			Buckets:   []float64{0.1, 1, 5, 10, 30, 60, 300, 600},
		},
		[]string{LabelController},
	)

	// PendingMigrations tracks migrations waiting to start
	PendingMigrations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "pending_migrations",
			Help:      "Number of migrations in pending state",
		},
		[]string{LabelMigrationType},
	)

	// ============================================================================
	// HEALTH AND STATUS METRICS
	// ============================================================================

	// OperatorHealth tracks overall operator health status
	OperatorHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "health_status",
			Help:      "Operator health status (1=healthy, 0=unhealthy)",
		},
		[]string{"component"}, // controller, api_server, etc.
	)

	// LeaderElectionStatus tracks leader election status
	LeaderElectionStatus = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "leader_election_status",
			Help:      "Leader election status (1=leader, 0=follower)",
		},
	)

	// BuildInfo provides build information
	BuildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Subsystem: MetricsSubsystem,
			Name:      "build_info",
			Help:      "Build information for the operator",
		},
		[]string{"version", "git_commit", "build_date", "go_version"},
	)
)

// init registers all metrics with the controller-runtime metrics registry
func init() {
	// Migration lifecycle metrics
	metrics.Registry.MustRegister(
		MigrationsTotal,
		MigrationsActive,
		MigrationDuration,
		MigrationPhaseTransitions,
	)

	// Resource migration metrics
	metrics.Registry.MustRegister(
		RepositoriesMigrated,
		RepositorySize,
		RepositoryCommits,
		RepositoryBranches,
		RepositoryTags,
		WorkItemsMigrated,
		WorkItemComments,
		WorkItemAttachments,
		DataTransferred,
	)

	// Controller performance metrics
	metrics.Registry.MustRegister(
		ReconciliationDuration,
		ReconciliationsTotal,
		ReconciliationsSkipped,
		StatusUpdates,
		StatusUpdateConflicts,
		StatusUpdateRetries,
	)

	// API client metrics
	metrics.Registry.MustRegister(
		APIRequestsTotal,
		APIRequestDuration,
		APIRateLimitRemaining,
		APIRateLimitHits,
		KubernetesAPIRequests,
	)

	// Error and retry metrics
	metrics.Registry.MustRegister(
		ErrorsTotal,
		RetriesTotal,
		RetryBackoff,
		ValidationErrors,
	)

	// Resource usage metrics
	metrics.Registry.MustRegister(
		GoroutinesActive,
		MemoryUsage,
		CacheHits,
		CacheMisses,
	)

	// Business metrics
	metrics.Registry.MustRegister(
		UsersImpacted,
		TeamsImpacted,
		ProjectsMigrated,
		MigrationCost,
	)

	// Queue and workload metrics
	metrics.Registry.MustRegister(
		QueueDepth,
		QueueLatency,
		PendingMigrations,
	)

	// Health and status metrics
	metrics.Registry.MustRegister(
		OperatorHealth,
		LeaderElectionStatus,
		BuildInfo,
	)
}

// RecordMigrationStart records the start of a migration
func RecordMigrationStart(migrationType, phase string) {
	MigrationsActive.WithLabelValues(migrationType, phase).Inc()
}

// RecordMigrationComplete records the completion of a migration
func RecordMigrationComplete(migrationType, status string, duration time.Duration) {
	MigrationsTotal.WithLabelValues(migrationType, status).Inc()
	MigrationDuration.WithLabelValues(migrationType, status).Observe(duration.Seconds())
	// Decrement active (find and dec from any phase)
	for _, phase := range []string{"Pending", "Validating", "Running"} {
		if gauge, err := MigrationsActive.GetMetricWithLabelValues(migrationType, phase); err == nil {
			gauge.Dec()
		}
	}
}

// RecordPhaseTransition records a phase transition
func RecordPhaseTransition(migrationType, fromPhase, toPhase string) {
	MigrationPhaseTransitions.WithLabelValues(migrationType, fromPhase, toPhase).Inc()
	// Update active gauges
	MigrationsActive.WithLabelValues(migrationType, fromPhase).Dec()
	MigrationsActive.WithLabelValues(migrationType, toPhase).Inc()
}

// RecordReconciliation records a reconciliation with duration and status
func RecordReconciliation(controller, phase, status string, duration time.Duration) {
	ReconciliationsTotal.WithLabelValues(controller, status).Inc()
	ReconciliationDuration.WithLabelValues(controller, phase).Observe(duration.Seconds())
}

// RecordReconciliationSkipped records a skipped reconciliation
func RecordReconciliationSkipped(controller string) {
	ReconciliationsSkipped.WithLabelValues(controller).Inc()
}

// RecordAPIRequest records an API request with timing
func RecordAPIRequest(service, endpoint, status string, duration time.Duration) {
	APIRequestsTotal.WithLabelValues(service, endpoint, status).Inc()
	APIRequestDuration.WithLabelValues(service, endpoint).Observe(duration.Seconds())
}

// RecordError records an error
func RecordError(errorType, operation, controller string) {
	ErrorsTotal.WithLabelValues(errorType, operation, controller).Inc()
}

// RecordRepositoryMigration records a repository migration with all details
func RecordRepositoryMigration(visibility, organization string, sizeBytes int64, commits, branches, tags int) {
	RepositoriesMigrated.WithLabelValues(visibility, organization).Inc()
	RepositorySize.WithLabelValues(visibility).Observe(float64(sizeBytes))
	RepositoryCommits.WithLabelValues().Observe(float64(commits))
	RepositoryBranches.WithLabelValues().Observe(float64(branches))
	RepositoryTags.WithLabelValues().Observe(float64(tags))
	DataTransferred.WithLabelValues("repository", "upload").Add(float64(sizeBytes))
}

// RecordWorkItemMigration records work item migration
func RecordWorkItemMigration(sourceType, targetType string, comments, attachments int) {
	WorkItemsMigrated.WithLabelValues(sourceType, targetType).Inc()
	WorkItemComments.WithLabelValues().Add(float64(comments))
	WorkItemAttachments.WithLabelValues().Add(float64(attachments))
}

// UpdateRateLimit updates rate limit metrics
func UpdateRateLimit(service string, remaining int) {
	APIRateLimitRemaining.WithLabelValues(service).Set(float64(remaining))
}

// RecordRateLimitHit records when rate limit is hit
func RecordRateLimitHit(service string) {
	APIRateLimitHits.WithLabelValues(service).Inc()
}
