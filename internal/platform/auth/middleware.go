package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AdminTokenMiddleware validates a static bearer token for admin access.
func AdminTokenMiddleware(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractBearerToken(c)
		if token == "" || token != adminToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing admin token"})
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
