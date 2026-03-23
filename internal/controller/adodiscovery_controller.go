package controller

import (
	"context"
	"fmt"
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
	"github.com/tesserix/reposhift/internal/websocket"
)

// AdoDiscoveryReconciler reconciles a AdoDiscovery object
type AdoDiscoveryReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	AzureDevOpsService *services.AzureDevOpsService
	WebSocketManager   *websocket.Manager
	Recorder           record.EventRecorder
}

//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=adodiscoveries,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=adodiscoveries/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=adodiscoveries/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *AdoDiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the AdoDiscovery instance
	discovery := &migrationv1.AdoDiscovery{}
	err := r.Get(ctx, req.NamespacedName, discovery)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("AdoDiscovery resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get AdoDiscovery")
		return ctrl.Result{}, err
	}

	// Add finalizer if not present
	finalizerName := "migration.ado-to-git-migration.io/discovery-finalizer"
	if discovery.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(discovery, finalizerName) {
			controllerutil.AddFinalizer(discovery, finalizerName)
			return ctrl.Result{}, r.Update(ctx, discovery)
		}
	} else {
		// Handle deletion
		if controllerutil.ContainsFinalizer(discovery, finalizerName) {
			controllerutil.RemoveFinalizer(discovery, finalizerName)
			return ctrl.Result{}, r.Update(ctx, discovery)
		}
		return ctrl.Result{}, nil
	}

	// Initialize status if needed
	if discovery.Status.Phase == "" {
		discovery.Status.Phase = migrationv1.DiscoveryPhasePending
		discovery.Status.Progress = migrationv1.DiscoveryProgress{
			CurrentStep:     "Initializing discovery",
			StepsCompleted:  0,
			TotalSteps:      r.calculateTotalSteps(discovery),
			Percentage:      0,
			ItemsDiscovered: 0,
		}

		now := metav1.Now()
		discovery.Status.StartTime = &now

		r.sendWebSocketUpdate(discovery, "Discovery initialized")
		r.Recorder.Event(discovery, corev1.EventTypeNormal, "Initialized", "Discovery initialized")

		return ctrl.Result{RequeueAfter: time.Second * 5}, r.Status().Update(ctx, discovery)
	}

	// Process based on current phase
	switch discovery.Status.Phase {
	case migrationv1.DiscoveryPhasePending:
		return r.processPending(ctx, discovery)
	case migrationv1.DiscoveryPhaseRunning:
		return r.processRunning(ctx, discovery)
	case migrationv1.DiscoveryPhaseCompleted, migrationv1.DiscoveryPhaseFailed, migrationv1.DiscoveryPhaseCancelled:
		// Terminal states - no action needed
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *AdoDiscoveryReconciler) processPending(ctx context.Context, discovery *migrationv1.AdoDiscovery) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Processing pending discovery", "name", discovery.Name)

	// Validate configuration
	if err := r.validateConfiguration(discovery); err != nil {
		discovery.Status.Phase = migrationv1.DiscoveryPhaseFailed
		discovery.Status.ErrorMessage = err.Error()
		r.sendWebSocketUpdate(discovery, "Discovery validation failed")
		r.Recorder.Event(discovery, corev1.EventTypeWarning, "ValidationFailed",
			fmt.Sprintf("Discovery validation failed: %v", err))
		return ctrl.Result{}, r.Status().Update(ctx, discovery)
	}

	// Start discovery
	discovery.Status.Phase = migrationv1.DiscoveryPhaseRunning
	discovery.Status.Progress.CurrentStep = "Starting discovery"

	r.sendWebSocketUpdate(discovery, "Starting discovery")
	r.Recorder.Event(discovery, corev1.EventTypeNormal, "DiscoveryStarted", "Discovery started")

	return ctrl.Result{RequeueAfter: time.Second * 5}, r.Status().Update(ctx, discovery)
}

func (r *AdoDiscoveryReconciler) processRunning(ctx context.Context, discovery *migrationv1.AdoDiscovery) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Processing running discovery", "name", discovery.Name)

	// Get credentials from secret (removed unused variables)
	err := r.validateCredentials(ctx, discovery)
	if err != nil {
		discovery.Status.Phase = migrationv1.DiscoveryPhaseFailed
		discovery.Status.ErrorMessage = fmt.Sprintf("Failed to get credentials: %v", err)
		r.sendWebSocketUpdate(discovery, "Failed to get credentials")
		r.Recorder.Event(discovery, corev1.EventTypeWarning, "CredentialsError",
			fmt.Sprintf("Failed to get credentials: %v", err))
		return ctrl.Result{}, r.Status().Update(ctx, discovery)
	}

	// Simulate discovery process
	updated := false

	// Update progress
	if discovery.Status.Progress.StepsCompleted < discovery.Status.Progress.TotalSteps {
		discovery.Status.Progress.StepsCompleted++
		discovery.Status.Progress.Percentage = (discovery.Status.Progress.StepsCompleted * 100) / discovery.Status.Progress.TotalSteps
		discovery.Status.Progress.CurrentStep = fmt.Sprintf("Discovering step %d of %d",
			discovery.Status.Progress.StepsCompleted, discovery.Status.Progress.TotalSteps)
		discovery.Status.Progress.ItemsDiscovered += 10 // Simulate finding items
		updated = true

		r.sendWebSocketUpdate(discovery, fmt.Sprintf("Discovery progress: %d%%", discovery.Status.Progress.Percentage))
		r.Recorder.Event(discovery, corev1.EventTypeNormal, "DiscoveryProgress",
			fmt.Sprintf("Discovery progress: %d%%", discovery.Status.Progress.Percentage))
	}

	// Check if discovery is complete
	if discovery.Status.Progress.StepsCompleted >= discovery.Status.Progress.TotalSteps {
		discovery.Status.Phase = migrationv1.DiscoveryPhaseCompleted
		discovery.Status.Progress.CurrentStep = "Discovery completed"
		discovery.Status.Progress.Percentage = 100

		now := metav1.Now()
		discovery.Status.CompletionTime = &now

		r.sendWebSocketUpdate(discovery, "Discovery completed successfully")
		r.Recorder.Event(discovery, corev1.EventTypeNormal, "DiscoveryCompleted", "Discovery completed successfully")
		updated = true
	}

	if updated {
		if err := r.Status().Update(ctx, discovery); err != nil {
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
	}

	// Continue processing if not complete
	if discovery.Status.Phase == migrationv1.DiscoveryPhaseRunning {
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	return ctrl.Result{}, nil
}

func (r *AdoDiscoveryReconciler) validateConfiguration(discovery *migrationv1.AdoDiscovery) error {
	if discovery.Spec.Source.Organization == "" {
		return fmt.Errorf("source organization is required")
	}

	// Check authentication configuration
	if discovery.Spec.Source.Auth.ServicePrincipal == nil && discovery.Spec.Source.Auth.PAT == nil {
		return fmt.Errorf("authentication configuration is required (either Service Principal or PAT)")
	}

	return nil
}

func (r *AdoDiscoveryReconciler) validateCredentials(ctx context.Context, discovery *migrationv1.AdoDiscovery) error {
	// For simplicity, just validate that configuration is present
	// In real implementation, this would fetch from Kubernetes secrets
	if discovery.Spec.Source.Auth.ServicePrincipal == nil && discovery.Spec.Source.Auth.PAT == nil {
		return fmt.Errorf("no authentication configuration found")
	}
	return nil
}

func (r *AdoDiscoveryReconciler) calculateTotalSteps(discovery *migrationv1.AdoDiscovery) int {
	steps := 0

	if discovery.Spec.Scope.Organizations {
		steps++
	}
	if discovery.Spec.Scope.Projects {
		steps++
	}
	if discovery.Spec.Scope.Repositories {
		steps++
	}
	if discovery.Spec.Scope.WorkItems {
		steps++
	}
	if discovery.Spec.Scope.Pipelines {
		steps++
	}
	if discovery.Spec.Scope.Builds {
		steps++
	}
	if discovery.Spec.Scope.Releases {
		steps++
	}
	if discovery.Spec.Scope.Teams {
		steps++
	}
	if discovery.Spec.Scope.Users {
		steps++
	}

	if steps == 0 {
		steps = 1 // At least one step
	}

	return steps
}

func (r *AdoDiscoveryReconciler) sendWebSocketUpdate(discovery *migrationv1.AdoDiscovery, message string) {
	if r.WebSocketManager != nil {
		update := map[string]interface{}{
			"discoveryId": discovery.Name,
			"namespace":   discovery.Namespace,
			"phase":       discovery.Status.Phase,
			"progress":    discovery.Status.Progress,
			"message":     message,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		}

		// Use the correct BroadcastUpdate method
		r.WebSocketManager.BroadcastUpdate("discovery_progress", "AdoDiscovery", discovery.Name, update)
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *AdoDiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize the recorder
	r.Recorder = mgr.GetEventRecorderFor("adodiscovery-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.AdoDiscovery{}).
		Complete(r)
}
