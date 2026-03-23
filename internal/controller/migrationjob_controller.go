package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	"github.com/tesserix/reposhift/internal/services"
)

// MigrationJobReconciler reconciles a MigrationJob object
type MigrationJobReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	MigrationService *services.MigrationService
	Recorder         record.EventRecorder
}

//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=migrationjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=migrationjobs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=migrationjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=repositorymigrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=workitemmigrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MigrationJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling MigrationJob", "namespacedName", req.NamespacedName)

	// Fetch the MigrationJob instance
	migrationJob := &migrationv1.MigrationJob{}
	err := r.Get(ctx, req.NamespacedName, migrationJob)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("MigrationJob resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get MigrationJob")
		return ctrl.Result{}, err
	}

	// Add finalizer if not present
	finalizerName := "migration.ado-to-git-migration.io/finalizer"
	if migrationJob.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(migrationJob, finalizerName) {
			controllerutil.AddFinalizer(migrationJob, finalizerName)
			if err := r.Update(ctx, migrationJob); err != nil {
				log.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Added finalizer to migration job")
			return ctrl.Result{}, nil
		}
	} else {
		// Handle deletion
		if controllerutil.ContainsFinalizer(migrationJob, finalizerName) {
			// Perform cleanup
			if err := r.cleanup(ctx, migrationJob); err != nil {
				log.Error(err, "Failed to clean up migration job resources")
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(migrationJob, finalizerName)
			if err := r.Update(ctx, migrationJob); err != nil {
				log.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Removed finalizer from migration job")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	// Initialize status if needed
	if migrationJob.Status.Phase == "" {
		// Use MigrationJobPhase types
		migrationJob.Status.Phase = migrationv1.MigrationJobPhasePending
		migrationJob.Status.Progress = migrationv1.MigrationJobProgress{
			Total:      len(migrationJob.Spec.Resources),
			Completed:  0,
			Failed:     0,
			Processing: 0,
			Percentage: 0,
		}

		// Set start time
		now := metav1.Now()
		migrationJob.Status.StartTime = &now

		r.Recorder.Event(migrationJob, corev1.EventTypeNormal, "Initialized", "Migration job initialized")

		if err := r.Status().Update(ctx, migrationJob); err != nil {
			log.Error(err, "Failed to update migration job status")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Process based on current phase
	switch migrationJob.Status.Phase {
	case migrationv1.MigrationJobPhasePending:
		return r.processPending(ctx, migrationJob)
	case migrationv1.MigrationJobPhaseValidating:
		return r.processValidating(ctx, migrationJob)
	case migrationv1.MigrationJobPhaseRunning:
		return r.processRunning(ctx, migrationJob)
	case migrationv1.MigrationJobPhaseCompleted, migrationv1.MigrationJobPhaseFailed, migrationv1.MigrationJobPhaseCancelled:
		// No action needed for terminal states
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *MigrationJobReconciler) processPending(ctx context.Context, migrationJob *migrationv1.MigrationJob) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Processing pending migration job", "name", migrationJob.Name)

	// Move to validation phase
	migrationJob.Status.Phase = migrationv1.MigrationJobPhaseValidating
	migrationJob.Status.Progress.CurrentStep = "Validating configuration"

	r.Recorder.Event(migrationJob, corev1.EventTypeNormal, "ValidationStarted", "Migration job validation started")

	if err := r.Status().Update(ctx, migrationJob); err != nil {
		log.Error(err, "Failed to update migration job status")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *MigrationJobReconciler) processValidating(ctx context.Context, migrationJob *migrationv1.MigrationJob) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Validating migration job configuration", "name", migrationJob.Name)

	// Validate configuration
	if err := r.validateConfiguration(ctx, migrationJob); err != nil {
		migrationJob.Status.Phase = migrationv1.MigrationJobPhaseFailed
		migrationJob.Status.ErrorMessage = err.Error()

		r.Recorder.Event(migrationJob, corev1.EventTypeWarning, "ValidationFailed",
			fmt.Sprintf("Migration job validation failed: %v", err))

		if err := r.Status().Update(ctx, migrationJob); err != nil {
			log.Error(err, "Failed to update migration job status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Start the migration
	migrationJob.Status.Phase = migrationv1.MigrationJobPhaseRunning
	migrationJob.Status.Progress.CurrentStep = "Creating child migrations"

	r.Recorder.Event(migrationJob, corev1.EventTypeNormal, "ValidationPassed", "Migration job validation passed")

	if err := r.Status().Update(ctx, migrationJob); err != nil {
		log.Error(err, "Failed to update migration job status")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, err
	}

	// Create child migration resources
	if err := r.createChildMigrations(ctx, migrationJob); err != nil {
		migrationJob.Status.Phase = migrationv1.MigrationJobPhaseFailed
		migrationJob.Status.ErrorMessage = fmt.Sprintf("Failed to create child migrations: %v", err)

		r.Recorder.Event(migrationJob, corev1.EventTypeWarning, "ChildMigrationCreationFailed",
			fmt.Sprintf("Failed to create child migrations: %v", err))

		if err := r.Status().Update(ctx, migrationJob); err != nil {
			log.Error(err, "Failed to update migration job status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	r.Recorder.Event(migrationJob, corev1.EventTypeNormal, "ChildMigrationsCreated", "Child migrations created successfully")

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *MigrationJobReconciler) processRunning(ctx context.Context, migrationJob *migrationv1.MigrationJob) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Processing running migration job", "name", migrationJob.Name)

	// Check status of child migrations
	progress, err := r.checkChildMigrationProgress(ctx, migrationJob)
	if err != nil {
		log.Error(err, "Failed to check child migration progress")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	migrationJob.Status.Progress = progress
	migrationJob.Status.Progress.CurrentStep = fmt.Sprintf("Processing migrations (%d/%d complete)",
		progress.Completed, progress.Total)

	// Determine if migration is complete
	if progress.Completed+progress.Failed+progress.Skipped == progress.Total {
		if progress.Failed == 0 {
			migrationJob.Status.Phase = migrationv1.MigrationJobPhaseCompleted
			migrationJob.Status.Progress.CurrentStep = "Migration job completed successfully"

			r.Recorder.Event(migrationJob, corev1.EventTypeNormal, "MigrationCompleted",
				"Migration job completed successfully")
		} else {
			migrationJob.Status.Phase = migrationv1.MigrationJobPhaseFailed
			migrationJob.Status.ErrorMessage = fmt.Sprintf("%d resources failed to migrate", progress.Failed)

			r.Recorder.Event(migrationJob, corev1.EventTypeWarning, "MigrationFailed",
				fmt.Sprintf("Migration job completed with %d failed resources", progress.Failed))
		}

		now := metav1.Now()
		migrationJob.Status.CompletionTime = &now

		// Calculate statistics
		migrationJob.Status.Statistics = r.calculateStatistics(ctx, migrationJob)

		if err := r.Status().Update(ctx, migrationJob); err != nil {
			log.Error(err, "Failed to update migration job status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Update status and continue processing
	if err := r.Status().Update(ctx, migrationJob); err != nil {
		log.Error(err, "Failed to update migration job status")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *MigrationJobReconciler) validateConfiguration(ctx context.Context, migrationJob *migrationv1.MigrationJob) error {
	// Validate Azure DevOps configuration
	if migrationJob.Spec.AzureDevOps.Organization == "" {
		return fmt.Errorf("azure DevOps organization is required")
	}
	if migrationJob.Spec.AzureDevOps.Project == "" {
		return fmt.Errorf("azure DevOps project is required")
	}
	if migrationJob.Spec.AzureDevOps.ServicePrincipal.ClientID == "" {
		return fmt.Errorf("azure DevOps client ID is required")
	}
	if migrationJob.Spec.AzureDevOps.ServicePrincipal.TenantID == "" {
		return fmt.Errorf("azure DevOps tenant ID is required")
	}
	if migrationJob.Spec.AzureDevOps.ServicePrincipal.ClientSecretRef.Name == "" {
		return fmt.Errorf("azure DevOps client secret reference is required")
	}

	// Validate GitHub configuration
	if migrationJob.Spec.GitHub.Owner == "" {
		return fmt.Errorf("github owner is required")
	}
	if migrationJob.Spec.GitHub.TokenRef.Name == "" {
		return fmt.Errorf("github token reference is required")
	}

	// Validate resources
	if len(migrationJob.Spec.Resources) == 0 {
		return fmt.Errorf("at least one resource to migrate is required")
	}

	// Validate secrets exist
	if err := r.validateSecrets(ctx, migrationJob); err != nil {
		return fmt.Errorf("secret validation failed: %w", err)
	}

	return nil
}

func (r *MigrationJobReconciler) validateSecrets(ctx context.Context, migrationJob *migrationv1.MigrationJob) error {
	// Check Azure Service Principal secret
	spSecret := types.NamespacedName{
		Name:      migrationJob.Spec.AzureDevOps.ServicePrincipal.ClientSecretRef.Name,
		Namespace: migrationJob.Namespace,
	}
	if migrationJob.Spec.AzureDevOps.ServicePrincipal.ClientSecretRef.Namespace != "" {
		spSecret.Namespace = migrationJob.Spec.AzureDevOps.ServicePrincipal.ClientSecretRef.Namespace
	}

	var secret corev1.Secret
	if err := r.Get(ctx, spSecret, &secret); err != nil {
		return fmt.Errorf("failed to get Azure Service Principal secret: %w", err)
	}

	// Check GitHub token secret
	ghSecret := types.NamespacedName{
		Name:      migrationJob.Spec.GitHub.TokenRef.Name,
		Namespace: migrationJob.Namespace,
	}
	if migrationJob.Spec.GitHub.TokenRef.Namespace != "" {
		ghSecret.Namespace = migrationJob.Spec.GitHub.TokenRef.Namespace
	}

	if err := r.Get(ctx, ghSecret, &secret); err != nil {
		return fmt.Errorf("failed to get GitHub token secret: %w", err)
	}

	return nil
}

// Simplified version - rest of the methods would be implemented similarly
func (r *MigrationJobReconciler) createChildMigrations(ctx context.Context, migrationJob *migrationv1.MigrationJob) error {
	// Implementation would create child migration resources
	return nil
}

func (r *MigrationJobReconciler) checkChildMigrationProgress(ctx context.Context, migrationJob *migrationv1.MigrationJob) (migrationv1.MigrationJobProgress, error) {
	// Implementation would check child migration statuses
	progress := migrationv1.MigrationJobProgress{
		Total:      len(migrationJob.Spec.Resources),
		Completed:  0,
		Failed:     0,
		Processing: 0,
		Skipped:    0,
		Percentage: 0,
	}
	return progress, nil
}

func (r *MigrationJobReconciler) calculateStatistics(ctx context.Context, migrationJob *migrationv1.MigrationJob) *migrationv1.MigrationJobStatistics {
	stats := &migrationv1.MigrationJobStatistics{
		APICalls: make(map[string]int),
	}

	// Calculate duration
	if migrationJob.Status.StartTime != nil {
		var endTime time.Time
		if migrationJob.Status.CompletionTime != nil {
			endTime = migrationJob.Status.CompletionTime.Time
		} else {
			endTime = time.Now()
		}
		duration := endTime.Sub(migrationJob.Status.StartTime.Time)
		stats.Duration = metav1.Duration{Duration: duration}
	}

	return stats
}

func (r *MigrationJobReconciler) cleanup(ctx context.Context, migrationJob *migrationv1.MigrationJob) error {
	log := log.FromContext(ctx)
	log.Info("Cleaning up migration job resources", "name", migrationJob.Name)

	// We don't need to explicitly delete child resources as they have owner references
	// and will be garbage collected automatically

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *MigrationJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize the recorder
	r.Recorder = mgr.GetEventRecorderFor("migrationjob-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.MigrationJob{}).
		Owns(&migrationv1.AdoToGitMigration{}).
		Owns(&migrationv1.WorkItemMigration{}).
		Complete(r)
}
