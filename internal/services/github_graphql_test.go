package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/time/rate"
)

func TestBuildBatchIssueMutation(t *testing.T) {
	tests := []struct {
		name       string
		repoID     string
		issues     []IssueInput
		wantAliases int
		wantLabels  bool
	}{
		{
			name:   "single issue",
			repoID: "R_abc123",
			issues: []IssueInput{
				{Title: "Bug: login fails", Body: "Steps to reproduce..."},
			},
			wantAliases: 1,
		},
		{
			name:   "five issues with labels",
			repoID: "R_abc123",
			issues: []IssueInput{
				{Title: "Issue 1", Body: "Body 1", Labels: []string{"LA_bug"}},
				{Title: "Issue 2", Body: "Body 2"},
				{Title: "Issue 3", Body: "Body 3", Labels: []string{"LA_bug", "LA_urgent"}},
				{Title: "Issue 4", Body: "Body 4", Assignees: []string{"U_user1"}},
				{Title: "Issue 5", Body: ""},
			},
			wantAliases: 5,
			wantLabels:  true,
		},
		{
			name:   "max batch of 20 issues",
			repoID: "R_repo",
			issues: func() []IssueInput {
				issues := make([]IssueInput, 20)
				for i := range issues {
					issues[i] = IssueInput{Title: "Issue", Body: "Body"}
				}
				return issues
			}(),
			wantAliases: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutation, variables := buildBatchIssueMutation(tt.repoID, tt.issues)

			// Variables should be nil since we inline everything.
			if variables != nil {
				t.Errorf("expected nil variables, got %v", variables)
			}

			// Verify the mutation starts with "mutation {".
			if !strings.HasPrefix(mutation, "mutation {") {
				t.Errorf("mutation should start with 'mutation {', got: %s", mutation[:40])
			}

			// Verify correct number of aliases.
			for i := 0; i < tt.wantAliases; i++ {
				expected := fmt.Sprintf("issue%d: createIssue", i)
				if !strings.Contains(mutation, expected) {
					t.Errorf("mutation missing alias %q", expected)
				}
			}

			// Verify repo ID is embedded.
			if !strings.Contains(mutation, tt.repoID) {
				t.Errorf("mutation should contain repo ID %q", tt.repoID)
			}

			// Verify labels are included when present.
			if tt.wantLabels && !strings.Contains(mutation, "labelIds") {
				t.Errorf("mutation should contain labelIds for issues with labels")
			}

			// Verify the response fields are requested.
			if !strings.Contains(mutation, "issue { id number url }") {
				t.Errorf("mutation should request issue fields (id, number, url)")
			}
		})
	}
}

func TestGetRepositoryInfoQuery(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		repo     string
		response string
		want     *GraphQLRepoInfo
		wantErr  bool
	}{
		{
			name:  "standard repository",
			owner: "octocat",
			repo:  "hello-world",
			response: `{
				"data": {
					"repository": {
						"id": "R_kgDOAbcdef",
						"name": "hello-world",
						"defaultBranchRef": {"name": "main"},
						"isEmpty": false,
						"diskUsage": 1024,
						"isPrivate": false,
						"isArchived": false,
						"hasIssuesEnabled": true,
						"url": "https://github.com/octocat/hello-world"
					}
				}
			}`,
			want: &GraphQLRepoInfo{
				ID:               "R_kgDOAbcdef",
				Name:             "hello-world",
				DefaultBranch:    "main",
				IsEmpty:          false,
				DiskUsageKB:      1024,
				IsPrivate:        false,
				IsArchived:       false,
				HasIssuesEnabled: true,
				URL:              "https://github.com/octocat/hello-world",
			},
		},
		{
			name:  "empty repository with no default branch",
			owner: "octocat",
			repo:  "empty-repo",
			response: `{
				"data": {
					"repository": {
						"id": "R_kgDOXyz123",
						"name": "empty-repo",
						"defaultBranchRef": null,
						"isEmpty": true,
						"diskUsage": 0,
						"isPrivate": true,
						"isArchived": false,
						"hasIssuesEnabled": true,
						"url": "https://github.com/octocat/empty-repo"
					}
				}
			}`,
			want: &GraphQLRepoInfo{
				ID:               "R_kgDOXyz123",
				Name:             "empty-repo",
				DefaultBranch:    "",
				IsEmpty:          true,
				DiskUsageKB:      0,
				IsPrivate:        true,
				IsArchived:       false,
				HasIssuesEnabled: true,
				URL:              "https://github.com/octocat/empty-repo",
			},
		},
		{
			name:  "not found error",
			owner: "octocat",
			repo:  "nonexistent",
			response: `{
				"data": {"repository": null},
				"errors": [{"message": "Could not resolve to a Repository with the name 'nonexistent'.", "type": "NOT_FOUND"}]
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Remaining", "4999")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := &GitHubGraphQLClient{
				httpClient:  server.Client(),
				endpoint:    server.URL,
				rateLimiter: newTestRateLimiter(),
			}

			got, err := client.GetRepositoryInfo(context.Background(), tt.owner, tt.repo)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.ID != tt.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.want.ID)
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.DefaultBranch != tt.want.DefaultBranch {
				t.Errorf("DefaultBranch = %q, want %q", got.DefaultBranch, tt.want.DefaultBranch)
			}
			if got.IsEmpty != tt.want.IsEmpty {
				t.Errorf("IsEmpty = %v, want %v", got.IsEmpty, tt.want.IsEmpty)
			}
			if got.DiskUsageKB != tt.want.DiskUsageKB {
				t.Errorf("DiskUsageKB = %d, want %d", got.DiskUsageKB, tt.want.DiskUsageKB)
			}
			if got.IsPrivate != tt.want.IsPrivate {
				t.Errorf("IsPrivate = %v, want %v", got.IsPrivate, tt.want.IsPrivate)
			}
			if got.IsArchived != tt.want.IsArchived {
				t.Errorf("IsArchived = %v, want %v", got.IsArchived, tt.want.IsArchived)
			}
			if got.HasIssuesEnabled != tt.want.HasIssuesEnabled {
				t.Errorf("HasIssuesEnabled = %v, want %v", got.HasIssuesEnabled, tt.want.HasIssuesEnabled)
			}
			if got.URL != tt.want.URL {
				t.Errorf("URL = %q, want %q", got.URL, tt.want.URL)
			}
		})
	}
}

func TestParseGraphQLErrors(t *testing.T) {
	tests := []struct {
		name      string
		errors    []graphQLError
		wantMsg   string
		isPartial bool
	}{
		{
			name: "single error",
			errors: []graphQLError{
				{Message: "Resource not found"},
			},
			wantMsg:   "graphql: Resource not found",
			isPartial: false,
		},
		{
			name: "multiple errors",
			errors: []graphQLError{
				{Message: "Field 'foo' not found"},
				{Message: "Argument 'bar' is invalid"},
			},
			wantMsg:   "graphql: 2 errors: Field 'foo' not found; Argument 'bar' is invalid",
			isPartial: false,
		},
		{
			name: "partial failure with path",
			errors: []graphQLError{
				{
					Message: "Could not create issue",
					Path:    []interface{}{"issue2", "createIssue"},
				},
			},
			wantMsg:   "graphql: Could not create issue",
			isPartial: true,
		},
		{
			name: "mixed partial and global errors",
			errors: []graphQLError{
				{Message: "issue0 failed", Path: []interface{}{"issue0"}},
				{Message: "rate limit exceeded"},
			},
			wantMsg:   "graphql: 2 errors: issue0 failed; rate limit exceeded",
			isPartial: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiErr := &GraphQLAPIError{Errors: tt.errors}

			if got := apiErr.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}

			if got := apiErr.IsPartialError(); got != tt.isPartial {
				t.Errorf("IsPartialError() = %v, want %v", got, tt.isPartial)
			}
		})
	}
}

func TestBatchCloseIssues(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var reqBody graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		// Verify it's a mutation with close aliases.
		if !strings.Contains(reqBody.Query, "closeIssue") {
			t.Error("expected closeIssue in mutation")
		}
		if !strings.Contains(reqBody.Query, "close0:") {
			t.Error("expected close0 alias")
		}
		if !strings.Contains(reqBody.Query, "close1:") {
			t.Error("expected close1 alias")
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "4990")
		resp := `{"data": {"close0": {"issue": {"id": "I_1", "state": "CLOSED"}}, "close1": {"issue": {"id": "I_2", "state": "CLOSED"}}}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := &GitHubGraphQLClient{
		httpClient:  server.Client(),
		endpoint:    server.URL,
		rateLimiter: newTestRateLimiter(),
	}

	err := client.BatchCloseIssues(context.Background(), []string{"I_1", "I_2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

func TestBatchCreateIssuesExceedsMax(t *testing.T) {
	client := &GitHubGraphQLClient{
		httpClient:  http.DefaultClient,
		endpoint:    "http://unused",
		rateLimiter: newTestRateLimiter(),
	}

	issues := make([]IssueInput, 21)
	for i := range issues {
		issues[i] = IssueInput{Title: "Issue"}
	}

	_, err := client.BatchCreateIssues(context.Background(), "R_123", issues)
	if err == nil {
		t.Fatal("expected error for batch exceeding max size")
	}
	if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Errorf("error should mention exceeds maximum, got: %v", err)
	}
}

func TestBatchCreateIssuesEmpty(t *testing.T) {
	client := &GitHubGraphQLClient{
		httpClient:  http.DefaultClient,
		endpoint:    "http://unused",
		rateLimiter: newTestRateLimiter(),
	}

	result, err := client.BatchCreateIssues(context.Background(), "R_123", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for empty input, got %v", result)
	}
}

func TestGetRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "4500")
		resp := `{"data": {"rateLimit": {"limit": 5000, "remaining": 4500, "cost": 1, "resetAt": "2026-01-01T00:00:00Z"}}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	client := &GitHubGraphQLClient{
		httpClient:  server.Client(),
		endpoint:    server.URL,
		rateLimiter: newTestRateLimiter(),
	}

	rl, err := client.GetRateLimit(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rl.Limit != 5000 {
		t.Errorf("Limit = %d, want 5000", rl.Limit)
	}
	if rl.Remaining != 4500 {
		t.Errorf("Remaining = %d, want 4500", rl.Remaining)
	}
	if rl.Cost != 1 {
		t.Errorf("Cost = %d, want 1", rl.Cost)
	}
}

// newTestRateLimiter creates a permissive rate limiter for tests.
func newTestRateLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Inf, 1)
}

