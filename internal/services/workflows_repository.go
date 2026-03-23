package services

import (
	"context"
	"fmt"
	"strings"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	"github.com/google/go-github/v84/github"
)

// WorkflowsRepositoryManager manages the dedicated workflows repository
type WorkflowsRepositoryManager struct {
	githubService *GitHubService
}

// NewWorkflowsRepositoryManager creates a new workflows repository manager
func NewWorkflowsRepositoryManager(githubService *GitHubService) *WorkflowsRepositoryManager {
	return &WorkflowsRepositoryManager{
		githubService: githubService,
	}
}

// WorkflowsRepositoryInfo contains information about the workflows repository
type WorkflowsRepositoryInfo struct {
	Name          string
	Owner         string
	FullName      string
	URL           string
	DefaultBranch string
	Created       bool
}

// SetupWorkflowsRepository sets up the dedicated workflows repository
func (m *WorkflowsRepositoryManager) SetupWorkflowsRepository(ctx context.Context, token string, conversion *migrationv1.PipelineToWorkflow) (*WorkflowsRepositoryInfo, error) {
	config := conversion.Spec.Target.WorkflowsRepository
	if config == nil || !config.Create {
		return nil, fmt.Errorf("workflows repository creation is not enabled")
	}

	owner := conversion.Spec.Target.Owner
	repoName := config.Name
	if repoName == "" {
		repoName = "ado-to-git-migration-workflows"
	}

	// Check if repository already exists
	exists, err := m.githubService.CheckRepositoryExists(ctx, token, owner, repoName)
	if err != nil {
		return nil, fmt.Errorf("failed to check repository existence: %w", err)
	}

	var repo *github.Repository

	if exists {
		// Get existing repository
		repo, err = m.githubService.GetRepository(ctx, token, owner, repoName)
		if err != nil {
			return nil, fmt.Errorf("failed to get existing repository: %w", err)
		}
	} else {
		// Create new repository
		settings := &GitHubRepoSettings{
			Visibility:        "private",
			AutoInit:          config.InitializeWithReadme,
			LicenseTemplate:   "",
			GitignoreTemplate: "",
		}

		if !config.Private {
			settings.Visibility = "public"
		}

		repo, err = m.githubService.CreateRepository(ctx, token, owner, repoName, settings)
		if err != nil {
			return nil, fmt.Errorf("failed to create workflows repository: %w", err)
		}
	}

	// Create directory structure
	if err := m.createDirectoryStructure(ctx, token, owner, repoName, repo.GetDefaultBranch()); err != nil {
		return nil, fmt.Errorf("failed to create directory structure: %w", err)
	}

	return &WorkflowsRepositoryInfo{
		Name:          repoName,
		Owner:         owner,
		FullName:      fmt.Sprintf("%s/%s", owner, repoName),
		URL:           repo.GetHTMLURL(),
		DefaultBranch: repo.GetDefaultBranch(),
		Created:       !exists,
	}, nil
}

// createDirectoryStructure creates the pipelines/ and releases/ directory structure
func (m *WorkflowsRepositoryManager) createDirectoryStructure(ctx context.Context, token, owner, repo, branch string) error {
	client := m.githubService.getClient(token)

	// Create README in pipelines directory
	pipelinesReadme := `# Pipelines

This directory contains GitHub workflows converted from Azure DevOps build pipelines.

## Structure

Each workflow file represents a converted Azure DevOps build pipeline. The file names are sanitized versions of the original pipeline names to ensure compatibility with Git.

## Usage

1. Review each workflow file thoroughly
2. Update placeholder values and secrets
3. Test workflows in a development environment
4. Copy workflows to your target repository's .github/workflows directory

## Notes

- All workflows require configuration of GitHub secrets
- Environment variables need to be reviewed and updated
- Some Azure DevOps-specific features may require manual adaptation
`

	_, _, err := client.Repositories.CreateFile(ctx, owner, repo, "pipelines/README.md", &github.RepositoryContentFileOptions{
		Message: github.String("Initialize pipelines directory"),
		Content: []byte(pipelinesReadme),
		Branch:  github.String(branch),
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create pipelines/README.md: %w", err)
	}

	// Create README in releases directory
	releasesReadme := `# Releases

This directory contains GitHub workflows converted from Azure DevOps release pipelines.

## Structure

Each workflow file represents a converted Azure DevOps release pipeline with multi-stage deployment capabilities.

## Usage

1. Review deployment stages and approval requirements
2. Configure GitHub Environments with protection rules
3. Set up environment-specific secrets
4. Test deployment workflows in non-production environments first

## Best Practices

- Use GitHub Environments for deployment stages (dev, staging, production)
- Configure required reviewers for production deployments
- Implement proper testing and smoke tests between stages
- Consider blue-green deployment strategies for zero-downtime
- Set up automatic rollback on deployment failures

## Notes

- Release workflows require GitHub Enterprise for advanced environment features
- Some Azure-specific deployment targets may need alternative actions
- Review and update all Azure resource names and configurations
`

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, "releases/README.md", &github.RepositoryContentFileOptions{
		Message: github.String("Initialize releases directory"),
		Content: []byte(releasesReadme),
		Branch:  github.String(branch),
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create releases/README.md: %w", err)
	}

	// Create main README
	mainReadme := `# ADO to GitHub Workflows Migration

This repository contains GitHub Actions workflows converted from Azure DevOps pipelines.

## Directory Structure

- **pipelines/**: Build pipelines converted from Azure DevOps
- **releases/**: Release pipelines converted from Azure DevOps

## Getting Started

### Prerequisites

1. GitHub repository with Actions enabled
2. Required secrets configured in GitHub repository settings
3. GitHub Environments set up for deployment workflows

### Migration Steps

1. **Review Workflows**: Examine each workflow file for completeness and accuracy
2. **Update Configuration**: Replace placeholder values with actual configuration
3. **Configure Secrets**: Set up required secrets in GitHub repository settings
4. **Set Up Environments**: Create GitHub Environments for deployment stages
5. **Test**: Run workflows in a test environment before production use
6. **Deploy**: Copy workflows to your target repository

## Common Configuration Tasks

### Secrets Configuration

All workflows require secrets to be configured in GitHub:

- AZURE_CREDENTIALS: Azure service principal credentials (JSON format)
- NPM_TOKEN: NPM registry authentication token
- NUGET_API_KEY: NuGet package repository API key
- Additional secrets as noted in individual workflow files

### Environment Variables

Review and update environment variables in each workflow:

- Azure resource names
- Application names
- Version numbers
- Build configurations

### GitHub Environments

For deployment workflows, create the following environments:

1. **development**: For dev deployments (automatic)
2. **staging**: For staging deployments (automatic with checks)
3. **production**: For production deployments (manual approval required)

Configure environment protection rules:

- Required reviewers for production
- Deployment branches (limit to main/master)
- Environment secrets specific to each environment

## Best Practices

1. **Start Small**: Migrate and test one workflow at a time
2. **Use Reusable Workflows**: Extract common steps into reusable workflows
3. **Implement Gates**: Use GitHub Environments for approval gates
4. **Monitor**: Set up workflow notifications and monitoring
5. **Document**: Keep this README updated with project-specific instructions

## Troubleshooting

### Common Issues

1. **Authentication Failures**: Verify secrets are correctly configured
2. **Runner Compatibility**: Ensure runner labels match available runners
3. **Path Issues**: Check file paths are correct for your repository structure
4. **Timeout**: Increase timeout-minutes if jobs are timing out

### Getting Help

- Review GitHub Actions documentation
- Check workflow run logs for detailed error messages
- Consult migration notes in individual workflow files

## Migration Metadata

- **Migration Tool**: ADO to Git Migration Operator
- **Migration Date**: Auto-generated
- **Source**: Azure DevOps
- **Target**: GitHub Actions

---

**Note**: This is an automated migration. Manual review and testing are required before production use.
`

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, "README.md", &github.RepositoryContentFileOptions{
		Message: github.String("Initialize workflows repository"),
		Content: []byte(mainReadme),
		Branch:  github.String(branch),
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		// README might already exist from AutoInit, that's okay
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create README.md: %w", err)
		}
	}

	return nil
}

// PushWorkflow pushes a workflow to the workflows repository
func (m *WorkflowsRepositoryManager) PushWorkflow(ctx context.Context, token string, repoInfo *WorkflowsRepositoryInfo, workflow *ConvertedWorkflow) error {
	client := m.githubService.getClient(token)

	// Check if file already exists
	_, _, resp, err := client.Repositories.GetContents(ctx, repoInfo.Owner, repoInfo.Name, workflow.FilePath, &github.RepositoryContentGetOptions{
		Ref: repoInfo.DefaultBranch,
	})

	if err != nil && resp != nil && resp.StatusCode != 404 {
		return fmt.Errorf("failed to check if workflow exists: %w", err)
	}

	fileExists := (err == nil)

	commitMessage := fmt.Sprintf("Add workflow: %s (from ADO pipeline: %s)", workflow.Name, workflow.SourcePipelineName)
	if fileExists {
		commitMessage = fmt.Sprintf("Update workflow: %s (from ADO pipeline: %s)", workflow.Name, workflow.SourcePipelineName)
	}

	if fileExists {
		// Update existing file
		existingFile, _, _, err := client.Repositories.GetContents(ctx, repoInfo.Owner, repoInfo.Name, workflow.FilePath, &github.RepositoryContentGetOptions{
			Ref: repoInfo.DefaultBranch,
		})
		if err != nil {
			return fmt.Errorf("failed to get existing file: %w", err)
		}

		_, _, err = client.Repositories.UpdateFile(ctx, repoInfo.Owner, repoInfo.Name, workflow.FilePath, &github.RepositoryContentFileOptions{
			Message: github.String(commitMessage),
			Content: []byte(workflow.Content),
			SHA:     existingFile.SHA,
			Branch:  github.String(repoInfo.DefaultBranch),
		})
		if err != nil {
			return fmt.Errorf("failed to update workflow file: %w", err)
		}
	} else {
		// Create new file
		_, _, err := client.Repositories.CreateFile(ctx, repoInfo.Owner, repoInfo.Name, workflow.FilePath, &github.RepositoryContentFileOptions{
			Message: github.String(commitMessage),
			Content: []byte(workflow.Content),
			Branch:  github.String(repoInfo.DefaultBranch),
		})
		if err != nil {
			return fmt.Errorf("failed to create workflow file: %w", err)
		}
	}

	return nil
}

// PushMigrationSummary creates a summary file documenting the migration
func (m *WorkflowsRepositoryManager) PushMigrationSummary(ctx context.Context, token string, repoInfo *WorkflowsRepositoryInfo, workflows []*ConvertedWorkflow) error {
	client := m.githubService.getClient(token)

	// Generate summary content
	summary := m.generateMigrationSummary(workflows)

	// Push summary file
	_, _, err := client.Repositories.CreateFile(ctx, repoInfo.Owner, repoInfo.Name, "MIGRATION_SUMMARY.md", &github.RepositoryContentFileOptions{
		Message: github.String("Add migration summary"),
		Content: []byte(summary),
		Branch:  github.String(repoInfo.DefaultBranch),
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("failed to create migration summary: %w", err)
	}

	return nil
}

// generateMigrationSummary generates a summary of the migration
func (m *WorkflowsRepositoryManager) generateMigrationSummary(workflows []*ConvertedWorkflow) string {
	buildCount := 0
	releaseCount := 0

	var buildWorkflows []string
	var releaseWorkflows []string

	for _, workflow := range workflows {
		if workflow.PipelineType == "build" {
			buildCount++
			buildWorkflows = append(buildWorkflows, fmt.Sprintf("- [%s](%s) - Source: %s (ID: %s)",
				workflow.Name, workflow.FilePath, workflow.SourcePipelineName, workflow.SourcePipelineID))
		} else {
			releaseCount++
			releaseWorkflows = append(releaseWorkflows, fmt.Sprintf("- [%s](%s) - Source: %s (ID: %s)",
				workflow.Name, workflow.FilePath, workflow.SourcePipelineName, workflow.SourcePipelineID))
		}
	}

	summary := fmt.Sprintf(`# Migration Summary

## Overview

- **Total Workflows**: %d
- **Build Pipelines**: %d
- **Release Pipelines**: %d
- **Migration Date**: Auto-generated

## Build Pipelines

%s

## Release Pipelines

%s

## Next Steps

1. Review each workflow file for accuracy
2. Update placeholder values and configurations
3. Configure GitHub secrets
4. Set up GitHub Environments for deployments
5. Test workflows in development environment
6. Gradually roll out to production

## Important Notes

- All workflows are initial conversions and require review
- Secrets must be configured before workflows can run
- Some Azure DevOps features may require manual adaptation
- Test thoroughly before using in production

## Support

For issues or questions about the migration, please refer to the main README.md file.
`, len(workflows), buildCount, releaseCount,
		strings.Join(buildWorkflows, "\n"),
		strings.Join(releaseWorkflows, "\n"))

	return summary
}
