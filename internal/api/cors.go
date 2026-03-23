package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSConfig defines CORS middleware settings.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// CORS returns a Gin middleware that handles Cross-Origin Resource Sharing.
func CORS(config CORSConfig) gin.HandlerFunc {
	allowedOrigins := make(map[string]struct{}, len(config.AllowedOrigins))
	hasWildcard := false
	for _, o := range config.AllowedOrigins {
		if o == "*" {
			hasWildcard = true
		}
		allowedOrigins[o] = struct{}{}
	}

	methods := strings.Join(config.AllowedMethods, ", ")
	headers := strings.Join(config.AllowedHeaders, ", ")
	exposed := strings.Join(config.ExposedHeaders, ", ")
	maxAge := strconv.Itoa(config.MaxAge)

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if origin != "" {
			_, ok := allowedOrigins[origin]
			if ok || hasWildcard {
				if hasWildcard && !config.AllowCredentials {
					c.Header("Access-Control-Allow-Origin", "*")
				} else {
					c.Header("Access-Control-Allow-Origin", origin)
				}
				c.Header("Access-Control-Allow-Methods", methods)
				c.Header("Access-Control-Allow-Headers", headers)
				if exposed != "" {
					c.Header("Access-Control-Expose-Headers", exposed)
				}
				if config.AllowCredentials {
					c.Header("Access-Control-Allow-Credentials", "true")
				}
				if config.MaxAge > 0 {
					c.Header("Access-Control-Max-Age", maxAge)
				}
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
