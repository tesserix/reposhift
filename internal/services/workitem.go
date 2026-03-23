package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v84/github"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// WorkItemService handles work item migration operations
type WorkItemService struct {
	adoService    *AzureDevOpsService
	githubService *GitHubService
}

// GitHubAuthConfig_Service represents GitHub authentication with actual values (not secret refs)
// This is the service-layer equivalent of migrationv1.GitHubAuthConfig
type GitHubAuthConfig_Service struct {
	// PAT token (if using PAT auth)
	Token string

	// GitHub App authentication (if using App auth)
	AppAuth *GitHubAppAuthConfig_Service
}

// GitHubAppAuthConfig_Service contains actual GitHub App credentials
type GitHubAppAuthConfig_Service struct {
	AppID          int64
	InstallationID int64
	PrivateKey     string
}

// NewWorkItemService creates a new work item service
func NewWorkItemService() *WorkItemService {
	return &WorkItemService{
		adoService:    NewAzureDevOpsService(),
		githubService: NewGitHubService(),
	}
}

// WorkItemMigrationRequest represents a work item migration request
type WorkItemMigrationRequest struct {
	SourceOrganization string                    `json:"sourceOrganization"`
	SourceProject      string                    `json:"sourceProject"`
	TargetOwner        string                    `json:"targetOwner"`
	TargetRepository   string                    `json:"targetRepository"`
	AzureAuth          migrationv1.AdoAuthConfig `json:"azureAuth"`
	GitHubAuth         GitHubAuthConfig_Service  `json:"githubAuth"` // Service-layer type with actual values
	Settings           WorkItemMigrationSettings `json:"settings"`
	Filters            WorkItemFilters           `json:"filters"`
}

// WorkItemMigrationSettings represents work item migration settings
type WorkItemMigrationSettings struct {
	TypeMapping           map[string]string `json:"typeMapping,omitempty"`
	StateMapping          map[string]string `json:"stateMapping,omitempty"`
	IncludeHistory        bool              `json:"includeHistory"`
	IncludeAttachments    bool              `json:"includeAttachments"`
	PreserveRelationships bool              `json:"preserveRelationships"`
	IncludeTags           bool              `json:"includeTags"`
	CombineComments       bool              `json:"combineComments"` // Combine all comments into one block
	BatchSize             int               `json:"batchSize"`
	AdoToken              string            `json:"-"` // Don't serialize tokens
	GitHubToken           string            `json:"-"` // Don't serialize tokens
}

// WorkItemFilters represents filters for work item migration
type WorkItemFilters struct {
	Types          []string   `json:"types,omitempty"`
	States         []string   `json:"states,omitempty"`
	AreaPaths      []string   `json:"areaPaths,omitempty"`
	IterationPaths []string   `json:"iterationPaths,omitempty"`
	Tags           []string   `json:"tags,omitempty"`
	AssignedTo     []string   `json:"assignedTo,omitempty"`
	DateRange      *DateRange `json:"dateRange,omitempty"`
	WIQLQuery      string     `json:"wiqlQuery,omitempty"`
}

// DateRange represents a date range filter
type DateRange struct {
	Start *time.Time `json:"start,omitempty"`
	End   *time.Time `json:"end,omitempty"`
}

// WorkItemMigrationResult represents the result of a work item migration
type WorkItemMigrationResult struct {
	Success         bool               `json:"success"`
	Message         string             `json:"message"`
	Error           string             `json:"error,omitempty"`
	ItemsDiscovered int                `json:"itemsDiscovered"`
	ItemsMigrated   int                `json:"itemsMigrated"`
	ItemsFailed     int                `json:"itemsFailed"`
	ItemsSkipped    int                `json:"itemsSkipped"`
	MigratedItems   []MigratedWorkItem `json:"migratedItems"`
}

// MigratedWorkItem represents a successfully migrated work item
type MigratedWorkItem struct {
	SourceID          int       `json:"sourceId"`
	SourceType        string    `json:"sourceType"`
	SourceTitle       string    `json:"sourceTitle"`
	SourceState       string    `json:"sourceState"`
	TargetIssueNumber int       `json:"targetIssueNumber"`
	TargetURL         string    `json:"targetUrl"`
	MigratedAt        time.Time `json:"migratedAt"`
}

// MigrateWorkItems migrates work items from Azure DevOps to GitHub Issues
func (s *WorkItemService) MigrateWorkItems(ctx context.Context, request WorkItemMigrationRequest) (*WorkItemMigrationResult, error) {
	result := &WorkItemMigrationResult{
		MigratedItems: make([]MigratedWorkItem, 0),
	}

	// 1. Get Azure DevOps connection (token should be passed from controller)
	// Note: This service expects tokens to be provided, not retrieved from secrets
	// Token retrieval from secrets is handled by the controller
	adoToken := request.Settings.AdoToken
	if adoToken == "" {
		result.Error = "Azure DevOps token not provided"
		return result, fmt.Errorf("Azure DevOps token is required")
	}

	adoConnection, err := s.getAzureDevOpsConnection(ctx, request.SourceOrganization, adoToken)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to connect to Azure DevOps: %v", err)
		return result, err
	}

	// 2. Get GitHub client (supports automatic token refresh for GitHub App auth)
	githubClient, err := s.getGitHubClient(ctx, request)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to connect to GitHub: %v", err)
		return result, err
	}

	// 2.5. Verify that target repository exists
	// GitHub Issues require a repository - if not specified, migration cannot proceed
	if request.TargetRepository == "" {
		result.Error = "Target repository is required for work item migration. GitHub Issues must be created in a repository."
		return result, fmt.Errorf("target repository is required - GitHub Issues cannot be created without a repository")
	}

	// For GitHub App auth, we already have a client so we know it's authenticated
	// For PAT auth, check if repository exists
	if request.Settings.GitHubToken != "" {
		repoExists, err := s.githubService.CheckRepositoryExists(ctx, request.Settings.GitHubToken, request.TargetOwner, request.TargetRepository)
		if err != nil {
			result.Error = fmt.Sprintf("Failed to check if repository exists: %v", err)
			return result, err
		}

		if !repoExists {
			result.Error = fmt.Sprintf("Target repository %s/%s does not exist. Please create it first or specify an existing repository.", request.TargetOwner, request.TargetRepository)
			return result, fmt.Errorf("target repository %s/%s does not exist - work items migration requires an existing repository", request.TargetOwner, request.TargetRepository)
		}
	}

	fmt.Printf("Verified repository %s/%s exists, proceeding with migration...\n", request.TargetOwner, request.TargetRepository)

	// 3. Build WIQL query from filters
	wiqlQuery := s.buildWIQLQuery(request)

	// 4. Query work items from ADO
	workItems, err := s.queryWorkItems(ctx, adoConnection, request.SourceProject, wiqlQuery)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to query work items: %v", err)
		return result, err
	}

	result.ItemsDiscovered = len(workItems)
	if result.ItemsDiscovered == 0 {
		result.Success = true
		result.Message = "No work items found matching the filters"
		return result, nil
	}

	// 5. Initialize PostgreSQL tracking (if enabled)
	var db *DatabaseService
	var migrationID string
	if os.Getenv("POSTGRES_ENABLED") == "true" {
		db, err = NewDatabaseService(ctx)
		if err != nil {
			fmt.Printf("⚠️  PostgreSQL not available, continuing without tracking: %v\n", err)
		} else {
			defer db.Close()

			// Initialize migration record
			migrationRecord := &MigrationRecord{
				MigrationName:      fmt.Sprintf("%s-%s", request.SourceProject, request.TargetRepository),
				MigrationNamespace: "ado-migration-operator", // TODO: Get from context
				SourceOrganization: request.SourceOrganization,
				SourceProject:      request.SourceProject,
				TargetOwner:        request.TargetOwner,
				TargetRepository:   request.TargetRepository,
				Status:             "running",
				TotalItems:         len(workItems),
			}

			migrationID, err = db.InitializeMigration(ctx, migrationRecord)
			if err != nil {
				fmt.Printf("⚠️  Failed to initialize migration tracking: %v\n", err)
				db = nil // Disable DB tracking for this migration
			}
		}
	}

	// 6. Process work items in batches with tracking
	// OPTIMIZATION (Task 1.2): Increased default batch size from 10 to 100 for better throughput
	batchSize := request.Settings.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	// OPTIMIZATION (Task 1.1): Removed proactive batch delays - now only delays on actual rate limit errors
	// Legacy code used 30-minute delays which caused 1000 items to take 50+ hours
	// Now we rely on GitHub's rate limit headers and only wait when necessary
	batchDelayMinutes := 0 // Disabled by default
	if delayEnv := os.Getenv("BATCH_DELAY_MINUTES"); delayEnv != "" {
		if delay, err := time.ParseDuration(delayEnv + "m"); err == nil {
			batchDelayMinutes = int(delay.Minutes())
		}
	}

	// Create a map to track ADO ID to GitHub issue number for relationship preservation
	adoToGithubMap := make(map[int]int)

	batchCount := 0
	for i := 0; i < len(workItems); i += batchSize {
		batchCount++
		end := i + batchSize
		if end > len(workItems) {
			end = len(workItems)
		}
		batch := workItems[i:end]

		fmt.Printf("\n🔄 Processing batch %d/%d (%d-%d of %d items)\n",
			batchCount, (len(workItems)+batchSize-1)/batchSize, i+1, end, len(workItems))

		// OPTIMIZATION (Task 1.3): Batch deduplication check - single query for entire batch
		var migratedMap map[int]*WorkItemMigrationRecord
		if db != nil && migrationID != "" {
			batchIDs := make([]int, 0, len(batch))
			for _, wi := range batch {
				if wi.Id != nil {
					batchIDs = append(batchIDs, *wi.Id)
				}
			}

			var err error
			migratedMap, err = db.BatchCheckMigrated(ctx, migrationID, batchIDs)
			if err != nil {
				fmt.Printf("⚠️  Failed to batch check migrated items: %v\n", err)
				migratedMap = make(map[int]*WorkItemMigrationRecord)
			} else {
				fmt.Printf("📊 Batch dedup check: %d items already migrated\n", len(migratedMap))
			}
		} else {
			migratedMap = make(map[int]*WorkItemMigrationRecord)
		}

		// OPTIMIZATION (Task 1.4): Collect records for batch write
		batchRecords := make([]WorkItemMigrationRecord, 0, len(batch))
		batchFailures := make(map[int]string)

		// Process each work item in the batch
		for _, wi := range batch {
			if wi.Id == nil {
				continue
			}

			// Check if already migrated using the batch result
			if existingRecord, isMigrated := migratedMap[*wi.Id]; isMigrated {
				result.ItemsSkipped++
				fmt.Printf("⏭️  Skipping work item %d (already migrated to issue #%d)\n",
					*wi.Id, *existingRecord.GithubIssueNumber)
				if existingRecord.GithubIssueNumber != nil {
					adoToGithubMap[*wi.Id] = *existingRecord.GithubIssueNumber
				}
				continue
			}

			// Migrate the work item
			migratedItem, err := s.migrateWorkItem(ctx, adoConnection, githubClient, wi, request, adoToGithubMap)
			if err != nil {
				result.ItemsFailed++
				fmt.Printf("❌ Failed to migrate work item %d: %v\n", *wi.Id, err)

				// Check if it's a rate limit error
				if isRateLimitError(err) {
					fmt.Printf("⏸️  Rate limit detected, recording event...\n")
					if db != nil && migrationID != "" {
						resetTime := extractRateLimitResetTime(err)
						retryAfter := extractRetryAfterSeconds(err)
						db.RecordRateLimitEvent(ctx, migrationID, "github", "secondary", resetTime, retryAfter)

						// Wait for rate limit to reset
						if resetTime != nil && resetTime.After(time.Now()) {
							waitDuration := time.Until(*resetTime)
							fmt.Printf("⏳ Waiting %v for rate limit to reset...\n", waitDuration)
							time.Sleep(waitDuration)
							fmt.Println("✅ Rate limit period over, resuming migration...")
						}
					}
				}

				// Collect failure for batch recording
				batchFailures[*wi.Id] = err.Error()
				continue
			}

			result.MigratedItems = append(result.MigratedItems, *migratedItem)
			result.ItemsMigrated++
			adoToGithubMap[*wi.Id] = migratedItem.TargetIssueNumber

			// Collect success record for batch write
			workItemRecord := WorkItemMigrationRecord{
				MigrationID:       migrationID,
				AdoWorkItemID:     *wi.Id,
				AdoWorkItemType:   migratedItem.SourceType,
				AdoWorkItemTitle:  migratedItem.SourceTitle,
				GithubIssueNumber: &migratedItem.TargetIssueNumber,
				GithubIssueURL:    &migratedItem.TargetURL,
				Status:            "success",
			}
			batchRecords = append(batchRecords, workItemRecord)

			fmt.Printf("✅ Migrated work item %d to issue #%d (%d/%d)\n",
				*wi.Id, migratedItem.TargetIssueNumber, result.ItemsMigrated+result.ItemsSkipped, result.ItemsDiscovered)
		}

		// OPTIMIZATION (Task 1.4): Batch write all successes and failures
		if db != nil && migrationID != "" {
			if len(batchRecords) > 0 {
				if err := db.BatchRecordMigrations(ctx, batchRecords); err != nil {
					fmt.Printf("⚠️  Failed to batch record migrations: %v\n", err)
				} else {
					fmt.Printf("💾 Batch recorded %d successful migrations\n", len(batchRecords))
				}
			}

			if len(batchFailures) > 0 {
				if err := db.BatchRecordFailures(ctx, migrationID, batchFailures); err != nil {
					fmt.Printf("⚠️  Failed to batch record failures: %v\n", err)
				} else {
					fmt.Printf("💾 Batch recorded %d failures\n", len(batchFailures))
				}
			}

			// Single progress update per batch instead of per item
			if err := db.UpdateMigrationProgress(ctx, migrationID, result.ItemsMigrated, result.ItemsFailed, result.ItemsSkipped); err != nil {
				fmt.Printf("⚠️  Failed to update progress: %v\n", err)
			}
		}

		// Add delay between batches (except for the last batch)
		if end < len(workItems) && batchDelayMinutes > 0 {
			fmt.Printf("\n⏸️  Batch complete. Waiting %d minutes before next batch to avoid rate limits...\n", batchDelayMinutes)
			fmt.Printf("📊 Progress: %d migrated, %d skipped, %d failed out of %d total\n",
				result.ItemsMigrated, result.ItemsSkipped, result.ItemsFailed, result.ItemsDiscovered)

			// Sleep with periodic status updates
			sleepDuration := time.Duration(batchDelayMinutes) * time.Minute
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()

			timer := time.NewTimer(sleepDuration)
			defer timer.Stop()

			select {
			case <-timer.C:
				fmt.Println("✅ Wait complete, resuming migration...")
			case <-ticker.C:
				remaining := time.Until(time.Now().Add(sleepDuration))
				fmt.Printf("⏳ Still waiting... %v remaining\n", remaining)
			case <-ctx.Done():
				fmt.Println("⚠️  Migration cancelled")
				return result, ctx.Err()
			}
		}
	}

	// 6. Update relationships if requested
	if request.Settings.PreserveRelationships && len(result.MigratedItems) > 0 {
		if err := s.updateRelationships(ctx, adoConnection, githubClient, request, result.MigratedItems, adoToGithubMap); err != nil {
			fmt.Printf("Warning: Failed to update some relationships: %v\n", err)
		}
	}

	// 7. Mark migration as complete in database
	if db != nil && migrationID != "" {
		status := "completed"
		errorMsg := ""

		if result.ItemsFailed > 0 && result.ItemsMigrated == 0 {
			status = "failed"
			errorMsg = "All migration attempts failed"
		} else if result.ItemsFailed > 0 {
			status = "partial"
			errorMsg = fmt.Sprintf("%d items failed to migrate", result.ItemsFailed)
		}

		if err := db.CompleteMigration(ctx, migrationID, status, errorMsg); err != nil {
			fmt.Printf("⚠️  Failed to mark migration as complete: %v\n", err)
		}
	}

	result.Success = result.ItemsMigrated > 0
	if result.Success {
		result.Message = fmt.Sprintf("Successfully migrated %d/%d work items", result.ItemsMigrated, result.ItemsDiscovered)
	} else {
		result.Message = "No work items were successfully migrated"
	}

	return result, nil
}

// getAzureDevOpsConnection creates an Azure DevOps connection
func (s *WorkItemService) getAzureDevOpsConnection(ctx context.Context, organization string, token string) (*azuredevops.Connection, error) {
	if token == "" {
		return nil, fmt.Errorf("Azure DevOps token is empty")
	}

	// Create connection to Azure DevOps
	organizationURL := fmt.Sprintf("https://dev.azure.com/%s", organization)
	connection := azuredevops.NewPatConnection(organizationURL, token)

	return connection, nil
}

// getGitHubClient returns a GitHub client, automatically refreshing tokens for GitHub App auth
// This method should be called at strategic points (e.g., start of each batch) to ensure fresh tokens
func (s *WorkItemService) getGitHubClient(ctx context.Context, request WorkItemMigrationRequest) (*github.Client, error) {
	// GitHub App authentication (preferred - auto-refreshes tokens)
	if request.GitHubAuth.AppAuth != nil {
		appID := request.GitHubAuth.AppAuth.AppID
		installationID := request.GitHubAuth.AppAuth.InstallationID
		privateKey := []byte(request.GitHubAuth.AppAuth.PrivateKey)

		if appID == 0 || installationID == 0 || len(privateKey) == 0 {
			return nil, fmt.Errorf("GitHub App authentication incomplete: appID, installationID, and privateKey required")
		}

		// GetClientFromApp automatically handles token refresh (tokens auto-refresh 5 min before expiry)
		return s.githubService.GetClientFromApp(ctx, appID, installationID, privateKey)
	}

	// PAT authentication (fallback)
	token := request.Settings.GitHubToken
	if token == "" {
		return nil, fmt.Errorf("GitHub authentication is required (either App or PAT)")
	}

	return s.githubService.GetClientFromPAT(token), nil
}

// buildWIQLQuery builds a WIQL query from filters
func (s *WorkItemService) buildWIQLQuery(request WorkItemMigrationRequest) string {
	if request.Filters.WIQLQuery != "" {
		return request.Filters.WIQLQuery
	}

	query := "SELECT [System.Id], [System.Title], [System.WorkItemType], [System.State], [System.AssignedTo], [System.Description], [System.Tags] FROM WorkItems WHERE [System.TeamProject] = @project"

	// Add type filter
	if len(request.Filters.Types) > 0 {
		typeFilter := " AND [System.WorkItemType] IN ("
		for i, t := range request.Filters.Types {
			if i > 0 {
				typeFilter += ", "
			}
			typeFilter += fmt.Sprintf("'%s'", t)
		}
		typeFilter += ")"
		query += typeFilter
	}

	// Add state filter
	if len(request.Filters.States) > 0 {
		stateFilter := " AND [System.State] IN ("
		for i, s := range request.Filters.States {
			if i > 0 {
				stateFilter += ", "
			}
			stateFilter += fmt.Sprintf("'%s'", s)
		}
		stateFilter += ")"
		query += stateFilter
	}

	// Add area path filter
	if len(request.Filters.AreaPaths) > 0 {
		areaFilter := " AND [System.AreaPath] IN ("
		for i, a := range request.Filters.AreaPaths {
			if i > 0 {
				areaFilter += ", "
			}
			areaFilter += fmt.Sprintf("'%s'", a)
		}
		areaFilter += ")"
		query += areaFilter
	}

	// Add date range filter
	if request.Filters.DateRange != nil {
		if request.Filters.DateRange.Start != nil {
			query += fmt.Sprintf(" AND [System.CreatedDate] >= '%s'", request.Filters.DateRange.Start.Format("2006-01-02"))
		}
		if request.Filters.DateRange.End != nil {
			query += fmt.Sprintf(" AND [System.CreatedDate] <= '%s'", request.Filters.DateRange.End.Format("2006-01-02"))
		}
	}

	query += " ORDER BY [System.Id]"

	return query
}

// queryWorkItems queries work items from Azure DevOps
func (s *WorkItemService) queryWorkItems(ctx context.Context, connection *azuredevops.Connection, project, wiql string) ([]*workitemtracking.WorkItem, error) {
	if connection == nil {
		return nil, fmt.Errorf("Azure DevOps connection is nil")
	}

	witClient, err := workitemtracking.NewClient(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to create work item tracking client: %w", err)
	}

	// Execute WIQL query
	wiqlRequest := workitemtracking.Wiql{
		Query: &wiql,
	}

	queryResults, err := witClient.QueryByWiql(ctx, workitemtracking.QueryByWiqlArgs{
		Wiql:    &wiqlRequest,
		Project: &project,
	})
	if err != nil {
		// Check if this is the 20,000 item limit error
		errStr := err.Error()
		if strings.Contains(errStr, "VS402337") || strings.Contains(errStr, "size limit") || strings.Contains(errStr, "20000") {
			return nil, fmt.Errorf("query returned too many work items (Azure DevOps limit is 20,000). Please add filters to narrow down your query:\n"+
				"  - Filter by work item type (e.g., Epic, Feature, User Story, Bug)\n"+
				"  - Filter by state (e.g., Active, New, Resolved)\n"+
				"  - Filter by area path or iteration path\n"+
				"  - Filter by date range (e.g., items created in last year)\n"+
				"  - Filter by tags\n"+
				"Original error: %w", err)
		}
		return nil, fmt.Errorf("failed to execute WIQL query: %w", err)
	}

	if queryResults.WorkItems == nil || len(*queryResults.WorkItems) == 0 {
		return []*workitemtracking.WorkItem{}, nil
	}

	// Extract work item IDs
	var ids []int
	for _, ref := range *queryResults.WorkItems {
		if ref.Id != nil {
			ids = append(ids, *ref.Id)
		}
	}

	if len(ids) == 0 {
		return []*workitemtracking.WorkItem{}, nil
	}

	// Batch fetch work item details to avoid Azure DevOps API limits
	// ADO has a limit of ~200 items per request, so we use 195 to be safe
	const adoBatchSize = 195
	var allWorkItems []*workitemtracking.WorkItem

	fmt.Printf("Fetching details for %d work items in batches of %d\n", len(ids), adoBatchSize)

	for i := 0; i < len(ids); i += adoBatchSize {
		end := i + adoBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batchIds := ids[i:end]

		fmt.Printf("Fetching batch %d-%d (%d items)\n", i+1, end, len(batchIds))

		// Get full work item details for this batch
		expand := workitemtracking.WorkItemExpandValues.All
		workItemsPtr, err := witClient.GetWorkItems(ctx, workitemtracking.GetWorkItemsArgs{
			Ids:    &batchIds,
			Expand: &expand,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get work item details for batch %d-%d: %w", i+1, end, err)
		}

		// Convert and append this batch
		if workItemsPtr != nil {
			workItems := *workItemsPtr
			for i := range workItems {
				allWorkItems = append(allWorkItems, &workItems[i])
			}
		}
	}

	fmt.Printf("Successfully fetched details for %d work items\n", len(allWorkItems))

	return allWorkItems, nil
}

// migrateWorkItem migrates a single work item to GitHub issue
func (s *WorkItemService) migrateWorkItem(ctx context.Context, adoConnection *azuredevops.Connection, githubClient *github.Client,
	wi *workitemtracking.WorkItem, request WorkItemMigrationRequest, adoToGithubMap map[int]int) (*MigratedWorkItem, error) {

	// Extract work item fields
	title := s.getWorkItemField(wi, "System.Title")
	description := s.getWorkItemField(wi, "System.Description")
	workItemType := s.getWorkItemField(wi, "System.WorkItemType")
	state := s.getWorkItemField(wi, "System.State")
	tags := s.getWorkItemField(wi, "System.Tags")

	// Map work item type to GitHub label
	issueLabels := s.mapTypeToLabels(workItemType, request.Settings.TypeMapping)

	// Add tags as labels if configured
	if request.Settings.IncludeTags && tags != "" {
		tagList := strings.Split(tags, ";")
		for _, tag := range tagList {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				issueLabels = append(issueLabels, tag)
			}
		}
	}

	// Build issue body
	issueBody := s.buildIssueBody(wi, description, request)

	// Determine issue state
	issueState := s.mapState(state, request.Settings.StateMapping)

	// Create GitHub issue
	issueRequest := &github.IssueRequest{
		Title:  github.String(title),
		Body:   github.String(issueBody),
		Labels: &issueLabels,
	}

	issue, _, err := githubClient.Issues.Create(ctx, request.TargetOwner, request.TargetRepository, issueRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub issue: %w", err)
	}

	migratedItem := &MigratedWorkItem{
		SourceID:          *wi.Id,
		SourceType:        workItemType,
		SourceTitle:       title,
		SourceState:       state,
		TargetIssueNumber: *issue.Number,
		TargetURL:         *issue.HTMLURL,
		MigratedAt:        time.Now(),
	}

	// Close the issue if needed
	if issueState == "closed" {
		closedState := "closed"
		_, _, err := githubClient.Issues.Edit(ctx, request.TargetOwner, request.TargetRepository, *issue.Number, &github.IssueRequest{
			State: &closedState,
		})
		if err != nil {
			// Log error but don't fail the migration
			fmt.Printf("Warning: Failed to close issue #%d: %v\n", *issue.Number, err)
		}
	}

	// Migrate comments if configured
	if request.Settings.IncludeHistory {
		if err := s.migrateComments(ctx, adoConnection, githubClient, wi, request, *issue.Number); err != nil {
			fmt.Printf("Warning: Failed to migrate comments for work item %d: %v\n", *wi.Id, err)
		}
	}

	// Migrate attachments if configured
	if request.Settings.IncludeAttachments {
		if err := s.migrateAttachments(ctx, adoConnection, githubClient, wi, request, *issue.Number); err != nil {
			fmt.Printf("Warning: Failed to migrate attachments for work item %d: %v\n", *wi.Id, err)
		}
	}

	return migratedItem, nil
}

// getWorkItemField extracts a field value from a work item
func (s *WorkItemService) getWorkItemField(wi *workitemtracking.WorkItem, fieldName string) string {
	if wi.Fields == nil {
		return ""
	}

	field, ok := (*wi.Fields)[fieldName]
	if !ok {
		return ""
	}

	// Convert field value to string
	switch v := field.(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	default:
		return fmt.Sprintf("%v", v)
	}

	return ""
}

// mapTypeToLabels maps ADO work item type to GitHub labels
// Unmapped types default to "user-story" label
func (s *WorkItemService) mapTypeToLabels(workItemType string, mapping map[string]string) []string {
	if mapping == nil {
		mapping = getDefaultTypeMapping()
	}

	label, ok := mapping[workItemType]
	if !ok {
		// Default to "user-story" for unmapped work item types
		fmt.Printf("Work item type '%s' not found in mapping, defaulting to 'user-story'\n", workItemType)
		label = "user-story"
	}

	return []string{label}
}

// mapState maps ADO state to GitHub state
func (s *WorkItemService) mapState(adoState string, mapping map[string]string) string {
	if mapping == nil {
		mapping = getDefaultStateMapping()
	}

	githubState, ok := mapping[adoState]
	if !ok {
		// Default mapping
		if strings.EqualFold(adoState, "Closed") || strings.EqualFold(adoState, "Resolved") || strings.EqualFold(adoState, "Removed") {
			return "closed"
		}
		return "open"
	}

	return githubState
}

// buildIssueBody builds the GitHub issue body from work item data
func (s *WorkItemService) buildIssueBody(wi *workitemtracking.WorkItem, description string, request WorkItemMigrationRequest) string {
	body := fmt.Sprintf("**Migrated from Azure DevOps Work Item #%d**\n\n", *wi.Id)

	// Add description
	if description != "" {
		// Strip HTML tags and convert to markdown (simplified)
		body += description + "\n\n"
	}

	// Add metadata
	body += "---\n\n"
	body += "**Original Work Item Details:**\n"
	body += fmt.Sprintf("- **ID:** %d\n", *wi.Id)
	body += fmt.Sprintf("- **Type:** %s\n", s.getWorkItemField(wi, "System.WorkItemType"))
	body += fmt.Sprintf("- **State:** %s\n", s.getWorkItemField(wi, "System.State"))

	if assignedTo := s.getWorkItemField(wi, "System.AssignedTo"); assignedTo != "" {
		body += fmt.Sprintf("- **Assigned To:** %s\n", assignedTo)
	}

	if areaPath := s.getWorkItemField(wi, "System.AreaPath"); areaPath != "" {
		body += fmt.Sprintf("- **Area Path:** %s\n", areaPath)
	}

	if iterationPath := s.getWorkItemField(wi, "System.IterationPath"); iterationPath != "" {
		body += fmt.Sprintf("- **Iteration:** %s\n", iterationPath)
	}

	body += fmt.Sprintf("\n_Migrated on %s_\n", time.Now().Format("2006-01-02 15:04:05"))

	return body
}

// updateRelationships updates issue relationships after all issues are created
func (s *WorkItemService) updateRelationships(ctx context.Context, adoConnection *azuredevops.Connection, githubClient *github.Client,
	request WorkItemMigrationRequest, migratedItems []MigratedWorkItem, adoToGithubMap map[int]int) error {

	// TODO: Implement relationship updates
	// For each migrated item, get its relationships from ADO
	// Update the corresponding GitHub issue with links to related issues

	return nil
}

// getDefaultTypeMapping returns the default work item type to label mapping
func getDefaultTypeMapping() map[string]string {
	return map[string]string{
		"Epic":       "epic",
		"Feature":    "feature",
		"User Story": "enhancement",
		"Task":       "task",
		"Bug":        "bug",
		"Issue":      "issue",
		"Test Case":  "test",
	}
}

// getDefaultStateMapping returns the default work item state mapping
func getDefaultStateMapping() map[string]string {
	return map[string]string{
		"New":      "open",
		"Active":   "open",
		"Resolved": "closed",
		"Closed":   "closed",
		"Removed":  "closed",
	}
}

// migrateComments migrates comments from ADO work item to GitHub issue
func (s *WorkItemService) migrateComments(ctx context.Context, adoConnection *azuredevops.Connection, githubClient *github.Client,
	wi *workitemtracking.WorkItem, request WorkItemMigrationRequest, issueNumber int) error {

	// Get work item comments from ADO
	witClient, err := workitemtracking.NewClient(ctx, adoConnection)
	if err != nil {
		return fmt.Errorf("failed to create work item client: %w", err)
	}

	comments, err := witClient.GetComments(ctx, workitemtracking.GetCommentsArgs{
		Project:    &request.SourceProject,
		WorkItemId: wi.Id,
	})
	if err != nil {
		return fmt.Errorf("failed to get comments: %w", err)
	}

	if comments == nil || comments.Comments == nil || len(*comments.Comments) == 0 {
		return nil // No comments to migrate
	}

	// Check if comments should be combined (default: true for better performance)
	if request.Settings.CombineComments {
		// Combine all comments into a single block
		return s.migrateCombinedComments(ctx, githubClient, comments, request, issueNumber)
	}

	// Legacy mode: Migrate each comment separately
	return s.migrateSeparateComments(ctx, githubClient, comments, request, issueNumber)
}

// migrateCombinedComments creates a single GitHub comment with all ADO comment history
// This reduces API calls from N (one per comment) to just 1, significantly improving performance
func (s *WorkItemService) migrateCombinedComments(ctx context.Context, githubClient *github.Client,
	comments *workitemtracking.CommentList, request WorkItemMigrationRequest, issueNumber int) error {

	if len(*comments.Comments) == 0 {
		return nil
	}

	// Build combined comment body
	var combinedBody strings.Builder
	combinedBody.WriteString("## 💬 Comment History from Azure DevOps\n\n")
	combinedBody.WriteString(fmt.Sprintf("**%d comment(s)** migrated from the original work item:\n\n", len(*comments.Comments)))
	combinedBody.WriteString("---\n\n")

	for i, comment := range *comments.Comments {
		if comment.Text == nil || *comment.Text == "" {
			continue
		}

		// Add comment number header
		combinedBody.WriteString(fmt.Sprintf("### Comment #%d\n\n", i+1))

		// Add metadata if available
		metadata := s.extractCommentMetadata(&comment)
		if metadata != "" {
			combinedBody.WriteString(metadata)
			combinedBody.WriteString("\n\n")
		}

		// Add comment content
		combinedBody.WriteString(*comment.Text)
		combinedBody.WriteString("\n\n---\n\n")
	}

	// Create single combined comment
	_, _, err := githubClient.Issues.CreateComment(ctx, request.TargetOwner, request.TargetRepository, issueNumber, &github.IssueComment{
		Body: github.String(combinedBody.String()),
	})
	if err != nil {
		return fmt.Errorf("failed to create combined comment on issue #%d: %w", issueNumber, err)
	}

	return nil
}

// migrateSeparateComments creates individual GitHub comments for each ADO comment (legacy mode)
func (s *WorkItemService) migrateSeparateComments(ctx context.Context, githubClient *github.Client,
	comments *workitemtracking.CommentList, request WorkItemMigrationRequest, issueNumber int) error {

	for i, comment := range *comments.Comments {
		if comment.Text == nil {
			continue
		}

		commentBody := fmt.Sprintf("**Migrated from Azure DevOps**\n\n%s", *comment.Text)

		_, _, err := githubClient.Issues.CreateComment(ctx, request.TargetOwner, request.TargetRepository, issueNumber, &github.IssueComment{
			Body: github.String(commentBody),
		})
		if err != nil {
			fmt.Printf("Warning: Failed to create comment on issue #%d: %v\n", issueNumber, err)
			// Continue with next comment
		}

		// Add delay between comment creates to avoid secondary rate limits
		// Skip delay after last comment
		if i < len(*comments.Comments)-1 {
			time.Sleep(200 * time.Millisecond) // 200ms between comments = max 5 comments/sec
		}
	}

	return nil
}

// extractCommentMetadata extracts author and timestamp information from ADO comment
func (s *WorkItemService) extractCommentMetadata(comment *workitemtracking.Comment) string {
	var metadata []string

	// Extract author
	if comment.CreatedBy != nil && comment.CreatedBy.DisplayName != nil {
		metadata = append(metadata, fmt.Sprintf("**Author:** %s", *comment.CreatedBy.DisplayName))
	}

	// Extract timestamp
	if comment.CreatedDate != nil && comment.CreatedDate.Time.Year() > 1 {
		metadata = append(metadata, fmt.Sprintf("**Date:** %s", comment.CreatedDate.Time.Format("2006-01-02 15:04:05 MST")))
	}

	// Extract revision info if available
	if comment.ModifiedBy != nil && comment.ModifiedBy.DisplayName != nil &&
		comment.ModifiedDate != nil && comment.ModifiedDate.Time.Year() > 1 {
		// Only show if modified by different person or significantly later
		if comment.CreatedBy == nil || comment.CreatedBy.DisplayName == nil ||
			*comment.ModifiedBy.DisplayName != *comment.CreatedBy.DisplayName {
			metadata = append(metadata, fmt.Sprintf("**Modified by:** %s on %s",
				*comment.ModifiedBy.DisplayName,
				comment.ModifiedDate.Time.Format("2006-01-02 15:04:05 MST")))
		}
	}

	if len(metadata) == 0 {
		return ""
	}

	return "_" + strings.Join(metadata, " | ") + "_"
}

// migrateAttachments migrates attachments from ADO work item to GitHub issue
func (s *WorkItemService) migrateAttachments(ctx context.Context, adoConnection *azuredevops.Connection, githubClient *github.Client,
	wi *workitemtracking.WorkItem, request WorkItemMigrationRequest, issueNumber int) error {

	// Note: Attachment migration is complex and would require downloading from ADO and uploading to GitHub
	// For now, we'll just add a comment listing the attachments
	if wi.Relations == nil || len(*wi.Relations) == 0 {
		return nil
	}

	var attachments []string
	for _, relation := range *wi.Relations {
		if relation.Rel != nil && *relation.Rel == "AttachedFile" && relation.Url != nil {
			attachments = append(attachments, *relation.Url)
		}
	}

	if len(attachments) > 0 {
		attachmentList := "**Attachments from Azure DevOps:**\n\n"
		for i, url := range attachments {
			attachmentList += fmt.Sprintf("%d. %s\n", i+1, url)
		}

		_, _, err := githubClient.Issues.CreateComment(ctx, request.TargetOwner, request.TargetRepository, issueNumber, &github.IssueComment{
			Body: github.String(attachmentList),
		})
		if err != nil {
			return fmt.Errorf("failed to create attachments comment: %w", err)
		}
	}

	return nil
}

// ValidateWorkItemMigrationRequest validates a work item migration request
func (s *WorkItemService) ValidateWorkItemMigrationRequest(request WorkItemMigrationRequest) error {
	if request.SourceOrganization == "" {
		return fmt.Errorf("source organization is required")
	}
	if request.SourceProject == "" {
		return fmt.Errorf("source project is required")
	}
	if request.TargetOwner == "" {
		return fmt.Errorf("target owner is required")
	}
	// TargetRepository is OPTIONAL - if not specified, issues will be created in organization scope
	// Note: GitHub Issues require a repository, so actual migration will fail gracefully if repository is not specified
	// This allows the CRD to accept optional repository while providing clear error during execution
	if request.AzureAuth.ServicePrincipal != nil && request.AzureAuth.ServicePrincipal.ClientIDRef.Name == "" {
		return fmt.Errorf("azure client ID reference is required when using service principal")
	}
	if request.AzureAuth.ServicePrincipal != nil && request.AzureAuth.ServicePrincipal.TenantIDRef.Name == "" {
		return fmt.Errorf("azure tenant ID reference is required when using service principal")
	}
	if request.AzureAuth.PAT != nil && request.AzureAuth.PAT.TokenRef.Name == "" {
		return fmt.Errorf("azure PAT token reference is required when using PAT")
	}
	// GitHub auth validation - check if either PAT token or App auth is provided
	if request.GitHubAuth.Token == "" && request.GitHubAuth.AppAuth == nil {
		return fmt.Errorf("github authentication is required (either Token or AppAuth)")
	}

	return nil
}

// GetWorkItemTypes gets available work item types for a project
func (s *WorkItemService) GetWorkItemTypes(ctx context.Context, organization, project string, auth migrationv1.AdoAuthConfig) ([]string, error) {
	// This would use the Azure DevOps API to get work item types
	// For now, return common work item types

	return []string{
		"Epic",
		"Feature",
		"User Story",
		"Task",
		"Bug",
		"Issue",
		"Test Case",
	}, nil
}

// GetWorkItemStates gets available work item states for a project
func (s *WorkItemService) GetWorkItemStates(ctx context.Context, organization, project string, auth migrationv1.AdoAuthConfig) ([]string, error) {
	// This would use the Azure DevOps API to get work item states
	// For now, return common work item states

	return []string{
		"New",
		"Active",
		"Resolved",
		"Closed",
		"Removed",
	}, nil
}

// isRateLimitError checks if an error is a GitHub rate limit error
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())

	// Check for common GitHub rate limit error patterns
	return (strings.Contains(errStr, "403") && strings.Contains(errStr, "rate limit")) ||
		strings.Contains(errStr, "secondary rate limit") ||
		strings.Contains(errStr, "you have exceeded") ||
		strings.Contains(errStr, "api rate limit exceeded")
}

// extractRateLimitResetTime parses the reset time from rate limit error
func extractRateLimitResetTime(err error) *time.Time {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	// Try to parse various time formats from GitHub error messages
	// Example: "rate limit will reset at 2025-11-13 09:11:52"
	// Example: "until 2025-11-13 09:11:52"

	// Look for "until YYYY-MM-DD HH:MM:SS" pattern
	if idx := strings.Index(errStr, "until "); idx >= 0 {
		timeStr := errStr[idx+6:]
		// Take first 19 characters (YYYY-MM-DD HH:MM:SS)
		if len(timeStr) >= 19 {
			timeStr = timeStr[:19]
			if resetTime, err := time.Parse("2006-01-02 15:04:05", timeStr); err == nil {
				return &resetTime
			}
		}
	}

	// Look for "reset at YYYY-MM-DD HH:MM:SS" pattern
	if idx := strings.Index(errStr, "reset at "); idx >= 0 {
		timeStr := errStr[idx+9:]
		if len(timeStr) >= 19 {
			timeStr = timeStr[:19]
			if resetTime, err := time.Parse("2006-01-02 15:04:05", timeStr); err == nil {
				return &resetTime
			}
		}
	}

	// Default: wait 1 hour from now
	defaultReset := time.Now().Add(1 * time.Hour)
	return &defaultReset
}

// extractRetryAfterSeconds parses retry-after seconds from error
func extractRetryAfterSeconds(err error) int {
	if err == nil {
		return 3600 // Default to 1 hour
	}

	errStr := strings.ToLower(err.Error())

	// Look for patterns like "retry after 3600 seconds"
	if idx := strings.Index(errStr, "retry after "); idx >= 0 {
		timeStr := errStr[idx+12:]
		// Extract the number
		var seconds int
		if _, err := fmt.Sscanf(timeStr, "%d", &seconds); err == nil {
			return seconds
		}
	}

	// Look for patterns like "try again in 60 minutes"
	if idx := strings.Index(errStr, "try again in "); idx >= 0 {
		timeStr := errStr[idx+13:]
		var value int
		var unit string
		if _, err := fmt.Sscanf(timeStr, "%d %s", &value, &unit); err == nil {
			if strings.HasPrefix(unit, "minute") {
				return value * 60
			} else if strings.HasPrefix(unit, "hour") {
				return value * 3600
			} else if strings.HasPrefix(unit, "second") {
				return value
			}
		}
	}

	// Default to 1 hour (GitHub secondary rate limit is typically 1 hour)
	return 3600
}
