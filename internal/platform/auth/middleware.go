package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminTokenMiddleware validates a static admin token for access.
// Checks X-Admin-Token header first (recommended), then falls back to
// Authorization: Bearer for compatibility. Using X-Admin-Token avoids
// conflicts with Istio JWT validation on the Authorization header.
func AdminTokenMiddleware(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Admin-Token")
		if token == "" {
			token = extractBearerToken(c)
		}
		if token == "" || token != adminToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing admin token — use X-Admin-Token header"})
			return
		}
		c.Next()
	}
}

func extractBearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}
