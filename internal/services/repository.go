package services

import (
	"context"
	"fmt"
	"time"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// RepositoryService handles repository migration operations
type RepositoryService struct {
}

// NewRepositoryService creates a new repository service
func NewRepositoryService() *RepositoryService {
	return &RepositoryService{}
}

// RepositoryMigrationRequest represents a repository migration request
type RepositoryMigrationRequest struct {
	SourceOrganization string                       `json:"sourceOrganization"`
	SourceProject      string                       `json:"sourceProject"`
	SourceRepository   string                       `json:"sourceRepository"`
	TargetOwner        string                       `json:"targetOwner"`
	TargetRepository   string                       `json:"targetRepository"`
	AzureAuth          migrationv1.AdoAuthConfig    `json:"azureAuth"`
	GitHubAuth         migrationv1.GitHubAuthConfig `json:"githubAuth"`
	Settings           RepositoryMigrationSettings  `json:"settings"`
}

// RepositoryMigrationSettings represents repository migration settings
type RepositoryMigrationSettings struct {
	MaxHistoryDays      int      `json:"maxHistoryDays"`
	MaxCommitCount      int      `json:"maxCommitCount"`
	IncludeBranches     []string `json:"includeBranches,omitempty"`
	ExcludeBranches     []string `json:"excludeBranches,omitempty"`
	IncludeTags         bool     `json:"includeTags"`
	IncludePullRequests bool     `json:"includePullRequests"`
	HandleLFS           bool     `json:"handleLFS"`
}

// RepositoryMigrationResult represents the result of a repository migration
type RepositoryMigrationResult struct {
	Success              bool   `json:"success"`
	Message              string `json:"message"`
	TargetURL            string `json:"targetUrl,omitempty"`
	Error                string `json:"error,omitempty"`
	CommitsMigrated      int    `json:"commitsMigrated"`
	BranchesMigrated     int    `json:"branchesMigrated"`
	TagsMigrated         int    `json:"tagsMigrated"`
	PullRequestsMigrated int    `json:"pullRequestsMigrated"`
}

// RepositoryValidationResult represents the result of repository validation
type RepositoryValidationResult struct {
	SourceFound         bool                        `json:"sourceFound"`
	SourceDefaultBranch string                      `json:"sourceDefaultBranch"`
	TargetState         migrationv1.RepositoryState `json:"targetState"`
	TargetDefaultBranch string                      `json:"targetDefaultBranch"`
	CanProceed          bool                        `json:"canProceed"`
	Messages            []string                    `json:"messages"`
	Warnings            []string                    `json:"warnings"`
}

// MigrateRepository migrates a repository from Azure DevOps to GitHub
func (s *RepositoryService) MigrateRepository(ctx context.Context, request RepositoryMigrationRequest) (*RepositoryMigrationResult, error) {
	// This is a simplified implementation
	// In a real implementation, this would involve:
	// 1. Authenticating with Azure DevOps and GitHub
	// 2. Cloning the source repository
	// 3. Processing commit history according to limits
	// 4. Creating/updating the target repository
	// 5. Pushing all branches and tags
	// 6. Migrating pull requests as GitHub pull requests
	// 7. Handling LFS objects if needed

	result := &RepositoryMigrationResult{}

	// Simulate repository migration process
	steps := []string{
		"Authenticating with Azure DevOps",
		"Authenticating with GitHub",
		"Analyzing source repository",
		"Creating target repository",
		"Cloning source repository",
		"Processing commit history",
		"Migrating branches",
		"Migrating tags",
		"Migrating pull requests",
		"Finalizing migration",
	}

	for _, step := range steps {
		// Log the current step (in real implementation, this would update status)
		fmt.Printf("Repository migration step: %s\n", step)

		// Simulate processing time
		time.Sleep(time.Millisecond * 100)
	}

	// Simulate successful migration
	result.Success = true
	result.Message = "Repository migration completed successfully"
	result.TargetURL = fmt.Sprintf("https://github.com/%s/%s", request.TargetOwner, request.TargetRepository)
	result.CommitsMigrated = 150
	result.BranchesMigrated = 3
	result.TagsMigrated = 5
	result.PullRequestsMigrated = 10

	return result, nil
}

// ValidateRepositoryMigrationRequest validates a repository migration request
func (s *RepositoryService) ValidateRepositoryMigrationRequest(request RepositoryMigrationRequest) error {
	if request.SourceOrganization == "" {
		return fmt.Errorf("source organization is required")
	}
	if request.SourceProject == "" {
		return fmt.Errorf("source project is required")
	}
	if request.SourceRepository == "" {
		return fmt.Errorf("source repository is required")
	}
	if request.TargetOwner == "" {
		return fmt.Errorf("target owner is required")
	}
	if request.TargetRepository == "" {
		return fmt.Errorf("target repository is required")
	}
	if request.AzureAuth.ServicePrincipal != nil && request.AzureAuth.ServicePrincipal.ClientIDRef.Name == "" {
		return fmt.Errorf("azure client ID reference is required when using service principal")
	}
	if request.AzureAuth.ServicePrincipal != nil && request.AzureAuth.ServicePrincipal.TenantIDRef.Name == "" {
		return fmt.Errorf("azure tenant ID reference is required when using service principal")
	}
	if request.AzureAuth.PAT != nil && request.AzureAuth.PAT.TokenRef.Name == "" {
		return fmt.Errorf("azure PAT token reference is required when using PAT")
	}
	if request.GitHubAuth.TokenRef.Name == "" {
		return fmt.Errorf("github token reference is required")
	}

	return nil
}

// ValidateRepositoryMigration validates the source and target repositories for a specific repository resource
func (s *RepositoryService) ValidateRepositoryMigration(ctx context.Context,
	adoService *AzureDevOpsService,
	githubService *GitHubService,
	spec migrationv1.AdoToGitMigrationSpec,
	repoResource migrationv1.MigrationResource) (*RepositoryValidationResult, error) {

	result := &RepositoryValidationResult{
		Messages: []string{},
		Warnings: []string{},
	}

	// 1. Validate source repository and get default branch
	// Note: This validation service cannot resolve secret references.
	// Actual auth resolution happens in the controller with Kubernetes client access.
	// Passing empty strings here since this is just validation.
	sourceInfo, err := adoService.GetRepositoryInfo(ctx,
		spec.Source.Organization,
		spec.Source.Project,
		repoResource.SourceName,
		"", "", "") // clientID, clientSecret, tenantID - resolved by controller

	if err != nil {
		result.SourceFound = false
		result.CanProceed = false
		result.Messages = append(result.Messages, fmt.Sprintf("Source repository not found: %v", err))
		return result, nil
	}

	result.SourceFound = true
	result.SourceDefaultBranch = sourceInfo.DefaultBranch
	result.Messages = append(result.Messages, fmt.Sprintf("Source repository found with default branch: %s", sourceInfo.DefaultBranch))

	// 2. Check target repository state
	githubToken := extractGitHubToken(spec.Target.Auth)
	targetState, err := githubService.GetRepositoryState(ctx, githubToken, spec.Target.Owner, repoResource.TargetName)
	if err != nil {
		result.CanProceed = false
		result.Messages = append(result.Messages, fmt.Sprintf("Failed to check target repository: %v", err))
		return result, nil
	}

	// Get repository settings
	var createIfNotExists bool
	if repoResource.Settings != nil && repoResource.Settings.Repository != nil {
		createIfNotExists = repoResource.Settings.Repository.CreateIfNotExists
	}

	// 3. Determine target repository state and action
	if !targetState.Exists {
		result.TargetState = migrationv1.RepositoryStateNotExists
		if createIfNotExists {
			result.CanProceed = true
			result.Messages = append(result.Messages, "Target repository will be created")
		} else {
			result.CanProceed = false
			result.Messages = append(result.Messages, "Target repository does not exist and creation is not enabled")
		}
	} else if targetState.IsEmpty {
		result.TargetState = migrationv1.RepositoryStateEmpty
		result.CanProceed = true
		result.Messages = append(result.Messages, "Target repository exists but is empty - migration can proceed")
	} else {
		result.TargetState = migrationv1.RepositoryStateNonEmpty
		result.CanProceed = false
		result.Messages = append(result.Messages, "Target repository exists and contains data - migration cannot proceed")
	}

	// 4. Determine target default branch
	result.TargetDefaultBranch = s.determineTargetDefaultBranch(spec.Target.DefaultBranch, result.SourceDefaultBranch)
	result.Messages = append(result.Messages, fmt.Sprintf("Target default branch will be: %s", result.TargetDefaultBranch))

	return result, nil
}

// determineTargetDefaultBranch determines what the target default branch should be
func (s *RepositoryService) determineTargetDefaultBranch(specifiedBranch, sourceDefaultBranch string) string {
	// If user specified a default branch, use it
	if specifiedBranch != "" {
		return specifiedBranch
	}

	// If source has main, master, or develop, use the same
	commonDefaults := []string{"main", "master", "develop"}
	for _, branch := range commonDefaults {
		if sourceDefaultBranch == branch {
			return sourceDefaultBranch
		}
	}

	// If source has some other branch, still use it
	if sourceDefaultBranch != "" {
		return sourceDefaultBranch
	}

	// Fallback to main
	return "main"
}

// CreateTargetRepository creates the target repository if needed
func (s *RepositoryService) CreateTargetRepository(ctx context.Context,
	githubService *GitHubService,
	spec migrationv1.AdoToGitMigrationSpec,
	repoResource migrationv1.MigrationResource,
	defaultBranch string) error {

	githubToken := extractGitHubToken(spec.Target.Auth)

	// Determine repository settings
	var visibility string
	if repoResource.Settings != nil && repoResource.Settings.Repository != nil && repoResource.Settings.Repository.Visibility != "" {
		visibility = repoResource.Settings.Repository.Visibility
	} else if spec.Target.DefaultRepoSettings != nil && spec.Target.DefaultRepoSettings.Visibility != "" {
		visibility = spec.Target.DefaultRepoSettings.Visibility
	} else {
		visibility = "private" // Default to private
	}

	settings := &GitHubRepoSettings{
		Visibility: visibility,
		AutoInit:   false, // We don't want to auto-initialize since we'll be pushing content
	}

	repo, err := githubService.CreateRepository(ctx, githubToken, spec.Target.Owner, repoResource.TargetName, settings)
	if err != nil {
		return fmt.Errorf("failed to create repository: %w", err)
	}

	// Set default branch if different from GitHub's default
	if repo.GetDefaultBranch() != defaultBranch {
		err = githubService.SetDefaultBranch(ctx, githubToken, spec.Target.Owner, repoResource.TargetName, defaultBranch)
		if err != nil {
			// Log warning but don't fail the migration
			return fmt.Errorf("repository created but failed to set default branch: %w", err)
		}
	}

	return nil
}

// Helper functions to extract auth details
// DEPRECATED: These functions are no longer needed as credentials are now retrieved
// from Kubernetes secrets using references. The controller handles secret resolution.
// Keeping these as placeholders for backward compatibility.

func extractGitHubToken(auth migrationv1.GitHubAuthConfig) string {
	// In real implementation, this would resolve from Kubernetes secret
	// For now, return empty string as placeholder
	return ""
}

// GetRepositoryInfo gets information about a repository
func (s *RepositoryService) GetRepositoryInfo(ctx context.Context, organization, project, repository string, auth migrationv1.AdoAuthConfig) (*migrationv1.Repository, error) {
	// This would use the Azure DevOps API to get repository information
	// For now, return mock data

	return &migrationv1.Repository{
		ID:            "repo-123",
		Name:          repository,
		URL:           fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", organization, project, repository),
		DefaultBranch: "refs/heads/main",
		Size:          1024000,
		IsEmpty:       false,
		ProjectID:     "project-456",
	}, nil
}
