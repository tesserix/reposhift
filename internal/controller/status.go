package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusUpdater manages status updates with batching and conflict resolution
type StatusUpdater struct {
	client client.Client
	log    logr.Logger
}

// NewStatusUpdater creates a new status updater
func NewStatusUpdater(client client.Client, log logr.Logger) *StatusUpdater {
	return &StatusUpdater{
		client: client,
		log:    log,
	}
}

// UpdateStatusWithRetry updates the status with automatic retry on conflicts
func (su *StatusUpdater) UpdateStatusWithRetry(ctx context.Context, obj client.Object, maxRetries int) error {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff between retries
			backoff := time.Duration(attempt*attempt) * 100 * time.Millisecond
			time.Sleep(backoff)

			// Refresh the object to get the latest resource version
			if err := su.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
				if errors.IsNotFound(err) {
					// Object was deleted, nothing to update
					return nil
				}
				return err
			}
		}

		// Attempt status update
		if err := su.client.Status().Update(ctx, obj); err != nil {
			if errors.IsConflict(err) {
				// Conflict - retry with fresh object
				lastErr = err
				su.log.V(1).Info("Status update conflict, retrying",
					"attempt", attempt+1,
					"maxRetries", maxRetries,
					"object", client.ObjectKeyFromObject(obj))
				continue
			}

			// Other error - return immediately
			return err
		}

		// Success
		if attempt > 0 {
			su.log.V(1).Info("Status update succeeded after retry",
				"attempts", attempt+1,
				"object", client.ObjectKeyFromObject(obj))
		}
		return nil
	}

	// Max retries exceeded
	return lastErr
}

// UpdateStatusIfChanged updates status only if it has changed
// This helps reduce unnecessary API calls
func (su *StatusUpdater) UpdateStatusIfChanged(ctx context.Context, oldObj, newObj client.Object) error {
	// This is a simplified check - in production, you might want to do deep equality
	// For now, we'll always update if called
	return su.UpdateStatusWithRetry(ctx, newObj, 3)
}

// SafeStatusUpdate performs a status update and logs errors instead of returning them
// Use this when status update failure shouldn't block the reconciliation
func (su *StatusUpdater) SafeStatusUpdate(ctx context.Context, obj client.Object) {
	if err := su.UpdateStatusWithRetry(ctx, obj, 3); err != nil {
		su.log.Error(err, "Failed to update status (non-blocking)",
			"object", client.ObjectKeyFromObject(obj))
	}
}

// PatchStatus performs a strategic merge patch on the status subresource
// This is more efficient than a full update for large objects
func (su *StatusUpdater) PatchStatus(ctx context.Context, obj client.Object, patch client.Patch) error {
	return su.client.Status().Patch(ctx, obj, patch)
}

// StatusBatcher batches multiple status updates to minimize API calls
type StatusBatcher struct {
	updater        *StatusUpdater
	pendingUpdates map[string]client.Object
	log            logr.Logger
}

// NewStatusBatcher creates a new status batcher
func NewStatusBatcher(updater *StatusUpdater, log logr.Logger) *StatusBatcher {
	return &StatusBatcher{
		updater:        updater,
		pendingUpdates: make(map[string]client.Object),
		log:            log,
	}
}

// Stage stages a status update without executing it immediately
func (sb *StatusBatcher) Stage(obj client.Object) {
	key := client.ObjectKeyFromObject(obj).String()
	sb.pendingUpdates[key] = obj
}

// Flush executes all pending status updates
func (sb *StatusBatcher) Flush(ctx context.Context) error {
	if len(sb.pendingUpdates) == 0 {
		return nil
	}

	sb.log.V(1).Info("Flushing batched status updates",
		"count", len(sb.pendingUpdates))

	var lastErr error
	successCount := 0

	for key, obj := range sb.pendingUpdates {
		if err := sb.updater.UpdateStatusWithRetry(ctx, obj, 3); err != nil {
			sb.log.Error(err, "Failed to update status in batch",
				"object", key)
			lastErr = err
		} else {
			successCount++
		}
	}

	// Clear pending updates
	sb.pendingUpdates = make(map[string]client.Object)

	sb.log.V(1).Info("Completed batched status updates",
		"successful", successCount,
		"failed", len(sb.pendingUpdates)-successCount)

	return lastErr
}

// HasPending returns true if there are pending updates
func (sb *StatusBatcher) HasPending() bool {
	return len(sb.pendingUpdates) > 0
}

// Clear removes all pending updates without executing them
func (sb *StatusBatcher) Clear() {
	sb.pendingUpdates = make(map[string]client.Object)
}
