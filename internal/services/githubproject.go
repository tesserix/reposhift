package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shurcooL/graphql"
	"golang.org/x/oauth2"
)

// GitHubProjectService handles GitHub Projects v2 operations
type GitHubProjectService struct {
	githubService *GitHubService
}

// NewGitHubProjectService creates a new GitHub Project service
func NewGitHubProjectService() *GitHubProjectService {
	return &GitHubProjectService{
		githubService: NewGitHubService(),
	}
}

// ProjectTemplate represents a GitHub Project template
type ProjectTemplate string

const (
	TemplateTeamPlanning         ProjectTemplate = "team-planning"
	TemplateKanban               ProjectTemplate = "kanban"
	TemplateFeatureRelease       ProjectTemplate = "feature-release"
	TemplateBugTracker           ProjectTemplate = "bug-tracker"
	TemplateIterativeDevelopment ProjectTemplate = "iterative-development"
	TemplateProductLaunch        ProjectTemplate = "product-launch"
	TemplateRoadmap              ProjectTemplate = "roadmap"
	TemplateTeamRetrospective    ProjectTemplate = "team-retrospective"
	TemplateBoard                ProjectTemplate = "board"
	TemplateBlank                ProjectTemplate = "blank"
)

// ProjectInfo represents information about a GitHub Project
type ProjectInfo struct {
	ID               string
	Number           int
	URL              string
	Title            string
	ShortDescription string
	Public           bool
}

// CreateProjectRequest represents a request to create a GitHub Project
type CreateProjectRequest struct {
	Owner       string
	ProjectName string
	Description string
	Template    ProjectTemplate
	Public      bool
	Repository  string // Optional: "owner/repo" format
	Token       string
}

// CreateProject creates a new GitHub Project v2
func (s *GitHubProjectService) CreateProject(ctx context.Context, req *CreateProjectRequest) (*ProjectInfo, error) {
	if req.Token == "" {
		return nil, fmt.Errorf("GitHub token is required")
	}

	// Create GraphQL client with token
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: req.Token},
	))
	client := graphql.NewClient("https://api.github.com/graphql", httpClient)

	// First, get the owner ID (organization or user)
	ownerID, _, err := s.getOwnerID(ctx, client, req.Owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner ID: %w", err)
	}

	// Build GraphQL mutation - only use fields that exist in CreateProjectV2Input
	query := `mutation($input: CreateProjectV2Input!) {
		createProjectV2(input: $input) {
			projectV2 {
				id
				number
				url
				title
				shortDescription
				public
			}
		}
	}`

	// Build input with only valid CreateProjectV2Input fields
	input := map[string]interface{}{
		"ownerId": ownerID,
		"title":   req.ProjectName,
	}

	// Add repository link if specified
	if req.Repository != "" {
		// We need to get repository ID first
		repoID, err := s.getRepositoryID(ctx, client, req.Repository)
		if err != nil {
			fmt.Printf("⚠️ Warning: Could not get repository ID: %v\n", err)
		} else {
			input["repositoryId"] = repoID
		}
	}

	// Create request body
	reqBody := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"input": input,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+req.Token)
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	httpClient = &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response
	var graphQLResp struct {
		Data struct {
			CreateProjectV2 struct {
				ProjectV2 struct {
					ID               string `json:"id"`
					Number           int    `json:"number"`
					URL              string `json:"url"`
					Title            string `json:"title"`
					ShortDescription string `json:"shortDescription"`
					Public           bool   `json:"public"`
				} `json:"projectV2"`
			} `json:"createProjectV2"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL errors: %v", graphQLResp.Errors)
	}

	projectInfo := &ProjectInfo{
		ID:               graphQLResp.Data.CreateProjectV2.ProjectV2.ID,
		Number:           graphQLResp.Data.CreateProjectV2.ProjectV2.Number,
		URL:              graphQLResp.Data.CreateProjectV2.ProjectV2.URL,
		Title:            graphQLResp.Data.CreateProjectV2.ProjectV2.Title,
		ShortDescription: graphQLResp.Data.CreateProjectV2.ProjectV2.ShortDescription,
		Public:           graphQLResp.Data.CreateProjectV2.ProjectV2.Public,
	}

	fmt.Printf("✅ Created GitHub Project: %s (ID: %s, Number: %d)\n", projectInfo.Title, projectInfo.ID, projectInfo.Number)
	fmt.Printf("📊 Project URL: %s\n", projectInfo.URL)

	// Note: Repository was linked using repositoryId in the input if specified
	// Template, description, and visibility settings are not supported in createProjectV2
	// and would need to be set using updateProjectV2 mutation after creation

	return projectInfo, nil
}

// StatusFieldOption represents a single option in a status field
type StatusFieldOption struct {
	Name        string
	Description string
	Color       string
}

// ConfigureStatusField creates a custom status field with options for a blank project
func (s *GitHubProjectService) ConfigureStatusField(ctx context.Context, token, projectID, fieldName string, options []StatusFieldOption) error {
	if token == "" {
		return fmt.Errorf("GitHub token is required")
	}

	if len(options) == 0 {
		return fmt.Errorf("at least one status option is required")
	}

	fmt.Printf("🔧 Creating status field '%s' with %d options for project %s\n", fieldName, len(options), projectID)

	// Build singleSelectOptions array
	singleSelectOptions := make([]map[string]interface{}, 0, len(options))
	for _, opt := range options {
		option := map[string]interface{}{
			"name": opt.Name,
		}
		// Note: description and color support may vary based on GitHub API version
		if opt.Description != "" {
			option["description"] = opt.Description
		}
		if opt.Color != "" {
			option["color"] = strings.ToUpper(opt.Color)
		}
		singleSelectOptions = append(singleSelectOptions, option)
	}

	// Use createProjectV2Field mutation to create a new field
	query := `mutation($input: CreateProjectV2FieldInput!) {
		createProjectV2Field(input: $input) {
			projectV2Field {
				... on ProjectV2SingleSelectField {
					id
					name
					options {
						id
						name
					}
				}
			}
		}
	}`

	input := map[string]interface{}{
		"projectId":           projectID,
		"name":                fieldName,
		"dataType":            "SINGLE_SELECT",
		"singleSelectOptions": singleSelectOptions,
	}

	reqBody := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"input": input,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse response
	var graphQLResp struct {
		Data struct {
			CreateProjectV2Field struct {
				ProjectV2Field struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Options []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"options"`
				} `json:"projectV2Field"`
			} `json:"createProjectV2Field"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return fmt.Errorf("GraphQL errors: %v", graphQLResp.Errors)
	}

	fmt.Printf("✅ Created status field '%s' with %d options\n", fieldName, len(options))
	for i, opt := range options {
		fmt.Printf("   %d. %s", i+1, opt.Name)
		if opt.Color != "" {
			fmt.Printf(" [%s]", opt.Color)
		}
		fmt.Println()
	}

	return nil
}

// AddIssueToProject adds an issue to a GitHub Project
func (s *GitHubProjectService) AddIssueToProject(ctx context.Context, token, projectID, issueNodeID string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("GitHub token is required")
	}

	mutation := `mutation($input: AddProjectV2ItemByIdInput!) {
		addProjectV2ItemById(input: $input) {
			item {
				id
			}
		}
	}`

	input := map[string]interface{}{
		"projectId": projectID,
		"contentId": issueNodeID,
	}

	reqBody := map[string]interface{}{
		"query": mutation,
		"variables": map[string]interface{}{
			"input": input,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var graphQLResp struct {
		Data struct {
			AddProjectV2ItemById struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"addProjectV2ItemById"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return "", fmt.Errorf("GraphQL errors: %v", graphQLResp.Errors)
	}

	itemID := graphQLResp.Data.AddProjectV2ItemById.Item.ID
	return itemID, nil
}

// BatchAddIssuesToProject adds multiple issues to a GitHub Project with chunked batching
// Returns a map of issueNodeID -> projectItemID for successful additions
func (s *GitHubProjectService) BatchAddIssuesToProject(ctx context.Context, token, projectID string, issueNodeIDs []string) (map[string]string, error) {
	if token == "" {
		return nil, fmt.Errorf("GitHub token is required")
	}

	if len(issueNodeIDs) == 0 {
		return make(map[string]string), nil
	}

	// Process in chunks to avoid GitHub GraphQL resource limits
	const chunkSize = 25
	const delayBetweenChunks = 3 * time.Second

	totalChunks := (len(issueNodeIDs) + chunkSize - 1) / chunkSize
	allResults := make(map[string]string)

	fmt.Printf("📋 Adding %d issues to project in %d chunks of %d items\n", len(issueNodeIDs), totalChunks, chunkSize)

	for chunkIndex := 0; chunkIndex < totalChunks; chunkIndex++ {
		start := chunkIndex * chunkSize
		end := start + chunkSize
		if end > len(issueNodeIDs) {
			end = len(issueNodeIDs)
		}

		chunk := issueNodeIDs[start:end]
		fmt.Printf("📦 Processing chunk %d/%d (%d items)...\n", chunkIndex+1, totalChunks, len(chunk))

		// Build batched mutation for this chunk
		var mutationParts []string
		for i, nodeID := range chunk {
			alias := fmt.Sprintf("add%d", i)
			mutationParts = append(mutationParts, fmt.Sprintf(`
				%s: addProjectV2ItemById(input: {projectId: "%s", contentId: "%s"}) {
					item {
						id
					}
				}`, alias, projectID, nodeID))
		}

		mutation := fmt.Sprintf("mutation { %s }", strings.Join(mutationParts, "\n"))

		reqBody := map[string]interface{}{
			"query": mutation,
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return allResults, fmt.Errorf("failed to marshal request for chunk %d: %w", chunkIndex+1, err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
		if err != nil {
			return allResults, fmt.Errorf("failed to create HTTP request for chunk %d: %w", chunkIndex+1, err)
		}

		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Content-Type", "application/json")

		httpClient := &http.Client{}
		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return allResults, fmt.Errorf("failed to send HTTP request for chunk %d: %w", chunkIndex+1, err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return allResults, fmt.Errorf("failed to read response body for chunk %d: %w", chunkIndex+1, err)
		}

		var graphQLResp struct {
			Data map[string]struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}

		if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
			return allResults, fmt.Errorf("failed to parse response for chunk %d: %w", chunkIndex+1, err)
		}

		if len(graphQLResp.Errors) > 0 {
			fmt.Printf("⚠️  Chunk %d had errors: %v\n", chunkIndex+1, graphQLResp.Errors)
			return allResults, fmt.Errorf("GraphQL errors in chunk %d: %v", chunkIndex+1, graphQLResp.Errors)
		}

		// Collect results from this chunk
		chunkResults := 0
		for i, nodeID := range chunk {
			alias := fmt.Sprintf("add%d", i)
			if data, ok := graphQLResp.Data[alias]; ok {
				allResults[nodeID] = data.Item.ID
				chunkResults++
			}
		}

		fmt.Printf("✅ Chunk %d/%d completed: added %d items to project\n", chunkIndex+1, totalChunks, chunkResults)

		// Add delay between chunks (except after the last chunk)
		if chunkIndex < totalChunks-1 {
			fmt.Printf("⏸️  Waiting %v before next chunk...\n", delayBetweenChunks)
			time.Sleep(delayBetweenChunks)
		}
	}

	fmt.Printf("✅ Successfully added %d/%d issues to project\n", len(allResults), len(issueNodeIDs))
	return allResults, nil
}

// BatchSetProjectItemStatus sets the status field for multiple project items in a single GraphQL call
func (s *GitHubProjectService) BatchSetProjectItemStatus(ctx context.Context, token, projectID, fieldID string, items map[string]string) error {
	if token == "" {
		return fmt.Errorf("GitHub token is required")
	}

	if len(items) == 0 {
		return nil
	}

	// Build batched mutation with aliases
	// items map: projectItemID -> optionID
	var mutationParts []string
	i := 0
	for itemID, optionID := range items {
		alias := fmt.Sprintf("update%d", i)
		mutationParts = append(mutationParts, fmt.Sprintf(`
			%s: updateProjectV2ItemFieldValue(input: {
				projectId: "%s",
				itemId: "%s",
				fieldId: "%s",
				value: {singleSelectOptionId: "%s"}
			}) {
				projectV2Item {
					id
				}
			}`, alias, projectID, itemID, fieldID, optionID))
		i++
	}

	mutation := fmt.Sprintf("mutation { %s }", strings.Join(mutationParts, "\n"))

	reqBody := map[string]interface{}{
		"query": mutation,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var graphQLResp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return fmt.Errorf("GraphQL errors: %v", graphQLResp.Errors)
	}

	return nil
}

// SetProjectItemStatus sets the status field value for a project item
func (s *GitHubProjectService) SetProjectItemStatus(ctx context.Context, token, projectID, itemID, fieldID, optionID string) error {
	if token == "" {
		return fmt.Errorf("GitHub token is required")
	}

	mutation := `mutation($input: UpdateProjectV2ItemFieldValueInput!) {
		updateProjectV2ItemFieldValue(input: $input) {
			projectV2Item {
				id
			}
		}
	}`

	input := map[string]interface{}{
		"projectId": projectID,
		"itemId":    itemID,
		"fieldId":   fieldID,
		"value": map[string]interface{}{
			"singleSelectOptionId": optionID,
		},
	}

	reqBody := map[string]interface{}{
		"query": mutation,
		"variables": map[string]interface{}{
			"input": input,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var graphQLResp struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return fmt.Errorf("GraphQL errors: %v", graphQLResp.Errors)
	}

	return nil
}

// GetProjectFieldID retrieves the field ID for a custom field by name
func (s *GitHubProjectService) GetProjectFieldID(ctx context.Context, token, projectID, fieldName string) (string, map[string]string, error) {
	if token == "" {
		return "", nil, fmt.Errorf("GitHub token is required")
	}

	query := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				fields(first: 20) {
					nodes {
						... on ProjectV2SingleSelectField {
							id
							name
							options {
								id
								name
							}
						}
					}
				}
			}
		}
	}`

	reqBody := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"projectId": projectID,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("failed to send HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var graphQLResp struct {
		Data struct {
			Node struct {
				Fields struct {
					Nodes []struct {
						ID      string `json:"id"`
						Name    string `json:"name"`
						Options []struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"options"`
					} `json:"nodes"`
				} `json:"fields"`
			} `json:"node"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &graphQLResp); err != nil {
		return "", nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return "", nil, fmt.Errorf("GraphQL errors: %v", graphQLResp.Errors)
	}

	// Find the field by name and return its ID and options map
	for _, field := range graphQLResp.Data.Node.Fields.Nodes {
		if field.Name == fieldName {
			optionsMap := make(map[string]string)
			for _, opt := range field.Options {
				optionsMap[opt.Name] = opt.ID
			}
			return field.ID, optionsMap, nil
		}
	}

	return "", nil, fmt.Errorf("field '%s' not found in project", fieldName)
}

// GetProjectByNumber retrieves a project by its number
func (s *GitHubProjectService) GetProjectByNumber(ctx context.Context, token, owner string, number int) (*ProjectInfo, error) {
	if token == "" {
		return nil, fmt.Errorf("GitHub token is required")
	}

	// Create GraphQL client with token
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
	client := graphql.NewClient("https://api.github.com/graphql", httpClient)

	// Get owner type to use correct query
	_, ownerType, err := s.getOwnerID(ctx, client, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner type: %w", err)
	}

	if ownerType == "Organization" {
		return s.getOrgProject(ctx, client, owner, number)
	}
	return s.getUserProject(ctx, client, owner, number)
}

// getOwnerID retrieves the node ID for an organization or user
func (s *GitHubProjectService) getOwnerID(ctx context.Context, client *graphql.Client, owner string) (string, string, error) {
	// Try as organization first
	var orgQuery struct {
		Organization struct {
			ID graphql.String
		} `graphql:"organization(login: $login)"`
	}

	variables := map[string]interface{}{
		"login": graphql.String(owner),
	}

	err := client.Query(ctx, &orgQuery, variables)
	if err == nil && orgQuery.Organization.ID != "" {
		return string(orgQuery.Organization.ID), "Organization", nil
	}

	// Try as user
	var userQuery struct {
		User struct {
			ID graphql.String
		} `graphql:"user(login: $login)"`
	}

	err = client.Query(ctx, &userQuery, variables)
	if err != nil {
		return "", "", fmt.Errorf("owner '%s' not found as organization or user: %w", owner, err)
	}

	return string(userQuery.User.ID), "User", nil
}

// getOrgProject retrieves an organization project
func (s *GitHubProjectService) getOrgProject(ctx context.Context, client *graphql.Client, owner string, number int) (*ProjectInfo, error) {
	var query struct {
		Organization struct {
			ProjectV2 struct {
				ID               graphql.String
				Number           graphql.Int
				URL              graphql.String
				Title            graphql.String
				ShortDescription graphql.String
				Public           graphql.Boolean
			} `graphql:"projectV2(number: $number)"`
		} `graphql:"organization(login: $login)"`
	}

	variables := map[string]interface{}{
		"login":  graphql.String(owner),
		"number": graphql.Int(number),
	}

	err := client.Query(ctx, &query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	return &ProjectInfo{
		ID:               string(query.Organization.ProjectV2.ID),
		Number:           int(query.Organization.ProjectV2.Number),
		URL:              string(query.Organization.ProjectV2.URL),
		Title:            string(query.Organization.ProjectV2.Title),
		ShortDescription: string(query.Organization.ProjectV2.ShortDescription),
		Public:           bool(query.Organization.ProjectV2.Public),
	}, nil
}

// getUserProject retrieves a user project
func (s *GitHubProjectService) getUserProject(ctx context.Context, client *graphql.Client, owner string, number int) (*ProjectInfo, error) {
	var query struct {
		User struct {
			ProjectV2 struct {
				ID               graphql.String
				Number           graphql.Int
				URL              graphql.String
				Title            graphql.String
				ShortDescription graphql.String
				Public           graphql.Boolean
			} `graphql:"projectV2(number: $number)"`
		} `graphql:"user(login: $login)"`
	}

	variables := map[string]interface{}{
		"login":  graphql.String(owner),
		"number": graphql.Int(number),
	}

	err := client.Query(ctx, &query, variables)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}

	return &ProjectInfo{
		ID:               string(query.User.ProjectV2.ID),
		Number:           int(query.User.ProjectV2.Number),
		URL:              string(query.User.ProjectV2.URL),
		Title:            string(query.User.ProjectV2.Title),
		ShortDescription: string(query.User.ProjectV2.ShortDescription),
		Public:           bool(query.User.ProjectV2.Public),
	}, nil
}

// getRepositoryID retrieves the node ID for a repository
func (s *GitHubProjectService) getRepositoryID(ctx context.Context, client *graphql.Client, repository string) (string, error) {
	// Parse repository (owner/repo format)
	parts := strings.Split(repository, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repository format, expected 'owner/repo'")
	}

	// Get repository ID
	var repoQuery struct {
		Repository struct {
			ID graphql.String
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	variables := map[string]interface{}{
		"owner": graphql.String(parts[0]),
		"name":  graphql.String(parts[1]),
	}

	err := client.Query(ctx, &repoQuery, variables)
	if err != nil {
		return "", fmt.Errorf("failed to get repository ID: %w", err)
	}

	return string(repoQuery.Repository.ID), nil
}

// linkRepositoryToProject links a repository to a project
func (s *GitHubProjectService) linkRepositoryToProject(ctx context.Context, client *graphql.Client, projectID, repository string) error {
	repoID, err := s.getRepositoryID(ctx, client, repository)
	if err != nil {
		return err
	}

	// Note: Linking repositories to projects is done through the project settings
	// and doesn't have a direct GraphQL mutation. The project will need to be
	// configured through the UI or through workflows to automatically add issues
	// from specific repositories.

	fmt.Printf("📎 Repository %s (ID: %s) can be linked to project in GitHub UI\n", repository, repoID)
	return nil
}

// getTemplateID maps template names to GitHub template IDs
func (s *GitHubProjectService) getTemplateID(template ProjectTemplate) string {
	// GitHub Projects v2 templates
	// These are the built-in templates available in GitHub
	templates := map[ProjectTemplate]string{
		TemplateTeamPlanning:         "Team planning",
		TemplateKanban:               "Kanban",
		TemplateFeatureRelease:       "Feature release",
		TemplateBugTracker:           "Bug tracker",
		TemplateIterativeDevelopment: "Iterative development",
		TemplateProductLaunch:        "Product launch",
		TemplateRoadmap:              "Roadmap",
		TemplateTeamRetrospective:    "Team retrospective",
		TemplateBoard:                "Board",
		TemplateBlank:                "",
	}

	return templates[template]
}

// GetIssueNodeID retrieves the node ID for an issue
func (s *GitHubProjectService) GetIssueNodeID(ctx context.Context, token, owner, repo string, issueNumber int) (string, error) {
	if token == "" {
		return "", fmt.Errorf("GitHub token is required")
	}

	// Create GraphQL client with token
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
	client := graphql.NewClient("https://api.github.com/graphql", httpClient)

	var query struct {
		Repository struct {
			Issue struct {
				ID graphql.String
			} `graphql:"issue(number: $number)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	variables := map[string]interface{}{
		"owner":  graphql.String(owner),
		"name":   graphql.String(repo),
		"number": graphql.Int(issueNumber),
	}

	err := client.Query(ctx, &query, variables)
	if err != nil {
		return "", fmt.Errorf("failed to get issue node ID: %w", err)
	}

	return string(query.Repository.Issue.ID), nil
}

// DeleteProject deletes a GitHub Project
func (s *GitHubProjectService) DeleteProject(ctx context.Context, token, projectID string) error {
	if token == "" {
		return fmt.Errorf("GitHub token is required")
	}

	// Create GraphQL client with token
	httpClient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
	client := graphql.NewClient("https://api.github.com/graphql", httpClient)

	var mutation struct {
		DeleteProjectV2 struct {
			ProjectV2 struct {
				ID graphql.String
			}
		} `graphql:"deleteProjectV2(input: $input)"`
	}

	inputFields := map[string]interface{}{
		"projectId": graphql.ID(projectID),
	}

	variables := map[string]interface{}{
		"input": inputFields,
	}

	err := client.Mutate(ctx, &mutation, variables)
	if err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}

	fmt.Printf("🗑️  Deleted GitHub Project: %s\n", projectID)
	return nil
}

// ProjectExists checks if a project exists
func (s *GitHubProjectService) ProjectExists(ctx context.Context, token, owner string, number int) (bool, error) {
	_, err := s.GetProjectByNumber(ctx, token, owner, number)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CheckRepositoryExists checks if a GitHub repository exists
// This is a wrapper around GitHubService.CheckRepositoryExists for use by WorkItemMigration controller
func (s *GitHubProjectService) CheckRepositoryExists(ctx context.Context, token, owner, repoName string) (bool, error) {
	return s.githubService.CheckRepositoryExists(ctx, token, owner, repoName)
}

// GetGitHubAppToken retrieves a GitHub App installation token
// This is used by WorkItemMigration controller to get tokens for API calls
func (s *GitHubProjectService) GetGitHubAppToken(ctx context.Context, appAuth interface{}, namespace string) (string, error) {
	// Import the necessary types to handle appAuth
	// Since appAuth comes from the API types, we need to handle it properly

	// For now, return an error indicating that GitHub App token retrieval needs to be
	// implemented through the controller's client access to secrets
	return "", fmt.Errorf("GitHub App token retrieval should be done through controller client access to secrets - use the existing secret reading pattern in validateConfiguration")
}

// MarshalJSON is a helper for debugging
func (p *ProjectInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"id":          p.ID,
		"number":      p.Number,
		"url":         p.URL,
		"title":       p.Title,
		"description": p.ShortDescription,
		"public":      p.Public,
	})
}
