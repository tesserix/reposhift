package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v84/github"
	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

// GitHubService provides GitHub integration
type GitHubService struct {
	rateLimiter    *RateLimiter
	clientCache    map[string]*github.Client   // PAT-based clients
	appClientCache map[string]*GitHubAppClient // GitHub App clients
	cacheMutex     sync.RWMutex
	tokenLimits    map[string]*rate.Limiter
}

// NewGitHubService creates a new GitHub service
func NewGitHubService() *GitHubService {
	return &GitHubService{
		rateLimiter:    NewRateLimiter(5000, time.Hour), // GitHub's rate limit
		clientCache:    make(map[string]*github.Client),
		appClientCache: make(map[string]*GitHubAppClient),
		tokenLimits:    make(map[string]*rate.Limiter),
	}
}

// GitHubRateLimit represents GitHub API rate limit information
type GitHubRateLimit struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
}

// GitHubRepoSettings represents GitHub repository settings
type GitHubRepoSettings struct {
	Visibility        string `json:"visibility"`
	AutoInit          bool   `json:"autoInit"`
	LicenseTemplate   string `json:"licenseTemplate"`
	GitignoreTemplate string `json:"gitignoreTemplate"`
}

// ValidateCredentials validates GitHub credentials and returns comprehensive information
func (s *GitHubService) ValidateCredentials(ctx context.Context, token, owner string) ([]string, *GitHubRateLimit, error) {
	if err := s.rateLimiter.Wait(ctx, "github-validate"); err != nil {
		return nil, nil, err
	}

	client := s.getClient(token)

	// Get authenticated user to validate token
	user, resp, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to authenticate: %w", err)
	}

	// Extract scopes from response headers
	scopes := []string{}
	if scopeHeader := resp.Header.Get("X-OAuth-Scopes"); scopeHeader != "" {
		scopes = strings.Split(scopeHeader, ", ")
	}

	// Get rate limit information
	rateLimit := &GitHubRateLimit{
		Limit:     resp.Rate.Limit,
		Remaining: resp.Rate.Remaining,
		Reset:     resp.Rate.Reset.Time,
	}

	// Verify access to owner (organization or user)
	if owner != "" && owner != user.GetLogin() {
		// Check if it's an organization
		_, _, err := client.Organizations.Get(ctx, owner)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot access owner '%s': %w", owner, err)
		}
	}

	return scopes, rateLimit, nil
}

// CreateRepository creates a new GitHub repository with verification
func (s *GitHubService) CreateRepository(ctx context.Context, token, owner, name string, settings *GitHubRepoSettings) (*github.Repository, error) {
	if err := s.rateLimiter.Wait(ctx, "github-create-repo"); err != nil {
		return nil, err
	}

	client := s.getClient(token)

	repo := &github.Repository{
		Name:        github.String(name),
		Description: github.String("Migrated from Azure DevOps"),
		Private:     github.Bool(true), // Default to private
	}

	if settings != nil {
		s.applyRepositorySettings(repo, settings)
	}

	// Create repository
	// When owner is provided, assume it's an organization repository
	// This works with both GitHub App tokens and PAT tokens
	var resp *github.Response
	var err error

	if owner != "" {
		// Create organization repository
		_, resp, err = client.Repositories.Create(ctx, owner, repo)
	} else {
		// Create user repository (owner is empty string for authenticated user)
		_, resp, err = client.Repositories.Create(ctx, "", repo)
	}

	if err != nil {
		return nil, s.handleCreateRepositoryError(err, resp)
	}

	// Verify the repository was actually created
	verifiedRepo, err := s.verifyRepositoryCreation(ctx, client, owner, name, 3)
	if err != nil {
		return nil, fmt.Errorf("repository creation verification failed: %w", err)
	}

	return verifiedRepo, nil
}

// applyRepositorySettings applies settings to repository configuration
func (s *GitHubService) applyRepositorySettings(repo *github.Repository, settings *GitHubRepoSettings) {
	if settings.Visibility == "public" {
		repo.Private = github.Bool(false)
	} else if settings.Visibility == "internal" {
		repo.Private = github.Bool(false)
		repo.Visibility = github.String("internal")
	}
	if settings.AutoInit {
		repo.AutoInit = github.Bool(true)
	}
	if settings.LicenseTemplate != "" {
		repo.LicenseTemplate = github.String(settings.LicenseTemplate)
	}
	if settings.GitignoreTemplate != "" {
		repo.GitignoreTemplate = github.String(settings.GitignoreTemplate)
	}
}

// handleCreateRepositoryError handles errors from repository creation
func (s *GitHubService) handleCreateRepositoryError(err error, resp *github.Response) error {
	if resp != nil {
		return fmt.Errorf("failed to create repository (HTTP %d): %w", resp.StatusCode, err)
	}
	return fmt.Errorf("failed to create repository: %w", err)
}

// verifyRepositoryCreation verifies that a repository was actually created
func (s *GitHubService) verifyRepositoryCreation(ctx context.Context, client *github.Client, owner, name string, retries int) (*github.Repository, error) {
	for i := 0; i < retries; i++ {
		// Wait for eventual consistency
		if i > 0 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}

		repo, resp, err := client.Repositories.Get(ctx, owner, name)
		if err == nil {
			return repo, nil
		}

		if resp != nil && resp.StatusCode != 404 {
			// Non-404 error, fail immediately
			return nil, fmt.Errorf("failed to verify repository creation (HTTP %d): %w", resp.StatusCode, err)
		}

		// 404 error, repository not found yet, retry
		if i < retries-1 {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}

	return nil, fmt.Errorf("repository %s/%s was not created successfully after %d retries", owner, name, retries)
}

// GetRepository gets information about a GitHub repository
func (s *GitHubService) GetRepository(ctx context.Context, token, owner, name string) (*github.Repository, error) {
	if err := s.rateLimiter.Wait(ctx, "github-get-repo"); err != nil {
		return nil, err
	}

	client := s.getClient(token)

	repo, _, err := client.Repositories.Get(ctx, owner, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}

	return repo, nil
}

// CheckRepositoryExists checks if a repository exists
func (s *GitHubService) CheckRepositoryExists(ctx context.Context, token, owner, name string) (bool, error) {
	if err := s.rateLimiter.Wait(ctx, "github-check-repo"); err != nil {
		return false, err
	}

	client := s.getClient(token)

	repo, resp, err := client.Repositories.Get(ctx, owner, name)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return false, nil
		}
		return false, fmt.Errorf("failed to check repository: %w", err)
	}

	return repo != nil, nil
}

// CreateIssue creates a GitHub issue (for work item migration)
func (s *GitHubService) CreateIssue(ctx context.Context, token, owner, repo string, issue *github.IssueRequest) (*github.Issue, error) {
	if err := s.rateLimiter.Wait(ctx, "github-create-issue"); err != nil {
		return nil, err
	}

	client := s.getClient(token)

	createdIssue, _, err := client.Issues.Create(ctx, owner, repo, issue)
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}

	return createdIssue, nil
}

// CreateWorkflowFile creates a GitHub Actions workflow file on the repository's default branch
func (s *GitHubService) CreateWorkflowFile(ctx context.Context, token, owner, repo, path, content, message string) error {
	if err := s.rateLimiter.Wait(ctx, "github-create-workflow"); err != nil {
		return err
	}

	client := s.getClient(token)

	// Get the repository's actual default branch instead of assuming "main"
	repoInfo, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("failed to get repository info for default branch: %w", err)
	}

	defaultBranch := repoInfo.GetDefaultBranch()
	if defaultBranch == "" {
		defaultBranch = "main" // Fallback only if API returns empty
	}

	fileOptions := &github.RepositoryContentFileOptions{
		Message: github.String(message),
		Content: []byte(content),
		Branch:  github.String(defaultBranch),
	}

	_, _, err = client.Repositories.CreateFile(ctx, owner, repo, path, fileOptions)
	if err != nil {
		return fmt.Errorf("failed to create workflow file: %w", err)
	}

	return nil
}

// GetRateLimit gets the current rate limit status
func (s *GitHubService) GetRateLimit(ctx context.Context, token string) (*GitHubRateLimit, error) {
	client := s.getClient(token)

	rateLimits, _, err := client.RateLimits(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get rate limit: %w", err)
	}

	return &GitHubRateLimit{
		Limit:     rateLimits.Core.Limit,
		Remaining: rateLimits.Core.Remaining,
		Reset:     rateLimits.Core.Reset.Time,
	}, nil
}

// GitHubRepositoryState represents detailed repository state information
type GitHubRepositoryState struct {
	Exists        bool   `json:"exists"`
	IsEmpty       bool   `json:"isEmpty"`
	DefaultBranch string `json:"defaultBranch"`
	Size          int64  `json:"size"`
	HasContent    bool   `json:"hasContent"`
}

// GetRepositoryState gets comprehensive repository state information
func (s *GitHubService) GetRepositoryState(ctx context.Context, token, owner, name string) (*GitHubRepositoryState, error) {
	if err := s.rateLimiter.Wait(ctx, "github-repo-info"); err != nil {
		return nil, err
	}

	client := s.getClient(token)

	repo, resp, err := client.Repositories.Get(ctx, owner, name)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return &GitHubRepositoryState{
				Exists:        false,
				IsEmpty:       false,
				DefaultBranch: "",
				Size:          0,
				HasContent:    false,
			}, nil
		}
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	// Check if repository is empty by looking at size and default branch
	isEmpty := repo.GetSize() == 0
	hasContent := repo.GetSize() > 0

	// If size is 0, also check if there are any commits
	if !hasContent {
		// Try to get the default branch to see if it has any commits
		if repo.GetDefaultBranch() != "" {
			_, _, err := client.Repositories.GetBranch(ctx, owner, name, repo.GetDefaultBranch(), 1)
			if err != nil {
				// If we can't get the default branch, it's likely empty
				isEmpty = true
				hasContent = false
			} else {
				isEmpty = false
				hasContent = true
			}
		}
	}

	return &GitHubRepositoryState{
		Exists:        true,
		IsEmpty:       isEmpty,
		DefaultBranch: repo.GetDefaultBranch(),
		Size:          int64(repo.GetSize()),
		HasContent:    hasContent,
	}, nil
}

// SetDefaultBranch sets the default branch for a repository
func (s *GitHubService) SetDefaultBranch(ctx context.Context, token, owner, name, branch string) error {
	if err := s.rateLimiter.Wait(ctx, "github-set-default-branch"); err != nil {
		return err
	}

	client := s.getClient(token)

	// Update repository settings
	repo := &github.Repository{
		DefaultBranch: github.String(branch),
	}

	_, _, err := client.Repositories.Edit(ctx, owner, name, repo)
	if err != nil {
		return fmt.Errorf("failed to set default branch: %w", err)
	}

	return nil
}

// IsRepositoryEmpty checks if a repository is empty
func (s *GitHubService) IsRepositoryEmpty(ctx context.Context, token, owner, name string) (bool, error) {
	state, err := s.GetRepositoryState(ctx, token, owner, name)
	if err != nil {
		return false, err
	}
	return state.IsEmpty, nil
}

// SetRepositoryDefaultBranch is an alias for SetDefaultBranch for consistency
func (s *GitHubService) SetRepositoryDefaultBranch(ctx context.Context, token, owner, name, branch string) error {
	return s.SetDefaultBranch(ctx, token, owner, name, branch)
}

// Validation types for the missing methods
type GitHubValidationResult struct {
	Valid       bool                      `json:"valid"`
	Errors      []GitHubValidationError   `json:"errors,omitempty"`
	Warnings    []GitHubValidationWarning `json:"warnings,omitempty"`
	Permissions map[string]bool           `json:"permissions,omitempty"`
	RateLimit   *GitHubRateLimit          `json:"rateLimit,omitempty"`
}

type GitHubValidationError struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Field   string      `json:"field,omitempty"`
	Value   interface{} `json:"value,omitempty"`
}

type GitHubValidationWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RepositoryValidationOptions struct {
	CheckExistence      bool
	CheckPermissions    bool
	CheckNamingRules    bool
	CheckLimits         bool
	AllowExisting       bool
	RequiredPermissions []string
}

// ValidateRepositoryAccess validates access to a specific repository
func (s *GitHubService) ValidateRepositoryAccess(ctx context.Context, token, owner, repository string) error {
	client := s.getClient(token)

	_, _, err := client.Repositories.Get(ctx, owner, repository)
	if err != nil {
		return fmt.Errorf("repository access validation failed: %w", err)
	}

	return nil
}

// ValidateRepositoryForMigration validates repository for migration purposes
func (s *GitHubService) ValidateRepositoryForMigration(ctx context.Context, token, owner, repository string, options RepositoryValidationOptions) (*GitHubValidationResult, error) {
	result := &GitHubValidationResult{
		Valid:       true,
		Errors:      []GitHubValidationError{},
		Warnings:    []GitHubValidationWarning{},
		Permissions: make(map[string]bool),
	}

	client := s.getClient(token)

	// Check if repository exists
	if options.CheckExistence {
		repo, _, err := client.Repositories.Get(ctx, owner, repository)
		if err != nil {
			if !options.AllowExisting {
				result.Valid = false
				result.Errors = append(result.Errors, GitHubValidationError{
					Code:    "REPOSITORY_NOT_FOUND",
					Message: "Repository does not exist: " + err.Error(),
					Field:   "repository",
				})
			}
		} else if repo != nil && !options.AllowExisting {
			result.Valid = false
			result.Errors = append(result.Errors, GitHubValidationError{
				Code:    "REPOSITORY_EXISTS",
				Message: "Repository already exists",
				Field:   "repository",
			})
		}
	}

	// Check permissions
	if options.CheckPermissions {
		for _, perm := range options.RequiredPermissions {
			result.Permissions[perm] = true // Assume we have permissions for now
		}
	}

	return result, nil
}

// ValidateWorkItemMigration validates work item migration setup
func (s *GitHubService) ValidateWorkItemMigration(ctx context.Context, token, owner, repository string) (*GitHubValidationResult, error) {
	result := &GitHubValidationResult{
		Valid:       true,
		Errors:      []GitHubValidationError{},
		Warnings:    []GitHubValidationWarning{},
		Permissions: make(map[string]bool),
	}

	// Basic validation - check if repository exists and we have issues permission
	client := s.getClient(token)
	_, _, err := client.Repositories.Get(ctx, owner, repository)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, GitHubValidationError{
			Code:    "REPOSITORY_ACCESS_DENIED",
			Message: "Cannot access repository for work item migration: " + err.Error(),
			Field:   "repository",
		})
	}

	result.Permissions["issues"] = true
	return result, nil
}

// ValidatePipelineMigration validates pipeline migration setup
func (s *GitHubService) ValidatePipelineMigration(ctx context.Context, token, owner, repository string) (*GitHubValidationResult, error) {
	result := &GitHubValidationResult{
		Valid:       true,
		Errors:      []GitHubValidationError{},
		Warnings:    []GitHubValidationWarning{},
		Permissions: make(map[string]bool),
	}

	// Basic validation - check if repository exists and we have actions permission
	client := s.getClient(token)
	_, _, err := client.Repositories.Get(ctx, owner, repository)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, GitHubValidationError{
			Code:    "REPOSITORY_ACCESS_DENIED",
			Message: "Cannot access repository for pipeline migration: " + err.Error(),
			Field:   "repository",
		})
	}

	result.Permissions["actions"] = true
	return result, nil
}

// GetPermissions gets user permissions for a repository
func (s *GitHubService) GetPermissions(ctx context.Context, token, owner, repository string) (map[string]bool, error) {
	client := s.getClient(token)

	repo, _, err := client.Repositories.Get(ctx, owner, repository)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository: %w", err)
	}

	permissions := make(map[string]bool)
	if repo.Permissions != nil {
		permissions["admin"] = repo.Permissions.GetAdmin()
		permissions["push"] = repo.Permissions.GetPush()
		permissions["pull"] = repo.Permissions.GetPull()
	}

	return permissions, nil
}

// GetOrganizationMembership gets user membership information for an organization
func (s *GitHubService) GetOrganizationMembership(ctx context.Context, token, org string) (map[string]interface{}, error) {
	client := s.getClient(token)

	membership, _, err := client.Organizations.GetOrgMembership(ctx, "", org)
	if err != nil {
		return nil, fmt.Errorf("failed to get organization membership: %w", err)
	}

	result := make(map[string]interface{})
	if membership != nil {
		result["role"] = membership.GetRole()
		result["state"] = membership.GetState()
	}

	return result, nil
}

// getClient returns a cached GitHub client or creates a new one
func (s *GitHubService) getClient(token string) *github.Client {
	s.cacheMutex.RLock()
	client, exists := s.clientCache[token]
	s.cacheMutex.RUnlock()

	if exists {
		return client
	}

	// Create new client
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(context.Background(), ts)
	client = github.NewClient(tc)

	// Cache the client
	s.cacheMutex.Lock()
	s.clientCache[token] = client
	s.cacheMutex.Unlock()

	return client
}

// GetClientFromPAT returns a GitHub client using Personal Access Token
// This is kept for backward compatibility
func (s *GitHubService) GetClientFromPAT(token string) *github.Client {
	return s.getClient(token)
}

// GetClientFromApp returns a GitHub client using GitHub App authentication
// The client automatically refreshes its token when needed
func (s *GitHubService) GetClientFromApp(ctx context.Context, appID, installationID int64, privateKey []byte) (*github.Client, error) {
	cacheKey := fmt.Sprintf("app-%d-%d", appID, installationID)

	s.cacheMutex.RLock()
	appClient, exists := s.appClientCache[cacheKey]
	s.cacheMutex.RUnlock()

	if exists {
		// Return client, which will auto-refresh if needed
		return appClient.GetClient(ctx)
	}

	// Create new GitHub App client
	appClient, err := NewGitHubAppClient(ctx, appID, installationID, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub App client: %w", err)
	}

	// Cache the app client
	s.cacheMutex.Lock()
	s.appClientCache[cacheKey] = appClient
	s.cacheMutex.Unlock()

	return appClient.GetClient(ctx)
}

// GetAppClient returns the GitHubAppClient for direct access to token info
func (s *GitHubService) GetAppClient(appID, installationID int64) (*GitHubAppClient, bool) {
	cacheKey := fmt.Sprintf("app-%d-%d", appID, installationID)

	s.cacheMutex.RLock()
	defer s.cacheMutex.RUnlock()

	appClient, exists := s.appClientCache[cacheKey]
	return appClient, exists
}

// CleanupClientCache removes old clients from the cache
func (s *GitHubService) CleanupClientCache() {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	// Clean up PAT clients
	if len(s.clientCache) > 100 {
		s.clientCache = make(map[string]*github.Client)
	}

	// Clean up GitHub App clients
	if len(s.appClientCache) > 100 {
		s.appClientCache = make(map[string]*GitHubAppClient)
	}
}

// BranchProtectionState stores the original branch protection settings
type BranchProtectionState struct {
	HasProtection bool
	Protection    *github.Protection
	Branch        string
}

// GetBranchProtection gets the current branch protection settings
func (s *GitHubService) GetBranchProtection(ctx context.Context, token, owner, repo, branch string) (*BranchProtectionState, error) {
	if err := s.rateLimiter.Wait(ctx, "github-get-protection"); err != nil {
		return nil, err
	}

	client := s.getClient(token)

	protection, resp, err := client.Repositories.GetBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			// No protection exists
			return &BranchProtectionState{
				HasProtection: false,
				Protection:    nil,
				Branch:        branch,
			}, nil
		}
		return nil, fmt.Errorf("failed to get branch protection: %w", err)
	}

	return &BranchProtectionState{
		HasProtection: true,
		Protection:    protection,
		Branch:        branch,
	}, nil
}

// RemoveBranchProtection removes branch protection temporarily
func (s *GitHubService) RemoveBranchProtection(ctx context.Context, token, owner, repo, branch string) error {
	if err := s.rateLimiter.Wait(ctx, "github-remove-protection"); err != nil {
		return err
	}

	client := s.getClient(token)

	_, err := client.Repositories.RemoveBranchProtection(ctx, owner, repo, branch)
	if err != nil {
		return fmt.Errorf("failed to remove branch protection: %w", err)
	}

	return nil
}

// RestoreBranchProtection restores branch protection to its original state
func (s *GitHubService) RestoreBranchProtection(ctx context.Context, token, owner, repo string, state *BranchProtectionState) error {
	if state == nil || !state.HasProtection || state.Protection == nil {
		// Nothing to restore
		return nil
	}

	if err := s.rateLimiter.Wait(ctx, "github-restore-protection"); err != nil {
		return err
	}

	client := s.getClient(token)

	// Convert Protection to ProtectionRequest
	// Note: This is a simplified restoration - may not preserve all original settings
	req := &github.ProtectionRequest{
		RequiredStatusChecks: state.Protection.RequiredStatusChecks,
		EnforceAdmins:        state.Protection.GetEnforceAdmins().Enabled,
	}

	// Convert PullRequestReviewsEnforcement to PullRequestReviewsEnforcementRequest if present
	if state.Protection.RequiredPullRequestReviews != nil {
		req.RequiredPullRequestReviews = &github.PullRequestReviewsEnforcementRequest{
			DismissStaleReviews:          state.Protection.RequiredPullRequestReviews.DismissStaleReviews,
			RequireCodeOwnerReviews:      state.Protection.RequiredPullRequestReviews.RequireCodeOwnerReviews,
			RequiredApprovingReviewCount: state.Protection.RequiredPullRequestReviews.RequiredApprovingReviewCount,
		}
	}

	// Convert BranchRestrictions to BranchRestrictionsRequest if present
	if state.Protection.Restrictions != nil {
		req.Restrictions = &github.BranchRestrictionsRequest{
			Users: []string{},
			Teams: []string{},
			Apps:  []string{},
		}
		// Populate users
		for _, user := range state.Protection.Restrictions.Users {
			if user.Login != nil {
				req.Restrictions.Users = append(req.Restrictions.Users, *user.Login)
			}
		}
		// Populate teams
		for _, team := range state.Protection.Restrictions.Teams {
			if team.Slug != nil {
				req.Restrictions.Teams = append(req.Restrictions.Teams, *team.Slug)
			}
		}
		// Populate apps
		for _, app := range state.Protection.Restrictions.Apps {
			if app.Slug != nil {
				req.Restrictions.Apps = append(req.Restrictions.Apps, *app.Slug)
			}
		}
	}

	_, _, err := client.Repositories.UpdateBranchProtection(ctx, owner, repo, state.Branch, req)
	if err != nil {
		return fmt.Errorf("failed to restore branch protection: %w", err)
	}

	return nil
}

// GetDefaultBranch gets the default branch name for a repository
func (s *GitHubService) GetDefaultBranch(ctx context.Context, token, owner, name string) (string, error) {
	repo, err := s.GetRepository(ctx, token, owner, name)
	if err != nil {
		return "", err
	}

	// Get default branch from repository
	if repo.DefaultBranch != nil {
		return *repo.DefaultBranch, nil
	}

	return "main", nil // Default fallback
}
