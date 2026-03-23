package services

import (
	"context"
	"fmt"
	"strings"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// WorkflowConverter handles conversion of ADO pipelines to GitHub workflows
type WorkflowConverter struct {
	rateLimiter *RateLimiter
}

// NewWorkflowConverter creates a new workflow converter
func NewWorkflowConverter() *WorkflowConverter {
	return &WorkflowConverter{}
}

// ConvertedWorkflow represents a converted GitHub workflow
type ConvertedWorkflow struct {
	Name               string
	Content            string
	FilePath           string
	SourcePipelineID   string
	SourcePipelineName string
	PipelineType       string
	ConversionNotes    []string
	RequiresManual     []string
}

// ConvertPipeline converts an ADO pipeline to a GitHub workflow
func (c *WorkflowConverter) ConvertPipeline(ctx context.Context, pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow) (*ConvertedWorkflow, error) {
	// Determine directory based on pipeline type
	directory := "pipelines"
	if pipeline.Type == "release" {
		directory = "releases"
	}

	// Generate workflow content
	content, notes, manualSteps := c.generateWorkflowContent(pipeline, conversion)

	workflow := &ConvertedWorkflow{
		Name:               pipeline.TargetWorkflowName,
		Content:            content,
		FilePath:           fmt.Sprintf("%s/%s", directory, pipeline.TargetWorkflowName),
		SourcePipelineID:   pipeline.ID,
		SourcePipelineName: pipeline.Name,
		PipelineType:       pipeline.Type,
		ConversionNotes:    notes,
		RequiresManual:     manualSteps,
	}

	return workflow, nil
}

// generateWorkflowContent generates the actual workflow YAML content
func (c *WorkflowConverter) generateWorkflowContent(pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow) (string, []string, []string) {
	// Get trigger configuration
	triggers := c.determineTriggers(pipeline, conversion)

	// Get runner configuration
	runnerLabels := c.determineRunners(pipeline, conversion)

	// Generate workflow based on pipeline type
	if pipeline.Type == "release" {
		return c.generateReleaseWorkflow(pipeline, conversion, triggers, runnerLabels)
	}

	return c.generateBuildWorkflow(pipeline, conversion, triggers, runnerLabels)
}

// generateBuildWorkflow generates a build pipeline workflow
func (c *WorkflowConverter) generateBuildWorkflow(pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow, triggers, runnerLabels string) (string, []string, []string) {
	notes := []string{
		"Converted from Azure DevOps build pipeline",
		"Review and update environment variables as needed",
		"Configure secrets in GitHub repository settings",
	}

	manualSteps := []string{
		"Configure required secrets: AZURE_CREDENTIALS, NPM_TOKEN, etc.",
		"Set up GitHub Environments for deployment stages",
		"Review and update runner labels if using self-hosted runners",
	}

	workflow := fmt.Sprintf(`# Auto-generated from Azure DevOps Pipeline: %s
# Source Pipeline ID: %s
# Migration Date: Auto-generated
#
# IMPORTANT: This is an initial conversion. Please review and test thoroughly.
# Manual steps required:
# - Configure repository secrets
# - Update environment-specific variables
# - Test workflow execution
# - Review and adjust job dependencies

name: %s

# Trigger configuration
%s

env:
  # Global environment variables
  # TODO: Review and update these values
  BUILD_CONFIGURATION: Release
  AZURE_WEBAPP_NAME: my-app-name
  DOTNET_VERSION: '8.0.x'
  NODE_VERSION: '20.x'

jobs:
  build:
    name: Build and Test
    runs-on: %s

    # Optional: Set timeout to prevent hanging jobs
    timeout-minutes: 60

    steps:
      - name: Checkout code
        uses: actions/checkout@v4
        with:
          fetch-depth: 0  # Full history for better analysis

      # Setup environments (adjust based on your stack)
      - name: Setup .NET
        uses: actions/setup-dotnet@v4
        with:
          dotnet-version: ${{ env.DOTNET_VERSION }}

      - name: Setup Node.js
        uses: actions/setup-node@v4
        with:
          node-version: ${{ env.NODE_VERSION }}
          cache: 'npm'

      # Restore dependencies
      - name: Restore NuGet packages
        run: dotnet restore

      - name: Install npm dependencies
        run: npm ci

      # Build
      - name: Build solution
        run: dotnet build --configuration ${{ env.BUILD_CONFIGURATION }} --no-restore

      # Run tests
      - name: Run unit tests
        run: dotnet test --configuration ${{ env.BUILD_CONFIGURATION }} --no-build --verbosity normal --collect:"XPlat Code Coverage"

      # Upload test results
      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: test-results
          path: '**/TestResults/**/*'

      # Publish artifacts
      - name: Publish build artifacts
        run: dotnet publish --configuration ${{ env.BUILD_CONFIGURATION }} --no-build --output ${{ github.workspace }}/publish

      - name: Upload build artifacts
        uses: actions/upload-artifact@v4
        with:
          name: app-package
          path: ${{ github.workspace }}/publish
          retention-days: 30

  # Code quality checks (optional but recommended)
  code-quality:
    name: Code Quality Analysis
    runs-on: %s
    needs: build

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      # Add code quality tools here
      - name: Run linting
        run: |
          echo "TODO: Add linting commands (e.g., eslint, dotnet format)"

      - name: Security scanning
        run: |
          echo "TODO: Add security scanning (e.g., CodeQL, Snyk)"

  # Deployment job (if applicable)
  deploy-dev:
    name: Deploy to Development
    runs-on: %s
    needs: [build, code-quality]
    if: github.ref == 'refs/heads/develop'
    environment:
      name: development
      url: https://dev.example.com

    steps:
      - name: Download artifacts
        uses: actions/download-artifact@v4
        with:
          name: app-package

      - name: Deploy to Azure Web App
        uses: azure/webapps-deploy@v2
        with:
          app-name: ${{ env.AZURE_WEBAPP_NAME }}-dev
          publish-profile: ${{ secrets.AZURE_WEBAPP_PUBLISH_PROFILE_DEV }}
          package: .

      # Add health check
      - name: Health check
        run: |
          echo "TODO: Add health check for deployed application"

# Notes:
# 1. Replace placeholder values with actual values from your ADO pipeline
# 2. Configure GitHub secrets for sensitive data
# 3. Adjust job dependencies based on your pipeline structure
# 4. Add/remove jobs as needed for your workflow
# 5. Consider using reusable workflows for common tasks
# 6. Enable branch protection rules for production deployments
`, pipeline.Name, pipeline.ID, pipeline.Name, triggers, runnerLabels, runnerLabels, runnerLabels)

	return workflow, notes, manualSteps
}

// generateReleaseWorkflow generates a release pipeline workflow
func (c *WorkflowConverter) generateReleaseWorkflow(pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow, triggers, runnerLabels string) (string, []string, []string) {
	notes := []string{
		"Converted from Azure DevOps release pipeline",
		"Review deployment stages and approval requirements",
		"Configure GitHub Environments with protection rules",
	}

	manualSteps := []string{
		"Set up GitHub Environments: staging, production",
		"Configure environment protection rules and required reviewers",
		"Add deployment secrets to each environment",
		"Test deployment workflow in non-production environment first",
	}

	workflow := fmt.Sprintf(`# Auto-generated from Azure DevOps Release Pipeline: %s
# Source Pipeline ID: %s
# Migration Date: Auto-generated
#
# IMPORTANT: This is a multi-stage deployment workflow.
# Configure GitHub Environments with appropriate protection rules.

name: %s

# Trigger configuration
%s

env:
  # Global environment variables
  ARTIFACT_NAME: app-package
  AZURE_WEBAPP_NAME: my-app

jobs:
  # Download or build artifacts
  prepare-release:
    name: Prepare Release Artifacts
    runs-on: %s

    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      # Option 1: Download from a build workflow
      - name: Download build artifacts
        uses: dawidd6/action-download-artifact@v3
        with:
          workflow: build.yml
          name: app-package
          path: ./artifacts

      # Option 2: Build here if not using separate build workflow
      # - name: Build application
      #   run: |
      #     # Add build commands here

      - name: Upload artifacts for deployment
        uses: actions/upload-artifact@v4
        with:
          name: ${{ env.ARTIFACT_NAME }}
          path: ./artifacts

  # Deploy to Staging
  deploy-staging:
    name: Deploy to Staging
    runs-on: %s
    needs: prepare-release
    environment:
      name: staging
      url: https://staging.example.com

    steps:
      - name: Download artifacts
        uses: actions/download-artifact@v4
        with:
          name: ${{ env.ARTIFACT_NAME }}

      - name: Azure Login
        uses: azure/login@v1
        with:
          creds: ${{ secrets.AZURE_CREDENTIALS_STAGING }}

      - name: Deploy to Azure Web App (Staging)
        uses: azure/webapps-deploy@v2
        with:
          app-name: ${{ env.AZURE_WEBAPP_NAME }}-staging
          package: .

      - name: Run smoke tests
        run: |
          # TODO: Add smoke test commands
          echo "Running smoke tests against staging environment"

      - name: Notify deployment success
        if: success()
        run: |
          echo "Staging deployment successful"
          # TODO: Add notification (Slack, Teams, Email)

  # Deploy to Production
  deploy-production:
    name: Deploy to Production
    runs-on: %s
    needs: deploy-staging
    environment:
      name: production
      url: https://production.example.com

    steps:
      - name: Download artifacts
        uses: actions/download-artifact@v4
        with:
          name: ${{ env.ARTIFACT_NAME }}

      - name: Azure Login
        uses: azure/login@v1
        with:
          creds: ${{ secrets.AZURE_CREDENTIALS_PRODUCTION }}

      # Blue-Green deployment strategy (recommended)
      - name: Deploy to Azure Web App (Production - Slot)
        uses: azure/webapps-deploy@v2
        with:
          app-name: ${{ env.AZURE_WEBAPP_NAME }}
          slot-name: staging  # Deploy to staging slot first
          package: .

      - name: Warm up staging slot
        run: |
          # TODO: Add warmup commands
          echo "Warming up staging slot"

      - name: Run production smoke tests
        run: |
          # TODO: Add smoke test commands against staging slot
          echo "Running smoke tests"

      - name: Swap slots
        uses: azure/cli@v1
        with:
          inlineScript: |
            az webapp deployment slot swap \
              --resource-group my-resource-group \
              --name ${{ env.AZURE_WEBAPP_NAME }} \
              --slot staging \
              --target-slot production

      - name: Verify production deployment
        run: |
          # TODO: Add verification commands
          echo "Verifying production deployment"

      - name: Notify deployment success
        if: success()
        run: |
          echo "Production deployment successful"
          # TODO: Add notification (Slack, Teams, Email)

      - name: Rollback on failure
        if: failure()
        uses: azure/cli@v1
        with:
          inlineScript: |
            echo "Deployment failed, initiating rollback"
            az webapp deployment slot swap \
              --resource-group my-resource-group \
              --name ${{ env.AZURE_WEBAPP_NAME }} \
              --slot production \
              --target-slot staging

# Best Practices Implemented:
# 1. Multi-stage deployment with approvals (via GitHub Environments)
# 2. Blue-green deployment strategy for zero-downtime
# 3. Smoke tests after each deployment
# 4. Automatic rollback on failure
# 5. Artifact caching between jobs
# 6. Environment-specific secrets
#
# Manual Configuration Required:
# 1. Create GitHub Environments: staging, production
# 2. Set up environment protection rules
# 3. Add required reviewers for production
# 4. Configure deployment secrets per environment
# 5. Update Azure resource names and URLs
# 6. Implement smoke test and warmup logic
# 7. Configure notification channels
`, pipeline.Name, pipeline.ID, pipeline.Name, triggers, runnerLabels, runnerLabels, runnerLabels)

	return workflow, notes, manualSteps
}

// determineTriggers determines workflow triggers based on pipeline settings
func (c *WorkflowConverter) determineTriggers(pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow) string {
	// Check for custom triggers in pipeline settings
	if pipeline.Settings != nil && len(pipeline.Settings.OverrideTriggers) > 0 {
		return c.formatTriggers(pipeline.Settings.OverrideTriggers)
	}

	// Check for default triggers
	if conversion.Spec.Target.DefaultWorkflowSettings != nil && len(conversion.Spec.Target.DefaultWorkflowSettings.Triggers) > 0 {
		return c.formatTriggers(conversion.Spec.Target.DefaultWorkflowSettings.Triggers)
	}

	// Default triggers based on pipeline type
	if pipeline.Type == "release" {
		return `on:
  workflow_dispatch:  # Manual trigger
  release:
    types: [published]
  workflow_run:
    workflows: ["Build"]
    types: [completed]
    branches: [main]`
	}

	return `on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main]
  workflow_dispatch:  # Manual trigger`
}

// formatTriggers formats triggers into YAML
func (c *WorkflowConverter) formatTriggers(triggers []migrationv1.WorkflowTrigger) string {
	var triggerLines []string

	for _, trigger := range triggers {
		switch trigger.Type {
		case "push":
			if trigger.Config != nil && len(trigger.Config.Branches) > 0 {
				branches := strings.Join(trigger.Config.Branches, ", ")
				triggerLines = append(triggerLines, fmt.Sprintf("  push:\n    branches: [%s]", branches))
			} else {
				triggerLines = append(triggerLines, "  push:")
			}
		case "pull_request":
			if trigger.Config != nil && len(trigger.Config.Branches) > 0 {
				branches := strings.Join(trigger.Config.Branches, ", ")
				triggerLines = append(triggerLines, fmt.Sprintf("  pull_request:\n    branches: [%s]", branches))
			} else {
				triggerLines = append(triggerLines, "  pull_request:")
			}
		case "schedule":
			if trigger.Config != nil && trigger.Config.Cron != "" {
				triggerLines = append(triggerLines, fmt.Sprintf("  schedule:\n    - cron: '%s'", trigger.Config.Cron))
			}
		case "workflow_dispatch":
			triggerLines = append(triggerLines, "  workflow_dispatch:")
		case "release":
			triggerLines = append(triggerLines, "  release:\n    types: [published]")
		}
	}

	if len(triggerLines) == 0 {
		return "on: [push, pull_request]"
	}

	return "on:\n" + strings.Join(triggerLines, "\n")
}

// determineRunners determines the runner labels to use
func (c *WorkflowConverter) determineRunners(pipeline migrationv1.PipelineResource, conversion *migrationv1.PipelineToWorkflow) string {
	// Check for pipeline-specific runner override
	if pipeline.Settings != nil && len(pipeline.Settings.OverrideRunnerLabels) > 0 {
		return c.formatRunnerLabels(pipeline.Settings.OverrideRunnerLabels)
	}

	// Check for default runner labels
	if conversion.Spec.Target.DefaultWorkflowSettings != nil && len(conversion.Spec.Target.DefaultWorkflowSettings.RunnerLabels) > 0 {
		return c.formatRunnerLabels(conversion.Spec.Target.DefaultWorkflowSettings.RunnerLabels)
	}

	// Default to ubuntu-latest
	return "ubuntu-latest"
}

// formatRunnerLabels formats runner labels
func (c *WorkflowConverter) formatRunnerLabels(labels []string) string {
	if len(labels) == 1 {
		return labels[0]
	}
	return "[" + strings.Join(labels, ", ") + "]"
}
