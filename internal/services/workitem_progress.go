package services

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	migrationv1 "github.com/tesserix/reposhift/api/v1"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MigrationProgressUpdate represents a progress update during migration
type MigrationProgressUpdate struct {
	CurrentStep     string
	ItemsDiscovered int
	ItemsMigrated   int
	ItemsFailed     int
	ItemsSkipped    int
	Percentage      int
	CurrentBatch    int
	TotalBatches    int
}

// ProgressCallback is a function that receives progress updates
type ProgressCallback func(update MigrationProgressUpdate)

// MigrateWorkItemsWithProgress is an improved version that sends incremental progress updates
// This function wraps the existing MigrateWorkItems but adds progress callback support
// RESUME SUPPORT: Pass alreadyMigrated to skip items that were successfully migrated before pod restart
func (s *WorkItemService) MigrateWorkItemsWithProgress(
	ctx context.Context,
	organization string,
	project string,
	team string,
	targetOwner string,
	targetRepo string,
	adoToken string,
	githubToken string,
	settings migrationv1.WorkItemMigrationSettings,
	filters migrationv1.WorkItemFilters,
	progressCallback ProgressCallback,
	// GitHub auth config for token refresh support
	githubAuth *migrationv1.GitHubAuthConfig,
	// Kubernetes client for reading secrets (if using GitHub App auth)
	kubeClient client.Client,
	namespace string,
	// Already-migrated items (for resume after pod restart)
	alreadyMigrated []migrationv1.MigratedWorkItem,
) ([]MigratedWorkItem, error) {

	// Apply defaults
	applyDefaultsWithOverrides(&settings, &filters)

	// If team is specified and no area paths are provided, use the team as area path
	if team != "" && (filters.AreaPaths == nil || len(filters.AreaPaths) == 0) {
		filters.AreaPaths = []string{project}
	}

	// Convert GitHub auth from CRD type to service type (with actual values)
	serviceGitHubAuth := GitHubAuthConfig_Service{}

	// If using GitHub App auth, read secrets to get actual credentials
	if githubAuth != nil && githubAuth.AppAuth != nil && kubeClient != nil {
		appIDStr, err := s.getSecretValue(ctx, kubeClient, namespace, &githubAuth.AppAuth.AppIdRef)
		if err != nil {
			return nil, fmt.Errorf("failed to read GitHub App ID: %w", err)
		}

		installationIDStr, err := s.getSecretValue(ctx, kubeClient, namespace, &githubAuth.AppAuth.InstallationIdRef)
		if err != nil {
			return nil, fmt.Errorf("failed to read GitHub Installation ID: %w", err)
		}

		privateKey, err := s.getSecretValue(ctx, kubeClient, namespace, &githubAuth.AppAuth.PrivateKeyRef)
		if err != nil {
			return nil, fmt.Errorf("failed to read GitHub App private key: %w", err)
		}

		appID, err := strconv.ParseInt(appIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub App ID '%s': %w", appIDStr, err)
		}

		installationID, err := strconv.ParseInt(installationIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub Installation ID '%s': %w", installationIDStr, err)
		}

		serviceGitHubAuth.AppAuth = &GitHubAppAuthConfig_Service{
			AppID:          appID,
			InstallationID: installationID,
			PrivateKey:     privateKey,
		}
	} else {
		// Using PAT auth
		serviceGitHubAuth.Token = githubToken
	}

	// Convert CRD types to service types
	request := WorkItemMigrationRequest{
		SourceOrganization: organization,
		SourceProject:      project,
		TargetOwner:        targetOwner,
		TargetRepository:   targetRepo,
		Settings: WorkItemMigrationSettings{
			TypeMapping:           settings.TypeMapping,
			StateMapping:          settings.StateMapping,
			IncludeHistory:        settings.IncludeHistory != nil && *settings.IncludeHistory,
			IncludeAttachments:    settings.IncludeAttachments != nil && *settings.IncludeAttachments,
			PreserveRelationships: settings.PreserveRelationships != nil && *settings.PreserveRelationships,
			IncludeTags:           settings.IncludeTags != nil && *settings.IncludeTags,
			CombineComments:       settings.CombineComments == nil || *settings.CombineComments, // Default true for performance
			BatchSize:             settings.BatchSize,
			AdoToken:              adoToken,
			GitHubToken:           githubToken,
		},
		Filters: WorkItemFilters{
			Types:          filters.Types,
			States:         filters.States,
			AreaPaths:      filters.AreaPaths,
			IterationPaths: filters.IterationPaths,
			Tags:           filters.Tags,
			AssignedTo:     filters.AssignedTo,
			WIQLQuery:      filters.WIQLQuery,
		},
		GitHubAuth: serviceGitHubAuth, // Set service-layer auth with actual values
	}

	// Convert date range if present
	if filters.DateRange != nil {
		request.Filters.DateRange = &DateRange{}
		if filters.DateRange.Start != nil {
			startTime := filters.DateRange.Start.Time
			request.Filters.DateRange.Start = &startTime
		}
		if filters.DateRange.End != nil {
			endTime := filters.DateRange.End.Time
			request.Filters.DateRange.End = &endTime
		}
	}

	// Call the enhanced migration function with progress updates
	return s.migrateWorkItemsWithProgressInternal(ctx, request, settings, progressCallback, alreadyMigrated)
}

// getSecretValue reads a secret value from Kubernetes
func (s *WorkItemService) getSecretValue(ctx context.Context, kubeClient client.Client, namespace string, ref *migrationv1.SecretReference) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("secret reference is nil")
	}

	var secret corev1.Secret
	err := kubeClient.Get(ctx, client.ObjectKey{
		Name:      ref.Name,
		Namespace: namespace,
	}, &secret)

	if err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", ref.Name, err)
	}

	value, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %s not found in secret %s", ref.Key, ref.Name)
	}

	return string(value), nil
}

// applyDefaultsWithOverrides applies defaults and respects user-configured settings
func applyDefaultsWithOverrides(settings *migrationv1.WorkItemMigrationSettings, filters *migrationv1.WorkItemFilters) {
	// Apply default type mapping if not specified
	if settings.TypeMapping == nil || len(settings.TypeMapping) == 0 {
		settings.TypeMapping = getDefaultTypeMapping()
	}

	// Apply default state mapping if not specified
	if settings.StateMapping == nil || len(settings.StateMapping) == 0 {
		settings.StateMapping = getDefaultStateMapping()
	}

	// Apply default batch size if not specified
	if settings.BatchSize == 0 {
		settings.BatchSize = 80
	}

	// Apply default batch delay if not specified
	// Default to 60 seconds (1 minute) instead of 30 minutes!
	if settings.BatchDelaySeconds == 0 {
		settings.BatchDelaySeconds = 60 // 1 minute default
	}

	// Apply default timeout if not specified
	if settings.TimeoutMinutes == 0 {
		settings.TimeoutMinutes = 360 // 6 hours default
	}

	// Apply default progress update interval
	if settings.ProgressUpdateIntervalSeconds == 0 {
		settings.ProgressUpdateIntervalSeconds = 30 // 30 seconds default
	}

	// Enable defaults for common options if not explicitly set
	// With pointer bools, nil means "not set", so we can distinguish from explicit false
	if settings.IncludeHistory == nil {
		trueVal := true
		settings.IncludeHistory = &trueVal
	}
	if settings.IncludeAttachments == nil {
		trueVal := true
		settings.IncludeAttachments = &trueVal
	}
	if settings.PreserveRelationships == nil {
		trueVal := true
		settings.PreserveRelationships = &trueVal
	}
	if settings.IncludeTags == nil {
		trueVal := true
		settings.IncludeTags = &trueVal
	}
	if settings.CombineComments == nil {
		trueVal := true
		settings.CombineComments = &trueVal // Default to true for better performance and rate limit avoidance
	}
}

// migrateWorkItemsWithProgressInternal performs the migration with progress updates
func (s *WorkItemService) migrateWorkItemsWithProgressInternal(
	ctx context.Context,
	request WorkItemMigrationRequest,
	settings migrationv1.WorkItemMigrationSettings,
	progressCallback ProgressCallback,
	alreadyMigrated []migrationv1.MigratedWorkItem,
) ([]MigratedWorkItem, error) {

	result := []MigratedWorkItem{}

	// Helper function to send progress updates
	sendProgress := func(step string, discovered, migrated, failed, skipped, percentage int, currentBatch, totalBatches int) {
		if progressCallback != nil {
			progressCallback(MigrationProgressUpdate{
				CurrentStep:     step,
				ItemsDiscovered: discovered,
				ItemsMigrated:   migrated,
				ItemsFailed:     failed,
				ItemsSkipped:    skipped,
				Percentage:      percentage,
				CurrentBatch:    currentBatch,
				TotalBatches:    totalBatches,
			})
		}
	}

	// 1. Send initial progress
	sendProgress("Connecting to Azure DevOps", 0, 0, 0, 0, 0, 0, 0)

	// 2. Get Azure DevOps connection
	if request.Settings.AdoToken == "" {
		return nil, fmt.Errorf("Azure DevOps token is required")
	}

	adoConnection, err := s.getAzureDevOpsConnection(ctx, request.SourceOrganization, request.Settings.AdoToken)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Azure DevOps: %w", err)
	}

	// 3. Send progress for GitHub connection
	sendProgress("Connecting to GitHub", 0, 0, 0, 0, 5, 0, 0)

	// 4. Get GitHub client (supports automatic token refresh for GitHub App auth)
	githubClient, err := s.getGitHubClient(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to GitHub: %w", err)
	}

	// 5. Verify repository exists
	if request.TargetRepository == "" {
		return nil, fmt.Errorf("target repository is required")
	}

	repoExists, err := s.githubService.CheckRepositoryExists(ctx, request.Settings.GitHubToken, request.TargetOwner, request.TargetRepository)
	if err != nil {
		return nil, fmt.Errorf("failed to check repository: %w", err)
	}

	if !repoExists {
		return nil, fmt.Errorf("repository %s/%s does not exist", request.TargetOwner, request.TargetRepository)
	}

	fmt.Printf("✅ Verified repository %s/%s exists\n", request.TargetOwner, request.TargetRepository)

	// 6. Send progress for querying work items
	sendProgress("Discovering Work Items", 0, 0, 0, 0, 10, 0, 0)

	// 7. Build WIQL query
	wiqlQuery := s.buildWIQLQuery(request)
	fmt.Printf("📝 WIQL Query: %s\n", wiqlQuery)

	// 8. Query work items
	workItems, err := s.queryWorkItems(ctx, adoConnection, request.SourceProject, wiqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query work items: %w", err)
	}

	itemsDiscovered := len(workItems)
	fmt.Printf("📊 Discovered %d work items matching filters\n", itemsDiscovered)

	// RESUME LOGIC: Filter out already-migrated work items
	if len(alreadyMigrated) > 0 {
		// Build a map of already-migrated source IDs for O(1) lookup
		migratedMap := make(map[int]bool)
		for _, item := range alreadyMigrated {
			migratedMap[item.SourceID] = true
		}

		// Filter work items to exclude already-migrated ones
		var remainingWorkItems []*workitemtracking.WorkItem
		for _, workItem := range workItems {
			if workItem.Id != nil && !migratedMap[*workItem.Id] {
				remainingWorkItems = append(remainingWorkItems, workItem)
			}
		}

		fmt.Printf("🔄 RESUME MODE: Skipping %d already-migrated items, processing %d remaining items\n",
			len(workItems)-len(remainingWorkItems), len(remainingWorkItems))

		workItems = remainingWorkItems
	}

	// Send discovery complete progress
	sendProgress("Work Items Discovered", itemsDiscovered, len(alreadyMigrated), 0, 0, 15, 0, 0)

	if itemsDiscovered == 0 {
		sendProgress("Completed - No Items Found", 0, 0, 0, 0, 100, 0, 0)
		return result, nil
	}

	// 9. Initialize PostgreSQL tracking (if enabled)
	var db *DatabaseService
	var migrationID string
	if os.Getenv("POSTGRES_ENABLED") == "true" {
		db, err = NewDatabaseService(ctx)
		if err != nil {
			fmt.Printf("⚠️  PostgreSQL not available, continuing without tracking: %v\n", err)
		} else {
			defer db.Close()

			migrationRecord := &MigrationRecord{
				MigrationName:      fmt.Sprintf("%s-%s", request.SourceProject, request.TargetRepository),
				MigrationNamespace: "ado-migration-operator",
				SourceOrganization: request.SourceOrganization,
				SourceProject:      request.SourceProject,
				TargetOwner:        request.TargetOwner,
				TargetRepository:   request.TargetRepository,
				Status:             "running",
				TotalItems:         itemsDiscovered,
			}

			migrationID, err = db.InitializeMigration(ctx, migrationRecord)
			if err != nil {
				fmt.Printf("⚠️  Failed to initialize migration tracking: %v\n", err)
				db = nil
			}
		}
	}

	// 10. Process work items in batches with configurable delays
	batchSize := request.Settings.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	// Get batch delay from settings (already has default applied)
	batchDelaySeconds := settings.BatchDelaySeconds
	if batchDelaySeconds <= 0 {
		batchDelaySeconds = 60 // Fallback to 1 minute
	}

	// Allow environment variable override for testing
	if delayEnv := os.Getenv("WORKITEM_BATCH_DELAY_SECONDS"); delayEnv != "" {
		if delay, err := strconv.Atoi(delayEnv); err == nil && delay > 0 {
			batchDelaySeconds = delay
			fmt.Printf("⚙️  Using batch delay from env: %d seconds\n", batchDelaySeconds)
		}
	}

	// Get per-item delay from settings to prevent GitHub rate limits
	perItemDelayMs := settings.PerItemDelayMs
	if perItemDelayMs <= 0 {
		perItemDelayMs = 1000 // Default: 1000ms between items (safer for migrations with history)
	}

	// Allow environment variable override for testing
	if delayEnv := os.Getenv("WORKITEM_PER_ITEM_DELAY_MS"); delayEnv != "" {
		if delay, err := strconv.Atoi(delayEnv); err == nil && delay >= 0 {
			perItemDelayMs = delay
			fmt.Printf("⚙️  Using per-item delay from env: %dms\n", perItemDelayMs)
		}
	}

	fmt.Printf("⚙️  Batch configuration: size=%d, delay=%ds, per-item delay=%dms\n", batchSize, batchDelaySeconds, perItemDelayMs)

	// Calculate total batches
	totalBatches := (itemsDiscovered + batchSize - 1) / batchSize

	// Track overall progress (start with already-migrated count for resume)
	itemsMigrated := len(alreadyMigrated)
	itemsFailed := 0
	itemsSkipped := 0

	// Pre-populate result with already-migrated items
	for _, item := range alreadyMigrated {
		result = append(result, MigratedWorkItem{
			SourceID:          item.SourceID,
			SourceType:        item.SourceType,
			SourceTitle:       item.SourceTitle,
			TargetIssueNumber: item.TargetIssueNumber,
			TargetURL:         item.TargetURL,
			MigratedAt:        item.MigratedAt.Time,
		})
	}

	// Map for relationship preservation
	adoToGithubMap := make(map[int]int)

	// Process batches
	batchNum := 0
	for i := 0; i < len(workItems); i += batchSize {
		batchNum++
		end := i + batchSize
		if end > len(workItems) {
			end = len(workItems)
		}
		batch := workItems[i:end]

		// Send batch start progress
		percentage := 15 + int(float64(i)/float64(itemsDiscovered)*70) // 15-85% for migration
		sendProgress(
			fmt.Sprintf("Migrating Batch %d/%d", batchNum, totalBatches),
			itemsDiscovered,
			itemsMigrated,
			itemsFailed,
			itemsSkipped,
			percentage,
			batchNum,
			totalBatches,
		)

		fmt.Printf("\n🔄 Processing batch %d/%d (%d-%d of %d items)\n", batchNum, totalBatches, i+1, end, itemsDiscovered)

		// Refresh GitHub client to ensure fresh tokens (especially important for GitHub App auth)
		// GitHub App tokens expire after 1 hour, so refreshing at each batch (every ~10 seconds)
		// ensures we always have valid tokens for long-running migrations
		githubClient, err = s.getGitHubClient(ctx, request)
		if err != nil {
			return result, fmt.Errorf("failed to refresh GitHub client for batch %d: %w", batchNum, err)
		}

		// Process each work item in batch
		for _, wi := range batch {
			if wi.Id == nil {
				continue
			}

			// Check if already migrated
			if db != nil && migrationID != "" {
				isMigrated, existingRecord, err := db.IsWorkItemMigrated(ctx, migrationID, *wi.Id)
				if err != nil {
					fmt.Printf("⚠️  Failed to check migration status for work item %d: %v\n", *wi.Id, err)
				} else if isMigrated {
					itemsSkipped++
					fmt.Printf("⏭️  Skipping work item %d (already migrated to issue #%d)\n", *wi.Id, *existingRecord.GithubIssueNumber)
					if existingRecord.GithubIssueNumber != nil {
						adoToGithubMap[*wi.Id] = *existingRecord.GithubIssueNumber
					}
					continue
				}
			}

			// Migrate the work item
			migratedItem, err := s.migrateWorkItem(ctx, adoConnection, githubClient, wi, request, adoToGithubMap)
			if err != nil {
				// Handle rate limits IMMEDIATELY before marking as failed
				if isRateLimitError(err) {
					fmt.Printf("⏸️  Rate limit detected for work item %d\n", *wi.Id)

					resetTime := extractRateLimitResetTime(err)
					retryAfter := extractRetryAfterSeconds(err)

					// Record rate limit event
					if db != nil && migrationID != "" {
						db.RecordRateLimitEvent(ctx, migrationID, "github", "secondary", resetTime, retryAfter)
					}

					// Wait for rate limit to reset
					if resetTime != nil && resetTime.After(time.Now()) {
						waitDuration := time.Until(*resetTime)
						fmt.Printf("⏳ Waiting %v for rate limit to reset (until %v)...\n", waitDuration, resetTime.Format("15:04:05"))
						time.Sleep(waitDuration)
						fmt.Println("✅ Rate limit period over, resuming migration...")
					} else {
						// Default wait if we can't parse reset time
						fmt.Println("⏳ Waiting 60 seconds before retrying...")
						time.Sleep(60 * time.Second)
						fmt.Println("✅ Wait complete, resuming migration...")
					}

					// Retry the work item after waiting
					fmt.Printf("🔄 Retrying work item %d after rate limit wait...\n", *wi.Id)
					migratedItem, err = s.migrateWorkItem(ctx, adoConnection, githubClient, wi, request, adoToGithubMap)
					if err != nil {
						// Still failed after retry
						itemsFailed++
						fmt.Printf("❌ Failed to migrate work item %d after retry: %v\n", *wi.Id, err)
						if db != nil && migrationID != "" {
							db.RecordWorkItemFailure(ctx, migrationID, *wi.Id, err.Error())
						}
						continue
					}
					// Retry succeeded, fall through to success handling
				} else {
					// Non-rate-limit error
					itemsFailed++
					fmt.Printf("❌ Failed to migrate work item %d: %v\n", *wi.Id, err)
					if db != nil && migrationID != "" {
						db.RecordWorkItemFailure(ctx, migrationID, *wi.Id, err.Error())
					}
					continue
				}
			}

			// Success
			result = append(result, *migratedItem)
			itemsMigrated++
			adoToGithubMap[*wi.Id] = migratedItem.TargetIssueNumber

			// Record success in database
			if db != nil && migrationID != "" {
				workItemRecord := &WorkItemMigrationRecord{
					MigrationID:       migrationID,
					AdoWorkItemID:     *wi.Id,
					AdoWorkItemType:   migratedItem.SourceType,
					AdoWorkItemTitle:  migratedItem.SourceTitle,
					GithubIssueNumber: &migratedItem.TargetIssueNumber,
					GithubIssueURL:    &migratedItem.TargetURL,
					Status:            "success",
				}
				if err := db.RecordWorkItemMigration(ctx, workItemRecord); err != nil {
					fmt.Printf("⚠️  Failed to record migration: %v\n", err)
				}

				if err := db.UpdateMigrationProgress(ctx, migrationID, itemsMigrated, itemsFailed, itemsSkipped); err != nil {
					fmt.Printf("⚠️  Failed to update progress: %v\n", err)
				}
			}

			fmt.Printf("✅ Migrated work item %d to issue #%d (%d/%d)\n", *wi.Id, migratedItem.TargetIssueNumber, itemsMigrated+itemsSkipped, itemsDiscovered)

			// Apply per-item delay to prevent GitHub rate limiting
			if perItemDelayMs > 0 {
				time.Sleep(time.Duration(perItemDelayMs) * time.Millisecond)
			}
		}

		// Send batch complete progress
		percentage = 15 + int(float64(end)/float64(itemsDiscovered)*70)
		sendProgress(
			fmt.Sprintf("Completed Batch %d/%d", batchNum, totalBatches),
			itemsDiscovered,
			itemsMigrated,
			itemsFailed,
			itemsSkipped,
			percentage,
			batchNum,
			totalBatches,
		)

		// Add delay between batches (except for last batch)
		if end < len(workItems) && batchDelaySeconds > 0 {
			fmt.Printf("\n⏸️  Batch complete. Waiting %d seconds before next batch...\n", batchDelaySeconds)
			fmt.Printf("📊 Progress: %d migrated, %d skipped, %d failed out of %d total\n",
				itemsMigrated, itemsSkipped, itemsFailed, itemsDiscovered)

			// Sleep with context cancellation support
			sleepDuration := time.Duration(batchDelaySeconds) * time.Second
			timer := time.NewTimer(sleepDuration)
			defer timer.Stop()

			select {
			case <-timer.C:
				fmt.Println("✅ Wait complete, resuming migration...")
			case <-ctx.Done():
				fmt.Println("⚠️  Migration cancelled during batch delay")
				return result, ctx.Err()
			}
		}
	}

	// 11. Update relationships if requested
	if request.Settings.PreserveRelationships && len(result) > 0 {
		sendProgress("Updating Relationships", itemsDiscovered, itemsMigrated, itemsFailed, itemsSkipped, 90, totalBatches, totalBatches)
		if err := s.updateRelationships(ctx, adoConnection, githubClient, request, result, adoToGithubMap); err != nil {
			fmt.Printf("⚠️  Failed to update some relationships: %v\n", err)
		}
	}

	// 12. Mark migration complete in database
	if db != nil && migrationID != "" {
		status := "completed"
		errorMsg := ""

		if itemsFailed > 0 && itemsMigrated == 0 {
			status = "failed"
			errorMsg = fmt.Sprintf("All %d items failed to migrate", itemsFailed)
		} else if itemsFailed > 0 {
			status = "partial"
			errorMsg = fmt.Sprintf("%d out of %d items failed", itemsFailed, itemsDiscovered)
		}

		if err := db.CompleteMigration(ctx, migrationID, status, errorMsg); err != nil {
			fmt.Printf("⚠️  Failed to mark migration complete: %v\n", err)
		}
	}

	// 13. Send final progress
	sendProgress("Migration Complete", itemsDiscovered, itemsMigrated, itemsFailed, itemsSkipped, 100, totalBatches, totalBatches)

	return result, nil
}
