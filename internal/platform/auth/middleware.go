package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	ctxKeyUserID   = "user_id"
	ctxKeyTenantID = "tenant_id"
	ctxKeyRole     = "role"
)

// JWTAuthMiddleware validates the Bearer JWT token and sets user_id, tenant_id,
// and role in the Gin context for downstream handlers.
func JWTAuthMiddleware(jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, ok := extractBearerToken(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed authorization header"})
			return
		}

		claims, err := ValidateToken(tokenString, jwtSecret)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired token"})
			return
		}

		c.Set(ctxKeyUserID, claims.UserID)
		c.Set(ctxKeyTenantID, claims.TenantID)
		c.Set(ctxKeyRole, claims.Role)
		c.Next()
	}
}

// AdminTokenMiddleware validates a static bearer token for self-hosted mode,
// granting admin-level access without JWT-based identity. The userID and
// tenantID must be the actual database UUIDs from the default tenant setup.
func AdminTokenMiddleware(adminToken, userID, tenantID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, ok := extractBearerToken(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or malformed authorization header"})
			return
		}

		if tokenString != adminToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
			return
		}

		c.Set(ctxKeyUserID, userID)
		c.Set(ctxKeyTenantID, tenantID)
		c.Set(ctxKeyRole, "admin")
		c.Next()
	}
}

// RequireRole returns middleware that ensures the authenticated user has one of
// the specified roles. Must be used after JWTAuthMiddleware or AdminTokenMiddleware.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, r := range roles {
		allowed[r] = struct{}{}
	}

	return func(c *gin.Context) {
		role := GetRole(c)
		if _, ok := allowed[role]; !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}

// GetUserID returns the authenticated user's ID from the Gin context.
func GetUserID(c *gin.Context) string {
	return c.GetString(ctxKeyUserID)
}

// GetTenantID returns the authenticated user's tenant ID from the Gin context.
func GetTenantID(c *gin.Context) string {
	return c.GetString(ctxKeyTenantID)
}

// GetRole returns the authenticated user's role from the Gin context.
func GetRole(c *gin.Context) string {
	return c.GetString(ctxKeyRole)
}

// extractBearerToken pulls the token from the Authorization: Bearer <token> header.
func extractBearerToken(c *gin.Context) (string, bool) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return "", false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}
