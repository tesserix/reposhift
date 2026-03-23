package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// ParallelProcessor handles parallel processing of migration resources
type ParallelProcessor struct {
	logger       logr.Logger
	maxWorkers   int
	retryHandler *RetryHandler
}

// NewParallelProcessor creates a new parallel processor
func NewParallelProcessor(logger logr.Logger, maxWorkers int, retryHandler *RetryHandler) *ParallelProcessor {
	if maxWorkers <= 0 {
		maxWorkers = 5 // Default
	}
	return &ParallelProcessor{
		logger:       logger,
		maxWorkers:   maxWorkers,
		retryHandler: retryHandler,
	}
}

// ResourceResult contains the result of processing a single resource
type ResourceResult struct {
	ResourceIndex int
	Resource      migrationv1.MigrationResource
	Status        migrationv1.ResourceMigrationStatus
	Error         error
	Duration      time.Duration
}

// ProcessResourcesParallel processes multiple resources in parallel
func (p *ParallelProcessor) ProcessResourcesParallel(
	ctx context.Context,
	r *AdoToGitMigrationReconciler,
	migration *migrationv1.AdoToGitMigration,
	token string,
) ([]ResourceResult, error) {

	resources := migration.Spec.Resources
	if len(resources) == 0 {
		return []ResourceResult{}, nil
	}

	// Get parallel workers setting from migration spec
	workers := migration.Spec.Settings.ParallelWorkers
	if workers == 0 {
		workers = p.maxWorkers
	}

	p.logger.Info("Starting parallel resource processing",
		"totalResources", len(resources),
		"parallelWorkers", workers)

	// Create worker pool
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	resultChan := make(chan ResourceResult, len(resources))

	// Process each resource
	for i, resource := range resources {
		// Check if resource already processed
		resourceIndex, alreadyProcessed := p.checkResourceStatus(migration, resource)

		if alreadyProcessed {
			p.logger.Info("Resource already processed, skipping",
				"resource", resource.TargetName,
				"status", migration.Status.ResourceStatuses[resourceIndex].Status)
			continue
		}

		wg.Add(1)
		go func(idx int, res migrationv1.MigrationResource, resIdx int) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Process resource
			result := p.processResourceWithTiming(ctx, r, migration, res, resIdx, token)
			resultChan <- result

		}(i, resource, resourceIndex)
	}

	// Wait for all workers to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make([]ResourceResult, 0, len(resources))
	for result := range resultChan {
		results = append(results, result)

		// Log result
		if result.Error != nil {
			p.logger.Error(result.Error, "Resource processing failed",
				"resource", result.Resource.TargetName,
				"duration", result.Duration)
		} else {
			p.logger.Info("Resource processing completed",
				"resource", result.Resource.TargetName,
				"status", result.Status.Status,
				"duration", result.Duration)
		}
	}

	p.logger.Info("Parallel processing completed",
		"totalProcessed", len(results),
		"successful", p.countSuccessful(results),
		"failed", p.countFailed(results))

	return results, nil
}

// processResourceWithTiming processes a single resource and measures duration
func (p *ParallelProcessor) processResourceWithTiming(
	ctx context.Context,
	r *AdoToGitMigrationReconciler,
	migration *migrationv1.AdoToGitMigration,
	resource migrationv1.MigrationResource,
	resourceIndex int,
	token string,
) ResourceResult {

	startTime := time.Now()

	// Create resource status
	now := metav1.Now()
	resourceStatus := migrationv1.ResourceMigrationStatus{
		ResourceID: resource.SourceID,
		Type:       resource.Type,
		SourceName: resource.SourceName,
		TargetName: resource.TargetName,
		Status:     migrationv1.ResourceStatusRunning,
		Progress:   0,
		StartTime:  &now,
		Name:       resource.SourceName,
	}

	// Update status in migration
	if resourceIndex >= 0 && resourceIndex < len(migration.Status.ResourceStatuses) {
		migration.Status.ResourceStatuses[resourceIndex] = resourceStatus
	} else {
		migration.Status.ResourceStatuses = append(migration.Status.ResourceStatuses, resourceStatus)
		resourceIndex = len(migration.Status.ResourceStatuses) - 1
	}

	// Process the resource
	err := r.processResourceByType(ctx, migration, &resource, &resourceStatus, token)

	// Update completion time and status
	completionTime := metav1.Now()
	resourceStatus.CompletionTime = &completionTime

	if err != nil {
		resourceStatus.Status = migrationv1.ResourceStatusFailed
		resourceStatus.ErrorMessage = err.Error()
		resourceStatus.Error = err.Error()
	} else {
		resourceStatus.Status = migrationv1.ResourceStatusCompleted
		resourceStatus.Progress = 100
	}

	// Update status in migration
	if resourceIndex >= 0 && resourceIndex < len(migration.Status.ResourceStatuses) {
		migration.Status.ResourceStatuses[resourceIndex] = resourceStatus
	}

	duration := time.Since(startTime)

	return ResourceResult{
		ResourceIndex: resourceIndex,
		Resource:      resource,
		Status:        resourceStatus,
		Error:         err,
		Duration:      duration,
	}
}

// checkResourceStatus checks if a resource has already been processed
func (p *ParallelProcessor) checkResourceStatus(
	migration *migrationv1.AdoToGitMigration,
	resource migrationv1.MigrationResource,
) (int, bool) {

	for i, status := range migration.Status.ResourceStatuses {
		if status.SourceName == resource.SourceName {
			if status.Status == migrationv1.ResourceStatusCompleted {
				return i, true
			}
			if status.Status == migrationv1.ResourceStatusFailed {
				// Check if retry is allowed
				maxRetries := migration.Spec.Settings.RetryAttempts
				if maxRetries == 0 {
					maxRetries = 3
				}
				if status.Progress >= maxRetries {
					return i, true // Exhausted retries
				}
			}
			return i, false
		}
	}
	return -1, false
}

// countSuccessful counts successful resource results
func (p *ParallelProcessor) countSuccessful(results []ResourceResult) int {
	count := 0
	for _, result := range results {
		if result.Error == nil && result.Status.Status == migrationv1.ResourceStatusCompleted {
			count++
		}
	}
	return count
}

// countFailed counts failed resource results
func (p *ParallelProcessor) countFailed(results []ResourceResult) int {
	count := 0
	for _, result := range results {
		if result.Error != nil || result.Status.Status == migrationv1.ResourceStatusFailed {
			count++
		}
	}
	return count
}

// UpdateMigrationProgress updates the migration progress based on results
func (p *ParallelProcessor) UpdateMigrationProgress(
	migration *migrationv1.AdoToGitMigration,
	results []ResourceResult,
) {

	totalResources := len(migration.Spec.Resources)
	completed := 0
	failed := 0
	processing := 0

	for _, status := range migration.Status.ResourceStatuses {
		switch status.Status {
		case migrationv1.ResourceStatusCompleted:
			completed++
		case migrationv1.ResourceStatusFailed:
			failed++
		case migrationv1.ResourceStatusRunning:
			processing++
		}
	}

	migration.Status.Progress.Total = totalResources
	migration.Status.Progress.Completed = completed
	migration.Status.Progress.Failed = failed
	migration.Status.Progress.Processing = processing

	if totalResources > 0 {
		migration.Status.Progress.Percentage = (completed * 100) / totalResources
	}

	migration.Status.Progress.ProgressSummary = fmt.Sprintf("%d/%d", completed, totalResources)
}

// ProcessResourcesSequential processes resources one at a time (fallback mode)
func (p *ParallelProcessor) ProcessResourcesSequential(
	ctx context.Context,
	r *AdoToGitMigrationReconciler,
	migration *migrationv1.AdoToGitMigration,
	token string,
) ([]ResourceResult, error) {

	resources := migration.Spec.Resources
	results := make([]ResourceResult, 0, len(resources))

	p.logger.Info("Starting sequential resource processing",
		"totalResources", len(resources))

	for i, resource := range resources {
		// Check if resource already processed
		resourceIndex, alreadyProcessed := p.checkResourceStatus(migration, resource)

		if alreadyProcessed {
			p.logger.Info("Resource already processed, skipping",
				"resource", resource.TargetName)
			continue
		}

		// Update current item
		migration.Status.Progress.CurrentItem = i + 1
		migration.Status.Progress.ProgressSummary = fmt.Sprintf("%d/%d", i+1, len(resources))

		// Process resource
		result := p.processResourceWithTiming(ctx, r, migration, resource, resourceIndex, token)
		results = append(results, result)

		if result.Error != nil {
			p.logger.Error(result.Error, "Resource processing failed",
				"resource", resource.TargetName)
		}
	}

	p.logger.Info("Sequential processing completed",
		"totalProcessed", len(results),
		"successful", p.countSuccessful(results),
		"failed", p.countFailed(results))

	return results, nil
}
