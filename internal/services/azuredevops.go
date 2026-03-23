package services

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/build"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/core"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/release"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/workitemtracking"

	migrationv1 "github.com/tesserix/reposhift/api/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AzureDevOpsService provides Azure DevOps integration
type AzureDevOpsService struct {
	rateLimiter         *RateLimiter
	adaptiveRateLimiter *AdaptiveRateLimiter
	connectionCache     map[string]*azuredevops.Connection
	cacheMutex          sync.RWMutex
	tokenCache          map[string]tokenCacheEntry
	tokenCacheMutex     sync.RWMutex
}

// tokenCacheEntry represents a cached token with expiration
type tokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

// NewAzureDevOpsService creates a new Azure DevOps service
func NewAzureDevOpsService() *AzureDevOpsService {
	return &AzureDevOpsService{
		rateLimiter:         NewRateLimiter(60, time.Minute), // 60 requests per minute
		adaptiveRateLimiter: NewAdaptiveRateLimiter(60, time.Minute, 5*time.Minute),
		connectionCache:     make(map[string]*azuredevops.Connection),
		tokenCache:          make(map[string]tokenCacheEntry),
	}
}

// DiscoverOrganizations discovers available Azure DevOps organizations
func (s *AzureDevOpsService) DiscoverOrganizations(ctx context.Context, clientID, clientSecret, tenantID string) ([]migrationv1.Organization, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-orgs"); err != nil {
		return nil, err
	}

	// Get token
	token, err := s.getToken(ctx, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Create Azure DevOps connection
	connection := azuredevops.NewAnonymousConnection("https://dev.azure.com/")
	connection.AuthorizationString = "Bearer " + token

	// For now, return mock data since the Azure DevOps Go SDK doesn't have
	// direct organization discovery. In a real implementation, you would
	// use the Profile APIs to get the user's organizations.

	organizations := []migrationv1.Organization{
		{
			ID:          "org1",
			Name:        "MyOrganization",
			URL:         "https://dev.azure.com/MyOrganization",
			Description: "Main development organization",
			Region:      "Central US",
			Owner:       "admin@myorg.com",
		},
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-orgs")

	return organizations, nil
}

// DiscoverProjects discovers projects in an Azure DevOps organization
func (s *AzureDevOpsService) DiscoverProjects(ctx context.Context, organization, clientID, clientSecret, tenantID string) ([]migrationv1.Project, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-projects"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create core client
	coreClient, err := core.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-discover-projects", 500)
		return nil, fmt.Errorf("failed to create core client: %w", err)
	}

	// Get projects with retry logic
	var projects *[]core.TeamProjectReference

	for retries := 0; retries < 3; retries++ {
		projects, err = s.getProjects(ctx, coreClient, organization)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-projects", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get projects after retries: %w", err)
	}

	var result []migrationv1.Project
	for _, project := range *projects {
		proj := migrationv1.Project{
			ID:         project.Id.String(),
			Name:       *project.Name,
			URL:        *project.Url,
			State:      string(*project.State),
			Visibility: string(*project.Visibility),
			Revision:   int64(*project.Revision),
		}
		if project.Description != nil {
			proj.Description = *project.Description
		}
		if project.LastUpdateTime != nil {
			proj.LastUpdateTime = &metav1.Time{Time: project.LastUpdateTime.Time}
		}
		result = append(result, proj)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-projects")

	return result, nil
}

// getProjects gets projects
func (s *AzureDevOpsService) getProjects(ctx context.Context, client core.Client, organization string) (*[]core.TeamProjectReference, error) {
	projectsResponse, err := client.GetProjects(ctx, core.GetProjectsArgs{})
	if err != nil {
		return nil, err
	}
	return &projectsResponse.Value, nil
}

// DiscoverRepositories discovers repositories in an Azure DevOps project
func (s *AzureDevOpsService) DiscoverRepositories(ctx context.Context, organization, project, clientID, clientSecret, tenantID string, includeEmpty, includeDisabled bool) ([]migrationv1.Repository, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-repos"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create git client
	gitClient, err := git.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-discover-repos", 500)
		return nil, fmt.Errorf("failed to create git client: %w", err)
	}

	// Get repositories with retry logic
	var repositories *[]git.GitRepository

	for retries := 0; retries < 3; retries++ {
		repositories, err = s.getRepositories(ctx, gitClient, project, includeDisabled)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-repos", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get repositories after retries: %w", err)
	}

	var result []migrationv1.Repository
	for _, repo := range *repositories {
		// Skip empty repositories if not requested
		if !includeEmpty && *repo.Size == 0 {
			continue
		}

		// Skip disabled repositories if not requested
		if !includeDisabled && repo.IsDisabled != nil && *repo.IsDisabled {
			continue
		}

		repository := migrationv1.Repository{
			ID:          repo.Id.String(),
			Name:        *repo.Name,
			URL:         *repo.RemoteUrl,
			IsEmpty:     *repo.Size == 0,
			Size:        int64(*repo.Size),
			ProjectID:   repo.Project.Id.String(),
			ProjectName: *repo.Project.Name,
			RemoteURL:   *repo.RemoteUrl,
			WebURL:      *repo.WebUrl,
		}

		if repo.DefaultBranch != nil {
			repository.DefaultBranch = *repo.DefaultBranch
		}

		if repo.IsDisabled != nil {
			repository.IsDisabled = *repo.IsDisabled
		}

		if repo.SshUrl != nil {
			repository.SSHURL = *repo.SshUrl
		}

		result = append(result, repository)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-repos")

	return result, nil
}

// getRepositories gets repositories
func (s *AzureDevOpsService) getRepositories(ctx context.Context, client git.Client, project string, includeHidden bool) (*[]git.GitRepository, error) {
	repositories, err := client.GetRepositories(ctx, git.GetRepositoriesArgs{
		Project:       &project,
		IncludeHidden: &includeHidden,
	})
	if err != nil {
		return nil, err
	}
	return repositories, nil
}

// DiscoverWorkItems discovers work items in an Azure DevOps project
func (s *AzureDevOpsService) DiscoverWorkItems(ctx context.Context, organization, project, clientID, clientSecret, tenantID, workItemTypes, states string, limit int) ([]migrationv1.WorkItem, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-workitems"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create work item tracking client
	witClient, err := workitemtracking.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-discover-workitems", 500)
		return nil, fmt.Errorf("failed to create work item tracking client: %w", err)
	}

	// Build WIQL query
	wiql := "SELECT [System.Id], [System.WorkItemType], [System.Title], [System.State], [System.AssignedTo], [System.CreatedDate], [System.ChangedDate], [System.AreaPath], [System.IterationPath], [System.Tags] FROM workitems WHERE [System.TeamProject] = @project"

	if workItemTypes != "" {
		wiql += fmt.Sprintf(" AND [System.WorkItemType] IN (%s)", workItemTypes)
	}

	if states != "" {
		wiql += fmt.Sprintf(" AND [System.State] IN (%s)", states)
	}

	wiql += " ORDER BY [System.Id] DESC"

	// Query work items with retry logic
	var queryResult *workitemtracking.WorkItemQueryResult

	for retries := 0; retries < 3; retries++ {
		queryResult, err = s.queryWorkItems(ctx, witClient, wiql, project, limit)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-workitems", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query work items after retries: %w", err)
	}

	if queryResult.WorkItems == nil {
		return []migrationv1.WorkItem{}, nil
	}

	// Get work item IDs
	var workItemIds []int
	for _, wi := range *queryResult.WorkItems {
		workItemIds = append(workItemIds, *wi.Id)
	}

	if len(workItemIds) == 0 {
		return []migrationv1.WorkItem{}, nil
	}

	// Get work item details with retry logic
	var workItems *[]workitemtracking.WorkItem

	for retries := 0; retries < 3; retries++ {
		workItems, err = s.getWorkItems(ctx, witClient, workItemIds)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-workitems", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get work item details after retries: %w", err)
	}

	var result []migrationv1.WorkItem
	for _, workItem := range *workItems {
		wiInfo := migrationv1.WorkItem{
			ID:          *workItem.Id,
			ProjectID:   project,
			ProjectName: project,
			URL:         *workItem.Url,
		}

		if fields := workItem.Fields; fields != nil {
			if workItemType, ok := (*fields)["System.WorkItemType"]; ok {
				if workItemTypeStr, ok := workItemType.(string); ok {
					wiInfo.Type = workItemTypeStr
				}
			}
			if title, ok := (*fields)["System.Title"]; ok {
				if titleStr, ok := title.(string); ok {
					wiInfo.Title = titleStr
				}
			}
			if state, ok := (*fields)["System.State"]; ok {
				if stateStr, ok := state.(string); ok {
					wiInfo.State = stateStr
				}
			}
			if assignedTo, ok := (*fields)["System.AssignedTo"]; ok {
				if assignedToStr, ok := assignedTo.(string); ok {
					wiInfo.AssignedTo = assignedToStr
				}
			}
			if areaPath, ok := (*fields)["System.AreaPath"]; ok {
				if areaPathStr, ok := areaPath.(string); ok {
					wiInfo.AreaPath = areaPathStr
				}
			}
			if iterationPath, ok := (*fields)["System.IterationPath"]; ok {
				if iterationPathStr, ok := iterationPath.(string); ok {
					wiInfo.IterationPath = iterationPathStr
				}
			}
			if tags, ok := (*fields)["System.Tags"]; ok {
				if tagsStr, ok := tags.(string); ok && tagsStr != "" {
					// Parse tags (comma-separated)
					wiInfo.Tags = []string{tagsStr}
				}
			}
			if createdDate, ok := (*fields)["System.CreatedDate"]; ok {
				if createdDateStr, ok := createdDate.(string); ok {
					if parsedTime, err := time.Parse(time.RFC3339, createdDateStr); err == nil {
						wiInfo.CreatedDate = metav1.Time{Time: parsedTime}
					}
				}
			}
			if changedDate, ok := (*fields)["System.ChangedDate"]; ok {
				if changedDateStr, ok := changedDate.(string); ok {
					if parsedTime, err := time.Parse(time.RFC3339, changedDateStr); err == nil {
						wiInfo.ChangedDate = metav1.Time{Time: parsedTime}
					}
				}
			}
		}

		result = append(result, wiInfo)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-workitems")

	return result, nil
}

// queryWorkItems queries work items
func (s *AzureDevOpsService) queryWorkItems(ctx context.Context, client workitemtracking.Client, wiql, project string, limit int) (*workitemtracking.WorkItemQueryResult, error) {
	top := limit
	queryResult, err := client.QueryByWiql(ctx, workitemtracking.QueryByWiqlArgs{
		Wiql: &workitemtracking.Wiql{
			Query: &wiql,
		},
		Project: &project,
		Top:     &top,
	})
	if err != nil {
		return nil, err
	}
	return queryResult, nil
}

// getWorkItems gets work item details
func (s *AzureDevOpsService) getWorkItems(ctx context.Context, client workitemtracking.Client, workItemIds []int) (*[]workitemtracking.WorkItem, error) {
	fields := []string{"System.Id", "System.WorkItemType", "System.Title", "System.State", "System.AssignedTo", "System.CreatedDate", "System.ChangedDate", "System.AreaPath", "System.IterationPath", "System.Tags"}
	workItems, err := client.GetWorkItems(ctx, workitemtracking.GetWorkItemsArgs{
		Ids:    &workItemIds,
		Fields: &fields,
	})
	if err != nil {
		return nil, err
	}
	return workItems, nil
}

// DiscoverPipelines discovers pipelines in an Azure DevOps project
func (s *AzureDevOpsService) DiscoverPipelines(ctx context.Context, organization, project, clientID, clientSecret, tenantID, pipelineType string) ([]migrationv1.Pipeline, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-pipelines"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	var result []migrationv1.Pipeline

	// Discover build pipelines
	if pipelineType == "build" || pipelineType == "all" {
		buildClient, err := build.NewClient(ctx, connection)
		if err != nil {
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)
			return nil, fmt.Errorf("failed to create build client: %w", err)
		}

		// Get build definitions with retry logic
		var definitions *[]build.BuildDefinitionReference

		for retries := 0; retries < 3; retries++ {
			definitions, err = s.getBuildDefinitions(ctx, buildClient, project)
			if err == nil {
				break
			}

			// Report error for adaptive rate limiting
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)

			// Wait before retry
			select {
			case <-time.After(time.Duration(retries+1) * time.Second):
				// Continue with retry
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if err != nil {
			return nil, fmt.Errorf("failed to get build definitions after retries: %w", err)
		}

		for _, def := range *definitions {
			pipeline := migrationv1.Pipeline{
				ID:          *def.Id,
				Name:        *def.Name,
				Type:        "build",
				Quality:     string(*def.Quality),
				QueueStatus: string(*def.QueueStatus),
				Revision:    *def.Revision,
				URL:         *def.Url,
				ProjectID:   project,
				ProjectName: project,
			}

			if def.Path != nil {
				pipeline.Folder = *def.Path
			}

			if def.CreatedDate != nil {
				pipeline.CreatedDate = metav1.Time{Time: def.CreatedDate.Time}
			}

			result = append(result, pipeline)
		}
	}

	// Discover release pipelines
	if pipelineType == "release" || pipelineType == "all" {
		releaseClient, err := release.NewClient(ctx, connection)
		if err != nil {
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)
			return nil, fmt.Errorf("failed to create release client: %w", err)
		}

		// Get release definitions with retry logic
		var definitions *[]release.ReleaseDefinition

		for retries := 0; retries < 3; retries++ {
			definitions, err = s.getReleaseDefinitions(ctx, releaseClient, project)
			if err == nil {
				break
			}

			// Report error for adaptive rate limiting
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)

			// Wait before retry
			select {
			case <-time.After(time.Duration(retries+1) * time.Second):
				// Continue with retry
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if err != nil {
			return nil, fmt.Errorf("failed to get release definitions after retries: %w", err)
		}

		for _, def := range *definitions {
			pipeline := migrationv1.Pipeline{
				ID:          *def.Id,
				Name:        *def.Name,
				Type:        "release",
				Revision:    *def.Revision,
				URL:         *def.Url,
				ProjectID:   project,
				ProjectName: project,
			}

			if def.Path != nil {
				pipeline.Folder = *def.Path
			}

			if def.CreatedOn != nil {
				pipeline.CreatedDate = metav1.Time{Time: def.CreatedOn.Time}
			}

			result = append(result, pipeline)
		}
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-pipelines")

	return result, nil
}

// DiscoverPipelinesWithPAT discovers pipelines in an Azure DevOps project using PAT authentication
func (s *AzureDevOpsService) DiscoverPipelinesWithPAT(ctx context.Context, organization, project, pat, pipelineType string) ([]migrationv1.Pipeline, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-pipelines"); err != nil {
		return nil, err
	}

	// Create connection using PAT
	// Azure DevOps PAT should be base64 encoded as :PAT (empty username)
	connection := azuredevops.NewPatConnection(fmt.Sprintf("https://dev.azure.com/%s", organization), pat)

	var result []migrationv1.Pipeline

	// Discover build pipelines
	if pipelineType == "build" || pipelineType == "all" {
		buildClient, err := build.NewClient(ctx, connection)
		if err != nil {
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)
			return nil, fmt.Errorf("failed to create build client: %w", err)
		}

		// Get build definitions with retry logic
		var definitions *[]build.BuildDefinitionReference

		for retries := 0; retries < 3; retries++ {
			definitions, err = s.getBuildDefinitions(ctx, buildClient, project)
			if err == nil {
				break
			}

			// Report error for adaptive rate limiting
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)

			// Wait before retry
			select {
			case <-time.After(time.Duration(retries+1) * time.Second):
				// Continue with retry
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if err != nil {
			return nil, fmt.Errorf("failed to get build definitions after retries: %w", err)
		}

		for _, def := range *definitions {
			pipeline := migrationv1.Pipeline{
				ID:          *def.Id,
				Name:        *def.Name,
				Type:        "build",
				Quality:     string(*def.Quality),
				QueueStatus: string(*def.QueueStatus),
				Revision:    *def.Revision,
				URL:         *def.Url,
				ProjectID:   project,
				ProjectName: project,
			}

			if def.Path != nil {
				pipeline.Folder = *def.Path
			}

			if def.CreatedDate != nil {
				pipeline.CreatedDate = metav1.Time{Time: def.CreatedDate.Time}
			}

			result = append(result, pipeline)
		}
	}

	// Discover release pipelines
	if pipelineType == "release" || pipelineType == "all" {
		releaseClient, err := release.NewClient(ctx, connection)
		if err != nil {
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)
			return nil, fmt.Errorf("failed to create release client: %w", err)
		}

		// Get release definitions with retry logic
		var definitions *[]release.ReleaseDefinition

		for retries := 0; retries < 3; retries++ {
			definitions, err = s.getReleaseDefinitions(ctx, releaseClient, project)
			if err == nil {
				break
			}

			// Report error for adaptive rate limiting
			s.adaptiveRateLimiter.ReportError("ado-discover-pipelines", 500)

			// Wait before retry
			select {
			case <-time.After(time.Duration(retries+1) * time.Second):
				// Continue with retry
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		if err != nil {
			return nil, fmt.Errorf("failed to get release definitions after retries: %w", err)
		}

		if definitions != nil {
			for _, def := range *definitions {
				pipeline := migrationv1.Pipeline{
					ID:          *def.Id,
					Name:        *def.Name,
					Type:        "release",
					Revision:    *def.Revision,
					URL:         *def.Url,
					ProjectID:   project,
					ProjectName: project,
				}

				if def.Path != nil {
					pipeline.Folder = *def.Path
				}

				if def.CreatedOn != nil {
					pipeline.CreatedDate = metav1.Time{Time: def.CreatedOn.Time}
				}

				result = append(result, pipeline)
			}
		}
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-pipelines")

	return result, nil
}

// getBuildDefinitions gets build definitions
func (s *AzureDevOpsService) getBuildDefinitions(ctx context.Context, client build.Client, project string) (*[]build.BuildDefinitionReference, error) {
	definitionsResponse, err := client.GetDefinitions(ctx, build.GetDefinitionsArgs{
		Project: &project,
	})
	if err != nil {
		return nil, err
	}
	return &definitionsResponse.Value, nil
}

// getReleaseDefinitions gets release definitions
func (s *AzureDevOpsService) getReleaseDefinitions(ctx context.Context, client release.Client, project string) (*[]release.ReleaseDefinition, error) {
	definitionsResponse, err := client.GetReleaseDefinitions(ctx, release.GetReleaseDefinitionsArgs{
		Project: &project,
	})
	if err != nil {
		return nil, err
	}
	return &definitionsResponse.Value, nil
}

// DiscoverBuilds discovers builds in an Azure DevOps project
func (s *AzureDevOpsService) DiscoverBuilds(ctx context.Context, organization, project, clientID, clientSecret, tenantID string, limit int) ([]migrationv1.Build, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-builds"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create build client
	buildClient, err := build.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-discover-builds", 500)
		return nil, fmt.Errorf("failed to create build client: %w", err)
	}

	// Get builds with retry logic
	var builds *[]build.Build

	for retries := 0; retries < 3; retries++ {
		builds, err = s.getBuilds(ctx, buildClient, project, limit)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-builds", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get builds after retries: %w", err)
	}

	var result []migrationv1.Build
	for _, b := range *builds {
		buildItem := migrationv1.Build{
			ID:           *b.Id,
			BuildNumber:  *b.BuildNumber,
			Status:       string(*b.Status),
			QueueTime:    metav1.Time{Time: b.QueueTime.Time},
			URL:          *b.Url,
			ProjectID:    project,
			ProjectName:  project,
			PipelineID:   *b.Definition.Id,
			PipelineName: *b.Definition.Name,
		}

		if b.Result != nil {
			buildItem.Result = string(*b.Result)
		}

		if b.StartTime != nil {
			buildItem.StartTime = &metav1.Time{Time: b.StartTime.Time}
		}

		if b.FinishTime != nil {
			buildItem.FinishTime = &metav1.Time{Time: b.FinishTime.Time}
		}

		if b.SourceBranch != nil {
			buildItem.SourceBranch = *b.SourceBranch
		}

		if b.SourceVersion != nil {
			buildItem.SourceVersion = *b.SourceVersion
		}

		if b.RequestedBy != nil && b.RequestedBy.DisplayName != nil {
			buildItem.RequestedBy = *b.RequestedBy.DisplayName
		}

		if b.RequestedFor != nil && b.RequestedFor.DisplayName != nil {
			buildItem.RequestedFor = *b.RequestedFor.DisplayName
		}

		result = append(result, buildItem)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-builds")

	return result, nil
}

// getBuilds gets builds
func (s *AzureDevOpsService) getBuilds(ctx context.Context, client build.Client, project string, limit int) (*[]build.Build, error) {
	top := limit
	buildsResponse, err := client.GetBuilds(ctx, build.GetBuildsArgs{
		Project: &project,
		Top:     &top,
	})
	if err != nil {
		return nil, err
	}
	return &buildsResponse.Value, nil
}

// DiscoverReleases discovers releases in an Azure DevOps project
func (s *AzureDevOpsService) DiscoverReleases(ctx context.Context, organization, project, clientID, clientSecret, tenantID string, limit int) ([]migrationv1.Release, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-releases"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create release client
	releaseClient, err := release.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-discover-releases", 500)
		return nil, fmt.Errorf("failed to create release client: %w", err)
	}

	// Get releases with retry logic
	var releases *[]release.Release

	for retries := 0; retries < 3; retries++ {
		releases, err = s.getReleases(ctx, releaseClient, project, limit)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-releases", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get releases after retries: %w", err)
	}

	var result []migrationv1.Release
	for _, r := range *releases {
		releaseItem := migrationv1.Release{
			ID:                    *r.Id,
			Name:                  *r.Name,
			Status:                string(*r.Status),
			CreatedOn:             metav1.Time{Time: r.CreatedOn.Time},
			ProjectID:             project,
			ProjectName:           project,
			ReleaseDefinitionID:   *r.ReleaseDefinition.Id,
			ReleaseDefinitionName: *r.ReleaseDefinition.Name,
		}

		// Handle URL - check if _links exists and has web property
		if r.Links != nil {
			if linksMap, ok := r.Links.(map[string]interface{}); ok {
				if webLink, exists := linksMap["web"]; exists {
					if webLinkMap, ok := webLink.(map[string]interface{}); ok {
						if href, exists := webLinkMap["href"]; exists {
							if hrefStr, ok := href.(string); ok {
								releaseItem.URL = hrefStr
							}
						}
					}
				}
			}
		}

		if r.Description != nil {
			releaseItem.Description = *r.Description
		}

		if r.ModifiedOn != nil {
			releaseItem.ModifiedOn = &metav1.Time{Time: r.ModifiedOn.Time}
		}

		if r.CreatedBy != nil && r.CreatedBy.DisplayName != nil {
			releaseItem.CreatedBy = *r.CreatedBy.DisplayName
		}

		if r.ModifiedBy != nil && r.ModifiedBy.DisplayName != nil {
			releaseItem.ModifiedBy = *r.ModifiedBy.DisplayName
		}

		// Process environments
		if r.Environments != nil {
			for _, env := range *r.Environments {
				environment := migrationv1.ReleaseEnvironment{
					ID:     *env.Id,
					Name:   *env.Name,
					Status: string(*env.Status),
					Rank:   *env.Rank,
				}

				if env.CreatedOn != nil {
					environment.CreatedOn = &metav1.Time{Time: env.CreatedOn.Time}
				}

				if env.ModifiedOn != nil {
					environment.ModifiedOn = &metav1.Time{Time: env.ModifiedOn.Time}
				}

				releaseItem.Environments = append(releaseItem.Environments, environment)
			}
		}

		result = append(result, releaseItem)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-releases")

	return result, nil
}

// getReleases gets releases
func (s *AzureDevOpsService) getReleases(ctx context.Context, client release.Client, project string, limit int) (*[]release.Release, error) {
	top := limit
	releasesResponse, err := client.GetReleases(ctx, release.GetReleasesArgs{
		Project: &project,
		Top:     &top,
	})
	if err != nil {
		return nil, err
	}
	return &releasesResponse.Value, nil
}

// DiscoverTeams discovers teams in an Azure DevOps organization/project
func (s *AzureDevOpsService) DiscoverTeams(ctx context.Context, organization, project, clientID, clientSecret, tenantID string) ([]migrationv1.Team, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-teams"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create core client
	coreClient, err := core.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-discover-teams", 500)
		return nil, fmt.Errorf("failed to create core client: %w", err)
	}

	// Get teams with retry logic
	var teams *[]core.WebApiTeam

	for retries := 0; retries < 3; retries++ {
		teams, err = s.getTeams(ctx, coreClient, project)
		if err == nil {
			break
		}

		// Report error for adaptive rate limiting
		s.adaptiveRateLimiter.ReportError("ado-discover-teams", 500)

		// Wait before retry
		select {
		case <-time.After(time.Duration(retries+1) * time.Second):
			// Continue with retry
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get teams after retries: %w", err)
	}

	var result []migrationv1.Team
	for _, t := range *teams {
		team := migrationv1.Team{
			ID:          t.Id.String(),
			Name:        *t.Name,
			URL:         *t.Url,
			ProjectID:   project,
			ProjectName: project,
		}

		if t.Description != nil {
			team.Description = *t.Description
		}

		if t.IdentityUrl != nil {
			team.IdentityURL = *t.IdentityUrl
		}

		result = append(result, team)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-teams")

	return result, nil
}

// getTeams gets teams
func (s *AzureDevOpsService) getTeams(ctx context.Context, client core.Client, project string) (*[]core.WebApiTeam, error) {
	teams, err := client.GetTeams(ctx, core.GetTeamsArgs{
		ProjectId: &project,
	})
	if err != nil {
		return nil, err
	}
	return teams, nil
}

// DiscoverUsers discovers users in an Azure DevOps organization
func (s *AzureDevOpsService) DiscoverUsers(ctx context.Context, organization, clientID, clientSecret, tenantID string) ([]migrationv1.User, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-discover-users"); err != nil {
		return nil, err
	}

	// In a real implementation, you would use the Graph API to get users
	// For now, return mock data
	users := []migrationv1.User{
		{
			ID:          "user1",
			DisplayName: "John Doe",
			UniqueName:  "john.doe@company.com",
			URL:         fmt.Sprintf("https://dev.azure.com/%s/_apis/identities/user1", organization),
		},
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-discover-users")

	return users, nil
}

// ValidateCredentials validates Azure DevOps credentials and returns permissions
func (s *AzureDevOpsService) ValidateCredentials(ctx context.Context, clientID, clientSecret, tenantID, organization string) ([]string, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-validate-creds"); err != nil {
		return nil, err
	}

	// Try to get a token
	token, err := s.getToken(ctx, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Create Azure DevOps connection
	connection := azuredevops.NewAnonymousConnection(fmt.Sprintf("https://dev.azure.com/%s", organization))
	connection.AuthorizationString = "Bearer " + token

	// Try to access the organization
	coreClient, err := core.NewClient(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to create core client: %w", err)
	}

	// Try to get projects to verify access
	_, err = coreClient.GetProjects(ctx, core.GetProjectsArgs{})
	if err != nil {
		return nil, fmt.Errorf("failed to access organization: %w", err)
	}

	// In a real implementation, you would check the actual permissions
	// For now, return common permissions
	permissions := []string{
		"vso.code_read",
		"vso.work_read",
		"vso.build_read",
		"vso.release_read",
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-validate-creds")

	return permissions, nil
}

// ValidateProjectAccess validates access to a specific project
func (s *AzureDevOpsService) ValidateProjectAccess(ctx context.Context, organization, project, clientID, clientSecret, tenantID string) error {
	if err := s.rateLimiter.Wait(ctx, "ado-validate-project"); err != nil {
		return err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}

	// Create core client
	coreClient, err := core.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-validate-project", 500)
		return fmt.Errorf("failed to create core client: %w", err)
	}

	// Try to get the project
	_, err = coreClient.GetProject(ctx, core.GetProjectArgs{
		ProjectId: &project,
	})
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-validate-project", 500)
		return fmt.Errorf("failed to access project: %w", err)
	}

	// Report success to adaptive rate limiter
	s.adaptiveRateLimiter.ReportSuccess("ado-validate-project")

	return nil
}

// HasPermission checks if the user has a specific permission
func (s *AzureDevOpsService) HasPermission(ctx context.Context, clientID, clientSecret, tenantID, organization, permission string) bool {
	permissions, err := s.ValidateCredentials(ctx, clientID, clientSecret, tenantID, organization)
	if err != nil {
		return false
	}

	for _, p := range permissions {
		if p == permission {
			return true
		}
	}

	return false
}

// AdoRepositoryInfo represents Azure DevOps repository information
type AdoRepositoryInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"defaultBranch"`
	URL           string `json:"url"`
	Size          int64  `json:"size"`
	IsDisabled    bool   `json:"isDisabled"`
}

// GetRepositoryInfo gets detailed information about a specific repository
func (s *AzureDevOpsService) GetRepositoryInfo(ctx context.Context, organization, project, repositoryNameOrId, clientID, clientSecret, tenantID string) (*AdoRepositoryInfo, error) {
	if err := s.rateLimiter.Wait(ctx, "ado-get-repo-info"); err != nil {
		return nil, err
	}

	// Get connection
	connection, err := s.getConnection(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}

	// Create git client
	gitClient, err := git.NewClient(ctx, connection)
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-get-repo-info", 500)
		return nil, fmt.Errorf("failed to create git client: %w", err)
	}

	// Get repository details
	repo, err := gitClient.GetRepository(ctx, git.GetRepositoryArgs{
		Project:      &project,
		RepositoryId: &repositoryNameOrId,
	})
	if err != nil {
		s.adaptiveRateLimiter.ReportError("ado-get-repo-info", 500)
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	if repo == nil {
		return nil, fmt.Errorf("repository not found: %s", repositoryNameOrId)
	}

	s.adaptiveRateLimiter.ReportSuccess("ado-get-repo-info")

	// Convert to our structure
	repoInfo := &AdoRepositoryInfo{
		ID:            repo.Id.String(),
		Name:          *repo.Name,
		URL:           *repo.RemoteUrl,
		Size:          int64(*repo.Size),
		IsDisabled:    *repo.IsDisabled,
		DefaultBranch: "",
	}

	// Get default branch - this is typically stored in the repository object
	if repo.DefaultBranch != nil {
		// Remove refs/heads/ prefix if present
		defaultBranch := *repo.DefaultBranch
		if strings.HasPrefix(defaultBranch, "refs/heads/") {
			defaultBranch = strings.TrimPrefix(defaultBranch, "refs/heads/")
		}
		repoInfo.DefaultBranch = defaultBranch
	} else {
		// If default branch is not set, try to detect common default branches
		repoInfo.DefaultBranch = s.detectDefaultBranch(ctx, gitClient, project, repositoryNameOrId)
	}

	return repoInfo, nil
}

// detectDefaultBranch tries to detect the default branch by checking common branch names
func (s *AzureDevOpsService) detectDefaultBranch(ctx context.Context, gitClient git.Client, project, repositoryId string) string {
	commonBranches := []string{"main", "master", "develop", "development"}

	for _, branchName := range commonBranches {
		_, err := gitClient.GetBranch(ctx, git.GetBranchArgs{
			Project:      &project,
			RepositoryId: &repositoryId,
			Name:         &branchName,
		})
		if err == nil {
			return branchName
		}
	}

	// If no common branches found, return empty string
	return ""
}

// Helper functions

// getToken gets an Azure AD token for Azure DevOps
func (s *AzureDevOpsService) getToken(ctx context.Context, clientID, clientSecret, tenantID string) (string, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("%s:%s:%s", clientID, tenantID, clientSecret[:4])

	s.tokenCacheMutex.RLock()
	entry, exists := s.tokenCache[cacheKey]
	s.tokenCacheMutex.RUnlock()

	if exists && time.Now().Before(entry.expiresAt) {
		return entry.token, nil
	}

	// Create Azure credential
	cred, err := azidentity.NewClientSecretCredential(tenantID, clientID, clientSecret, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create credential: %w", err)
	}

	// Get access token for Azure DevOps - Using policy.TokenRequestOptions
	tokenResponse, err := cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"499b84ac-1321-427f-aa17-267ca6975798/.default"}, // Azure DevOps scope
	})
	if err != nil {
		return "", fmt.Errorf("failed to get token: %w", err)
	}

	// Cache the token
	s.tokenCacheMutex.Lock()
	s.tokenCache[cacheKey] = tokenCacheEntry{
		token:     tokenResponse.Token,
		expiresAt: tokenResponse.ExpiresOn,
	}
	s.tokenCacheMutex.Unlock()

	return tokenResponse.Token, nil
}

// getConnection gets or creates an Azure DevOps connection
func (s *AzureDevOpsService) getConnection(ctx context.Context, organization, clientID, clientSecret, tenantID string) (*azuredevops.Connection, error) {
	// Generate cache key
	cacheKey := fmt.Sprintf("%s:%s:%s:%s", organization, clientID, tenantID, clientSecret[:4])

	// Check cache first
	s.cacheMutex.RLock()
	connection, exists := s.connectionCache[cacheKey]
	s.cacheMutex.RUnlock()

	if exists {
		return connection, nil
	}

	// Get token
	token, err := s.getToken(ctx, clientID, clientSecret, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	// Create Azure DevOps connection
	organizationURL := fmt.Sprintf("https://dev.azure.com/%s", organization)
	connection = azuredevops.NewAnonymousConnection(organizationURL)
	connection.AuthorizationString = "Bearer " + token

	// Cache the connection
	s.cacheMutex.Lock()
	s.connectionCache[cacheKey] = connection
	s.cacheMutex.Unlock()

	return connection, nil
}

// CleanupConnectionCache removes old connections from the cache
func (s *AzureDevOpsService) CleanupConnectionCache() {
	s.cacheMutex.Lock()
	defer s.cacheMutex.Unlock()

	// In a real implementation, you would track last usage time
	// and remove connections that haven't been used recently
	if len(s.connectionCache) > 100 {
		s.connectionCache = make(map[string]*azuredevops.Connection)
	}
}

// CleanupTokenCache removes expired tokens from the cache
func (s *AzureDevOpsService) CleanupTokenCache() {
	s.tokenCacheMutex.Lock()
	defer s.tokenCacheMutex.Unlock()

	now := time.Now()
	for key, entry := range s.tokenCache {
		if now.After(entry.expiresAt) {
			delete(s.tokenCache, key)
		}
	}
}

// GetRepositoryDefaultBranch gets the default branch of an Azure DevOps repository
func (s *AzureDevOpsService) GetRepositoryDefaultBranch(ctx context.Context, token, organization, project, repositoryName string) (string, error) {
	if err := s.rateLimiter.Wait(ctx, fmt.Sprintf("ado-repo-info-%s", organization)); err != nil {
		return "", err
	}

	// Get connection using PAT
	connection := azuredevops.NewAnonymousConnection(fmt.Sprintf("https://dev.azure.com/%s", organization))
	connection.AuthorizationString = "Basic " + token

	// Get Git client
	gitClient, err := git.NewClient(ctx, connection)
	if err != nil {
		return "", fmt.Errorf("failed to create git client: %w", err)
	}

	// Get repository information
	repo, err := gitClient.GetRepository(ctx, git.GetRepositoryArgs{
		RepositoryId: &repositoryName,
		Project:      &project,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get repository info: %w", err)
	}

	if repo == nil || repo.DefaultBranch == nil {
		return "main", nil // Default fallback
	}

	// Extract branch name from refs/heads/branch-name format
	defaultBranch := *repo.DefaultBranch
	defaultBranch = strings.TrimPrefix(defaultBranch, "refs/heads/")

	return defaultBranch, nil
}
