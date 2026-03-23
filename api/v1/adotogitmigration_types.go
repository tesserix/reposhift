package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AdoToGitMigrationSpec defines the desired state of AdoToGitMigration
type AdoToGitMigrationSpec struct {
	// Source Azure DevOps configuration
	Source AdoSourceConfig `json:"source"`

	// Target GitHub configuration
	Target GitHubTargetConfig `json:"target"`

	// Migration settings
	Settings MigrationSettings `json:"settings"`

	// Resources to migrate
	Resources []MigrationResource `json:"resources"`

	// Migration type (repository, workitems, pipelines, all)
	// +kubebuilder:validation:Enum=repository;workitems;pipelines;all
	Type string `json:"type"`

	// Validation rules
	// +optional
	ValidationRules *ValidationRules `json:"validationRules,omitempty"`
}

// AdoToGitMigrationStatus defines the observed state of AdoToGitMigration
type AdoToGitMigrationStatus struct {
	// Phase of the migration
	Phase MigrationPhase `json:"phase"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Progress tracking
	Progress MigrationProgress `json:"progress"`

	// Resource migration statuses
	ResourceStatuses []ResourceMigrationStatus `json:"resourceStatuses,omitempty"`

	// Validation results
	// +optional
	ValidationResults *ValidationResults `json:"validationResults,omitempty"`

	// Error messages
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Warnings
	// +optional
	Warnings []string `json:"warnings,omitempty"`

	// Migration statistics
	// +optional
	Statistics *MigrationStatistics `json:"statistics,omitempty"`

	// Last reconcile time
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Reconcile generation
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Default branch information
	// +optional
	DefaultBranchInfo *DefaultBranchInfo `json:"defaultBranchInfo,omitempty"`

	// Repository state information
	// +optional
	RepositoryStates []RepositoryStateInfo `json:"repositoryStates,omitempty"`

	// Sync status (for continuous sync)
	// +optional
	SyncStatus *SyncStatus `json:"syncStatus,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.progressSummary"
//+kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
//+kubebuilder:printcolumn:name="Completed",type="integer",JSONPath=".status.progress.completed"
//+kubebuilder:printcolumn:name="Failed",type="integer",JSONPath=".status.progress.failed"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AdoToGitMigration is the Schema for the adotogitmigrations API
type AdoToGitMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AdoToGitMigrationSpec   `json:"spec,omitempty"`
	Status AdoToGitMigrationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AdoToGitMigrationList contains a list of AdoToGitMigration
type AdoToGitMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AdoToGitMigration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AdoToGitMigration{}, &AdoToGitMigrationList{})
}
