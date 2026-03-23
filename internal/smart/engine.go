package smart

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/time/rate"
)

// SmartMigrationEngine provides intelligent migration orchestration
type SmartMigrationEngine struct {
	log                logr.Logger
	rateLimiter        *AdaptiveRateLimiter
	dependencyGraph    *DependencyGraph
	circuitBreaker     *CircuitBreaker
	parallelExecutor   *ParallelExecutor
	optimizer          *MigrationOptimizer
	artifactMigrator   *ArtifactMigrator
	performanceMonitor *PerformanceMonitor
}

// NewSmartMigrationEngine creates a new smart migration engine
func NewSmartMigrationEngine(log logr.Logger) *SmartMigrationEngine {
	return &SmartMigrationEngine{
		log:                log,
		rateLimiter:        NewAdaptiveRateLimiter(log),
		dependencyGraph:    NewDependencyGraph(),
		circuitBreaker:     NewCircuitBreaker(log),
		parallelExecutor:   NewParallelExecutor(log, 10), // Max 10 concurrent
		optimizer:          NewMigrationOptimizer(log),
		artifactMigrator:   NewArtifactMigrator(log),
		performanceMonitor: NewPerformanceMonitor(log),
	}
}

// AnalyzeAndPlan analyzes the migration and creates an optimal execution plan
func (e *SmartMigrationEngine) AnalyzeAndPlan(ctx context.Context, migrations []MigrationRequest) (*MigrationPlan, error) {
	e.log.Info("Analyzing migrations and creating optimal plan")

	plan := &MigrationPlan{
		Migrations: make([]*PlannedMigration, 0, len(migrations)),
		CreatedAt:  time.Now(),
	}

	// Step 1: Analyze each migration
	for _, req := range migrations {
		analyzed, err := e.analyzeMigration(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to analyze migration %s: %w", req.Name, err)
		}
		plan.Migrations = append(plan.Migrations, analyzed)
	}

	// Step 2: Build dependency graph
	if err := e.dependencyGraph.BuildFromMigrations(plan.Migrations); err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Step 3: Detect circular dependencies
	if circular := e.dependencyGraph.DetectCircularDependencies(); len(circular) > 0 {
		return nil, fmt.Errorf("circular dependencies detected: %v", circular)
	}

	// Step 4: Optimize execution order
	plan.ExecutionOrder = e.dependencyGraph.TopologicalSort()

	// Step 5: Calculate optimal batch sizes
	for _, pm := range plan.Migrations {
		pm.OptimalBatchSize = e.optimizer.CalculateOptimalBatchSize(pm)
	}

	// Step 6: Estimate durations
	plan.EstimatedDuration = e.estimateTotalDuration(plan)

	// Step 7: Identify parallel execution opportunities
	plan.ParallelGroups = e.identifyParallelGroups(plan)

	e.log.Info("Migration plan created",
		"total_migrations", len(plan.Migrations),
		"parallel_groups", len(plan.ParallelGroups),
		"estimated_duration", plan.EstimatedDuration)

	return plan, nil
}

// ExecutePlan executes the migration plan with intelligent orchestration
func (e *SmartMigrationEngine) ExecutePlan(ctx context.Context, plan *MigrationPlan) (*MigrationResult, error) {
	e.log.Info("Starting smart migration execution", "migrations", len(plan.Migrations))

	result := &MigrationResult{
		StartTime: time.Now(),
		Results:   make(map[string]*IndividualResult),
	}

	// Execute in parallel groups
	for i, group := range plan.ParallelGroups {
		e.log.Info("Executing parallel group", "group", i+1, "migrations", len(group))

		groupResults := e.parallelExecutor.ExecuteGroup(ctx, group, func(ctx context.Context, pm *PlannedMigration) error {
			return e.executeSingleMigration(ctx, pm, result)
		})

		// Check for failures in this group
		for name, err := range groupResults {
			if err != nil {
				result.Results[name] = &IndividualResult{
					Success: false,
					Error:   err,
				}
				result.FailedCount++
			}
		}

		// Stop if circuit breaker opens
		if e.circuitBreaker.IsOpen() {
			e.log.Error(nil, "Circuit breaker opened, stopping migration",
				"completed", result.SuccessCount,
				"failed", result.FailedCount)
			result.Stopped = true
			break
		}
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	e.log.Info("Migration execution completed",
		"duration", result.Duration,
		"success", result.SuccessCount,
		"failed", result.FailedCount)

	return result, nil
}

// executeSingleMigration executes a single migration with all smart features
func (e *SmartMigrationEngine) executeSingleMigration(ctx context.Context, pm *PlannedMigration, result *MigrationResult) error {
	e.log.Info("Executing migration", "name", pm.Name, "complexity", pm.Complexity)

	migrationCtx := &MigrationContext{
		Context:        ctx,
		Migration:      pm,
		RateLimiter:    e.rateLimiter,
		CircuitBreaker: e.circuitBreaker,
	}

	// Start performance monitoring
	monitor := e.performanceMonitor.Start(pm.Name)
	defer monitor.End()

	// Execute with smart retry
	err := e.executeWithSmartRetry(migrationCtx, func() error {
		// Step 1: Migrate repository
		if pm.IncludeRepository {
			if err := e.migrateRepository(migrationCtx); err != nil {
				return fmt.Errorf("repository migration failed: %w", err)
			}
		}

		// Step 2: Migrate work items
		if pm.IncludeWorkItems {
			if err := e.migrateWorkItems(migrationCtx); err != nil {
				return fmt.Errorf("work items migration failed: %w", err)
			}
		}

		// Step 3: Migrate artifacts/packages
		if pm.IncludeArtifacts {
			if err := e.artifactMigrator.MigrateArtifacts(migrationCtx); err != nil {
				return fmt.Errorf("artifacts migration failed: %w", err)
			}
		}

		// Step 4: Migrate pipelines
		if pm.IncludePipelines {
			if err := e.migratePipelines(migrationCtx); err != nil {
				return fmt.Errorf("pipelines migration failed: %w", err)
			}
		}

		return nil
	})

	// Record result
	result.mu.Lock()
	defer result.mu.Unlock()

	if err != nil {
		result.Results[pm.Name] = &IndividualResult{
			Success:  false,
			Error:    err,
			Duration: monitor.Duration(),
		}
		result.FailedCount++
		e.circuitBreaker.RecordFailure()
		return err
	}

	result.Results[pm.Name] = &IndividualResult{
		Success:  true,
		Duration: monitor.Duration(),
	}
	result.SuccessCount++
	e.circuitBreaker.RecordSuccess()

	return nil
}

// executeWithSmartRetry executes an operation with intelligent retry logic
func (e *SmartMigrationEngine) executeWithSmartRetry(ctx *MigrationContext, operation func() error) error {
	maxRetries := 5
	baseDelay := 1 * time.Second
	maxDelay := 2 * time.Minute

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check circuit breaker before attempting
		if e.circuitBreaker.IsOpen() {
			return fmt.Errorf("circuit breaker is open, operation aborted")
		}

		// Wait for rate limiter
		if err := ctx.RateLimiter.Wait(ctx.Context); err != nil {
			return fmt.Errorf("rate limiter error: %w", err)
		}

		// Execute operation
		err := operation()
		if err == nil {
			return nil // Success
		}

		// Check if error is retryable
		if !isRetryableError(err) {
			return fmt.Errorf("terminal error: %w", err)
		}

		// Check if we should retry
		if attempt >= maxRetries {
			return fmt.Errorf("max retries exceeded: %w", err)
		}

		// Calculate backoff with jitter
		delay := calculateExponentialBackoff(attempt, baseDelay, maxDelay)
		e.log.Info("Retrying after error",
			"attempt", attempt+1,
			"max_retries", maxRetries,
			"delay", delay,
			"error", err)

		// Wait before retry
		select {
		case <-ctx.Context.Done():
			return ctx.Context.Err()
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation failed after %d retries", maxRetries)
}

// analyzeMigration performs deep analysis of a migration request
func (e *SmartMigrationEngine) analyzeMigration(ctx context.Context, req MigrationRequest) (*PlannedMigration, error) {
	pm := &PlannedMigration{
		Name:              req.Name,
		SourceRepo:        req.SourceRepo,
		TargetRepo:        req.TargetRepo,
		IncludeRepository: req.IncludeRepository,
		IncludeWorkItems:  req.IncludeWorkItems,
		IncludeArtifacts:  req.IncludeArtifacts,
		IncludePipelines:  req.IncludePipelines,
		Dependencies:      req.Dependencies,
	}

	// Analyze repository if included
	if req.IncludeRepository {
		repoAnalysis, err := e.analyzeRepository(ctx, req.SourceRepo)
		if err != nil {
			return nil, fmt.Errorf("repository analysis failed: %w", err)
		}
		pm.RepositoryAnalysis = repoAnalysis
		pm.EstimatedSize += repoAnalysis.SizeBytes
	}

	// Analyze work items if included
	if req.IncludeWorkItems {
		workItemAnalysis, err := e.analyzeWorkItems(ctx, req.SourceRepo)
		if err != nil {
			return nil, fmt.Errorf("work items analysis failed: %w", err)
		}
		pm.WorkItemAnalysis = workItemAnalysis
	}

	// Analyze artifacts if included
	if req.IncludeArtifacts {
		artifactAnalysis, err := e.artifactMigrator.AnalyzeArtifacts(ctx, req.SourceRepo)
		if err != nil {
			return nil, fmt.Errorf("artifacts analysis failed: %w", err)
		}
		pm.ArtifactAnalysis = artifactAnalysis
		pm.EstimatedSize += artifactAnalysis.TotalSizeBytes
	}

	// Calculate complexity score (0-100)
	pm.Complexity = e.calculateComplexity(pm)

	// Estimate duration
	pm.EstimatedDuration = e.estimateDuration(pm)

	return pm, nil
}

// calculateComplexity calculates a complexity score for the migration
func (e *SmartMigrationEngine) calculateComplexity(pm *PlannedMigration) int {
	score := 0

	// Repository complexity (0-40 points)
	if pm.RepositoryAnalysis != nil {
		ra := pm.RepositoryAnalysis

		// Size (0-15 points)
		if ra.SizeBytes > 10*1024*1024*1024 { // >10GB
			score += 15
		} else if ra.SizeBytes > 1*1024*1024*1024 { // >1GB
			score += 10
		} else if ra.SizeBytes > 100*1024*1024 { // >100MB
			score += 5
		}

		// Commits (0-10 points)
		if ra.CommitCount > 10000 {
			score += 10
		} else if ra.CommitCount > 1000 {
			score += 5
		} else if ra.CommitCount > 100 {
			score += 2
		}

		// Branches (0-10 points)
		if ra.BranchCount > 100 {
			score += 10
		} else if ra.BranchCount > 20 {
			score += 5
		} else if ra.BranchCount > 5 {
			score += 2
		}

		// LFS (0-5 points)
		if ra.HasLFS {
			score += 5
		}
	}

	// Work items complexity (0-20 points)
	if pm.WorkItemAnalysis != nil {
		wa := pm.WorkItemAnalysis
		if wa.TotalCount > 1000 {
			score += 20
		} else if wa.TotalCount > 500 {
			score += 15
		} else if wa.TotalCount > 100 {
			score += 10
		} else if wa.TotalCount > 10 {
			score += 5
		}
	}

	// Artifacts complexity (0-20 points)
	if pm.ArtifactAnalysis != nil {
		aa := pm.ArtifactAnalysis
		if aa.TotalArtifacts > 500 {
			score += 20
		} else if aa.TotalArtifacts > 100 {
			score += 15
		} else if aa.TotalArtifacts > 20 {
			score += 10
		} else if aa.TotalArtifacts > 5 {
			score += 5
		}
	}

	// Dependencies (0-10 points)
	if len(pm.Dependencies) > 5 {
		score += 10
	} else if len(pm.Dependencies) > 2 {
		score += 5
	} else if len(pm.Dependencies) > 0 {
		score += 2
	}

	// Pipelines (0-10 points)
	if pm.IncludePipelines {
		score += 10
	}

	return score
}

// estimateDuration estimates migration duration based on analysis
func (e *SmartMigrationEngine) estimateDuration(pm *PlannedMigration) time.Duration {
	duration := time.Duration(0)

	// Base time
	duration += 30 * time.Second

	// Repository time
	if pm.RepositoryAnalysis != nil {
		ra := pm.RepositoryAnalysis

		// Size-based time (1 minute per 100MB)
		sizeMB := ra.SizeBytes / (1024 * 1024)
		duration += time.Duration(sizeMB/100) * time.Minute

		// Commit-based time (1 second per 100 commits)
		duration += time.Duration(ra.CommitCount/100) * time.Second

		// Branch-based time (5 seconds per branch)
		duration += time.Duration(ra.BranchCount*5) * time.Second

		// LFS overhead
		if ra.HasLFS {
			duration += 5 * time.Minute
		}
	}

	// Work items time (10 seconds per item)
	if pm.WorkItemAnalysis != nil {
		duration += time.Duration(pm.WorkItemAnalysis.TotalCount*10) * time.Second
	}

	// Artifacts time (varies by type and size)
	if pm.ArtifactAnalysis != nil {
		aa := pm.ArtifactAnalysis
		// 5 seconds per artifact + size-based
		duration += time.Duration(aa.TotalArtifacts*5) * time.Second
		sizeMB := aa.TotalSizeBytes / (1024 * 1024)
		duration += time.Duration(sizeMB/10) * time.Minute
	}

	// Pipelines time
	if pm.IncludePipelines {
		duration += 10 * time.Minute
	}

	return duration
}

// estimateTotalDuration estimates total plan duration considering parallelism
func (e *SmartMigrationEngine) estimateTotalDuration(plan *MigrationPlan) time.Duration {
	if len(plan.ParallelGroups) == 0 {
		// Sequential execution
		total := time.Duration(0)
		for _, pm := range plan.Migrations {
			total += pm.EstimatedDuration
		}
		return total
	}

	// Parallel execution - sum of longest in each group
	total := time.Duration(0)
	for _, group := range plan.ParallelGroups {
		maxInGroup := time.Duration(0)
		for _, pm := range group {
			if pm.EstimatedDuration > maxInGroup {
				maxInGroup = pm.EstimatedDuration
			}
		}
		total += maxInGroup
	}
	return total
}

// identifyParallelGroups identifies migrations that can run in parallel
func (e *SmartMigrationEngine) identifyParallelGroups(plan *MigrationPlan) [][]*PlannedMigration {
	groups := make([][]*PlannedMigration, 0)
	processed := make(map[string]bool)

	// Process in dependency order
	for _, name := range plan.ExecutionOrder {
		if processed[name] {
			continue
		}

		// Find all migrations at this level (same dependency depth)
		group := make([]*PlannedMigration, 0)
		for _, pm := range plan.Migrations {
			if pm.Name == name {
				// Check if all dependencies are processed
				allDepsProcessed := true
				for _, dep := range pm.Dependencies {
					if !processed[dep] {
						allDepsProcessed = false
						break
					}
				}

				if allDepsProcessed {
					group = append(group, pm)
					processed[pm.Name] = true
				}
			}
		}

		// Find other migrations with same dependency level
		for _, pm := range plan.Migrations {
			if processed[pm.Name] {
				continue
			}

			allDepsProcessed := true
			for _, dep := range pm.Dependencies {
				if !processed[dep] {
					allDepsProcessed = false
					break
				}
			}

			if allDepsProcessed {
				group = append(group, pm)
				processed[pm.Name] = true
			}
		}

		if len(group) > 0 {
			groups = append(groups, group)
		}
	}

	return groups
}

// Smart helper functions

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Network errors are retryable
	retryablePatterns := []string{
		"timeout",
		"connection refused",
		"connection reset",
		"temporary failure",
		"rate limit",
		"429",
		"503",
		"502",
		"504",
	}

	for _, pattern := range retryablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

func calculateExponentialBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	delay := baseDelay * time.Duration(1<<uint(attempt)) // 2^attempt

	// Add jitter (±25%)
	jitter := time.Duration(float64(delay) * 0.25 * (2*getRandomFloat() - 1))
	delay += jitter

	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		len(s) > len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func getRandomFloat() float64 {
	// Simple random float between 0 and 1
	return float64(time.Now().UnixNano()%1000) / 1000.0
}

// Types and structures

type MigrationRequest struct {
	Name              string
	SourceRepo        string
	TargetRepo        string
	IncludeRepository bool
	IncludeWorkItems  bool
	IncludeArtifacts  bool
	IncludePipelines  bool
	Dependencies      []string
}

type PlannedMigration struct {
	Name               string
	SourceRepo         string
	TargetRepo         string
	IncludeRepository  bool
	IncludeWorkItems   bool
	IncludeArtifacts   bool
	IncludePipelines   bool
	Dependencies       []string
	RepositoryAnalysis *RepositoryAnalysis
	WorkItemAnalysis   *WorkItemAnalysis
	ArtifactAnalysis   *ArtifactAnalysis
	Complexity         int // 0-100
	EstimatedDuration  time.Duration
	EstimatedSize      int64
	OptimalBatchSize   int
}

type RepositoryAnalysis struct {
	SizeBytes     int64
	CommitCount   int
	BranchCount   int
	TagCount      int
	HasLFS        bool
	HasSubmodules bool
	Languages     map[string]int
}

type WorkItemAnalysis struct {
	TotalCount      int
	ByType          map[string]int
	ByState         map[string]int
	WithComments    int
	WithAttachments int
}

type ArtifactAnalysis struct {
	TotalArtifacts int
	TotalSizeBytes int64
	ByType         map[string]int
	ByFeed         map[string]int
}

type MigrationPlan struct {
	Migrations        []*PlannedMigration
	ExecutionOrder    []string
	ParallelGroups    [][]*PlannedMigration
	EstimatedDuration time.Duration
	CreatedAt         time.Time
}

type MigrationContext struct {
	Context        context.Context
	Migration      *PlannedMigration
	RateLimiter    *AdaptiveRateLimiter
	CircuitBreaker *CircuitBreaker
}

type MigrationResult struct {
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	SuccessCount int
	FailedCount  int
	Results      map[string]*IndividualResult
	Stopped      bool
	mu           sync.Mutex
}

type IndividualResult struct {
	Success  bool
	Error    error
	Duration time.Duration
}

// Placeholder functions for migrations (to be implemented in separate files)

func (e *SmartMigrationEngine) migrateRepository(ctx *MigrationContext) error {
	// Implemented in repository migration logic
	return nil
}

func (e *SmartMigrationEngine) migrateWorkItems(ctx *MigrationContext) error {
	// Implemented in work items migration logic
	return nil
}

func (e *SmartMigrationEngine) migratePipelines(ctx *MigrationContext) error {
	// Implemented in pipelines migration logic
	return nil
}

func (e *SmartMigrationEngine) analyzeRepository(ctx context.Context, repo string) (*RepositoryAnalysis, error) {
	// Placeholder - implement with actual ADO API calls
	return &RepositoryAnalysis{
		SizeBytes:   100 * 1024 * 1024, // 100MB
		CommitCount: 500,
		BranchCount: 5,
		TagCount:    10,
		HasLFS:      false,
	}, nil
}

func (e *SmartMigrationEngine) analyzeWorkItems(ctx context.Context, repo string) (*WorkItemAnalysis, error) {
	// Placeholder - implement with actual ADO API calls
	return &WorkItemAnalysis{
		TotalCount: 100,
		ByType: map[string]int{
			"Bug":  30,
			"Task": 70,
		},
	}, nil
}

// AdaptiveRateLimiter manages rate limiting with adaptive behavior
type AdaptiveRateLimiter struct {
	limiter *rate.Limiter
	log     logr.Logger
	mu      sync.Mutex
}

func NewAdaptiveRateLimiter(log logr.Logger) *AdaptiveRateLimiter {
	// Start with 10 requests per second
	return &AdaptiveRateLimiter{
		limiter: rate.NewLimiter(rate.Limit(10), 10),
		log:     log,
	}
}

func (arl *AdaptiveRateLimiter) Wait(ctx context.Context) error {
	return arl.limiter.Wait(ctx)
}

func (arl *AdaptiveRateLimiter) AdjustRate(newRate float64) {
	arl.mu.Lock()
	defer arl.mu.Unlock()
	arl.limiter.SetLimit(rate.Limit(newRate))
	arl.log.Info("Rate limit adjusted", "new_rate", newRate)
}

// CircuitBreaker prevents cascading failures
type CircuitBreaker struct {
	failures  int
	successes int
	isOpen    bool
	threshold int
	log       logr.Logger
	mu        sync.Mutex
}

func NewCircuitBreaker(log logr.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: 5, // Open after 5 consecutive failures
		log:       log,
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.successes++
	cb.failures = 0
	if cb.isOpen && cb.successes >= 3 {
		cb.isOpen = false
		cb.log.Info("Circuit breaker closed after successful operations")
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures++
	cb.successes = 0
	if cb.failures >= cb.threshold && !cb.isOpen {
		cb.isOpen = true
		cb.log.Error(nil, "Circuit breaker opened due to failures", "failures", cb.failures)
	}
}

func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.isOpen
}

// ParallelExecutor executes operations in parallel with concurrency control
type ParallelExecutor struct {
	maxConcurrent int
	log           logr.Logger
}

func NewParallelExecutor(log logr.Logger, maxConcurrent int) *ParallelExecutor {
	return &ParallelExecutor{
		maxConcurrent: maxConcurrent,
		log:           log,
	}
}

func (pe *ParallelExecutor) ExecuteGroup(ctx context.Context, group []*PlannedMigration, executor func(context.Context, *PlannedMigration) error) map[string]error {
	results := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Create semaphore for concurrency control
	sem := make(chan struct{}, pe.maxConcurrent)

	for _, pm := range group {
		wg.Add(1)
		go func(migration *PlannedMigration) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Execute
			err := executor(ctx, migration)

			// Store result
			mu.Lock()
			results[migration.Name] = err
			mu.Unlock()
		}(pm)
	}

	wg.Wait()
	return results
}
