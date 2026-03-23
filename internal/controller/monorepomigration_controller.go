package controller

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
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
	monoRepoMigrationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "monorepo_migration_total",
			Help: "Total number of monorepo migrations by phase and namespace",
		},
		[]string{"phase", "namespace"},
	)

	monoRepoReconciliationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "monorepo_migration_reconciliations_total",
			Help: "Total number of monorepo reconciliations by trigger reason and namespace",
		},
		[]string{"trigger_reason", "namespace"},
	)

	monoRepoReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "monorepo_migration_reconcile_duration_seconds",
			Help:    "Duration of monorepo reconciliation operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"phase", "namespace"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		monoRepoMigrationsTotal,
		monoRepoReconciliationsTotal,
		monoRepoReconcileDuration,
	)
}

const (
	monoRepoFinalizerName = "migration.ado-to-git-migration.io/monorepo-finalizer"

	monoRepoPauseAnnotation            = "migration.ado-to-git-migration.io/pause"
	monoRepoCancelAnnotation           = "migration.ado-to-git-migration.io/cancel"
	monoRepoRetryAnnotation            = "migration.ado-to-git-migration.io/retry"
	monoRepoReconcileTriggerAnnotation = "migration.ado-to-git-migration.io/reconcile-trigger"

	monoRepoPendingRequeueInterval = 5 * time.Second
	monoRepoRunningRequeueInterval = 10 * time.Second
	monoRepoPausedRequeueInterval  = 60 * time.Second
)

// MonoRepoMigrationReconciler reconciles a MonoRepoMigration object
type MonoRepoMigrationReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	MigrationService *services.MigrationService
	GitHubService    *services.GitHubService
	WebSocketManager *websocket.Manager
	Recorder         record.EventRecorder

	activeMigrations     map[string]bool
	activeMigrationMutex sync.Mutex

	// Persistent workspace paths per migration for resumability
	workspaces      map[string]string // migration name -> workspace dir
	workspacesMutex sync.Mutex
}

//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=monorepomigrations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=monorepomigrations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=migration.ado-to-git-migration.io,resources=monorepomigrations/finalizers,verbs=update

// Reconcile is the main reconciliation loop for MonoRepoMigration
func (r *MonoRepoMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	startTime := time.Now()

	monoRepoReconciliationsTotal.WithLabelValues("reconcile_started", req.Namespace).Inc()
	logger.Info("Reconciling MonoRepoMigration", "namespacedName", req.NamespacedName)

	if r.activeMigrations == nil {
		r.activeMigrations = make(map[string]bool)
	}
	if r.workspaces == nil {
		r.workspaces = make(map[string]string)
	}

	// Fetch the MonoRepoMigration
	migration := &migrationv1.MonoRepoMigration{}
	if err := r.Get(ctx, req.NamespacedName, migration); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MonoRepoMigration not found, ignoring")
			r.removeFromActive(req.NamespacedName.String())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	defer func() {
		duration := time.Since(startTime).Seconds()
		phase := string(migration.Status.Phase)
		if phase == "" {
			phase = "initializing"
		}
		monoRepoReconcileDuration.WithLabelValues(phase, req.Namespace).Observe(duration)
	}()

	// Handle finalizer and deletion
	if migration.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(migration, monoRepoFinalizerName) {
			controllerutil.AddFinalizer(migration, monoRepoFinalizerName)
			if err := r.Update(ctx, migration); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	} else {
		if controllerutil.ContainsFinalizer(migration, monoRepoFinalizerName) {
			r.cleanupWorkspace(migration.Name, logger)
			r.removeFromActive(req.NamespacedName.String())
			controllerutil.RemoveFinalizer(migration, monoRepoFinalizerName)
			if err := r.Update(ctx, migration); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Handle annotations
	if result, err := r.handleAnnotations(ctx, migration); result != nil {
		return *result, err
	}

	// Initialize status if needed
	if migration.Status.Phase == "" {
		return r.initializeStatus(ctx, migration)
	}

	// Process based on current phase
	return r.processPhase(ctx, migration)
}

// handleAnnotations handles pause, cancel, retry annotations
func (r *MonoRepoMigrationReconciler) handleAnnotations(ctx context.Context, migration *migrationv1.MonoRepoMigration) (*ctrl.Result, error) {
	if migration.Annotations == nil {
		return nil, nil
	}

	// Handle reconcile trigger
	if _, exists := migration.Annotations[monoRepoReconcileTriggerAnnotation]; exists {
		delete(migration.Annotations, monoRepoReconcileTriggerAnnotation)
		if err := r.Update(ctx, migration); err != nil {
			return &ctrl.Result{}, err
		}

		isTerminal := migration.Status.Phase == migrationv1.MonoRepoMigrationPhaseCompleted ||
			migration.Status.Phase == migrationv1.MonoRepoMigrationPhaseFailed ||
			migration.Status.Phase == migrationv1.MonoRepoMigrationPhaseCancelled
		if isTerminal {
			return r.retryMigration(ctx, migration)
		}
		return &ctrl.Result{Requeue: true}, nil
	}

	// Handle pause
	if _, exists := migration.Annotations[monoRepoPauseAnnotation]; exists {
		if migration.Status.Phase != migrationv1.MonoRepoMigrationPhasePaused {
			migration.Status.Phase = migrationv1.MonoRepoMigrationPhasePaused
			migration.Status.Progress.CurrentStep = "Migration paused by user"
			monoRepoMigrationsTotal.WithLabelValues("paused", migration.Namespace).Inc()
			r.sendWebSocketUpdate(migration, "Migration paused")
			r.Recorder.Event(migration, corev1.EventTypeNormal, "MigrationPaused", "Migration paused by user")
			if err := r.Status().Update(ctx, migration); err != nil {
				return &ctrl.Result{}, err
			}
		}
		return &ctrl.Result{RequeueAfter: monoRepoPausedRequeueInterval}, nil
	}

	// Handle cancel
	if _, exists := migration.Annotations[monoRepoCancelAnnotation]; exists {
		if migration.Status.Phase != migrationv1.MonoRepoMigrationPhaseCancelled {
			migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseCancelled
			migration.Status.Progress.CurrentStep = "Migration cancelled by user"
			now := metav1.Now()
			migration.Status.CompletionTime = &now
			monoRepoMigrationsTotal.WithLabelValues("cancelled", migration.Namespace).Inc()
			r.sendWebSocketUpdate(migration, "Migration cancelled")
			r.Recorder.Event(migration, corev1.EventTypeWarning, "MigrationCancelled", "Migration cancelled by user")
			if err := r.Status().Update(ctx, migration); err != nil {
				return &ctrl.Result{}, err
			}
		}
		return &ctrl.Result{}, nil
	}

	// Handle retry
	if _, exists := migration.Annotations[monoRepoRetryAnnotation]; exists {
		delete(migration.Annotations, monoRepoRetryAnnotation)
		if err := r.Update(ctx, migration); err != nil {
			return &ctrl.Result{}, err
		}
		if migration.Status.Phase == migrationv1.MonoRepoMigrationPhaseFailed {
			return r.retryMigration(ctx, migration)
		}
	}

	return nil, nil
}

// retryMigration resets a failed/terminal migration for retry
func (r *MonoRepoMigrationReconciler) retryMigration(ctx context.Context, migration *migrationv1.MonoRepoMigration) (*ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Retrying monorepo migration")

	// Clean up old workspace so git clone doesn't fail on existing directories
	r.cleanupWorkspace(migration.Name, logger)

	migration.Status.Phase = migrationv1.MonoRepoMigrationPhasePending
	migration.Status.ErrorMessage = ""
	migration.Status.CompletionTime = nil
	migration.Status.Progress.CurrentStep = "Retrying migration"

	// Reset failed/skipped repos to pending
	for i := range migration.Status.RepoStatuses {
		if migration.Status.RepoStatuses[i].Phase == migrationv1.MonoRepoRepoPhaseFailed ||
			migration.Status.RepoStatuses[i].Phase == migrationv1.MonoRepoRepoPhaseSkipped {
			migration.Status.RepoStatuses[i].Phase = migrationv1.MonoRepoRepoPhasePending
			migration.Status.RepoStatuses[i].ErrorMessage = ""
		}
	}

	r.Recorder.Event(migration, corev1.EventTypeNormal, "MigrationRetry", "Migration retry initiated")

	if err := r.Status().Update(ctx, migration); err != nil {
		return &ctrl.Result{}, err
	}
	return &ctrl.Result{Requeue: true}, nil
}

// initializeStatus initializes the migration status
func (r *MonoRepoMigrationReconciler) initializeStatus(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Initializing MonoRepoMigration status")

	now := metav1.Now()
	migration.Status.Phase = migrationv1.MonoRepoMigrationPhasePending
	migration.Status.StartTime = &now
	migration.Status.ObservedGeneration = migration.Generation
	migration.Status.LastReconcileTime = &now

	// Sort repos by priority and initialize per-repo statuses
	repos := make([]migrationv1.MonoRepoSourceRepo, len(migration.Spec.SourceRepos))
	copy(repos, migration.Spec.SourceRepos)
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Priority < repos[j].Priority
	})

	migration.Status.RepoStatuses = make([]migrationv1.MonoRepoRepoStatus, len(repos))
	for i, repo := range repos {
		subdirName := repo.SubdirectoryName
		if subdirName == "" {
			subdirName = repo.Name
		}
		migration.Status.RepoStatuses[i] = migrationv1.MonoRepoRepoStatus{
			Name:             repo.Name,
			SubdirectoryName: subdirName,
			Phase:            migrationv1.MonoRepoRepoPhasePending,
		}
	}

	migration.Status.Progress = migrationv1.MonoRepoProgress{
		TotalRepos:      len(repos),
		CompletedRepos:  0,
		FailedRepos:     0,
		SkippedRepos:    0,
		Percentage:      0,
		ProgressSummary: fmt.Sprintf("0/%d repos", len(repos)),
	}

	migration.Status.Statistics = &migrationv1.MonoRepoStatistics{}

	r.sendWebSocketUpdate(migration, "MonoRepo migration initialized")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "Initialized",
		fmt.Sprintf("MonoRepo migration initialized with %d source repos", len(repos)))

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{RequeueAfter: monoRepoPendingRequeueInterval}, err
	}
	return ctrl.Result{RequeueAfter: monoRepoPendingRequeueInterval}, nil
}

// processPhase dispatches to the appropriate phase handler
func (r *MonoRepoMigrationReconciler) processPhase(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	switch migration.Status.Phase {
	case migrationv1.MonoRepoMigrationPhasePending:
		return r.processPending(ctx, migration)
	case migrationv1.MonoRepoMigrationPhaseValidating:
		return r.processValidating(ctx, migration)
	case migrationv1.MonoRepoMigrationPhaseCloning:
		return r.processCloning(ctx, migration)
	case migrationv1.MonoRepoMigrationPhaseRewriting:
		return r.processRewriting(ctx, migration)
	case migrationv1.MonoRepoMigrationPhaseMerging:
		return r.processMerging(ctx, migration)
	case migrationv1.MonoRepoMigrationPhasePushing:
		return r.processPushing(ctx, migration)
	case migrationv1.MonoRepoMigrationPhasePaused:
		return ctrl.Result{RequeueAfter: monoRepoPausedRequeueInterval}, nil
	case migrationv1.MonoRepoMigrationPhaseCompleted,
		migrationv1.MonoRepoMigrationPhaseFailed,
		migrationv1.MonoRepoMigrationPhaseCancelled:
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// processPending transitions to Validating
func (r *MonoRepoMigrationReconciler) processPending(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseValidating
	migration.Status.Progress.CurrentStep = "Validating configuration"

	r.sendWebSocketUpdate(migration, "Starting validation")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "ValidationStarted", "Monorepo migration validation started")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{RequeueAfter: monoRepoPendingRequeueInterval}, err
	}
	return ctrl.Result{RequeueAfter: monoRepoPendingRequeueInterval}, nil
}

// processValidating validates auth, source repos, and target
func (r *MonoRepoMigrationReconciler) processValidating(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Validating monorepo migration")

	// Validate ADO token
	_, err := r.getAzureDevOpsToken(ctx, migration)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to resolve ADO token: %v", err))
	}

	// Validate GitHub token
	_, err = r.getGitHubToken(ctx, migration)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to resolve GitHub token: %v", err))
	}

	// Run configuration validation
	validationResult, err := r.MigrationService.ValidateMonoRepoMigration(ctx, migration)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Validation error: %v", err))
	}

	if !validationResult.Valid {
		var errMsgs []string
		for _, ve := range validationResult.Errors {
			errMsgs = append(errMsgs, ve.Message)
		}
		return r.failMigration(ctx, migration, fmt.Sprintf("Validation failed: %s", strings.Join(errMsgs, "; ")))
	}

	// Transition to Cloning
	migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseCloning
	migration.Status.Progress.CurrentStep = "Validation passed, starting clone phase"

	r.sendWebSocketUpdate(migration, "Validation passed, starting cloning")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "ValidationPassed", "Monorepo migration validation passed")

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
}

// processCloning clones repos in parallel using the configured parallelClones setting.
// Rewriting and merging remain sequential since they share the same workspace.
func (r *MonoRepoMigrationReconciler) processCloning(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	workDir := r.getWorkspace(migration)

	// Only clean workspace if ALL repos are still Pending (fresh start).
	// Once any repo has entered Cloning or beyond, the workspace may contain
	// valid cloned data that must not be removed.
	if _, err := os.Stat(workDir); err == nil {
		allPending := true
		for _, rs := range migration.Status.RepoStatuses {
			if rs.Phase != migrationv1.MonoRepoRepoPhasePending {
				allPending = false
				break
			}
		}
		if allPending {
			logger.Info("Cleaning up existing workspace before cloning (all repos pending)", "dir", workDir)
			os.RemoveAll(workDir)
		}
	}

	// Collect repos that need cloning (Pending or stuck in Cloning from an interrupted reconcile)
	var pendingRepos []int
	for i, rs := range migration.Status.RepoStatuses {
		if rs.Phase == migrationv1.MonoRepoRepoPhasePending || rs.Phase == migrationv1.MonoRepoRepoPhaseCloning {
			pendingRepos = append(pendingRepos, i)
		}
	}

	if len(pendingRepos) == 0 {
		// All repos done with cloning phase, transition to Rewriting
		migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseRewriting
		migration.Status.Progress.CurrentStep = "All repos cloned, starting rewrite phase"
		r.sendWebSocketUpdate(migration, "Starting rewrite phase")
		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
	}

	// Limit batch to parallelClones
	parallelClones := migration.Spec.Settings.GetParallelClones()
	batch := pendingRepos
	if len(batch) > parallelClones {
		batch = batch[:parallelClones]
	}

	// Get ADO token once for all clones
	adoToken, err := r.getAzureDevOpsToken(ctx, migration)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to get ADO token: %v", err))
	}

	repoNames := make([]string, len(batch))
	for i, idx := range batch {
		repoNames[i] = migration.Status.RepoStatuses[idx].Name
	}
	logger.Info("Starting parallel cloning", "repos", repoNames, "parallelClones", parallelClones)

	// Clone repos in parallel using semaphore + WaitGroup pattern.
	// Each goroutine cleans up its own stale clone dir before cloning,
	// so we never nuke the entire workspace while other clones may exist.
	type cloneResult struct {
		idx    int
		result *services.CloneRepoResult
		err    error
	}

	sem := make(chan struct{}, parallelClones)
	var wg sync.WaitGroup
	results := make(chan cloneResult, len(batch))

	for _, idx := range batch {
		repoName := migration.Status.RepoStatuses[idx].Name
		wg.Add(1)
		go func(repoIdx int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Remove stale clone dir for this specific repo (handles stuck Cloning phase)
			repoDir := fmt.Sprintf("%s/repos/%s", workDir, name)
			if _, statErr := os.Stat(repoDir); statErr == nil {
				logger.Info("Removing stale clone dir before re-clone", "repo", name, "dir", repoDir)
				os.RemoveAll(repoDir)
			}

			cr, cloneErr := r.MigrationService.CloneMonoRepoSource(ctx,
				migration.Spec.Source.Organization,
				migration.Spec.Source.Project,
				name, adoToken, workDir, logger,
				migration.Spec.Settings.CloneDepth)
			results <- cloneResult{idx: repoIdx, result: cr, err: cloneErr}
		}(idx, repoName)
	}

	// Wait for all goroutines then close results channel
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results and update ALL repo statuses in a single batch
	for cr := range results {
		repoStatus := &migration.Status.RepoStatuses[cr.idx]
		repoName := repoStatus.Name

		// Set start time (we set it here instead of a separate status update
		// to avoid a resource version conflict between two consecutive updates)
		if repoStatus.StartTime == nil {
			now := metav1.Now()
			repoStatus.StartTime = &now
		}

		if cr.err != nil {
			logger.Error(cr.err, "Parallel clone failed", "repo", repoName)
			now := metav1.Now()
			repoStatus.Phase = migrationv1.MonoRepoRepoPhaseFailed
			repoStatus.ErrorMessage = cr.err.Error()
			repoStatus.CompletionTime = &now
			migration.Status.Progress.FailedRepos++

			r.Recorder.Event(migration, corev1.EventTypeWarning, "RepoFailed",
				fmt.Sprintf("Failed to clone repo %s: %v", repoName, cr.err))

			if !migration.Spec.Settings.GetContinueOnError() {
				r.updateProgressSummary(migration)
				return r.failMigration(ctx, migration, fmt.Sprintf("Repo %s clone failed: %v", repoName, cr.err))
			}

			r.sendWebSocketUpdate(migration, fmt.Sprintf("Repo %s clone failed (continuing): %v", repoName, cr.err))
		} else {
			// Update repo status with clone results
			repoStatus.DefaultBranch = cr.result.DefaultBranch
			if cr.result.RepoInfo != nil {
				repoStatus.BranchesMigrated = len(cr.result.RepoInfo.Branches)
				repoStatus.TagsMigrated = len(cr.result.RepoInfo.Tags)
				repoStatus.CommitCount = cr.result.RepoInfo.CommitCount
				repoStatus.SizeMB = int64(cr.result.RepoInfo.SizeMB)
			}
			repoStatus.Phase = migrationv1.MonoRepoRepoPhaseRewriting

			r.sendWebSocketUpdate(migration, fmt.Sprintf("Cloned %s successfully", repoName))
			logger.Info("Cloned repo successfully", "repo", repoName)
		}
	}

	// Single status update after all clones complete — avoids resource version conflicts
	migration.Status.Progress.CurrentStep = fmt.Sprintf("Cloned %d repos", len(batch))
	r.updateProgressSummary(migration)
	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
}

// processRewriting rewrites ONE repo per reconcile loop
func (r *MonoRepoMigrationReconciler) processRewriting(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Find the next repo in Rewriting phase (meaning it's been cloned and ready to rewrite)
	repoIdx := r.findNextRepoInPhase(migration, migrationv1.MonoRepoRepoPhaseRewriting)
	if repoIdx == -1 {
		// All repos done with rewriting - transition to Merging
		migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseMerging
		migration.Status.Progress.CurrentStep = "All repos rewritten, starting merge phase"
		r.sendWebSocketUpdate(migration, "Starting merge phase")
		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
	}

	repoStatus := &migration.Status.RepoStatuses[repoIdx]
	repoName := repoStatus.Name
	subdirName := repoStatus.SubdirectoryName

	logger.Info("Rewriting repo", "repo", repoName, "subdir", subdirName)

	workDir := r.getWorkspace(migration)
	bareCloneDir := fmt.Sprintf("%s/repos/%s", workDir, repoName)

	rewriteResult, err := r.MigrationService.RewriteRepoToSubdirectory(ctx,
		bareCloneDir, subdirName, workDir,
		migration.Spec.Settings.GetCleanupBetweenRepos(), logger)
	if err != nil {
		return r.handleRepoError(ctx, migration, repoIdx, err)
	}

	// Single status update after rewrite completes — avoids stale cache issues
	// from doing two Status().Update() calls in the same reconcile
	repoStatus.Phase = migrationv1.MonoRepoRepoPhaseMerging
	repoStatus.BranchesMigrated = len(rewriteResult.Branches)
	repoStatus.TagsMigrated = len(rewriteResult.Tags)
	repoStatus.CommitCount = rewriteResult.CommitCount
	migration.Status.Progress.CurrentStep = fmt.Sprintf("Rewritten %s to %s/", repoName, subdirName)

	r.sendWebSocketUpdate(migration, fmt.Sprintf("Rewritten %s to %s/", repoName, subdirName))
	r.updateProgressSummary(migration)

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
}

// processMerging merges ONE repo per reconcile loop
func (r *MonoRepoMigrationReconciler) processMerging(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	workDir := r.getWorkspace(migration)
	monoRepoDir := fmt.Sprintf("%s/monorepo", workDir)

	// Initialize monorepo on first merge
	needsInit := true
	for _, rs := range migration.Status.RepoStatuses {
		if rs.Phase == migrationv1.MonoRepoRepoPhaseCompleted {
			needsInit = false
			break
		}
	}

	if needsInit {
		defaultBranch := migration.Spec.Target.DefaultBranch
		if defaultBranch == "" {
			defaultBranch = "main"
		}

		_, err := r.MigrationService.InitMonoRepo(ctx, workDir, defaultBranch, logger)
		if err != nil {
			return r.failMigration(ctx, migration, fmt.Sprintf("Failed to initialize monorepo: %v", err))
		}
		logger.Info("Initialized monorepo directory")
	}

	// Find the next repo to merge
	repoIdx := r.findNextRepoInPhase(migration, migrationv1.MonoRepoRepoPhaseMerging)
	if repoIdx == -1 {
		// All repos merged - transition to Pushing
		migration.Status.Phase = migrationv1.MonoRepoMigrationPhasePushing
		migration.Status.Progress.CurrentStep = "All repos merged, pushing to GitHub"
		r.sendWebSocketUpdate(migration, "Starting push phase")
		if err := r.Status().Update(ctx, migration); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
	}

	repoStatus := &migration.Status.RepoStatuses[repoIdx]
	repoName := repoStatus.Name
	subdirName := repoStatus.SubdirectoryName

	logger.Info("Merging repo into monorepo", "repo", repoName)

	rewrittenDir := fmt.Sprintf("%s/repos/%s-work", workDir, repoName)

	// Determine default branch for this repo
	defaultBranch := repoStatus.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// Find the matching source repo spec to get branch info
	var sourceRepo *migrationv1.MonoRepoSourceRepo
	for i := range migration.Spec.SourceRepos {
		if migration.Spec.SourceRepos[i].Name == repoName {
			sourceRepo = &migration.Spec.SourceRepos[i]
			break
		}
	}
	if sourceRepo != nil && sourceRepo.DefaultBranch != "" {
		defaultBranch = sourceRepo.DefaultBranch
	}

	// Build a RewriteRepoResult from stored status for the merge operation
	rewriteResult := &services.RewriteRepoResult{
		RepoName:   repoName,
		SubdirName: subdirName,
		WorkDir:    rewrittenDir,
	}

	// Collect branches and tags from the rewritten repo directory
	r.collectBranchesAndTags(ctx, rewrittenDir, rewriteResult)

	// Extract branch filtering from the source repo spec
	var repoExcludeBranches []string
	var repoIncludeBranches []string
	if sourceRepo != nil {
		if len(sourceRepo.ExcludeBranches) > 0 {
			repoExcludeBranches = sourceRepo.ExcludeBranches
			logger.Info("Branch exclusion patterns configured for repo",
				"repo", repoName, "patterns", repoExcludeBranches)
		}
		if len(sourceRepo.IncludeBranches) > 0 {
			repoIncludeBranches = sourceRepo.IncludeBranches
			logger.Info("Branch inclusion patterns configured for repo",
				"repo", repoName, "patterns", repoIncludeBranches)
		}
	}

	if err := r.MigrationService.MergeRepoIntoMonoRepo(ctx, monoRepoDir, rewrittenDir,
		repoName, defaultBranch, rewriteResult, logger, repoExcludeBranches, repoIncludeBranches); err != nil {
		return r.handleRepoError(ctx, migration, repoIdx, err)
	}

	// Calculate actual migrated branch count (accounting for filtering)
	actualBranchesMigrated := len(rewriteResult.Branches)
	if len(repoExcludeBranches) > 0 || len(repoIncludeBranches) > 0 {
		filtered := services.FilterBranches(rewriteResult.Branches, repoIncludeBranches, repoExcludeBranches, defaultBranch, logger)
		actualBranchesMigrated = len(filtered)
	}

	// Mark repo as completed — single status update to avoid stale cache issues
	now := metav1.Now()
	repoStatus.Phase = migrationv1.MonoRepoRepoPhaseCompleted
	repoStatus.CompletionTime = &now
	repoStatus.BranchesMigrated = actualBranchesMigrated
	repoStatus.TagsMigrated = len(rewriteResult.Tags)

	migration.Status.Progress.CompletedRepos++
	migration.Status.Progress.CurrentStep = fmt.Sprintf("Merged %s into monorepo", repoName)
	r.sendWebSocketUpdate(migration, fmt.Sprintf("Merged %s into monorepo", repoName))
	r.updateProgressSummary(migration)
	r.updateStatistics(migration)

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
}

// processPushing pushes the assembled monorepo to GitHub
func (r *MonoRepoMigrationReconciler) processPushing(ctx context.Context, migration *migrationv1.MonoRepoMigration) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Guard: do not push an empty monorepo if no repos were successfully merged
	hasCompleted := false
	for _, rs := range migration.Status.RepoStatuses {
		if rs.Phase == migrationv1.MonoRepoRepoPhaseCompleted {
			hasCompleted = true
			break
		}
	}
	if !hasCompleted {
		return r.failMigration(ctx, migration, "No repos were successfully merged into the monorepo, aborting push")
	}

	logger.Info("Pushing monorepo to GitHub")

	githubToken, err := r.getGitHubToken(ctx, migration)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to get GitHub token: %v", err))
	}

	owner := migration.Spec.Target.Owner
	repo := migration.Spec.Target.Repository
	defaultBranch := migration.Spec.Target.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// Create GitHub repo if needed
	exists, err := r.GitHubService.CheckRepositoryExists(ctx, githubToken, owner, repo)
	if err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to check target repo: %v", err))
	}

	if !exists {
		settings := &services.GitHubRepoSettings{
			Visibility: migration.Spec.Target.Visibility,
		}
		if _, err := r.GitHubService.CreateRepository(ctx, githubToken, owner, repo, settings); err != nil {
			return r.failMigration(ctx, migration, fmt.Sprintf("Failed to create GitHub repository: %v", err))
		}
		logger.Info("Created GitHub repository", "owner", owner, "repo", repo)
	}

	workDir := r.getWorkspace(migration)
	monoRepoDir := fmt.Sprintf("%s/monorepo", workDir)

	if err := r.MigrationService.PushMonoRepo(ctx, monoRepoDir, githubToken,
		owner, repo, defaultBranch, logger); err != nil {
		return r.failMigration(ctx, migration, fmt.Sprintf("Failed to push monorepo: %v", err))
	}

	// Complete!
	now := metav1.Now()
	migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseCompleted
	migration.Status.CompletionTime = &now
	migration.Status.MonoRepoURL = fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	migration.Status.Progress.CurrentStep = "MonoRepo migration completed successfully"
	migration.Status.Progress.Percentage = 100
	r.updateProgressSummary(migration)
	r.updateStatistics(migration)

	monoRepoMigrationsTotal.WithLabelValues("completed", migration.Namespace).Inc()
	r.sendWebSocketUpdate(migration, "MonoRepo migration completed successfully")
	r.Recorder.Event(migration, corev1.EventTypeNormal, "MigrationCompleted",
		fmt.Sprintf("MonoRepo migration completed: %s", migration.Status.MonoRepoURL))

	// Cleanup workspace
	r.cleanupWorkspace(migration.Name, logger)

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// --- Helper methods ---

// findNextRepoInPhase finds the index of the next repo in the given phase
func (r *MonoRepoMigrationReconciler) findNextRepoInPhase(migration *migrationv1.MonoRepoMigration, phase migrationv1.MonoRepoRepoPhase) int {
	for i, rs := range migration.Status.RepoStatuses {
		if rs.Phase == phase {
			return i
		}
	}
	return -1
}

// allReposAtLeastPhase checks if all non-failed/skipped repos are at least at the given phase
func (r *MonoRepoMigrationReconciler) allReposAtLeastPhase(migration *migrationv1.MonoRepoMigration, targetPhase, currentPhase migrationv1.MonoRepoRepoPhase) bool {
	for _, rs := range migration.Status.RepoStatuses {
		if rs.Phase == currentPhase || rs.Phase == migrationv1.MonoRepoRepoPhasePending {
			return false
		}
	}
	return true
}

// handleRepoError handles an error for a specific repo
func (r *MonoRepoMigrationReconciler) handleRepoError(ctx context.Context, migration *migrationv1.MonoRepoMigration, repoIdx int, err error) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	repoStatus := &migration.Status.RepoStatuses[repoIdx]
	repoName := repoStatus.Name

	logger.Error(err, "Repo migration failed", "repo", repoName)

	now := metav1.Now()
	repoStatus.Phase = migrationv1.MonoRepoRepoPhaseFailed
	repoStatus.ErrorMessage = err.Error()
	repoStatus.CompletionTime = &now
	migration.Status.Progress.FailedRepos++

	r.Recorder.Event(migration, corev1.EventTypeWarning, "RepoFailed",
		fmt.Sprintf("Failed to process repo %s: %v", repoName, err))

	if !migration.Spec.Settings.GetContinueOnError() {
		return r.failMigration(ctx, migration, fmt.Sprintf("Repo %s failed: %v", repoName, err))
	}

	r.sendWebSocketUpdate(migration, fmt.Sprintf("Repo %s failed (continuing): %v", repoName, err))
	r.updateProgressSummary(migration)

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: monoRepoRunningRequeueInterval}, nil
}

// failMigration transitions the migration to Failed
func (r *MonoRepoMigrationReconciler) failMigration(ctx context.Context, migration *migrationv1.MonoRepoMigration, message string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Error(fmt.Errorf("%s", message), "MonoRepo migration failed")

	now := metav1.Now()
	migration.Status.Phase = migrationv1.MonoRepoMigrationPhaseFailed
	migration.Status.ErrorMessage = message
	migration.Status.CompletionTime = &now
	migration.Status.Progress.CurrentStep = "Migration failed"

	monoRepoMigrationsTotal.WithLabelValues("failed", migration.Namespace).Inc()
	r.sendWebSocketUpdate(migration, "Migration failed: "+message)
	r.Recorder.Event(migration, corev1.EventTypeWarning, "MigrationFailed", message)

	if err := r.Status().Update(ctx, migration); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// updateProgressSummary recalculates progress
func (r *MonoRepoMigrationReconciler) updateProgressSummary(migration *migrationv1.MonoRepoMigration) {
	completed := 0
	failed := 0
	skipped := 0
	for _, rs := range migration.Status.RepoStatuses {
		switch rs.Phase {
		case migrationv1.MonoRepoRepoPhaseCompleted:
			completed++
		case migrationv1.MonoRepoRepoPhaseFailed:
			failed++
		case migrationv1.MonoRepoRepoPhaseSkipped:
			skipped++
		}
	}

	total := len(migration.Status.RepoStatuses)
	migration.Status.Progress.CompletedRepos = completed
	migration.Status.Progress.FailedRepos = failed
	migration.Status.Progress.SkippedRepos = skipped
	migration.Status.Progress.ProgressSummary = fmt.Sprintf("%d/%d repos", completed, total)

	if total > 0 {
		// Calculate percentage based on overall phase progress
		done := completed + failed + skipped
		migration.Status.Progress.Percentage = (done * 100) / total
	}
}

// updateStatistics updates aggregate statistics
func (r *MonoRepoMigrationReconciler) updateStatistics(migration *migrationv1.MonoRepoMigration) {
	if migration.Status.Statistics == nil {
		migration.Status.Statistics = &migrationv1.MonoRepoStatistics{}
	}

	totalCommits := 0
	totalBranches := 0
	totalTags := 0
	var totalSize int64

	for _, rs := range migration.Status.RepoStatuses {
		totalCommits += rs.CommitCount
		totalBranches += rs.BranchesMigrated
		totalTags += rs.TagsMigrated
		totalSize += rs.SizeMB
	}

	migration.Status.Statistics.TotalCommits = totalCommits
	migration.Status.Statistics.TotalBranches = totalBranches
	migration.Status.Statistics.TotalTags = totalTags
	migration.Status.Statistics.TotalSizeMB = totalSize

	if migration.Status.StartTime != nil {
		duration := time.Since(migration.Status.StartTime.Time)
		migration.Status.Statistics.Duration = metav1.Duration{Duration: duration}
	}
}

// sendWebSocketUpdate sends a WebSocket update
func (r *MonoRepoMigrationReconciler) sendWebSocketUpdate(migration *migrationv1.MonoRepoMigration, message string) {
	if r.WebSocketManager != nil {
		update := map[string]any{
			"migrationId": migration.Name,
			"namespace":   migration.Namespace,
			"phase":       migration.Status.Phase,
			"progress":    migration.Status.Progress,
			"message":     message,
			"timestamp":   time.Now().UTC().Format(time.RFC3339),
		}
		r.WebSocketManager.BroadcastUpdate("migration_progress", "MonoRepoMigration", migration.Name, update)
	}
}

// getWorkspace returns the workspace directory for a migration
func (r *MonoRepoMigrationReconciler) getWorkspace(migration *migrationv1.MonoRepoMigration) string {
	r.workspacesMutex.Lock()
	defer r.workspacesMutex.Unlock()

	if dir, ok := r.workspaces[migration.Name]; ok {
		return dir
	}

	dir := fmt.Sprintf("/tmp/migrations/monorepo-%s", migration.Name)
	r.workspaces[migration.Name] = dir
	return dir
}

// cleanupWorkspace cleans up the workspace directory
func (r *MonoRepoMigrationReconciler) cleanupWorkspace(migrationName string, logger logr.Logger) {
	r.workspacesMutex.Lock()
	dir, ok := r.workspaces[migrationName]
	if ok {
		delete(r.workspaces, migrationName)
	}
	r.workspacesMutex.Unlock()

	if ok && dir != "" {
		r.MigrationService.CleanupDir(dir, logger)
	}
}

// removeFromActive removes a migration from active tracking
func (r *MonoRepoMigrationReconciler) removeFromActive(namespacedName string) {
	r.activeMigrationMutex.Lock()
	delete(r.activeMigrations, namespacedName)
	r.activeMigrationMutex.Unlock()
}

// getAzureDevOpsToken resolves the ADO token from secrets
func (r *MonoRepoMigrationReconciler) getAzureDevOpsToken(ctx context.Context, migration *migrationv1.MonoRepoMigration) (string, error) {
	auth := migration.Spec.Source.Auth

	if auth.PAT != nil {
		return r.getSecretValue(ctx, migration.Namespace, &auth.PAT.TokenRef)
	}

	if auth.ServicePrincipal != nil {
		clientID, err := r.getSecretValue(ctx, migration.Namespace, &auth.ServicePrincipal.ClientIDRef)
		if err != nil {
			return "", fmt.Errorf("failed to get SP client ID: %w", err)
		}
		clientSecret, err := r.getSecretValue(ctx, migration.Namespace, &auth.ServicePrincipal.ClientSecretRef)
		if err != nil {
			return "", fmt.Errorf("failed to get SP client secret: %w", err)
		}
		tenantID, err := r.getSecretValue(ctx, migration.Namespace, &auth.ServicePrincipal.TenantIDRef)
		if err != nil {
			return "", fmt.Errorf("failed to get SP tenant ID: %w", err)
		}
		return r.MigrationService.GetAzureDevOpsTokenFromServicePrincipal(ctx, clientID, clientSecret, tenantID)
	}

	return "", fmt.Errorf("no Azure DevOps authentication configured")
}

// getGitHubToken resolves the GitHub token from secrets
func (r *MonoRepoMigrationReconciler) getGitHubToken(ctx context.Context, migration *migrationv1.MonoRepoMigration) (string, error) {
	auth := migration.Spec.Target.Auth

	if auth.Token != "" {
		return auth.Token, nil
	}

	if auth.TokenRef != nil {
		return r.getSecretValue(ctx, migration.Namespace, auth.TokenRef)
	}

	if auth.AppAuth != nil {
		logger := log.FromContext(ctx)
		logger.Info("Getting token from GitHub App for monorepo migration")

		appIDStr, err := r.getSecretValue(ctx, migration.Namespace, &auth.AppAuth.AppIdRef)
		if err != nil {
			return "", fmt.Errorf("failed to get GitHub App ID: %w", err)
		}
		installationIDStr, err := r.getSecretValue(ctx, migration.Namespace, &auth.AppAuth.InstallationIdRef)
		if err != nil {
			return "", fmt.Errorf("failed to get GitHub Installation ID: %w", err)
		}

		appID, err := strconv.ParseInt(appIDStr, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub App ID: %w", err)
		}
		installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			return "", fmt.Errorf("invalid GitHub Installation ID: %w", err)
		}

		// Try to get from cache first
		appClient, exists := r.GitHubService.GetAppClient(appID, installationID)
		if !exists {
			// Initialize the app client with private key
			privateKeyPEM, err := r.getSecretValue(ctx, migration.Namespace, &auth.AppAuth.PrivateKeyRef)
			if err != nil {
				return "", fmt.Errorf("failed to get GitHub App private key: %w", err)
			}
			_, err = r.GitHubService.GetClientFromApp(ctx, appID, installationID, []byte(privateKeyPEM))
			if err != nil {
				return "", fmt.Errorf("failed to initialize GitHub App client: %w", err)
			}
			appClient, exists = r.GitHubService.GetAppClient(appID, installationID)
			if !exists {
				return "", fmt.Errorf("failed to get GitHub App client after initialization")
			}
		}

		return appClient.GetToken(ctx)
	}

	return "", fmt.Errorf("no GitHub authentication configured")
}

// getSecretValue reads a value from a Kubernetes secret
func (r *MonoRepoMigrationReconciler) getSecretValue(ctx context.Context, defaultNamespace string, ref *migrationv1.SecretReference) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("secret reference is nil")
	}

	ns := defaultNamespace
	if ref.Namespace != "" {
		ns = ref.Namespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: ns, Name: ref.Name}, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s/%s: %w", ns, ref.Name, err)
	}

	value, exists := secret.Data[ref.Key]
	if !exists {
		return "", fmt.Errorf("key %s not found in secret %s/%s", ref.Key, ns, ref.Name)
	}

	return string(value), nil
}

// collectBranchesAndTags reads branches and tags from a working directory
func (r *MonoRepoMigrationReconciler) collectBranchesAndTags(ctx context.Context, repoDir string, result *services.RewriteRepoResult) {
	// Branches
	branchCmd := fmt.Sprintf("git -C %s branch --list --no-color", repoDir)
	if output, err := execCommand(ctx, branchCmd); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "* "))
			if branch != "" {
				result.Branches = append(result.Branches, branch)
			}
		}
	}

	// Tags
	tagCmd := fmt.Sprintf("git -C %s tag --list", repoDir)
	if output, err := execCommand(ctx, tagCmd); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			tag := strings.TrimSpace(line)
			if tag != "" {
				result.Tags = append(result.Tags, tag)
			}
		}
	}
}

// execCommand is a helper to run a shell command and return output
func execCommand(ctx context.Context, command string) (string, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// SetupWithManager sets up the controller with the Manager
func (r *MonoRepoMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("monorepomigration-controller")
	r.activeMigrations = make(map[string]bool)
	r.workspaces = make(map[string]string)

	return ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1.MonoRepoMigration{}).
		WithEventFilter(GenerationChangedWithLogging()).
		Complete(r)
}
