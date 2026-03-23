package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PipelineToWorkflowSpec defines the desired state of PipelineToWorkflow
type PipelineToWorkflowSpec struct {
	// Source Azure DevOps pipeline configuration
	Source PipelineSourceConfig `json:"source"`

	// Target GitHub workflow configuration
	Target WorkflowTargetConfig `json:"target"`

	// Conversion settings
	Settings ConversionSettings `json:"settings"`

	// Auto-discovery configuration
	// +optional
	AutoDiscovery *AutoDiscoveryConfig `json:"autoDiscovery,omitempty"`

	// Pipeline resources to convert (ignored if AutoDiscovery is enabled)
	// +optional
	Pipelines []PipelineResource `json:"pipelines,omitempty"`

	// Conversion type (build, release, yaml, classic)
	// +kubebuilder:validation:Enum=build;release;yaml;classic;all
	Type string `json:"type"`

	// Validation rules
	// +optional
	ValidationRules *ConversionValidationRules `json:"validationRules,omitempty"`
}

// AutoDiscoveryConfig defines configuration for automatically discovering pipelines
type AutoDiscoveryConfig struct {
	// Enable automatic discovery of all pipelines from ADO (default: false)
	Enabled bool `json:"enabled"`

	// Include build pipelines (default: true)
	// +optional
	IncludeBuildPipelines bool `json:"includeBuildPipelines,omitempty"`

	// Include release pipelines (default: true)
	// +optional
	IncludeReleasePipelines bool `json:"includeReleasePipelines,omitempty"`

	// Filter pipelines by folder path (e.g., "/Production/*")
	// +optional
	FolderFilter string `json:"folderFilter,omitempty"`

	// Filter pipelines by name pattern (regex)
	// +optional
	NameFilter string `json:"nameFilter,omitempty"`

	// Maximum number of pipelines to discover (0 = no limit)
	// +optional
	MaxPipelines int `json:"maxPipelines,omitempty"`
}

// PipelineSourceConfig defines Azure DevOps pipeline source configuration
type PipelineSourceConfig struct {
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

// WorkflowTargetConfig defines GitHub workflow target configuration
type WorkflowTargetConfig struct {
	// Owner (organization or user)
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	// Repository name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`

	// Authentication configuration
	Auth GitHubAuthConfig `json:"auth"`

	// API URL (optional, defaults to https://api.github.com)
	// +optional
	APIURL string `json:"apiUrl,omitempty"`

	// Workflow directory (default: .github/workflows)
	// +optional
	WorkflowDirectory string `json:"workflowDirectory,omitempty"`

	// Default workflow settings
	// +optional
	DefaultWorkflowSettings *WorkflowSettings `json:"defaultWorkflowSettings,omitempty"`

	// Workflows repository configuration
	// +optional
	WorkflowsRepository *WorkflowsRepositoryConfig `json:"workflowsRepository,omitempty"`
}

// WorkflowsRepositoryConfig defines configuration for the dedicated workflows repository
type WorkflowsRepositoryConfig struct {
	// Create a dedicated repository for workflows (default: false)
	// +optional
	Create bool `json:"create,omitempty"`

	// Name of the workflows repository (default: ado-to-git-migration-workflows)
	// +optional
	Name string `json:"name,omitempty"`

	// Description for the workflows repository
	// +optional
	Description string `json:"description,omitempty"`

	// Make repository private (default: true)
	// +optional
	Private bool `json:"private,omitempty"`

	// Initialize repository with README
	// +optional
	InitializeWithReadme bool `json:"initializeWithReadme,omitempty"`
}

// WorkflowSettings defines GitHub workflow settings
type WorkflowSettings struct {
	// Workflow triggers
	// +optional
	Triggers []WorkflowTrigger `json:"triggers,omitempty"`

	// Environment variables
	// +optional
	EnvironmentVariables map[string]string `json:"environmentVariables,omitempty"`

	// Secrets mapping
	// +optional
	SecretsMapping map[string]string `json:"secretsMapping,omitempty"`

	// Runner labels
	// +optional
	RunnerLabels []string `json:"runnerLabels,omitempty"`

	// Timeout minutes
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=360
	// +optional
	TimeoutMinutes int `json:"timeoutMinutes,omitempty"`
}

// TriggerConfig represents workflow trigger configuration
type TriggerConfig struct {
	// Branches for push triggers
	// +optional
	Branches []string `json:"branches,omitempty"`

	// Paths for push triggers
	// +optional
	Paths []string `json:"paths,omitempty"`

	// Tags for push triggers
	// +optional
	Tags []string `json:"tags,omitempty"`

	// Cron schedule for scheduled triggers
	// +optional
	Cron string `json:"cron,omitempty"`

	// Inputs for workflow_dispatch triggers
	// +optional
	Inputs map[string]string `json:"inputs,omitempty"`
}

// WorkflowTrigger defines workflow trigger configuration
type WorkflowTrigger struct {
	// Trigger type (push, pull_request, schedule, workflow_dispatch)
	// +kubebuilder:validation:Enum=push;pull_request;schedule;workflow_dispatch;release
	Type string `json:"type"`

	// Trigger configuration
	// +optional
	Config *TriggerConfig `json:"config,omitempty"`
}

// ConversionSettings defines pipeline conversion behavior settings
type ConversionSettings struct {
	// Convert variables to environment variables
	// +optional
	ConvertVariables bool `json:"convertVariables,omitempty"`

	// Convert variable groups to secrets
	// +optional
	ConvertVariableGroups bool `json:"convertVariableGroups,omitempty"`

	// Convert service connections
	// +optional
	ConvertServiceConnections bool `json:"convertServiceConnections,omitempty"`

	// Convert artifacts
	// +optional
	ConvertArtifacts bool `json:"convertArtifacts,omitempty"`

	// Convert approvals to environments
	// +optional
	ConvertApprovals bool `json:"convertApprovals,omitempty"`

	// Preserve comments
	// +optional
	PreserveComments bool `json:"preserveComments,omitempty"`

	// Generate matrix builds
	// +optional
	GenerateMatrix bool `json:"generateMatrix,omitempty"`

	// Use composite actions
	// +optional
	UseCompositeActions bool `json:"useCompositeActions,omitempty"`

	// Custom task mappings
	// +optional
	TaskMappings map[string]TaskMapping `json:"taskMappings,omitempty"`

	// Conversion templates
	// +optional
	Templates *ConversionTemplates `json:"templates,omitempty"`

	// Parallel jobs limit
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=20
	// +optional
	ParallelJobs int `json:"parallelJobs,omitempty"`

	// Retry attempts
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=5
	// +optional
	RetryAttempts int `json:"retryAttempts,omitempty"`
}

// TaskMapping defines how Azure DevOps tasks map to GitHub Actions
type TaskMapping struct {
	// GitHub Action to use
	Action string `json:"action"`

	// Version of the action
	// +optional
	Version string `json:"version,omitempty"`

	// Input mappings
	// +optional
	InputMappings map[string]string `json:"inputMappings,omitempty"`

	// Output mappings
	// +optional
	OutputMappings map[string]string `json:"outputMappings,omitempty"`

	// Condition mappings
	// +optional
	ConditionMappings map[string]string `json:"conditionMappings,omitempty"`

	// Custom script
	// +optional
	CustomScript string `json:"customScript,omitempty"`
}

// ConversionTemplates defines conversion templates
type ConversionTemplates struct {
	// Job template
	// +optional
	JobTemplate string `json:"jobTemplate,omitempty"`

	// Step template
	// +optional
	StepTemplate string `json:"stepTemplate,omitempty"`

	// Workflow template
	// +optional
	WorkflowTemplate string `json:"workflowTemplate,omitempty"`

	// Custom templates
	// +optional
	CustomTemplates map[string]string `json:"customTemplates,omitempty"`
}

// PipelineResource defines a pipeline resource to convert
type PipelineResource struct {
	// Pipeline ID
	// +kubebuilder:validation:Required
	ID string `json:"id"`

	// Pipeline name
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Pipeline type (build, release)
	// +kubebuilder:validation:Enum=build;release
	Type string `json:"type"`

	// Target workflow name
	// +kubebuilder:validation:Required
	TargetWorkflowName string `json:"targetWorkflowName"`

	// Resource-specific settings
	// +optional
	Settings *PipelineResourceSettings `json:"settings,omitempty"`

	// Custom mappings for this pipeline
	// +optional
	CustomMappings map[string]string `json:"customMappings,omitempty"`
}

// PipelineResourceSettings defines pipeline-specific conversion settings
type PipelineResourceSettings struct {
	// Override default triggers
	// +optional
	OverrideTriggers []WorkflowTrigger `json:"overrideTriggers,omitempty"`

	// Override runner labels
	// +optional
	OverrideRunnerLabels []string `json:"overrideRunnerLabels,omitempty"`

	// Override environment variables
	// +optional
	OverrideEnvironmentVariables map[string]string `json:"overrideEnvironmentVariables,omitempty"`

	// Include specific stages/jobs
	// +optional
	IncludeStages []string `json:"includeStages,omitempty"`

	// Exclude specific stages/jobs
	// +optional
	ExcludeStages []string `json:"excludeStages,omitempty"`

	// Stage/job mappings
	// +optional
	StageMappings map[string]string `json:"stageMappings,omitempty"`
}

// ConversionValidationRules defines validation rules for conversion
type ConversionValidationRules struct {
	// Skip validation
	// +optional
	SkipValidation bool `json:"skipValidation,omitempty"`

	// Validate syntax
	// +optional
	ValidateSyntax bool `json:"validateSyntax,omitempty"`

	// Validate actions exist
	// +optional
	ValidateActionsExist bool `json:"validateActionsExist,omitempty"`

	// Validate secrets exist
	// +optional
	ValidateSecretsExist bool `json:"validateSecretsExist,omitempty"`

	// Required GitHub features
	// +optional
	RequiredGitHubFeatures []string `json:"requiredGitHubFeatures,omitempty"`
}

// PipelineToWorkflowStatus defines the observed state of PipelineToWorkflow
type PipelineToWorkflowStatus struct {
	// Phase of the conversion
	// +kubebuilder:validation:Enum=Pending;Analyzing;Converting;Validating;Completed;Failed;Cancelled
	Phase ConversionPhase `json:"phase"`

	// Conditions represent the latest available observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Last time the conversion was reconciled
	// +optional
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// Start time
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Completion time
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Conversion progress
	Progress ConversionProgress `json:"progress"`

	// Pipeline conversion statuses
	PipelineStatuses []PipelineConversionStatus `json:"pipelineStatuses,omitempty"`

	// Validation results
	// +optional
	ValidationResults *ConversionValidationResults `json:"validationResults,omitempty"`

	// Generated workflows
	GeneratedWorkflows []GeneratedWorkflow `json:"generatedWorkflows,omitempty"`

	// Error message
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Warnings
	// +optional
	Warnings []string `json:"warnings,omitempty"`

	// Conversion statistics
	// +optional
	Statistics *ConversionStatistics `json:"statistics,omitempty"`
}

// ConversionPhase represents the phase of conversion
type ConversionPhase string

const (
	ConversionPhasePending    ConversionPhase = "Pending"
	ConversionPhaseAnalyzing  ConversionPhase = "Analyzing"
	ConversionPhaseConverting ConversionPhase = "Converting"
	ConversionPhaseValidating ConversionPhase = "Validating"
	ConversionPhaseCompleted  ConversionPhase = "Completed"
	ConversionPhaseFailed     ConversionPhase = "Failed"
	ConversionPhaseCancelled  ConversionPhase = "Cancelled"
)

// ConversionProgress tracks conversion progress
type ConversionProgress struct {
	// Total pipelines to convert
	Total int `json:"total"`

	// Completed pipelines
	Completed int `json:"completed"`

	// Failed pipelines
	Failed int `json:"failed"`

	// Currently processing
	Processing int `json:"processing"`

	// Skipped pipelines
	Skipped int `json:"skipped"`

	// Percentage complete
	Percentage int `json:"percentage"`

	// Current step
	CurrentStep string `json:"currentStep,omitempty"`

	// Current pipeline being processed
	CurrentPipeline string `json:"currentPipeline,omitempty"`

	// Estimated completion time
	// +optional
	EstimatedCompletion *metav1.Time `json:"estimatedCompletion,omitempty"`
}

// PipelineConversionStatus tracks individual pipeline conversion
type PipelineConversionStatus struct {
	// Pipeline ID
	PipelineID string `json:"pipelineId"`

	// Pipeline name
	PipelineName string `json:"pipelineName"`

	// Pipeline type
	PipelineType string `json:"pipelineType"`

	// Target workflow name
	TargetWorkflowName string `json:"targetWorkflowName"`

	// Status
	Status ConversionResourceStatus `json:"status"`

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

	// Generated workflow file path
	// +optional
	WorkflowFilePath string `json:"workflowFilePath,omitempty"`

	// GitHub workflow URL
	// +optional
	WorkflowURL string `json:"workflowUrl,omitempty"`

	// Conversion details
	// +optional
	Details *PipelineConversionDetails `json:"details,omitempty"`
}

// ConversionResourceStatus represents the status of a pipeline conversion
type ConversionResourceStatus string

const (
	ConversionResourceStatusPending    ConversionResourceStatus = "Pending"
	ConversionResourceStatusAnalyzing  ConversionResourceStatus = "Analyzing"
	ConversionResourceStatusConverting ConversionResourceStatus = "Converting"
	ConversionResourceStatusValidating ConversionResourceStatus = "Validating"
	ConversionResourceStatusCompleted  ConversionResourceStatus = "Completed"
	ConversionResourceStatusFailed     ConversionResourceStatus = "Failed"
	ConversionResourceStatusSkipped    ConversionResourceStatus = "Skipped"
)

// PipelineConversionDetails provides detailed conversion information
type PipelineConversionDetails struct {
	// Original pipeline definition
	// +optional
	OriginalDefinition string `json:"originalDefinition,omitempty"`

	// Converted workflow definition
	// +optional
	ConvertedDefinition string `json:"convertedDefinition,omitempty"`

	// Stages converted
	StagesConverted int `json:"stagesConverted,omitempty"`

	// Jobs converted
	JobsConverted int `json:"jobsConverted,omitempty"`

	// Tasks converted
	TasksConverted int `json:"tasksConverted,omitempty"`

	// Variables converted
	VariablesConverted int `json:"variablesConverted,omitempty"`

	// Unsupported features
	UnsupportedFeatures []string `json:"unsupportedFeatures,omitempty"`

	// Manual intervention required
	ManualInterventionRequired []string `json:"manualInterventionRequired,omitempty"`

	// Conversion notes
	ConversionNotes []string `json:"conversionNotes,omitempty"`
}

// GeneratedWorkflow represents a generated GitHub workflow
type GeneratedWorkflow struct {
	// Name of the workflow
	Name string `json:"name"`

	// File path in the repository
	FilePath string `json:"filePath"`

	// Workflow content
	Content string `json:"content"`

	// Source pipeline ID
	SourcePipelineID string `json:"sourcePipelineId"`

	// Source pipeline name
	SourcePipelineName string `json:"sourcePipelineName"`

	// GitHub URL
	// +optional
	GitHubURL string `json:"githubUrl,omitempty"`

	// Validation status
	ValidationStatus string `json:"validationStatus"`

	// Validation errors
	// +optional
	ValidationErrors []string `json:"validationErrors,omitempty"`
}

// ConversionValidationResults contains conversion validation results
type ConversionValidationResults struct {
	// Overall validation status
	Valid bool `json:"valid"`

	// Validation errors
	Errors []ConversionValidationError `json:"errors,omitempty"`

	// Validation warnings
	Warnings []ConversionValidationWarning `json:"warnings,omitempty"`

	// Validation timestamp
	Timestamp metav1.Time `json:"timestamp"`

	// Syntax validation results
	SyntaxValidation map[string]bool `json:"syntaxValidation,omitempty"`

	// Action existence validation
	ActionValidation map[string]bool `json:"actionValidation,omitempty"`

	// Secret existence validation
	SecretValidation map[string]bool `json:"secretValidation,omitempty"`
}

// ConversionValidationError represents a conversion validation error
type ConversionValidationError struct {
	// Error code
	Code string `json:"code"`

	// Error message
	Message string `json:"message"`

	// Pipeline that caused the error
	Pipeline string `json:"pipeline,omitempty"`

	// Stage that caused the error
	Stage string `json:"stage,omitempty"`

	// Job that caused the error
	Job string `json:"job,omitempty"`

	// Task that caused the error
	Task string `json:"task,omitempty"`

	// Line number in the original pipeline
	LineNumber int `json:"lineNumber,omitempty"`
}

// ConversionValidationWarning represents a conversion validation warning
type ConversionValidationWarning struct {
	// Warning code
	Code string `json:"code"`

	// Warning message
	Message string `json:"message"`

	// Pipeline that caused the warning
	Pipeline string `json:"pipeline,omitempty"`

	// Stage that caused the warning
	Stage string `json:"stage,omitempty"`

	// Job that caused the warning
	Job string `json:"job,omitempty"`

	// Task that caused the warning
	Task string `json:"task,omitempty"`

	// Line number in the original pipeline
	LineNumber int `json:"lineNumber,omitempty"`
}

// ConversionStatistics contains conversion statistics
type ConversionStatistics struct {
	// Total duration
	Duration metav1.Duration `json:"duration,omitempty"`

	// Total pipelines analyzed
	PipelinesAnalyzed int `json:"pipelinesAnalyzed,omitempty"`

	// Total workflows generated
	WorkflowsGenerated int `json:"workflowsGenerated,omitempty"`

	// Total stages converted
	StagesConverted int `json:"stagesConverted,omitempty"`

	// Total jobs converted
	JobsConverted int `json:"jobsConverted,omitempty"`

	// Total tasks converted
	TasksConverted int `json:"tasksConverted,omitempty"`

	// Total variables converted
	VariablesConverted int `json:"variablesConverted,omitempty"`

	// Conversion success rate as string (e.g., "85.5%")
	SuccessRate string `json:"successRate,omitempty"`

	// Most common unsupported features
	UnsupportedFeatures map[string]int `json:"unsupportedFeatures,omitempty"`

	// Task conversion statistics
	TaskConversionStats map[string]int `json:"taskConversionStats,omitempty"`

	// API calls made during conversion (by service)
	APICalls map[string]int `json:"apiCalls,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.percentage"
//+kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PipelineToWorkflow is the Schema for the pipelinetoworkflows API
type PipelineToWorkflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PipelineToWorkflowSpec   `json:"spec,omitempty"`
	Status PipelineToWorkflowStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PipelineToWorkflowList contains a list of PipelineToWorkflow
type PipelineToWorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PipelineToWorkflow `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PipelineToWorkflow{}, &PipelineToWorkflowList{})
}
