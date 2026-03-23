package metrics

import (
	"context"
	"runtime"
	"time"

	"github.com/go-logr/logr"
)

// Collector periodically collects runtime metrics
type Collector struct {
	log      logr.Logger
	interval time.Duration
	stopCh   chan struct{}
}

// NewCollector creates a new metrics collector
func NewCollector(log logr.Logger, interval time.Duration) *Collector {
	return &Collector{
		log:      log,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins collecting metrics periodically
func (c *Collector) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	c.log.Info("Starting metrics collector", "interval", c.interval)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("Stopping metrics collector")
			return
		case <-c.stopCh:
			c.log.Info("Stopping metrics collector")
			return
		case <-ticker.C:
			c.collectRuntimeMetrics()
		}
	}
}

// Stop stops the metrics collector
func (c *Collector) Stop() {
	close(c.stopCh)
}

// collectRuntimeMetrics collects Go runtime metrics
func (c *Collector) collectRuntimeMetrics() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Update memory metrics
	MemoryUsage.WithLabelValues("alloc").Set(float64(memStats.Alloc))
	MemoryUsage.WithLabelValues("sys").Set(float64(memStats.Sys))
	MemoryUsage.WithLabelValues("heap").Set(float64(memStats.HeapAlloc))
	MemoryUsage.WithLabelValues("heap_sys").Set(float64(memStats.HeapSys))
	MemoryUsage.WithLabelValues("heap_idle").Set(float64(memStats.HeapIdle))
	MemoryUsage.WithLabelValues("heap_in_use").Set(float64(memStats.HeapInuse))

	// Update goroutine count
	GoroutinesActive.Set(float64(runtime.NumGoroutine()))

	c.log.V(2).Info("Collected runtime metrics",
		"goroutines", runtime.NumGoroutine(),
		"alloc_mb", memStats.Alloc/1024/1024,
		"sys_mb", memStats.Sys/1024/1024)
}

// MigrationMetrics holds metrics for a single migration
type MigrationMetrics struct {
	Type      string
	StartTime time.Time
	Phase     string
}

// NewMigrationMetrics creates a new migration metrics tracker
func NewMigrationMetrics(migrationType string) *MigrationMetrics {
	return &MigrationMetrics{
		Type:      migrationType,
		StartTime: time.Now(),
		Phase:     "Pending",
	}
}

// Start records the start of a migration
func (m *MigrationMetrics) Start() {
	RecordMigrationStart(m.Type, m.Phase)
	PendingMigrations.WithLabelValues(m.Type).Inc()
}

// TransitionPhase records a phase transition
func (m *MigrationMetrics) TransitionPhase(newPhase string) {
	oldPhase := m.Phase
	m.Phase = newPhase
	RecordPhaseTransition(m.Type, oldPhase, newPhase)

	// Update pending count
	if oldPhase == "Pending" {
		PendingMigrations.WithLabelValues(m.Type).Dec()
	}
}

// Complete records the completion of a migration
func (m *MigrationMetrics) Complete(status string) {
	duration := time.Since(m.StartTime)
	RecordMigrationComplete(m.Type, status, duration)
}

// ReconciliationTimer tracks reconciliation timing
type ReconciliationTimer struct {
	Controller string
	Phase      string
	StartTime  time.Time
}

// NewReconciliationTimer creates a new reconciliation timer
func NewReconciliationTimer(controller, phase string) *ReconciliationTimer {
	return &ReconciliationTimer{
		Controller: controller,
		Phase:      phase,
		StartTime:  time.Now(),
	}
}

// ObserveSuccess records a successful reconciliation
func (rt *ReconciliationTimer) ObserveSuccess() {
	duration := time.Since(rt.StartTime)
	RecordReconciliation(rt.Controller, rt.Phase, "success", duration)
}

// ObserveError records a failed reconciliation
func (rt *ReconciliationTimer) ObserveError() {
	duration := time.Since(rt.StartTime)
	RecordReconciliation(rt.Controller, rt.Phase, "error", duration)
}

// APITimer tracks API request timing
type APITimer struct {
	Service   string
	Endpoint  string
	StartTime time.Time
}

// NewAPITimer creates a new API request timer
func NewAPITimer(service, endpoint string) *APITimer {
	return &APITimer{
		Service:   service,
		Endpoint:  endpoint,
		StartTime: time.Now(),
	}
}

// ObserveSuccess records a successful API request
func (at *APITimer) ObserveSuccess() {
	duration := time.Since(at.StartTime)
	RecordAPIRequest(at.Service, at.Endpoint, "success", duration)
}

// ObserveError records a failed API request
func (at *APITimer) ObserveError(statusCode int) {
	duration := time.Since(at.StartTime)
	status := "error"
	if statusCode == 429 {
		status = "rate_limited"
		RecordRateLimitHit(at.Service)
	} else if statusCode >= 500 {
		status = "server_error"
	} else if statusCode >= 400 {
		status = "client_error"
	}
	RecordAPIRequest(at.Service, at.Endpoint, status, duration)
}

// RepositoryMetrics tracks repository migration metrics
type RepositoryMetrics struct {
	Visibility   string
	Organization string
	SizeBytes    int64
	Commits      int
	Branches     int
	Tags         int
}

// Record records all repository metrics
func (rm *RepositoryMetrics) Record() {
	RecordRepositoryMigration(
		rm.Visibility,
		rm.Organization,
		rm.SizeBytes,
		rm.Commits,
		rm.Branches,
		rm.Tags,
	)
}

// WorkItemMetrics tracks work item migration metrics
type WorkItemMetrics struct {
	SourceType  string
	TargetType  string
	Comments    int
	Attachments int
}

// Record records all work item metrics
func (wm *WorkItemMetrics) Record() {
	RecordWorkItemMigration(
		wm.SourceType,
		wm.TargetType,
		wm.Comments,
		wm.Attachments,
	)
}

// CacheMetrics tracks cache performance
type CacheMetrics struct {
	Type string
}

// NewCacheMetrics creates a new cache metrics tracker
func NewCacheMetrics(cacheType string) *CacheMetrics {
	return &CacheMetrics{
		Type: cacheType,
	}
}

// RecordHit records a cache hit
func (cm *CacheMetrics) RecordHit() {
	CacheHits.WithLabelValues(cm.Type).Inc()
}

// RecordMiss records a cache miss
func (cm *CacheMetrics) RecordMiss() {
	CacheMisses.WithLabelValues(cm.Type).Inc()
}

// GetHitRate calculates cache hit rate (requires querying Prometheus)
func (cm *CacheMetrics) GetHitRate() float64 {
	// This would require querying Prometheus
	// For now, return 0 - implement with Prometheus client if needed
	return 0.0
}

// BusinessMetrics tracks business-level metrics
type BusinessMetrics struct {
	Organization string
}

// NewBusinessMetrics creates a new business metrics tracker
func NewBusinessMetrics(organization string) *BusinessMetrics {
	return &BusinessMetrics{
		Organization: organization,
	}
}

// RecordUser records a user impact
func (bm *BusinessMetrics) RecordUser() {
	UsersImpacted.WithLabelValues(bm.Organization).Inc()
}

// RecordTeam records a team impact
func (bm *BusinessMetrics) RecordTeam() {
	TeamsImpacted.WithLabelValues(bm.Organization).Inc()
}

// RecordProject records a project migration
func (bm *BusinessMetrics) RecordProject() {
	ProjectsMigrated.WithLabelValues(bm.Organization).Inc()
}

// HealthStatus represents health status values
type HealthStatus int

const (
	HealthStatusUnhealthy HealthStatus = 0
	HealthStatusHealthy   HealthStatus = 1
)

// SetComponentHealth sets the health status of a component
func SetComponentHealth(component string, status HealthStatus) {
	OperatorHealth.WithLabelValues(component).Set(float64(status))
}

// SetLeaderStatus sets the leader election status
func SetLeaderStatus(isLeader bool) {
	if isLeader {
		LeaderElectionStatus.Set(1)
	} else {
		LeaderElectionStatus.Set(0)
	}
}

// SetBuildInfo sets build information
func SetBuildInfo(version, gitCommit, buildDate, goVersion string) {
	BuildInfo.WithLabelValues(version, gitCommit, buildDate, goVersion).Set(1)
}

// StatusUpdateMetrics tracks status update performance
type StatusUpdateMetrics struct {
	Controller string
	Retries    int
}

// NewStatusUpdateMetrics creates a new status update metrics tracker
func NewStatusUpdateMetrics(controller string) *StatusUpdateMetrics {
	return &StatusUpdateMetrics{
		Controller: controller,
		Retries:    0,
	}
}

// RecordAttempt records a status update attempt
func (sum *StatusUpdateMetrics) RecordAttempt(success bool) {
	if success {
		StatusUpdates.WithLabelValues(sum.Controller, "success").Inc()
		StatusUpdateRetries.WithLabelValues(sum.Controller).Observe(float64(sum.Retries))
	} else {
		StatusUpdates.WithLabelValues(sum.Controller, "error").Inc()
		sum.Retries++
	}
}

// RecordConflict records a conflict during status update
func (sum *StatusUpdateMetrics) RecordConflict() {
	StatusUpdateConflicts.WithLabelValues(sum.Controller).Inc()
	sum.Retries++
}

// QueueMetrics tracks queue performance
type QueueMetrics struct {
	Controller string
}

// NewQueueMetrics creates a new queue metrics tracker
func NewQueueMetrics(controller string) *QueueMetrics {
	return &QueueMetrics{
		Controller: controller,
	}
}

// SetDepth sets the current queue depth
func (qm *QueueMetrics) SetDepth(depth int) {
	QueueDepth.WithLabelValues(qm.Controller).Set(float64(depth))
}

// ObserveLatency observes queue latency
func (qm *QueueMetrics) ObserveLatency(duration time.Duration) {
	QueueLatency.WithLabelValues(qm.Controller).Observe(duration.Seconds())
}
