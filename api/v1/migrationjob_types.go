package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MigrationJobSpec defines the desired state of MigrationJob
type MigrationJobSpec struct {
	// Azure DevOps configuration
	AzureDevOps AzureDevOpsConfig `json:"azureDevOps"`

	// GitHub configuration
	GitHub GitHubConfig `json:"github"`

	// Migration settings
	Settings MigrationJobSettings `json:"settings"`

	// AUTO-DISCOVERY: Enable automatic resource discovery
	// When enabled, resources field is optional and will be populated automatically
	// +optional
	Discovery *DiscoveryConfig `json:"discovery,omitempty"`

	// Resources to migrate (optional if discovery is enabled)
	// +optional
	Resources []MigrationJobResource `json:"resources,omitempty"`
}

// DiscoveryConfig defines automatic resource discovery settings
type DiscoveryConfig struct {
	// Repository discovery configuration
	// +optional
	Repositories *RepositoryDiscoveryConfig `json:"repositories,omitempty"`

	// Work item discovery configuration
	// +optional
	WorkItems *WorkItemDiscoveryConfig `json:"workItems,omitempty"`

	// Pipeline discovery configuration
	// +optional
	Pipelines *PipelineDiscoveryConfig `json:"pipelines,omitempty"`
}

// AzureDevOpsConfig defines Azure DevOps connection settings
type AzureDevOpsConfig struct {
	// Organization name
	Organization string `json:"organization"`

	// Project name
	Project string `json:"project"`

	// Service Principal configuration
	ServicePrincipal ServicePrincipalJobConfig `json:"servicePrincipal"`
}

// ServicePrincipalJobConfig defines Azure Service Principal settings
type ServicePrincipalJobConfig struct {
	// Client ID
	ClientID string `json:"clientId"`

	// Client secret reference
	ClientSecretRef SecretJobReference `json:"clientSecretRef"`

	// Tenant ID
	TenantID string `json:"tenantId"`
}

// GitHubConfig defines GitHub connection settings
type GitHubConfig struct {
	// Organization or user name
	Owner string `json:"owner"`

	// Personal Access Token reference
	TokenRef SecretJobReference `json:"tokenRef"`

	// GitHub API URL (defaults to github.com)
	// +optional
	APIURL string `json:"apiUrl,omitempty"`
}

// SecretJobReference defines a reference to a Kubernetes secret
type SecretJobReference struct {
	// Name of the secret
	Name string `json:"name"`

	// Key within the secret
	Key string `json:"key"`

	// Namespace of the secret (optional, defaults to same namespace)
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// MigrationJobSettings defines migration behavior settings
type MigrationJobSettings struct {
	// Maximum commit history days (default: 730 for 2 years)
	// +optional
	MaxHistoryDays int `json:"maxHistoryDays,omitempty"`

	// Maximum commit count (default: 2000)
	// +optional
	MaxCommitCount int `json:"maxCommitCount,omitempty"`

	// Include work items migration
	// +optional
	IncludeWorkItems bool `json:"includeWorkItems,omitempty"`

	// Include builds/releases migration
	// +optional
	IncludeBuilds bool `json:"includeBuilds,omitempty"`

	// Batch size for processing
	// +optional
	BatchSize int `json:"batchSize,omitempty"`

	// Retry attempts
	// +optional
	RetryAttempts int `json:"retryAttempts,omitempty"`

	// Parallel workers
	// +optional
	ParallelWorkers int `json:"parallelWorkers,omitempty"`
}

// MigrationJobResource defines a resource to migrate
type MigrationJobResource struct {
	// Type of resource (repository, work-item, build, release)
	Type string `json:"type"`

	// Source identifier in Azure DevOps
	SourceID string `json:"sourceId"`

	// Source name
	SourceName string `json:"sourceName"`

	// Target name in GitHub
	TargetName string `json:"targetName"`

	// Additional configuration
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// Resource-specific settings
	// +optional
	Settings *ResourceJobSettings `json:"settings,omitempty"`
}

// ResourceJobSettings defines resource-specific migration settings
type ResourceJobSettings struct {
	// Repository-specific settings
	// +optional
	Repository *RepositoryJobSettings `json:"repository,omitempty"`

	// Work item-specific settings
	// +optional
	WorkItem *WorkItemJobSettings `json:"workItem,omitempty"`

	// Pipeline-specific settings
	// +optional
	Pipeline *PipelineJobSettings `json:"pipeline,omitempty"`
}

// RepositoryJobSettings defines repository-specific settings
type RepositoryJobSettings struct {
	// Include specific branches
	// +optional
	IncludeBranches []string `json:"includeBranches,omitempty"`

	// Exclude specific branches
	// +optional
	ExcludeBranches []string `json:"excludeBranches,omitempty"`

	// Branch mapping
	// +optional
	BranchMapping map[string]string `json:"branchMapping,omitempty"`
}

// WorkItemJobSettings defines work item-specific settings
type WorkItemJobSettings struct {
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

// PipelineJobSettings defines pipeline-specific settings
type PipelineJobSettings struct {
	// Convert to GitHub Actions
	// +optional
	ConvertToActions bool `json:"convertToActions,omitempty"`

	// Workflow template
	// +optional
	WorkflowTemplate string `json:"workflowTemplate,omitempty"`
}

// MigrationJobStatus defines the observed state of MigrationJob
type MigrationJobStatus struct {
	// Phase of the migration job
	Phase MigrationJobPhase `json:"phase"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Progress tracking
	Progress MigrationJobProgress `json:"progress"`

	// Resource migration statuses
	ResourceStatuses []ResourceMigrationJobStatus `json:"resourceStatuses,omitempty"`

	// Error messages
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Warnings
	// +optional
	Warnings []string `json:"warnings,omitempty"`

	// Migration statistics
	// +optional
	Statistics *MigrationJobStatistics `json:"statistics,omitempty"`
}

// MigrationJobPhase represents the phase of migration
type MigrationJobPhase string

const (
	MigrationJobPhasePending    MigrationJobPhase = "Pending"
	MigrationJobPhaseValidating MigrationJobPhase = "Validating"
	MigrationJobPhaseRunning    MigrationJobPhase = "Running"
	MigrationJobPhaseCompleted  MigrationJobPhase = "Completed"
	MigrationJobPhaseFailed     MigrationJobPhase = "Failed"
	MigrationJobPhaseCancelled  MigrationJobPhase = "Cancelled"
	MigrationJobPhasePaused     MigrationJobPhase = "Paused"
)

// MigrationJobProgress tracks overall progress
type MigrationJobProgress struct {
	// Total resources to migrate
	Total int `json:"total"`

	// Completed resources
	Completed int `json:"completed"`

	// Failed resources
	Failed int `json:"failed"`

	// Currently processing
	Processing int `json:"processing"`

	// Skipped resources
	Skipped int `json:"skipped,omitempty"`

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
}

// ResourceMigrationJobStatus tracks individual resource migration
type ResourceMigrationJobStatus struct {
	// Resource identifier
	ResourceID string `json:"resourceId"`

	// Resource type
	Type string `json:"type"`

	// Source name
	SourceName string `json:"sourceName"`

	// Target name
	TargetName string `json:"targetName"`

	// Status
	Status ResourceJobStatus `json:"status"`

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
	Details *ResourceMigrationJobDetails `json:"details,omitempty"`
}

// ResourceJobStatus represents the status of a resource migration
type ResourceJobStatus string

const (
	ResourceJobStatusPending    ResourceJobStatus = "Pending"
	ResourceJobStatusValidating ResourceJobStatus = "Validating"
	ResourceJobStatusRunning    ResourceJobStatus = "Running"
	ResourceJobStatusCompleted  ResourceJobStatus = "Completed"
	ResourceJobStatusFailed     ResourceJobStatus = "Failed"
	ResourceJobStatusSkipped    ResourceJobStatus = "Skipped"
	ResourceJobStatusPaused     ResourceJobStatus = "Paused"
)

// ResourceMigrationJobDetails provides detailed migration information
type ResourceMigrationJobDetails struct {
	// Items processed
	ItemsProcessed int `json:"itemsProcessed,omitempty"`

	// Total items
	TotalItems int `json:"totalItems,omitempty"`

	// Bytes transferred
	BytesTransferred int64 `json:"bytesTransferred,omitempty"`

	// Current operation
	CurrentOperation string `json:"currentOperation,omitempty"`

	// Sub-resources
	SubResources []SubResourceJobStatus `json:"subResources,omitempty"`
}

// SubResourceJobStatus tracks sub-resource migration status
type SubResourceJobStatus struct {
	// Name of the sub-resource
	Name string `json:"name"`

	// Type of the sub-resource
	Type string `json:"type"`

	// Status
	Status ResourceJobStatus `json:"status"`

	// Progress percentage
	Progress int `json:"progress"`

	// Error message if failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// MigrationJobStatistics contains migration statistics
type MigrationJobStatistics struct {
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

	// Total data transferred (bytes)
	DataTransferred int64 `json:"dataTransferred,omitempty"`

	// API calls made
	APICalls map[string]int `json:"apiCalls,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.percentage"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MigrationJob is the Schema for the migrationjobs API
type MigrationJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MigrationJobSpec   `json:"spec,omitempty"`
	Status MigrationJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MigrationJobList contains a list of MigrationJob
type MigrationJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MigrationJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MigrationJob{}, &MigrationJobList{})
}
