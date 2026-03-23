package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BatchMigrationSpec defines the desired state of BatchMigration
type BatchMigrationSpec struct {
	// Parent migration job reference
	MigrationJobRef NamespacedName `json:"migrationJobRef"`

	// Batch number (for tracking and ordering)
	BatchNumber int `json:"batchNumber"`

	// Priority (higher = processed first)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	Priority int `json:"priority,omitempty"`

	// Resources in this batch to migrate
	Resources []MigrationResource `json:"resources"`

	// Source Azure DevOps configuration
	Source AdoSourceConfig `json:"source"`

	// Target GitHub configuration
	Target GitHubTargetConfig `json:"target"`

	// Migration settings
	Settings MigrationSettings `json:"settings"`

	// Retry policy for this batch
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`

	// Timeout for this batch processing
	// +optional
	TimeoutMinutes int `json:"timeoutMinutes,omitempty"`
}

// NamespacedName represents a namespaced resource reference
type NamespacedName struct {
	// Namespace of the referenced resource
	Namespace string `json:"namespace"`

	// Name of the referenced resource
	Name string `json:"name"`
}

// RetryPolicy defines retry behavior for failed batches
type RetryPolicy struct {
	// Maximum number of retry attempts
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	MaxRetries int `json:"maxRetries"`

	// Initial retry delay in seconds
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	InitialDelaySeconds int `json:"initialDelaySeconds"`

	// Backoff multiplier for each retry (e.g., 2.0 for exponential backoff)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	BackoffMultiplier int `json:"backoffMultiplier"`

	// Maximum retry delay in seconds
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=7200
	MaxDelaySeconds int `json:"maxDelaySeconds"`
}

// BatchMigrationStatus defines the observed state of BatchMigration
type BatchMigrationStatus struct {
	// Processing phase
	Phase BatchPhase `json:"phase"`

	// Worker pod that claimed this batch
	// +optional
	ClaimedBy string `json:"claimedBy,omitempty"`

	// Timestamp when batch was claimed
	// +optional
	ClaimedAt *metav1.Time `json:"claimedAt,omitempty"`

	// Lease expiration time (for worker health check)
	// +optional
	LeaseExpiresAt *metav1.Time `json:"leaseExpiresAt,omitempty"`

	// Progress tracking
	Progress BatchProgress `json:"progress"`

	// Number of retry attempts
	RetryCount int `json:"retryCount"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Resource migration statuses
	ResourceStatuses []ResourceMigrationStatus `json:"resourceStatuses,omitempty"`

	// Error message if failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Last reconcile time
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Next retry time if applicable
	// +optional
	NextRetryTime *metav1.Time `json:"nextRetryTime,omitempty"`

	// Statistics for this batch
	// +optional
	Statistics *BatchStatistics `json:"statistics,omitempty"`
}

// BatchPhase represents the phase of batch processing
type BatchPhase string

const (
	// BatchPhasePending indicates batch is waiting to be claimed
	BatchPhasePending BatchPhase = "Pending"

	// BatchPhaseClaimed indicates batch has been claimed by a worker
	BatchPhaseClaimed BatchPhase = "Claimed"

	// BatchPhaseProcessing indicates batch is being processed
	BatchPhaseProcessing BatchPhase = "Processing"

	// BatchPhaseCompleted indicates batch processed successfully
	BatchPhaseCompleted BatchPhase = "Completed"

	// BatchPhaseFailed indicates batch processing failed
	BatchPhaseFailed BatchPhase = "Failed"

	// BatchPhaseRetrying indicates batch is waiting for retry
	BatchPhaseRetrying BatchPhase = "Retrying"

	// BatchPhaseStale indicates batch claim has expired (worker died)
	BatchPhaseStale BatchPhase = "Stale"
)

// BatchProgress tracks batch processing progress
type BatchProgress struct {
	// Total resources in batch
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

	// Current step description
	CurrentStep string `json:"currentStep,omitempty"`
}

// BatchStatistics contains batch processing statistics
type BatchStatistics struct {
	// Processing duration
	Duration metav1.Duration `json:"duration,omitempty"`

	// Items migrated per resource type
	ItemsMigrated map[string]int `json:"itemsMigrated,omitempty"`

	// API calls made
	APICalls map[string]int `json:"apiCalls,omitempty"`

	// Data transferred in bytes
	DataTransferred int64 `json:"dataTransferred,omitempty"`

	// Average processing time per resource in seconds
	AvgResourceProcessingTimeSeconds int `json:"avgResourceProcessingTimeSeconds,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.percentage"
//+kubebuilder:printcolumn:name="Batch",type="integer",JSONPath=".spec.batchNumber"
//+kubebuilder:printcolumn:name="Priority",type="integer",JSONPath=".spec.priority"
//+kubebuilder:printcolumn:name="Worker",type="string",JSONPath=".status.claimedBy"
//+kubebuilder:printcolumn:name="Retries",type="integer",JSONPath=".status.retryCount"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+kubebuilder:resource:shortName=bm

// BatchMigration is the Schema for the batchmigrations API
type BatchMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BatchMigrationSpec   `json:"spec,omitempty"`
	Status BatchMigrationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BatchMigrationList contains a list of BatchMigration
type BatchMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BatchMigration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BatchMigration{}, &BatchMigrationList{})
}
