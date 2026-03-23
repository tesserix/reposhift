package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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

const githubProjectFinalizerName = "migration.ado-to-git-migration.io/githubproject-finalizer"

// GitHubProjectReconciler reconciles a GitHubProject object
type GitHubProjectReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// ProjectService handles GitHub Projects v2 operations
	ProjectService *services.GitHubProjectService
}

// +kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=githubprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=githubprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=githubprojects/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *GitHubProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling GitHubProject", "namespacedName", req.NamespacedName)

	// Fetch the GitHubProject instance
	var githubProject migrationv1.GitHubProject
	if err := r.Get(ctx, req.NamespacedName, &githubProject); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("GitHubProject resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get GitHubProject")
		return ctrl.Result{}, err
	}

	// Check if spec changed for completed project (before updating observed generation)
	if githubProject.Status.ObservedGeneration != 0 && githubProject.Status.ObservedGeneration != githubProject.Generation {
		if githubProject.Status.Phase == migrationv1.ProjectPhaseReady || githubProject.Status.Phase == migrationv1.ProjectPhaseFailed {
			// Spec changed for a terminal phase - warn user that recreation is needed
			log.Info("Spec changed for completed project",
				"oldGeneration", githubProject.Status.ObservedGeneration,
				"newGeneration", githubProject.Generation,
				"phase", githubProject.Status.Phase)
			return r.handleSpecChangeForCompletedProject(ctx, &githubProject)
		}
	}

	// Update observed generation
	if githubProject.Status.ObservedGeneration != githubProject.Generation {
		githubProject.Status.ObservedGeneration = githubProject.Generation
		now := metav1.Now()
		githubProject.Status.LastUpdatedAt = &now
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(&githubProject, githubProjectFinalizerName) {
		controllerutil.AddFinalizer(&githubProject, githubProjectFinalizerName)
		if err := r.Update(ctx, &githubProject); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Added finalizer to GitHub project")
	}

	// Handle deletion
	if !githubProject.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &githubProject)
	}

	// Handle based on phase
	switch githubProject.Status.Phase {
	case migrationv1.ProjectPhasePending, "":
		return r.processCreation(ctx, &githubProject)
	case migrationv1.ProjectPhaseCreating:
		return r.checkCreationStatus(ctx, &githubProject)
	case migrationv1.ProjectPhaseReady:
		return ctrl.Result{}, nil
	case migrationv1.ProjectPhaseFailed:
		// Allow manual retry by updating the phase to Pending
		return ctrl.Result{}, nil
	default:
		return ctrl.Result{}, nil
	}
}

func (r *GitHubProjectReconciler) processCreation(ctx context.Context, githubProject *migrationv1.GitHubProject) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Starting GitHub project creation", "name", githubProject.Name)

	// Update phase to Creating
	githubProject.Status.Phase = migrationv1.ProjectPhaseCreating
	githubProject.Status.Message = "Creating GitHub Project"
	now := metav1.Now()
	githubProject.Status.LastUpdatedAt = &now

	r.Recorder.Event(githubProject, corev1.EventTypeNormal, "Creating", "Starting GitHub project creation")

	if err := r.Status().Update(ctx, githubProject); err != nil {
		log.Error(err, "Failed to update GitHubProject status")
		return ctrl.Result{}, err
	}

	// Get GitHub token
	token, err := r.getGitHubToken(ctx, githubProject)
	if err != nil {
		return r.setFailedStatus(ctx, githubProject, fmt.Errorf("failed to get GitHub token: %w", err))
	}

	// Initialize project service if needed
	if r.ProjectService == nil {
		r.ProjectService = services.NewGitHubProjectService()
	}

	// Create project
	projectInfo, err := r.ProjectService.CreateProject(ctx, &services.CreateProjectRequest{
		Owner:       githubProject.Spec.Owner,
		ProjectName: githubProject.Spec.ProjectName,
		Description: githubProject.Spec.Description,
		Template:    services.ProjectTemplate(githubProject.Spec.Template),
		Public:      githubProject.Spec.Public,
		Repository:  githubProject.Spec.Repository,
		Token:       token,
	})

	if err != nil {
		return r.setFailedStatus(ctx, githubProject, fmt.Errorf("failed to create project: %w", err))
	}

	// Configure status field if specified
	if githubProject.Spec.StatusField != nil {
		log.Info("Configuring custom status field", "fieldName", githubProject.Spec.StatusField.Name)

		// Convert CRD StatusOption to service StatusFieldOption
		statusOptions := make([]services.StatusFieldOption, 0, len(githubProject.Spec.StatusField.Options))
		for _, opt := range githubProject.Spec.StatusField.Options {
			statusOptions = append(statusOptions, services.StatusFieldOption{
				Name:        opt.Name,
				Description: opt.Description,
				Color:       opt.Color,
			})
		}

		// Set default field name if not specified
		fieldName := githubProject.Spec.StatusField.Name
		if fieldName == "" {
			fieldName = "Status"
		}

		// Configure the status field
		if err := r.ProjectService.ConfigureStatusField(ctx, token, projectInfo.ID, fieldName, statusOptions); err != nil {
			log.Error(err, "Failed to configure status field", "fieldName", fieldName)
			// Don't fail the entire project creation, just log a warning
			r.Recorder.Event(githubProject, corev1.EventTypeWarning, "StatusFieldConfigFailed",
				fmt.Sprintf("Failed to configure status field: %v", err))
		} else {
			log.Info("Successfully configured status field", "fieldName", fieldName, "optionsCount", len(statusOptions))
			r.Recorder.Event(githubProject, corev1.EventTypeNormal, "StatusFieldConfigured",
				fmt.Sprintf("Configured status field '%s' with %d options", fieldName, len(statusOptions)))
		}
	}

	// Update status with project details
	githubProject.Status.Phase = migrationv1.ProjectPhaseReady
	githubProject.Status.ProjectID = projectInfo.ID
	githubProject.Status.ProjectNumber = projectInfo.Number
	githubProject.Status.ProjectURL = projectInfo.URL
	githubProject.Status.Message = "GitHub Project created successfully"
	githubProject.Status.ErrorMessage = ""
	createdAt := metav1.Now()
	githubProject.Status.CreatedAt = &createdAt
	githubProject.Status.LastUpdatedAt = &createdAt

	// Set condition
	meta.SetStatusCondition(&githubProject.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "ProjectCreated",
		Message:            fmt.Sprintf("Project created successfully with ID %s", projectInfo.ID),
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, githubProject); err != nil {
		log.Error(err, "Failed to update GitHubProject status")
		return ctrl.Result{}, err
	}

	r.Recorder.Event(githubProject, corev1.EventTypeNormal, "Created",
		fmt.Sprintf("GitHub Project created: %s (Number: %d)", projectInfo.URL, projectInfo.Number))

	log.Info("Successfully created GitHub project",
		"projectID", projectInfo.ID,
		"projectNumber", projectInfo.Number,
		"projectURL", projectInfo.URL)

	return ctrl.Result{}, nil
}

func (r *GitHubProjectReconciler) checkCreationStatus(ctx context.Context, githubProject *migrationv1.GitHubProject) (ctrl.Result, error) {
	// If we're in Creating phase, requeue to check status
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *GitHubProjectReconciler) reconcileDelete(ctx context.Context, githubProject *migrationv1.GitHubProject) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Handling deletion of GitHubProject", "name", githubProject.Name)

	// Note: We don't delete the GitHub project when the CR is deleted
	// to avoid data loss. Projects should be manually deleted if needed.

	r.Recorder.Event(githubProject, corev1.EventTypeNormal, "Deleting",
		"GitHubProject CR deleted (GitHub project not deleted to prevent data loss)")

	// Remove finalizer
	controllerutil.RemoveFinalizer(githubProject, githubProjectFinalizerName)
	if err := r.Update(ctx, githubProject); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Removed finalizer from GitHub project")
	return ctrl.Result{}, nil
}

// handleSpecChangeForCompletedProject handles when spec changes for a completed project
func (r *GitHubProjectReconciler) handleSpecChangeForCompletedProject(ctx context.Context, githubProject *migrationv1.GitHubProject) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// GitHub Projects cannot be easily updated programmatically
	// User needs to delete and recreate the CR if they want different specs
	log.Info("Spec changed for completed GitHub project - manual recreation required",
		"name", githubProject.Name,
		"projectURL", githubProject.Status.ProjectURL)

	githubProject.Status.Message = "Spec changed - manual deletion and recreation required. GitHub Projects cannot be automatically updated."
	githubProject.Status.ObservedGeneration = githubProject.Generation
	now := metav1.Now()
	githubProject.Status.LastUpdatedAt = &now

	r.Recorder.Event(githubProject, corev1.EventTypeWarning, "SpecChanged",
		"Spec changed for completed project - manual deletion and recreation required")

	if err := r.Status().Update(ctx, githubProject); err != nil {
		log.Error(err, "Failed to update status for spec change")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *GitHubProjectReconciler) setFailedStatus(ctx context.Context, githubProject *migrationv1.GitHubProject, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	githubProject.Status.Phase = migrationv1.ProjectPhaseFailed
	githubProject.Status.ErrorMessage = err.Error()
	githubProject.Status.Message = "GitHub Project creation failed"
	now := metav1.Now()
	githubProject.Status.LastUpdatedAt = &now

	// Set condition
	meta.SetStatusCondition(&githubProject.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "CreationFailed",
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	})

	if updateErr := r.Status().Update(ctx, githubProject); updateErr != nil {
		log.Error(updateErr, "Failed to update GitHubProject status")
		return ctrl.Result{}, updateErr
	}

	r.Recorder.Event(githubProject, corev1.EventTypeWarning, "CreationFailed",
		fmt.Sprintf("GitHub Project creation failed: %v", err))

	// Don't requeue on failure, wait for manual intervention
	return ctrl.Result{}, nil
}

func (r *GitHubProjectReconciler) getGitHubToken(ctx context.Context, githubProject *migrationv1.GitHubProject) (string, error) {
	// Check if using GitHub App or PAT
	if githubProject.Spec.Auth.AppAuth != nil {
		return r.getGitHubAppToken(ctx, githubProject)
	}

	if githubProject.Spec.Auth.TokenRef != nil {
		return r.getGitHubPATToken(ctx, githubProject)
	}

	return "", fmt.Errorf("no GitHub authentication configured")
}

func (r *GitHubProjectReconciler) getGitHubAppToken(ctx context.Context, githubProject *migrationv1.GitHubProject) (string, error) {
	appAuth := githubProject.Spec.Auth.AppAuth

	// Get App ID
	appIDSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      appAuth.AppIdRef.Name,
		Namespace: githubProject.Namespace,
	}, appIDSecret); err != nil {
		return "", fmt.Errorf("failed to get app ID secret: %w", err)
	}

	appIDStr := string(appIDSecret.Data[appAuth.AppIdRef.Key])
	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse app ID: %w", err)
	}

	// Get Installation ID
	installationIDSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      appAuth.InstallationIdRef.Name,
		Namespace: githubProject.Namespace,
	}, installationIDSecret); err != nil {
		return "", fmt.Errorf("failed to get installation ID secret: %w", err)
	}

	installationIDStr := string(installationIDSecret.Data[appAuth.InstallationIdRef.Key])
	installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("failed to parse installation ID: %w", err)
	}

	// Get Private Key
	privateKeySecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      appAuth.PrivateKeyRef.Name,
		Namespace: githubProject.Namespace,
	}, privateKeySecret); err != nil {
		return "", fmt.Errorf("failed to get private key secret: %w", err)
	}

	privateKey := privateKeySecret.Data[appAuth.PrivateKeyRef.Key]

	// Create GitHub App client and get installation token
	appClient, err := services.NewGitHubAppClient(ctx, appID, installationID, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create GitHub App client: %w", err)
	}

	token, err := appClient.GetToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get installation token: %w", err)
	}

	return token, nil
}

func (r *GitHubProjectReconciler) getGitHubPATToken(ctx context.Context, githubProject *migrationv1.GitHubProject) (string, error) {
	tokenRef := githubProject.Spec.Auth.TokenRef

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{
		Name:      tokenRef.Name,
		Namespace: githubProject.Namespace,
	}, secret); err != nil {
		return "", fmt.Errorf("failed to get GitHub token secret: %w", err)
	}

	token := string(secret.Data[tokenRef.Key])
	if token == "" {
		return "", fmt.Errorf("GitHub token is empty in secret")
	}

	return token, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitHubProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.GitHubProject{}).
		Complete(r)
}
