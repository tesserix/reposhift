package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitHubProjectTemplate defines the available GitHub Project templates
// +kubebuilder:validation:Enum=team-planning;kanban;feature-release;bug-tracker;iterative-development;product-launch;roadmap;team-retrospective;board;blank
type GitHubProjectTemplate string

const (
	// ProjectTemplateTeamPlanning represents a team planning template
	ProjectTemplateTeamPlanning GitHubProjectTemplate = "team-planning"

	// ProjectTemplateKanban represents a kanban board template
	ProjectTemplateKanban GitHubProjectTemplate = "kanban"

	// ProjectTemplateFeatureRelease represents a feature release template
	ProjectTemplateFeatureRelease GitHubProjectTemplate = "feature-release"

	// ProjectTemplateBugTracker represents a bug tracker template
	ProjectTemplateBugTracker GitHubProjectTemplate = "bug-tracker"

	// ProjectTemplateIterativeDevelopment represents an iterative development template
	ProjectTemplateIterativeDevelopment GitHubProjectTemplate = "iterative-development"

	// ProjectTemplateProductLaunch represents a product launch template
	ProjectTemplateProductLaunch GitHubProjectTemplate = "product-launch"

	// ProjectTemplateRoadmap represents a roadmap template
	ProjectTemplateRoadmap GitHubProjectTemplate = "roadmap"

	// ProjectTemplateTeamRetrospective represents a team retrospective template
	ProjectTemplateTeamRetrospective GitHubProjectTemplate = "team-retrospective"

	// ProjectTemplateBoard represents a basic board template
	ProjectTemplateBoard GitHubProjectTemplate = "board"

	// ProjectTemplateBlank represents a blank project
	ProjectTemplateBlank GitHubProjectTemplate = "blank"
)

// GitHubProjectSpec defines the desired state of GitHubProject
type GitHubProjectSpec struct {
	// Owner is the GitHub organization or user that owns the project
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	// ProjectName is the name of the GitHub project to create
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	ProjectName string `json:"projectName"`

	// Description is the optional description for the project
	// +optional
	Description string `json:"description,omitempty"`

	// Template specifies which GitHub Project template to use
	// Options: team-planning, kanban, feature-release, bug-tracker, iterative-development,
	// product-launch, roadmap, team-retrospective, board, blank
	// If not specified, defaults to "blank" to allow custom status field configuration
	// +optional
	// +kubebuilder:default=blank
	Template GitHubProjectTemplate `json:"template,omitempty"`

	// Public determines if the project is public or private
	// +kubebuilder:default=true
	// +optional
	Public bool `json:"public,omitempty"`

	// Repository optionally links the project to a specific repository
	// Format: "owner/repo"
	// +optional
	Repository string `json:"repository,omitempty"`

	// Auth contains the GitHub authentication configuration
	// +kubebuilder:validation:Required
	Auth GitHubAuthConfig `json:"auth"`

	// AutoLink automatically links issues from the repository to this project
	// Only works if Repository is specified
	// +kubebuilder:default=true
	// +optional
	AutoLink bool `json:"autoLink,omitempty"`

	// StatusField defines custom status field configuration for the project
	// If not specified, the template default status columns will be used
	// Note: "Status" is a reserved field name in GitHub Projects. Use names like
	// "ADO Status", "Work Item Status", or "Issue Status" instead.
	// +optional
	StatusField *StatusFieldConfig `json:"statusField,omitempty"`
}

// StatusFieldConfig defines the configuration for a custom status field
type StatusFieldConfig struct {
	// Name is the name of the status field
	// Note: "Status" is reserved by GitHub. Use "ADO Status", "Work Item Status", etc.
	// +kubebuilder:default="ADO Status"
	// +optional
	Name string `json:"name,omitempty"`

	// Options is the list of status options/columns in order
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=20
	Options []StatusOption `json:"options"`
}

// StatusOption defines a single status option
type StatusOption struct {
	// Name is the display name of the status option
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=100
	Name string `json:"name"`

	// Description is an optional description for this status
	// +optional
	Description string `json:"description,omitempty"`

	// Color is the color for this status (optional)
	// Valid values: gray, blue, green, yellow, orange, red, pink, purple
	// +optional
	// +kubebuilder:validation:Enum=gray;blue;green;yellow;orange;red;pink;purple
	Color string `json:"color,omitempty"`
}

// GitHubProjectStatus defines the observed state of GitHubProject
type GitHubProjectStatus struct {
	// Phase represents the current phase of the project creation
	// +kubebuilder:validation:Enum=Pending;Creating;Ready;Failed
	Phase ProjectPhase `json:"phase,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed spec
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ProjectID is the GitHub Project ID (node ID)
	// +optional
	ProjectID string `json:"projectId,omitempty"`

	// ProjectNumber is the GitHub Project number
	// +optional
	ProjectNumber int `json:"projectNumber,omitempty"`

	// ProjectURL is the URL to the created GitHub Project
	// +optional
	ProjectURL string `json:"projectUrl,omitempty"`

	// Message provides additional information about the project status
	// +optional
	Message string `json:"message,omitempty"`

	// ErrorMessage contains error details if the project creation failed
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// CreatedAt is the timestamp when the project was created
	// +optional
	CreatedAt *metav1.Time `json:"createdAt,omitempty"`

	// LastUpdatedAt is the timestamp of the last status update
	// +optional
	LastUpdatedAt *metav1.Time `json:"lastUpdatedAt,omitempty"`

	// Conditions represent the latest available observations of the project's state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ProjectPhase represents the phase of project creation
type ProjectPhase string

const (
	// ProjectPhasePending indicates the project creation is pending
	ProjectPhasePending ProjectPhase = "Pending"

	// ProjectPhaseCreating indicates the project is being created
	ProjectPhaseCreating ProjectPhase = "Creating"

	// ProjectPhaseReady indicates the project is ready and created
	ProjectPhaseReady ProjectPhase = "Ready"

	// ProjectPhaseFailed indicates the project creation failed
	ProjectPhaseFailed ProjectPhase = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ghproject;ghproj
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectName`
// +kubebuilder:printcolumn:name="Template",type=string,JSONPath=`.spec.template`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Project ID",type=string,JSONPath=`.status.projectId`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.projectUrl`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GitHubProject is the Schema for the githubprojects API
type GitHubProject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitHubProjectSpec   `json:"spec,omitempty"`
	Status GitHubProjectStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GitHubProjectList contains a list of GitHubProject
type GitHubProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitHubProject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitHubProject{}, &GitHubProjectList{})
}
