package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MonoRepoMigrationPhase represents the phase of a monorepo migration
// +kubebuilder:validation:Enum=Pending;Validating;Cloning;Rewriting;Merging;Pushing;Completed;Failed;Cancelled;Paused
type MonoRepoMigrationPhase string

const (
	MonoRepoMigrationPhasePending    MonoRepoMigrationPhase = "Pending"
	MonoRepoMigrationPhaseValidating MonoRepoMigrationPhase = "Validating"
	MonoRepoMigrationPhaseCloning    MonoRepoMigrationPhase = "Cloning"
	MonoRepoMigrationPhaseRewriting  MonoRepoMigrationPhase = "Rewriting"
	MonoRepoMigrationPhaseMerging    MonoRepoMigrationPhase = "Merging"
	MonoRepoMigrationPhasePushing    MonoRepoMigrationPhase = "Pushing"
	MonoRepoMigrationPhaseCompleted  MonoRepoMigrationPhase = "Completed"
	MonoRepoMigrationPhaseFailed     MonoRepoMigrationPhase = "Failed"
	MonoRepoMigrationPhaseCancelled  MonoRepoMigrationPhase = "Cancelled"
	MonoRepoMigrationPhasePaused     MonoRepoMigrationPhase = "Paused"
)

// MonoRepoRepoPhase represents the phase of an individual repo within the monorepo migration
// +kubebuilder:validation:Enum=Pending;Cloning;Rewriting;Merging;Completed;Failed;Skipped
type MonoRepoRepoPhase string

const (
	MonoRepoRepoPhaseCloning   MonoRepoRepoPhase = "Cloning"
	MonoRepoRepoPhaseRewriting MonoRepoRepoPhase = "Rewriting"
	MonoRepoRepoPhaseMerging   MonoRepoRepoPhase = "Merging"
	MonoRepoRepoPhaseCompleted MonoRepoRepoPhase = "Completed"
	MonoRepoRepoPhaseFailed    MonoRepoRepoPhase = "Failed"
	MonoRepoRepoPhaseSkipped   MonoRepoRepoPhase = "Skipped"
	MonoRepoRepoPhasePending   MonoRepoRepoPhase = "Pending"
)

// MonoRepoMigrationSpec defines the desired state of MonoRepoMigration
type MonoRepoMigrationSpec struct {
	// Source Azure DevOps configuration (shared across all repos)
	// +kubebuilder:validation:Required
	Source MonoRepoSourceConfig `json:"source"`

	// Target GitHub configuration for the monorepo
	// +kubebuilder:validation:Required
	Target MonoRepoTargetConfig `json:"target"`

	// List of source repositories to merge into the monorepo
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	SourceRepos []MonoRepoSourceRepo `json:"sourceRepos"`

	// Migration settings
	// +optional
	Settings MonoRepoMigrationSettings `json:"settings,omitempty"`
}

// MonoRepoSourceConfig defines the ADO source configuration for monorepo migration
type MonoRepoSourceConfig struct {
	// Organization name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Organization string `json:"organization"`

	// Project name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Project string `json:"project"`

	// Authentication configuration
	Auth AdoAuthConfig `json:"auth"`

	// API URL (optional, defaults to https://dev.azure.com)
	// +optional
	APIURL string `json:"apiUrl,omitempty"`
}

// MonoRepoTargetConfig defines the GitHub target configuration for the monorepo
type MonoRepoTargetConfig struct {
	// Owner (organization or user)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	// Repository name for the monorepo
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`

	// Repository visibility (public, private, internal)
	// +kubebuilder:validation:Enum=public;private;internal
	// +kubebuilder:default=private
	// +optional
	Visibility string `json:"visibility,omitempty"`

	// Authentication configuration
	Auth GitHubAuthConfig `json:"auth"`

	// API URL (optional, defaults to https://api.github.com)
	// +optional
	APIURL string `json:"apiUrl,omitempty"`

	// Default branch name for the monorepo (default: main)
	// +kubebuilder:default=main
	// +optional
	DefaultBranch string `json:"defaultBranch,omitempty"`
}

// MonoRepoSourceRepo defines a source repository to include in the monorepo
type MonoRepoSourceRepo struct {
	// ADO repository name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Override subdirectory name in the monorepo (defaults to repo name)
	// +optional
	SubdirectoryName string `json:"subdirectoryName,omitempty"`

	// Include only these branches (empty = all branches)
	// +optional
	IncludeBranches []string `json:"includeBranches,omitempty"`

	// Exclude these branches
	// +optional
	ExcludeBranches []string `json:"excludeBranches,omitempty"`

	// Override default branch detection
	// +optional
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// Processing priority (lower = processed first)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	// +optional
	Priority int `json:"priority,omitempty"`
}

// MonoRepoMigrationSettings defines migration behavior settings
type MonoRepoMigrationSettings struct {
	// Retry attempts for failed operations
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	// +optional
	RetryAttempts int `json:"retryAttempts,omitempty"`

	// Continue processing remaining repos if one fails
	// +kubebuilder:default=true
	// +optional
	ContinueOnError *bool `json:"continueOnError,omitempty"`

	// Delete bare clones after rewriting to save disk space
	// +kubebuilder:default=true
	// +optional
	CleanupBetweenRepos *bool `json:"cleanupBetweenRepos,omitempty"`

	// Number of repos to clone in parallel (1-10, default 3)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:default=3
	// +optional
	ParallelClones int `json:"parallelClones,omitempty"`

	// Clone depth for shallow cloning (0 = full history, N>0 = shallow with N commits)
	// Shallow cloning reduces clone time and disk usage. All branches are still fetched
	// but commit history is truncated to the specified depth.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	// +optional
	CloneDepth int `json:"cloneDepth,omitempty"`

	// Disk space limit in MB (0 = unlimited)
	// +kubebuilder:validation:Minimum=0
	// +optional
	DiskSpaceLimitMB int `json:"diskSpaceLimitMB,omitempty"`

	// Rate limiting settings
	// +optional
	RateLimit *RateLimitSettings `json:"rateLimit,omitempty"`
}

// GetContinueOnError returns the value of ContinueOnError with a default of true
func (s *MonoRepoMigrationSettings) GetContinueOnError() bool {
	if s.ContinueOnError == nil {
		return true
	}
	return *s.ContinueOnError
}

// GetCleanupBetweenRepos returns the value of CleanupBetweenRepos with a default of true
func (s *MonoRepoMigrationSettings) GetCleanupBetweenRepos() bool {
	if s.CleanupBetweenRepos == nil {
		return true
	}
	return *s.CleanupBetweenRepos
}

// GetParallelClones returns the number of parallel clone workers, clamped to [1, 10] with default 3
func (s *MonoRepoMigrationSettings) GetParallelClones() int {
	if s.ParallelClones <= 0 {
		return 3
	}
	if s.ParallelClones > 10 {
		return 10
	}
	return s.ParallelClones
}

// MonoRepoMigrationStatus defines the observed state of MonoRepoMigration
type MonoRepoMigrationStatus struct {
	// Phase of the overall migration
	Phase MonoRepoMigrationPhase `json:"phase"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Per-repo status tracking
	RepoStatuses []MonoRepoRepoStatus `json:"repoStatuses,omitempty"`

	// Overall progress
	Progress MonoRepoProgress `json:"progress"`

	// Error message if failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Warnings
	// +optional
	Warnings []string `json:"warnings,omitempty"`

	// Aggregate statistics
	// +optional
	Statistics *MonoRepoStatistics `json:"statistics,omitempty"`

	// Last reconcile time
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Reconcile generation
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// GitHub URL of the created monorepo
	// +optional
	MonoRepoURL string `json:"monoRepoUrl,omitempty"`
}

// MonoRepoRepoStatus tracks the status of an individual repo within the migration
type MonoRepoRepoStatus struct {
	// Repository name
	Name string `json:"name"`

	// Subdirectory name in the monorepo
	SubdirectoryName string `json:"subdirectoryName"`

	// Current phase of this repo
	Phase MonoRepoRepoPhase `json:"phase"`

	// Detected default branch
	// +optional
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// Number of branches migrated
	BranchesMigrated int `json:"branchesMigrated,omitempty"`

	// Number of tags migrated
	TagsMigrated int `json:"tagsMigrated,omitempty"`

	// Number of commits
	CommitCount int `json:"commitCount,omitempty"`

	// Size in MB
	SizeMB int64 `json:"sizeMB,omitempty"`

	// Start time for this repo
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time for this repo
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Error message if failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`
}

// MonoRepoProgress tracks overall monorepo migration progress
type MonoRepoProgress struct {
	// Total repos to process
	TotalRepos int `json:"totalRepos"`

	// Repos completed
	CompletedRepos int `json:"completedRepos"`

	// Repos failed
	FailedRepos int `json:"failedRepos"`

	// Repos skipped
	SkippedRepos int `json:"skippedRepos"`

	// Percentage complete
	Percentage int `json:"percentage"`

	// Current step description
	CurrentStep string `json:"currentStep,omitempty"`

	// Progress summary (e.g. "2/5 repos")
	ProgressSummary string `json:"progressSummary,omitempty"`
}

// MonoRepoStatistics contains aggregate statistics for the monorepo migration
type MonoRepoStatistics struct {
	// Total duration
	Duration metav1.Duration `json:"duration,omitempty"`

	// Total commits across all repos
	TotalCommits int `json:"totalCommits,omitempty"`

	// Total branches across all repos
	TotalBranches int `json:"totalBranches,omitempty"`

	// Total tags across all repos
	TotalTags int `json:"totalTags,omitempty"`

	// Total data size in MB
	TotalSizeMB int64 `json:"totalSizeMB,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.progressSummary"
//+kubebuilder:printcolumn:name="Repos",type="integer",JSONPath=".status.progress.totalRepos"
//+kubebuilder:printcolumn:name="Completed",type="integer",JSONPath=".status.progress.completedRepos"
//+kubebuilder:printcolumn:name="Failed",type="integer",JSONPath=".status.progress.failedRepos"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MonoRepoMigration is the Schema for the monorepomigrations API
type MonoRepoMigration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MonoRepoMigrationSpec   `json:"spec,omitempty"`
	Status MonoRepoMigrationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MonoRepoMigrationList contains a list of MonoRepoMigration
type MonoRepoMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MonoRepoMigration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MonoRepoMigration{}, &MonoRepoMigrationList{})
}
