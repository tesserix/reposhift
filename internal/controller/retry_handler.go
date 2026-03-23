package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// RetryHandler manages retry logic with exponential backoff for migration failures
type RetryHandler struct {
	logger      logr.Logger
	retryConfig RetryConfig
}

// NewRetryHandler creates a new retry handler
func NewRetryHandler(logger logr.Logger) *RetryHandler {
	return &RetryHandler{
		logger:      logger,
		retryConfig: DefaultRetryConfig(),
	}
}

// NewRetryHandlerWithConfig creates a retry handler with custom configuration
func NewRetryHandlerWithConfig(logger logr.Logger, config RetryConfig) *RetryHandler {
	return &RetryHandler{
		logger:      logger,
		retryConfig: config,
	}
}

// ErrorClassification contains error analysis results
type ErrorClassification struct {
	Type           ErrorType
	Message        string
	SuggestedDelay time.Duration
	Retryable      bool
}

// ClassifyError analyzes an error and determines retry strategy
func (h *RetryHandler) ClassifyError(err error) ErrorClassification {
	if err == nil {
		return ErrorClassification{
			Type:      ErrorTypeTerminal,
			Retryable: false,
		}
	}

	// Check if already a classified error
	if classErr, ok := err.(*ClassifiedError); ok {
		return ErrorClassification{
			Type:           classErr.Type,
			Message:        classErr.Message,
			SuggestedDelay: classErr.RetryAfter,
			Retryable:      classErr.Type == ErrorTypeRetryable,
		}
	}

	errMsg := err.Error()

	// Transient network errors
	transientPatterns := []string{
		"connection reset",
		"connection refused",
		"timeout",
		"temporary failure",
		"network is unreachable",
		"TLS handshake timeout",
		"EOF",
		"i/o timeout",
		"broken pipe",
		"connection timed out",
		"dial tcp",
		"no such host",
	}

	for _, pattern := range transientPatterns {
		if containsIgnoreCase(errMsg, pattern) {
			return ErrorClassification{
				Type:           ErrorTypeRetryable,
				Message:        fmt.Sprintf("Transient network error: %s", pattern),
				SuggestedDelay: 30 * time.Second,
				Retryable:      true,
			}
		}
	}

	// Rate limiting errors
	rateLimitPatterns := []string{
		"rate limit",
		"429",
		"too many requests",
		"abuse detection",
		"secondary rate limit",
		"retry after",
		"rate_limit_exceeded",
	}

	for _, pattern := range rateLimitPatterns {
		if containsIgnoreCase(errMsg, pattern) {
			return ErrorClassification{
				Type:           ErrorTypeRetryable,
				Message:        fmt.Sprintf("Rate limit encountered: %s", pattern),
				SuggestedDelay: 5 * time.Minute,
				Retryable:      true,
			}
		}
	}

	// Validation errors (terminal)
	validationPatterns := []string{
		"invalid",
		"malformed",
		"bad request",
		"400",
		"403",
		"forbidden",
		"unauthorized",
		"401",
		"not found",
		"404",
		"conflict",
		"409",
		"already exists",
	}

	for _, pattern := range validationPatterns {
		if containsIgnoreCase(errMsg, pattern) {
			return ErrorClassification{
				Type:           ErrorTypeValidation,
				Message:        fmt.Sprintf("Validation error: %s", pattern),
				SuggestedDelay: 0,
				Retryable:      false,
			}
		}
	}

	// Server errors (retryable)
	serverErrorPatterns := []string{
		"500",
		"502",
		"503",
		"504",
		"internal server error",
		"bad gateway",
		"service unavailable",
		"gateway timeout",
	}

	for _, pattern := range serverErrorPatterns {
		if containsIgnoreCase(errMsg, pattern) {
			return ErrorClassification{
				Type:           ErrorTypeRetryable,
				Message:        fmt.Sprintf("Server error: %s", pattern),
				SuggestedDelay: 2 * time.Minute,
				Retryable:      true,
			}
		}
	}

	// Out of memory (terminal)
	if containsIgnoreCase(errMsg, "out of memory") || containsIgnoreCase(errMsg, "OOMKilled") {
		return ErrorClassification{
			Type:           ErrorTypeTerminal,
			Message:        "Out of memory - increase pod resources (see values-production.yaml)",
			SuggestedDelay: 0,
			Retryable:      false,
		}
	}

	// Default: treat as retryable if not classified
	return ErrorClassification{
		Type:           ErrorTypeRetryable,
		Message:        "Unclassified error, treating as transient",
		SuggestedDelay: 1 * time.Minute,
		Retryable:      true,
	}
}

// ShouldRetry determines if a migration resource should be retried
func (h *RetryHandler) ShouldRetry(
	migration *migrationv1.AdoToGitMigration,
	resourceIndex int,
	err error,
) (shouldRetry bool, requeueAfter time.Duration) {

	if resourceIndex < 0 || resourceIndex >= len(migration.Status.ResourceStatuses) {
		h.logger.Error(fmt.Errorf("invalid resource index"), "index", resourceIndex)
		return false, 0
	}

	resourceStatus := &migration.Status.ResourceStatuses[resourceIndex]

	// Get max retry attempts from settings
	maxRetries := migration.Spec.Settings.RetryAttempts
	if maxRetries == 0 {
		maxRetries = h.retryConfig.MaxAttempts
	}

	// Check if we've exhausted retries
	retryCount := resourceStatus.Progress
	if retryCount >= maxRetries {
		h.logger.Info("Retry attempts exhausted",
			"resource", resourceStatus.TargetName,
			"attempts", retryCount,
			"maxRetries", maxRetries)
		return false, 0
	}

	// Classify the error
	classification := h.ClassifyError(err)

	if !classification.Retryable {
		h.logger.Info("Error is not retryable",
			"resource", resourceStatus.TargetName,
			"errorType", classification.Type,
			"message", classification.Message)
		return false, 0
	}

	// Calculate backoff using existing infrastructure
	backoff := CalculateBackoff(retryCount, h.retryConfig)

	// Use suggested delay if it's longer than calculated backoff (e.g., rate limits)
	if classification.SuggestedDelay > backoff {
		backoff = classification.SuggestedDelay
	}

	h.logger.Info("Will retry resource migration",
		"resource", resourceStatus.TargetName,
		"attempt", retryCount+1,
		"maxRetries", maxRetries,
		"backoff", backoff,
		"errorType", classification.Type,
		"errorMessage", classification.Message)

	return true, backoff
}

// HandleResourceFailure handles resource processing failures with retry logic
func (h *RetryHandler) HandleResourceFailure(
	ctx context.Context,
	r *AdoToGitMigrationReconciler,
	migration *migrationv1.AdoToGitMigration,
	resource migrationv1.MigrationResource,
	resourceIndex int,
	processingErr error,
) (ctrl.Result, error) {

	// Determine if we should retry
	shouldRetry, requeueAfter := h.ShouldRetry(migration, resourceIndex, processingErr)

	if !shouldRetry {
		// Terminal failure - mark migration as failed
		return h.handleTerminalFailure(ctx, r, migration, resource, resourceIndex, processingErr)
	}

	// Retryable failure - increment retry count and requeue
	return h.handleRetryableFailure(ctx, r, migration, resource, resourceIndex, processingErr, requeueAfter)
}

// handleTerminalFailure handles non-retryable failures
func (h *RetryHandler) handleTerminalFailure(
	ctx context.Context,
	r *AdoToGitMigrationReconciler,
	migration *migrationv1.AdoToGitMigration,
	resource migrationv1.MigrationResource,
	resourceIndex int,
	processingErr error,
) (ctrl.Result, error) {

	// Classify error for better error message
	classification := h.ClassifyError(processingErr)

	// Update resource status
	if resourceIndex >= 0 && resourceIndex < len(migration.Status.ResourceStatuses) {
		resourceStatus := &migration.Status.ResourceStatuses[resourceIndex]
		resourceStatus.Status = migrationv1.ResourceStatusFailed
		resourceStatus.ErrorMessage = fmt.Sprintf("%s: %v", classification.Message, processingErr)
		resourceStatus.Error = resourceStatus.ErrorMessage
		now := metav1.Now()
		resourceStatus.CompletionTime = &now
	}

	// Update migration progress counters
	migration.Status.Progress.Failed++
	migration.Status.Progress.Processing--

	// Check if all resources are processed
	totalProcessed := migration.Status.Progress.Completed + migration.Status.Progress.Failed
	if totalProcessed >= migration.Status.Progress.Total {
		// All resources processed, but some failed
		migration.Status.Phase = migrationv1.MigrationPhaseFailed
	}

	// Update migration error message
	migration.Status.ErrorMessage = fmt.Sprintf(
		"Resource %s failed permanently after %d attempts: %s",
		resource.TargetName,
		migration.Status.ResourceStatuses[resourceIndex].Progress,
		classification.Message,
	)

	// Record event
	r.Recorder.Event(migration, corev1.EventTypeWarning, "MigrationFailed",
		fmt.Sprintf("Resource %s: %s", resource.TargetName, classification.Message))

	// Send WebSocket update
	r.sendWebSocketUpdate(migration, fmt.Sprintf("Resource %s failed: %s", resource.TargetName, classification.Message))

	// Update status
	if err := r.Status().Update(ctx, migration); err != nil {
		h.logger.Error(err, "Failed to update migration status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// handleRetryableFailure handles retryable failures with backoff
func (h *RetryHandler) handleRetryableFailure(
	ctx context.Context,
	r *AdoToGitMigrationReconciler,
	migration *migrationv1.AdoToGitMigration,
	resource migrationv1.MigrationResource,
	resourceIndex int,
	processingErr error,
	requeueAfter time.Duration,
) (ctrl.Result, error) {

	classification := h.ClassifyError(processingErr)

	// Increment retry count (stored in Progress field)
	if resourceIndex >= 0 && resourceIndex < len(migration.Status.ResourceStatuses) {
		resourceStatus := &migration.Status.ResourceStatuses[resourceIndex]
		resourceStatus.Progress++                                 // Increment retry counter
		resourceStatus.Status = migrationv1.ResourceStatusPending // Reset to pending for retry
		resourceStatus.ErrorMessage = fmt.Sprintf(
			"Attempt %d failed: %s (retrying in %v)",
			resourceStatus.Progress,
			classification.Message,
			requeueAfter,
		)
		resourceStatus.Error = resourceStatus.ErrorMessage
	}

	// Update current step to show retry
	maxRetries := migration.Spec.Settings.RetryAttempts
	if maxRetries == 0 {
		maxRetries = h.retryConfig.MaxAttempts
	}

	migration.Status.Progress.CurrentStep = fmt.Sprintf(
		"Retrying %s (attempt %d/%d) in %v",
		resource.TargetName,
		migration.Status.ResourceStatuses[resourceIndex].Progress+1,
		maxRetries,
		requeueAfter,
	)

	// Record event
	r.Recorder.Event(migration, corev1.EventTypeWarning, "RetryScheduled",
		fmt.Sprintf("Resource %s: Retry attempt %d scheduled in %v due to: %s",
			resource.TargetName,
			migration.Status.ResourceStatuses[resourceIndex].Progress+1,
			requeueAfter,
			classification.Message))

	// Send WebSocket update
	r.sendWebSocketUpdate(migration, migration.Status.Progress.CurrentStep)

	// Update status
	if err := r.Status().Update(ctx, migration); err != nil {
		h.logger.Error(err, "Failed to update migration status for retry")
		return ctrl.Result{}, err
	}

	// Requeue with backoff
	h.logger.Info("Requeuing migration for retry",
		"resource", resource.TargetName,
		"requeueAfter", requeueAfter,
		"attempt", migration.Status.ResourceStatuses[resourceIndex].Progress+1)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// containsIgnoreCase checks if a string contains a substring (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
