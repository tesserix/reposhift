package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	migrationv1 "github.com/tesserix/reposhift/api/v1"

	"github.com/go-logr/logr"
)

// MonoRepoMigrationResult contains the result of a monorepo migration
type MonoRepoMigrationResult struct {
	MonoRepoURL   string
	RepoResults   []MonoRepoRepoResult
	TotalCommits  int
	TotalBranches int
	TotalTags     int
	TotalSizeMB   float64
	DefaultBranch string
}

// MonoRepoRepoResult contains the result for an individual repo
type MonoRepoRepoResult struct {
	Name             string
	SubdirectoryName string
	DefaultBranch    string
	Branches         []string
	Tags             []string
	CommitCount      int
	SizeMB           float64
	Error            error
}

// MonoRepoMigrationProgress represents the progress of monorepo migration
type MonoRepoMigrationProgress struct {
	Phase       string `json:"phase"`
	RepoName    string `json:"repoName,omitempty"`
	Description string `json:"description"`
	Percentage  int    `json:"percentage"`
	Error       string `json:"error,omitempty"`
}

// CloneRepoResult is the result of cloning a single repo
type CloneRepoResult struct {
	RepoName      string
	CloneDir      string
	DefaultBranch string
	RepoInfo      *RepositoryInfo
	Error         error
}

// RewriteRepoResult is the result of rewriting a single repo
type RewriteRepoResult struct {
	RepoName    string
	WorkDir     string
	SubdirName  string
	Branches    []string
	Tags        []string
	CommitCount int
	Error       error
}

// CloneMonoRepoSource clones a single ADO repo for monorepo migration.
// Returns the clone directory path and detected default branch.
// cloneDepth controls shallow cloning: 0 = full history, N>0 = shallow with N commits.
func (s *MigrationService) CloneMonoRepoSource(
	ctx context.Context,
	sourceOrg, sourceProject, repoName, adoToken string,
	workDir string,
	logger logr.Logger,
	cloneDepth ...int,
) (*CloneRepoResult, error) {
	result := &CloneRepoResult{RepoName: repoName}

	depth := 0
	if len(cloneDepth) > 0 {
		depth = cloneDepth[0]
	}

	// Build the ADO URL
	adoURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s", sourceOrg, sourceProject, repoName)

	// Clone directory is a bare mirror (or bare shallow)
	cloneDir := filepath.Join(workDir, "repos", repoName)
	result.CloneDir = cloneDir

	logger.Info("Cloning ADO repo for monorepo", "repo", repoName, "url", maskToken(adoURL, adoToken), "depth", depth)

	if err := s.cloneRepository(ctx, adoURL, cloneDir, adoToken, logger, depth); err != nil {
		result.Error = fmt.Errorf("failed to clone repo %s: %w", repoName, err)
		return result, result.Error
	}

	// Detect default branch
	defaultBranch, err := s.detectDefaultBranch(cloneDir, logger)
	if err != nil {
		logger.Info("Failed to detect default branch, using 'main' as fallback", "repo", repoName, "error", err)
		defaultBranch = "main"
	}
	result.DefaultBranch = defaultBranch

	// Analyze the repo
	repoInfo, err := s.analyzeRepository(cloneDir, logger)
	if err != nil {
		logger.Error(err, "Failed to analyze repository", "repo", repoName)
		// Initialize empty info so callers don't need nil checks
		repoInfo = &RepositoryInfo{}
	}
	result.RepoInfo = repoInfo

	logger.Info("Successfully cloned repo for monorepo",
		"repo", repoName,
		"defaultBranch", defaultBranch,
		"branches", len(repoInfo.Branches),
		"tags", len(repoInfo.Tags),
		"commits", repoInfo.CommitCount)

	return result, nil
}

// RewriteRepoToSubdirectory converts a bare clone into a working copy and
// rewrites all history so files live under <subdirName>/ using git-filter-repo.
func (s *MigrationService) RewriteRepoToSubdirectory(
	ctx context.Context,
	bareCloneDir, subdirName string,
	workDir string,
	cleanupBareClone bool,
	logger logr.Logger,
) (*RewriteRepoResult, error) {
	result := &RewriteRepoResult{
		RepoName:   filepath.Base(bareCloneDir),
		SubdirName: subdirName,
	}

	// Create a working copy from the bare clone
	workingDir := bareCloneDir + "-work"
	result.WorkDir = workingDir

	// Idempotency: if the working copy already exists with the subdirectory
	// rewrite applied (from a previous successful reconcile), skip clone + filter-repo.
	if _, statErr := os.Stat(filepath.Join(workingDir, subdirName)); statErr == nil {
		logger.Info("Working copy already exists with subdirectory rewrite, skipping clone+filter-repo",
			"work", workingDir, "subdir", subdirName)
		goto collectStats
	}

	// Clean up any partial working copy from a previous failed attempt
	if _, statErr := os.Stat(workingDir); statErr == nil {
		logger.Info("Removing partial working copy before retry", "work", workingDir)
		os.RemoveAll(workingDir)
	}

	logger.Info("Creating working copy from bare clone", "bare", bareCloneDir, "work", workingDir)

	{
		// git clone (local, from bare to working copy)
		cmd := exec.CommandContext(ctx, "git", "clone", bareCloneDir, workingDir)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			result.Error = fmt.Errorf("failed to create working copy: %w (output: %s)", err, string(output))
			return result, result.Error
		}
	}

	// Fetch all remote branches as local branches so filter-repo can rewrite them
	{
		fetchCmd := exec.CommandContext(ctx, "git", "-C", workingDir, "fetch", "origin", "+refs/heads/*:refs/heads/*")
		fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if fetchOutput, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
			logger.Info("Fetch all branches info", "output", string(fetchOutput), "error", fetchErr)
		}
	}

	// Run git-filter-repo to rewrite history into subdirectory
	logger.Info("Running git-filter-repo", "subdir", subdirName, "workDir", workingDir)

	{
		filterCmd := exec.CommandContext(ctx, "git", "-C", workingDir, "filter-repo",
			"--to-subdirectory-filter", subdirName+"/",
			"--force")
		filterCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		filterOutput, err := filterCmd.CombinedOutput()
		if err != nil {
			result.Error = fmt.Errorf("git-filter-repo failed for %s: %w (output: %s)", subdirName, err, string(filterOutput))
			return result, result.Error
		}
		logger.Info("git-filter-repo completed successfully", "subdir", subdirName, "output", string(filterOutput))
	}

collectStats:
	// Collect branch info from the rewritten repo
	branchCmd := exec.CommandContext(ctx, "git", "-C", workingDir, "branch", "--list", "--no-color")
	branchOutput, err := branchCmd.CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(branchOutput)), "\n") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "* "))
			if branch != "" {
				result.Branches = append(result.Branches, branch)
			}
		}
	}

	// Collect tag info
	tagCmd := exec.CommandContext(ctx, "git", "-C", workingDir, "tag", "--list")
	tagOutput, err := tagCmd.CombinedOutput()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(tagOutput)), "\n") {
			tag := strings.TrimSpace(line)
			if tag != "" {
				result.Tags = append(result.Tags, tag)
			}
		}
	}

	// Count commits
	countCmd := exec.CommandContext(ctx, "git", "-C", workingDir, "rev-list", "--count", "--all")
	countOutput, err := countCmd.CombinedOutput()
	if err == nil {
		fmt.Sscanf(strings.TrimSpace(string(countOutput)), "%d", &result.CommitCount)
	}

	// Clean up bare clone if requested to save disk space
	if cleanupBareClone {
		logger.Info("Cleaning up bare clone to save disk space", "dir", bareCloneDir)
		if err := os.RemoveAll(bareCloneDir); err != nil {
			logger.Error(err, "Failed to clean up bare clone", "dir", bareCloneDir)
		}
	}

	logger.Info("Repository rewritten to subdirectory",
		"subdir", subdirName,
		"branches", len(result.Branches),
		"tags", len(result.Tags),
		"commits", result.CommitCount)

	return result, nil
}

// InitMonoRepo initializes an empty monorepo with an initial commit.
func (s *MigrationService) InitMonoRepo(
	ctx context.Context,
	workDir string,
	defaultBranch string,
	logger logr.Logger,
) (string, error) {
	monoRepoDir := filepath.Join(workDir, "monorepo")

	if err := os.MkdirAll(monoRepoDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create monorepo directory: %w", err)
	}

	// git init
	initCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "init", "--initial-branch", defaultBranch)
	initCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := initCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init failed: %w (output: %s)", err, string(output))
	}

	// Configure git user for commits
	configCmds := [][]string{
		{"git", "-C", monoRepoDir, "config", "user.email", "migration@ado-to-git-migration.io"},
		{"git", "-C", monoRepoDir, "config", "user.name", "ADO Migration Operator"},
	}
	for _, args := range configCmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git config failed: %w (output: %s)", err, string(output))
		}
	}

	// Create initial empty commit
	commitCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "commit",
		"--allow-empty", "-m", "Initialize monorepo")
	commitCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("initial commit failed: %w (output: %s)", err, string(output))
	}

	logger.Info("Initialized monorepo", "dir", monoRepoDir, "defaultBranch", defaultBranch)
	return monoRepoDir, nil
}

// MergeRepoIntoMonoRepo merges a rewritten repo into the monorepo.
// It merges the default branch with --allow-unrelated-histories,
// creates prefixed branches for non-default branches, and prefixed tags.
// excludeBranches patterns are applied to filter out unwanted branches before merging.
func (s *MigrationService) MergeRepoIntoMonoRepo(
	ctx context.Context,
	monoRepoDir, rewrittenRepoDir string,
	repoName, defaultBranch string,
	rewriteResult *RewriteRepoResult,
	logger logr.Logger,
	excludeBranches ...[]string,
) error {
	logger.Info("Merging repo into monorepo",
		"repo", repoName,
		"defaultBranch", defaultBranch,
		"monoRepoDir", monoRepoDir)

	// Add the rewritten repo as a remote
	addRemoteCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "remote", "add", repoName, rewrittenRepoDir)
	if output, err := addRemoteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add remote %s: %w (output: %s)", repoName, err, string(output))
	}

	// Fetch all refs from the rewritten repo
	fetchCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "fetch", repoName)
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to fetch from %s: %w (output: %s)", repoName, err, string(output))
	}

	// Merge the default branch with --allow-unrelated-histories
	// Since files were moved to non-overlapping subdirs, this never conflicts
	mergeRef := fmt.Sprintf("%s/%s", repoName, defaultBranch)
	mergeCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "merge",
		"--allow-unrelated-histories",
		"--no-edit",
		"-m", fmt.Sprintf("Merge %s into monorepo", repoName),
		mergeRef)
	mergeCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := mergeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to merge %s: %w (output: %s)", repoName, err, string(output))
	}

	logger.Info("Merged default branch", "repo", repoName, "branch", defaultBranch)

	// Filter branches if exclusion patterns are provided
	branchesToMerge := rewriteResult.Branches
	if len(excludeBranches) > 0 && len(excludeBranches[0]) > 0 {
		branchesToMerge = FilterBranches(rewriteResult.Branches, nil, excludeBranches[0], defaultBranch, logger)
		logger.Info("Filtered branches for monorepo merge",
			"repo", repoName,
			"total", len(rewriteResult.Branches),
			"kept", len(branchesToMerge),
			"excluded", len(rewriteResult.Branches)-len(branchesToMerge),
			"patterns", excludeBranches[0])
	}

	// Create prefixed branches for non-default branches
	for _, branch := range branchesToMerge {
		if branch == defaultBranch {
			continue
		}
		prefixedBranch := fmt.Sprintf("%s/%s", repoName, branch)
		remoteBranch := fmt.Sprintf("%s/%s", repoName, branch)

		branchCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "branch", prefixedBranch, remoteBranch)
		if output, err := branchCmd.CombinedOutput(); err != nil {
			logger.Error(err, "Failed to create prefixed branch",
				"branch", prefixedBranch, "output", string(output))
			// Continue - non-critical
		} else {
			logger.V(1).Info("Created prefixed branch", "branch", prefixedBranch)
		}
	}

	// Create prefixed tags
	for _, tag := range rewriteResult.Tags {
		prefixedTag := fmt.Sprintf("%s/%s", repoName, tag)
		remoteTag := fmt.Sprintf("%s/%s", repoName, tag)

		// Check if remote tag exists as a fetched ref
		tagCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "tag", prefixedTag, remoteTag)
		if output, err := tagCmd.CombinedOutput(); err != nil {
			logger.Error(err, "Failed to create prefixed tag",
				"tag", prefixedTag, "output", string(output))
			// Continue - non-critical
		} else {
			logger.V(1).Info("Created prefixed tag", "tag", prefixedTag)
		}
	}

	// Remove the remote to keep things clean
	removeRemoteCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "remote", "remove", repoName)
	if output, err := removeRemoteCmd.CombinedOutput(); err != nil {
		logger.Error(err, "Failed to remove remote", "remote", repoName, "output", string(output))
	}

	logger.Info("Successfully merged repo into monorepo",
		"repo", repoName,
		"branches", len(rewriteResult.Branches),
		"tags", len(rewriteResult.Tags))

	return nil
}

// PushMonoRepo pushes the assembled monorepo to GitHub.
func (s *MigrationService) PushMonoRepo(
	ctx context.Context,
	monoRepoDir string,
	githubToken, owner, repo, defaultBranch string,
	logger logr.Logger,
) error {
	// Build authenticated GitHub URL
	authenticatedURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", githubToken, owner, repo)

	logger.Info("Pushing monorepo to GitHub",
		"owner", owner, "repo", repo, "defaultBranch", defaultBranch)

	// Add GitHub remote
	addRemoteCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "remote", "add", "origin", authenticatedURL)
	if output, err := addRemoteCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add origin remote: %w (output: %s)", err, string(output))
	}

	// Push all branches
	pushBranchesCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "push", "--all", "origin")
	pushBranchesCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo")
	if output, err := pushBranchesCmd.CombinedOutput(); err != nil {
		outputStr := string(output)
		if strings.Contains(outputStr, "repository rule violations") ||
			strings.Contains(outputStr, "push declined") ||
			strings.Contains(outputStr, "protected branch") {
			return fmt.Errorf("push blocked by GitHub repository rulesets for %s/%s: %w\nOutput: %s", owner, repo, err, outputStr)
		}
		return fmt.Errorf("git push branches failed: %w (output: %s)", err, outputStr)
	}

	logger.Info("Pushed all branches successfully")

	// Push all tags
	pushTagsCmd := exec.CommandContext(ctx, "git", "-C", monoRepoDir, "push", "--tags", "origin")
	pushTagsCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo")
	if output, err := pushTagsCmd.CombinedOutput(); err != nil {
		logger.Error(err, "Failed to push tags", "output", string(output))
		return fmt.Errorf("git push tags failed: %w (output: %s)", err, string(output))
	}

	logger.Info("Pushed all tags successfully")

	// Set default branch via GitHub API
	if err := s.gitHubService.SetDefaultBranch(ctx, githubToken, owner, repo, defaultBranch); err != nil {
		logger.Error(err, "Failed to set default branch on GitHub", "branch", defaultBranch)
		// Non-fatal - the repo is still usable
	} else {
		logger.Info("Set default branch on GitHub", "branch", defaultBranch)
	}

	return nil
}

// MigrateMonoRepo orchestrates the full monorepo migration in a single call.
// This is useful for testing or one-shot migration; the controller uses individual
// phase methods for resumability.
func (s *MigrationService) MigrateMonoRepo(
	ctx context.Context,
	migration *migrationv1.MonoRepoMigration,
	adoToken, githubToken string,
	logger logr.Logger,
	progressCallback func(*MonoRepoMigrationProgress),
) (*MonoRepoMigrationResult, error) {
	result := &MonoRepoMigrationResult{}

	// Sort repos by priority
	repos := make([]migrationv1.MonoRepoSourceRepo, len(migration.Spec.SourceRepos))
	copy(repos, migration.Spec.SourceRepos)
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].Priority < repos[j].Priority
	})

	// Create workspace
	workDir := filepath.Join(s.workDir, migration.Name)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}
	defer s.cleanup(workDir, logger)

	defaultBranch := migration.Spec.Target.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	// Phase 1: Clone all repos
	cloneResults := make(map[string]*CloneRepoResult)
	for i, repo := range repos {
		progressCallback(&MonoRepoMigrationProgress{
			Phase:       "cloning",
			RepoName:    repo.Name,
			Description: fmt.Sprintf("Cloning %s (%d/%d)", repo.Name, i+1, len(repos)),
			Percentage:  (i * 25) / len(repos),
		})

		cloneResult, err := s.CloneMonoRepoSource(ctx, migration.Spec.Source.Organization,
			migration.Spec.Source.Project, repo.Name, adoToken, workDir, logger)
		if err != nil {
			if !migration.Spec.Settings.GetContinueOnError() {
				return nil, err
			}
			logger.Error(err, "Failed to clone repo, continuing", "repo", repo.Name)
			result.RepoResults = append(result.RepoResults, MonoRepoRepoResult{
				Name:  repo.Name,
				Error: err,
			})
			continue
		}
		cloneResults[repo.Name] = cloneResult
	}

	// Phase 2: Rewrite all repos
	rewriteResults := make(map[string]*RewriteRepoResult)
	for i, repo := range repos {
		cloneResult, ok := cloneResults[repo.Name]
		if !ok {
			continue // skipped due to clone failure
		}

		subdirName := repo.SubdirectoryName
		if subdirName == "" {
			subdirName = repo.Name
		}

		progressCallback(&MonoRepoMigrationProgress{
			Phase:       "rewriting",
			RepoName:    repo.Name,
			Description: fmt.Sprintf("Rewriting %s to subdirectory %s/ (%d/%d)", repo.Name, subdirName, i+1, len(repos)),
			Percentage:  25 + (i*25)/len(repos),
		})

		rewriteResult, err := s.RewriteRepoToSubdirectory(ctx, cloneResult.CloneDir, subdirName,
			workDir, migration.Spec.Settings.GetCleanupBetweenRepos(), logger)
		if err != nil {
			if !migration.Spec.Settings.GetContinueOnError() {
				return nil, err
			}
			logger.Error(err, "Failed to rewrite repo, continuing", "repo", repo.Name)
			result.RepoResults = append(result.RepoResults, MonoRepoRepoResult{
				Name:  repo.Name,
				Error: err,
			})
			continue
		}
		rewriteResults[repo.Name] = rewriteResult
	}

	// Phase 3: Assemble monorepo
	progressCallback(&MonoRepoMigrationProgress{
		Phase:       "merging",
		Description: "Initializing monorepo",
		Percentage:  50,
	})

	monoRepoDir, err := s.InitMonoRepo(ctx, workDir, defaultBranch, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize monorepo: %w", err)
	}

	for i, repo := range repos {
		rewriteResult, ok := rewriteResults[repo.Name]
		if !ok {
			continue // skipped
		}

		cloneResult, ok := cloneResults[repo.Name]
		if !ok {
			continue // repo was skipped during clone
		}
		repoBranch := cloneResult.DefaultBranch
		if repo.DefaultBranch != "" {
			repoBranch = repo.DefaultBranch
		}

		progressCallback(&MonoRepoMigrationProgress{
			Phase:       "merging",
			RepoName:    repo.Name,
			Description: fmt.Sprintf("Merging %s into monorepo (%d/%d)", repo.Name, i+1, len(repos)),
			Percentage:  50 + (i*25)/len(repos),
		})

		if err := s.MergeRepoIntoMonoRepo(ctx, monoRepoDir, rewriteResult.WorkDir,
			repo.Name, repoBranch, rewriteResult, logger); err != nil {
			if !migration.Spec.Settings.GetContinueOnError() {
				return nil, err
			}
			logger.Error(err, "Failed to merge repo, continuing", "repo", repo.Name)
			result.RepoResults = append(result.RepoResults, MonoRepoRepoResult{
				Name:  repo.Name,
				Error: err,
			})
			continue
		}

		subdirName := repo.SubdirectoryName
		if subdirName == "" {
			subdirName = repo.Name
		}

		repoResult := MonoRepoRepoResult{
			Name:             repo.Name,
			SubdirectoryName: subdirName,
			DefaultBranch:    repoBranch,
			Branches:         rewriteResult.Branches,
			Tags:             rewriteResult.Tags,
			CommitCount:      rewriteResult.CommitCount,
		}
		result.RepoResults = append(result.RepoResults, repoResult)
		result.TotalCommits += rewriteResult.CommitCount
		result.TotalBranches += len(rewriteResult.Branches)
		result.TotalTags += len(rewriteResult.Tags)
	}

	// Phase 4: Create repo on GitHub if needed
	progressCallback(&MonoRepoMigrationProgress{
		Phase:       "pushing",
		Description: "Creating GitHub repository",
		Percentage:  75,
	})

	exists, err := s.gitHubService.CheckRepositoryExists(ctx, githubToken,
		migration.Spec.Target.Owner, migration.Spec.Target.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to check if target repo exists: %w", err)
	}

	if !exists {
		settings := &GitHubRepoSettings{
			Visibility: migration.Spec.Target.Visibility,
		}
		_, err := s.gitHubService.CreateRepository(ctx, githubToken,
			migration.Spec.Target.Owner, migration.Spec.Target.Repository, settings)
		if err != nil {
			return nil, fmt.Errorf("failed to create GitHub repository: %w", err)
		}
	}

	// Push monorepo
	progressCallback(&MonoRepoMigrationProgress{
		Phase:       "pushing",
		Description: "Pushing monorepo to GitHub",
		Percentage:  85,
	})

	if err := s.PushMonoRepo(ctx, monoRepoDir, githubToken,
		migration.Spec.Target.Owner, migration.Spec.Target.Repository, defaultBranch, logger); err != nil {
		return nil, fmt.Errorf("failed to push monorepo: %w", err)
	}

	result.MonoRepoURL = fmt.Sprintf("https://github.com/%s/%s", migration.Spec.Target.Owner, migration.Spec.Target.Repository)
	result.DefaultBranch = defaultBranch

	progressCallback(&MonoRepoMigrationProgress{
		Phase:       "completed",
		Description: "Monorepo migration completed successfully",
		Percentage:  100,
	})

	return result, nil
}

// CheckDiskSpace checks if there is enough disk space available
func (s *MigrationService) CheckDiskSpace(path string, requiredMB int) (bool, int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false, 0, fmt.Errorf("failed to check disk space: %w", err)
	}

	// Available space in MB
	availableMB := int64(stat.Bavail) * int64(stat.Bsize) / (1024 * 1024)

	if requiredMB > 0 && availableMB < int64(requiredMB) {
		return false, availableMB, nil
	}

	return true, availableMB, nil
}

// ValidateMonoRepoMigration validates a monorepo migration configuration
func (s *MigrationService) ValidateMonoRepoMigration(ctx context.Context, migration *migrationv1.MonoRepoMigration) (*ValidationResult, error) {
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
	if migration.Spec.Target.Repository == "" {
		result.Errors = append(result.Errors, ValidationError{
			Code:    "MISSING_TARGET_REPO",
			Message: "Target repository name is required",
			Field:   "spec.target.repository",
		})
	}

	// Validate source repos
	if len(migration.Spec.SourceRepos) == 0 {
		result.Errors = append(result.Errors, ValidationError{
			Code:    "NO_SOURCE_REPOS",
			Message: "At least one source repository is required",
			Field:   "spec.sourceRepos",
		})
	}

	// Check for duplicate subdirectory names
	subdirNames := make(map[string]string) // subdir -> repo name
	for i, repo := range migration.Spec.SourceRepos {
		if repo.Name == "" {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "MISSING_REPO_NAME",
				Message:  "Source repository name is required",
				Field:    fmt.Sprintf("spec.sourceRepos[%d].name", i),
				Resource: repo.Name,
			})
			continue
		}

		subdirName := repo.SubdirectoryName
		if subdirName == "" {
			subdirName = repo.Name
		}

		if existingRepo, exists := subdirNames[subdirName]; exists {
			result.Errors = append(result.Errors, ValidationError{
				Code:     "DUPLICATE_SUBDIR",
				Message:  fmt.Sprintf("Subdirectory name '%s' is used by both '%s' and '%s'", subdirName, existingRepo, repo.Name),
				Field:    fmt.Sprintf("spec.sourceRepos[%d].subdirectoryName", i),
				Resource: repo.Name,
			})
		}
		subdirNames[subdirName] = repo.Name
	}

	// Check git-filter-repo availability
	if _, err := exec.LookPath("git-filter-repo"); err != nil {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Code:    "MISSING_FILTER_REPO",
			Message: "git-filter-repo is not installed - required for monorepo migration",
			Field:   "runtime",
		})
	}

	result.Valid = len(result.Errors) == 0
	return result, nil
}
