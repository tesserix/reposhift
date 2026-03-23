package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/time/rate"
)

const (
	githubGraphQLEndpoint = "https://api.github.com/graphql"

	// maxBatchSize is the maximum number of mutations in a single GraphQL request.
	// GitHub's GraphQL API allows up to 500,000 nodes per query, but aliased mutations
	// have a practical limit around 20 before request complexity becomes an issue.
	maxBatchSize = 20
)

// GitHubGraphQLClient provides efficient batch operations via GitHub's GraphQL API v4.
// Use this for operations that benefit from batching or need to avoid N+1 REST patterns.
// For simple CRUD operations, the REST client (github.go) is simpler and preferred.
type GitHubGraphQLClient struct {
	httpClient  *http.Client
	endpoint    string
	rateLimiter *rate.Limiter
}

// GraphQLRepoInfo contains repository metadata returned from a single GraphQL query.
type GraphQLRepoInfo struct {
	ID               string
	Name             string
	DefaultBranch    string
	IsEmpty          bool
	DiskUsageKB      int
	IsPrivate        bool
	IsArchived       bool
	HasIssuesEnabled bool
	URL              string
}

// IssueInput describes an issue to be created via batch mutation.
type IssueInput struct {
	Title     string
	Body      string
	Labels    []string // label node IDs (not names)
	Assignees []string // user node IDs
}

// CreatedIssue holds the result of a successfully created issue.
type CreatedIssue struct {
	ID     string
	Number int
	URL    string
}

// LabelInput describes a label to ensure exists on a repository.
type LabelInput struct {
	Name        string
	Color       string // hex color without leading '#'
	Description string
}

// GraphQLRateLimit contains the current GraphQL API rate limit status.
type GraphQLRateLimit struct {
	Limit     int
	Remaining int
	Cost      int
	ResetAt   time.Time
}

// graphQLRequest is the JSON body sent to the GraphQL endpoint.
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// graphQLResponse is the top-level JSON envelope for all GraphQL responses.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors,omitempty"`
}

// graphQLError represents a single error returned by the GraphQL API.
type graphQLError struct {
	Message   string        `json:"message"`
	Type      string        `json:"type"`
	Path      []interface{} `json:"path,omitempty"`
	Locations []struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"locations,omitempty"`
}

// NewGitHubGraphQLClient creates a GraphQL client authenticated with a personal access token.
// The client uses an oauth2 transport for automatic bearer token injection and a rate limiter
// set to 4,500 points/hour (below GitHub's 5,000 point/hour limit to provide headroom).
func NewGitHubGraphQLClient(token string) *GitHubGraphQLClient {
	httpClient := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
	return &GitHubGraphQLClient{
		httpClient:  httpClient,
		endpoint:    githubGraphQLEndpoint,
		rateLimiter: rate.NewLimiter(rate.Every(time.Hour/4500), 50),
	}
}

// NewGitHubGraphQLClientWithHTTPClient creates a GraphQL client with a pre-configured HTTP client.
// This is useful when the caller already has an oauth2 client or needs custom transport settings.
func NewGitHubGraphQLClientWithHTTPClient(httpClient *http.Client) *GitHubGraphQLClient {
	return &GitHubGraphQLClient{
		httpClient:  httpClient,
		endpoint:    githubGraphQLEndpoint,
		rateLimiter: rate.NewLimiter(rate.Every(time.Hour/4500), 50),
	}
}

// GetRepositoryInfo returns repo metadata, default branch, empty status, and size in a single query.
// This replaces 2-4 REST calls (Get repo + Get branch + size check).
func (c *GitHubGraphQLClient) GetRepositoryInfo(ctx context.Context, owner, name string) (*GraphQLRepoInfo, error) {
	query := `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    id
    name
    defaultBranchRef { name }
    isEmpty
    diskUsage
    isPrivate
    isArchived
    hasIssuesEnabled
    url
  }
}`

	variables := map[string]interface{}{
		"owner": owner,
		"name":  name,
	}

	var result struct {
		Repository struct {
			ID               string `json:"id"`
			Name             string `json:"name"`
			DefaultBranchRef *struct {
				Name string `json:"name"`
			} `json:"defaultBranchRef"`
			IsEmpty          bool   `json:"isEmpty"`
			DiskUsage        int    `json:"diskUsage"`
			IsPrivate        bool   `json:"isPrivate"`
			IsArchived       bool   `json:"isArchived"`
			HasIssuesEnabled bool   `json:"hasIssuesEnabled"`
			URL              string `json:"url"`
		} `json:"repository"`
	}

	if err := c.execute(ctx, query, variables, &result); err != nil {
		return nil, fmt.Errorf("GetRepositoryInfo %s/%s: %w", owner, name, err)
	}

	defaultBranch := ""
	if result.Repository.DefaultBranchRef != nil {
		defaultBranch = result.Repository.DefaultBranchRef.Name
	}

	return &GraphQLRepoInfo{
		ID:               result.Repository.ID,
		Name:             result.Repository.Name,
		DefaultBranch:    defaultBranch,
		IsEmpty:          result.Repository.IsEmpty,
		DiskUsageKB:      result.Repository.DiskUsage,
		IsPrivate:        result.Repository.IsPrivate,
		IsArchived:       result.Repository.IsArchived,
		HasIssuesEnabled: result.Repository.HasIssuesEnabled,
		URL:              result.Repository.URL,
	}, nil
}

// GetRepositoryID returns the GraphQL node ID for a repository.
// This ID is required for mutations like createIssue and createLabel.
func (c *GitHubGraphQLClient) GetRepositoryID(ctx context.Context, owner, name string) (string, error) {
	info, err := c.GetRepositoryInfo(ctx, owner, name)
	if err != nil {
		return "", err
	}
	return info.ID, nil
}

// BatchCreateIssues creates up to 20 issues in a single GraphQL mutation using aliases.
// Returns created issue IDs and numbers. Falls back to individual creation on partial failure.
// Each issue costs ~10 GraphQL points, so 20 issues = ~200 points vs 20 REST calls = 20 REST points.
// Note: GraphQL issue creation is best for large batches where the reduced round-trips
// outweigh the higher per-mutation point cost.
func (c *GitHubGraphQLClient) BatchCreateIssues(ctx context.Context, repoID string, issues []IssueInput) ([]CreatedIssue, error) {
	if len(issues) == 0 {
		return nil, nil
	}
	if len(issues) > maxBatchSize {
		return nil, fmt.Errorf("batch size %d exceeds maximum of %d", len(issues), maxBatchSize)
	}

	mutation, variables := buildBatchIssueMutation(repoID, issues)

	var rawResult map[string]json.RawMessage
	if err := c.execute(ctx, mutation, variables, &rawResult); err != nil {
		slog.Warn("batch issue creation failed, falling back to individual creation",
			"error", err, "count", len(issues))
		return c.fallbackCreateIssues(ctx, repoID, issues)
	}

	created := make([]CreatedIssue, 0, len(issues))
	var errs []string

	for i := range issues {
		alias := fmt.Sprintf("issue%d", i)
		raw, ok := rawResult[alias]
		if !ok {
			errs = append(errs, fmt.Sprintf("missing response for %s", alias))
			continue
		}

		var issueResp struct {
			Issue struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				URL    string `json:"url"`
			} `json:"issue"`
		}
		if err := json.Unmarshal(raw, &issueResp); err != nil {
			errs = append(errs, fmt.Sprintf("failed to parse %s: %v", alias, err))
			continue
		}

		created = append(created, CreatedIssue{
			ID:     issueResp.Issue.ID,
			Number: issueResp.Issue.Number,
			URL:    issueResp.Issue.URL,
		})
	}

	if len(errs) > 0 {
		slog.Warn("partial failures in batch issue creation",
			"created", len(created), "errors", len(errs), "details", strings.Join(errs, "; "))
	}

	slog.Info("batch issue creation completed",
		"requested", len(issues), "created", len(created))

	return created, nil
}

// EnsureLabels gets existing labels and creates missing ones. Returns a map of label name to node ID.
// This replaces N REST calls (1 per label check + 1 per label create).
func (c *GitHubGraphQLClient) EnsureLabels(ctx context.Context, repoID string, labels []LabelInput) (map[string]string, error) {
	if len(labels) == 0 {
		return map[string]string{}, nil
	}

	// First, query existing labels on the repository.
	existing, err := c.fetchExistingLabels(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("EnsureLabels fetch existing: %w", err)
	}

	result := make(map[string]string, len(labels))
	var toCreate []LabelInput

	for _, l := range labels {
		if id, ok := existing[strings.ToLower(l.Name)]; ok {
			result[l.Name] = id
		} else {
			toCreate = append(toCreate, l)
		}
	}

	if len(toCreate) == 0 {
		return result, nil
	}

	// Batch create missing labels.
	if len(toCreate) > maxBatchSize {
		// Process in chunks if we exceed the batch limit.
		for i := 0; i < len(toCreate); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(toCreate) {
				end = len(toCreate)
			}
			created, err := c.batchCreateLabels(ctx, repoID, toCreate[i:end])
			if err != nil {
				return nil, fmt.Errorf("EnsureLabels batch create (chunk %d): %w", i/maxBatchSize, err)
			}
			for name, id := range created {
				result[name] = id
			}
		}
	} else {
		created, err := c.batchCreateLabels(ctx, repoID, toCreate)
		if err != nil {
			return nil, fmt.Errorf("EnsureLabels batch create: %w", err)
		}
		for name, id := range created {
			result[name] = id
		}
	}

	slog.Info("labels ensured", "total", len(result), "created", len(toCreate))
	return result, nil
}

// BatchCloseIssues closes multiple issues in a single mutation.
func (c *GitHubGraphQLClient) BatchCloseIssues(ctx context.Context, issueIDs []string) error {
	if len(issueIDs) == 0 {
		return nil
	}
	if len(issueIDs) > maxBatchSize {
		return fmt.Errorf("batch size %d exceeds maximum of %d", len(issueIDs), maxBatchSize)
	}

	var b strings.Builder
	b.WriteString("mutation {\n")
	for i, id := range issueIDs {
		fmt.Fprintf(&b, "  close%d: closeIssue(input: {issueId: %q}) {\n    issue { id state }\n  }\n", i, id)
	}
	b.WriteString("}")

	var rawResult map[string]json.RawMessage
	if err := c.execute(ctx, b.String(), nil, &rawResult); err != nil {
		return fmt.Errorf("BatchCloseIssues: %w", err)
	}

	slog.Info("batch close issues completed", "count", len(issueIDs))
	return nil
}

// GetRateLimit queries the current GraphQL API rate limit status.
func (c *GitHubGraphQLClient) GetRateLimit(ctx context.Context) (*GraphQLRateLimit, error) {
	query := `query {
  rateLimit {
    limit
    remaining
    cost
    resetAt
  }
}`

	var result struct {
		RateLimit struct {
			Limit     int    `json:"limit"`
			Remaining int    `json:"remaining"`
			Cost      int    `json:"cost"`
			ResetAt   string `json:"resetAt"`
		} `json:"rateLimit"`
	}

	if err := c.execute(ctx, query, nil, &result); err != nil {
		return nil, fmt.Errorf("GetRateLimit: %w", err)
	}

	resetAt, err := time.Parse(time.RFC3339, result.RateLimit.ResetAt)
	if err != nil {
		return nil, fmt.Errorf("GetRateLimit parse resetAt: %w", err)
	}

	return &GraphQLRateLimit{
		Limit:     result.RateLimit.Limit,
		Remaining: result.RateLimit.Remaining,
		Cost:      result.RateLimit.Cost,
		ResetAt:   resetAt,
	}, nil
}

// execute sends a GraphQL request, handles errors, and unmarshals the data field into result.
func (c *GitHubGraphQLClient) execute(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Log rate limit headers for observability.
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	if remaining != "" {
		rem, _ := strconv.Atoi(remaining)
		if rem < 500 {
			slog.Warn("GraphQL rate limit running low",
				"remaining", rem, "elapsed_ms", elapsed.Milliseconds())
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return &GraphQLAPIError{Errors: gqlResp.Errors}
	}

	slog.Debug("GraphQL query executed",
		"elapsed_ms", elapsed.Milliseconds(),
		"rate_remaining", remaining)

	if result != nil && gqlResp.Data != nil {
		if err := json.Unmarshal(gqlResp.Data, result); err != nil {
			return fmt.Errorf("unmarshal data: %w", err)
		}
	}

	return nil
}

// GraphQLAPIError wraps one or more GraphQL errors from the API response.
type GraphQLAPIError struct {
	Errors []graphQLError
}

func (e *GraphQLAPIError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("graphql: %s", e.Errors[0].Message)
	}
	msgs := make([]string, len(e.Errors))
	for i, err := range e.Errors {
		msgs[i] = err.Message
	}
	return fmt.Sprintf("graphql: %d errors: %s", len(e.Errors), strings.Join(msgs, "; "))
}

// IsPartialError returns true if some operations in a batch succeeded while others failed.
func (e *GraphQLAPIError) IsPartialError() bool {
	for _, err := range e.Errors {
		if len(err.Path) > 0 {
			return true
		}
	}
	return false
}

// buildBatchIssueMutation constructs an aliased mutation string and variables for batch issue creation.
func buildBatchIssueMutation(repoID string, issues []IssueInput) (string, map[string]interface{}) {
	var b strings.Builder
	b.WriteString("mutation {\n")

	for i, issue := range issues {
		inputParts := []string{
			fmt.Sprintf("repositoryId: %q", repoID),
			fmt.Sprintf("title: %q", issue.Title),
		}
		if issue.Body != "" {
			inputParts = append(inputParts, fmt.Sprintf("body: %q", issue.Body))
		}
		if len(issue.Labels) > 0 {
			labelList := make([]string, len(issue.Labels))
			for j, l := range issue.Labels {
				labelList[j] = fmt.Sprintf("%q", l)
			}
			inputParts = append(inputParts, fmt.Sprintf("labelIds: [%s]", strings.Join(labelList, ", ")))
		}
		if len(issue.Assignees) > 0 {
			assigneeList := make([]string, len(issue.Assignees))
			for j, a := range issue.Assignees {
				assigneeList[j] = fmt.Sprintf("%q", a)
			}
			inputParts = append(inputParts, fmt.Sprintf("assigneeIds: [%s]", strings.Join(assigneeList, ", ")))
		}

		fmt.Fprintf(&b, "  issue%d: createIssue(input: {%s}) {\n    issue { id number url }\n  }\n",
			i, strings.Join(inputParts, ", "))
	}

	b.WriteString("}")
	return b.String(), nil
}

// fetchExistingLabels queries all labels on a repository and returns a map of lowercase name to node ID.
func (c *GitHubGraphQLClient) fetchExistingLabels(ctx context.Context, repoNodeID string) (map[string]string, error) {
	// We need owner/name for the labels query, but we only have the node ID.
	// Use a node query to get labels via the repository node ID.
	query := `query($id: ID!, $cursor: String) {
  node(id: $id) {
    ... on Repository {
      labels(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes { id name }
      }
    }
  }
}`

	labels := make(map[string]string)
	var cursor *string

	for {
		variables := map[string]interface{}{
			"id": repoNodeID,
		}
		if cursor != nil {
			variables["cursor"] = *cursor
		}

		var result struct {
			Node struct {
				Labels struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
			} `json:"node"`
		}

		if err := c.execute(ctx, query, variables, &result); err != nil {
			return nil, err
		}

		for _, l := range result.Node.Labels.Nodes {
			labels[strings.ToLower(l.Name)] = l.ID
		}

		if !result.Node.Labels.PageInfo.HasNextPage {
			break
		}
		cursor = &result.Node.Labels.PageInfo.EndCursor
	}

	return labels, nil
}

// batchCreateLabels creates multiple labels in a single mutation and returns a map of name to node ID.
func (c *GitHubGraphQLClient) batchCreateLabels(ctx context.Context, repoID string, labels []LabelInput) (map[string]string, error) {
	var b strings.Builder
	b.WriteString("mutation {\n")

	for i, l := range labels {
		color := l.Color
		if strings.HasPrefix(color, "#") {
			color = color[1:]
		}
		desc := l.Description
		fmt.Fprintf(&b, "  label%d: createLabel(input: {repositoryId: %q, name: %q, color: %q, description: %q}) {\n    label { id name }\n  }\n",
			i, repoID, l.Name, color, desc)
	}

	b.WriteString("}")

	var rawResult map[string]json.RawMessage
	if err := c.execute(ctx, b.String(), nil, &rawResult); err != nil {
		return nil, err
	}

	result := make(map[string]string, len(labels))
	for i, l := range labels {
		alias := fmt.Sprintf("label%d", i)
		raw, ok := rawResult[alias]
		if !ok {
			continue
		}

		var labelResp struct {
			Label struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"label"`
		}
		if err := json.Unmarshal(raw, &labelResp); err != nil {
			slog.Warn("failed to parse label response", "alias", alias, "error", err)
			continue
		}
		result[l.Name] = labelResp.Label.ID
	}

	return result, nil
}

// fallbackCreateIssues creates issues one at a time when batch creation fails.
func (c *GitHubGraphQLClient) fallbackCreateIssues(ctx context.Context, repoID string, issues []IssueInput) ([]CreatedIssue, error) {
	created := make([]CreatedIssue, 0, len(issues))
	var lastErr error

	for i, issue := range issues {
		single := []IssueInput{issue}
		mutation, variables := buildBatchIssueMutation(repoID, single)

		var rawResult map[string]json.RawMessage
		if err := c.execute(ctx, mutation, variables, &rawResult); err != nil {
			slog.Warn("fallback issue creation failed",
				"index", i, "title", issue.Title, "error", err)
			lastErr = err
			continue
		}

		raw, ok := rawResult["issue0"]
		if !ok {
			continue
		}

		var issueResp struct {
			Issue struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				URL    string `json:"url"`
			} `json:"issue"`
		}
		if err := json.Unmarshal(raw, &issueResp); err != nil {
			lastErr = err
			continue
		}

		created = append(created, CreatedIssue{
			ID:     issueResp.Issue.ID,
			Number: issueResp.Issue.Number,
			URL:    issueResp.Issue.URL,
		})
	}

	if len(created) == 0 && lastErr != nil {
		return nil, fmt.Errorf("all individual issue creations failed, last error: %w", lastErr)
	}

	slog.Info("fallback issue creation completed",
		"requested", len(issues), "created", len(created))

	return created, nil
}
