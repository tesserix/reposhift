package platform

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/reposhift/internal/platform/auth"
	"github.com/tesserix/reposhift/internal/platform/config"
	"github.com/tesserix/reposhift/internal/platform/migration"
	"github.com/tesserix/reposhift/internal/platform/secrets"
)

// validSecretTypes defines the allowed secret type values.
var validSecretTypes = map[string]struct{}{
	"ado_pat":      {},
	"github_token": {},
	"github_app":   {},
	"azure_sp":     {},
}

// PlatformServer wraps all platform dependencies and exposes the HTTP API.
type PlatformServer struct {
	cfg             *config.PlatformConfig
	migrationStore  *migration.MigrationStore
	secretsProvider secrets.SecretsProvider
	orchestrator    *migration.Orchestrator
}

// NewPlatformServer creates a PlatformServer with the given dependencies.
func NewPlatformServer(
	cfg *config.PlatformConfig,
	migrationStore *migration.MigrationStore,
	secretsProvider secrets.SecretsProvider,
	orchestrator *migration.Orchestrator,
) *PlatformServer {
	return &PlatformServer{
		cfg:             cfg,
		migrationStore:  migrationStore,
		secretsProvider: secretsProvider,
		orchestrator:    orchestrator,
	}
}

// SetupRouter configures and returns the Gin router with all platform routes.
func (s *PlatformServer) SetupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(s.corsMiddleware())

	// Health endpoints.
	r.GET("/health", s.handleHealth)
	r.GET("/ready", s.handleReady)

	v1 := r.Group("/platform/v1")

	// Public config endpoint (no auth required).
	v1.GET("/config/mode", s.handleConfigMode)

	// Protected routes: admin token auth.
	protected := v1.Group("")
	protected.Use(auth.AdminTokenMiddleware(s.cfg.AdminToken))
	{
		// Secret routes.
		protected.GET("/secrets", s.listSecrets)
		protected.POST("/secrets", s.createSecret)
		protected.GET("/secrets/:name", s.getSecret)
		protected.PUT("/secrets/:name", s.updateSecret)
		protected.DELETE("/secrets/:name", s.deleteSecret)
		protected.POST("/secrets/:name/validate", s.validateSecret)

		// Migration routes.
		protected.GET("/migrations", s.listMigrations)
		protected.POST("/migrations", s.createMigration)
		protected.GET("/migrations/:id", s.getMigration)
		protected.DELETE("/migrations/:id", s.deleteMigration)
		protected.POST("/migrations/:id/pause", s.pauseMigration)
		protected.POST("/migrations/:id/resume", s.resumeMigration)
		protected.POST("/migrations/:id/cancel", s.cancelMigration)
		protected.POST("/migrations/:id/retry", s.retryMigration)

		// Dashboard routes.
		protected.GET("/dashboard/stats", s.getDashboardStats)
	}

	return r
}

// corsMiddleware returns a Gin middleware that handles CORS headers.
func (s *PlatformServer) corsMiddleware() gin.HandlerFunc {
	allowedOrigins := make(map[string]struct{}, len(s.cfg.CORS.AllowedOrigins))
	for _, o := range s.cfg.CORS.AllowedOrigins {
		allowedOrigins[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if _, ok := allowedOrigins[origin]; ok {
			c.Header("Access-Control-Allow-Origin", origin)
		} else if _, ok := allowedOrigins["*"]; ok {
			c.Header("Access-Control-Allow-Origin", "*")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// ---------- Health ----------

func (s *PlatformServer) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *PlatformServer) handleReady(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// handleConfigMode returns basic deployment information.
func (s *PlatformServer) handleConfigMode(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": "0.1.0",
	})
}

// ---------- Secrets ----------

func (s *PlatformServer) listSecrets(c *gin.Context) {
	items, err := s.secretsProvider.List(c.Request.Context())
	if err != nil {
		slog.Error("failed to list secrets", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list secrets"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"secrets": items})
}

type createSecretRequest struct {
	Name       string            `json:"name" binding:"required"`
	SecretType string            `json:"secretType" binding:"required"`
	Data       map[string]string `json:"data" binding:"required"`
}

func (s *PlatformServer) createSecret(c *gin.Context) {
	var req createSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	// Validate secret type.
	if _, ok := validSecretTypes[req.SecretType]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid secretType %q: must be one of ado_pat, github_token, github_app, azure_sp", req.SecretType)})
		return
	}

	if err := s.secretsProvider.Store(c.Request.Context(), req.Name, req.SecretType, req.Data); err != nil {
		slog.Error("failed to create secret", "name", req.Name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create secret"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "secret created", "name": req.Name})
}

func (s *PlatformServer) getSecret(c *gin.Context) {
	secretName := c.Param("name")

	// List all secrets and find the one matching the name.
	// We don't return the decrypted value — only metadata.
	items, err := s.secretsProvider.List(c.Request.Context())
	if err != nil {
		slog.Error("failed to list secrets", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve secret"})
		return
	}

	for _, item := range items {
		if item.Name == secretName {
			c.JSON(http.StatusOK, gin.H{"secret": item})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
}

func (s *PlatformServer) updateSecret(c *gin.Context) {
	secretName := c.Param("name")

	var req createSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	if _, ok := validSecretTypes[req.SecretType]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid secretType %q: must be one of ado_pat, github_token, github_app, azure_sp", req.SecretType)})
		return
	}

	// Store overwrites the existing secret (upsert).
	if err := s.secretsProvider.Store(c.Request.Context(), secretName, req.SecretType, req.Data); err != nil {
		slog.Error("failed to update secret", "name", secretName, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "secret updated", "name": secretName})
}

func (s *PlatformServer) deleteSecret(c *gin.Context) {
	secretName := c.Param("name")

	if err := s.secretsProvider.Delete(c.Request.Context(), secretName); err != nil {
		slog.Error("failed to delete secret", "name", secretName, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "secret deleted"})
}

// validateSecret decrypts the stored secret and tests connectivity against
// the upstream provider (Azure DevOps or GitHub). This verifies the PAT/token
// is valid, has required permissions, and can reach the target service.
func (s *PlatformServer) validateSecret(c *gin.Context) {
	secretName := c.Param("name")
	ctx := c.Request.Context()

	// Retrieve the decrypted secret data.
	data, err := s.secretsProvider.Get(ctx, secretName)
	if err != nil {
		slog.Error("failed to retrieve secret for validation", "name", secretName, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found or could not be decrypted"})
		return
	}

	// Determine secret type from metadata.
	items, err := s.secretsProvider.List(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to look up secret metadata"})
		return
	}
	var secretType string
	for _, item := range items {
		if item.Name == secretName {
			secretType = item.SecretType
			break
		}
	}
	if secretType == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "secret not found"})
		return
	}

	result := gin.H{
		"name":       secretName,
		"secretType": secretType,
		"valid":      false,
		"checks":     []gin.H{},
	}

	switch secretType {
	case "ado_pat":
		s.validateADOPAT(ctx, data, result)
	case "github_token":
		s.validateGitHubToken(ctx, data, result)
	case "github_app":
		s.validateGitHubApp(ctx, data, result)
	case "azure_sp":
		s.validateAzureSP(ctx, data, result)
	default:
		result["valid"] = true
		result["checks"] = []gin.H{{
			"check":   "type",
			"status":  "skipped",
			"message": fmt.Sprintf("no validation available for type %q", secretType),
		}}
	}

	c.JSON(http.StatusOK, gin.H{"validation": result})
}

// validateADOPAT tests an Azure DevOps Personal Access Token.
// Expected data keys: "token" (the PAT), optionally "organization".
func (s *PlatformServer) validateADOPAT(ctx context.Context, data map[string]string, result gin.H) {
	token := data["token"]
	org := data["organization"]

	checks := []gin.H{}

	if token == "" {
		checks = append(checks, gin.H{"check": "token_present", "status": "failed", "message": "PAT token is missing from secret data (expected key: 'token')"})
		result["checks"] = checks
		return
	}
	checks = append(checks, gin.H{"check": "token_present", "status": "passed", "message": "PAT token is present"})

	// Test ADO API connectivity with the PAT.
	adoURL := "https://dev.azure.com"
	if org != "" {
		adoURL = fmt.Sprintf("https://dev.azure.com/%s/_apis/connectionData", org)
	} else {
		adoURL = "https://app.vssps.visualstudio.com/_apis/profile/profiles/me?api-version=7.0"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, adoURL, nil)
	if err != nil {
		checks = append(checks, gin.H{"check": "connectivity", "status": "failed", "message": "failed to build request: " + err.Error()})
		result["checks"] = checks
		return
	}
	req.SetBasicAuth("", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		checks = append(checks, gin.H{"check": "connectivity", "status": "failed", "message": "failed to connect to Azure DevOps: " + err.Error()})
		result["checks"] = checks
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		checks = append(checks, gin.H{"check": "connectivity", "status": "passed", "message": "successfully authenticated with Azure DevOps"})
		result["valid"] = true
	} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		checks = append(checks, gin.H{"check": "connectivity", "status": "failed", "message": fmt.Sprintf("authentication failed (HTTP %d): token may be expired or revoked", resp.StatusCode)})
	} else {
		checks = append(checks, gin.H{"check": "connectivity", "status": "warning", "message": fmt.Sprintf("unexpected response (HTTP %d): token may still be valid", resp.StatusCode)})
		result["valid"] = true // Non-auth errors don't necessarily mean invalid
	}

	if org != "" {
		checks = append(checks, gin.H{"check": "organization", "status": "passed", "message": fmt.Sprintf("organization '%s' is configured", org)})
	} else {
		checks = append(checks, gin.H{"check": "organization", "status": "warning", "message": "no organization configured — set 'organization' key in secret data for org-scoped operations"})
	}

	result["checks"] = checks
}

// validateGitHubToken tests a GitHub Personal Access Token.
// Expected data keys: "token" (the PAT), optionally "owner" (org or user).
func (s *PlatformServer) validateGitHubToken(ctx context.Context, data map[string]string, result gin.H) {
	token := data["token"]
	owner := data["owner"]

	checks := []gin.H{}

	if token == "" {
		checks = append(checks, gin.H{"check": "token_present", "status": "failed", "message": "GitHub token is missing from secret data (expected key: 'token')"})
		result["checks"] = checks
		return
	}
	checks = append(checks, gin.H{"check": "token_present", "status": "passed", "message": "GitHub token is present"})

	// Test GitHub API with the token.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		checks = append(checks, gin.H{"check": "connectivity", "status": "failed", "message": "failed to build request: " + err.Error()})
		result["checks"] = checks
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		checks = append(checks, gin.H{"check": "connectivity", "status": "failed", "message": "failed to connect to GitHub API: " + err.Error()})
		result["checks"] = checks
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		checks = append(checks, gin.H{"check": "connectivity", "status": "passed", "message": "successfully authenticated with GitHub"})
		result["valid"] = true

		// Check scopes from response header.
		scopes := resp.Header.Get("X-OAuth-Scopes")
		if scopes != "" {
			checks = append(checks, gin.H{"check": "scopes", "status": "passed", "message": "token scopes: " + scopes})

			// Verify required scopes for migration.
			scopeList := strings.Split(scopes, ", ")
			hasRepo := false
			for _, scope := range scopeList {
				if strings.TrimSpace(scope) == "repo" {
					hasRepo = true
				}
			}
			if !hasRepo {
				checks = append(checks, gin.H{"check": "repo_scope", "status": "warning", "message": "token is missing 'repo' scope — required for repository migrations"})
			} else {
				checks = append(checks, gin.H{"check": "repo_scope", "status": "passed", "message": "'repo' scope is present"})
			}
		}

		// Check rate limit.
		remaining := resp.Header.Get("X-RateLimit-Remaining")
		limit := resp.Header.Get("X-RateLimit-Limit")
		if remaining != "" && limit != "" {
			checks = append(checks, gin.H{"check": "rate_limit", "status": "passed", "message": fmt.Sprintf("rate limit: %s/%s remaining", remaining, limit)})
		}
	} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		checks = append(checks, gin.H{"check": "connectivity", "status": "failed", "message": fmt.Sprintf("authentication failed (HTTP %d): token may be expired or revoked", resp.StatusCode)})
	} else {
		checks = append(checks, gin.H{"check": "connectivity", "status": "warning", "message": fmt.Sprintf("unexpected response (HTTP %d)", resp.StatusCode)})
	}

	// Check owner access if specified.
	if owner != "" && result["valid"] == true {
		ownerReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.github.com/orgs/%s", owner), nil)
		ownerReq.Header.Set("Authorization", "Bearer "+token)
		ownerReq.Header.Set("Accept", "application/vnd.github+json")
		ownerResp, err := http.DefaultClient.Do(ownerReq)
		if err == nil {
			defer ownerResp.Body.Close()
			if ownerResp.StatusCode == http.StatusOK {
				checks = append(checks, gin.H{"check": "owner_access", "status": "passed", "message": fmt.Sprintf("can access organization '%s'", owner)})
			} else {
				checks = append(checks, gin.H{"check": "owner_access", "status": "warning", "message": fmt.Sprintf("cannot access organization '%s' (HTTP %d) — may be a user account", owner, ownerResp.StatusCode)})
			}
		}
	} else if owner == "" {
		checks = append(checks, gin.H{"check": "owner_access", "status": "warning", "message": "no owner configured — set 'owner' key in secret data to validate org/user access"})
	}

	result["checks"] = checks
}

// validateGitHubApp checks that a GitHub App installation has valid credentials.
// Expected data keys: "app_id", "installation_id", "private_key".
func (s *PlatformServer) validateGitHubApp(_ context.Context, data map[string]string, result gin.H) {
	checks := []gin.H{}
	allPresent := true

	for _, key := range []string{"app_id", "installation_id", "private_key"} {
		if data[key] == "" {
			checks = append(checks, gin.H{"check": key + "_present", "status": "failed", "message": fmt.Sprintf("required key '%s' is missing from secret data", key)})
			allPresent = false
		} else {
			checks = append(checks, gin.H{"check": key + "_present", "status": "passed", "message": fmt.Sprintf("'%s' is present", key)})
		}
	}

	if allPresent {
		result["valid"] = true
		checks = append(checks, gin.H{"check": "credentials_complete", "status": "passed", "message": "all GitHub App credentials are present"})
	}

	result["checks"] = checks
}

// validateAzureSP checks an Azure Service Principal's credentials.
// Expected data keys: "client_id", "client_secret", "tenant_id", optionally "organization".
func (s *PlatformServer) validateAzureSP(ctx context.Context, data map[string]string, result gin.H) {
	checks := []gin.H{}
	allPresent := true

	for _, key := range []string{"client_id", "client_secret", "tenant_id"} {
		if data[key] == "" {
			checks = append(checks, gin.H{"check": key + "_present", "status": "failed", "message": fmt.Sprintf("required key '%s' is missing from secret data", key)})
			allPresent = false
		} else {
			checks = append(checks, gin.H{"check": key + "_present", "status": "passed", "message": fmt.Sprintf("'%s' is present", key)})
		}
	}

	if !allPresent {
		result["checks"] = checks
		return
	}

	// Test Azure AD token acquisition.
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", data["tenant_id"])
	body := fmt.Sprintf("grant_type=client_credentials&client_id=%s&client_secret=%s&scope=499b84ac-1321-427f-aa17-267ca6975798/.default",
		data["client_id"], data["client_secret"])

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body))
	if err != nil {
		checks = append(checks, gin.H{"check": "token_acquisition", "status": "failed", "message": "failed to build request: " + err.Error()})
		result["checks"] = checks
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		checks = append(checks, gin.H{"check": "token_acquisition", "status": "failed", "message": "failed to contact Azure AD: " + err.Error()})
		result["checks"] = checks
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		checks = append(checks, gin.H{"check": "token_acquisition", "status": "passed", "message": "successfully acquired Azure AD token"})
		result["valid"] = true
	} else {
		checks = append(checks, gin.H{"check": "token_acquisition", "status": "failed", "message": fmt.Sprintf("Azure AD token acquisition failed (HTTP %d): check client_id, client_secret, and tenant_id", resp.StatusCode)})
	}

	result["checks"] = checks
}

// ---------- Migrations ----------

func (s *PlatformServer) listMigrations(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	items, total, err := s.orchestrator.ListMigrations(c.Request.Context(), limit, offset)
	if err != nil {
		slog.Error("failed to list migrations", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list migrations"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"migrations": items,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

func (s *PlatformServer) createMigration(c *gin.Context) {
	var req migration.CreateMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	m, err := s.orchestrator.CreateMigration(c.Request.Context(), req)
	if err != nil {
		slog.Error("failed to create migration", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create migration"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"migration": m})
}

func (s *PlatformServer) getMigration(c *gin.Context) {
	migrationID := c.Param("id")

	resp, err := s.orchestrator.GetMigrationStatus(c.Request.Context(), migrationID)
	if err != nil {
		slog.Error("failed to get migration", "migrationId", migrationID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "migration not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"migration": resp})
}

func (s *PlatformServer) deleteMigration(c *gin.Context) {
	migrationID := c.Param("id")

	if err := s.orchestrator.DeleteMigration(c.Request.Context(), migrationID); err != nil {
		slog.Error("failed to delete migration", "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration deleted"})
}

func (s *PlatformServer) pauseMigration(c *gin.Context) {
	migrationID := c.Param("id")

	if err := s.orchestrator.PauseMigration(c.Request.Context(), migrationID); err != nil {
		slog.Error("failed to pause migration", "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to pause migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration paused"})
}

func (s *PlatformServer) resumeMigration(c *gin.Context) {
	migrationID := c.Param("id")

	if err := s.orchestrator.ResumeMigration(c.Request.Context(), migrationID); err != nil {
		slog.Error("failed to resume migration", "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resume migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration resumed"})
}

func (s *PlatformServer) cancelMigration(c *gin.Context) {
	migrationID := c.Param("id")

	if err := s.orchestrator.CancelMigration(c.Request.Context(), migrationID); err != nil {
		slog.Error("failed to cancel migration", "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration cancelled"})
}

func (s *PlatformServer) retryMigration(c *gin.Context) {
	migrationID := c.Param("id")

	if err := s.orchestrator.RetryMigration(c.Request.Context(), migrationID); err != nil {
		slog.Error("failed to retry migration", "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retry migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration retry initiated"})
}

// ---------- Dashboard ----------

func (s *PlatformServer) getDashboardStats(c *gin.Context) {
	ctx := c.Request.Context()

	// TODO: Replace this with a dedicated CountByStatus method that uses a
	// GROUP BY query to avoid loading all migration rows into memory.
	migrations, total, err := s.migrationStore.List(ctx, 10000, 0)
	if err != nil {
		slog.Error("failed to get dashboard stats", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve dashboard stats"})
		return
	}

	statusCounts := make(map[string]int)
	for _, m := range migrations {
		statusCounts[m.Status]++
	}

	c.JSON(http.StatusOK, gin.H{
		"totalMigrations": total,
		"byStatus":        statusCounts,
	})
}
