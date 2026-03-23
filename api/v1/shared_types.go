package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MigrationPhase represents the phase of migration
// +kubebuilder:validation:Enum=Pending;Validating;Running;Completed;Failed;Cancelled;Paused;Syncing
type MigrationPhase string

const (
	MigrationPhasePending    MigrationPhase = "Pending"
	MigrationPhaseValidating MigrationPhase = "Validating"
	MigrationPhaseRunning    MigrationPhase = "Running"
	MigrationPhaseCompleted  MigrationPhase = "Completed"
	MigrationPhaseFailed     MigrationPhase = "Failed"
	MigrationPhaseCancelled  MigrationPhase = "Cancelled"
	MigrationPhasePaused     MigrationPhase = "Paused"
	MigrationPhaseSyncing    MigrationPhase = "Syncing"
)

// ResourceStatus represents the status of a resource migration
type ResourceStatus string

const (
	ResourceStatusPending    ResourceStatus = "Pending"
	ResourceStatusValidating ResourceStatus = "Validating"
	ResourceStatusRunning    ResourceStatus = "Running"
	ResourceStatusCompleted  ResourceStatus = "Completed"
	ResourceStatusFailed     ResourceStatus = "Failed"
	ResourceStatusSkipped    ResourceStatus = "Skipped"
	ResourceStatusPaused     ResourceStatus = "Paused"
)

// RepositoryState represents the state of a target repository
// +kubebuilder:validation:Enum=NotExists;Empty;NonEmpty;Created
type RepositoryState string

const (
	RepositoryStateNotExists RepositoryState = "NotExists"
	RepositoryStateEmpty     RepositoryState = "Empty"
	RepositoryStateNonEmpty  RepositoryState = "NonEmpty"
	RepositoryStateCreated   RepositoryState = "Created"
)

// SecretReference defines a reference to a Kubernetes secret
type SecretReference struct {
	// Name of the secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Key within the secret
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// Namespace of the secret (optional, defaults to same namespace)
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// AdoAuthConfig defines Azure DevOps authentication
type AdoAuthConfig struct {
	// Service Principal configuration
	ServicePrincipal *ServicePrincipalConfig `json:"servicePrincipal,omitempty"`

	// Personal Access Token configuration
	PAT *PATConfig `json:"pat,omitempty"`
}

// ServicePrincipalConfig defines Azure Service Principal settings
type ServicePrincipalConfig struct {
	// Client ID reference
	// +kubebuilder:validation:Required
	ClientIDRef SecretReference `json:"clientIdRef"`

	// Client secret reference
	// +kubebuilder:validation:Required
	ClientSecretRef SecretReference `json:"clientSecretRef"`

	// Tenant ID reference
	// +kubebuilder:validation:Required
	TenantIDRef SecretReference `json:"tenantIdRef"`
}

// PATConfig defines Personal Access Token configuration
type PATConfig struct {
	// Token reference
	TokenRef SecretReference `json:"tokenRef"`
}

// GitHubAuthConfig defines GitHub authentication
type GitHubAuthConfig struct {
	// Personal Access Token reference (Option A)
	// +optional
	TokenRef *SecretReference `json:"tokenRef,omitempty"`

	// GitHub App authentication (Option B - Recommended for production)
	// +optional
	AppAuth *GitHubAppAuthConfig `json:"appAuth,omitempty"`

	// Inline OAuth/PAT token (Option C - for OAuth flow from UI)
	// This token is passed directly from the frontend and used temporarily
	// +optional
	Token string `json:"token,omitempty"`
}

// GitHubAppAuthConfig defines GitHub App authentication settings
type GitHubAppAuthConfig struct {
	// App ID reference
	// +kubebuilder:validation:Required
	AppIdRef SecretReference `json:"appIdRef"`

	// Installation ID reference
	// +kubebuilder:validation:Required
	InstallationIdRef SecretReference `json:"installationIdRef"`

	// Private Key reference (PEM format)
	// +kubebuilder:validation:Required
	PrivateKeyRef SecretReference `json:"privateKeyRef"`
}

// MigrationType defines the type of migration for naming conventions
// +kubebuilder:validation:Enum=product;platform;shared
type MigrationType string

const (
	MigrationTypeProduct  MigrationType = "product"
	MigrationTypePlatform MigrationType = "platform"
	MigrationTypeShared   MigrationType = "shared"
)

// AdoSourceConfig defines Azure DevOps source configuration
type AdoSourceConfig struct {
	// Organization name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Organization string `json:"organization"`

	// Project name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Project string `json:"project"`

	// Repository name (optional, can be inferred from resource)
	// +optional
	Repository string `json:"repository,omitempty"`

	// Business unit name for repository naming (e.g., "lg", "ea", "hb")
	// Used in auto-generated GitHub repo names
	// +optional
	BusinessUnit string `json:"businessUnit,omitempty"`

	// Product name for product repositories (e.g., "altitude", "authority")
	// Used in auto-generated GitHub repo names for product migrations
	// +optional
	ProductName string `json:"productName,omitempty"`

	// Migration type determines the naming convention for GitHub repos
	// - product: generates names like "product-<bu>-<product>-<repo>"
	// - platform: generates names like "platform-<bu>-<repo>"
	// - shared: generates names like "shared-<repo>"
	// +optional
	MigrationType MigrationType `json:"migrationType,omitempty"`

	// Whether this is a product repository (deprecated, use migrationType instead)
	// If true, repo will be named: product-<repo_name>
	// If false, repo will be named: <bu_name>-<repo_name>
	// +optional
	IsProductRepo bool `json:"isProductRepo,omitempty"`

	// Authentication configuration
	Auth AdoAuthConfig `json:"auth"`

	// API URL (optional, defaults to https://dev.azure.com)
	// +optional
	APIURL string `json:"apiUrl,omitempty"`
}

// GitHubTargetConfig defines GitHub target configuration
type GitHubTargetConfig struct {
	// Owner (organization or user)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	// Repository name (optional, will be auto-generated based on naming rules if not specified)
	// +optional
	Repository string `json:"repository,omitempty"`

	// Auto-generate repository name based on source metadata
	// If true: uses isProductRepo and businessUnit from source to generate name
	// If false: uses repository name as-is or from source
	// +optional
	AutoGenerateName bool `json:"autoGenerateName,omitempty"`

	// Repository visibility (public, private, internal)
	// +kubebuilder:validation:Enum=public;private;internal
	// +optional
	Visibility string `json:"visibility,omitempty"`

	// Authentication configuration
	Auth GitHubAuthConfig `json:"auth"`

	// API URL (optional, defaults to https://api.github.com)
	// +optional
	APIURL string `json:"apiUrl,omitempty"`

	// Default branch name for target repositories
	// If not specified, will be auto-detected from source repository
	// +optional
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// Default repository settings
	// +optional
	DefaultRepoSettings *GitHubRepoSettings `json:"defaultRepoSettings,omitempty"`
}

// GitHubRepoSettings defines GitHub repository settings
type GitHubRepoSettings struct {
	// Repository visibility (public, private, internal)
	// +kubebuilder:validation:Enum=public;private;internal
	// +optional
	Visibility string `json:"visibility,omitempty"`

	// Auto-initialize repository
	// +optional
	AutoInit bool `json:"autoInit,omitempty"`

	// License template
	// +optional
	LicenseTemplate string `json:"licenseTemplate,omitempty"`

	// Gitignore template
	// +optional
	GitignoreTemplate string `json:"gitignoreTemplate,omitempty"`
}

// MigrationSettings defines migration behavior settings
type MigrationSettings struct {
	// Maximum commit history days (default: 730 for 2 years)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3650
	// +kubebuilder:default=730
	// +optional
	MaxHistoryDays int `json:"maxHistoryDays,omitempty"`

	// Maximum commit count (default: 2000)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50000
	// +optional
	MaxCommitCount int `json:"maxCommitCount,omitempty"`

	// Delay in minutes between batch repository migrations (default: 30)
	// This applies when migrating multiple repositories sequentially
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1440
	// +kubebuilder:default=30
	// +optional
	BatchDelayMinutes int `json:"batchDelayMinutes,omitempty"`

	// Include work items migration
	// +optional
	IncludeWorkItems bool `json:"includeWorkItems,omitempty"`

	// Include builds/releases migration
	// +optional
	IncludeBuilds bool `json:"includeBuilds,omitempty"`

	// Include pull requests
	// +optional
	IncludePullRequests bool `json:"includePullRequests,omitempty"`

	// Include tags
	// +optional
	IncludeTags bool `json:"includeTags,omitempty"`

	// Handle LFS files
	// +optional
	HandleLFS bool `json:"handleLFS,omitempty"`

	// Batch size for processing
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000
	// +optional
	BatchSize int `json:"batchSize,omitempty"`

	// Retry attempts
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +optional
	RetryAttempts int `json:"retryAttempts,omitempty"`

	// Parallel workers
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	// +optional
	ParallelWorkers int `json:"parallelWorkers,omitempty"`

	// Clone depth for shallow cloning (0 = full history, N>0 = shallow with N commits)
	// Shallow cloning significantly reduces clone time and disk usage for large repositories.
	// When set, branches and tags are still fetched but commit history is truncated.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	// +optional
	CloneDepth int `json:"cloneDepth,omitempty"`

	// Repository-specific settings (branch filtering, etc.)
	// These apply globally to all repositories in the migration
	// +optional
	Repository *RepositorySettings `json:"repository,omitempty"`

	// Rate limiting settings
	// +optional
	RateLimit *RateLimitSettings `json:"rateLimit,omitempty"`

	// Continuous sync settings
	// +optional
	Sync *SyncSettings `json:"sync,omitempty"`
}

// RateLimitSettings defines rate limiting configuration
type RateLimitSettings struct {
	// Requests per minute for Azure DevOps
	// +optional
	AdoRequestsPerMinute int `json:"adoRequestsPerMinute,omitempty"`

	// Requests per minute for GitHub
	// +optional
	GitHubRequestsPerMinute int `json:"githubRequestsPerMinute,omitempty"`
}

// SyncSettings defines continuous synchronization configuration
type SyncSettings struct {
	// Enable continuous synchronization
	// When enabled, the operator will periodically sync changes from ADO to GitHub
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Sync interval in minutes (default: 5, min: 1, max: 1440 for 24 hours)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1440
	// +optional
	IntervalMinutes int `json:"intervalMinutes,omitempty"`

	// Sync specific branches only (if empty, sync all branches)
	// +optional
	BranchFilter []string `json:"branchFilter,omitempty"`

	// Sync tags
	// +optional
	SyncTags bool `json:"syncTags,omitempty"`

	// Force push (use with caution, may overwrite GitHub changes)
	// +optional
	ForcePush bool `json:"forcePush,omitempty"`
}

// MigrationResource defines a resource to migrate
type MigrationResource struct {
	// Type of resource (repository, work-item, build, release)
	// +kubebuilder:validation:Enum=repository;work-item;build;release;pipeline
	Type string `json:"type"`

	// Source identifier in Azure DevOps
	// +kubebuilder:validation:Required
	SourceID string `json:"sourceId"`

	// Source name
	// +kubebuilder:validation:Required
	SourceName string `json:"sourceName"`

	// Target name in GitHub
	// +kubebuilder:validation:Required
	TargetName string `json:"targetName"`

	// Additional configuration
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// Resource-specific settings
	// +optional
	Settings *ResourceSettings `json:"settings,omitempty"`
}

// ResourceSettings defines resource-specific migration settings
type ResourceSettings struct {
	// Repository-specific settings
	// +optional
	Repository *RepositorySettings `json:"repository,omitempty"`

	// Work item-specific settings
	// +optional
	WorkItem *WorkItemSettings `json:"workItem,omitempty"`

	// Pipeline-specific settings
	// +optional
	Pipeline *PipelineSettings `json:"pipeline,omitempty"`
}

// RepositorySettings defines repository-specific settings
type RepositorySettings struct {
	// Include specific branches
	// +optional
	IncludeBranches []string `json:"includeBranches,omitempty"`

	// Exclude specific branches
	// +optional
	ExcludeBranches []string `json:"excludeBranches,omitempty"`

	// Branch mapping
	// +optional
	BranchMapping map[string]string `json:"branchMapping,omitempty"`

	// Repository visibility (public, private, internal)
	// +kubebuilder:validation:Enum=public;private;internal
	// +optional
	Visibility string `json:"visibility,omitempty"`

	// Create repository if it doesn't exist
	// +optional
	CreateIfNotExists bool `json:"createIfNotExists,omitempty"`
}

// WorkItemSettings defines work item-specific settings
type WorkItemSettings struct {
	// Work item type mapping
	// +optional
	TypeMapping map[string]string `json:"typeMapping,omitempty"`

	// State mapping
	// +optional
	StateMapping map[string]string `json:"stateMapping,omitempty"`

	// Field mapping
	// +optional
	FieldMapping map[string]string `json:"fieldMapping,omitempty"`
}

// PipelineSettings defines pipeline-specific settings
type PipelineSettings struct {
	// Convert to GitHub Actions
	// +optional
	ConvertToActions bool `json:"convertToActions,omitempty"`

	// Workflow template
	// +optional
	WorkflowTemplate string `json:"workflowTemplate,omitempty"`
}

// ValidationRules defines validation rules for migration
type ValidationRules struct {
	// Skip validation
	// +optional
	SkipValidation bool `json:"skipValidation,omitempty"`

	// Required permissions
	// +optional
	RequiredPermissions []string `json:"requiredPermissions,omitempty"`

	// Pre-migration checks
	// +optional
	PreMigrationChecks []string `json:"preMigrationChecks,omitempty"`
}

// MigrationProgress tracks overall progress
type MigrationProgress struct {
	// Total resources to migrate
	Total int `json:"total"`

	// Completed resources
	Completed int `json:"completed"`

	// Failed resources
	Failed int `json:"failed"`

	// Currently processing
	Processing int `json:"processing"`

	// Skipped resources
	Skipped int `json:"skipped"`

	// Percentage complete
	Percentage int `json:"percentage"`

	// Current step
	CurrentStep string `json:"currentStep,omitempty"`

	// Estimated completion time
	// +optional
	EstimatedCompletion *metav1.Time `json:"estimatedCompletion,omitempty"`

	// Processing rate (items per minute) as string
	// +optional
	ProcessingRate string `json:"processingRate,omitempty"`

	// Current item being processed (1-indexed) - shows which item is currently being migrated
	// +optional
	CurrentItem int `json:"currentItem,omitempty"`

	// Progress summary in "X/Y" format (e.g., "1/2", "2/5") for better visibility
	// +optional
	ProgressSummary string `json:"progressSummary,omitempty"`
}

// ResourceMigrationStatus tracks individual resource migration
type ResourceMigrationStatus struct {
	// Resource identifier
	ResourceID string `json:"resourceId"`

	// Resource type
	Type string `json:"type"`

	// Source name
	SourceName string `json:"sourceName"`

	// Target name
	TargetName string `json:"targetName"`

	// Status
	Status ResourceStatus `json:"status"`

	// Progress percentage
	Progress int `json:"progress"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Error message if failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// GitHub URL if successful
	// +optional
	GitHubURL string `json:"githubUrl,omitempty"`

	// Migration details
	// +optional
	Details *ResourceMigrationDetails `json:"details,omitempty"`

	// Backward compatibility fields
	Name          string `json:"name,omitempty"`
	Error         string `json:"error,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
}

// ResourceMigrationDetails provides detailed migration information
type ResourceMigrationDetails struct {
	// Items processed
	ItemsProcessed int `json:"itemsProcessed,omitempty"`

	// Total items
	TotalItems int `json:"totalItems,omitempty"`

	// Bytes transferred
	BytesTransferred int64 `json:"bytesTransferred,omitempty"`

	// Current operation
	CurrentOperation string `json:"currentOperation,omitempty"`

	// Sub-resources
	SubResources []SubResourceStatus `json:"subResources,omitempty"`
}

// SubResourceStatus tracks sub-resource migration status
type SubResourceStatus struct {
	// Name of the sub-resource
	Name string `json:"name"`

	// Type of the sub-resource
	Type string `json:"type"`

	// Status
	Status ResourceStatus `json:"status"`

	// Progress percentage
	Progress int `json:"progress"`

	// Error message if failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// ValidationResults contains validation results
type ValidationResults struct {
	// Overall validation status
	Valid bool `json:"valid"`

	// Validation errors
	Errors []ValidationError `json:"errors,omitempty"`

	// Validation warnings
	Warnings []ValidationWarning `json:"warnings,omitempty"`

	// Validation timestamp
	Timestamp metav1.Time `json:"timestamp"`
}

// ValidationError represents a validation error
type ValidationError struct {
	// Error code
	Code string `json:"code"`

	// Error message
	Message string `json:"message"`

	// Field that caused the error
	Field string `json:"field,omitempty"`

	// Resource that caused the error
	Resource string `json:"resource,omitempty"`

	// Severity level
	Severity string `json:"severity,omitempty"`

	// Resolution suggestion
	Resolution string `json:"resolution,omitempty"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	// Warning code
	Code string `json:"code"`

	// Warning message
	Message string `json:"message"`

	// Field that caused the warning
	Field string `json:"field,omitempty"`

	// Resource that caused the warning
	Resource string `json:"resource,omitempty"`

	// Suggestion for resolution
	Suggestion string `json:"suggestion,omitempty"`
}

// MigrationStatistics contains migration statistics
type MigrationStatistics struct {
	// Total duration
	Duration metav1.Duration `json:"duration,omitempty"`

	// Total commits migrated
	CommitsMigrated int `json:"commitsMigrated,omitempty"`

	// Total branches migrated
	BranchesMigrated int `json:"branchesMigrated,omitempty"`

	// Total tags migrated
	TagsMigrated int `json:"tagsMigrated,omitempty"`

	// Total work items migrated
	WorkItemsMigrated int `json:"workItemsMigrated,omitempty"`

	// Total pull requests migrated
	PullRequestsMigrated int `json:"pullRequestsMigrated,omitempty"`

	// Total pipelines migrated
	PipelinesMigrated int `json:"pipelinesMigrated,omitempty"`

	// Total repositories created
	RepositoriesCreated int `json:"repositoriesCreated,omitempty"`

	// Total data transferred (bytes)
	DataTransferred int64 `json:"dataTransferred,omitempty"`

	// API calls made
	APICalls map[string]int `json:"apiCalls,omitempty"`
}

// DefaultBranchInfo contains information about default branches
type DefaultBranchInfo struct {
	// Source default branch (detected from ADO)
	// +optional
	SourceDefaultBranch string `json:"sourceDefaultBranch,omitempty"`

	// Target default branch (configured or auto-detected)
	// +optional
	TargetDefaultBranch string `json:"targetDefaultBranch,omitempty"`

	// Whether default branch was auto-detected or explicitly configured
	// +optional
	AutoDetected bool `json:"autoDetected,omitempty"`

	// Detection timestamp
	// +optional
	DetectedAt *metav1.Time `json:"detectedAt,omitempty"`
}

// RepositoryStateInfo contains repository state information
type RepositoryStateInfo struct {
	// Repository name
	RepositoryName string `json:"repositoryName"`

	// Current state of the repository
	State RepositoryState `json:"state"`

	// GitHub repository URL if exists
	// +optional
	GitHubURL string `json:"githubUrl,omitempty"`

	// Whether repository was created during migration
	// +optional
	CreatedDuringMigration bool `json:"createdDuringMigration,omitempty"`

	// State check timestamp
	// +optional
	CheckedAt *metav1.Time `json:"checkedAt,omitempty"`

	// Additional details about the repository state
	// +optional
	Details string `json:"details,omitempty"`
}

// SyncStatus tracks continuous synchronization status
type SyncStatus struct {
	// Whether sync is currently enabled
	Enabled bool `json:"enabled"`

	// Last successful sync time
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Last sync error (if any)
	// +optional
	LastSyncError string `json:"lastSyncError,omitempty"`

	// Number of successful syncs
	SyncCount int `json:"syncCount"`

	// Number of failed syncs
	FailedSyncCount int `json:"failedSyncCount"`

	// Branches synced in last sync
	// +optional
	BranchesSynced []string `json:"branchesSynced,omitempty"`

	// Tags synced in last sync
	// +optional
	TagsSynced []string `json:"tagsSynced,omitempty"`

	// Next sync scheduled time
	// +optional
	NextSyncTime *metav1.Time `json:"nextSyncTime,omitempty"`
}
