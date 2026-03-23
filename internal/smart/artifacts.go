package smart

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
)

// ArtifactMigrator handles migration of packages and artifacts from ADO to GitHub
type ArtifactMigrator struct {
	log          logr.Logger
	adoClient    *ADOArtifactsClient
	githubClient *GitHubPackagesClient
	cache        *ArtifactCache
	parallelism  int
}

// NewArtifactMigrator creates a new artifact migrator
func NewArtifactMigrator(log logr.Logger) *ArtifactMigrator {
	return &ArtifactMigrator{
		log:         log,
		cache:       NewArtifactCache(),
		parallelism: 5, // 5 concurrent artifact downloads
	}
}

// AnalyzeArtifacts analyzes artifacts in ADO for migration planning
func (am *ArtifactMigrator) AnalyzeArtifacts(ctx context.Context, projectOrRepo string) (*ArtifactAnalysis, error) {
	am.log.Info("Analyzing artifacts", "project", projectOrRepo)

	analysis := &ArtifactAnalysis{
		ByType: make(map[string]int),
		ByFeed: make(map[string]int),
	}

	// Discover all feeds in the project
	feeds, err := am.adoClient.ListFeeds(ctx, projectOrRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to list feeds: %w", err)
	}

	for _, feed := range feeds {
		// Get packages in each feed
		packages, err := am.adoClient.ListPackages(ctx, projectOrRepo, feed.Name)
		if err != nil {
			am.log.Error(err, "Failed to list packages in feed", "feed", feed.Name)
			continue
		}

		analysis.ByFeed[feed.Name] = len(packages)

		for _, pkg := range packages {
			// Count by type
			analysis.ByType[pkg.PackageType]++

			// Get package versions for size calculation
			versions, err := am.adoClient.ListPackageVersions(ctx, projectOrRepo, feed.Name, pkg.ID)
			if err != nil {
				am.log.Error(err, "Failed to list package versions", "package", pkg.Name)
				continue
			}

			analysis.TotalArtifacts += len(versions)

			// Sum up sizes
			for _, version := range versions {
				analysis.TotalSizeBytes += version.SizeBytes
			}
		}
	}

	am.log.Info("Artifact analysis completed",
		"total_artifacts", analysis.TotalArtifacts,
		"total_size_mb", analysis.TotalSizeBytes/(1024*1024),
		"feeds", len(feeds))

	return analysis, nil
}

// MigrateArtifacts migrates artifacts from ADO to GitHub Packages
func (am *ArtifactMigrator) MigrateArtifacts(ctx *MigrationContext) error {
	am.log.Info("Starting artifact migration", "project", ctx.Migration.SourceRepo)

	project := ctx.Migration.SourceRepo

	// Step 1: Discover feeds
	feeds, err := am.adoClient.ListFeeds(ctx.Context, project)
	if err != nil {
		return fmt.Errorf("failed to list feeds: %w", err)
	}

	totalMigrated := 0
	totalFailed := 0

	// Step 2: Migrate each feed
	for _, feed := range feeds {
		am.log.Info("Migrating feed", "feed", feed.Name, "type", feed.FeedType)

		migrated, failed, err := am.migrateFeed(ctx, project, feed)
		if err != nil {
			am.log.Error(err, "Feed migration failed", "feed", feed.Name)
			totalFailed += failed
			continue
		}

		totalMigrated += migrated
		totalFailed += failed
	}

	am.log.Info("Artifact migration completed",
		"total_migrated", totalMigrated,
		"total_failed", totalFailed)

	if totalFailed > 0 {
		return fmt.Errorf("artifact migration completed with %d failures", totalFailed)
	}

	return nil
}

// migrateFeed migrates all packages in a feed
func (am *ArtifactMigrator) migrateFeed(ctx *MigrationContext, project string, feed *ADOFeed) (int, int, error) {
	// List packages in feed
	packages, err := am.adoClient.ListPackages(ctx.Context, project, feed.Name)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list packages: %w", err)
	}

	migrated := 0
	failed := 0

	// Migrate based on feed type
	switch feed.FeedType {
	case "nuget":
		m, f := am.migrateNuGetPackages(ctx, project, feed, packages)
		migrated, failed = m, f
	case "npm":
		m, f := am.migrateNpmPackages(ctx, project, feed, packages)
		migrated, failed = m, f
	case "maven":
		m, f := am.migrateMavenPackages(ctx, project, feed, packages)
		migrated, failed = m, f
	case "python":
		m, f := am.migratePythonPackages(ctx, project, feed, packages)
		migrated, failed = m, f
	case "universal":
		m, f := am.migrateUniversalPackages(ctx, project, feed, packages)
		migrated, failed = m, f
	default:
		am.log.Info("Unsupported feed type, skipping", "type", feed.FeedType)
		return 0, 0, nil
	}

	return migrated, failed, nil
}

// migrateNuGetPackages migrates NuGet packages to GitHub Packages
func (am *ArtifactMigrator) migrateNuGetPackages(ctx *MigrationContext, project string, feed *ADOFeed, packages []*ADOPackage) (int, int) {
	am.log.Info("Migrating NuGet packages", "count", len(packages))

	migrated := 0
	failed := 0

	var wg sync.WaitGroup
	sem := make(chan struct{}, am.parallelism)
	var mu sync.Mutex

	for _, pkg := range packages {
		wg.Add(1)
		go func(p *ADOPackage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Wait for rate limiter
			if err := ctx.RateLimiter.Wait(ctx.Context); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			// Migrate package
			if err := am.migrateNuGetPackage(ctx, project, feed.Name, p); err != nil {
				am.log.Error(err, "Failed to migrate NuGet package", "package", p.Name)
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			mu.Lock()
			migrated++
			mu.Unlock()
		}(pkg)
	}

	wg.Wait()

	am.log.Info("NuGet migration completed", "migrated", migrated, "failed", failed)
	return migrated, failed
}

// migrateNuGetPackage migrates a single NuGet package with all versions
func (am *ArtifactMigrator) migrateNuGetPackage(ctx *MigrationContext, project, feedName string, pkg *ADOPackage) error {
	// Get all versions
	versions, err := am.adoClient.ListPackageVersions(ctx.Context, project, feedName, pkg.ID)
	if err != nil {
		return fmt.Errorf("failed to list versions: %w", err)
	}

	for _, version := range versions {
		am.log.V(1).Info("Migrating NuGet package version",
			"package", pkg.Name,
			"version", version.Version)

		// Check cache
		cacheKey := fmt.Sprintf("nuget:%s:%s", pkg.Name, version.Version)
		if am.cache.Has(cacheKey) {
			am.log.V(2).Info("Package already migrated, skipping", "cache_key", cacheKey)
			continue
		}

		// Download package from ADO
		packageData, err := am.adoClient.DownloadNuGetPackage(ctx.Context, project, feedName, pkg.Name, version.Version)
		if err != nil {
			return fmt.Errorf("failed to download package: %w", err)
		}
		defer packageData.Close()

		// Upload to GitHub Packages
		err = am.githubClient.UploadNuGetPackage(ctx.Context, ctx.Migration.TargetRepo, pkg.Name, version.Version, packageData)
		if err != nil {
			return fmt.Errorf("failed to upload to GitHub: %w", err)
		}

		// Cache successful migration
		am.cache.Set(cacheKey)

		am.log.Info("Package version migrated successfully",
			"package", pkg.Name,
			"version", version.Version,
			"size_mb", version.SizeBytes/(1024*1024))
	}

	return nil
}

// migrateNpmPackages migrates npm packages to GitHub Packages
func (am *ArtifactMigrator) migrateNpmPackages(ctx *MigrationContext, project string, feed *ADOFeed, packages []*ADOPackage) (int, int) {
	am.log.Info("Migrating npm packages", "count", len(packages))

	migrated := 0
	failed := 0

	var wg sync.WaitGroup
	sem := make(chan struct{}, am.parallelism)
	var mu sync.Mutex

	for _, pkg := range packages {
		wg.Add(1)
		go func(p *ADOPackage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := ctx.RateLimiter.Wait(ctx.Context); err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			if err := am.migrateNpmPackage(ctx, project, feed.Name, p); err != nil {
				am.log.Error(err, "Failed to migrate npm package", "package", p.Name)
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}

			mu.Lock()
			migrated++
			mu.Unlock()
		}(pkg)
	}

	wg.Wait()

	am.log.Info("npm migration completed", "migrated", migrated, "failed", failed)
	return migrated, failed
}

// migrateNpmPackage migrates a single npm package with all versions
func (am *ArtifactMigrator) migrateNpmPackage(ctx *MigrationContext, project, feedName string, pkg *ADOPackage) error {
	versions, err := am.adoClient.ListPackageVersions(ctx.Context, project, feedName, pkg.ID)
	if err != nil {
		return fmt.Errorf("failed to list versions: %w", err)
	}

	for _, version := range versions {
		am.log.V(1).Info("Migrating npm package version",
			"package", pkg.Name,
			"version", version.Version)

		cacheKey := fmt.Sprintf("npm:%s:%s", pkg.Name, version.Version)
		if am.cache.Has(cacheKey) {
			continue
		}

		// Download from ADO
		packageData, err := am.adoClient.DownloadNpmPackage(ctx.Context, project, feedName, pkg.Name, version.Version)
		if err != nil {
			return fmt.Errorf("failed to download package: %w", err)
		}
		defer packageData.Close()

		// Upload to GitHub Packages (npm registry)
		err = am.githubClient.UploadNpmPackage(ctx.Context, ctx.Migration.TargetRepo, pkg.Name, version.Version, packageData)
		if err != nil {
			return fmt.Errorf("failed to upload to GitHub: %w", err)
		}

		am.cache.Set(cacheKey)

		am.log.Info("npm package version migrated",
			"package", pkg.Name,
			"version", version.Version)
	}

	return nil
}

// migrateMavenPackages migrates Maven artifacts to GitHub Packages
func (am *ArtifactMigrator) migrateMavenPackages(ctx *MigrationContext, project string, feed *ADOFeed, packages []*ADOPackage) (int, int) {
	am.log.Info("Migrating Maven artifacts", "count", len(packages))

	migrated := 0
	failed := 0

	for _, pkg := range packages {
		if err := ctx.RateLimiter.Wait(ctx.Context); err != nil {
			failed++
			continue
		}

		if err := am.migrateMavenPackage(ctx, project, feed.Name, pkg); err != nil {
			am.log.Error(err, "Failed to migrate Maven artifact", "artifact", pkg.Name)
			failed++
			continue
		}

		migrated++
	}

	am.log.Info("Maven migration completed", "migrated", migrated, "failed", failed)
	return migrated, failed
}

// migrateMavenPackage migrates a Maven artifact
func (am *ArtifactMigrator) migrateMavenPackage(ctx *MigrationContext, project, feedName string, pkg *ADOPackage) error {
	versions, err := am.adoClient.ListPackageVersions(ctx.Context, project, feedName, pkg.ID)
	if err != nil {
		return fmt.Errorf("failed to list versions: %w", err)
	}

	for _, version := range versions {
		cacheKey := fmt.Sprintf("maven:%s:%s", pkg.Name, version.Version)
		if am.cache.Has(cacheKey) {
			continue
		}

		// Maven artifacts have group:artifact:version structure
		parts := strings.Split(pkg.Name, ":")
		if len(parts) < 2 {
			return fmt.Errorf("invalid Maven artifact name: %s", pkg.Name)
		}

		groupId := parts[0]
		artifactId := parts[1]

		// Download from ADO
		artifactData, err := am.adoClient.DownloadMavenArtifact(ctx.Context, project, feedName, groupId, artifactId, version.Version)
		if err != nil {
			return fmt.Errorf("failed to download artifact: %w", err)
		}
		defer artifactData.Close()

		// Upload to GitHub Packages (Maven registry)
		err = am.githubClient.UploadMavenArtifact(ctx.Context, ctx.Migration.TargetRepo, groupId, artifactId, version.Version, artifactData)
		if err != nil {
			return fmt.Errorf("failed to upload to GitHub: %w", err)
		}

		am.cache.Set(cacheKey)

		am.log.Info("Maven artifact migrated",
			"artifact", pkg.Name,
			"version", version.Version)
	}

	return nil
}

// migratePythonPackages migrates Python packages to GitHub Packages
func (am *ArtifactMigrator) migratePythonPackages(ctx *MigrationContext, project string, feed *ADOFeed, packages []*ADOPackage) (int, int) {
	am.log.Info("Migrating Python packages", "count", len(packages))

	migrated := 0
	failed := 0

	for _, pkg := range packages {
		if err := ctx.RateLimiter.Wait(ctx.Context); err != nil {
			failed++
			continue
		}

		if err := am.migratePythonPackage(ctx, project, feed.Name, pkg); err != nil {
			am.log.Error(err, "Failed to migrate Python package", "package", pkg.Name)
			failed++
			continue
		}

		migrated++
	}

	am.log.Info("Python migration completed", "migrated", migrated, "failed", failed)
	return migrated, failed
}

// migratePythonPackage migrates a Python package
func (am *ArtifactMigrator) migratePythonPackage(ctx *MigrationContext, project, feedName string, pkg *ADOPackage) error {
	versions, err := am.adoClient.ListPackageVersions(ctx.Context, project, feedName, pkg.ID)
	if err != nil {
		return fmt.Errorf("failed to list versions: %w", err)
	}

	for _, version := range versions {
		cacheKey := fmt.Sprintf("python:%s:%s", pkg.Name, version.Version)
		if am.cache.Has(cacheKey) {
			continue
		}

		// Download wheel or sdist from ADO
		packageData, err := am.adoClient.DownloadPythonPackage(ctx.Context, project, feedName, pkg.Name, version.Version)
		if err != nil {
			return fmt.Errorf("failed to download package: %w", err)
		}
		defer packageData.Close()

		// Upload to GitHub Packages (PyPI compatible)
		err = am.githubClient.UploadPythonPackage(ctx.Context, ctx.Migration.TargetRepo, pkg.Name, version.Version, packageData)
		if err != nil {
			return fmt.Errorf("failed to upload to GitHub: %w", err)
		}

		am.cache.Set(cacheKey)

		am.log.Info("Python package migrated",
			"package", pkg.Name,
			"version", version.Version)
	}

	return nil
}

// migrateUniversalPackages migrates Universal Packages to GitHub Releases or Container Registry
func (am *ArtifactMigrator) migrateUniversalPackages(ctx *MigrationContext, project string, feed *ADOFeed, packages []*ADOPackage) (int, int) {
	am.log.Info("Migrating Universal Packages", "count", len(packages))

	migrated := 0
	failed := 0

	for _, pkg := range packages {
		if err := ctx.RateLimiter.Wait(ctx.Context); err != nil {
			failed++
			continue
		}

		if err := am.migrateUniversalPackage(ctx, project, feed.Name, pkg); err != nil {
			am.log.Error(err, "Failed to migrate Universal Package", "package", pkg.Name)
			failed++
			continue
		}

		migrated++
	}

	am.log.Info("Universal Package migration completed", "migrated", migrated, "failed", failed)
	return migrated, failed
}

// migrateUniversalPackage migrates a Universal Package
// Universal packages are migrated to GitHub Releases
func (am *ArtifactMigrator) migrateUniversalPackage(ctx *MigrationContext, project, feedName string, pkg *ADOPackage) error {
	versions, err := am.adoClient.ListPackageVersions(ctx.Context, project, feedName, pkg.ID)
	if err != nil {
		return fmt.Errorf("failed to list versions: %w", err)
	}

	for _, version := range versions {
		cacheKey := fmt.Sprintf("universal:%s:%s", pkg.Name, version.Version)
		if am.cache.Has(cacheKey) {
			continue
		}

		// Download from ADO
		packageData, err := am.adoClient.DownloadUniversalPackage(ctx.Context, project, feedName, pkg.Name, version.Version)
		if err != nil {
			return fmt.Errorf("failed to download package: %w", err)
		}
		defer packageData.Close()

		// Create GitHub Release with package as asset
		releaseTag := fmt.Sprintf("%s-v%s", pkg.Name, version.Version)
		assetName := fmt.Sprintf("%s-%s.zip", pkg.Name, version.Version)

		err = am.githubClient.CreateReleaseWithAsset(ctx.Context, ctx.Migration.TargetRepo, releaseTag, pkg.Name, assetName, packageData)
		if err != nil {
			return fmt.Errorf("failed to create GitHub release: %w", err)
		}

		am.cache.Set(cacheKey)

		am.log.Info("Universal Package migrated to GitHub Release",
			"package", pkg.Name,
			"version", version.Version,
			"release_tag", releaseTag)
	}

	return nil
}

// ArtifactCache provides simple in-memory caching for migrated artifacts
type ArtifactCache struct {
	cache map[string]time.Time
	mu    sync.RWMutex
}

func NewArtifactCache() *ArtifactCache {
	return &ArtifactCache{
		cache: make(map[string]time.Time),
	}
}

func (ac *ArtifactCache) Has(key string) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	_, exists := ac.cache[key]
	return exists
}

func (ac *ArtifactCache) Set(key string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.cache[key] = time.Now()
}

// ADO Artifacts Client types (placeholders - implement with actual ADO SDK)

type ADOArtifactsClient struct {
	// Implementation with Azure DevOps SDK
}

type ADOFeed struct {
	ID       string
	Name     string
	FeedType string // nuget, npm, maven, python, universal
}

type ADOPackage struct {
	ID          string
	Name        string
	PackageType string
}

type ADOPackageVersion struct {
	Version   string
	SizeBytes int64
}

func (c *ADOArtifactsClient) ListFeeds(ctx context.Context, project string) ([]*ADOFeed, error) {
	// Implement with Azure DevOps SDK
	// https://docs.microsoft.com/en-us/rest/api/azure/devops/artifacts/feed-management/get-feeds
	return nil, nil
}

func (c *ADOArtifactsClient) ListPackages(ctx context.Context, project, feedName string) ([]*ADOPackage, error) {
	// Implement with Azure DevOps SDK
	return nil, nil
}

func (c *ADOArtifactsClient) ListPackageVersions(ctx context.Context, project, feedName, packageID string) ([]*ADOPackageVersion, error) {
	// Implement with Azure DevOps SDK
	return nil, nil
}

func (c *ADOArtifactsClient) DownloadNuGetPackage(ctx context.Context, project, feedName, packageName, version string) (io.ReadCloser, error) {
	// Implement download logic
	return nil, nil
}

func (c *ADOArtifactsClient) DownloadNpmPackage(ctx context.Context, project, feedName, packageName, version string) (io.ReadCloser, error) {
	return nil, nil
}

func (c *ADOArtifactsClient) DownloadMavenArtifact(ctx context.Context, project, feedName, groupId, artifactId, version string) (io.ReadCloser, error) {
	return nil, nil
}

func (c *ADOArtifactsClient) DownloadPythonPackage(ctx context.Context, project, feedName, packageName, version string) (io.ReadCloser, error) {
	return nil, nil
}

func (c *ADOArtifactsClient) DownloadUniversalPackage(ctx context.Context, project, feedName, packageName, version string) (io.ReadCloser, error) {
	return nil, nil
}

// GitHub Packages Client types (placeholders - implement with GitHub SDK)

type GitHubPackagesClient struct {
	// Implementation with GitHub SDK
}

func (c *GitHubPackagesClient) UploadNuGetPackage(ctx context.Context, repo, packageName, version string, data io.Reader) error {
	// Implement with GitHub Packages API
	// https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-nuget-registry
	return nil
}

func (c *GitHubPackagesClient) UploadNpmPackage(ctx context.Context, repo, packageName, version string, data io.Reader) error {
	// Implement with npm registry API
	// https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry
	return nil
}

func (c *GitHubPackagesClient) UploadMavenArtifact(ctx context.Context, repo, groupId, artifactId, version string, data io.Reader) error {
	// Implement with Maven registry API
	// https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-apache-maven-registry
	return nil
}

func (c *GitHubPackagesClient) UploadPythonPackage(ctx context.Context, repo, packageName, version string, data io.Reader) error {
	// Implement with PyPI-compatible API
	return nil
}

func (c *GitHubPackagesClient) CreateReleaseWithAsset(ctx context.Context, repo, tag, name, assetName string, data io.Reader) error {
	// Implement with GitHub Releases API
	// https://docs.github.com/en/rest/releases/releases
	return nil
}

// Helper types

type PerformanceMonitor struct {
	log logr.Logger
}

func NewPerformanceMonitor(log logr.Logger) *PerformanceMonitor {
	return &PerformanceMonitor{log: log}
}

type Monitor struct {
	startTime time.Time
	name      string
}

func (pm *PerformanceMonitor) Start(name string) *Monitor {
	return &Monitor{
		startTime: time.Now(),
		name:      name,
	}
}

func (m *Monitor) End() {
	// Log performance
}

func (m *Monitor) Duration() time.Duration {
	return time.Since(m.startTime)
}

type MigrationOptimizer struct {
	log logr.Logger
}

func NewMigrationOptimizer(log logr.Logger) *MigrationOptimizer {
	return &MigrationOptimizer{log: log}
}

func (mo *MigrationOptimizer) CalculateOptimalBatchSize(pm *PlannedMigration) int {
	// Calculate based on complexity and size
	if pm.Complexity > 70 {
		return 5 // Small batches for complex migrations
	} else if pm.Complexity > 40 {
		return 10
	}
	return 20 // Larger batches for simple migrations
}

type DependencyGraph struct {
	nodes map[string]*DependencyNode
	mu    sync.RWMutex
}

type DependencyNode struct {
	Name         string
	Dependencies []string
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*DependencyNode),
	}
}

func (dg *DependencyGraph) BuildFromMigrations(migrations []*PlannedMigration) error {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	for _, pm := range migrations {
		dg.nodes[pm.Name] = &DependencyNode{
			Name:         pm.Name,
			Dependencies: pm.Dependencies,
		}
	}
	return nil
}

func (dg *DependencyGraph) DetectCircularDependencies() []string {
	// Implement cycle detection algorithm
	return nil
}

func (dg *DependencyGraph) TopologicalSort() []string {
	// Implement topological sort for dependency order
	result := make([]string, 0, len(dg.nodes))
	for name := range dg.nodes {
		result = append(result, name)
	}
	return result
}
