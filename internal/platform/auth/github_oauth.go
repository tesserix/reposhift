package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/tesserix/reposhift/internal/platform/config"
	"golang.org/x/oauth2"
	oauth2github "golang.org/x/oauth2/github"
)

// GitHubUser represents the authenticated GitHub user profile.
type GitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubOAuth wraps an oauth2.Config for GitHub authentication.
type GitHubOAuth struct {
	config oauth2.Config
}

// NewGitHubOAuth creates a GitHubOAuth from the application's GitHub OAuth configuration.
func NewGitHubOAuth(cfg config.GitHubOAuthConfig) *GitHubOAuth {
	return &GitHubOAuth{
		config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
			Endpoint:     oauth2github.Endpoint,
		},
	}
}

// GetAuthURL returns the GitHub OAuth authorization URL for the given state parameter.
func (g *GitHubOAuth) GetAuthURL(state string) string {
	return g.config.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

// Exchange trades an authorization code for an OAuth2 token.
func (g *GitHubOAuth) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	token, err := g.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for token: %w", err)
	}
	return token, nil
}

// GetUser fetches the authenticated user's profile from the GitHub API.
func (g *GitHubOAuth) GetUser(ctx context.Context, token *oauth2.Token) (*GitHubUser, error) {
	client := g.config.Client(ctx, token)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching github user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github user API returned status %d", resp.StatusCode)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding github user: %w", err)
	}
	return &user, nil
}
