package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	migrationv1 "github.com/tesserix/reposhift/api/v1"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-logr/logr"
)

// maskToken masks sensitive tokens in strings for logging
func maskToken(input, token string) string {
	if token == "" || len(token) < 8 {
		return input
	}
	// Show first 4 and last 4 characters, mask the middle
	maskedToken := token[:4] + "***" + token[len(token)-4:]
	return strings.ReplaceAll(input, token, maskedToken)
}

// MigrationService handles migration operations
type MigrationService struct {
	azureDevOpsService *AzureDevOpsService
	gitHubService      *GitHubService
	workDir            string
}

// NewMigrationService creates a new migration service
// workDir is the base directory for migration workspaces (typically from PVC mount)
func NewMigrationService(workDir string) *MigrationService {
	// Use environment variable if provided, otherwise use parameter
	if envWorkDir := os.Getenv("MIGRATION_WORKSPACE"); envWorkDir != "" {
		workDir = envWorkDir
	}

	// Fallback to /tmp/migrations if nothing is set
	if workDir == "" {
		workDir = "/tmp/migrations"
	}

	return &MigrationService{
		azureDevOpsService: NewAzureDevOpsService(),
		gitHubService:      NewGitHubService(),
		workDir:            workDir,
	}
}

// RepositoryMigrationProgress represents the progress of repository migration
type RepositoryMigrationProgress struct {
	Phase       string `json:"phase"`
	Description string `json:"description"`
	Percentage  int    `json:"percentage"`
	Error       string `json:"error,omitempty"`
}

// GetAzureDevOpsTokenFromServicePrincipal gets an Azure DevOps token using Service Principal credentials
func (s *MigrationService) GetAzureDevOpsTokenFromServicePrincipal(ctx context.Context, clientID, clientSecret, tenantID string) (string, error) {
	// Use the Azure DevOps service to get a token
	return s.azureDevOpsService.getToken(ctx, clientID, clientSecret, tenantID)
}

// MigrateRepository migrates a repository from Azure DevOps to GitHub
func (s *MigrationService) MigrateRepository(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, status *migrationv1.ResourceMigrationStatus, adoToken, githubToken string, logger logr.Logger, progressCallback func(*RepositoryMigrationProgress)) error {
	// Create migration workspace
	migrationDir := filepath.Join(s.workDir, migration.Name, resource.SourceName)
	if err := os.MkdirAll(migrationDir, 0755); err != nil {
		return fmt.Errorf("failed to create migration directory: %w", err)
	}
	defer s.cleanup(migrationDir, logger)

	logger.Info("Starting repository migration", "source", resource.SourceName, "target", resource.TargetName)

	// Phase 1: Clone from Azure DevOps
	progressCallback(&RepositoryMigrationProgress{
		Phase:       "cloning",
		Description: "Cloning repository from Azure DevOps",
		Percentage:  10,
	})

	adoRepoURL, err := s.buildAdoRepoURL(migration.Spec.Source, resource)
	if err != nil {
		return fmt.Errorf("failed to build ADO repository URL: %w", err)
	}

	// Determine clone depth from settings (0 = full history)
	cloneDepth := migration.Spec.Settings.CloneDepth
	logger.Info("Cloning from Azure DevOps", "url", adoRepoURL, "auth", "OAuth token", "cloneDepth", cloneDepth)

	if err := s.cloneRepository(ctx, adoRepoURL, migrationDir, adoToken, logger, cloneDepth); err != nil {
		progressCallback(&RepositoryMigrationProgress{
			Phase:       "cloning",
			Description: "Failed to clone from Azure DevOps",
			Percentage:  10,
			Error:       err.Error(),
		})
		return fmt.Errorf("failed to clone from Azure DevOps: %w", err)
	}

	progressCallback(&RepositoryMigrationProgress{
		Phase:       "cloning",
		Description: "Successfully cloned from Azure DevOps",
		Percentage:  30,
	})

	// Phase 2: Analyze repository
	progressCallback(&RepositoryMigrationProgress{
		Phase:       "analyzing",
		Description: "Analyzing repository structure",
		Percentage:  40,
	})

	repoInfo, err := s.analyzeRepository(migrationDir, logger)
	if err != nil {
		logger.Error(err, "Failed to analyze repository")
		// Continue anyway - this is not critical
	} else {
		logger.Info("Repository analysis complete",
			"branches", len(repoInfo.Branches),
			"tags", len(repoInfo.Tags),
			"commits", repoInfo.CommitCount,
			"size", repoInfo.SizeMB)
	}

	// Detect default branch from cloned repository
	defaultBranch, err := s.detectDefaultBranch(migrationDir, logger)
	if err != nil {
		logger.Info("Failed to detect default branch, will use 'main' as fallback", "error", err)
		defaultBranch = "main" // Fallback to main
	} else {
		logger.Info("Detected default branch from source repository", "branch", defaultBranch)
	}

	// Phase 3: Prepare for GitHub
	progressCallback(&RepositoryMigrationProgress{
		Phase:       "preparing",
		Description: "Preparing repository for GitHub",
		Percentage:  50,
	})

	githubRepoURL, err := s.buildGitHubRepoURL(migration.Spec.Target, resource)
	if err != nil {
		return fmt.Errorf("failed to build GitHub repository URL: %w", err)
	}

	logger.Info("Preparing for GitHub push", "url", githubRepoURL, "auth", "GitHub token")

	if err := s.prepareForGitHub(migrationDir, githubRepoURL, logger); err != nil {
		progressCallback(&RepositoryMigrationProgress{
			Phase:       "preparing",
			Description: "Failed to prepare for GitHub",
			Percentage:  50,
			Error:       err.Error(),
		})
		return fmt.Errorf("failed to prepare for GitHub: %w", err)
	}

	progressCallback(&RepositoryMigrationProgress{
		Phase:       "preparing",
		Description: "Repository prepared for GitHub",
		Percentage:  60,
	})

	// Phase 4: Push to GitHub
	progressCallback(&RepositoryMigrationProgress{
		Phase:       "pushing",
		Description: "Pushing repository to GitHub",
		Percentage:  70,
	})

	// Extract branch filter settings from migration spec
	var repoIncludeBranches, repoExcludeBranches []string
	if migration.Spec.Settings.Repository != nil {
		repoIncludeBranches = migration.Spec.Settings.Repository.IncludeBranches
		repoExcludeBranches = migration.Spec.Settings.Repository.ExcludeBranches
		if len(repoIncludeBranches) > 0 {
			logger.Info("Branch inclusion patterns configured", "patterns", repoIncludeBranches)
		}
		if len(repoExcludeBranches) > 0 {
			logger.Info("Branch exclusion patterns configured", "patterns", repoExcludeBranches)
		}
	}

	if err := s.pushToGitHub(ctx, migrationDir, githubToken, logger, repoIncludeBranches, repoExcludeBranches); err != nil {
		progressCallback(&RepositoryMigrationProgress{
			Phase:       "pushing",
			Description: "Failed to push to GitHub",
			Percentage:  70,
			Error:       err.Error(),
		})
		return fmt.Errorf("failed to push to GitHub: %w", err)
	}

	progressCallback(&RepositoryMigrationProgress{
		Phase:       "pushing",
		Description: "Successfully pushed to GitHub",
		Percentage:  90,
	})

	// Set default branch on GitHub to match source repository
	// Retry a few times since GitHub may need a moment to process the pushed branches
	logger.Info("Setting default branch on GitHub", "branch", defaultBranch)
	var setDefaultBranchErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			logger.Info("Retrying default branch setting", "attempt", attempt+1, "branch", defaultBranch)
			select {
			case <-ctx.Done():
				setDefaultBranchErr = ctx.Err()
				break
			case <-time.After(time.Duration(attempt*2) * time.Second):
			}
		}
		setDefaultBranchErr = s.gitHubService.SetDefaultBranch(ctx, githubToken, migration.Spec.Target.Owner, resource.TargetName, defaultBranch)
		if setDefaultBranchErr == nil {
			logger.Info("Successfully set default branch on GitHub", "branch", defaultBranch)
			break
		}
		logger.Info("Failed to set default branch, will retry", "attempt", attempt+1, "error", setDefaultBranchErr)
	}
	if setDefaultBranchErr != nil {
		logger.Error(setDefaultBranchErr, "Failed to set default branch on GitHub after retries", "branch", defaultBranch)
		return fmt.Errorf("failed to set default branch to '%s': %w", defaultBranch, setDefaultBranchErr)
	}

	// Phase 5: Verify migration
	progressCallback(&RepositoryMigrationProgress{
		Phase:       "verifying",
		Description: "Verifying migration completion",
		Percentage:  95,
	})

	if err := s.verifyMigration(ctx, migration.Spec.Target.Owner, resource.TargetName, githubToken, repoInfo, logger); err != nil {
		logger.Error(err, "Migration verification failed")
		// Continue anyway - the repository might still be usable
	}

	// Phase 6: Complete
	progressCallback(&RepositoryMigrationProgress{
		Phase:       "completed",
		Description: "Repository migration completed successfully",
		Percentage:  100,
	})

	logger.Info("Repository migration completed successfully",
		"source", resource.SourceName,
		"target", resource.TargetName,
		"githubURL", fmt.Sprintf("https://github.com/%s/%s", migration.Spec.Target.Owner, resource.TargetName))

	return nil
}

// RepositoryInfo contains information about a repository
type RepositoryInfo struct {
	Branches    []string
	Tags        []string
	CommitCount int
	SizeMB      float64
}

// FilterBranches filters a list of branches based on include and exclude patterns.
// Patterns support glob-style matching:
//   - "feature/*" matches any branch starting with "feature/"
//   - "develop/7.0" matches exactly "develop/7.0"
//   - "*" matches everything
//
// If includeBranches is non-empty, only branches matching at least one include pattern are kept.
// Then, any branch matching an exclude pattern is removed.
// The defaultBranch is never excluded to prevent broken migrations.
func FilterBranches(branches []string, includeBranches, excludeBranches []string, defaultBranch string, logger logr.Logger) []string {
	if len(branches) == 0 || (len(includeBranches) == 0 && len(excludeBranches) == 0) {
		return branches
	}

	var filtered []string

	for _, branch := range branches {
		// Step 1: Check include list (if specified)
		if len(includeBranches) > 0 {
			if !matchesAnyPattern(branch, includeBranches) {
				// Not in include list — skip unless it's the default branch
				if branch != defaultBranch {
					logger.V(1).Info("Branch excluded (not in include list)", "branch", branch)
					continue
				}
			}
		}

		// Step 2: Check exclude list
		if len(excludeBranches) > 0 && matchesAnyPattern(branch, excludeBranches) {
			// Never exclude the default branch
			if branch == defaultBranch {
				logger.Info("Default branch matched exclude pattern but will not be excluded", "branch", branch)
			} else {
				logger.Info("Branch excluded by pattern", "branch", branch)
				continue
			}
		}

		filtered = append(filtered, branch)
	}

	if excluded := len(branches) - len(filtered); excluded > 0 {
		logger.Info("Branch filtering complete",
			"total", len(branches),
			"kept", len(filtered),
			"excluded", excluded,
			"excludePatterns", excludeBranches,
			"includePatterns", includeBranches)
	}

	return filtered
}

// matchesAnyPattern checks if a branch name matches any of the given glob patterns.
func matchesAnyPattern(branch string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchBranchPattern(branch, pattern) {
			return true
		}
	}
	return false
}

// matchBranchPattern checks if a branch name matches a single pattern.
// Supports:
//   - Exact match: "develop/7.0" matches "develop/7.0"
//   - Wildcard suffix: "feature/*" matches "feature/anything", "feature-anything", and "feature/nested/path"
//   - Single asterisk: "*" matches everything
//
// Azure DevOps commonly uses hyphens as separators (feature-login, bugfix-123)
// while GitHub uses slashes (feature/login). The wildcard pattern "feature/*"
// matches both conventions: feature/x AND feature-x.
func matchBranchPattern(branch, pattern string) bool {
	// Exact match
	if branch == pattern {
		return true
	}

	// Wildcard matching — "prefix/*" matches both prefix/ and prefix- separators
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		// Match slash separator: feature/login-page
		if strings.HasPrefix(branch, prefix+"/") {
			return true
		}
		// Match hyphen separator: feature-login-page (common in ADO)
		if strings.HasPrefix(branch, prefix+"-") {
			return true
		}
		return false
	}

	// Full wildcard
	if pattern == "*" {
		return true
	}

	// filepath.Match style glob as fallback
	matched, err := filepath.Match(pattern, branch)
	if err == nil && matched {
		return true
	}

	return false
}

// buildAdoRepoURL builds the Azure DevOps repository URL with authentication
func (s *MigrationService) buildAdoRepoURL(source migrationv1.AdoSourceConfig, resource *migrationv1.MigrationResource) (string, error) {
	// Return clean URL without embedded credentials
	// Authentication is handled separately via HTTP Basic Auth
	return fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s",
		source.Organization,
		source.Project,
		resource.SourceName), nil
}

// buildGitHubRepoURL builds the GitHub repository URL
func (s *MigrationService) buildGitHubRepoURL(target migrationv1.GitHubTargetConfig, resource *migrationv1.MigrationResource) (string, error) {
	// Return clean URL without embedded credentials
	// Authentication is handled separately via HTTP Basic Auth
	return fmt.Sprintf("https://github.com/%s/%s.git",
		target.Owner,
		resource.TargetName), nil
}

// cloneRepository clones a repository from Azure DevOps using git CLI.
// We use git CLI instead of go-git due to compatibility issues with Azure DevOps (HTTP 400 errors)
// even with the latest go-git versions that claim to support multi_ack capabilities.
//
// When cloneDepth is 0 (or omitted), a full --mirror clone is performed.
// When cloneDepth > 0, a shallow bare clone with --depth N is used instead,
// significantly reducing clone time and disk usage for large repositories.
// Note: --mirror is incompatible with --depth, so shallow clones use
// --bare --depth N --no-single-branch to fetch all branches with truncated history.
func (s *MigrationService) cloneRepository(ctx context.Context, sourceURL, targetDir, token string, logger logr.Logger, cloneDepth ...int) error {
	depth := 0
	if len(cloneDepth) > 0 {
		depth = cloneDepth[0]
	}

	logger.Info("Cloning repository with git CLI",
		"target", targetDir,
		"url", maskToken(sourceURL, token),
		"depth", depth)

	// Ensure the parent directory exists
	parentDir := filepath.Dir(targetDir)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Build the authenticated URL for Azure DevOps
	// Format: https://x-access-token:<PAT>@dev.azure.com/org/project/_git/repo
	// Note: empty username (https://:<PAT>@...) fails in git 2.47+ so we use x-access-token
	authenticatedURL := sourceURL
	if strings.Contains(sourceURL, "dev.azure.com") {
		// Extract the part after https://
		parts := strings.SplitN(sourceURL, "://", 2)
		if len(parts) == 2 {
			authenticatedURL = fmt.Sprintf("https://x-access-token:%s@%s", token, parts[1])
		}
	}

	// Build clone command based on depth setting
	var args []string
	if depth > 0 {
		// Shallow clone: --bare --depth N --no-single-branch
		// --mirror is incompatible with --depth, so use --bare instead.
		// --no-single-branch ensures all remote branches are fetched (not just default).
		args = []string{
			"clone", "--bare",
			"--depth", fmt.Sprintf("%d", depth),
			"--no-single-branch",
			authenticatedURL, targetDir,
		}
		logger.Info("Using shallow clone",
			"depth", depth,
			"mode", "bare with all branches")
	} else {
		// Full clone: --mirror (all refs, full history)
		args = []string{"clone", "--mirror", authenticatedURL, targetDir}
	}

	cmd := exec.CommandContext(ctx, "git", args...)

	// Set up environment to avoid credential prompts
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // Disable interactive prompts
		"GIT_ASKPASS=echo",      // Avoid password prompts
	)

	// Capture output for logging
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error(err, "Git clone failed", "output", string(output), "depth", depth)
		return fmt.Errorf("git clone failed: %w (output: %s)", err, string(output))
	}

	// For shallow bare clones, also fetch all tags (--bare --depth doesn't always get them)
	if depth > 0 {
		fetchTagsCmd := exec.CommandContext(ctx, "git", "-C", targetDir, "fetch", "origin", "--tags", "--depth", fmt.Sprintf("%d", depth))
		fetchTagsCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo")
		if tagOutput, tagErr := fetchTagsCmd.CombinedOutput(); tagErr != nil {
			logger.Info("Shallow tag fetch completed with warning (non-critical)",
				"output", string(tagOutput), "error", tagErr)
		}
	}

	logger.Info("Git clone completed successfully",
		"output", string(output),
		"shallow", depth > 0,
		"depth", depth)
	return nil
}

// BuildCloneArgs returns the git clone arguments for a given depth (exported for testing).
func BuildCloneArgs(depth int, url, targetDir string) []string {
	if depth > 0 {
		return []string{
			"clone", "--bare",
			"--depth", fmt.Sprintf("%d", depth),
			"--no-single-branch",
			url, targetDir,
		}
	}
	return []string{"clone", "--mirror", url, targetDir}
}

// analyzeRepository analyzes the cloned repository using go-git
func (s *MigrationService) analyzeRepository(repoDir string, logger logr.Logger) (*RepositoryInfo, error) {
	info := &RepositoryInfo{}

	// Open the repository
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		logger.Error(err, "Failed to open repository for analysis")
		return info, fmt.Errorf("failed to open repository: %w", err)
	}

	// Get branches
	refs, err := repo.References()
	if err != nil {
		logger.Error(err, "Failed to get references")
	} else {
		err = refs.ForEach(func(ref *plumbing.Reference) error {
			if ref.Name().IsBranch() || ref.Name().IsRemote() {
				branchName := ref.Name().Short()
				if !strings.Contains(branchName, "HEAD") {
					info.Branches = append(info.Branches, branchName)
				}
			}
			return nil
		})
		if err != nil {
			logger.Error(err, "Failed to iterate branches")
		}
	}

	// Get tags
	tags, err := repo.Tags()
	if err != nil {
		logger.Error(err, "Failed to get tags")
	} else {
		err = tags.ForEach(func(ref *plumbing.Reference) error {
			tagName := ref.Name().Short()
			if tagName != "" {
				info.Tags = append(info.Tags, tagName)
			}
			return nil
		})
		if err != nil {
			logger.Error(err, "Failed to iterate tags")
		}
	}

	// Get commit count
	commitIter, err := repo.CommitObjects()
	if err != nil {
		logger.Error(err, "Failed to get commits")
	} else {
		count := 0
		err = commitIter.ForEach(func(c *object.Commit) error {
			count++
			return nil
		})
		if err != nil {
			logger.Error(err, "Failed to count commits")
		}
		info.CommitCount = count
	}

	// Get repository size (still use OS call as go-git doesn't provide this)
	if _, err := os.Stat(repoDir); err == nil {
		// Approximate size calculation
		var size int64
		filepath.Walk(repoDir, func(path string, fileInfo os.FileInfo, err error) error {
			if err == nil && !fileInfo.IsDir() {
				size += fileInfo.Size()
			}
			return nil
		})
		info.SizeMB = float64(size) / (1024 * 1024)
	}

	return info, nil
}

// detectDefaultBranch detects the default branch from a cloned (bare) repository
// by checking what HEAD points to
func (s *MigrationService) detectDefaultBranch(repoDir string, logger logr.Logger) (string, error) {
	// Read the HEAD file in the bare repository
	headFile := filepath.Join(repoDir, "HEAD")
	content, err := os.ReadFile(headFile)
	if err != nil {
		logger.Error(err, "Failed to read HEAD file", "file", headFile)
		return "", fmt.Errorf("failed to read HEAD file: %w", err)
	}

	// HEAD file format: "ref: refs/heads/main" or "ref: refs/heads/master"
	headContent := strings.TrimSpace(string(content))
	if !strings.HasPrefix(headContent, "ref: refs/heads/") {
		// If HEAD is detached or in unexpected format, fall back to checking common branches
		logger.Info("HEAD is not pointing to a branch reference", "content", headContent)

		// Check for common default branches
		for _, branch := range []string{"main", "master", "develop", "development"} {
			branchRef := filepath.Join(repoDir, "refs", "heads", branch)
			if _, err := os.Stat(branchRef); err == nil {
				logger.Info("Using fallback branch", "branch", branch)
				return branch, nil
			}
		}

		return "", fmt.Errorf("could not determine default branch from HEAD")
	}

	// Extract branch name from "ref: refs/heads/BRANCH_NAME"
	defaultBranch := strings.TrimPrefix(headContent, "ref: refs/heads/")
	logger.Info("Detected default branch from HEAD", "branch", defaultBranch)

	return defaultBranch, nil
}

// prepareForGitHub prepares the repository for pushing to GitHub using go-git
func (s *MigrationService) prepareForGitHub(repoDir, githubURL string, logger logr.Logger) error {
	// Open the repository
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		logger.Error(err, "Failed to open repository for preparation")
		return fmt.Errorf("failed to open repository: %w", err)
	}

	// Remove Azure DevOps remote (origin) if it exists
	err = repo.DeleteRemote("origin")
	if err != nil && err != git.ErrRemoteNotFound {
		logger.Info("Failed to remove origin remote (might not exist)", "error", err.Error())
	}

	// Add GitHub remote as origin
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{githubURL},
	})
	if err != nil {
		logger.Error(err, "Failed to add GitHub remote")
		return fmt.Errorf("failed to add GitHub remote: %w", err)
	}

	logger.Info("GitHub remote added successfully")
	return nil
}

// pushToGitHub pushes the repository to GitHub using git CLI with automatic branch protection handling.
// We use git CLI instead of go-git to better handle authentication and push operations.
// Branch filtering:
//   - If includeBranches is set, ONLY those branches are pushed (whitelist mode).
//   - If excludeBranches is set, those branches are skipped (blacklist mode).
//   - If both are set, include is applied first then exclude removes from that set.
//   - The default branch is never excluded to prevent broken migrations.
func (s *MigrationService) pushToGitHub(ctx context.Context, repoDir, token string, logger logr.Logger, includeBranches, excludeBranches []string) error {
	logger.Info("Pushing repository to GitHub with git CLI", "dir", repoDir)

	// Get the remote URL
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		logger.Error(err, "Failed to open repository for push")
		return fmt.Errorf("failed to open repository: %w", err)
	}

	remote, err := repo.Remote("origin")
	if err != nil {
		logger.Error(err, "Failed to get origin remote")
		return fmt.Errorf("failed to get origin remote: %w", err)
	}

	if len(remote.Config().URLs) == 0 {
		return fmt.Errorf("no URLs configured for origin remote")
	}

	githubURL := remote.Config().URLs[0]
	logger.Info("GitHub remote URL", "url", maskToken(githubURL, token))

	// Extract owner and repo name from URL
	// Format: https://github.com/owner/repo.git
	owner, repoName, err := s.extractOwnerAndRepo(githubURL)
	if err != nil {
		return fmt.Errorf("failed to extract owner/repo from URL: %w", err)
	}

	logger.Info("Preparing to push to GitHub repository", "owner", owner, "repo", repoName)

	// Note: Repository existence check is done before creation in the controller
	// At this point, the repository should exist (either created by us or pre-existing empty repo)
	// We proceed with the push operation

	// Build authenticated URL for GitHub
	// Format: https://x-access-token:TOKEN@github.com/owner/repo.git
	authenticatedURL := githubURL
	if strings.Contains(githubURL, "github.com") {
		parts := strings.SplitN(githubURL, "://", 2)
		if len(parts) == 2 {
			authenticatedURL = fmt.Sprintf("https://x-access-token:%s@%s", token, parts[1])
		}
	}

	// Determine if we have branch filters (include or exclude)
	hasBranchFilters := len(includeBranches) > 0 || len(excludeBranches) > 0

	// Push branches — either selectively (with filters) or all at once
	if hasBranchFilters {
		logger.Info("Pushing branches with filters", "includePatterns", includeBranches, "excludePatterns", excludeBranches)

		// Analyze repo to get all branches
		repoInfo, analyzeErr := s.analyzeRepository(repoDir, logger)
		if analyzeErr != nil {
			return fmt.Errorf("failed to analyze repository for branch filtering: %w", analyzeErr)
		}

		// Detect default branch
		defaultBranch, _ := s.detectDefaultBranch(repoDir, logger)
		if defaultBranch == "" {
			defaultBranch = "main"
		}

		// Filter branches using both include and exclude lists
		filteredBranches := FilterBranches(repoInfo.Branches, includeBranches, excludeBranches, defaultBranch, logger)
		logger.Info("Filtered branches for push",
			"total", len(repoInfo.Branches),
			"pushing", len(filteredBranches),
			"excluded", len(repoInfo.Branches)-len(filteredBranches))

		// Push each filtered branch individually
		for _, branch := range filteredBranches {
			refSpec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
			cmdBranch := exec.CommandContext(ctx, "git", "-C", repoDir, "push", authenticatedURL, refSpec)
			cmdBranch.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo")

			output, pushErr := cmdBranch.CombinedOutput()
			if pushErr != nil {
				outputStr := string(output)
				if strings.Contains(outputStr, "repository rule violations") ||
					strings.Contains(outputStr, "push declined") ||
					strings.Contains(outputStr, "protected branch") {
					logger.Error(pushErr, "Push blocked by GitHub protection rules", "branch", branch, "output", outputStr)
					return fmt.Errorf("❌ Push blocked by GitHub repository rulesets\n\nThe repository 'https://github.com/%s/%s' has organization-level rulesets that cannot be bypassed automatically.\n\nPlease:\n1. Go to https://github.com/organizations/%s/settings/rules\n2. Add your GitHub App to the 'Bypass list' for the ruleset\n3. Retry the migration\n\nError: %v", owner, repoName, owner, pushErr)
				}
				logger.Error(pushErr, "Failed to push branch", "branch", branch, "output", outputStr)
				return fmt.Errorf("git push branch %s failed: %w\n\nOutput: %s", branch, pushErr, outputStr)
			}
			logger.V(1).Info("Pushed branch", "branch", branch)
		}
		logger.Info("✅ Filtered branches pushed successfully", "count", len(filteredBranches))
	} else {
		// No filters — push all branches at once
		logger.Info("Pushing all branches")
		cmdBranches := exec.CommandContext(ctx, "git", "-C", repoDir, "push", "--all", authenticatedURL)
		cmdBranches.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ASKPASS=echo",
		)

		outputBranches, pushErr := cmdBranches.CombinedOutput()
		if pushErr != nil {
			outputStr := string(outputBranches)
			if strings.Contains(outputStr, "repository rule violations") ||
				strings.Contains(outputStr, "push declined") ||
				strings.Contains(outputStr, "protected branch") {
				logger.Error(pushErr, "Push blocked by GitHub protection rules despite automatic handling", "output", outputStr)
				return fmt.Errorf("❌ Push blocked by GitHub repository rulesets\n\nThe repository 'https://github.com/%s/%s' has organization-level rulesets that cannot be bypassed automatically.\n\nPlease:\n1. Go to https://github.com/organizations/%s/settings/rules\n2. Add your GitHub App to the 'Bypass list' for the ruleset\n3. Retry the migration\n\nError: %v", owner, repoName, owner, pushErr)
			}

			logger.Error(pushErr, "Git push branches failed", "output", outputStr)
			return fmt.Errorf("git push branches failed: %w\n\nOutput: %s", pushErr, outputStr)
		}
		logger.Info("✅ Branches pushed successfully", "output", string(outputBranches))
	}

	// Second, push all tags
	logger.Info("Pushing all tags")
	cmdTags := exec.CommandContext(ctx, "git", "-C", repoDir, "push", "--tags", authenticatedURL)
	cmdTags.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=echo",
	)

	outputTags, err := cmdTags.CombinedOutput()
	if err != nil {
		outputStr := string(outputTags)
		logger.Error(err, "Git push tags failed", "output", outputStr)
		return fmt.Errorf("git push tags failed: %w\n\nOutput: %s", err, outputStr)
	}
	logger.Info("✅ Tags pushed successfully", "output", string(outputTags))

	logger.Info("✅ Git push completed successfully (all branches and tags)")
	return nil
}

// extractOwnerAndRepo extracts owner and repository name from GitHub URL
func (s *MigrationService) extractOwnerAndRepo(githubURL string) (string, string, error) {
	// Remove .git suffix if present
	url := strings.TrimSuffix(githubURL, ".git")

	// Extract path from URL
	// Format: https://github.com/owner/repo
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid GitHub URL format: %s", githubURL)
	}

	// Get the last two parts (owner and repo)
	repo := parts[len(parts)-1]
	owner := parts[len(parts)-2]

	return owner, repo, nil
}

// verifyMigration verifies that the migration was successful
func (s *MigrationService) verifyMigration(ctx context.Context, owner, repo, token string, originalInfo *RepositoryInfo, logger logr.Logger) error {
	// Use GitHub API to verify repository exists and has content
	exists, err := s.gitHubService.CheckRepositoryExists(ctx, token, owner, repo)
	if err != nil {
		return fmt.Errorf("failed to verify repository existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("repository does not exist on GitHub after migration")
	}

	// Get repository information from GitHub
	githubRepo, err := s.gitHubService.GetRepository(ctx, token, owner, repo)
	if err != nil {
		return fmt.Errorf("failed to get repository information from GitHub: %w", err)
	}

	logger.Info("Migration verification completed",
		"repository", githubRepo.GetName(),
		"defaultBranch", githubRepo.GetDefaultBranch(),
		"size", githubRepo.GetSize())

	return nil
}

// cleanup removes the temporary migration directory
func (s *MigrationService) cleanup(migrationDir string, logger logr.Logger) {
	if err := os.RemoveAll(migrationDir); err != nil {
		logger.Error(err, "Failed to cleanup migration directory", "dir", migrationDir)
	} else {
		logger.Info("Migration directory cleaned up", "dir", migrationDir)
	}
}

// CleanupDir is the public version of cleanup for use by controllers
func (s *MigrationService) CleanupDir(dir string, logger logr.Logger) {
	s.cleanup(dir, logger)
}

// ValidationResult represents the result of migration validation
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationError
	Warnings []ValidationWarning
}

// ValidationError represents a validation error
type ValidationError struct {
	Code     string
	Message  string
	Field    string
	Resource string
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Code     string
	Message  string
	Field    string
	Resource string
}

// ValidateMigration validates a migration configuration
func (s *MigrationService) ValidateMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Validate source configuration
	if migration.Spec.Source.Organization == "" {
		result.Errors = append(result.Errors, ValidationError{
			Code:    "MISSING_ORGANIZATION",
			Message: "Source organization is required",
			Field:   "spec.source.organization",
		})
	}

	if migration.Spec.Source.Project == "" {
		result.Errors = append(result.Errors, ValidationError{
			Code:    "MISSING_PROJECT",
			Message: "Source project is required",
			Field:   "spec.source.project",
		})
	}

	// Validate target configuration
	if migration.Spec.Target.Owner == "" {
		result.Errors = append(result.Errors, ValidationError{
			Code:    "MISSING_TARGET_OWNER",
			Message: "Target owner is required",
			Field:   "spec.target.owner",
		})
	}

	// Validate resources
	if len(migration.Spec.Resources) == 0 {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:    "NO_RESOURCES",
			Message: "No resources specified for migration",
			Field:   "spec.resources",
		})
	}

	// Validate each resource
	for i, resource := range migration.Spec.Resources {
		if resource.SourceID == "" {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "MISSING_SOURCE_ID",
				Message:  "Resource source ID is required",
				Field:    fmt.Sprintf("spec.resources[%d].sourceId", i),
				Resource: resource.SourceName,
			})
		}

		if resource.Type == "" {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "MISSING_RESOURCE_TYPE",
				Message:  "Resource type is required",
				Field:    fmt.Sprintf("spec.resources[%d].type", i),
				Resource: resource.SourceName,
			})
		}

		if resource.SourceName == "" {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "MISSING_SOURCE_NAME",
				Message:  "Resource source name is required",
				Field:    fmt.Sprintf("spec.resources[%d].sourceName", i),
				Resource: resource.SourceName,
			})
		}

		if resource.TargetName == "" {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "MISSING_TARGET_NAME",
				Message:  "Resource target name is required",
				Field:    fmt.Sprintf("spec.resources[%d].targetName", i),
				Resource: resource.SourceName,
			})
		}
	}

	// Git CLI is no longer required - using go-git library instead

	// Set overall validation result
	result.Valid = len(result.Errors) == 0

	return result, nil
}

// StartMigration starts a migration process
func (s *MigrationService) StartMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) error {
	// This would start the actual migration process
	// For now, we'll just return success
	return nil
}

// StopMigration stops a running migration
func (s *MigrationService) StopMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) error {
	// This would stop the migration process
	return nil
}

// PauseMigration pauses a running migration
func (s *MigrationService) PauseMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) error {
	// This would pause the migration process
	return nil
}

// ResumeMigration resumes a paused migration
func (s *MigrationService) ResumeMigration(ctx context.Context, migration *migrationv1.AdoToGitMigration) error {
	// This would resume the migration process
	return nil
}

// SyncResult represents the result of a repository sync operation
type SyncResult struct {
	BranchesSynced []string
	TagsSynced     []string
	Error          error
}

// SyncRepository performs incremental sync from ADO to GitHub
// This is used for continuous synchronization after initial migration
func (s *MigrationService) SyncRepository(ctx context.Context, migration *migrationv1.AdoToGitMigration, resource *migrationv1.MigrationResource, adoToken, githubToken string, logger logr.Logger) (*SyncResult, error) {
	result := &SyncResult{
		BranchesSynced: []string{},
		TagsSynced:     []string{},
	}

	// Create sync workspace
	syncDir := filepath.Join(s.workDir, "sync", migration.Name, resource.SourceName)

	// Check if workspace exists from previous sync
	repoExists := false
	if _, err := os.Stat(filepath.Join(syncDir, "config")); err == nil {
		repoExists = true
		logger.Info("Using existing sync workspace", "dir", syncDir)
	}

	if !repoExists {
		// First sync - need to clone
		logger.Info("First sync - cloning repository", "dir", syncDir)
		if err := os.MkdirAll(filepath.Dir(syncDir), 0755); err != nil {
			return result, fmt.Errorf("failed to create sync directory: %w", err)
		}

		adoRepoURL, err := s.buildAdoRepoURL(migration.Spec.Source, resource)
		if err != nil {
			return result, fmt.Errorf("failed to build ADO repository URL: %w", err)
		}

		if err := s.cloneRepository(ctx, adoRepoURL, syncDir, adoToken, logger); err != nil {
			return result, fmt.Errorf("failed to clone repository for sync: %w", err)
		}
	}

	// Fetch latest changes from ADO
	// Note: git fetch uses the credentials from the existing remote configuration
	logger.Info("Fetching latest changes from ADO")

	// Fetch all refs from ADO
	fetchCmd := exec.CommandContext(ctx, "git", "-C", syncDir, "fetch", "--all", "--prune", "--tags")
	fetchCmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=echo",
	)

	fetchOutput, err := fetchCmd.CombinedOutput()
	if err != nil {
		logger.Error(err, "Git fetch failed", "output", string(fetchOutput))
		return result, fmt.Errorf("git fetch failed: %w", err)
	}
	logger.Info("Fetched latest changes from ADO", "output", string(fetchOutput))

	// Get all refs that need to be synced
	repo, err := git.PlainOpen(syncDir)
	if err != nil {
		return result, fmt.Errorf("failed to open repository: %w", err)
	}

	// Get all branches
	branches, err := repo.Branches()
	if err != nil {
		logger.Error(err, "Failed to list branches")
	} else {
		err = branches.ForEach(func(ref *plumbing.Reference) error {
			branchName := ref.Name().Short()
			result.BranchesSynced = append(result.BranchesSynced, branchName)
			return nil
		})
		if err != nil {
			logger.Error(err, "Error iterating branches")
		}
	}

	// Get all tags if sync tags is enabled
	if migration.Spec.Settings.Sync != nil && migration.Spec.Settings.Sync.SyncTags {
		tags, err := repo.Tags()
		if err != nil {
			logger.Error(err, "Failed to list tags")
		} else {
			err = tags.ForEach(func(ref *plumbing.Reference) error {
				tagName := ref.Name().Short()
				result.TagsSynced = append(result.TagsSynced, tagName)
				return nil
			})
			if err != nil {
				logger.Error(err, "Error iterating tags")
			}
		}
	}

	// Build GitHub URL
	githubURL := fmt.Sprintf("https://github.com/%s/%s.git", migration.Spec.Target.Owner, resource.TargetName)

	// Build authenticated URL for GitHub
	authenticatedGitHubURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git",
		githubToken, migration.Spec.Target.Owner, resource.TargetName)

	// Push to GitHub
	logger.Info("Pushing changes to GitHub", "branches", len(result.BranchesSynced), "tags", len(result.TagsSynced))

	pushArgs := []string{"-C", syncDir, "push"}

	// Check if force push is enabled
	if migration.Spec.Settings.Sync != nil && migration.Spec.Settings.Sync.ForcePush {
		pushArgs = append(pushArgs, "--force")
		logger.Info("Force push enabled")
	}

	// Push all branches and tags
	pushArgs = append(pushArgs, "--mirror", authenticatedGitHubURL)

	pushCmd := exec.CommandContext(ctx, "git", pushArgs...)
	pushCmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=echo",
	)

	pushOutput, err := pushCmd.CombinedOutput()
	if err != nil {
		outputStr := string(pushOutput)
		logger.Error(err, "Git push failed during sync", "output", outputStr)

		// Check for common errors
		if strings.Contains(outputStr, "repository rule violations") ||
			strings.Contains(outputStr, "push declined") ||
			strings.Contains(outputStr, "protected branch") {
			return result, fmt.Errorf("❌ Sync push blocked by GitHub protection rules\n\nThe repository '%s' has branch protection that blocks sync.\n\nPlease:\n1. Go to https://github.com/%s/%s/settings/branches\n2. Temporarily disable protection OR add GitHub App to bypass list\n3. Retry sync\n\nError: %v",
				githubURL, migration.Spec.Target.Owner, resource.TargetName, err)
		}

		return result, fmt.Errorf("git push failed during sync: %w\nOutput: %s", err, outputStr)
	}

	logger.Info("✅ Sync completed successfully",
		"branches", len(result.BranchesSynced),
		"tags", len(result.TagsSynced),
		"output", string(pushOutput))

	return result, nil
}
