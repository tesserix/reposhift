package controller

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	"github.com/tesserix/reposhift/internal/services"
	"github.com/tesserix/reposhift/internal/websocket"
)

const (
	// Worker lease duration (after this, batch is considered stale)
	workerLeaseDuration = 10 * time.Minute

	// Lease renewal interval
	leaseRenewalInterval = 2 * time.Minute

	// Default retry policy
	defaultMaxRetries          = 3
	defaultInitialDelaySeconds = 30
	defaultBackoffMultiplier   = 2
	defaultMaxDelaySeconds     = 600

	// Reconcile intervals
	batchPendingRequeue    = 5 * time.Second
	batchProcessingRequeue = 30 * time.Second
	batchRetryingRequeue   = 10 * time.Second
)

// BatchMigrationReconciler reconciles a BatchMigration object
type BatchMigrationReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	MigrationService *services.MigrationService
	GitHubService    *services.GitHubService
	WebSocketManager *websocket.Manager
	Recorder         record.EventRecorder
	WorkerID         string
}

//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=batchmigrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=batchmigrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=batchmigrations/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *BatchMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling BatchMigration", "namespacedName", req.NamespacedName)

	// Fetch the BatchMigration instance
	batch := &migrationv1.BatchMigration{}
	err := r.Get(ctx, req.NamespacedName, batch)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("BatchMigration resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get BatchMigration")
		return ctrl.Result{}, err
	}

	// Update last reconcile time
	now := metav1.Now()
	batch.Status.LastReconcileTime = &now

	// Initialize status if needed
	if batch.Status.Phase == "" {
		return r.initializeStatus(ctx, batch)
	}

	// Process based on current phase
	return r.processPhase(ctx, batch, logger)
}

// initializeStatus initializes the batch status
func (r *BatchMigrationReconciler) initializeStatus(ctx context.Context, batch *migrationv1.BatchMigration) (ctrl.Result, error) {
	batch.Status.Phase = migrationv1.BatchPhasePending
	batch.Status.Progress = migrationv1.BatchProgress{
		Total:      len(batch.Spec.Resources),
		Completed:  0,
		Failed:     0,
		Processing: 0,
		Percentage: 0,
	}
	batch.Status.RetryCount = 0

	r.Recorder.Event(batch, corev1.EventTypeNormal, "Initialized", "Batch migration initialized")

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{RequeueAfter: batchPendingRequeue}, err
	}

	return ctrl.Result{RequeueAfter: batchPendingRequeue}, nil
}

// processPhase processes the batch based on its current phase
func (r *BatchMigrationReconciler) processPhase(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	switch batch.Status.Phase {
	case migrationv1.BatchPhasePending:
		return r.processPending(ctx, batch, logger)
	case migrationv1.BatchPhaseClaimed:
		return r.processClaimed(ctx, batch, logger)
	case migrationv1.BatchPhaseProcessing:
		return r.processProcessing(ctx, batch, logger)
	case migrationv1.BatchPhaseStale:
		return r.processStale(ctx, batch, logger)
	case migrationv1.BatchPhaseRetrying:
		return r.processRetrying(ctx, batch, logger)
	case migrationv1.BatchPhaseCompleted, migrationv1.BatchPhaseFailed:
		// Terminal states
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// processPending tries to claim the batch for processing
func (r *BatchMigrationReconciler) processPending(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	// Try to claim the batch
	success, err := r.claimBatch(ctx, batch, logger)
	if err != nil {
		logger.Error(err, "Failed to claim batch")
		return ctrl.Result{RequeueAfter: batchPendingRequeue}, err
	}

	if !success {
		// Another worker claimed it or we chose not to claim it
		return ctrl.Result{RequeueAfter: batchPendingRequeue}, nil
	}

	// Successfully claimed, start processing
	logger.Info("Successfully claimed batch", "workerID", r.WorkerID, "batchNumber", batch.Spec.BatchNumber)
	return ctrl.Result{RequeueAfter: batchProcessingRequeue}, nil
}

// claimBatch attempts to claim a batch for processing
func (r *BatchMigrationReconciler) claimBatch(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (bool, error) {
	// Optimistic locking - try to claim the batch
	batch.Status.Phase = migrationv1.BatchPhaseClaimed
	batch.Status.ClaimedBy = r.WorkerID
	now := metav1.Now()
	batch.Status.ClaimedAt = &now
	batch.Status.LeaseExpiresAt = &metav1.Time{Time: now.Add(workerLeaseDuration)}

	err := r.Status().Update(ctx, batch)
	if err != nil {
		// Another worker claimed it, or there was a conflict
		return false, err
	}

	r.Recorder.Event(batch, corev1.EventTypeNormal, "BatchClaimed",
		fmt.Sprintf("Batch claimed by worker %s", r.WorkerID))

	return true, nil
}

// processClaimed transitions claimed batch to processing
func (r *BatchMigrationReconciler) processClaimed(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	// Verify we own this batch
	if batch.Status.ClaimedBy != r.WorkerID {
		logger.Info("Batch claimed by another worker", "claimedBy", batch.Status.ClaimedBy)
		return ctrl.Result{}, nil
	}

	// Check if lease has expired
	if r.isLeaseExpired(batch) {
		logger.Info("Lease expired, marking batch as stale")
		return r.markStale(ctx, batch)
	}

	// Start processing
	batch.Status.Phase = migrationv1.BatchPhaseProcessing
	now := metav1.Now()
	batch.Status.StartTime = &now
	batch.Status.Progress.CurrentStep = "Starting batch processing"

	r.Recorder.Event(batch, corev1.EventTypeNormal, "ProcessingStarted", "Batch processing started")

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{RequeueAfter: batchProcessingRequeue}, err
	}

	return ctrl.Result{RequeueAfter: batchProcessingRequeue}, nil
}

// processProcessing processes the batch resources
func (r *BatchMigrationReconciler) processProcessing(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	// Verify we own this batch
	if batch.Status.ClaimedBy != r.WorkerID {
		logger.Info("Batch owned by another worker", "claimedBy", batch.Status.ClaimedBy)
		return ctrl.Result{}, nil
	}

	// Check if lease has expired
	if r.isLeaseExpired(batch) {
		logger.Info("Lease expired during processing, marking batch as stale")
		return r.markStale(ctx, batch)
	}

	// Renew lease if needed
	if r.shouldRenewLease(batch) {
		if err := r.renewLease(ctx, batch, logger); err != nil {
			logger.Error(err, "Failed to renew lease")
			// Continue processing, will try again next time
		}
	}

	// Process each resource in the batch
	for i := range batch.Spec.Resources {
		resource := &batch.Spec.Resources[i]

		// Check if already processed
		if r.isResourceProcessed(batch, resource) {
			continue
		}

		// Process the resource
		logger.Info("Processing resource", "type", resource.Type, "source", resource.SourceName, "target", resource.TargetName)

		err := r.processResource(ctx, batch, resource, logger)
		if err != nil {
			logger.Error(err, "Failed to process resource", "resource", resource.SourceName)
			// Mark resource as failed but continue with others
			r.markResourceFailed(batch, resource, err)
		} else {
			logger.Info("Resource processed successfully", "resource", resource.SourceName)
			r.markResourceCompleted(batch, resource)
		}

		// Update progress
		r.updateProgress(batch)

		// Save status after each resource
		if err := r.Status().Update(ctx, batch); err != nil {
			logger.Error(err, "Failed to update batch status")
		}
	}

	// All resources processed - determine final state
	return r.completeBatch(ctx, batch, logger)
}

// processResource processes a single resource in the batch
func (r *BatchMigrationReconciler) processResource(ctx context.Context, batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource, logger logr.Logger) error {
	// Get authentication tokens
	adoToken, err := r.getAzureDevOpsToken(ctx, batch)
	if err != nil {
		return fmt.Errorf("failed to get ADO token: %w", err)
	}

	githubToken, err := r.getGitHubToken(ctx, batch)
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	// Process based on resource type
	switch resource.Type {
	case "repository":
		return r.processRepositoryResource(ctx, batch, resource, adoToken, githubToken, logger)
	case "work-item":
		return r.processWorkItemResource(ctx, batch, resource, adoToken, githubToken, logger)
	case "pipeline":
		return r.processPipelineResource(ctx, batch, resource, adoToken, githubToken, logger)
	default:
		return fmt.Errorf("unsupported resource type: %s", resource.Type)
	}
}

// processRepositoryResource processes a repository migration
func (r *BatchMigrationReconciler) processRepositoryResource(ctx context.Context, batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource, adoToken, githubToken string, logger logr.Logger) error {
	// Create a simplified migration object for the service
	migration := &migrationv1.AdoToGitMigration{
		Spec: migrationv1.AdoToGitMigrationSpec{
			Source:   batch.Spec.Source,
			Target:   batch.Spec.Target,
			Settings: batch.Spec.Settings,
		},
	}

	// Create resource status
	status := &migrationv1.ResourceMigrationStatus{
		ResourceID: resource.SourceID,
		Type:       resource.Type,
		SourceName: resource.SourceName,
		TargetName: resource.TargetName,
		Status:     migrationv1.ResourceStatusRunning,
	}

	// Progress callback
	progressCallback := func(progress *services.RepositoryMigrationProgress) {
		logger.Info("Repository migration progress",
			"resource", resource.SourceName,
			"phase", progress.Phase,
			"percentage", progress.Percentage,
		)
	}

	// Perform migration
	err := r.MigrationService.MigrateRepository(ctx, migration, resource, status, adoToken, githubToken, logger, progressCallback)
	if err != nil {
		return fmt.Errorf("repository migration failed: %w", err)
	}

	return nil
}

// processWorkItemResource processes work items migration (batch of issues)
func (r *BatchMigrationReconciler) processWorkItemResource(ctx context.Context, batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource, adoToken, githubToken string, logger logr.Logger) error {
	// TODO: Implement work item batch migration
	// This will handle migrating a batch of ADO work items to GitHub issues
	logger.Info("Work item migration not yet implemented", "resource", resource.SourceName)
	return fmt.Errorf("work item migration not yet implemented")
}

// processPipelineResource processes pipeline migration
func (r *BatchMigrationReconciler) processPipelineResource(ctx context.Context, batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource, adoToken, githubToken string, logger logr.Logger) error {
	// TODO: Implement pipeline migration
	logger.Info("Pipeline migration not yet implemented", "resource", resource.SourceName)
	return fmt.Errorf("pipeline migration not yet implemented")
}

// Helper functions

func (r *BatchMigrationReconciler) isLeaseExpired(batch *migrationv1.BatchMigration) bool {
	if batch.Status.LeaseExpiresAt == nil {
		return true
	}
	return time.Now().After(batch.Status.LeaseExpiresAt.Time)
}

func (r *BatchMigrationReconciler) shouldRenewLease(batch *migrationv1.BatchMigration) bool {
	if batch.Status.LeaseExpiresAt == nil {
		return true
	}
	timeUntilExpiry := time.Until(batch.Status.LeaseExpiresAt.Time)
	return timeUntilExpiry < leaseRenewalInterval
}

func (r *BatchMigrationReconciler) renewLease(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) error {
	now := metav1.Now()
	batch.Status.LeaseExpiresAt = &metav1.Time{Time: now.Add(workerLeaseDuration)}

	logger.Info("Renewing lease", "expiresAt", batch.Status.LeaseExpiresAt.Time)
	return r.Status().Update(ctx, batch)
}

func (r *BatchMigrationReconciler) markStale(ctx context.Context, batch *migrationv1.BatchMigration) (ctrl.Result, error) {
	batch.Status.Phase = migrationv1.BatchPhaseStale
	batch.Status.Progress.CurrentStep = "Batch marked as stale, worker lease expired"

	r.Recorder.Event(batch, corev1.EventTypeWarning, "LeaseExpired",
		fmt.Sprintf("Worker %s lease expired, batch marked as stale", batch.Status.ClaimedBy))

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{}, err
	}

	// Retry the batch
	return r.retryBatch(ctx, batch)
}

func (r *BatchMigrationReconciler) processStale(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	// Reset to pending for another worker to claim
	return r.retryBatch(ctx, batch)
}

func (r *BatchMigrationReconciler) processRetrying(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	// Check if it's time to retry
	if batch.Status.NextRetryTime != nil && time.Now().Before(batch.Status.NextRetryTime.Time) {
		waitTime := time.Until(batch.Status.NextRetryTime.Time)
		logger.Info("Waiting for retry", "waitTime", waitTime)
		return ctrl.Result{RequeueAfter: waitTime}, nil
	}

	// Reset to pending for retry
	batch.Status.Phase = migrationv1.BatchPhasePending
	batch.Status.ClaimedBy = ""
	batch.Status.ClaimedAt = nil
	batch.Status.LeaseExpiresAt = nil
	batch.Status.Progress.CurrentStep = fmt.Sprintf("Retrying batch (attempt %d)", batch.Status.RetryCount+1)

	logger.Info("Retrying batch", "retryCount", batch.Status.RetryCount)
	r.Recorder.Event(batch, corev1.EventTypeNormal, "BatchRetrying",
		fmt.Sprintf("Retrying batch (attempt %d)", batch.Status.RetryCount+1))

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{RequeueAfter: batchRetryingRequeue}, err
	}

	return ctrl.Result{RequeueAfter: batchPendingRequeue}, nil
}

func (r *BatchMigrationReconciler) retryBatch(ctx context.Context, batch *migrationv1.BatchMigration) (ctrl.Result, error) {
	retryPolicy := r.getRetryPolicy(batch)

	if batch.Status.RetryCount >= retryPolicy.MaxRetries {
		// Max retries exceeded, fail the batch
		return r.failBatch(ctx, batch, "Maximum retry attempts exceeded")
	}

	// Calculate next retry time with exponential backoff
	delay := r.calculateRetryDelay(batch.Status.RetryCount, retryPolicy)
	nextRetryTime := metav1.NewTime(time.Now().Add(delay))

	batch.Status.Phase = migrationv1.BatchPhaseRetrying
	batch.Status.RetryCount++
	batch.Status.NextRetryTime = &nextRetryTime
	batch.Status.ClaimedBy = ""
	batch.Status.ClaimedAt = nil
	batch.Status.LeaseExpiresAt = nil

	r.Recorder.Event(batch, corev1.EventTypeNormal, "BatchScheduledForRetry",
		fmt.Sprintf("Batch scheduled for retry in %v", delay))

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: delay}, nil
}

func (r *BatchMigrationReconciler) calculateRetryDelay(retryCount int, policy *migrationv1.RetryPolicy) time.Duration {
	// Exponential backoff with jitter
	baseDelay := time.Duration(policy.InitialDelaySeconds) * time.Second
	maxDelay := time.Duration(policy.MaxDelaySeconds) * time.Second

	delay := float64(baseDelay) * math.Pow(float64(policy.BackoffMultiplier), float64(retryCount))
	if time.Duration(delay) > maxDelay {
		delay = float64(maxDelay)
	}

	// Add jitter (up to 25% of delay)
	jitter := rand.Float64() * delay * 0.25
	totalDelay := time.Duration(delay + jitter)

	return totalDelay
}

func (r *BatchMigrationReconciler) getRetryPolicy(batch *migrationv1.BatchMigration) *migrationv1.RetryPolicy {
	if batch.Spec.RetryPolicy != nil {
		return batch.Spec.RetryPolicy
	}

	// Default retry policy
	return &migrationv1.RetryPolicy{
		MaxRetries:          defaultMaxRetries,
		InitialDelaySeconds: defaultInitialDelaySeconds,
		BackoffMultiplier:   defaultBackoffMultiplier,
		MaxDelaySeconds:     defaultMaxDelaySeconds,
	}
}

func (r *BatchMigrationReconciler) isResourceProcessed(batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource) bool {
	for _, status := range batch.Status.ResourceStatuses {
		if status.SourceName == resource.SourceName {
			return status.Status == migrationv1.ResourceStatusCompleted || status.Status == migrationv1.ResourceStatusFailed
		}
	}
	return false
}

func (r *BatchMigrationReconciler) markResourceFailed(batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource, err error) {
	now := metav1.Now()
	status := migrationv1.ResourceMigrationStatus{
		ResourceID:     resource.SourceID,
		Type:           resource.Type,
		SourceName:     resource.SourceName,
		TargetName:     resource.TargetName,
		Status:         migrationv1.ResourceStatusFailed,
		ErrorMessage:   err.Error(),
		CompletionTime: &now,
	}

	// Find existing status or append new one
	found := false
	for i, s := range batch.Status.ResourceStatuses {
		if s.SourceName == resource.SourceName {
			batch.Status.ResourceStatuses[i] = status
			found = true
			break
		}
	}
	if !found {
		batch.Status.ResourceStatuses = append(batch.Status.ResourceStatuses, status)
	}

	batch.Status.Progress.Failed++
}

func (r *BatchMigrationReconciler) markResourceCompleted(batch *migrationv1.BatchMigration, resource *migrationv1.MigrationResource) {
	now := metav1.Now()
	status := migrationv1.ResourceMigrationStatus{
		ResourceID:     resource.SourceID,
		Type:           resource.Type,
		SourceName:     resource.SourceName,
		TargetName:     resource.TargetName,
		Status:         migrationv1.ResourceStatusCompleted,
		CompletionTime: &now,
		Progress:       100,
	}

	// Find existing status or append new one
	found := false
	for i, s := range batch.Status.ResourceStatuses {
		if s.SourceName == resource.SourceName {
			batch.Status.ResourceStatuses[i] = status
			found = true
			break
		}
	}
	if !found {
		batch.Status.ResourceStatuses = append(batch.Status.ResourceStatuses, status)
	}

	batch.Status.Progress.Completed++
}

func (r *BatchMigrationReconciler) updateProgress(batch *migrationv1.BatchMigration) {
	total := batch.Status.Progress.Total
	completed := batch.Status.Progress.Completed
	failed := batch.Status.Progress.Failed

	if total > 0 {
		batch.Status.Progress.Percentage = ((completed + failed) * 100) / total
	}
}

func (r *BatchMigrationReconciler) completeBatch(ctx context.Context, batch *migrationv1.BatchMigration, logger logr.Logger) (ctrl.Result, error) {
	now := metav1.Now()
	batch.Status.CompletionTime = &now

	// Calculate statistics
	if batch.Status.StartTime != nil {
		duration := now.Time.Sub(batch.Status.StartTime.Time)
		if batch.Status.Statistics == nil {
			batch.Status.Statistics = &migrationv1.BatchStatistics{}
		}
		batch.Status.Statistics.Duration = metav1.Duration{Duration: duration}
	}

	// Determine final phase
	if batch.Status.Progress.Failed > 0 {
		// Partial failure - retry if possible
		if batch.Status.RetryCount < r.getRetryPolicy(batch).MaxRetries {
			logger.Info("Batch completed with failures, scheduling retry")
			return r.retryBatch(ctx, batch)
		}
		// Max retries exceeded
		return r.failBatch(ctx, batch, fmt.Sprintf("%d resources failed", batch.Status.Progress.Failed))
	}

	// All successful
	batch.Status.Phase = migrationv1.BatchPhaseCompleted
	batch.Status.Progress.CurrentStep = "Batch completed successfully"
	batch.Status.Progress.Percentage = 100

	logger.Info("Batch completed successfully",
		"batchNumber", batch.Spec.BatchNumber,
		"completed", batch.Status.Progress.Completed,
		"duration", batch.Status.Statistics.Duration.Duration,
	)

	r.Recorder.Event(batch, corev1.EventTypeNormal, "BatchCompleted",
		fmt.Sprintf("Batch completed successfully (%d resources)", batch.Status.Progress.Completed))

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *BatchMigrationReconciler) failBatch(ctx context.Context, batch *migrationv1.BatchMigration, message string) (ctrl.Result, error) {
	now := metav1.Now()
	batch.Status.Phase = migrationv1.BatchPhaseFailed
	batch.Status.ErrorMessage = message
	batch.Status.Progress.CurrentStep = "Batch failed"
	batch.Status.CompletionTime = &now

	r.Recorder.Event(batch, corev1.EventTypeWarning, "BatchFailed",
		fmt.Sprintf("Batch failed: %s", message))

	if err := r.Status().Update(ctx, batch); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// Token retrieval helpers (simplified - adapt from main controller)

func (r *BatchMigrationReconciler) getAzureDevOpsToken(ctx context.Context, batch *migrationv1.BatchMigration) (string, error) {
	auth := batch.Spec.Source.Auth

	if auth.PAT != nil {
		return r.getSecretValue(ctx, batch.Namespace, &auth.PAT.TokenRef)
	}

	if auth.ServicePrincipal != nil {
		// TODO: Implement service principal token acquisition
		return "", fmt.Errorf("service principal authentication not yet implemented in batch controller")
	}

	return "", fmt.Errorf("no Azure DevOps authentication configured")
}

func (r *BatchMigrationReconciler) getGitHubToken(ctx context.Context, batch *migrationv1.BatchMigration) (string, error) {
	auth := batch.Spec.Target.Auth

	if auth.TokenRef != nil {
		return r.getSecretValue(ctx, batch.Namespace, auth.TokenRef)
	}

	if auth.AppAuth != nil {
		// TODO: Implement GitHub App authentication
		return "", fmt.Errorf("GitHub App authentication not yet implemented in batch controller")
	}

	return "", fmt.Errorf("no GitHub authentication configured")
}

func (r *BatchMigrationReconciler) getSecretValue(ctx context.Context, namespace string, ref *migrationv1.SecretReference) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("secret reference is nil")
	}

	secretNamespace := namespace
	if ref.Namespace != "" {
		secretNamespace = ref.Namespace
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: secretNamespace,
		Name:      ref.Name,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", ref.Name, err)
	}

	value, exists := secret.Data[ref.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in secret %s", ref.Key, ref.Name)
	}

	return string(value), nil
}

// SetupWithManager sets up the controller with the Manager
func (r *BatchMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize recorder
	r.Recorder = mgr.GetEventRecorderFor("batchmigration-controller")

	// Generate worker ID from hostname and process ID
	hostname, _ := os.Hostname()
	r.WorkerID = fmt.Sprintf("%s-%d", hostname, os.Getpid())

	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.BatchMigration{}).
		Complete(r)
}
