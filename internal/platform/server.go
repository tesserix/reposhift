package platform

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/reposhift/internal/platform/auth"
	"github.com/tesserix/reposhift/internal/platform/config"
	"github.com/tesserix/reposhift/internal/platform/migration"
	"github.com/tesserix/reposhift/internal/platform/secrets"
	"github.com/tesserix/reposhift/internal/platform/tenant"
)

// PlatformServer wraps all platform dependencies and exposes the HTTP API.
type PlatformServer struct {
	cfg             *config.PlatformConfig
	tenantStore     *tenant.TenantStore
	migrationStore  *migration.MigrationStore
	secretsProvider secrets.SecretsProvider
	orchestrator    *migration.Orchestrator
	githubOAuth     *auth.GitHubOAuth
}

// NewPlatformServer creates a PlatformServer with the given dependencies.
func NewPlatformServer(
	cfg *config.PlatformConfig,
	tenantStore *tenant.TenantStore,
	migrationStore *migration.MigrationStore,
	secretsProvider secrets.SecretsProvider,
	orchestrator *migration.Orchestrator,
	githubOAuth *auth.GitHubOAuth,
) *PlatformServer {
	return &PlatformServer{
		cfg:             cfg,
		tenantStore:     tenantStore,
		migrationStore:  migrationStore,
		secretsProvider: secretsProvider,
		orchestrator:    orchestrator,
		githubOAuth:     githubOAuth,
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

	// Auth routes (no auth required).
	authGroup := v1.Group("/auth")
	{
		authGroup.POST("/github", s.handleGitHubAuth)
		authGroup.GET("/github/callback", s.handleGitHubCallback)
		authGroup.POST("/refresh", s.handleRefreshToken)
	}

	// Protected routes (JWT middleware).
	protected := v1.Group("")
	protected.Use(auth.JWTAuthMiddleware(s.cfg.JWTSecret))
	{
		// Tenant routes.
		protected.GET("/tenant", s.getCurrentTenant)
		protected.PUT("/tenant", s.updateTenant)
		protected.GET("/tenant/members", s.listMembers)

		// Secret routes.
		protected.GET("/secrets", s.listSecrets)
		protected.POST("/secrets", s.createSecret)
		protected.DELETE("/secrets/:id", s.deleteSecret)

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

// ---------- Auth ----------

type githubAuthRequest struct {
	RedirectURL string `json:"redirectUrl"`
}

func (s *PlatformServer) handleGitHubAuth(c *gin.Context) {
	if s.githubOAuth == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub OAuth is not configured"})
		return
	}

	var req githubAuthRequest
	_ = c.ShouldBindJSON(&req) // optional body

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		slog.Error("failed to generate OAuth state", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state"})
		return
	}
	state := hex.EncodeToString(stateBytes)

	authURL := s.githubOAuth.GetAuthURL(state)
	c.JSON(http.StatusOK, gin.H{
		"authUrl": authURL,
		"state":   state,
	})
}

type githubCallbackRequest struct {
	Code  string `form:"code" binding:"required"`
	State string `form:"state" binding:"required"`
}

func (s *PlatformServer) handleGitHubCallback(c *gin.Context) {
	if s.githubOAuth == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "GitHub OAuth is not configured"})
		return
	}

	var req githubCallbackRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing code or state parameter"})
		return
	}

	// Exchange authorization code for an OAuth token.
	oauthToken, err := s.githubOAuth.Exchange(c.Request.Context(), req.Code)
	if err != nil {
		slog.Error("GitHub OAuth exchange failed", "error", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "failed to exchange authorization code"})
		return
	}

	// Fetch the GitHub user profile.
	ghUser, err := s.githubOAuth.GetUser(c.Request.Context(), oauthToken)
	if err != nil {
		slog.Error("failed to fetch GitHub user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch GitHub user profile"})
		return
	}

	// Upsert the user in the database.
	userID := uuid.New().String()
	user, err := s.tenantStore.UpsertUser(c.Request.Context(), &tenant.User{
		ID:          userID,
		GitHubID:    ghUser.ID,
		GitHubLogin: ghUser.Login,
		GitHubEmail: ghUser.Email,
		Name:        ghUser.Name,
		AvatarURL:   ghUser.AvatarURL,
	})
	if err != nil {
		slog.Error("failed to upsert user", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create or update user"})
		return
	}

	// Check if the user has an existing tenant membership.
	memberships, err := s.tenantStore.GetMembership(c.Request.Context(), user.ID)
	if err != nil {
		slog.Error("failed to get membership", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check tenant membership"})
		return
	}

	var tenantID, tenantSlug, role string

	if len(memberships) > 0 {
		// Use the first tenant membership.
		tenantID = memberships[0].TenantID
		tenantSlug = memberships[0].Tenant.Slug
		role = memberships[0].Role
	} else {
		// First login: create a new tenant for this user.
		tenantID = uuid.New().String()
		slug := strings.ToLower(ghUser.Login)
		newTenant := &tenant.Tenant{
			ID:           tenantID,
			Name:         fmt.Sprintf("%s's workspace", ghUser.Login),
			Slug:         slug,
			Plan:         "free",
			Mode:         s.cfg.Mode,
			K8sNamespace: s.cfg.K8sNamespace,
			Settings:     map[string]any{},
		}
		if err := s.tenantStore.CreateTenant(c.Request.Context(), newTenant); err != nil {
			slog.Error("failed to create tenant", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create workspace"})
			return
		}

		role = tenant.RoleOwner
		tenantSlug = slug
		if err := s.tenantStore.AddMember(c.Request.Context(), tenantID, user.ID, role); err != nil {
			slog.Error("failed to add tenant member", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to assign workspace membership"})
			return
		}
	}

	// Issue a JWT for the authenticated user.
	token, err := auth.IssueToken(user.ID, tenantID, tenantSlug, role, s.cfg.JWTSecret)
	if err != nil {
		slog.Error("failed to issue JWT", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue access token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user": gin.H{
			"id":          user.ID,
			"githubLogin": user.GitHubLogin,
			"email":       user.GitHubEmail,
			"name":        user.Name,
			"avatarUrl":   user.AvatarURL,
		},
		"tenant": gin.H{
			"id":   tenantID,
			"slug": tenantSlug,
			"role": role,
		},
	})
}

type refreshTokenRequest struct {
	Token string `json:"token" binding:"required"`
}

func (s *PlatformServer) handleRefreshToken(c *gin.Context) {
	var req refreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
		return
	}

	claims, err := auth.ValidateToken(req.Token, s.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
		return
	}

	// Issue a fresh token with the same claims.
	newToken, err := auth.IssueToken(claims.UserID, claims.TenantID, claims.TenantSlug, claims.Role, s.cfg.JWTSecret)
	if err != nil {
		slog.Error("failed to issue refreshed JWT", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to issue refreshed token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": newToken})
}

// ---------- Tenant ----------

func (s *PlatformServer) getCurrentTenant(c *gin.Context) {
	tenantID := auth.GetTenantID(c)

	t, err := s.tenantStore.GetTenantByID(c.Request.Context(), tenantID)
	if err != nil {
		slog.Error("failed to get tenant", "tenantId", tenantID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tenant": t})
}

type updateTenantRequest struct {
	Name     string         `json:"name"`
	Slug     string         `json:"slug"`
	Settings map[string]any `json:"settings"`
}

func (s *PlatformServer) updateTenant(c *gin.Context) {
	tenantID := auth.GetTenantID(c)

	var req updateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	t, err := s.tenantStore.GetTenantByID(c.Request.Context(), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "tenant not found"})
		return
	}

	if req.Name != "" {
		t.Name = req.Name
	}
	if req.Slug != "" {
		t.Slug = req.Slug
	}
	if req.Settings != nil {
		t.Settings = req.Settings
	}

	if err := s.tenantStore.UpdateTenant(c.Request.Context(), t); err != nil {
		slog.Error("failed to update tenant", "tenantId", tenantID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update tenant"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"tenant": t})
}

func (s *PlatformServer) listMembers(c *gin.Context) {
	tenantID := auth.GetTenantID(c)

	members, err := s.tenantStore.GetTenantMembers(c.Request.Context(), tenantID)
	if err != nil {
		slog.Error("failed to list tenant members", "tenantId", tenantID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list members"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"members": members})
}

// ---------- Secrets ----------

func (s *PlatformServer) listSecrets(c *gin.Context) {
	tenantID := auth.GetTenantID(c)

	items, err := s.secretsProvider.List(c.Request.Context(), tenantID)
	if err != nil {
		slog.Error("failed to list secrets", "tenantId", tenantID, "error", err)
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
	tenantID := auth.GetTenantID(c)

	var req createSecretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: name, secretType, and data are required"})
		return
	}

	if err := s.secretsProvider.Store(c.Request.Context(), tenantID, req.Name, req.SecretType, req.Data); err != nil {
		slog.Error("failed to create secret", "tenantId", tenantID, "name", req.Name, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create secret"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "secret created", "name": req.Name})
}

func (s *PlatformServer) deleteSecret(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	secretName := c.Param("id")

	if err := s.secretsProvider.Delete(c.Request.Context(), tenantID, secretName); err != nil {
		slog.Error("failed to delete secret", "tenantId", tenantID, "name", secretName, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete secret"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "secret deleted"})
}

// ---------- Migrations ----------

func (s *PlatformServer) listMigrations(c *gin.Context) {
	tenantID := auth.GetTenantID(c)

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	items, total, err := s.orchestrator.ListMigrations(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		slog.Error("failed to list migrations", "tenantId", tenantID, "error", err)
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
	tenantID := auth.GetTenantID(c)

	var req migration.CreateMigrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	m, err := s.orchestrator.CreateMigration(c.Request.Context(), tenantID, req)
	if err != nil {
		slog.Error("failed to create migration", "tenantId", tenantID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create migration"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"migration": m})
}

func (s *PlatformServer) getMigration(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	migrationID := c.Param("id")

	resp, err := s.orchestrator.GetMigrationStatus(c.Request.Context(), tenantID, migrationID)
	if err != nil {
		slog.Error("failed to get migration", "tenantId", tenantID, "migrationId", migrationID, "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "migration not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"migration": resp})
}

func (s *PlatformServer) deleteMigration(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	migrationID := c.Param("id")

	if err := s.orchestrator.DeleteMigration(c.Request.Context(), tenantID, migrationID); err != nil {
		slog.Error("failed to delete migration", "tenantId", tenantID, "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration deleted"})
}

func (s *PlatformServer) pauseMigration(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	migrationID := c.Param("id")

	if err := s.orchestrator.PauseMigration(c.Request.Context(), tenantID, migrationID); err != nil {
		slog.Error("failed to pause migration", "tenantId", tenantID, "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to pause migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration paused"})
}

func (s *PlatformServer) resumeMigration(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	migrationID := c.Param("id")

	if err := s.orchestrator.ResumeMigration(c.Request.Context(), tenantID, migrationID); err != nil {
		slog.Error("failed to resume migration", "tenantId", tenantID, "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resume migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration resumed"})
}

func (s *PlatformServer) cancelMigration(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	migrationID := c.Param("id")

	if err := s.orchestrator.CancelMigration(c.Request.Context(), tenantID, migrationID); err != nil {
		slog.Error("failed to cancel migration", "tenantId", tenantID, "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cancel migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration cancelled"})
}

func (s *PlatformServer) retryMigration(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	migrationID := c.Param("id")

	if err := s.orchestrator.RetryMigration(c.Request.Context(), tenantID, migrationID); err != nil {
		slog.Error("failed to retry migration", "tenantId", tenantID, "migrationId", migrationID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retry migration"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "migration retry initiated"})
}

// ---------- Dashboard ----------

func (s *PlatformServer) getDashboardStats(c *gin.Context) {
	tenantID := auth.GetTenantID(c)
	ctx := c.Request.Context()

	// Fetch all migrations for this tenant (use a large limit to get everything).
	migrations, total, err := s.migrationStore.List(ctx, tenantID, 10000, 0)
	if err != nil {
		slog.Error("failed to get dashboard stats", "tenantId", tenantID, "error", err)
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
