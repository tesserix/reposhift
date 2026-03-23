package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkItemMigrationSpec defines the desired state of WorkItemMigration
type WorkItemMigrationSpec struct {
	// Source configuration
	Source WorkItemSource `json:"source"`

	// Target configuration
	Target WorkItemTarget `json:"target"`

	// Migration settings
	Settings WorkItemMigrationSettings `json:"settings"`

	// Work item filters
	Filters WorkItemFilters `json:"filters"`
}

// WorkItemSource defines the source work item configuration
type WorkItemSource struct {
	// Azure DevOps organization
	Organization string `json:"organization"`

	// Azure DevOps project
	Project string `json:"project"`

	// Azure DevOps team/board name (optional)
	// If specified, will filter work items to this team's board
	// Example: "Authority Team" for the Authority Team board
	// +optional
	Team string `json:"team,omitempty"`

	// Authentication configuration
	Auth AdoAuthConfig `json:"auth"`
}

// WorkItemTarget defines the target configuration
type WorkItemTarget struct {
	// GitHub owner
	Owner string `json:"owner"`

	// GitHub repository (required)
	// The repository where GitHub Issues will be created for migrated work items
	// If the repository doesn't exist, it will be automatically created
	// Example: "product-localgov-altitude-backend"
	Repository string `json:"repository"`

	// ProjectRef references a GitHubProject to add migrated issues to (required)
	// The GitHub Project must be created before starting the work item migration
	// Accepts either:
	//   - GitHubProject CR name (e.g., "java-authority-project")
	//   - Actual GitHub Project name (e.g., "Java Authority")
	// The operator will search by CR name first, then by project name
	// See: https://github.com/civica/onboarding-operator-ui/blob/main/docs/HOW_TO_CREATE_GITHUB_PROJECT.md
	ProjectRef string `json:"projectRef"`

	// Authentication configuration
	Auth GitHubAuthConfig `json:"auth"`
}

// WorkItemMigrationSettings defines work item migration settings
type WorkItemMigrationSettings struct {
	// Mapping of work item types to GitHub issue labels
	// +optional
	TypeMapping map[string]string `json:"typeMapping,omitempty"`

	// Mapping of work item states to GitHub issue states
	// +optional
	StateMapping map[string]string `json:"stateMapping,omitempty"`

	// Include work item history/comments
	// Default: true if not specified
	// +optional
	IncludeHistory *bool `json:"includeHistory,omitempty"`

	// Include work item attachments
	// Default: true if not specified
	// +optional
	IncludeAttachments *bool `json:"includeAttachments,omitempty"`

	// Preserve work item relationships
	// Default: true if not specified
	// +optional
	PreserveRelationships *bool `json:"preserveRelationships,omitempty"`

	// Include work item tags as GitHub labels
	// Default: true if not specified
	// +optional
	IncludeTags *bool `json:"includeTags,omitempty"`

	// Combine all comments into a single GitHub comment block
	// When true: Creates one comment with all ADO comment history (preserves author/date metadata)
	// When false: Creates separate GitHub comments for each ADO comment (legacy behavior)
	// Default: true (recommended for better performance and rate limit avoidance)
	// Combining comments reduces API calls by ~90%, significantly improving migration speed
	// +optional
	CombineComments *bool `json:"combineComments,omitempty"`

	// Batch size for processing
	// +optional
	BatchSize int `json:"batchSize,omitempty"`

	// Delay in seconds between batches to avoid rate limits
	// Default: 60 seconds (1 minute)
	// For large migrations with strict rate limits, increase to 120-300 seconds
	// For small migrations or testing, can decrease to 5-30 seconds
	// +optional
	BatchDelaySeconds int `json:"batchDelaySeconds,omitempty"`

	// Delay in milliseconds between individual work items within a batch
	// Helps avoid GitHub secondary rate limits by spacing out API calls
	// Default: 1000ms (1 second) - safer for migrations with history enabled
	// For migrations without history, can decrease to 500ms
	// For very aggressive migrations, decrease to 200-300ms (increased risk of rate limits)
	// For conservative migrations, increase to 1500-2000ms
	// Set to 0 to disable (not recommended for large batches)
	// +optional
	PerItemDelayMs int `json:"perItemDelayMs,omitempty"`

	// Timeout in minutes for the entire migration
	// Default: 360 minutes (6 hours)
	// For very large migrations (5000+ items), increase to 1440 (24 hours) or more
	// +optional
	TimeoutMinutes int `json:"timeoutMinutes,omitempty"`

	// Progress update interval in seconds
	// How often to update the CRD status during migration
	// Default: 30 seconds
	// +optional
	ProgressUpdateIntervalSeconds int `json:"progressUpdateIntervalSeconds,omitempty"`

	// Field mapping
	// +optional
	FieldMapping map[string]string `json:"fieldMapping,omitempty"`
}

// WorkItemFilters defines filters for work items to migrate
type WorkItemFilters struct {
	// Work item types to include
	// +optional
	Types []string `json:"types,omitempty"`

	// Work item states to include
	// +optional
	States []string `json:"states,omitempty"`

	// Date range filter
	// +optional
	DateRange *WorkItemDateRange `json:"dateRange,omitempty"`

	// Area paths to include
	// +optional
	AreaPaths []string `json:"areaPaths,omitempty"`

	// Iteration paths to include
	// +optional
	IterationPaths []string `json:"iterationPaths,omitempty"`

	// Tags to include
	// +optional
	Tags []string `json:"tags,omitempty"`

	// Assigned to users
	// +optional
	AssignedTo []string `json:"assignedTo,omitempty"`

	// WIQL query for advanced filtering
	// +optional
	WIQLQuery string `json:"wiqlQuery,omitempty"`
}

// WorkItemDateRange defines a date range filter for work items
type WorkItemDateRange struct {
	// Start date
	// +optional
	Start *metav1.Time `json:"start,omitempty"`

	// End date
	// +optional
	End *metav1.Time `json:"end,omitempty"`
}

// WorkItemMigrationStatus defines the observed state of WorkItemMigration
type WorkItemMigrationStatus struct {
	// Phase of the migration
	Phase MigrationPhase `json:"phase"`

	// Conditions
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Last time the migration was reconciled
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Progress details
	Progress WorkItemMigrationProgress `json:"progress"`

	// Migrated work items
	MigratedItems []MigratedWorkItem `json:"migratedItems,omitempty"`

	// Error message
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Warnings
	// +optional
	Warnings []string `json:"warnings,omitempty"`

	// Migration statistics
	// +optional
	Statistics *WorkItemMigrationStatistics `json:"statistics,omitempty"`
}

// WorkItemMigrationProgress tracks work item migration progress
type WorkItemMigrationProgress struct {
	// Current step
	CurrentStep string `json:"currentStep"`

	// Work items discovered
	ItemsDiscovered int `json:"itemsDiscovered"`

	// Work items migrated
	ItemsMigrated int `json:"itemsMigrated"`

	// Work items failed
	ItemsFailed int `json:"itemsFailed"`

	// Work items skipped
	ItemsSkipped int `json:"itemsSkipped"`

	// Percentage complete
	Percentage int `json:"percentage"`

	// Current batch
	CurrentBatch int `json:"currentBatch"`

	// Total batches
	TotalBatches int `json:"totalBatches"`

	// Estimated completion time
	// +optional
	EstimatedCompletion *metav1.Time `json:"estimatedCompletion,omitempty"`
}

// MigratedWorkItem represents a successfully migrated work item
type MigratedWorkItem struct {
	// Source work item ID
	SourceID int `json:"sourceId"`

	// Source work item type
	SourceType string `json:"sourceType"`

	// Source work item title
	SourceTitle string `json:"sourceTitle"`

	// Target GitHub issue number
	TargetIssueNumber int `json:"targetIssueNumber"`

	// Target GitHub issue URL
	TargetURL string `json:"targetUrl"`

	// Migration timestamp
	MigratedAt metav1.Time `json:"migratedAt"`
}

// WorkItemMigrationStatistics contains work item migration statistics
type WorkItemMigrationStatistics struct {
	// Total duration
	Duration metav1.Duration `json:"duration,omitempty"`

	// Total work items discovered
	ItemsDiscovered int `json:"itemsDiscovered,omitempty"`

	// Total work items migrated
	ItemsMigrated int `json:"itemsMigrated,omitempty"`

	// Total work items failed
	ItemsFailed int `json:"itemsFailed,omitempty"`

	// Total work items skipped
	ItemsSkipped int `json:"itemsSkipped,omitempty"`

	// Total comments migrated
	CommentsMigrated int `json:"commentsMigrated,omitempty"`

	// Total attachments migrated
	AttachmentsMigrated int `json:"attachmentsMigrated,omitempty"`

	// Total data transferred (bytes)
	DataTransferred int64 `json:"dataTransferred,omitempty"`

	// API calls made
	APICalls map[string]int `json:"apiCalls,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.percentage"
//+kubebuilder:printcolumn:name="Items",type="string",JSONPath=".status.progress.itemsMigrated"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// WorkItemMigration is the Schema for the workitemmigrations API
type WorkItemMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkItemMigrationSpec   `json:"spec,omitempty"`
	Status WorkItemMigrationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WorkItemMigrationList contains a list of WorkItemMigration
type WorkItemMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkItemMigration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkItemMigration{}, &WorkItemMigrationList{})
}
