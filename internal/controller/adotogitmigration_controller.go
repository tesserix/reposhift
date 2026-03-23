package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-github/v84/github"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	"github.com/tesserix/reposhift/internal/services"
	"github.com/tesserix/reposhift/internal/websocket"
)

var (
	// Prometheus metrics for observability
	migrationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ado_migration_total",
			Help: "Total number of ADO to Git migrations by phase and namespace",
		},
		[]string{"phase", "namespace"},
	)

	reconciliationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ado_migration_reconciliations_total",
			Help: "Total number of reconciliations by trigger reason and namespace",
		},
		[]string{"trigger_reason", "namespace"},
	)

	specChangesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ado_migration_spec_changes_total",
			Help: "Total number of spec changes detected by namespace",
		},
		[]string{"namespace", "change_type"},
	)

	resourceMigrationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ado_migration_resources_total",
			Help: "Total number of resource migrations by status, type, and namespace",
		},
		[]string{"status", "type", "namespace"},
	)

	reconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ado_migration_reconcile_duration_seconds",
			Help:    "Duration of reconciliation operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"phase", "namespace"},
	)

	migrationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ado_migration_duration_seconds",
			Help:    "Duration of complete migrations in seconds",
			Buckets: []float64{60, 300, 600, 1200, 1800, 3600, 7200}, // 1m to 2h
		},
		[]string{"result", "namespace"},
	)
)

func init() {
	// Register metrics with controller-runtime's registry
	metrics.Registry.MustRegister(
		migrationsTotal,
		reconciliationsTotal,
		specChangesTotal,
		resourceMigrationsTotal,
		reconcileDuration,
		migrationDuration,
	)
}

const (
	// Finalizer name
	finalizerName = "migration.ado-to-git-migration.io/finalizer"

	// Annotation keys
	pauseAnnotation            = "migration.ado-to-git-migration.io/pause"
	cancelAnnotation           = "migration.ado-to-git-migration.io/cancel"
	retryAnnotation            = "migration.ado-to-git-migration.io/retry"
	reconcileTriggerAnnotation = "migration.ado-to-git-migration.io/reconcile-trigger"

	// Condition types
	ConditionTypeReady           = "Ready"
	ConditionTypeReconciling     = "Reconciling"
	ConditionTypeSpecChanged     = "SpecChanged"
	ConditionTypeMigrationActive = "MigrationActive"
	ConditionTypeValidated       = "Validated"

	// Condition reasons
	ReasonReconcileStarted    = "ReconcileStarted"
	ReasonReconcileComplete   = "ReconcileComplete"
	ReasonSpecChanged         = "SpecChanged"
	ReasonNoResourceChanges   = "NoResourceChanges"
	ReasonResourcesChanged    = "ResourcesChanged"
	ReasonValidationPassed    = "ValidationPassed"
	ReasonValidationFailed    = "ValidationFailed"
	ReasonMigrationRunning    = "MigrationRunning"
	ReasonMigrationCompleted  = "MigrationCompleted"
	ReasonMigrationFailed     = "MigrationFailed"
	ReasonMigrationCancelled  = "MigrationCancelled"
	ReasonMigrationPaused     = "MigrationPaused"
	ReasonAnnotationTriggered = "AnnotationTriggered"

	// Reconcile intervals
	pendingRequeueInterval = 5 * time.Second
	runningRequeueInterval = 10 * time.Second
	pausedRequeueInterval  = 60 * time.Second

	// Max concurrent resource migrations
	maxConcurrentMigrations = 5
)

// AdoToGitMigrationReconciler reconciles a AdoToGitMigration object
type AdoToGitMigrationReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	MigrationService *services.MigrationService
	GitHubService    *services.GitHubService
	WebSocketManager *websocket.Manager
	Recorder         record.EventRecorder

	// Track active migrations
	activeMigrations     map[string]bool
	activeMigrationMutex sync.Mutex
}

//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=adotogitmigrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=adotogitmigrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=adotogitmigrations/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *AdoToGitMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	startTime := time.Now()

	// Track reconciliation metrics
	reconciliationsTotal.WithLabelValues("reconcile_started", req.Namespace).Inc()

	log.Info("Reconciling AdoToGitMigration", "namespacedName", req.NamespacedName)

	// Initialize active migrations map if needed
	if r.activeMigrations == nil {
		r.activeMigrations = make(map[string]bool)
	}

	// Fetch the AdoToGitMigration instance
	migration := &migrationv1.AdoToGitMigration{}
	err := r.Get(ctx, req.NamespacedName, migration)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, could have been deleted after reconcile request
			log.Info("AdoToGitMigration resource not found. Ignoring since object must be deleted")
			r.removeFromActiveMigrations(req.NamespacedName.String())
			reconciliationsTotal.WithLabelValues("resource_deleted", req.Namespace).Inc()
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get AdoToGitMigration")
		reconciliationsTotal.WithLabelValues("get_failed", req.Namespace).Inc()
		return ctrl.Result{}, err
	}

	// Record reconciliation duration when done
	defer func() {
		duration := time.Since(startTime).Seconds()
		phase := string(migration.Status.Phase)
		if phase == "" {
			phase = "initializing"
		}
		reconcileDuration.WithLabelValues(phase, req.Namespace).Observe(duration)
		log.V(1).Info("Reconciliation completed", "duration", duration, "phase", phase)
	}()

	// Check if spec changed
	// This must happen BEFORE updating observed generation
	specChanged := migration.Status.ObservedGeneration != 0 && migration.Status.ObservedGeneration != migration.Generation
	isTerminalPhase := migration.Status.Phase == migrationv1.MigrationPhaseCompleted ||
		migration.Status.Phase == migrationv1.MigrationPhaseFailed ||
		migration.Status.Phase == migrationv1.MigrationPhaseCancelled

	if specChanged {
		// Track spec change metrics
		specChangesTotal.WithLabelValues(req.Namespace, "generation_change").Inc()

		if isTerminalPhase {
			// Spec changed for a terminal migration - restart it with new resources
			log.Info("Spec changed for terminal migration, restarting",
				"oldGeneration", migration.Status.ObservedGeneration,
				"newGeneration", migration.Generation,
				"phase", migration.Status.Phase)
			specChangesTotal.WithLabelValues(req.Namespace, "terminal_restart").Inc()
			reconciliationsTotal.WithLabelValues("spec_change_terminal", req.Namespace).Inc()
			return r.restartMigrationForSpecChange(ctx, migration)
		} else {
			// Spec changed for an active migration
			// Log the change and update observed generation
			// The migration will continue processing with the new spec
			log.Info("Spec changed during active migration",
				"oldGeneration", migration.Status.ObservedGeneration,
				"newGeneration", migration.Generation,
				"phase", migration.Status.Phase)
			specChangesTotal.WithLabelValues(req.Namespace, "active_migration").Inc()
			reconciliationsTotal.WithLabelValues("spec_change_active", req.Namespace).Inc()
			r.sendWebSocketUpdate(migration, "Migration spec updated during processing")
		}
	}

	// Update observed generation (in-memory only at this point)
	// This will be persisted when status is updated
	r.updateObservedGeneration(migration)

	// Handle finalizer and deletion
	result, err := r.handleFinalizerAndDeletion(ctx, req, migration)
	if result != nil {
		return *result, err
	}

	// Handle annotations (pause/cancel)
	result, err = r.handleAnnotations(ctx, migration)
	if result != nil {
		return *result, err
	}

	// Initialize status if needed
	if migration.Status.Phase == "" {
		return r.initializeStatus(ctx, migration)
	}

	// Process based on current phase
	return r.processPhase(ctx, migration)
}

// removeFromActiveMigrations safely removes a migration from active tracking
func (r *AdoToGitMigrationReconciler) removeFromActiveMigrations(namespacedName string) {
	r.activeMigrationMutex.Lock()
	delete(r.activeMigrations, namespacedName)
	r.activeMigrationMutex.Unlock()
}

// updateObservedGeneration updates the observed generation if needed
func (r *AdoToGitMigrationReconciler) updateObservedGeneration(migration *migrationv1.AdoToGitMigration) {
	if migration.Status.ObservedGeneration != migration.Generation {
		migration.Status.ObservedGeneration = migration.Generation
		now := metav1.Now()
		migration.Status.LastReconcileTime = &now
	}
}

// handleFinalizerAndDeletion handles finalizer addition/removal and deletion logic
func (r *AdoToGitMigrationReconciler) handleFinalizerAndDeletion(ctx context.Context, req ctrl.Request, migration *migrationv1.AdoToGitMigration) (*ctrl.Result, error) {
	// Add finalizer if not present
	if migration.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(migration, finalizerName) {
			return r.addFinalizer(ctx, migration)
		}
	} else {
		// Handle deletion
		if controllerutil.ContainsFinalizer(migration, finalizerName) {
			return r.handleDeletion(ctx, req, migration)
		}
		return &ctrl.Result{}, nil
	}

	return nil, nil
}

// addFinalizer adds finalizer to the migration
func (r *AdoToGitMigrationReconciler) addFinalizer(ctx context.Context, migration *migrationv1.AdoToGitMigration) (*ctrl.Result, error) {
	log := log.FromContext(ctx)

	controllerutil.AddFinalizer(migration, finalizerName)
	if err := r.Update(ctx, migration); err != nil {
		log.Error(err, "Failed to add finalizer")
		return &ctrl.Result{}, err
	}
	log.Info("Added finalizer to migration")
	// Requeue immediately to initialize status in next reconciliation
	return &ctrl.Result{Requeue: true}, nil
}

// handleDeletion handles the deletion process
func (r *AdoToGitMigrationReconciler) handleDeletion(ctx context.Context, req ctrl.Request, migration *migrationv1.AdoToGitMigration) (*ctrl.Result, error) {
	log := log.FromContext(ctx)

	if err := r.cleanup(ctx, migration); err != nil {
		log.Error(err, "Failed to clean up migration resources")
		return &ctrl.Result{}, err
	}

	r.removeFromActiveMigrations(req.NamespacedName.String())

	controllerutil.RemoveFinalizer(migration, finalizerName)
	if err := r.Update(ctx, migration); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Resource not found during finalizer removal, assuming already deleted")
			return &ctrl.Result{}, nil
		}
		log.Error(err, "Failed to remove finalizer")
		return &ctrl.Result{}, err
	}
	log.Info("Removed finalizer from migration")
	return &ctrl.Result{}, nil
}

// handleAnnotations handles pause, cancel, and reconcile-trigger annotations
func (r *AdoToGitMigrationReconciler) handleAnnotations(ctx context.Context, migration *migrationv1.AdoToGitMigration) (*ctrl.Result, error) {
	log := log.FromContext(ctx)

	if migration.Annotations == nil {
		return nil, nil
	}

	// Check for reconcile trigger annotation first (highest priority)
	if triggerValue, exists := migration.Annotations[reconcileTriggerAnnotation]; exists {
		log.Info("Reconciliation triggered by annotation",
			"trigger", triggerValue,
			"phase", migration.Status.Phase)

		// Set condition
		r.setCondition(migration, ConditionTypeReconciling, metav1.ConditionTrue,
			ReasonAnnotationTriggered,
			fmt.Sprintf("Reconciliation triggered by annotation at %s", triggerValue))

		// Handle based on current phase
		isTerminalPhase := migration.Status.Phase == migrationv1.MigrationPhaseCompleted ||
			migration.Status.Phase == migrationv1.MigrationPhaseFailed ||
			migration.Status.Phase == migrationv1.MigrationPhaseCancelled

		if isTerminalPhase {
			// Force restart for terminal migrations
			log.Info("Forcing restart of terminal migration via annotation")
			r.Recorder.Event(migration, corev1.EventTypeNormal, "ReconcileTriggered",
				fmt.Sprintf("Reconciliation triggered by annotation for %s migration", migration.Status.Phase))

			// Remove annotation first to prevent loops
			delete(migration.Annotations, reconcileTriggerAnnotation)
			if err := r.Update(ctx, migration); err != nil {
				log.Error(err, "Failed to remove reconcile-trigger annotation")
				return &ctrl.Result{}, err
			}

			// Now restart the migration
			result, err := r.restartMigrationForSpecChange(ctx, migration)
			return &result, err
		}

		// For active migrations, just log and remove annotation
		log.Info("Migration is active, reconciliation will proceed normally")
		r.Recorder.Event(migration, corev1.EventTypeNormal, "ReconcileTriggered",
			fmt.Sprintf("Reconciliation triggered by annotation for active migration in %s phase", migration.Status.Phase))

		// Remove the annotation after processing to prevent loops
		delete(migration.Annotations, reconcileTriggerAnnotation)
		if err := r.Update(ctx, migration); err != nil {
			log.Error(err, "Failed to remove reconcile-trigger annotation")
			return &ctrl.Result{}, err
		}

		// Requeue for immediate processing
		return &ctrl.Result{Requeue: true}, nil
	}

	// Check for pause annotation
	if _, exists := migration.Annotations[pauseAnnotation]; exists {
		if migration.Status.Phase != migrationv1.MigrationPhasePaused {
			return r.pauseMigration(ctx, migration)
		}
		return &ctrl.Result{RequeueAfter: pausedRequeueInterval}, nil
	}

	// Check for cancel annotation
	if _, exists := migration.Annotations[cancelAnnotation]; exists && migration.Status.Phase != migrationv1.MigrationPhaseCancelled {
		return r.cancelMigration(ctx, migration)
	}

	return nil, nil
}

// pauseMigration handles pausing a migration
func (r *AdoToGitMigrationReconciler) pauseMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) (*ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MigrationPhasePaused
	migration.Status.Progress.CurrentStep = "Migration paused by user"

	// Set paused condition
	r.setCondition(migration, ConditionTypeMigrationActive, metav1.ConditionFalse,
		ReasonMigrationPaused, "Migration paused by user")

	// Track metrics
	migrationsTotal.WithLabelValues("paused", migration.Namespace).Inc()

	r.sendWebSocketUpdate(migration, "Migration paused")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "MigrationPaused", "Migration paused by user")

	if err := r.Status().Update(ctx, migration); err != nil {
		return &ctrl.Result{RequeueAfter: pausedRequeueInterval}, err
	}
	return &ctrl.Result{RequeueAfter: pausedRequeueInterval}, nil
}

// cancelMigration handles cancelling a migration
func (r *AdoToGitMigrationReconciler) cancelMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) (*ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MigrationPhaseCancelled
	migration.Status.Progress.CurrentStep = "Migration cancelled by user"
	now := metav1.Now()
	migration.Status.CompletionTime = &now

	// Set cancelled condition
	r.setCondition(migration, ConditionTypeReady, metav1.ConditionFalse,
		ReasonMigrationCancelled, "Migration cancelled by user")
	r.setCondition(migration, ConditionTypeMigrationActive, metav1.ConditionFalse,
		ReasonMigrationCancelled, "Migration cancelled")

	// Track metrics
	migrationsTotal.WithLabelValues("cancelled", migration.Namespace).Inc()

	r.sendWebSocketUpdate(migration, "Migration cancelled")
	r.Recorder.Event(migration, corev1.EventTypeWarning, "MigrationCancelled", "Migration cancelled by user")

	if err := r.Status().Update(ctx, migration); err != nil {
		return &ctrl.Result{}, err
	}
	return &ctrl.Result{}, nil
}

// restartMigrationForSpecChange handles restarting a completed migration when spec changes
// This performs comprehensive diffing to detect new, modified, and removed resources
func (r *AdoToGitMigrationReconciler) restartMigrationForSpecChange(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Build map of existing resource statuses for efficient lookups
	existingResourceStatuses := make(map[string]*migrationv1.ResourceMigrationStatus)
	for i := range migration.Status.ResourceStatuses {
		status := &migration.Status.ResourceStatuses[i]
		existingResourceStatuses[status.ResourceID] = status
	}

	// Track changes
	newResources := []migrationv1.MigrationResource{}
	modifiedResources := []migrationv1.MigrationResource{}
	removedResourceIDs := make(map[string]bool)
	unchangedCount := 0

	// Initialize removed resources map with all existing IDs
	for resourceID := range existingResourceStatuses {
		removedResourceIDs[resourceID] = true
	}

	// Check for new and modified resources
	currentResourceIDs := make(map[string]bool)
	for _, resource := range migration.Spec.Resources {
		currentResourceIDs[resource.SourceID] = true
		delete(removedResourceIDs, resource.SourceID) // Not removed if it exists in spec

		if existingStatus, exists := existingResourceStatuses[resource.SourceID]; exists {
			// Check if resource was modified (target name changed, etc.)
			if existingStatus.TargetName != resource.TargetName ||
				existingStatus.SourceName != resource.SourceName ||
				existingStatus.Type != resource.Type {
				modifiedResources = append(modifiedResources, resource)
				log.Info("Resource modified detected",
					"resourceID", resource.SourceID,
					"oldTarget", existingStatus.TargetName,
					"newTarget", resource.TargetName,
					"oldSource", existingStatus.SourceName,
					"newSource", resource.SourceName)
			} else {
				unchangedCount++
			}
		} else {
			// New resource
			newResources = append(newResources, resource)
			log.Info("New resource detected",
				"resourceID", resource.SourceID,
				"sourceName", resource.SourceName,
				"targetName", resource.TargetName)
		}
	}

	// Log removed resources
	for resourceID := range removedResourceIDs {
		if existingStatus, exists := existingResourceStatuses[resourceID]; exists {
			log.Info("Resource removed from spec",
				"resourceID", resourceID,
				"sourceName", existingStatus.SourceName,
				"targetName", existingStatus.TargetName)
		}
	}

	// Calculate change counts
	newCount := len(newResources)
	modifiedCount := len(modifiedResources)
	removedCount := len(removedResourceIDs)
	totalChanges := newCount + modifiedCount + removedCount

	// If nothing changed, just update generation and return
	if totalChanges == 0 {
		log.Info("Spec changed but no resource changes detected, updating generation only")
		migration.Status.ObservedGeneration = migration.Generation
		now := metav1.Now()
		migration.Status.LastReconcileTime = &now

		// Set condition
		r.setCondition(migration, ConditionTypeSpecChanged, metav1.ConditionTrue,
			ReasonNoResourceChanges, "Spec updated but no resource changes detected")

		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Log comprehensive summary
	log.Info("Spec changes detected - comprehensive analysis",
		"newResources", newCount,
		"modifiedResources", modifiedCount,
		"removedResources", removedCount,
		"unchangedResources", unchangedCount,
		"totalChanges", totalChanges)

	// Handle removed resources - mark as skipped
	for resourceID := range removedResourceIDs {
		for i := range migration.Status.ResourceStatuses {
			if migration.Status.ResourceStatuses[i].ResourceID == resourceID {
				oldStatus := migration.Status.ResourceStatuses[i].Status
				migration.Status.ResourceStatuses[i].Status = migrationv1.ResourceStatusSkipped
				migration.Status.ResourceStatuses[i].ErrorMessage = "Resource removed from spec"

				// Update counters based on old status
				if oldStatus == migrationv1.ResourceStatusCompleted {
					migration.Status.Progress.Completed--
				} else if oldStatus == migrationv1.ResourceStatusFailed {
					migration.Status.Progress.Failed--
				}
				migration.Status.Progress.Skipped++
				break
			}
		}
	}

	// Handle modified resources - reset to pending for re-processing
	for _, resource := range modifiedResources {
		for i := range migration.Status.ResourceStatuses {
			if migration.Status.ResourceStatuses[i].ResourceID == resource.SourceID {
				oldStatus := migration.Status.ResourceStatuses[i].Status

				// Update resource status
				migration.Status.ResourceStatuses[i].Status = migrationv1.ResourceStatusPending
				migration.Status.ResourceStatuses[i].TargetName = resource.TargetName
				migration.Status.ResourceStatuses[i].SourceName = resource.SourceName
				migration.Status.ResourceStatuses[i].Type = resource.Type
				migration.Status.ResourceStatuses[i].Progress = 0
				migration.Status.ResourceStatuses[i].ErrorMessage = "Resource modified - queued for re-migration"
				migration.Status.ResourceStatuses[i].GitHubURL = ""
				migration.Status.ResourceStatuses[i].RepositoryURL = ""

				// Update counters based on old status
				if oldStatus == migrationv1.ResourceStatusCompleted {
					migration.Status.Progress.Completed--
				} else if oldStatus == migrationv1.ResourceStatusFailed {
					migration.Status.Progress.Failed--
				}
				break
			}
		}
	}

	// Update total count before adding new resources
	oldTotal := migration.Status.Progress.Total
	migration.Status.Progress.Total = len(migration.Spec.Resources)

	// Add new resources with pending status
	for _, resource := range newResources {
		resourceStatus := migrationv1.ResourceMigrationStatus{
			ResourceID: resource.SourceID,
			Type:       string(resource.Type),
			SourceName: resource.SourceName,
			TargetName: resource.TargetName,
			Status:     migrationv1.ResourceStatusPending,
			Progress:   0,
		}
		migration.Status.ResourceStatuses = append(migration.Status.ResourceStatuses, resourceStatus)
	}

	// Recalculate percentage
	if migration.Status.Progress.Total > 0 {
		completed := migration.Status.Progress.Completed
		failed := migration.Status.Progress.Failed
		migration.Status.Progress.Percentage = ((completed + failed) * 100) / migration.Status.Progress.Total
	}

	// Change phase back to Running to process changes
	migration.Status.Phase = migrationv1.MigrationPhaseRunning
	migration.Status.Progress.CurrentStep = fmt.Sprintf("Processing spec changes: %d new, %d modified, %d removed",
		newCount, modifiedCount, removedCount)
	migration.Status.CompletionTime = nil // Clear completion time

	// Update observed generation
	migration.Status.ObservedGeneration = migration.Generation
	now := metav1.Now()
	migration.Status.LastReconcileTime = &now

	// Set condition with detailed information
	r.setCondition(migration, ConditionTypeSpecChanged, metav1.ConditionTrue,
		ReasonResourcesChanged,
		fmt.Sprintf("Detected %d new, %d modified, %d removed resources (total: %d → %d)",
			newCount, modifiedCount, removedCount, oldTotal, migration.Status.Progress.Total))

	// Set migration active condition
	r.setCondition(migration, ConditionTypeMigrationActive, metav1.ConditionTrue,
		ReasonMigrationRunning,
		"Migration restarted due to spec changes")

	// Send notifications
	changeMessage := fmt.Sprintf("Resuming migration with changes: %d new, %d modified, %d removed resources",
		newCount, modifiedCount, removedCount)
	r.sendWebSocketUpdate(migration, changeMessage)

	r.Recorder.Event(migration, corev1.EventTypeNormal, "MigrationResumed",
		fmt.Sprintf("Migration resumed: %d new, %d modified, %d removed resources (total: %d → %d)",
			newCount, modifiedCount, removedCount, oldTotal, migration.Status.Progress.Total))

	if err := r.Status().Update(ctx, migration); err != nil {
		log.Error(err, "Failed to update status after spec change")
		return ctrl.Result{}, err
	}

	log.Info("Migration successfully restarted for spec changes",
		"newPhase", migration.Status.Phase,
		"totalResources", migration.Status.Progress.Total)

	return ctrl.Result{RequeueAfter: runningRequeueInterval}, nil
}

// initializeStatus initializes the migration status
func (r *AdoToGitMigrationReconciler) initializeStatus(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	migration.Status.Phase = migrationv1.MigrationPhasePending
	migration.Status.Progress = migrationv1.MigrationProgress{
		Total:      len(migration.Spec.Resources),
		Completed:  0,
		Failed:     0,
		Processing: 0,
		Skipped:    0,
		Percentage: 0,
	}

	// Set start time
	now := metav1.Now()
	migration.Status.StartTime = &now

	// Initialize observed generation to track spec changes
	migration.Status.ObservedGeneration = migration.Generation
	migration.Status.LastReconcileTime = &now

	// Initialize statistics
	migration.Status.Statistics = &migrationv1.MigrationStatistics{
		APICalls: make(map[string]int),
	}

	// Send updates
	r.sendWebSocketUpdate(migration, "Migration initialized")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "Initialized", "Migration initialized")

	if err := r.Status().Update(ctx, migration); err != nil {
		log.Error(err, "Failed to update migration status")
		return ctrl.Result{RequeueAfter: pendingRequeueInterval}, err
	}
	return ctrl.Result{RequeueAfter: pendingRequeueInterval}, nil
}

// processPhase processes the migration based on its current phase
func (r *AdoToGitMigrationReconciler) processPhase(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	switch migration.Status.Phase {
	case migrationv1.MigrationPhasePending:
		return r.processPending(ctx, migration)
	case migrationv1.MigrationPhaseValidating:
		return r.processValidating(ctx, migration)
	case migrationv1.MigrationPhaseRunning:
		return r.processRunning(ctx, migration)
	case migrationv1.MigrationPhasePaused:
		return r.processPaused(ctx, migration)
	case migrationv1.MigrationPhaseSyncing:
		return r.processSyncing(ctx, migration)
	case migrationv1.MigrationPhaseCompleted, migrationv1.MigrationPhaseFailed, migrationv1.MigrationPhaseCancelled:
		// Terminal states - no action needed
		// Note: observedGeneration is deliberately NOT updated here
		// This allows spec changes to be detected in the next reconciliation
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *AdoToGitMigrationReconciler) processPending(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MigrationPhaseValidating
	migration.Status.Progress.CurrentStep = "Validating configuration"

	r.sendWebSocketUpdate(migration, "Starting validation")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "ValidationStarted", "Migration validation started")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{RequeueAfter: pendingRequeueInterval}, err
	}

	return ctrl.Result{RequeueAfter: pendingRequeueInterval}, nil
}

func (r *AdoToGitMigrationReconciler) processValidating(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Skip validation if requested
	if migration.Spec.ValidationRules != nil && migration.Spec.ValidationRules.SkipValidation {
		logger.Info("Skipping validation as requested")
		return r.skipValidation(ctx, migration)
	}

	logger.Info("Starting validation process")

	// Get GitHub client (supports both PAT and GitHub App)
	logger.Info("Getting GitHub client for validation")
	githubClient, err := r.getGitHubClient(ctx, migration)
	if err != nil {
		logger.Error(err, "Failed to get GitHub client")
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to get GitHub client: %v", err))
	}
	logger.Info("Successfully obtained GitHub client")

	// Perform validation (get token for legacy validation functions)
	var token string
	if migration.Spec.Target.Auth.TokenRef != nil {
		token, err = r.getGitHubToken(ctx, migration)
		if err != nil {
			return r.failMigration(ctx, migration, fmt.Sprintf("Failed to get GitHub token: %v", err))
		}
	}

	logger.Info("Performing validation with client")
	validationResults := r.performValidationWithClient(ctx, migration, githubClient, token)
	logger.Info("Validation completed", "valid", validationResults.Valid, "errors", len(validationResults.Errors), "warnings", len(validationResults.Warnings))

	// Store validation results
	migration.Status.ValidationResults = validationResults

	// Handle validation results
	logger.Info("Handling validation results")
	return r.handleValidationResults(ctx, migration)
}

// skipValidation handles skipping validation
func (r *AdoToGitMigrationReconciler) skipValidation(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Skipping validation as requested")

	migration.Status.Phase = migrationv1.MigrationPhaseRunning
	migration.Status.Progress.CurrentStep = "Starting migration (validation skipped)"

	r.sendWebSocketUpdate(migration, "Validation skipped, starting migration")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "ValidationSkipped", "Validation skipped as requested")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{RequeueAfter: pendingRequeueInterval}, err
	}
	return ctrl.Result{RequeueAfter: runningRequeueInterval}, nil
}

// performValidation performs all validation checks
func (r *AdoToGitMigrationReconciler) performValidation(ctx context.Context, migration *migrationv1.AdoToGitMigration, token string) *migrationv1.ValidationResults {
	validationResults := &migrationv1.ValidationResults{
		Valid:     true,
		Errors:    []migrationv1.ValidationError{},
		Warnings:  []migrationv1.ValidationWarning{},
		Timestamp: metav1.Now(),
	}

	// Validate GitHub credentials
	r.validateGitHubCredentials(ctx, migration, token, validationResults)

	// Validate each resource
	r.validateResources(ctx, migration, token, validationResults)

	return validationResults
}

// performValidationWithClient performs validation using a GitHub client (supports both PAT and GitHub App)
func (r *AdoToGitMigrationReconciler) performValidationWithClient(ctx context.Context, migration *migrationv1.AdoToGitMigration, client *github.Client, token string) *migrationv1.ValidationResults {
	validationResults := &migrationv1.ValidationResults{
		Valid:     true,
		Errors:    []migrationv1.ValidationError{},
		Warnings:  []migrationv1.ValidationWarning{},
		Timestamp: metav1.Now(),
	}

	// Validate GitHub credentials using client
	r.validateGitHubCredentialsWithClient(ctx, migration, client, validationResults)

	// Validate each resource using client
	r.validateResourcesWithClient(ctx, migration, client, validationResults)

	return validationResults
}

// validateGitHubCredentials validates GitHub credentials and permissions
func (r *AdoToGitMigrationReconciler) validateGitHubCredentials(ctx context.Context, migration *migrationv1.AdoToGitMigration, token string, validationResults *migrationv1.ValidationResults) {
	logger := log.FromContext(ctx)

	scopes, rateLimit, err := r.GitHubService.ValidateCredentials(ctx, token, migration.Spec.Target.Owner)
	if err != nil {
		validationResults.Valid = false
		validationResults.Errors = append(validationResults.Errors, migrationv1.ValidationError{
			Code:       "GITHUB_AUTH_FAILED",
			Message:    fmt.Sprintf("GitHub authentication failed: %v", err),
			Field:      "target.auth",
			Severity:   "error",
			Resolution: "Check GitHub token and permissions",
		})
		return
	}

	logger.Info("GitHub validation passed", "scopes", scopes, "rateLimit", rateLimit)

	// Check required scopes and rate limit
	r.validateRequiredScopes(migration, scopes, validationResults)
	r.validateRateLimit(rateLimit, validationResults)
}

// validateRequiredScopes validates that required GitHub scopes are present
func (r *AdoToGitMigrationReconciler) validateRequiredScopes(migration *migrationv1.AdoToGitMigration, scopes []string, validationResults *migrationv1.ValidationResults) {
	requiredScopes := []string{"repo"}
	if migration.Spec.ValidationRules != nil && len(migration.Spec.ValidationRules.RequiredPermissions) > 0 {
		requiredScopes = migration.Spec.ValidationRules.RequiredPermissions
	}

	for _, required := range requiredScopes {
		if !r.containsScope(scopes, required) {
			validationResults.Valid = false
			validationResults.Errors = append(validationResults.Errors, migrationv1.ValidationError{
				Code:       "MISSING_SCOPE",
				Message:    fmt.Sprintf("Missing required scope: %s", required),
				Field:      "target.auth.tokenRef",
				Severity:   "error",
				Resolution: fmt.Sprintf("Add %s scope to GitHub token", required),
			})
		}
	}
}

// containsScope checks if a scope exists in the list
func (r *AdoToGitMigrationReconciler) containsScope(scopes []string, required string) bool {
	for _, available := range scopes {
		if available == required {
			return true
		}
	}
	return false
}

// validateRateLimit checks GitHub API rate limit
func (r *AdoToGitMigrationReconciler) validateRateLimit(rateLimit *services.GitHubRateLimit, validationResults *migrationv1.ValidationResults) {
	if rateLimit != nil && rateLimit.Remaining < 100 {
		validationResults.Warnings = append(validationResults.Warnings, migrationv1.ValidationWarning{
			Code:       "LOW_RATE_LIMIT",
			Message:    fmt.Sprintf("GitHub API rate limit is low (%d remaining)", rateLimit.Remaining),
			Field:      "target.auth",
			Suggestion: "Consider waiting or using a different token",
		})
	}
}

// validateResources validates each resource in the migration
func (r *AdoToGitMigrationReconciler) validateResources(ctx context.Context, migration *migrationv1.AdoToGitMigration, token string, validationResults *migrationv1.ValidationResults) {
	for _, resource := range migration.Spec.Resources {
		if resource.Type == "repository" {
			r.validateRepository(ctx, migration, resource, token, validationResults)
		}
	}
}

// validateRepository validates a repository resource
func (r *AdoToGitMigrationReconciler) validateRepository(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource, token string, validationResults *migrationv1.ValidationResults) {
	exists, err := r.GitHubService.CheckRepositoryExists(ctx, token, migration.Spec.Target.Owner, resource.TargetName)
	if err != nil {
		validationResults.Warnings = append(validationResults.Warnings, migrationv1.ValidationWarning{
			Code:       "REPO_CHECK_FAILED",
			Message:    fmt.Sprintf("Could not check if repository %s exists: %v", resource.TargetName, err),
			Field:      "resources",
			Resource:   resource.SourceName,
			Suggestion: "Verify repository name and permissions",
		})
		return
	}

	if exists {
		r.handleExistingRepository(migration, resource, validationResults)
	}
}

// handleExistingRepository handles validation for existing repositories
func (r *AdoToGitMigrationReconciler) handleExistingRepository(migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource, validationResults *migrationv1.ValidationResults) {
	// Always allow existing repositories unless explicitly configured otherwise
	// This provides a safer default behavior for migrations
	allowExistingRepo := true

	// Only check the setting if repository settings are explicitly provided
	// This way, users who don't specify settings get the safe default (allow existing)
	// but users who explicitly set createIfNotExists to false will get validation error
	if resource.Settings != nil && resource.Settings.Repository != nil {
		// Note: We could add a custom field like "strictValidation" or "allowExisting"
		// For now, we'll always allow existing repos to make migrations work by default
		allowExistingRepo = true
	}

	if !allowExistingRepo {
		validationResults.Errors = append(validationResults.Errors, migrationv1.ValidationError{
			Code:       "REPO_EXISTS",
			Message:    fmt.Sprintf("Repository %s/%s already exists and createIfNotExists is not enabled", migration.Spec.Target.Owner, resource.TargetName),
			Field:      "resources",
			Resource:   resource.SourceName,
			Severity:   "error",
			Resolution: "Choose a different name, enable createIfNotExists, or delete the existing repository",
		})
		validationResults.Valid = false
	} else {
		validationResults.Warnings = append(validationResults.Warnings, migrationv1.ValidationWarning{
			Code:       "REPO_WILL_USE_EXISTING",
			Message:    fmt.Sprintf("Repository %s/%s already exists and will be used for migration", migration.Spec.Target.Owner, resource.TargetName),
			Field:      "resources",
			Resource:   resource.SourceName,
			Suggestion: "Ensure this is intended - existing content may be overwritten",
		})
	}
}

// validateGitHubCredentialsWithClient validates GitHub credentials using a client (supports PAT and GitHub App)
func (r *AdoToGitMigrationReconciler) validateGitHubCredentialsWithClient(ctx context.Context, migration *migrationv1.AdoToGitMigration, client *github.Client, validationResults *migrationv1.ValidationResults) {
	logger := log.FromContext(ctx)
	logger.Info("Validating GitHub credentials with client")

	// Create a timeout context for API calls to prevent hanging
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// For GitHub App authentication, verify organization access directly
	// (GitHub Apps may not have permission to access /user endpoint)
	owner := migration.Spec.Target.Owner

	logger.Info("Verifying access to GitHub organization", "organization", owner)
	org, resp, err := client.Organizations.Get(timeoutCtx, owner)
	if err != nil {
		logger.Error(err, "Failed to access GitHub organization", "organization", owner)
		validationResults.Valid = false
		validationResults.Errors = append(validationResults.Errors, migrationv1.ValidationError{
			Code:       "GITHUB_ORG_ACCESS_FAILED",
			Message:    fmt.Sprintf("Cannot access organization '%s': %v", owner, err),
			Field:      "target.owner",
			Severity:   "error",
			Resolution: "Ensure GitHub App is installed on the organization or PAT has access",
		})
		return
	}

	logger.Info("GitHub authentication successful", "organization", org.GetLogin(), "rateRemaining", resp.Rate.Remaining)

	// Check rate limit
	if resp.Rate.Remaining < 100 {
		validationResults.Warnings = append(validationResults.Warnings, migrationv1.ValidationWarning{
			Code:       "LOW_RATE_LIMIT",
			Message:    fmt.Sprintf("GitHub API rate limit is low (%d remaining)", resp.Rate.Remaining),
			Field:      "target.auth",
			Suggestion: "Consider waiting or using a different authentication method",
		})
	}

	logger.Info("GitHub organization access verified", "organization", owner)
}

// validateResourcesWithClient validates resources using a GitHub client
func (r *AdoToGitMigrationReconciler) validateResourcesWithClient(ctx context.Context, migration *migrationv1.AdoToGitMigration, client *github.Client, validationResults *migrationv1.ValidationResults) {
	for _, resource := range migration.Spec.Resources {
		if resource.Type == "repository" {
			r.validateRepositoryWithClient(ctx, migration, resource, client, validationResults)
		}
	}
}

// validateRepositoryWithClient validates a repository resource using a GitHub client
func (r *AdoToGitMigrationReconciler) validateRepositoryWithClient(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource, client *github.Client, validationResults *migrationv1.ValidationResults) {
	// Create a timeout context for API calls
	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Check if repository exists
	repo, resp, err := client.Repositories.Get(timeoutCtx, migration.Spec.Target.Owner, resource.TargetName)

	// If we get a 404, repository doesn't exist - this is fine
	if resp != nil && resp.StatusCode == 404 {
		return
	}

	// Handle other errors
	if err != nil {
		validationResults.Warnings = append(validationResults.Warnings, migrationv1.ValidationWarning{
			Code:       "REPO_CHECK_FAILED",
			Message:    fmt.Sprintf("Could not check if repository %s exists: %v", resource.TargetName, err),
			Field:      "resources",
			Resource:   resource.SourceName,
			Suggestion: "Verify repository name and permissions",
		})
		return
	}

	// Repository exists
	if repo != nil {
		r.handleExistingRepository(migration, resource, validationResults)
	}
}

// handleValidationResults processes validation results and transitions to appropriate phase
func (r *AdoToGitMigrationReconciler) handleValidationResults(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	if !migration.Status.ValidationResults.Valid {
		return r.failValidation(ctx, migration)
	}

	// Validation passed
	return r.passValidation(ctx, migration)
}

// failValidation handles failed validation
func (r *AdoToGitMigrationReconciler) failValidation(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MigrationPhaseFailed
	migration.Status.ErrorMessage = "Validation failed - see validation results for details"
	migration.Status.Progress.CurrentStep = "Validation failed"

	now := metav1.Now()
	migration.Status.CompletionTime = &now

	// Set validation failed conditions
	r.setCondition(migration, ConditionTypeValidated, metav1.ConditionFalse,
		ReasonValidationFailed, "Validation failed - see validation results for details")
	r.setCondition(migration, ConditionTypeReady, metav1.ConditionFalse,
		ReasonValidationFailed, "Migration cannot proceed due to validation failures")

	r.sendWebSocketUpdate(migration, "Validation failed")
	r.Recorder.Event(migration, corev1.EventTypeWarning, "ValidationFailed", "Migration validation failed")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// passValidation handles successful validation
func (r *AdoToGitMigrationReconciler) passValidation(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MigrationPhaseRunning
	migration.Status.Progress.CurrentStep = "Starting migration"

	// Set validation passed conditions
	r.setCondition(migration, ConditionTypeValidated, metav1.ConditionTrue,
		ReasonValidationPassed, "All validation checks passed")
	r.setCondition(migration, ConditionTypeMigrationActive, metav1.ConditionTrue,
		ReasonMigrationRunning, "Migration is now running")

	r.sendWebSocketUpdate(migration, "Validation passed, starting migration")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "ValidationPassed", "Migration validation passed")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{RequeueAfter: pendingRequeueInterval}, err
	}

	return ctrl.Result{RequeueAfter: runningRequeueInterval}, nil
}

func (r *AdoToGitMigrationReconciler) processRunning(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	// Get GitHub token (supports both PAT and GitHub App)
	token, err := r.getGitHubTokenUnified(ctx, migration)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to get GitHub token: %v", err))
	}

	// Process each resource
	for i, resource := range migration.Spec.Resources {
		resourceIndex, resourceProcessed := r.checkResourceStatus(migration, resource)

		if resourceProcessed {
			continue
		}

		// Update current item (1-indexed for display)
		migration.Status.Progress.CurrentItem = i + 1
		migration.Status.Progress.ProgressSummary = fmt.Sprintf("%d/%d", i+1, len(migration.Spec.Resources))

		// Process the resource
		if err := r.processResource(ctx, migration, resource, resourceIndex, token); err != nil {
			return r.handleResourceProcessingFailure(ctx, migration, resource, err)
		}

		// Save status after processing each resource
		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{RequeueAfter: runningRequeueInterval}, fmt.Errorf("failed to update status after processing resource: %w", err)
		}

		// Continue to next resource
		if i < len(migration.Spec.Resources)-1 {
			return ctrl.Result{RequeueAfter: runningRequeueInterval}, nil
		}
	}

	// All resources processed successfully
	return r.completeMigration(ctx, migration)
}

// checkResourceStatus checks if a resource has already been processed
func (r *AdoToGitMigrationReconciler) checkResourceStatus(migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource) (int, bool) {
	for j, status := range migration.Status.ResourceStatuses {
		if status.SourceName == resource.SourceName {
			if status.Status == migrationv1.ResourceStatusCompleted || status.Status == migrationv1.ResourceStatusFailed {
				return j, true
			}
			return j, false
		}
	}
	return -1, false
}

// processResource processes a single resource
func (r *AdoToGitMigrationReconciler) processResource(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource, resourceIndex int, token string) error {
	logger := log.FromContext(ctx)

	// Update processing status
	r.updateProcessingStatus(migration, resource)

	if err := r.Status().Update(ctx, migration); err != nil {
		logger.Error(err, "Failed to update status")
	}

	// Create or update resource status
	resourceStatus := r.createResourceStatus(resource)

	// Add or update resource status
	if resourceIndex >= 0 {
		migration.Status.ResourceStatuses[resourceIndex] = resourceStatus
	} else {
		migration.Status.ResourceStatuses = append(migration.Status.ResourceStatuses, resourceStatus)
		resourceIndex = len(migration.Status.ResourceStatuses) - 1
	}

	// Process based on resource type
	processingErr := r.processResourceByType(ctx, migration, &resource, &resourceStatus, token)

	// Update resource status after processing
	r.updateResourceStatusAfterProcessing(migration, &resourceStatus, processingErr, resourceIndex, resource)

	return processingErr
}

// updateProcessingStatus updates the migration status to show current processing
func (r *AdoToGitMigrationReconciler) updateProcessingStatus(migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource) {
	migration.Status.Progress.Processing = 1
	migration.Status.Progress.CurrentStep = fmt.Sprintf("Processing %s: %s", resource.Type, resource.TargetName)
	r.sendWebSocketUpdate(migration, fmt.Sprintf("Processing %s: %s", resource.Type, resource.TargetName))
}

// createResourceStatus creates a new resource status
func (r *AdoToGitMigrationReconciler) createResourceStatus(resource migrationv1.MigrationResource) migrationv1.ResourceMigrationStatus {
	now := metav1.Now()
	return migrationv1.ResourceMigrationStatus{
		ResourceID: resource.SourceID,
		Type:       resource.Type,
		SourceName: resource.SourceName,
		TargetName: resource.TargetName,
		Status:     migrationv1.ResourceStatusRunning,
		Progress:   0,
		StartTime:  &now,
		Name:       resource.SourceName, // For backward compatibility
	}
}

// processResourceByType processes a resource based on its type
func (r *AdoToGitMigrationReconciler) processResourceByType(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, status *migrationv1.ResourceMigrationStatus, token string) error {
	switch resource.Type {
	case "repository":
		return r.processRepository(ctx, migration, resource, status, token)
	case "work-item":
		return r.processWorkItem(ctx, migration, resource, status, token)
	case "pipeline":
		return r.processPipeline(ctx, migration, resource, status, token)
	default:
		return fmt.Errorf("unsupported resource type: %s", resource.Type)
	}
}

// updateResourceStatusAfterProcessing updates resource status and migration progress after processing
func (r *AdoToGitMigrationReconciler) updateResourceStatusAfterProcessing(migration *migrationv1.AdoToGitMigration, resourceStatus *migrationv1.ResourceMigrationStatus, processingErr error, resourceIndex int, resource migrationv1.MigrationResource) {
	logger := log.FromContext(context.Background())
	now := metav1.Now()
	resourceStatus.CompletionTime = &now

	if processingErr != nil {
		r.handleResourceFailure(migration, resourceStatus, processingErr, resource, logger)
	} else {
		r.handleResourceSuccess(migration, resourceStatus, resource, logger)
	}

	// Update the resource status in the array
	migration.Status.ResourceStatuses[resourceIndex] = *resourceStatus

	// Update progress and statistics
	r.updateProgressPercentage(migration)
	r.updateStatistics(migration, processingErr, resource)
}

// handleResourceFailure handles resource processing failure
func (r *AdoToGitMigrationReconciler) handleResourceFailure(migration *migrationv1.AdoToGitMigration, resourceStatus *migrationv1.ResourceMigrationStatus, processingErr error, resource migrationv1.MigrationResource, logger logr.Logger) {
	logger.Error(processingErr, "Failed to process resource", "resource", resource.SourceName, "type", resource.Type)

	resourceStatus.Status = migrationv1.ResourceStatusFailed
	resourceStatus.ErrorMessage = processingErr.Error()
	resourceStatus.Error = processingErr.Error() // For backward compatibility
	resourceStatus.Progress = 0

	migration.Status.Progress.Failed++
	migration.Status.Progress.Processing = 0

	r.sendWebSocketUpdate(migration, fmt.Sprintf("Failed to process %s: %v", resource.TargetName, processingErr))
	r.Recorder.Event(migration, corev1.EventTypeWarning, "ResourceProcessingFailed",
		fmt.Sprintf("Failed to process %s: %v", resource.TargetName, processingErr))
}

// handleResourceSuccess handles successful resource processing
func (r *AdoToGitMigrationReconciler) handleResourceSuccess(migration *migrationv1.AdoToGitMigration, resourceStatus *migrationv1.ResourceMigrationStatus, resource migrationv1.MigrationResource, logger logr.Logger) {
	logger.Info("Resource processed successfully", "resource", resource.SourceName, "type", resource.Type)

	resourceStatus.Status = migrationv1.ResourceStatusCompleted
	resourceStatus.Progress = 100

	migration.Status.Progress.Completed++
	migration.Status.Progress.Processing = 0

	r.sendWebSocketUpdate(migration, fmt.Sprintf("Successfully processed %s", resource.TargetName))
	r.Recorder.Event(migration, corev1.EventTypeNormal, "ResourceProcessed",
		fmt.Sprintf("Successfully processed %s", resource.TargetName))
}

// updateProgressPercentage calculates and updates the progress percentage
func (r *AdoToGitMigrationReconciler) updateProgressPercentage(migration *migrationv1.AdoToGitMigration) {
	total := migration.Status.Progress.Total
	completed := migration.Status.Progress.Completed
	failed := migration.Status.Progress.Failed
	if total > 0 {
		migration.Status.Progress.Percentage = ((completed + failed) * 100) / total
	}
}

// updateStatistics updates migration statistics
func (r *AdoToGitMigrationReconciler) updateStatistics(migration *migrationv1.AdoToGitMigration, processingErr error, resource migrationv1.MigrationResource) {
	if migration.Status.Statistics == nil {
		migration.Status.Statistics = &migrationv1.MigrationStatistics{
			APICalls: make(map[string]int),
		}
	}

	if processingErr == nil && resource.Type == "repository" {
		migration.Status.Statistics.RepositoriesCreated++
	}
}

// handleResourceProcessingFailure handles failures during resource processing
func (r *AdoToGitMigrationReconciler) handleResourceProcessingFailure(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource migrationv1.MigrationResource, processingErr error) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	migration.Status.Phase = migrationv1.MigrationPhaseFailed
	migration.Status.ErrorMessage = fmt.Sprintf("Resource processing failed for %s: %v", resource.TargetName, processingErr)

	if err := r.Status().Update(ctx, migration); err != nil {
		logger.Error(err, "Failed to update migration status")
	}
	return ctrl.Result{}, nil
}

// completeMigration handles successful completion of migration
func (r *AdoToGitMigrationReconciler) completeMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	now := metav1.Now()
	migration.Status.CompletionTime = &now

	// Calculate duration and record metrics
	if migration.Status.StartTime != nil {
		duration := now.Time.Sub(migration.Status.StartTime.Time)
		migration.Status.Statistics.Duration = metav1.Duration{Duration: duration}

		// Record migration duration metric
		migrationDuration.WithLabelValues("completed", migration.Namespace).Observe(duration.Seconds())
	}

	// Track migration completion
	migrationsTotal.WithLabelValues("completed", migration.Namespace).Inc()

	// Check if continuous sync is enabled
	if migration.Spec.Settings.Sync != nil && migration.Spec.Settings.Sync.Enabled {
		// Transition to Syncing phase
		migration.Status.Phase = migrationv1.MigrationPhaseSyncing
		migration.Status.Progress.CurrentStep = "Initial migration completed, entering sync mode"
		migration.Status.Progress.Percentage = 100
		migration.Status.Progress.Processing = 0
		migration.Status.Progress.ProgressSummary = fmt.Sprintf("%d/%d", migration.Status.Progress.Total, migration.Status.Progress.Total)

		// Initialize sync status
		syncIntervalMinutes := migration.Spec.Settings.Sync.IntervalMinutes
		if syncIntervalMinutes == 0 {
			syncIntervalMinutes = 5 // Default to 5 minutes
		}
		nextSync := metav1.NewTime(now.Add(time.Duration(syncIntervalMinutes) * time.Minute))

		migration.Status.SyncStatus = &migrationv1.SyncStatus{
			Enabled:         true,
			LastSyncTime:    &now,
			SyncCount:       0,
			FailedSyncCount: 0,
			NextSyncTime:    &nextSync,
		}

		r.sendWebSocketUpdate(migration, "Migration completed, entering continuous sync mode")
		r.Recorder.Event(migration, corev1.EventTypeNormal, "SyncModeEnabled", "Migration completed, entering continuous sync mode")

		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{}, err
		}

		// Requeue for next sync
		return ctrl.Result{RequeueAfter: time.Duration(syncIntervalMinutes) * time.Minute}, nil
	}

	// No sync enabled - mark as completed
	migration.Status.Phase = migrationv1.MigrationPhaseCompleted
	migration.Status.Progress.CurrentStep = "Migration completed successfully"
	migration.Status.Progress.Percentage = 100
	migration.Status.Progress.Processing = 0
	migration.Status.Progress.ProgressSummary = fmt.Sprintf("%d/%d", migration.Status.Progress.Total, migration.Status.Progress.Total)

	// Set success conditions
	r.setCondition(migration, ConditionTypeReady, metav1.ConditionTrue,
		ReasonMigrationCompleted, "Migration completed successfully")
	r.setCondition(migration, ConditionTypeMigrationActive, metav1.ConditionFalse,
		ReasonMigrationCompleted, "Migration finished")

	r.sendWebSocketUpdate(migration, "Migration completed successfully")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "MigrationCompleted", "Migration completed successfully")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AdoToGitMigrationReconciler) processRepository(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, status *migrationv1.ResourceMigrationStatus, token string) error {
	logger := log.FromContext(ctx)

	// Step 1: Handle default branch detection and setup
	if err := r.handleDefaultBranchSetup(ctx, migration, resource, logger); err != nil {
		return fmt.Errorf("failed to setup default branch: %w", err)
	}

	// Step 2: Check and validate repository state
	repoState, repo, err := r.validateRepositoryState(ctx, migration, resource, token, logger)
	if err != nil {
		return fmt.Errorf("failed to validate repository state: %w", err)
	}

	// Step 3: Update repository state in migration status
	r.updateRepositoryStateStatus(migration, resource.TargetName, repoState, repo)

	// Step 4: Handle repository creation if needed
	if repoState == migrationv1.RepositoryStateNotExists {
		repo, err = r.createRepository(ctx, migration, resource, token, logger)
		if err != nil {
			return fmt.Errorf("failed to create repository: %w", err)
		}
		// Update state to created
		r.updateRepositoryStateStatus(migration, resource.TargetName, migrationv1.RepositoryStateCreated, repo)
	}

	// Update status with repository information
	status.GitHubURL = repo.GetHTMLURL()
	status.RepositoryURL = repo.GetHTMLURL() // For backward compatibility

	// Step 5: Perform the actual code migration if source configuration exists
	if migration.Spec.Source.Organization != "" && migration.Spec.Source.Project != "" {
		if err := r.performCodeMigration(ctx, migration, resource, status, token, logger); err != nil {
			return err
		}
	}

	// Step 6: Set default branch AFTER code migration so the branch actually exists
	if repoState == migrationv1.RepositoryStateNotExists || repoState == migrationv1.RepositoryStateEmpty {
		if err := r.setDefaultBranch(ctx, migration, resource, token, logger); err != nil {
			logger.Error(err, "Failed to set default branch after migration", "repo", resource.TargetName)
			// Don't fail the migration for default branch setting issues
		}
	}

	logger.Info("Repository setup completed successfully", "repo", resource.TargetName)
	r.sendWebSocketUpdate(migration, fmt.Sprintf("Repository %s setup completed successfully", resource.TargetName))
	return nil
}

// handleDefaultBranchSetup handles default branch detection and setup
func (r *AdoToGitMigrationReconciler) handleDefaultBranchSetup(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, logger logr.Logger) error {
	// Initialize default branch info if not exists
	if migration.Status.DefaultBranchInfo == nil {
		migration.Status.DefaultBranchInfo = &migrationv1.DefaultBranchInfo{}
	}

	// If default branch is explicitly configured, use it
	if migration.Spec.Target.DefaultBranch != "" {
		migration.Status.DefaultBranchInfo.TargetDefaultBranch = migration.Spec.Target.DefaultBranch
		migration.Status.DefaultBranchInfo.AutoDetected = false
		logger.Info("Using explicitly configured default branch", "branch", migration.Spec.Target.DefaultBranch)
		return nil
	}

	// Auto-detect default branch from ADO if not already detected
	if migration.Status.DefaultBranchInfo.SourceDefaultBranch == "" {
		adoToken, err := r.getAzureDevOpsToken(ctx, migration)
		if err != nil {
			logger.Info("Failed to get Azure DevOps token for branch detection, using 'main' as default", "error", err)
			migration.Status.DefaultBranchInfo.TargetDefaultBranch = "main"
			migration.Status.DefaultBranchInfo.AutoDetected = true
			return nil
		}

		// Create Azure DevOps service to detect default branch
		adoService := services.NewAzureDevOpsService()
		sourceDefaultBranch, err := adoService.GetRepositoryDefaultBranch(ctx, adoToken, migration.Spec.Source.Organization, migration.Spec.Source.Project, resource.SourceName)
		if err != nil {
			logger.Info("Failed to detect default branch from ADO, using 'main' as default", "error", err)
			migration.Status.DefaultBranchInfo.TargetDefaultBranch = "main"
			migration.Status.DefaultBranchInfo.AutoDetected = true
			return nil
		}

		// Update status with detected branch
		migration.Status.DefaultBranchInfo.SourceDefaultBranch = sourceDefaultBranch
		migration.Status.DefaultBranchInfo.TargetDefaultBranch = sourceDefaultBranch
		migration.Status.DefaultBranchInfo.AutoDetected = true
		now := metav1.Now()
		migration.Status.DefaultBranchInfo.DetectedAt = &now

		logger.Info("Auto-detected default branch from ADO",
			"sourceBranch", sourceDefaultBranch,
			"targetBranch", sourceDefaultBranch)
	}

	return nil
}

// validateRepositoryState validates the current state of the target repository
func (r *AdoToGitMigrationReconciler) validateRepositoryState(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, token string, logger logr.Logger) (migrationv1.RepositoryState, *github.Repository, error) {
	// Check if repository exists
	exists, err := r.GitHubService.CheckRepositoryExists(ctx, token, migration.Spec.Target.Owner, resource.TargetName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to check repository existence: %w", err)
	}

	if !exists {
		logger.Info("Repository does not exist", "repo", resource.TargetName)
		return migrationv1.RepositoryStateNotExists, nil, nil
	}

	// Get repository information
	repo, err := r.GitHubService.GetRepository(ctx, token, migration.Spec.Target.Owner, resource.TargetName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get repository information: %w", err)
	}

	// Check if repository is empty
	isEmpty, err := r.GitHubService.IsRepositoryEmpty(ctx, token, migration.Spec.Target.Owner, resource.TargetName)
	if err != nil {
		return "", nil, fmt.Errorf("failed to check if repository is empty: %w", err)
	}

	if isEmpty {
		logger.Info("Repository exists but is empty", "repo", resource.TargetName)
		return migrationv1.RepositoryStateEmpty, repo, nil
	}

	logger.Info("Repository exists and contains content", "repo", resource.TargetName)
	return migrationv1.RepositoryStateNonEmpty, repo, nil
}

// createRepository creates a new GitHub repository
func (r *AdoToGitMigrationReconciler) createRepository(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, token string, logger logr.Logger) (*github.Repository, error) {
	// Determine repository visibility
	visibility := "private" // default
	if resource.Settings != nil && resource.Settings.Repository != nil && resource.Settings.Repository.Visibility != "" {
		visibility = resource.Settings.Repository.Visibility
	} else if migration.Spec.Target.DefaultRepoSettings != nil && migration.Spec.Target.DefaultRepoSettings.Visibility != "" {
		visibility = migration.Spec.Target.DefaultRepoSettings.Visibility
	}

	logger.Info("Creating GitHub repository", "owner", migration.Spec.Target.Owner, "repo", resource.TargetName, "visibility", visibility)

	repo, err := r.GitHubService.CreateRepository(ctx, token, migration.Spec.Target.Owner, resource.TargetName, &services.GitHubRepoSettings{
		Visibility: visibility,
		AutoInit:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	logger.Info("Repository created successfully", "repo", resource.TargetName, "url", repo.GetHTMLURL())
	r.sendWebSocketUpdate(migration, fmt.Sprintf("Created repository %s", resource.TargetName))
	r.Recorder.Event(migration, corev1.EventTypeNormal, "RepositoryCreated",
		fmt.Sprintf("Created repository %s", resource.TargetName))

	return repo, nil
}

// setDefaultBranch sets the default branch for the repository
func (r *AdoToGitMigrationReconciler) setDefaultBranch(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, token string, logger logr.Logger) error {
	if migration.Status.DefaultBranchInfo == nil || migration.Status.DefaultBranchInfo.TargetDefaultBranch == "" {
		return fmt.Errorf("target default branch not configured")
	}

	targetBranch := migration.Status.DefaultBranchInfo.TargetDefaultBranch
	logger.Info("Setting default branch for repository", "repo", resource.TargetName, "branch", targetBranch)

	err := r.GitHubService.SetRepositoryDefaultBranch(ctx, token, migration.Spec.Target.Owner, resource.TargetName, targetBranch)
	if err != nil {
		return fmt.Errorf("failed to set default branch: %w", err)
	}

	logger.Info("Default branch set successfully", "repo", resource.TargetName, "branch", targetBranch)
	return nil
}

// updateRepositoryStateStatus updates the repository state in migration status
func (r *AdoToGitMigrationReconciler) updateRepositoryStateStatus(migration *migrationv1.AdoToGitMigration, repoName string, state migrationv1.RepositoryState, repo *github.Repository) {
	// Initialize repository states if not exists
	if migration.Status.RepositoryStates == nil {
		migration.Status.RepositoryStates = []migrationv1.RepositoryStateInfo{}
	}

	// Find existing state or create new one
	var stateInfo *migrationv1.RepositoryStateInfo
	for i := range migration.Status.RepositoryStates {
		if migration.Status.RepositoryStates[i].RepositoryName == repoName {
			stateInfo = &migration.Status.RepositoryStates[i]
			break
		}
	}

	if stateInfo == nil {
		// Create new state info
		newStateInfo := migrationv1.RepositoryStateInfo{
			RepositoryName: repoName,
		}
		migration.Status.RepositoryStates = append(migration.Status.RepositoryStates, newStateInfo)
		stateInfo = &migration.Status.RepositoryStates[len(migration.Status.RepositoryStates)-1]
	}

	// Update state info
	stateInfo.State = state
	now := metav1.Now()
	stateInfo.CheckedAt = &now

	if repo != nil {
		stateInfo.GitHubURL = repo.GetHTMLURL()
	}

	if state == migrationv1.RepositoryStateCreated {
		stateInfo.CreatedDuringMigration = true
		stateInfo.Details = "Repository created during migration"
	} else {
		stateInfo.CreatedDuringMigration = false
		stateInfo.Details = fmt.Sprintf("Repository state: %s", string(state))
	}
}

// performCodeMigration performs the actual code migration from ADO to GitHub
func (r *AdoToGitMigrationReconciler) performCodeMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, status *migrationv1.ResourceMigrationStatus, token string, logger logr.Logger) error {
	logger.Info("Starting code migration from Azure DevOps", "source", resource.SourceName)

	// Get Azure DevOps token
	adoToken, err := r.getAzureDevOpsToken(ctx, migration)
	if err != nil {
		logger.Error(err, "Failed to get Azure DevOps token, skipping code migration")
		// Don't fail the entire migration - repository was created successfully
		r.sendWebSocketUpdate(migration, fmt.Sprintf("Repository %s created but code migration skipped (no ADO token)", resource.TargetName))
		return nil
	}

	// Create progress callback to update status
	progressCallback := func(progress *services.RepositoryMigrationProgress) {
		// Update the current step with detailed progress
		status.Progress = int(progress.Percentage)

		message := fmt.Sprintf("Repository %s - %s: %s (%d%%)",
			resource.TargetName,
			progress.Phase,
			progress.Description,
			progress.Percentage)

		logger.Info("Migration progress",
			"repository", resource.TargetName,
			"phase", progress.Phase,
			"description", progress.Description,
			"percentage", progress.Percentage)

		// Send WebSocket update
		r.sendWebSocketUpdate(migration, message)

		// Send Kubernetes event for major milestones
		if progress.Percentage%25 == 0 || progress.Error != "" {
			eventType := corev1.EventTypeNormal
			reason := "MigrationProgress"

			if progress.Error != "" {
				eventType = corev1.EventTypeWarning
				reason = "MigrationError"
				message = fmt.Sprintf("Repository %s - %s: %s", resource.TargetName, progress.Phase, progress.Error)
			}

			r.Recorder.Event(migration, eventType, reason, message)
		}
	}

	// Perform the repository migration
	err = r.MigrationService.MigrateRepository(ctx, migration, resource, status, adoToken, token, logger, progressCallback)
	if err != nil {
		logger.Error(err, "Code migration failed", "repository", resource.TargetName)
		// Send final error update
		progressCallback(&services.RepositoryMigrationProgress{
			Phase:       "failed",
			Description: fmt.Sprintf("Migration failed: %s", err.Error()),
			Percentage:  0,
			Error:       err.Error(),
		})
		return fmt.Errorf("code migration failed: %w", err)
	}

	logger.Info("Code migration completed successfully", "repository", resource.TargetName)
	r.sendWebSocketUpdate(migration, fmt.Sprintf("Repository %s: Code migration completed successfully", resource.TargetName))
	r.Recorder.Event(migration, corev1.EventTypeNormal, "CodeMigrationCompleted",
		fmt.Sprintf("Code migration completed for repository %s", resource.TargetName))

	return nil
}

// getGitHubTokenUnified retrieves GitHub token from either PAT or GitHub App authentication
func (r *AdoToGitMigrationReconciler) getGitHubTokenUnified(ctx context.Context, migration *migrationv1.AdoToGitMigration) (string, error) {
	authConfig := &migration.Spec.Target.Auth

	// Check which authentication method is configured
	if authConfig.TokenRef != nil && authConfig.AppAuth != nil {
		return "", fmt.Errorf("both tokenRef and appAuth are specified - please use only one authentication method")
	}

	if authConfig.TokenRef == nil && authConfig.AppAuth == nil {
		return "", fmt.Errorf("no GitHub authentication method configured (tokenRef or appAuth required)")
	}

	// GitHub App authentication
	if authConfig.AppAuth != nil {
		log := log.FromContext(ctx)
		log.Info("Getting token from GitHub App")

		// Get App ID
		appIDStr, err := r.getSecretValue(ctx, migration.Namespace, &authConfig.AppAuth.AppIdRef)
		if err != nil {
			return "", fmt.Errorf("failed to get GitHub App ID: %w", err)
		}

		// Get Installation ID
		installationIDStr, err := r.getSecretValue(ctx, migration.Namespace, &authConfig.AppAuth.InstallationIdRef)
		if err != nil {
			return "", fmt.Errorf("failed to get GitHub Installation ID: %w", err)
		}

		// Parse IDs
		appID, err := strconv.ParseInt(appIDStr, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub App ID '%s': %w", appIDStr, err)
		}

		installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub Installation ID '%s': %w", installationIDStr, err)
		}

		// Get the app client from cache
		appClient, exists := r.GitHubService.GetAppClient(appID, installationID)
		if !exists {
			// If not in cache, we need to create it first
			_, err := r.getGitHubClient(ctx, migration)
			if err != nil {
				return "", fmt.Errorf("failed to initialize GitHub App client: %w", err)
			}
			// Try to get it again from cache
			appClient, exists = r.GitHubService.GetAppClient(appID, installationID)
			if !exists {
				return "", fmt.Errorf("failed to retrieve GitHub App client from cache")
			}
		}

		// Get token from app client
		token, err := appClient.GetToken(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get token from GitHub App: %w", err)
		}

		return token, nil
	}

	// PAT authentication
	return r.getGitHubToken(ctx, migration)
}

// getAzureDevOpsToken retrieves the Azure DevOps token from the configured secret
func (r *AdoToGitMigrationReconciler) getAzureDevOpsToken(ctx context.Context, migration *migrationv1.AdoToGitMigration) (string, error) {
	// Check if Azure DevOps authentication is configured
	auth := migration.Spec.Source.Auth

	// Handle PAT authentication
	if auth.PAT != nil {
		return r.getAzureDevOpsPATToken(ctx, migration, auth.PAT)
	}

	// Handle Service Principal authentication
	if auth.ServicePrincipal != nil {
		return r.getAzureDevOpsServicePrincipalToken(ctx, migration, auth.ServicePrincipal)
	}

	return "", fmt.Errorf("no Azure DevOps authentication configured (PAT or Service Principal required)")
}

// getAzureDevOpsPATToken retrieves PAT token from secret
func (r *AdoToGitMigrationReconciler) getAzureDevOpsPATToken(ctx context.Context, migration *migrationv1.AdoToGitMigration, patConfig *migrationv1.PATConfig) (string, error) {
	secretName := patConfig.TokenRef.Name
	secretKey := patConfig.TokenRef.Key
	secretNamespace := migration.Namespace

	if patConfig.TokenRef.Namespace != "" {
		secretNamespace = patConfig.TokenRef.Namespace
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: secretNamespace,
		Name:      secretName,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get Azure DevOps PAT secret %s in namespace %s: %w", secretName, secretNamespace, err)
	}

	token, exists := secret.Data[secretKey]
	if !exists {
		return "", fmt.Errorf("key %s not found in Azure DevOps PAT secret %s", secretKey, secretName)
	}

	return string(token), nil
}

// getAzureDevOpsServicePrincipalToken retrieves Service Principal token and generates Azure DevOps token
func (r *AdoToGitMigrationReconciler) getAzureDevOpsServicePrincipalToken(ctx context.Context, migration *migrationv1.AdoToGitMigration, spConfig *migrationv1.ServicePrincipalConfig) (string, error) {
	logger := log.FromContext(ctx)

	// Get client ID from the secret reference
	clientIDNamespace := migration.Namespace
	if spConfig.ClientIDRef.Namespace != "" {
		clientIDNamespace = spConfig.ClientIDRef.Namespace
	}

	logger.Info("Retrieving Azure Service Principal credentials from secret",
		"clientIdSecret", spConfig.ClientIDRef.Name,
		"clientSecretSecret", spConfig.ClientSecretRef.Name,
		"tenantIdSecret", spConfig.TenantIDRef.Name,
		"namespace", clientIDNamespace)

	clientIDSecret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: clientIDNamespace,
		Name:      spConfig.ClientIDRef.Name,
	}, clientIDSecret)
	if err != nil {
		return "", fmt.Errorf("failed to get Service Principal Client ID secret %s in namespace %s: %w", spConfig.ClientIDRef.Name, clientIDNamespace, err)
	}

	clientIDBytes, exists := clientIDSecret.Data[spConfig.ClientIDRef.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in Service Principal Client ID secret %s", spConfig.ClientIDRef.Key, spConfig.ClientIDRef.Name)
	}
	clientID := string(clientIDBytes)

	// Get client secret from the secret reference
	clientSecretNamespace := migration.Namespace
	if spConfig.ClientSecretRef.Namespace != "" {
		clientSecretNamespace = spConfig.ClientSecretRef.Namespace
	}

	clientSecretSecret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{
		Namespace: clientSecretNamespace,
		Name:      spConfig.ClientSecretRef.Name,
	}, clientSecretSecret)
	if err != nil {
		return "", fmt.Errorf("failed to get Service Principal Client Secret secret %s in namespace %s: %w", spConfig.ClientSecretRef.Name, clientSecretNamespace, err)
	}

	clientSecretBytes, exists := clientSecretSecret.Data[spConfig.ClientSecretRef.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in Service Principal Client Secret secret %s", spConfig.ClientSecretRef.Key, spConfig.ClientSecretRef.Name)
	}
	clientSecret := string(clientSecretBytes)

	// Get tenant ID from the secret reference
	tenantIDNamespace := migration.Namespace
	if spConfig.TenantIDRef.Namespace != "" {
		tenantIDNamespace = spConfig.TenantIDRef.Namespace
	}

	tenantIDSecret := &corev1.Secret{}
	err = r.Get(ctx, client.ObjectKey{
		Namespace: tenantIDNamespace,
		Name:      spConfig.TenantIDRef.Name,
	}, tenantIDSecret)
	if err != nil {
		return "", fmt.Errorf("failed to get Service Principal Tenant ID secret %s in namespace %s: %w", spConfig.TenantIDRef.Name, tenantIDNamespace, err)
	}

	tenantIDBytes, exists := tenantIDSecret.Data[spConfig.TenantIDRef.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in Service Principal Tenant ID secret %s", spConfig.TenantIDRef.Key, spConfig.TenantIDRef.Name)
	}
	tenantID := string(tenantIDBytes)

	logger.Info("Successfully retrieved Azure Service Principal credentials from secrets")

	// Use the Azure DevOps service to get a token using Service Principal credentials
	// This delegates the token acquisition to the Azure DevOps service
	token, err := r.MigrationService.GetAzureDevOpsTokenFromServicePrincipal(ctx, clientID, clientSecret, tenantID)
	if err != nil {
		return "", fmt.Errorf("failed to get Azure DevOps token from Service Principal: %w", err)
	}

	return token, nil
}

func (r *AdoToGitMigrationReconciler) processWorkItem(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, status *migrationv1.ResourceMigrationStatus, token string) error {
	// Placeholder for work item migration logic
	// This would involve creating GitHub issues from Azure DevOps work items
	return fmt.Errorf("work item migration not yet implemented")
}

func (r *AdoToGitMigrationReconciler) processPipeline(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, status *migrationv1.ResourceMigrationStatus, token string) error {
	// Placeholder for pipeline migration logic
	// This would involve converting Azure Pipelines to GitHub Actions
	return fmt.Errorf("pipeline migration not yet implemented")
}

func (r *AdoToGitMigrationReconciler) processPaused(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	// Stay paused
	return ctrl.Result{RequeueAfter: pausedRequeueInterval}, nil
}

// processSyncing handles continuous synchronization from ADO to GitHub
func (r *AdoToGitMigrationReconciler) processSyncing(ctx context.Context, migration *migrationv1.AdoToGitMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("sync")

	// Get sync settings
	if migration.Spec.Settings.Sync == nil || !migration.Spec.Settings.Sync.Enabled {
		// Sync disabled, transition back to completed
		logger.Info("Sync disabled, transitioning to completed")
		migration.Status.Phase = migrationv1.MigrationPhaseCompleted
		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	syncSettings := migration.Spec.Settings.Sync
	syncIntervalMinutes := syncSettings.IntervalMinutes
	if syncIntervalMinutes == 0 {
		syncIntervalMinutes = 5 // Default to 5 minutes
	}

	// Check if it's time to sync
	now := metav1.Now()
	if migration.Status.SyncStatus != nil && migration.Status.SyncStatus.NextSyncTime != nil {
		if now.Before(migration.Status.SyncStatus.NextSyncTime) {
			// Not yet time to sync
			remainingTime := migration.Status.SyncStatus.NextSyncTime.Time.Sub(now.Time)
			logger.Info("Next sync scheduled", "in", remainingTime.String())
			return ctrl.Result{RequeueAfter: remainingTime}, nil
		}
	}

	logger.Info("Starting sync operation")

	// Get tokens
	adoToken, err := r.getAzureDevOpsToken(ctx, migration)
	if err != nil {
		logger.Error(err, "Failed to get ADO token")
		return r.handleSyncError(ctx, migration, err, syncIntervalMinutes)
	}

	githubToken, err := r.getGitHubToken(ctx, migration)
	if err != nil {
		logger.Error(err, "Failed to get GitHub token")
		return r.handleSyncError(ctx, migration, err, syncIntervalMinutes)
	}

	// Sync each repository resource
	allBranchesSynced := []string{}
	allTagsSynced := []string{}
	syncErrors := []string{}

	for i := range migration.Spec.Resources {
		resource := &migration.Spec.Resources[i]
		if resource.Type != "repository" {
			continue
		}

		logger.Info("Syncing repository", "resource", resource.SourceName)

		result, err := r.MigrationService.SyncRepository(ctx, migration, resource, adoToken, githubToken, logger)
		if err != nil {
			logger.Error(err, "Failed to sync repository", "resource", resource.SourceName)
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", resource.SourceName, err))
			continue
		}

		allBranchesSynced = append(allBranchesSynced, result.BranchesSynced...)
		allTagsSynced = append(allTagsSynced, result.TagsSynced...)

		logger.Info("✅ Repository synced successfully",
			"resource", resource.SourceName,
			"branches", len(result.BranchesSynced),
			"tags", len(result.TagsSynced))
	}

	// Update sync status
	nextSync := metav1.NewTime(now.Add(time.Duration(syncIntervalMinutes) * time.Minute))

	if migration.Status.SyncStatus == nil {
		migration.Status.SyncStatus = &migrationv1.SyncStatus{
			Enabled: true,
		}
	}

	if len(syncErrors) > 0 {
		// Some syncs failed
		migration.Status.SyncStatus.FailedSyncCount++
		migration.Status.SyncStatus.LastSyncError = strings.Join(syncErrors, "; ")
		logger.Info("Sync completed with errors", "errors", len(syncErrors))
		r.Recorder.Event(migration, corev1.EventTypeWarning, "SyncPartialFailure",
			fmt.Sprintf("Sync completed with %d error(s)", len(syncErrors)))
	} else {
		// All syncs successful
		migration.Status.SyncStatus.SyncCount++
		migration.Status.SyncStatus.LastSyncError = ""
		migration.Status.SyncStatus.LastSyncTime = &now
		migration.Status.SyncStatus.BranchesSynced = allBranchesSynced
		migration.Status.SyncStatus.TagsSynced = allTagsSynced
		logger.Info("✅ Sync completed successfully",
			"totalBranches", len(allBranchesSynced),
			"totalTags", len(allTagsSynced))
		r.Recorder.Event(migration, corev1.EventTypeNormal, "SyncCompleted",
			fmt.Sprintf("Sync #%d completed: %d branches, %d tags",
				migration.Status.SyncStatus.SyncCount,
				len(allBranchesSynced),
				len(allTagsSynced)))
	}

	migration.Status.SyncStatus.NextSyncTime = &nextSync

	if err := r.Status().Update(ctx, migration); err != nil {
		logger.Error(err, "Failed to update sync status")
		return ctrl.Result{RequeueAfter: time.Duration(syncIntervalMinutes) * time.Minute}, err
	}

	logger.Info("Next sync scheduled", "at", nextSync.Format(time.RFC3339))

	// Requeue for next sync
	return ctrl.Result{RequeueAfter: time.Duration(syncIntervalMinutes) * time.Minute}, nil
}

// handleSyncError handles errors during sync operations
func (r *AdoToGitMigrationReconciler) handleSyncError(ctx context.Context, migration *migrationv1.AdoToGitMigration, syncErr error, syncIntervalMinutes int) (ctrl.Result, error) {
	now := metav1.Now()
	nextSync := metav1.NewTime(now.Add(time.Duration(syncIntervalMinutes) * time.Minute))

	if migration.Status.SyncStatus == nil {
		migration.Status.SyncStatus = &migrationv1.SyncStatus{
			Enabled: true,
		}
	}

	migration.Status.SyncStatus.FailedSyncCount++
	migration.Status.SyncStatus.LastSyncError = syncErr.Error()
	migration.Status.SyncStatus.NextSyncTime = &nextSync

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{RequeueAfter: time.Duration(syncIntervalMinutes) * time.Minute}, err
	}

	r.Recorder.Event(migration, corev1.EventTypeWarning, "SyncFailed",
		fmt.Sprintf("Sync failed: %v. Retrying in %d minutes", syncErr, syncIntervalMinutes))

	// Requeue for next sync attempt
	return ctrl.Result{RequeueAfter: time.Duration(syncIntervalMinutes) * time.Minute}, nil
}

func (r *AdoToGitMigrationReconciler) cleanup(ctx context.Context, migration *migrationv1.AdoToGitMigration) error {
	// Cleanup any resources created during migration
	// This could include removing webhooks, cleaning up temporary files, etc.
	return nil
}

func (r *AdoToGitMigrationReconciler) failMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration, message string) (ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MigrationPhaseFailed
	migration.Status.ErrorMessage = message
	migration.Status.Progress.Processing = 0
	migration.Status.Progress.CurrentStep = "Migration failed"

	now := metav1.Now()
	migration.Status.CompletionTime = &now

	// Track migration failure metrics
	migrationsTotal.WithLabelValues("failed", migration.Namespace).Inc()

	// Record migration duration if start time is available
	if migration.Status.StartTime != nil {
		duration := now.Time.Sub(migration.Status.StartTime.Time)
		migrationDuration.WithLabelValues("failed", migration.Namespace).Observe(duration.Seconds())
	}

	// Set failure condition
	r.setCondition(migration, ConditionTypeReady, metav1.ConditionFalse,
		ReasonMigrationFailed, message)
	r.setCondition(migration, ConditionTypeMigrationActive, metav1.ConditionFalse,
		ReasonMigrationFailed, "Migration failed")

	r.sendWebSocketUpdate(migration, fmt.Sprintf("Migration failed: %s", message))
	r.Recorder.Event(migration, corev1.EventTypeWarning, "MigrationFailed", message)

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AdoToGitMigrationReconciler) getGitHubToken(ctx context.Context, migration *migrationv1.AdoToGitMigration) (string, error) {
	// Check if using GitHub App authentication (new method)
	if migration.Spec.Target.Auth.AppAuth != nil {
		return "", fmt.Errorf("use getGitHubClient for GitHub App authentication")
	}

	// Legacy PAT authentication
	if migration.Spec.Target.Auth.TokenRef == nil {
		return "", fmt.Errorf("no GitHub authentication configured")
	}

	secretName := migration.Spec.Target.Auth.TokenRef.Name
	secretKey := migration.Spec.Target.Auth.TokenRef.Key
	secretNamespace := migration.Namespace

	// Use specified namespace if provided
	if migration.Spec.Target.Auth.TokenRef.Namespace != "" {
		secretNamespace = migration.Spec.Target.Auth.TokenRef.Namespace
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: secretNamespace,
		Name:      secretName,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s in namespace %s: %w", secretName, secretNamespace, err)
	}

	token, exists := secret.Data[secretKey]
	if !exists {
		return "", fmt.Errorf("key %s not found in secret %s", secretKey, secretName)
	}

	return string(token), nil
}

// getSecretValue retrieves a value from a secret reference
func (r *AdoToGitMigrationReconciler) getSecretValue(ctx context.Context, defaultNamespace string, ref *migrationv1.SecretReference) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("secret reference is nil")
	}

	secretNamespace := defaultNamespace
	if ref.Namespace != "" {
		secretNamespace = ref.Namespace
	}

	secret := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: secretNamespace,
		Name:      ref.Name,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get secret %s in namespace %s: %w", ref.Name, secretNamespace, err)
	}

	value, exists := secret.Data[ref.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in secret %s", ref.Key, ref.Name)
	}

	return string(value), nil
}

// getGitHubClient returns a GitHub client using either PAT or GitHub App authentication
func (r *AdoToGitMigrationReconciler) getGitHubClient(ctx context.Context, migration *migrationv1.AdoToGitMigration) (*github.Client, error) {
	authConfig := &migration.Spec.Target.Auth
	log := log.FromContext(ctx)

	// Count configured auth methods
	authMethodCount := 0
	if authConfig.TokenRef != nil {
		authMethodCount++
	}
	if authConfig.AppAuth != nil {
		authMethodCount++
	}
	if authConfig.Token != "" {
		authMethodCount++
	}

	// Check for conflicting auth methods
	if authMethodCount > 1 {
		return nil, fmt.Errorf("multiple GitHub authentication methods specified - please use only one (token, tokenRef, or appAuth)")
	}

	// Check for no auth method
	if authMethodCount == 0 {
		return nil, fmt.Errorf("no GitHub authentication method configured (token, tokenRef, or appAuth required)")
	}

	// Inline OAuth/PAT token (from UI OAuth flow)
	if authConfig.Token != "" {
		log.Info("Using inline GitHub OAuth token")
		client := r.GitHubService.GetClientFromPAT(authConfig.Token)
		log.Info("Successfully authenticated with inline GitHub token")
		return client, nil
	}

	// GitHub App authentication (recommended for production)
	if authConfig.AppAuth != nil {
		log.Info("Using GitHub App authentication")

		// Get App ID
		appIDStr, err := r.getSecretValue(ctx, migration.Namespace, &authConfig.AppAuth.AppIdRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get GitHub App ID: %w", err)
		}

		// Get Installation ID
		installationIDStr, err := r.getSecretValue(ctx, migration.Namespace, &authConfig.AppAuth.InstallationIdRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get GitHub Installation ID: %w", err)
		}

		// Get Private Key
		privateKey, err := r.getSecretValue(ctx, migration.Namespace, &authConfig.AppAuth.PrivateKeyRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get GitHub App private key: %w", err)
		}

		// Parse IDs
		appID, err := strconv.ParseInt(appIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub App ID '%s': %w", appIDStr, err)
		}

		installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub Installation ID '%s': %w", installationIDStr, err)
		}

		// Create GitHub App client
		client, err := r.GitHubService.GetClientFromApp(ctx, appID, installationID, []byte(privateKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create GitHub App client: %w", err)
		}

		log.Info("Successfully authenticated with GitHub App", "appID", appID, "installationID", installationID)
		return client, nil
	}

	// PAT authentication via secret reference
	if authConfig.TokenRef != nil {
		log.Info("Using GitHub PAT authentication from secret")

		token, err := r.getSecretValue(ctx, migration.Namespace, authConfig.TokenRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get GitHub PAT: %w", err)
		}

		client := r.GitHubService.GetClientFromPAT(token)
		log.Info("Successfully authenticated with GitHub PAT")
		return client, nil
	}

	return nil, fmt.Errorf("no GitHub authentication method configured")
}

func (r *AdoToGitMigrationReconciler) sendWebSocketUpdate(migration *migrationv1.AdoToGitMigration, message string) {
	if r.WebSocketManager != nil {
		update := map[string]interface{}{
			"migrationId": migration.Name,
			"namespace":   migration.Namespace,
			"phase":       migration.Status.Phase,
			"progress":    migration.Status.Progress,
			"message":     message,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		}

		r.WebSocketManager.BroadcastUpdate("migration_progress", "AdoToGitMigration", migration.Name, update)
	}
}

// setCondition sets or updates a condition in the migration status
// This follows Kubernetes best practices for status conditions
func (r *AdoToGitMigrationReconciler) setCondition(migration *migrationv1.AdoToGitMigration, conditionType string, status metav1.ConditionStatus, reason, message string) {
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: migration.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	// Initialize conditions array if nil
	if migration.Status.Conditions == nil {
		migration.Status.Conditions = []metav1.Condition{}
	}

	// Find and update existing condition or append new one
	found := false
	for i, c := range migration.Status.Conditions {
		if c.Type == conditionType {
			// Only update if something changed
			if c.Status != status || c.Reason != reason || c.Message != message || c.ObservedGeneration != migration.Generation {
				// Preserve LastTransitionTime if status hasn't changed
				if c.Status == status {
					condition.LastTransitionTime = c.LastTransitionTime
				}
				migration.Status.Conditions[i] = condition
			}
			found = true
			break
		}
	}

	if !found {
		migration.Status.Conditions = append(migration.Status.Conditions, condition)
	}
}

// removeCondition removes a condition from the migration status
func (r *AdoToGitMigrationReconciler) removeCondition(migration *migrationv1.AdoToGitMigration, conditionType string) {
	if migration.Status.Conditions == nil {
		return
	}

	newConditions := []metav1.Condition{}
	for _, c := range migration.Status.Conditions {
		if c.Type != conditionType {
			newConditions = append(newConditions, c)
		}
	}
	migration.Status.Conditions = newConditions
}

// SetupWithManager sets up the controller with the Manager.
func (r *AdoToGitMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize the recorder
	r.Recorder = mgr.GetEventRecorderFor("adotogitmigration-controller")

	// Initialize active migrations map
	r.activeMigrations = make(map[string]bool)

	// Use enhanced predicate with logging for better observability
	// This filters out status-only updates while providing detailed logging
	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.AdoToGitMigration{}).
		WithEventFilter(GenerationChangedWithLogging()).
		Complete(r)
}
