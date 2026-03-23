package v1

// RepositoryDiscoveryConfig defines how to discover repositories automatically
type RepositoryDiscoveryConfig struct {
	// Auto-discover repositories from Azure DevOps
	Enabled bool `json:"enabled"`

	// Discovery mode
	// +kubebuilder:validation:Enum=all;include;exclude
	// +kubebuilder:default=all
	// +optional
	Mode string `json:"mode,omitempty"`

	// Include patterns (glob-style)
	// Examples: ["platform-*", "service-*", "*-api"]
	// +optional
	IncludePatterns []string `json:"includePatterns,omitempty"`

	// Exclude patterns (glob-style)
	// Examples: ["temp-*", "*-archived", "test-*"]
	// +optional
	ExcludePatterns []string `json:"excludePatterns,omitempty"`

	// Target naming convention
	// +optional
	NamingConvention *NamingConvention `json:"namingConvention,omitempty"`
}

// NamingConvention defines how to name target repositories
type NamingConvention struct {
	// Strategy for naming target repos
	// +kubebuilder:validation:Enum=same;prefix;suffix;template
	// +kubebuilder:default=same
	Strategy string `json:"strategy"`

	// Prefix to add to target repo name
	// Example: "migrated-" → "migrated-my-repo"
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// Suffix to add to target repo name
	// Example: "-github" → "my-repo-github"
	// +optional
	Suffix string `json:"suffix,omitempty"`

	// Template for target repo name
	// Variables: {{.SourceName}}, {{.Project}}, {{.Organization}}
	// Example: "{{.Project}}-{{.SourceName}}" → "platform-team-my-repo"
	// +optional
	Template string `json:"template,omitempty"`

	// Transform strategy
	// +kubebuilder:validation:Enum=none;lowercase;uppercase;kebab-case;snake_case
	// +kubebuilder:default=none
	// +optional
	Transform string `json:"transform,omitempty"`
}

// WorkItemDiscoveryConfig defines how to discover work items automatically
type WorkItemDiscoveryConfig struct {
	// Auto-discover work items from Azure DevOps
	Enabled bool `json:"enabled"`

	// WIQL query to discover work items
	// If not specified, discovers all work items in the project
	// +optional
	Query string `json:"query,omitempty"`

	// Area paths to include
	// +optional
	AreaPaths []string `json:"areaPaths,omitempty"`

	// Work item types to include
	// +optional
	Types []string `json:"types,omitempty"`

	// Work item states to include
	// +optional
	States []string `json:"states,omitempty"`
}

// PipelineDiscoveryConfig defines how to discover pipelines automatically
type PipelineDiscoveryConfig struct {
	// Auto-discover pipelines from Azure DevOps
	Enabled bool `json:"enabled"`

	// Include patterns for pipeline names
	// +optional
	IncludePatterns []string `json:"includePatterns,omitempty"`

	// Exclude patterns for pipeline names
	// +optional
	ExcludePatterns []string `json:"excludePatterns,omitempty"`

	// Only include enabled pipelines
	// +kubebuilder:default=true
	// +optional
	OnlyEnabled bool `json:"onlyEnabled,omitempty"`
}
