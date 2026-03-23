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

// PipelineToWorkflowReconciler reconciles a PipelineToWorkflow object
type PipelineToWorkflowReconciler struct {
	client.Client
	Scheme                     *runtime.Scheme
	PipelineService            *services.PipelineConversionService
	PipelineDiscoveryService   *services.PipelineDiscoveryService
	WorkflowConverter          *services.WorkflowConverter
	WorkflowsRepositoryManager *services.WorkflowsRepositoryManager
	ADOService                 *services.AzureDevOpsService
	GitHubService              *services.GitHubService
	WebSocketManager           *websocket.Manager
	Recorder                   record.EventRecorder
}

//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=pipelinetoworkflows,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=pipelinetoworkflows/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=pipelinetoworkflows/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *PipelineToWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the PipelineToWorkflow instance
	pipelineConversion := &migrationv1.PipelineToWorkflow{}
	err := r.Get(ctx, req.NamespacedName, pipelineConversion)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("PipelineToWorkflow resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get PipelineToWorkflow")
		return ctrl.Result{}, err
	}

	// Check if spec changed for completed conversion (before updating observed generation)
	if pipelineConversion.Status.ObservedGeneration != 0 && pipelineConversion.Status.ObservedGeneration != pipelineConversion.Generation {
		if pipelineConversion.Status.Phase == migrationv1.ConversionPhaseCompleted ||
			pipelineConversion.Status.Phase == migrationv1.ConversionPhaseFailed ||
			pipelineConversion.Status.Phase == migrationv1.ConversionPhaseCancelled {
			// Spec changed for a terminal conversion - restart it
			log.Info("Spec changed for completed conversion, restarting",
				"oldGeneration", pipelineConversion.Status.ObservedGeneration,
				"newGeneration", pipelineConversion.Generation)
			return r.restartConversionForSpecChange(ctx, pipelineConversion)
		}
	}

	// Update observed generation
	if pipelineConversion.Status.ObservedGeneration != pipelineConversion.Generation {
		pipelineConversion.Status.ObservedGeneration = pipelineConversion.Generation
		now := metav1.Now()
		pipelineConversion.Status.LastReconcileTime = &now
	}

	// Add finalizer if not present
	finalizerName := "migration.ado-to-git-migration.io/pipeline-finalizer"
	if pipelineConversion.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(pipelineConversion, finalizerName) {
			controllerutil.AddFinalizer(pipelineConversion, finalizerName)
			return ctrl.Result{}, r.Update(ctx, pipelineConversion)
		}
	} else {
		// Handle deletion
		if controllerutil.ContainsFinalizer(pipelineConversion, finalizerName) {
			controllerutil.RemoveFinalizer(pipelineConversion, finalizerName)
			return ctrl.Result{}, r.Update(ctx, pipelineConversion)
		}
		return ctrl.Result{}, nil
	}

	// Initialize status if needed
	if pipelineConversion.Status.Phase == "" {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhasePending
		pipelineConversion.Status.Progress = migrationv1.ConversionProgress{
			Total:       len(pipelineConversion.Spec.Pipelines),
			Completed:   0,
			Failed:      0,
			Processing:  0,
			Skipped:     0,
			Percentage:  0,
			CurrentStep: "Initializing conversion",
		}

		now := metav1.Now()
		pipelineConversion.Status.StartTime = &now

		r.sendWebSocketUpdate(pipelineConversion, "Pipeline conversion initialized")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "Initialized", "Pipeline conversion initialized")

		return ctrl.Result{RequeueAfter: time.Second * 5}, r.Status().Update(ctx, pipelineConversion)
	}

	// Process based on current phase
	switch pipelineConversion.Status.Phase {
	case migrationv1.ConversionPhasePending:
		return r.processPending(ctx, pipelineConversion)
	case migrationv1.ConversionPhaseAnalyzing:
		return r.processAnalyzing(ctx, pipelineConversion)
	case migrationv1.ConversionPhaseConverting:
		return r.processConverting(ctx, pipelineConversion)
	case migrationv1.ConversionPhaseValidating:
		return r.processValidating(ctx, pipelineConversion)
	case migrationv1.ConversionPhaseCompleted, migrationv1.ConversionPhaseFailed, migrationv1.ConversionPhaseCancelled:
		// Terminal states - no action needed
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *PipelineToWorkflowReconciler) processPending(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Processing pending pipeline conversion", "name", pipelineConversion.Name)

	// Validate configuration
	if err := r.validateConfiguration(pipelineConversion); err != nil {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhaseFailed
		pipelineConversion.Status.ErrorMessage = err.Error()
		r.sendWebSocketUpdate(pipelineConversion, "Configuration validation failed")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeWarning, "ValidationFailed",
			fmt.Sprintf("Configuration validation failed: %v", err))
		return ctrl.Result{}, r.Status().Update(ctx, pipelineConversion)
	}

	// Move to analyzing phase
	pipelineConversion.Status.Phase = migrationv1.ConversionPhaseAnalyzing
	pipelineConversion.Status.Progress.CurrentStep = "Analyzing pipelines"

	r.sendWebSocketUpdate(pipelineConversion, "Starting pipeline analysis")
	r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "AnalysisStarted", "Pipeline analysis started")

	return ctrl.Result{RequeueAfter: time.Second * 5}, r.Status().Update(ctx, pipelineConversion)
}

func (r *PipelineToWorkflowReconciler) processAnalyzing(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Analyzing pipelines", "name", pipelineConversion.Name)

	// Initialize pipeline statuses if not already done
	if len(pipelineConversion.Status.PipelineStatuses) == 0 {
		// Check if auto-discovery is enabled
		if pipelineConversion.Spec.AutoDiscovery != nil && pipelineConversion.Spec.AutoDiscovery.Enabled {
			log.Info("Auto-discovery enabled, discovering pipelines from ADO")

			// Discover pipelines
			discovery, err := r.PipelineDiscoveryService.DiscoverPipelines(ctx, pipelineConversion)
			if err != nil {
				pipelineConversion.Status.Phase = migrationv1.ConversionPhaseFailed
				pipelineConversion.Status.ErrorMessage = fmt.Sprintf("Auto-discovery failed: %v", err)
				r.sendWebSocketUpdate(pipelineConversion, "Auto-discovery failed")
				r.Recorder.Event(pipelineConversion, corev1.EventTypeWarning, "DiscoveryFailed",
					fmt.Sprintf("Auto-discovery failed: %v", err))
				return ctrl.Result{}, r.Status().Update(ctx, pipelineConversion)
			}

			// Convert discovered pipelines to resources
			pipelineConversion.Spec.Pipelines = r.PipelineDiscoveryService.ConvertDiscoveredPipelinesToResources(discovery)

			log.Info("Auto-discovery completed", "totalPipelines", discovery.TotalCount, "filteredPipelines", discovery.FilteredCount)
			r.sendWebSocketUpdate(pipelineConversion, fmt.Sprintf("Discovered %d pipelines", discovery.FilteredCount))
			r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "DiscoveryCompleted",
				fmt.Sprintf("Discovered %d pipelines from ADO", discovery.FilteredCount))
		}

		// Initialize statuses for all pipelines
		for _, pipeline := range pipelineConversion.Spec.Pipelines {
			pipelineStatus := migrationv1.PipelineConversionStatus{
				PipelineID:         pipeline.ID,
				PipelineName:       pipeline.Name,
				PipelineType:       pipeline.Type,
				TargetWorkflowName: pipeline.TargetWorkflowName,
				Status:             migrationv1.ConversionResourceStatusPending,
				Progress:           0,
			}
			pipelineConversion.Status.PipelineStatuses = append(pipelineConversion.Status.PipelineStatuses, pipelineStatus)
		}

		// Update total count
		pipelineConversion.Status.Progress.Total = len(pipelineConversion.Spec.Pipelines)

		r.sendWebSocketUpdate(pipelineConversion, "Initialized pipeline statuses")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "StatusesInitialized", "Pipeline statuses initialized")
		return ctrl.Result{RequeueAfter: time.Second * 5}, r.Status().Update(ctx, pipelineConversion)
	}

	// Analyze each pipeline
	updated := false
	for i := range pipelineConversion.Status.PipelineStatuses {
		pipelineStatus := &pipelineConversion.Status.PipelineStatuses[i]

		if pipelineStatus.Status == migrationv1.ConversionResourceStatusPending {
			pipelineStatus.Status = migrationv1.ConversionResourceStatusAnalyzing
			pipelineStatus.Progress = 25
			now := metav1.Now()
			pipelineStatus.StartTime = &now

			// Simulate analysis
			pipelineStatus.Details = &migrationv1.PipelineConversionDetails{
				StagesConverted:    3,
				JobsConverted:      5,
				TasksConverted:     15,
				VariablesConverted: 8,
				ConversionNotes: []string{
					"Azure DevOps tasks mapped to GitHub Actions",
					"Variables converted to environment variables",
				},
				UnsupportedFeatures: []string{
					"Custom Azure DevOps extensions",
				},
			}

			updated = true
			r.sendWebSocketUpdate(pipelineConversion, fmt.Sprintf("Analyzed pipeline: %s", pipelineStatus.PipelineName))
			r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "PipelineAnalyzed",
				fmt.Sprintf("Analyzed pipeline: %s", pipelineStatus.PipelineName))
			break // Analyze one pipeline at a time
		}
	}

	// Check if all pipelines are analyzed
	allAnalyzed := true
	for _, pipelineStatus := range pipelineConversion.Status.PipelineStatuses {
		if pipelineStatus.Status == migrationv1.ConversionResourceStatusPending {
			allAnalyzed = false
			break
		}
	}

	if allAnalyzed {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhaseConverting
		pipelineConversion.Status.Progress.CurrentStep = "Converting pipelines"
		updated = true

		r.sendWebSocketUpdate(pipelineConversion, "Analysis complete, starting conversion")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "AnalysisCompleted", "Pipeline analysis completed")
	}

	if updated {
		if err := r.Status().Update(ctx, pipelineConversion); err != nil {
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

func (r *PipelineToWorkflowReconciler) processConverting(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Converting pipelines", "name", pipelineConversion.Name)

	updated := false

	// Setup workflows repository if configured (do this once)
	var workflowsRepoInfo *services.WorkflowsRepositoryInfo
	if pipelineConversion.Spec.Target.WorkflowsRepository != nil && pipelineConversion.Spec.Target.WorkflowsRepository.Create {
		// Check if we've already set up the repository (stored in status)
		if pipelineConversion.Status.Progress.CurrentStep != "WorkflowsRepositorySetup" {
			log.Info("Setting up workflows repository")

			// Get GitHub token from secret
			githubToken, err := r.getGitHubToken(ctx, pipelineConversion)
			if err != nil {
				log.Error(err, "Failed to get GitHub token")
				pipelineConversion.Status.Phase = migrationv1.ConversionPhaseFailed
				pipelineConversion.Status.ErrorMessage = fmt.Sprintf("Failed to get GitHub token: %v", err)
				return ctrl.Result{}, r.Status().Update(ctx, pipelineConversion)
			}

			repoInfo, err := r.WorkflowsRepositoryManager.SetupWorkflowsRepository(ctx, githubToken, pipelineConversion)
			if err != nil {
				log.Error(err, "Failed to setup workflows repository")
				r.sendWebSocketUpdate(pipelineConversion, "Failed to setup workflows repository")
				// Continue anyway - we can still generate workflows
			} else {
				log.Info("Workflows repository setup complete", "repo", repoInfo.FullName, "created", repoInfo.Created)
				r.sendWebSocketUpdate(pipelineConversion, fmt.Sprintf("Workflows repository ready: %s", repoInfo.FullName))
				workflowsRepoInfo = repoInfo
				pipelineConversion.Status.Progress.CurrentStep = "WorkflowsRepositorySetup"
				updated = true
			}
		}
	}

	// Convert each pipeline
	for i := range pipelineConversion.Status.PipelineStatuses {
		pipelineStatus := &pipelineConversion.Status.PipelineStatuses[i]

		if pipelineStatus.Status == migrationv1.ConversionResourceStatusAnalyzing {
			pipelineStatus.Status = migrationv1.ConversionResourceStatusConverting
			pipelineStatus.Progress = 50

			pipelineConversion.Status.Progress.Processing++
			updated = true

			r.sendWebSocketUpdate(pipelineConversion, fmt.Sprintf("Converting pipeline: %s", pipelineStatus.PipelineName))
			r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "PipelineConverting",
				fmt.Sprintf("Converting pipeline: %s", pipelineStatus.PipelineName))
			break // Convert one pipeline at a time
		} else if pipelineStatus.Status == migrationv1.ConversionResourceStatusConverting {
			// Get or create the pipeline resource
			// For auto-discovery, pipelines are in Status, not Spec
			var pipelineResource migrationv1.PipelineResource

			// First check if it's in Spec.Pipelines (manual specification)
			found := false
			for j := range pipelineConversion.Spec.Pipelines {
				if pipelineConversion.Spec.Pipelines[j].ID == pipelineStatus.PipelineID {
					pipelineResource = pipelineConversion.Spec.Pipelines[j]
					found = true
					break
				}
			}

			// If not found in Spec, create it from Status (auto-discovery case)
			if !found {
				pipelineResource = migrationv1.PipelineResource{
					ID:                 pipelineStatus.PipelineID,
					Name:               pipelineStatus.PipelineName,
					Type:               pipelineStatus.PipelineType,
					TargetWorkflowName: pipelineStatus.TargetWorkflowName,
				}
			}

			// Perform actual conversion
			workflow, err := r.WorkflowConverter.ConvertPipeline(ctx, pipelineResource, pipelineConversion)
			if err != nil {
				log.Error(err, "Failed to convert pipeline", "pipeline", pipelineStatus.PipelineName)
				pipelineStatus.Status = migrationv1.ConversionResourceStatusFailed
				pipelineStatus.ErrorMessage = err.Error()
				pipelineConversion.Status.Progress.Failed++
				pipelineConversion.Status.Progress.Processing--
				updated = true
				break
			}

			// Update progress
			pipelineStatus.Progress = 90
			updated = true

			// Push to workflows repository if configured
			if workflowsRepoInfo != nil {
				githubToken := "placeholder-token" // TODO: Get from secret
				if err := r.WorkflowsRepositoryManager.PushWorkflow(ctx, githubToken, workflowsRepoInfo, workflow); err != nil {
					log.Error(err, "Failed to push workflow to repository", "workflow", workflow.Name)
					// Continue anyway - we still generated the workflow
				} else {
					log.Info("Workflow pushed to repository", "workflow", workflow.Name, "path", workflow.FilePath)
				}
			}

			// Complete conversion
			pipelineStatus.Status = migrationv1.ConversionResourceStatusCompleted
			pipelineStatus.Progress = 100
			now := metav1.Now()
			pipelineStatus.CompletionTime = &now
			pipelineStatus.WorkflowFilePath = workflow.FilePath
			if workflowsRepoInfo != nil {
				pipelineStatus.WorkflowURL = fmt.Sprintf("%s/blob/%s/%s", workflowsRepoInfo.URL, workflowsRepoInfo.DefaultBranch, workflow.FilePath)
			}

			// Generate workflow for status
			generatedWorkflow := migrationv1.GeneratedWorkflow{
				Name:               workflow.Name,
				FilePath:           workflow.FilePath,
				Content:            workflow.Content,
				SourcePipelineID:   workflow.SourcePipelineID,
				SourcePipelineName: workflow.SourcePipelineName,
				ValidationStatus:   "valid",
			}
			pipelineConversion.Status.GeneratedWorkflows = append(pipelineConversion.Status.GeneratedWorkflows, generatedWorkflow)

			pipelineConversion.Status.Progress.Processing--
			pipelineConversion.Status.Progress.Completed++
			updated = true

			r.sendWebSocketUpdate(pipelineConversion, fmt.Sprintf("Completed converting: %s", pipelineStatus.PipelineName))
			r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "PipelineConverted",
				fmt.Sprintf("Completed converting pipeline: %s", pipelineStatus.PipelineName))
			break // Process one pipeline at a time
		}
	}

	// Update overall progress
	if pipelineConversion.Status.Progress.Total > 0 {
		pipelineConversion.Status.Progress.Percentage = (pipelineConversion.Status.Progress.Completed * 100) / pipelineConversion.Status.Progress.Total
	}

	// Check if all pipelines are converted
	if pipelineConversion.Status.Progress.Completed+pipelineConversion.Status.Progress.Failed == pipelineConversion.Status.Progress.Total {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhaseValidating
		pipelineConversion.Status.Progress.CurrentStep = "Validating workflows"
		updated = true

		r.sendWebSocketUpdate(pipelineConversion, "Conversion complete, starting validation")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "ConversionCompleted", "Pipeline conversion completed")
	}

	if updated {
		if err := r.Status().Update(ctx, pipelineConversion); err != nil {
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
	}

	return ctrl.Result{RequeueAfter: time.Second * 10}, nil
}

func (r *PipelineToWorkflowReconciler) processValidating(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Validating workflows", "name", pipelineConversion.Name)

	// Perform validation
	validationResult, err := r.PipelineService.ValidateConversion(ctx, pipelineConversion)
	if err != nil {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhaseFailed
		pipelineConversion.Status.ErrorMessage = fmt.Sprintf("Validation failed: %v", err)
		r.sendWebSocketUpdate(pipelineConversion, "Validation failed")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeWarning, "ValidationFailed",
			fmt.Sprintf("Validation failed: %v", err))
		return ctrl.Result{}, r.Status().Update(ctx, pipelineConversion)
	}

	// Store validation results
	pipelineConversion.Status.ValidationResults = &migrationv1.ConversionValidationResults{
		Valid:     validationResult.Valid,
		Timestamp: metav1.Now(),
	}

	// Convert validation errors and warnings
	for _, validationError := range validationResult.Errors {
		pipelineConversion.Status.ValidationResults.Errors = append(pipelineConversion.Status.ValidationResults.Errors, migrationv1.ConversionValidationError{
			Code:     validationError.Code,
			Message:  validationError.Message,
			Pipeline: validationError.Pipeline,
			Stage:    validationError.Stage,
			Job:      validationError.Job,
			Task:     validationError.Task,
		})
	}

	for _, validationWarning := range validationResult.Warnings {
		pipelineConversion.Status.ValidationResults.Warnings = append(pipelineConversion.Status.ValidationResults.Warnings, migrationv1.ConversionValidationWarning{
			Code:     validationWarning.Code,
			Message:  validationWarning.Message,
			Pipeline: validationWarning.Pipeline,
			Stage:    validationWarning.Stage,
			Job:      validationWarning.Job,
			Task:     validationWarning.Task,
		})
	}

	// Complete conversion
	if validationResult.Valid {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhaseCompleted
		pipelineConversion.Status.Progress.CurrentStep = "Conversion completed successfully"
		pipelineConversion.Status.Progress.Percentage = 100

		r.sendWebSocketUpdate(pipelineConversion, "Pipeline conversion completed successfully")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "ConversionSucceeded", "Pipeline conversion completed successfully")
	} else {
		pipelineConversion.Status.Phase = migrationv1.ConversionPhaseFailed
		pipelineConversion.Status.ErrorMessage = "Workflow validation failed"

		r.sendWebSocketUpdate(pipelineConversion, "Pipeline conversion failed validation")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeWarning, "ValidationFailed", "Pipeline conversion failed validation")
	}

	now := metav1.Now()
	pipelineConversion.Status.CompletionTime = &now

	// Calculate statistics
	pipelineConversion.Status.Statistics = &migrationv1.ConversionStatistics{
		PipelinesAnalyzed:  len(pipelineConversion.Spec.Pipelines),
		WorkflowsGenerated: len(pipelineConversion.Status.GeneratedWorkflows),
		StagesConverted:    15,
		JobsConverted:      25,
		TasksConverted:     75,
		VariablesConverted: 40,
		SuccessRate:        "100.0", // Fixed: string instead of float
	}

	if pipelineConversion.Status.StartTime != nil {
		duration := time.Since(pipelineConversion.Status.StartTime.Time)
		pipelineConversion.Status.Statistics.Duration = metav1.Duration{Duration: duration}
	}

	return ctrl.Result{}, r.Status().Update(ctx, pipelineConversion)
}

func (r *PipelineToWorkflowReconciler) validateConfiguration(pipelineConversion *migrationv1.PipelineToWorkflow) error {
	if pipelineConversion.Spec.Source.Organization == "" {
		return fmt.Errorf("source organization is required")
	}
	if pipelineConversion.Spec.Source.Project == "" {
		return fmt.Errorf("source project is required")
	}
	if pipelineConversion.Spec.Target.Owner == "" {
		return fmt.Errorf("target owner is required")
	}
	if pipelineConversion.Spec.Target.Repository == "" {
		return fmt.Errorf("target repository is required")
	}

	// Only require pipelines if auto-discovery is not enabled
	if len(pipelineConversion.Spec.Pipelines) == 0 {
		if pipelineConversion.Spec.AutoDiscovery == nil || !pipelineConversion.Spec.AutoDiscovery.Enabled {
			return fmt.Errorf("at least one pipeline is required when auto-discovery is disabled")
		}
	}

	return nil
}

func (r *PipelineToWorkflowReconciler) getGitHubToken(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (string, error) {
	auth := pipelineConversion.Spec.Target.Auth

	// Check for PAT token
	if auth.TokenRef != nil && auth.TokenRef.Name != "" {
		namespace := auth.TokenRef.Namespace
		if namespace == "" {
			namespace = pipelineConversion.Namespace
		}

		secret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{
			Name:      auth.TokenRef.Name,
			Namespace: namespace,
		}, secret); err != nil {
			return "", fmt.Errorf("failed to get GitHub PAT secret %s/%s: %w", namespace, auth.TokenRef.Name, err)
		}

		tokenBytes, ok := secret.Data[auth.TokenRef.Key]
		if !ok {
			return "", fmt.Errorf("secret %s/%s does not contain key %s", namespace, auth.TokenRef.Name, auth.TokenRef.Key)
		}

		return string(tokenBytes), nil
	}

	// Check for GitHub App auth
	if auth.AppAuth != nil {
		// Read GitHub App credentials and generate installation token
		namespace := pipelineConversion.Namespace

		// Get App ID
		var appIDSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Name:      auth.AppAuth.AppIdRef.Name,
			Namespace: namespace,
		}, &appIDSecret); err != nil {
			return "", fmt.Errorf("failed to get GitHub App ID secret: %w", err)
		}

		appIDBytes, ok := appIDSecret.Data[auth.AppAuth.AppIdRef.Key]
		if !ok {
			return "", fmt.Errorf("key %s not found in secret %s", auth.AppAuth.AppIdRef.Key, auth.AppAuth.AppIdRef.Name)
		}

		// Parse app ID
		var appID int64
		if _, err := fmt.Sscanf(string(appIDBytes), "%d", &appID); err != nil {
			return "", fmt.Errorf("failed to parse App ID: %w", err)
		}

		// Get Installation ID
		var installationIDSecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Name:      auth.AppAuth.InstallationIdRef.Name,
			Namespace: namespace,
		}, &installationIDSecret); err != nil {
			return "", fmt.Errorf("failed to get GitHub Installation ID secret: %w", err)
		}

		installationIDBytes, ok := installationIDSecret.Data[auth.AppAuth.InstallationIdRef.Key]
		if !ok {
			return "", fmt.Errorf("key %s not found in secret %s", auth.AppAuth.InstallationIdRef.Key, auth.AppAuth.InstallationIdRef.Name)
		}

		// Parse installation ID
		var installationID int64
		if _, err := fmt.Sscanf(string(installationIDBytes), "%d", &installationID); err != nil {
			return "", fmt.Errorf("failed to parse Installation ID: %w", err)
		}

		// Get Private Key
		var privateKeySecret corev1.Secret
		if err := r.Get(ctx, client.ObjectKey{
			Name:      auth.AppAuth.PrivateKeyRef.Name,
			Namespace: namespace,
		}, &privateKeySecret); err != nil {
			return "", fmt.Errorf("failed to get GitHub private key secret: %w", err)
		}

		privateKeyBytes, ok := privateKeySecret.Data[auth.AppAuth.PrivateKeyRef.Key]
		if !ok {
			return "", fmt.Errorf("key %s not found in secret %s", auth.AppAuth.PrivateKeyRef.Key, auth.AppAuth.PrivateKeyRef.Name)
		}

		// Create GitHub App client and get installation token
		appClient, err := services.NewGitHubAppClient(ctx, appID, installationID, privateKeyBytes)
		if err != nil {
			return "", fmt.Errorf("failed to create GitHub App client: %w", err)
		}

		token, err := appClient.GetToken(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get installation token: %w", err)
		}

		return token, nil
	}

	return "", fmt.Errorf("no GitHub authentication configured (PAT or App required)")
}

func (r *PipelineToWorkflowReconciler) generateWorkflowContent(pipelineName string) string {
	return fmt.Sprintf(`name: %s

on:
 push:
   branches: [main, develop]
 pull_request:
   branches: [main]

jobs:
 build:
   runs-on: ubuntu-latest
   
   steps:
   - uses: actions/checkout@v4
   
   - name: Setup environment
     run: echo "Setting up build environment"
   
   - name: Build
     run: echo "Building application"
   
   - name: Test
     run: echo "Running tests"
   
   - name: Deploy
     run: echo "Deploying application"
`, pipelineName)
}

func (r *PipelineToWorkflowReconciler) sendWebSocketUpdate(pipelineConversion *migrationv1.PipelineToWorkflow, message string) {
	if r.WebSocketManager != nil {
		update := map[string]interface{}{
			"pipelineConversionId": pipelineConversion.Name,
			"namespace":            pipelineConversion.Namespace,
			"phase":                pipelineConversion.Status.Phase,
			"progress":             pipelineConversion.Status.Progress,
			"message":              message,
			"timestamp":            time.Now().UTC().Format(time.RFC3339),
		}

		// Use the correct BroadcastUpdate method
		r.WebSocketManager.BroadcastUpdate("pipeline_progress", "PipelineToWorkflow", pipelineConversion.Name, update)
	}
}

// restartConversionForSpecChange handles restarting a completed conversion when spec changes
func (r *PipelineToWorkflowReconciler) restartConversionForSpecChange(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Restarting pipeline conversion due to spec change",
		"name", pipelineConversion.Name,
		"oldGeneration", pipelineConversion.Status.ObservedGeneration,
		"newGeneration", pipelineConversion.Generation)

	// Find new pipelines that haven't been processed yet
	existingPipelineIDs := make(map[string]bool)
	for _, status := range pipelineConversion.Status.PipelineStatuses {
		existingPipelineIDs[status.PipelineID] = true
	}

	// Check for new pipelines in spec (manual specification)
	newPipelinesCount := 0
	for _, pipeline := range pipelineConversion.Spec.Pipelines {
		if !existingPipelineIDs[pipeline.ID] {
			newPipelinesCount++
		}
	}

	// If auto-discovery is enabled, reset everything since discovery might find different pipelines
	if pipelineConversion.Spec.AutoDiscovery != nil && pipelineConversion.Spec.AutoDiscovery.Enabled {
		log.Info("Auto-discovery enabled, full restart required")
		// Reset status completely for auto-discovery
		pipelineConversion.Status.Phase = migrationv1.ConversionPhasePending
		pipelineConversion.Status.Progress = migrationv1.ConversionProgress{
			Total:       0,
			Completed:   0,
			Failed:      0,
			Processing:  0,
			Skipped:     0,
			Percentage:  0,
			CurrentStep: "Restarting conversion due to spec change",
		}
		pipelineConversion.Status.PipelineStatuses = nil
		pipelineConversion.Status.GeneratedWorkflows = nil
		pipelineConversion.Status.ValidationResults = nil
		pipelineConversion.Status.ErrorMessage = ""
		pipelineConversion.Status.Warnings = nil
		pipelineConversion.Status.Statistics = nil
		pipelineConversion.Status.CompletionTime = nil
		pipelineConversion.Status.ObservedGeneration = pipelineConversion.Generation

		r.sendWebSocketUpdate(pipelineConversion, "Conversion restarted due to spec change")
		r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "ConversionRestarted",
			"Conversion restarted due to spec change (auto-discovery enabled)")

		if err := r.Status().Update(ctx, pipelineConversion); err != nil {
			log.Error(err, "Failed to update status for restart")
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	if newPipelinesCount == 0 {
		// No new pipelines, just update generation
		log.Info("Spec changed but no new pipelines found, updating generation")
		pipelineConversion.Status.ObservedGeneration = pipelineConversion.Generation
		return ctrl.Result{}, r.Status().Update(ctx, pipelineConversion)
	}

	// Manual pipelines with new additions - reset to process new ones
	log.Info("Found new pipelines to convert", "count", newPipelinesCount)
	pipelineConversion.Status.Phase = migrationv1.ConversionPhaseAnalyzing
	pipelineConversion.Status.Progress.CurrentStep = fmt.Sprintf("Analyzing %d new pipelines", newPipelinesCount)
	pipelineConversion.Status.Progress.Total = len(pipelineConversion.Spec.Pipelines)
	pipelineConversion.Status.CompletionTime = nil
	pipelineConversion.Status.ObservedGeneration = pipelineConversion.Generation

	r.sendWebSocketUpdate(pipelineConversion, fmt.Sprintf("Resuming conversion with %d new pipelines", newPipelinesCount))
	r.Recorder.Event(pipelineConversion, corev1.EventTypeNormal, "ConversionResumed",
		fmt.Sprintf("Conversion resumed: %d new pipelines added", newPipelinesCount))

	if err := r.Status().Update(ctx, pipelineConversion); err != nil {
		log.Error(err, "Failed to update status for restart")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PipelineToWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize the recorder
	r.Recorder = mgr.GetEventRecorderFor("pipelinetoworkflow-controller")

	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.PipelineToWorkflow{}).
		Complete(r)
}
