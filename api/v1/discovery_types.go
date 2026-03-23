package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AdoDiscoverySpec defines the desired state of AdoDiscovery
type AdoDiscoverySpec struct {
	// Azure DevOps configuration
	Source AdoSourceConfig `json:"source"`

	// Discovery scope
	Scope DiscoveryScope `json:"scope"`

	// Discovery filters
	// +optional
	Filters *DiscoveryFilters `json:"filters,omitempty"`

	// Discovery settings
	// +optional
	Settings *DiscoverySettings `json:"settings,omitempty"`
}

// DiscoveryScope defines what to discover
type DiscoveryScope struct {
	// Include organizations
	// +optional
	Organizations bool `json:"organizations,omitempty"`

	// Include projects
	// +optional
	Projects bool `json:"projects,omitempty"`

	// Include repositories
	// +optional
	Repositories bool `json:"repositories,omitempty"`

	// Include work items
	// +optional
	WorkItems bool `json:"workItems,omitempty"`

	// Include pipelines
	// +optional
	Pipelines bool `json:"pipelines,omitempty"`

	// Include builds
	// +optional
	Builds bool `json:"builds,omitempty"`

	// Include releases
	// +optional
	Releases bool `json:"releases,omitempty"`

	// Include teams
	// +optional
	Teams bool `json:"teams,omitempty"`

	// Include users
	// +optional
	Users bool `json:"users,omitempty"`
}

// DiscoveryFilters defines filters for discovery
type DiscoveryFilters struct {
	// Project name patterns
	// +optional
	ProjectPatterns []string `json:"projectPatterns,omitempty"`

	// Repository name patterns
	// +optional
	RepositoryPatterns []string `json:"repositoryPatterns,omitempty"`

	// Work item types to include
	// +optional
	WorkItemTypes []string `json:"workItemTypes,omitempty"`

	// Pipeline types to include
	// +optional
	PipelineTypes []string `json:"pipelineTypes,omitempty"`

	// Date range for work items and builds
	// +optional
	DateRange *DiscoveryDateRange `json:"dateRange,omitempty"`

	// Include archived items
	// +optional
	IncludeArchived bool `json:"includeArchived,omitempty"`

	// Include disabled items
	// +optional
	IncludeDisabled bool `json:"includeDisabled,omitempty"`
}

// DiscoveryDateRange defines a date range filter for discovery
type DiscoveryDateRange struct {
	// Start date
	// +optional
	Start *metav1.Time `json:"start,omitempty"`

	// End date
	// +optional
	End *metav1.Time `json:"end,omitempty"`
}

// DiscoverySettings defines discovery behavior settings
type DiscoverySettings struct {
	// Maximum items to discover per type
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10000
	// +optional
	MaxItems int `json:"maxItems,omitempty"`

	// Parallel discovery workers
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +optional
	ParallelWorkers int `json:"parallelWorkers,omitempty"`

	// Discovery timeout in minutes
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	// +optional
	TimeoutMinutes int `json:"timeoutMinutes,omitempty"`

	// Cache results
	// +optional
	CacheResults bool `json:"cacheResults,omitempty"`

	// Cache duration in minutes
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1440
	// +optional
	CacheDurationMinutes int `json:"cacheDurationMinutes,omitempty"`
}

// AdoDiscoveryStatus defines the observed state of AdoDiscovery
type AdoDiscoveryStatus struct {
	// Phase of the discovery
	// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed;Cancelled
	Phase DiscoveryPhase `json:"phase"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Discovery progress
	Progress DiscoveryProgress `json:"progress"`

	// Discovered resources
	DiscoveredResources DiscoveredResources `json:"discoveredResources"`

	// Error message
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Warnings
	// +optional
	Warnings []string `json:"warnings,omitempty"`

	// Cache information
	// +optional
	CacheInfo *CacheInfo `json:"cacheInfo,omitempty"`
}

// DiscoveryPhase represents the phase of discovery
type DiscoveryPhase string

const (
	DiscoveryPhasePending   DiscoveryPhase = "Pending"
	DiscoveryPhaseRunning   DiscoveryPhase = "Running"
	DiscoveryPhaseCompleted DiscoveryPhase = "Completed"
	DiscoveryPhaseFailed    DiscoveryPhase = "Failed"
	DiscoveryPhaseCancelled DiscoveryPhase = "Cancelled"
)

// DiscoveryProgress tracks discovery progress
type DiscoveryProgress struct {
	// Current step
	CurrentStep string `json:"currentStep"`

	// Steps completed
	StepsCompleted int `json:"stepsCompleted"`

	// Total steps
	TotalSteps int `json:"totalSteps"`

	// Percentage complete
	Percentage int `json:"percentage"`

	// Items discovered
	ItemsDiscovered int `json:"itemsDiscovered"`

	// Current operation
	CurrentOperation string `json:"currentOperation,omitempty"`
}

// DiscoveredResources contains all discovered resources
type DiscoveredResources struct {
	// Organizations
	Organizations []Organization `json:"organizations,omitempty"`

	// Projects
	Projects []Project `json:"projects,omitempty"`

	// Repositories
	Repositories []Repository `json:"repositories,omitempty"`

	// Work items
	WorkItems []WorkItem `json:"workItems,omitempty"`

	// Pipelines
	Pipelines []Pipeline `json:"pipelines,omitempty"`

	// Builds
	Builds []Build `json:"builds,omitempty"`

	// Releases
	Releases []Release `json:"releases,omitempty"`

	// Teams
	Teams []Team `json:"teams,omitempty"`

	// Users
	Users []User `json:"users,omitempty"`
}

// Organization represents an Azure DevOps organization
type Organization struct {
	// ID of the organization
	ID string `json:"id"`

	// Name of the organization
	Name string `json:"name"`

	// URL of the organization
	URL string `json:"url"`

	// Description
	// +optional
	Description string `json:"description,omitempty"`

	// Region
	// +optional
	Region string `json:"region,omitempty"`

	// Owner
	// +optional
	Owner string `json:"owner,omitempty"`

	// Created date
	// +optional
	CreatedDate *metav1.Time `json:"createdDate,omitempty"`

	// Last modified date
	// +optional
	LastModifiedDate *metav1.Time `json:"lastModifiedDate,omitempty"`
}

// ProjectCapabilities represents project capabilities
type ProjectCapabilities struct {
	// Version control capabilities
	// +optional
	VersionControl *VersionControlCapability `json:"versioncontrol,omitempty"`

	// Process template capabilities
	// +optional
	ProcessTemplate *ProcessTemplateCapability `json:"processTemplate,omitempty"`
}

// VersionControlCapability represents version control capability
type VersionControlCapability struct {
	// Source control type (Git, TFVC)
	// +optional
	SourceControlType string `json:"sourceControlType,omitempty"`

	// Git enabled
	// +optional
	GitEnabled bool `json:"gitEnabled,omitempty"`

	// TFVC enabled
	// +optional
	TfvcEnabled bool `json:"tfvcEnabled,omitempty"`
}

// ProcessTemplateCapability represents process template capability
type ProcessTemplateCapability struct {
	// Template type ID
	// +optional
	TemplateTypeId string `json:"templateTypeId,omitempty"`

	// Template name
	// +optional
	TemplateName string `json:"templateName,omitempty"`

	// Is default template
	// +optional
	IsDefault bool `json:"isDefault,omitempty"`
}

// Project represents an Azure DevOps project
type Project struct {
	// ID of the project
	ID string `json:"id"`

	// Name of the project
	Name string `json:"name"`

	// Description
	// +optional
	Description string `json:"description,omitempty"`

	// URL of the project
	URL string `json:"url"`

	// State (wellFormed, createPending, deleting, new, all)
	State string `json:"state"`

	// Visibility (private, public)
	Visibility string `json:"visibility"`

	// Revision
	Revision int64 `json:"revision"`

	// Last update time
	// +optional
	LastUpdateTime *metav1.Time `json:"lastUpdateTime,omitempty"`

	// Capabilities
	// +optional
	Capabilities *ProjectCapabilities `json:"capabilities,omitempty"`
}

// Repository represents an Azure DevOps repository
type Repository struct {
	// ID of the repository
	ID string `json:"id"`

	// Name of the repository
	Name string `json:"name"`

	// URL of the repository
	URL string `json:"url"`

	// Default branch
	DefaultBranch string `json:"defaultBranch"`

	// Size in bytes
	Size int64 `json:"size"`

	// Is empty
	IsEmpty bool `json:"isEmpty"`

	// Is disabled
	IsDisabled bool `json:"isDisabled"`

	// Project ID
	ProjectID string `json:"projectId"`

	// Project name
	ProjectName string `json:"projectName"`

	// Remote URL
	RemoteURL string `json:"remoteUrl"`

	// SSH URL
	SSHURL string `json:"sshUrl"`

	// Web URL
	WebURL string `json:"webUrl"`

	// Last commit
	// +optional
	LastCommit *Commit `json:"lastCommit,omitempty"`

	// Branch count
	BranchCount int `json:"branchCount"`

	// Tag count
	TagCount int `json:"tagCount"`

	// Pull request count
	PullRequestCount int `json:"pullRequestCount"`
}

// Commit represents a Git commit
type Commit struct {
	// Commit ID
	ID string `json:"id"`

	// Author
	Author string `json:"author"`

	// Committer
	Committer string `json:"committer"`

	// Comment
	Comment string `json:"comment"`

	// Date
	Date metav1.Time `json:"date"`

	// URL
	URL string `json:"url"`
}

// WorkItem represents an Azure DevOps work item
type WorkItem struct {
	// ID of the work item
	ID int `json:"id"`

	// Type of the work item
	Type string `json:"type"`

	// Title
	Title string `json:"title"`

	// State
	State string `json:"state"`

	// Assigned to
	// +optional
	AssignedTo string `json:"assignedTo,omitempty"`

	// Created date
	CreatedDate metav1.Time `json:"createdDate"`

	// Changed date
	ChangedDate metav1.Time `json:"changedDate"`

	// Area path
	// +optional
	AreaPath string `json:"areaPath,omitempty"`

	// Iteration path
	// +optional
	IterationPath string `json:"iterationPath,omitempty"`

	// Tags
	// +optional
	Tags []string `json:"tags,omitempty"`

	// Priority
	// +optional
	Priority string `json:"priority,omitempty"`

	// Severity
	// +optional
	Severity string `json:"severity,omitempty"`

	// URL
	URL string `json:"url"`

	// Project ID
	ProjectID string `json:"projectId"`

	// Project name
	ProjectName string `json:"projectName"`
}

// PipelineVariable represents a pipeline variable
type PipelineVariable struct {
	// Variable name
	Name string `json:"name"`

	// Variable value
	Value string `json:"value"`

	// Is secret
	// +optional
	IsSecret bool `json:"isSecret,omitempty"`

	// Allow override
	// +optional
	AllowOverride bool `json:"allowOverride,omitempty"`
}

// Pipeline represents an Azure DevOps pipeline
type Pipeline struct {
	// ID of the pipeline
	ID int `json:"id"`

	// Name of the pipeline
	Name string `json:"name"`

	// Folder path
	// +optional
	Folder string `json:"folder,omitempty"`

	// Type (build, release)
	Type string `json:"type"`

	// Quality (definition, draft)
	Quality string `json:"quality"`

	// Queue status
	QueueStatus string `json:"queueStatus"`

	// Revision
	Revision int `json:"revision"`

	// Created date
	CreatedDate metav1.Time `json:"createdDate"`

	// URL
	URL string `json:"url"`

	// Project ID
	ProjectID string `json:"projectId"`

	// Project name
	ProjectName string `json:"projectName"`

	// Repository
	// +optional
	Repository *Repository `json:"repository,omitempty"`

	// Variables
	// +optional
	Variables []PipelineVariable `json:"variables,omitempty"`
}

// Build represents an Azure DevOps build
type Build struct {
	// ID of the build
	ID int `json:"id"`

	// Build number
	BuildNumber string `json:"buildNumber"`

	// Status (inProgress, completed, cancelling, postponed, notStarted, all)
	Status string `json:"status"`

	// Result (succeeded, partiallySucceeded, failed, canceled)
	// +optional
	Result string `json:"result,omitempty"`

	// Queue time
	QueueTime metav1.Time `json:"queueTime"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Finish time
	// +optional
	FinishTime *metav1.Time `json:"finishTime,omitempty"`

	// URL
	URL string `json:"url"`

	// Project ID
	ProjectID string `json:"projectId"`

	// Project name
	ProjectName string `json:"projectName"`

	// Pipeline ID
	PipelineID int `json:"pipelineId"`

	// Pipeline name
	PipelineName string `json:"pipelineName"`

	// Source branch
	// +optional
	SourceBranch string `json:"sourceBranch,omitempty"`

	// Source version
	// +optional
	SourceVersion string `json:"sourceVersion,omitempty"`

	// Requested by
	// +optional
	RequestedBy string `json:"requestedBy,omitempty"`

	// Requested for
	// +optional
	RequestedFor string `json:"requestedFor,omitempty"`
}

// Release represents an Azure DevOps release
type Release struct {
	// ID of the release
	ID int `json:"id"`

	// Name of the release
	Name string `json:"name"`

	// Status (draft, active, abandoned)
	Status string `json:"status"`

	// Created on
	CreatedOn metav1.Time `json:"createdOn"`

	// Modified on
	// +optional
	ModifiedOn *metav1.Time `json:"modifiedOn,omitempty"`

	// Created by
	// +optional
	CreatedBy string `json:"createdBy,omitempty"`

	// Modified by
	// +optional
	ModifiedBy string `json:"modifiedBy,omitempty"`

	// URL
	URL string `json:"url"`

	// Project ID
	ProjectID string `json:"projectId"`

	// Project name
	ProjectName string `json:"projectName"`

	// Release definition ID
	ReleaseDefinitionID int `json:"releaseDefinitionId"`

	// Release definition name
	ReleaseDefinitionName string `json:"releaseDefinitionName"`

	// Description
	// +optional
	Description string `json:"description,omitempty"`

	// Environments
	// +optional
	Environments []ReleaseEnvironment `json:"environments,omitempty"`
}

// ReleaseEnvironment represents a release environment
type ReleaseEnvironment struct {
	// ID of the environment
	ID int `json:"id"`

	// Name of the environment
	Name string `json:"name"`

	// Status
	Status string `json:"status"`

	// Rank
	Rank int `json:"rank"`

	// Created on
	// +optional
	CreatedOn *metav1.Time `json:"createdOn,omitempty"`

	// Modified on
	// +optional
	ModifiedOn *metav1.Time `json:"modifiedOn,omitempty"`
}

// Team represents an Azure DevOps team
type Team struct {
	// ID of the team
	ID string `json:"id"`

	// Name of the team
	Name string `json:"name"`

	// Description
	// +optional
	Description string `json:"description,omitempty"`

	// URL
	URL string `json:"url"`

	// Project ID
	ProjectID string `json:"projectId"`

	// Project name
	ProjectName string `json:"projectName"`

	// Identity URL
	// +optional
	IdentityURL string `json:"identityUrl,omitempty"`
}

// User represents an Azure DevOps user
type User struct {
	// ID of the user
	ID string `json:"id"`

	// Display name
	DisplayName string `json:"displayName"`

	// Unique name
	UniqueName string `json:"uniqueName"`

	// URL
	URL string `json:"url"`

	// Image URL
	// +optional
	ImageURL string `json:"imageUrl,omitempty"`

	// Descriptor
	// +optional
	Descriptor string `json:"descriptor,omitempty"`
}

// CacheInfo contains cache information
type CacheInfo struct {
	// Cache hit
	Hit bool `json:"hit"`

	// Cache timestamp
	Timestamp metav1.Time `json:"timestamp"`

	// Cache expiry
	Expiry metav1.Time `json:"expiry"`

	// Cache key
	Key string `json:"key"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.percentage"
//+kubebuilder:printcolumn:name="Items",type="string",JSONPath=".status.progress.itemsDiscovered"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AdoDiscovery is the Schema for the adodiscoveries API
type AdoDiscovery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AdoDiscoverySpec   `json:"spec,omitempty"`
	Status AdoDiscoveryStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AdoDiscoveryList contains a list of AdoDiscovery
type AdoDiscoveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AdoDiscovery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AdoDiscovery{}, &AdoDiscoveryList{})
}
