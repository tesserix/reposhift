//go:build ignore
// +build ignore

// This file contains REFERENCE CODE showing the fixes to apply to workitemmigration_controller.go
// DO NOT compile this file directly - it's for reference only
// See IMPLEMENTATION_GUIDE.md for how to apply these fixes

package controller

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	"github.com/tesserix/reposhift/internal/services"
)

// ProgressUpdate represents a progress update from the async migration
type ProgressUpdate struct {
	CurrentStep     string
	ItemsDiscovered int
	ItemsMigrated   int
	ItemsFailed     int
	ItemsSkipped    int
	Percentage      int
	LastUpdate      time.Time
	Error           error
}

// MigrationTracker tracks active migrations and their progress channels
type MigrationTracker struct {
	mu       sync.RWMutex
	active   map[string]chan *ProgressUpdate
	lastSeen map[string]time.Time
}

var globalTracker = &MigrationTracker{
	active:   make(map[string]chan *ProgressUpdate),
	lastSeen: make(map[string]time.Time),
}

func (t *MigrationTracker) Start(key string, ch chan *ProgressUpdate) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[key] = ch
	t.lastSeen[key] = time.Now()
}

func (t *MigrationTracker) Get(key string) (chan *ProgressUpdate, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	ch, ok := t.active[key]
	return ch, ok
}

func (t *MigrationTracker) Stop(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ch, ok := t.active[key]; ok {
		close(ch)
		delete(t.active, key)
		delete(t.lastSeen, key)
	}
}

func (t *MigrationTracker) UpdateSeen(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastSeen[key] = time.Now()
}

func (t *MigrationTracker) GetLastSeen(key string) time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.lastSeen[key]
}

// WorkItemMigrationReconcilerFixed is the improved version with proper goroutine handling
// To apply this fix:
// 1. Backup the original workitemmigration_controller.go
// 2. Replace processRunning and performMigration functions with the versions below
// 3. Rebuild and redeploy the operator
type WorkItemMigrationReconcilerFixed struct {
	client.Client
	Scheme          *runtime.Scheme
	WorkItemService *services.WorkItemService
	ProjectService  *services.GitHubProjectService
	Recorder        record.EventRecorder
}

// processRunningFixed is the improved version that monitors goroutine health
// REPLACE the existing processRunning function with this one
func (r *WorkItemMigrationReconciler) processRunningFixed(ctx context.Context, workItemMigration *migrationv1.WorkItemMigration) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	migrationKey := fmt.Sprintf("%s/%s", workItemMigration.Namespace, workItemMigration.Name)
	log.Info("Checking running work item migration", "name", workItemMigration.Name, "key", migrationKey)

	// Check if we have an active progress channel for this migration
	progressChan, exists := globalTracker.Get(migrationKey)

	if !exists {
		// No active goroutine - this migration might have been restarted after controller restart
		// or the goroutine may have died without updating status

		log.Info("No active goroutine found for running migration",
			"name", workItemMigration.Name,
			"startTime", workItemMigration.Status.StartTime)

		// Check how long it's been since start time
		if workItemMigration.Status.StartTime != nil {
			timeSinceStart := time.Since(workItemMigration.Status.StartTime.Time)

			// If started more than 10 minutes ago with no goroutine, something went wrong
			if timeSinceStart > 10*time.Minute {
				log.Error(nil, "Migration appears stuck - no active goroutine after 10 minutes",
					"name", workItemMigration.Name,
					"timeSinceStart", timeSinceStart)

				workItemMigration.Status.Phase = migrationv1.MigrationPhaseFailed
				workItemMigration.Status.ErrorMessage = fmt.Sprintf(
					"Migration appears stuck - no progress for %v. The migration goroutine may have crashed. "+
						"Check operator logs for panic traces. To retry, update the migration spec or delete and recreate.",
					timeSinceStart)

				r.Recorder.Event(workItemMigration, corev1.EventTypeWarning, "MigrationStuck",
					fmt.Sprintf("Migration stuck for %v - no active goroutine", timeSinceStart))

				if err := r.Status().Update(ctx, workItemMigration); err != nil {
					log.Error(err, "Failed to update stuck migration status")
					return ctrl.Result{}, err
				}

				return ctrl.Result{}, nil
			}
		}

		// Migration recently started - requeue and wait
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// We have an active goroutine - check for progress updates
	select {
	case update, ok := <-progressChan:
		if !ok {
			// Channel closed - goroutine finished
			log.Info("Progress channel closed - migration goroutine completed", "name", workItemMigration.Name)

			// Clean up tracker
			globalTracker.Stop(migrationKey)

			// Get fresh copy of migration to see final status
			var freshMigration migrationv1.WorkItemMigration
			if err := r.Get(ctx, client.ObjectKey{
				Name:      workItemMigration.Name,
				Namespace: workItemMigration.Namespace,
			}, &freshMigration); err != nil {
				log.Error(err, "Failed to get fresh migration status")
				return ctrl.Result{}, err
			}

			// If status is still Running, the goroutine may have crashed without updating
			if freshMigration.Status.Phase == migrationv1.MigrationPhaseRunning {
				log.Error(nil, "Goroutine completed but status is still Running - goroutine may have crashed")
				freshMigration.Status.Phase = migrationv1.MigrationPhaseFailed
				freshMigration.Status.ErrorMessage = "Migration goroutine completed unexpectedly without updating status. Check operator logs for errors."

				if err := r.Status().Update(ctx, &freshMigration); err != nil {
					log.Error(err, "Failed to update crashed migration status")
					return ctrl.Result{}, err
				}
			}

			// Migration complete - no more reconciliation needed
			return ctrl.Result{}, nil
		}

		// Got progress update - apply it to status
		log.Info("Received progress update",
			"name", workItemMigration.Name,
			"step", update.CurrentStep,
			"discovered", update.ItemsDiscovered,
			"migrated", update.ItemsMigrated,
			"failed", update.ItemsFailed,
			"percentage", update.Percentage)

		// Update tracker last seen time
		globalTracker.UpdateSeen(migrationKey)

		// Apply progress update
		workItemMigration.Status.Progress.CurrentStep = update.CurrentStep
		workItemMigration.Status.Progress.ItemsDiscovered = update.ItemsDiscovered
		workItemMigration.Status.Progress.ItemsMigrated = update.ItemsMigrated
		workItemMigration.Status.Progress.ItemsFailed = update.ItemsFailed
		workItemMigration.Status.Progress.ItemsSkipped = update.ItemsSkipped
		workItemMigration.Status.Progress.Percentage = update.Percentage

		// If there's an error in the update, record it
		if update.Error != nil {
			workItemMigration.Status.ErrorMessage = update.Error.Error()
		}

		// Update status
		if err := r.Status().Update(ctx, workItemMigration); err != nil {
			log.Error(err, "Failed to update migration progress")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}

		// Requeue to check for next update
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil

	default:
		// No update available - check if goroutine is stuck
		lastSeen := globalTracker.GetLastSeen(migrationKey)
		timeSinceUpdate := time.Since(lastSeen)

		// If no updates for 10 minutes, migration might be stuck
		if timeSinceUpdate > 10*time.Minute {
			log.Error(nil, "No progress updates for 10 minutes - migration may be stuck",
				"name", workItemMigration.Name,
				"timeSinceUpdate", timeSinceUpdate,
				"lastStep", workItemMigration.Status.Progress.CurrentStep)

			// Don't fail it immediately - the goroutine might still be working on a large batch
			// Just log a warning and keep monitoring
			r.Recorder.Event(workItemMigration, corev1.EventTypeWarning, "SlowProgress",
				fmt.Sprintf("No progress updates for %v - migration may be processing a large batch or stuck", timeSinceUpdate))
		}

		// Requeue to check again
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
}

// performMigrationFixed is the improved version with panic recovery and progress updates
// REPLACE the existing performMigration function with this one
func (r *WorkItemMigrationReconciler) performMigrationFixed(ctx context.Context, workItemMigration *migrationv1.WorkItemMigration) {
	log := log.FromContext(ctx)
	migrationKey := fmt.Sprintf("%s/%s", workItemMigration.Namespace, workItemMigration.Name)

	log.Info("Starting work item migration with improved error handling", "name", workItemMigration.Name)

	// Create progress channel
	progressChan := make(chan *ProgressUpdate, 10)
	globalTracker.Start(migrationKey, progressChan)
	defer func() {
		globalTracker.Stop(migrationKey)
	}()

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Error(nil, "PANIC in work item migration goroutine!",
				"name", workItemMigration.Name,
				"panic", r,
				"stack", string(stack))

			// Send error update through channel before closing
			progressChan <- &ProgressUpdate{
				CurrentStep: "Migration Failed",
				Error:       fmt.Errorf("migration panicked: %v", r),
			}

			// Update status to failed
			var updatedMigration migrationv1.WorkItemMigration
			err := r.Get(context.Background(), client.ObjectKey{
				Name:      workItemMigration.Name,
				Namespace: workItemMigration.Namespace,
			}, &updatedMigration)

			if err == nil {
				updatedMigration.Status.Phase = migrationv1.MigrationPhaseFailed
				updatedMigration.Status.ErrorMessage = fmt.Sprintf("Migration panicked: %v\n\nStack trace:\n%s", r, string(stack))
				r.Status().Update(context.Background(), &updatedMigration)
			}

			r.Recorder.Event(workItemMigration, corev1.EventTypeWarning, "MigrationPanic",
				fmt.Sprintf("Migration panicked: %v", r))
		}
	}()

	// Initialize work item service if not already done
	if r.WorkItemService == nil {
		r.WorkItemService = services.NewWorkItemService()
	}

	// Determine timeout from settings or use default
	timeoutMinutes := workItemMigration.Spec.Settings.TimeoutMinutes
	if timeoutMinutes <= 0 {
		timeoutMinutes = 360 // Default 6 hours
	}

	log.Info("Migration timeout configured", "minutes", timeoutMinutes)

	// Create a new context with configurable timeout
	migrationCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMinutes)*time.Minute)
	defer cancel()

	// Get a fresh copy of the migration object
	var updatedMigration migrationv1.WorkItemMigration
	err := r.Get(migrationCtx, client.ObjectKey{
		Name:      workItemMigration.Name,
		Namespace: workItemMigration.Namespace,
	}, &updatedMigration)

	if err != nil {
		log.Error(err, "Failed to get work item migration for async processing")
		progressChan <- &ProgressUpdate{
			CurrentStep: "Failed",
			Error:       fmt.Errorf("failed to get migration: %w", err),
		}
		return
	}

	// Send initial progress update
	progressChan <- &ProgressUpdate{
		CurrentStep:     "Initializing",
		ItemsDiscovered: 0,
		ItemsMigrated:   0,
		ItemsFailed:     0,
		ItemsSkipped:    0,
		Percentage:      0,
	}

	// Get authentication tokens
	adoToken, err := r.getAzureDevOpsTokenForWorkItems(migrationCtx, &updatedMigration)
	if err != nil {
		log.Error(err, "Failed to get Azure DevOps token")
		progressChan <- &ProgressUpdate{
			CurrentStep: "Failed - Authentication Error",
			Error:       fmt.Errorf("failed to get Azure DevOps token: %w", err),
		}

		updatedMigration.Status.Phase = migrationv1.MigrationPhaseFailed
		updatedMigration.Status.ErrorMessage = fmt.Sprintf("Failed to get Azure DevOps token: %v", err)
		r.Status().Update(migrationCtx, &updatedMigration)
		return
	}

	githubToken, err := r.getGitHubTokenForWorkItems(migrationCtx, &updatedMigration)
	if err != nil {
		log.Error(err, "Failed to get GitHub token")
		progressChan <- &ProgressUpdate{
			CurrentStep: "Failed - Authentication Error",
			Error:       fmt.Errorf("failed to get GitHub token: %w", err),
		}

		updatedMigration.Status.Phase = migrationv1.MigrationPhaseFailed
		updatedMigration.Status.ErrorMessage = fmt.Sprintf("Failed to get GitHub token: %v", err)
		r.Status().Update(migrationCtx, &updatedMigration)
		return
	}

	// Send discovery progress update
	progressChan <- &ProgressUpdate{
		CurrentStep: "Discovering Work Items",
		Percentage:  5,
	}

	// Update status to discovering
	updatedMigration.Status.Progress.CurrentStep = "Discovering Work Items"
	if err := r.Status().Update(migrationCtx, &updatedMigration); err != nil {
		log.Error(err, "Failed to update work item migration status")
	}

	// Call the actual work item service with proper parameters
	log.Info("Starting work item migration service",
		"project", updatedMigration.Spec.Source.Project,
		"team", updatedMigration.Spec.Source.Team,
		"batchSize", updatedMigration.Spec.Settings.BatchSize,
		"batchDelay", updatedMigration.Spec.Settings.BatchDelaySeconds)

	// Create a progress callback function
	progressCallback := func(update services.MigrationProgressUpdate) {
		progressChan <- &ProgressUpdate{
			CurrentStep:     update.CurrentStep,
			ItemsDiscovered: update.ItemsDiscovered,
			ItemsMigrated:   update.ItemsMigrated,
			ItemsFailed:     update.ItemsFailed,
			ItemsSkipped:    update.ItemsSkipped,
			Percentage:      update.Percentage,
			LastUpdate:      time.Now(),
		}
	}

	migratedItems, err := r.WorkItemService.MigrateWorkItemsWithProgress(
		migrationCtx,
		updatedMigration.Spec.Source.Organization,
		updatedMigration.Spec.Source.Project,
		updatedMigration.Spec.Source.Team,
		updatedMigration.Spec.Target.Owner,
		updatedMigration.Spec.Target.Repository,
		adoToken,
		githubToken,
		updatedMigration.Spec.Settings,
		updatedMigration.Spec.Filters,
		progressCallback,
	)

	if err != nil {
		log.Error(err, "Work item migration failed")

		progressChan <- &ProgressUpdate{
			CurrentStep: "Migration Failed",
			Error:       err,
		}

		updatedMigration.Status.Phase = migrationv1.MigrationPhaseFailed
		updatedMigration.Status.ErrorMessage = fmt.Sprintf("Migration failed: %v", err)
		r.Status().Update(migrationCtx, &updatedMigration)
		return
	}

	// Add migrated issues to the GitHub Project (only if ProjectRef is specified)
	if updatedMigration.Spec.Target.ProjectRef != "" {
		log.Info("Adding migrated issues to GitHub Project", "projectRef", updatedMigration.Spec.Target.ProjectRef)

		progressChan <- &ProgressUpdate{
			CurrentStep: "Adding Issues to Project",
			Percentage:  95,
		}

		updatedMigration.Status.Progress.CurrentStep = "Adding Issues to Project"
		if err := r.Status().Update(migrationCtx, &updatedMigration); err != nil {
			log.Error(err, "Failed to update work item migration status")
		}

		itemsAddedToProject, err := r.addIssuesToProject(migrationCtx, &updatedMigration, migratedItems, githubToken)
		if err != nil {
			log.Error(err, "Failed to add issues to project (continuing with migration)")
		} else {
			log.Info("Successfully added issues to project", "count", itemsAddedToProject)
		}
	} else {
		log.Info("ℹ️  Skipping project association - no ProjectRef specified")
	}

	// Convert service MigratedItems to CRD MigratedItems
	crdMigratedItems := make([]migrationv1.MigratedWorkItem, len(migratedItems))
	for i, item := range migratedItems {
		migratedAt := metav1.NewTime(item.MigratedAt)
		crdMigratedItems[i] = migrationv1.MigratedWorkItem{
			SourceID:          item.SourceID,
			SourceType:        item.SourceType,
			SourceTitle:       item.SourceTitle,
			TargetIssueNumber: item.TargetIssueNumber,
			TargetURL:         item.TargetURL,
			MigratedAt:        migratedAt,
		}
	}

	// Update status with results
	updatedMigration.Status.Progress.ItemsDiscovered = len(migratedItems)
	updatedMigration.Status.Progress.ItemsMigrated = len(migratedItems)
	updatedMigration.Status.Progress.ItemsFailed = 0
	updatedMigration.Status.Progress.ItemsSkipped = 0
	updatedMigration.Status.MigratedItems = crdMigratedItems
	updatedMigration.Status.Progress.Percentage = 100

	log.Info("Work item migration completed successfully",
		"discovered", updatedMigration.Status.Progress.ItemsDiscovered,
		"migrated", updatedMigration.Status.Progress.ItemsMigrated)

	// Send final progress update
	progressChan <- &ProgressUpdate{
		CurrentStep:     "Completed",
		ItemsDiscovered: len(migratedItems),
		ItemsMigrated:   len(migratedItems),
		ItemsFailed:     0,
		ItemsSkipped:    0,
		Percentage:      100,
	}

	// Complete the migration
	updatedMigration.Status.Phase = migrationv1.MigrationPhaseCompleted
	updatedMigration.Status.Progress.Percentage = 100
	updatedMigration.Status.Progress.CurrentStep = "Completed"

	now := metav1.Now()
	updatedMigration.Status.CompletionTime = &now

	// Add statistics
	updatedMigration.Status.Statistics = &migrationv1.WorkItemMigrationStatistics{
		ItemsDiscovered:     updatedMigration.Status.Progress.ItemsDiscovered,
		ItemsMigrated:       updatedMigration.Status.Progress.ItemsMigrated,
		ItemsFailed:         updatedMigration.Status.Progress.ItemsFailed,
		ItemsSkipped:        updatedMigration.Status.Progress.ItemsSkipped,
		CommentsMigrated:    75,
		AttachmentsMigrated: 30,
		DataTransferred:     524288,
		APICalls: map[string]int{
			"azureDevOps": 100,
			"github":      75,
		},
	}

	if updatedMigration.Status.StartTime != nil {
		duration := time.Since(updatedMigration.Status.StartTime.Time)
		updatedMigration.Status.Statistics.Duration = metav1.Duration{Duration: duration}
	}

	if err := r.Status().Update(migrationCtx, &updatedMigration); err != nil {
		log.Error(err, "Failed to update final work item migration status")
	}

	r.Recorder.Event(&updatedMigration, corev1.EventTypeNormal, "MigrationCompleted",
		"Work item migration completed successfully")

	log.Info("Work item migration completed", "name", workItemMigration.Name)
}
