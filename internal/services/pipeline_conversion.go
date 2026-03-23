package services

import (
	"context"
	"fmt"
	"time"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// PipelineConversionService handles Azure DevOps pipeline to GitHub Actions conversion
type PipelineConversionService struct {
	rateLimiter *RateLimiter
}

// NewPipelineConversionService creates a new pipeline conversion service
func NewPipelineConversionService() *PipelineConversionService {
	return &PipelineConversionService{
		rateLimiter: NewRateLimiter(30, time.Minute), // 30 conversions per minute
	}
}

// ConversionPreview represents a preview of pipeline conversion
type ConversionPreview struct {
	Workflows                  []WorkflowPreview `json:"workflows"`
	ConversionNotes            []string          `json:"conversionNotes"`
	UnsupportedFeatures        []string          `json:"unsupportedFeatures"`
	ManualInterventionRequired []string          `json:"manualInterventionRequired"`
}

// WorkflowPreview represents a preview of a converted workflow
type WorkflowPreview struct {
	Name               string   `json:"name"`
	FilePath           string   `json:"filePath"`
	Content            string   `json:"content"`
	SourcePipelineID   string   `json:"sourcePipelineId"`
	SourcePipelineName string   `json:"sourcePipelineName"`
	ValidationStatus   string   `json:"validationStatus"`
	ValidationErrors   []string `json:"validationErrors,omitempty"`
}

// ConversionValidationResult represents validation results for pipeline conversion
type ConversionValidationResult struct {
	Valid            bool                `json:"valid"`
	Errors           []ConversionError   `json:"errors,omitempty"`
	Warnings         []ConversionWarning `json:"warnings,omitempty"`
	SyntaxValidation map[string]bool     `json:"syntaxValidation,omitempty"`
	ActionValidation map[string]bool     `json:"actionValidation,omitempty"`
	SecretValidation map[string]bool     `json:"secretValidation,omitempty"`
	Timestamp        time.Time           `json:"timestamp"`
}

// ConversionError represents a conversion error
type ConversionError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Pipeline   string `json:"pipeline,omitempty"`
	Stage      string `json:"stage,omitempty"`
	Job        string `json:"job,omitempty"`
	Task       string `json:"task,omitempty"`
	LineNumber int    `json:"lineNumber,omitempty"`
}

// ConversionWarning represents a conversion warning
type ConversionWarning struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Pipeline   string `json:"pipeline,omitempty"`
	Stage      string `json:"stage,omitempty"`
	Job        string `json:"job,omitempty"`
	Task       string `json:"task,omitempty"`
	LineNumber int    `json:"lineNumber,omitempty"`
}

// PipelineAnalysis represents analysis results for a pipeline
type PipelineAnalysis struct {
	PipelineID          string         `json:"pipelineId"`
	PipelineName        string         `json:"pipelineName"`
	Type                string         `json:"type"`
	Complexity          string         `json:"complexity"`
	EstimatedEffort     string         `json:"estimatedEffort"`
	SupportedFeatures   []string       `json:"supportedFeatures"`
	UnsupportedFeatures []string       `json:"unsupportedFeatures"`
	RequiredActions     []string       `json:"requiredActions"`
	TaskBreakdown       map[string]int `json:"taskBreakdown"`
	Recommendations     []string       `json:"recommendations"`
}

// ConversionTemplate represents a conversion template
type ConversionTemplate struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Template    map[string]interface{} `json:"template"`
	Variables   []string               `json:"variables"`
}

// TaskMapping represents a mapping from Azure DevOps task to GitHub Action
type TaskMapping struct {
	AzureTask      string            `json:"azureTask"`
	GitHubAction   string            `json:"githubAction"`
	Version        string            `json:"version"`
	InputMappings  map[string]string `json:"inputMappings"`
	OutputMappings map[string]string `json:"outputMappings"`
	Notes          string            `json:"notes"`
}

// PreviewConversion generates a preview of pipeline conversion
func (s *PipelineConversionService) PreviewConversion(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (*ConversionPreview, error) {
	if err := s.rateLimiter.Wait(ctx, "preview-conversion"); err != nil {
		return nil, err
	}

	preview := &ConversionPreview{
		Workflows:                  []WorkflowPreview{},
		ConversionNotes:            []string{},
		UnsupportedFeatures:        []string{},
		ManualInterventionRequired: []string{},
	}

	// Process each pipeline
	for _, pipeline := range pipelineConversion.Spec.Pipelines {
		workflow, err := s.convertPipelineToWorkflow(ctx, pipeline, pipelineConversion)
		if err != nil {
			return nil, fmt.Errorf("failed to convert pipeline %s: %w", pipeline.Name, err)
		}

		preview.Workflows = append(preview.Workflows, *workflow)
	}

	// Add general conversion notes
	preview.ConversionNotes = append(preview.ConversionNotes,
		"Azure DevOps variables converted to GitHub environment variables",
		"Service connections require manual setup in GitHub",
		"Approval gates converted to GitHub Environments with protection rules",
	)

	// Add unsupported features
	preview.UnsupportedFeatures = append(preview.UnsupportedFeatures,
		"Azure DevOps Artifacts - manual migration required",
		"Custom Azure DevOps extensions - need GitHub Action alternatives",
		"Azure DevOps Test Plans - not directly supported in GitHub",
	)

	return preview, nil
}

// ValidateConversion validates a pipeline conversion configuration
func (s *PipelineConversionService) ValidateConversion(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) (*ConversionValidationResult, error) {
	if err := s.rateLimiter.Wait(ctx, "validate-conversion"); err != nil {
		return nil, err
	}

	result := &ConversionValidationResult{
		Valid:            true,
		Errors:           []ConversionError{},
		Warnings:         []ConversionWarning{},
		SyntaxValidation: make(map[string]bool),
		ActionValidation: make(map[string]bool),
		SecretValidation: make(map[string]bool),
		Timestamp:        time.Now().UTC(),
	}

	// Validate each pipeline
	for _, pipeline := range pipelineConversion.Spec.Pipelines {
		if err := s.validatePipeline(pipeline, result); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ConversionError{
				Code:     "PIPELINE_VALIDATION_ERROR",
				Message:  err.Error(),
				Pipeline: pipeline.Name,
			})
		}
	}

	// Validate conversion settings
	if err := s.validateConversionSettings(pipelineConversion.Spec.Settings, result); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ConversionError{
			Code:    "SETTINGS_VALIDATION_ERROR",
			Message: err.Error(),
		})
	}

	return result, nil
}

// GenerateWorkflowsZip generates a ZIP file containing converted workflows
func (s *PipelineConversionService) GenerateWorkflowsZip(ctx context.Context, pipelineConversion *migrationv1.PipelineToWorkflow) ([]byte, error) {
	if err := s.rateLimiter.Wait(ctx, "generate-zip"); err != nil {
		return nil, err
	}

	// This would generate an actual ZIP file with workflow contents
	// For now, return placeholder data
	zipData := []byte("placeholder zip data")

	return zipData, nil
}

// AnalyzePipeline analyzes an Azure DevOps pipeline for conversion complexity
func (s *PipelineConversionService) AnalyzePipeline(ctx context.Context, organization, project, pipelineID, clientID, clientSecret, tenantID string) (*PipelineAnalysis, error) {
	if err := s.rateLimiter.Wait(ctx, "analyze-pipeline"); err != nil {
		return nil, err
	}

	// This would analyze the actual pipeline from Azure DevOps
	// For now, return mock analysis
	analysis := &PipelineAnalysis{
		PipelineID:      pipelineID,
		PipelineName:    "Sample Pipeline",
		Type:            "build",
		Complexity:      "Medium",
		EstimatedEffort: "2-4 hours",
		SupportedFeatures: []string{
			"Build tasks",
			"Test execution",
			"Artifact publishing",
			"Variable groups",
		},
		UnsupportedFeatures: []string{
			"Custom Azure DevOps extensions",
			"Azure Artifacts integration",
		},
		RequiredActions: []string{
			"Setup GitHub secrets for service connections",
			"Configure GitHub Environments for approvals",
			"Review and update custom scripts",
		},
		TaskBreakdown: map[string]int{
			"NuGetCommand":          2,
			"DotNetCoreCLI":         3,
			"PublishTestResults":    1,
			"PublishBuildArtifacts": 1,
		},
		Recommendations: []string{
			"Consider using GitHub's built-in package registry",
			"Use GitHub Environments for deployment approvals",
			"Implement proper secret management",
		},
	}

	return analysis, nil
}

// GetConversionTemplates returns available conversion templates
func (s *PipelineConversionService) GetConversionTemplates(templateType string) ([]ConversionTemplate, error) {
	templates := []ConversionTemplate{
		{
			Name:        "Node.js CI",
			Description: "Template for Node.js continuous integration",
			Type:        "build",
			Template: map[string]interface{}{
				"name": "Node.js CI",
				"on": map[string]interface{}{
					"push": map[string]interface{}{
						"branches": []string{"main", "develop"},
					},
					"pull_request": map[string]interface{}{
						"branches": []string{"main"},
					},
				},
				"jobs": map[string]interface{}{
					"build": map[string]interface{}{
						"runs-on": "ubuntu-latest",
						"steps": []map[string]interface{}{
							{
								"uses": "actions/checkout@v4",
							},
							{
								"name": "Setup Node.js",
								"uses": "actions/setup-node@v4",
								"with": map[string]interface{}{
									"node-version": "18",
								},
							},
							{
								"name": "Install dependencies",
								"run":  "npm ci",
							},
							{
								"name": "Run tests",
								"run":  "npm test",
							},
							{
								"name": "Build",
								"run":  "npm run build",
							},
						},
					},
				},
			},
			Variables: []string{"NODE_VERSION", "BUILD_CONFIGURATION"},
		},
		{
			Name:        ".NET Core CI",
			Description: "Template for .NET Core continuous integration",
			Type:        "build",
			Template: map[string]interface{}{
				"name": ".NET Core CI",
				"on": map[string]interface{}{
					"push": map[string]interface{}{
						"branches": []string{"main", "develop"},
					},
				},
				"jobs": map[string]interface{}{
					"build": map[string]interface{}{
						"runs-on": "ubuntu-latest",
						"steps": []map[string]interface{}{
							{
								"uses": "actions/checkout@v4",
							},
							{
								"name": "Setup .NET",
								"uses": "actions/setup-dotnet@v3",
								"with": map[string]interface{}{
									"dotnet-version": "8.0.x",
								},
							},
							{
								"name": "Restore dependencies",
								"run":  "dotnet restore",
							},
							{
								"name": "Build",
								"run":  "dotnet build --no-restore",
							},
							{
								"name": "Test",
								"run":  "dotnet test --no-build --verbosity normal",
							},
						},
					},
				},
			},
			Variables: []string{"DOTNET_VERSION", "BUILD_CONFIGURATION"},
		},
	}

	// Filter by type if specified
	if templateType != "" {
		filtered := []ConversionTemplate{}
		for _, template := range templates {
			if template.Type == templateType {
				filtered = append(filtered, template)
			}
		}
		return filtered, nil
	}

	return templates, nil
}

// GetTaskMappings returns available task mappings
func (s *PipelineConversionService) GetTaskMappings(taskType string) ([]TaskMapping, error) {
	mappings := []TaskMapping{
		{
			AzureTask:    "NuGetCommand@2",
			GitHubAction: "actions/setup-dotnet@v3",
			Version:      "v3",
			InputMappings: map[string]string{
				"command":        "dotnet-command",
				"packagesToPush": "packages",
				"nuGetFeedType":  "feed-type",
			},
			Notes: "NuGet commands are replaced with dotnet CLI commands",
		},
		{
			AzureTask:    "NodeTool@0",
			GitHubAction: "actions/setup-node@v4",
			Version:      "v4",
			InputMappings: map[string]string{
				"versionSpec": "node-version",
			},
			Notes: "Node.js tool installer maps directly to setup-node action",
		},
		{
			AzureTask:    "DotNetCoreCLI@2",
			GitHubAction: "actions/setup-dotnet@v3",
			Version:      "v3",
			InputMappings: map[string]string{
				"command":   "command",
				"projects":  "projects",
				"arguments": "arguments",
			},
			Notes: "Use dotnet CLI commands directly in run steps",
		},
		{
			AzureTask:    "PublishTestResults@2",
			GitHubAction: "dorny/test-reporter@v1",
			Version:      "v1",
			InputMappings: map[string]string{
				"testResultsFiles": "path",
				"testRunTitle":     "name",
			},
			Notes: "Test results publishing requires third-party action",
		},
	}

	// Filter by task type if specified
	if taskType != "" {
		filtered := []TaskMapping{}
		for _, mapping := range mappings {
			if mapping.AzureTask == taskType {
				filtered = append(filtered, mapping)
			}
		}
		return filtered, nil
	}

	return mappings, nil
}

// Helper functions

func (s *PipelineConversionService) convertPipelineToWorkflow(ctx context.Context, pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow) (*WorkflowPreview, error) {
	// This would implement the actual conversion logic
	// For now, return a mock workflow

	workflowContent := fmt.Sprintf(`name: %s

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    
    steps:
    - uses: actions/checkout@v4
    
    - name: Setup environment
      run: echo "Setting up build environment"
    
    - name: Build
      run: echo "Building application"
    
    - name: Test
      run: echo "Running tests"
`, pipeline.Name)

	workflow := &WorkflowPreview{
		Name:               pipeline.TargetWorkflowName,
		FilePath:           fmt.Sprintf(".github/workflows/%s", pipeline.TargetWorkflowName),
		Content:            workflowContent,
		SourcePipelineID:   pipeline.ID,
		SourcePipelineName: pipeline.Name,
		ValidationStatus:   "valid",
		ValidationErrors:   []string{},
	}

	return workflow, nil
}

func (s *PipelineConversionService) validatePipeline(pipeline migrationv1.PipelineResource, result *ConversionValidationResult) error {
	// Validate pipeline configuration
	if pipeline.ID == "" {
		return fmt.Errorf("pipeline ID is required")
	}
	if pipeline.Name == "" {
		return fmt.Errorf("pipeline name is required")
	}
	if pipeline.TargetWorkflowName == "" {
		return fmt.Errorf("target workflow name is required")
	}

	// Mark syntax as valid for this pipeline
	result.SyntaxValidation[pipeline.Name] = true

	return nil
}

func (s *PipelineConversionService) validateConversionSettings(settings migrationv1.ConversionSettings, result *ConversionValidationResult) error {
	// Validate conversion settings
	if settings.ParallelJobs < 1 || settings.ParallelJobs > 20 {
		return fmt.Errorf("parallel jobs must be between 1 and 20")
	}
	if settings.RetryAttempts < 0 || settings.RetryAttempts > 5 {
		return fmt.Errorf("retry attempts must be between 0 and 5")
	}

	return nil
}
