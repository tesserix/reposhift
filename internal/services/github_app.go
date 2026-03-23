package services

import (
	"context"
	"crypto/rsa"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-github/v84/github"
	"golang.org/x/oauth2"
)

// GitHubAppClient represents a GitHub App authenticated client
type GitHubAppClient struct {
	client         *github.Client
	installationID int64
	appID          int64
	privateKey     *rsa.PrivateKey
	token          string
	tokenExpiry    time.Time
	tokenMutex     sync.RWMutex
}

// NewGitHubAppClient creates a new GitHub App authenticated client
func NewGitHubAppClient(ctx context.Context, appID int64, installationID int64, privateKeyPEM []byte) (*GitHubAppClient, error) {
	// Parse private key
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	appClient := &GitHubAppClient{
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
	}

	// Generate initial installation token
	if err := appClient.refreshToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to get initial installation token: %w", err)
	}

	return appClient, nil
}

// generateJWT generates a JWT token for GitHub App authentication
// JWT is used to authenticate as the GitHub App itself (not as an installation)
func (c *GitHubAppClient) generateJWT() (string, error) {
	now := time.Now()

	// GitHub requires:
	// - iat (issued at) - current time
	// - exp (expiration) - max 10 minutes in the future
	// - iss (issuer) - the GitHub App ID
	claims := jwt.MapClaims{
		"iat": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
		"iss": c.appID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedToken, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return signedToken, nil
}

// refreshToken exchanges JWT for installation access token
// Installation tokens are valid for 1 hour and have higher rate limits
func (c *GitHubAppClient) refreshToken(ctx context.Context) error {
	// Generate JWT token for authentication as the App
	jwtToken, err := c.generateJWT()
	if err != nil {
		return fmt.Errorf("failed to generate JWT: %w", err)
	}

	// Create temporary client authenticated with JWT
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: jwtToken})
	tc := oauth2.NewClient(ctx, ts)
	tempClient := github.NewClient(tc)

	// Exchange JWT for installation access token
	installationToken, resp, err := tempClient.Apps.CreateInstallationToken(
		ctx,
		c.installationID,
		&github.InstallationTokenOptions{},
	)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return fmt.Errorf("installation ID %d not found - verify the GitHub App is installed on the target organization", c.installationID)
		}
		if resp != nil && resp.StatusCode == 401 {
			return fmt.Errorf("authentication failed - verify App ID and private key are correct")
		}
		return fmt.Errorf("failed to create installation token: %w", err)
	}

	// Update client with new installation token
	c.tokenMutex.Lock()
	defer c.tokenMutex.Unlock()

	c.token = installationToken.GetToken()
	c.tokenExpiry = installationToken.GetExpiresAt().Time

	// Create authenticated client with installation token
	ts = oauth2.StaticTokenSource(&oauth2.Token{AccessToken: c.token})
	tc = oauth2.NewClient(ctx, ts)
	c.client = github.NewClient(tc)

	return nil
}

// GetClient returns a GitHub client, refreshing the token if needed
// Tokens are automatically refreshed 5 minutes before expiry
func (c *GitHubAppClient) GetClient(ctx context.Context) (*github.Client, error) {
	c.tokenMutex.RLock()

	// Check if token needs refresh (5 minute buffer before expiry)
	needsRefresh := time.Until(c.tokenExpiry) < 5*time.Minute

	if !needsRefresh {
		client := c.client
		c.tokenMutex.RUnlock()
		return client, nil
	}

	c.tokenMutex.RUnlock()

	// Token expired or near expiry - refresh it
	if err := c.refreshToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh installation token: %w", err)
	}

	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()
	return c.client, nil
}

// GetToken returns the current installation token
func (c *GitHubAppClient) GetToken(ctx context.Context) (string, error) {
	c.tokenMutex.RLock()

	// Check if token needs refresh
	needsRefresh := time.Until(c.tokenExpiry) < 5*time.Minute

	if !needsRefresh {
		token := c.token
		c.tokenMutex.RUnlock()
		return token, nil
	}

	c.tokenMutex.RUnlock()

	// Refresh token
	if err := c.refreshToken(ctx); err != nil {
		return "", err
	}

	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()
	return c.token, nil
}

// GetTokenExpiry returns when the current token expires
func (c *GitHubAppClient) GetTokenExpiry() time.Time {
	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()
	return c.tokenExpiry
}

// ForceRefresh forces an immediate token refresh
func (c *GitHubAppClient) ForceRefresh(ctx context.Context) error {
	return c.refreshToken(ctx)
}
