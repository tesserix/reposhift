package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/tesserix/reposhift/internal/services"
)

// Middleware provides HTTP middleware functions
type Middleware struct {
	rateLimiter         *services.RateLimiter
	globalRateLimiter   *services.GlobalRateLimiter
	adaptiveRateLimiter *services.AdaptiveRateLimiter
	validator           *validator.Validate
}

// NewMiddleware creates a new middleware instance
func NewMiddleware() *Middleware {
	return &Middleware{
		rateLimiter:         services.NewRateLimiter(100, time.Minute), // 100 requests per minute per client
		globalRateLimiter:   services.NewGlobalRateLimiter(1000, 100),  // 1000 requests per second globally
		adaptiveRateLimiter: services.NewAdaptiveRateLimiter(100, time.Minute, 5*time.Minute),
		validator:           validator.New(),
	}
}

// RateLimitMiddleware applies rate limiting based on client IP or API key
func (m *Middleware) RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get client identifier (IP address or API key)
		clientID := m.getClientIdentifier(r)

		// Check global rate limit first
		if !m.globalRateLimiter.Allow() {
			m.writeRateLimitError(w, "Global rate limit exceeded", 429)
			return
		}

		// Check per-client rate limit
		if !m.rateLimiter.Allow(clientID) {
			m.writeRateLimitError(w, "Rate limit exceeded for client", 429)
			return
		}

		// Check adaptive rate limiter
		backoff := m.adaptiveRateLimiter.GetBackoff(clientID)
		if backoff > 0 {
			retryAfter := int(backoff.Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}

			w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
			m.writeRateLimitError(w, "Service temporarily unavailable, please retry later", 503)
			return
		}

		// Create a response wrapper to capture status code
		wrappedWriter := &responseWriter{ResponseWriter: w, statusCode: 200}

		next.ServeHTTP(wrappedWriter, r)

		// Update adaptive rate limiter based on response
		if wrappedWriter.statusCode >= 400 {
			m.adaptiveRateLimiter.ReportError(clientID, wrappedWriter.statusCode)
		} else {
			m.adaptiveRateLimiter.ReportSuccess(clientID)
		}
	})
}

// AuthenticationMiddleware validates API authentication
func (m *Middleware) AuthenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for health endpoints
		if strings.HasPrefix(r.URL.Path, "/api/v1/utils/health") ||
			strings.HasPrefix(r.URL.Path, "/api/v1/utils/ready") ||
			strings.HasPrefix(r.URL.Path, "/metrics") {
			next.ServeHTTP(w, r)
			return
		}

		// For discovery endpoints, validate Azure credentials in query params
		if strings.HasPrefix(r.URL.Path, "/api/v1/discovery/") {
			if !m.validateAzureCredentials(r) {
				m.writeAuthError(w, "Invalid or missing Azure DevOps credentials", 401)
				return
			}
		}

		// Add client info to context
		ctx := context.WithValue(r.Context(), "client_id", m.getClientIdentifier(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ValidationMiddleware validates request payloads
func (m *Middleware) ValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only validate POST and PUT requests with JSON content
		if (r.Method == "POST" || r.Method == "PUT") &&
			strings.Contains(r.Header.Get("Content-Type"), "application/json") {

			// Validate content length
			if r.ContentLength > 10*1024*1024 { // 10MB limit
				m.writeValidationError(w, "Request body too large", 413)
				return
			}

			// Validate required headers
			if r.Header.Get("Content-Type") == "" {
				m.writeValidationError(w, "Content-Type header is required", 400)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// SecurityMiddleware adds security headers and protections
func (m *Middleware) SecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Remove server information
		w.Header().Set("Server", "")

		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs HTTP requests and responses
func (m *Middleware) LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Log request details
		duration := time.Since(start)
		clientID := m.getClientIdentifier(r)

		log.Log.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
			"status", wrapped.statusCode,
			"duration", duration,
			"client_id", clientID,
			"user_agent", r.Header.Get("User-Agent"),
			"content_length", r.ContentLength,
			"remote_addr", r.RemoteAddr,
		)

		// Log slow requests
		if duration > 5*time.Second {
			log.Log.Info("Slow request detected",
				"method", r.Method,
				"path", r.URL.Path,
				"duration", duration,
				"client_id", clientID,
			)
		}

		// Log errors
		if wrapped.statusCode >= 400 {
			log.Log.Info("Error response",
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.statusCode,
				"client_id", clientID,
			)
		}
	})
}

// CORSMiddleware handles Cross-Origin Resource Sharing
func (m *Middleware) CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-API-Key, X-ADO-PAT")
		w.Header().Set("Access-Control-Expose-Headers", "X-Total-Count, X-Rate-Limit-Remaining, X-Rate-Limit-Reset, Retry-After")
		w.Header().Set("Access-Control-Max-Age", "86400")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RecoveryMiddleware recovers from panics and returns proper error responses
func (m *Middleware) RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the panic with stack trace
				stackTrace := debug.Stack()
				log.Log.Error(fmt.Errorf("panic recovered: %v", err), "Panic in HTTP handler",
					"method", r.Method,
					"path", r.URL.Path,
					"client_id", m.getClientIdentifier(r),
					"stack", string(stackTrace),
				)

				m.writeInternalError(w, "Internal server error", 500)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// CompressionMiddleware handles response compression
func (m *Middleware) CompressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if client accepts gzip encoding
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			// Create a gzip response writer
			gzipWriter := newGzipResponseWriter(w)
			defer gzipWriter.Close()

			// Use the gzip writer instead
			next.ServeHTTP(gzipWriter, r)
			return
		}

		// No compression requested
		next.ServeHTTP(w, r)
	})
}

// TimeoutMiddleware adds request timeouts
func (m *Middleware) TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create a context with timeout
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			// Create a channel to signal when the handler is done
			done := make(chan struct{})

			// Create a response writer that can be checked for completion
			tw := &timeoutWriter{
				w:     w,
				h:     make(http.Header),
				ready: make(chan struct{}),
			}

			// Process the request in a goroutine
			go func() {
				next.ServeHTTP(tw, r.WithContext(ctx))
				close(done)
			}()

			// Wait for either completion or timeout
			select {
			case <-done:
				// Request completed normally
				tw.copyToResponse()
			case <-ctx.Done():
				// Request timed out
				if ctx.Err() == context.DeadlineExceeded {
					w.WriteHeader(http.StatusGatewayTimeout)
					w.Write([]byte("Request timed out"))
				}
			}
		})
	}
}

// Helper functions

func (m *Middleware) getClientIdentifier(r *http.Request) string {
	// Try to get client ID from various sources
	if clientID := r.Header.Get("X-API-Key"); clientID != "" {
		return clientID
	}

	if clientID := r.Header.Get("X-Client-ID"); clientID != "" {
		return clientID
	}

	if clientID := r.URL.Query().Get("client_id"); clientID != "" {
		return clientID
	}

	// Fall back to IP address
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		return strings.Split(forwarded, ",")[0]
	}

	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	return r.RemoteAddr
}

func (m *Middleware) validateAzureCredentials(r *http.Request) bool {
	clientID := r.URL.Query().Get("client_id")
	clientSecret := r.URL.Query().Get("client_secret")
	tenantID := r.URL.Query().Get("tenant_id")

	// Basic validation
	if clientID == "" || clientSecret == "" || tenantID == "" {
		return false
	}

	// In a real implementation, you might do additional validation

	return true
}

func (m *Middleware) writeRateLimitError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Rate-Limit-Exceeded", "true")
	w.WriteHeader(status)

	retryAfter := 60
	if status == 503 {
		// For 503 Service Unavailable, use the Retry-After header value
		if retryAfterStr := w.Header().Get("Retry-After"); retryAfterStr != "" {
			if retryAfterInt, err := strconv.Atoi(retryAfterStr); err == nil {
				retryAfter = retryAfterInt
			}
		}
	}

	response := map[string]interface{}{
		"error":       message,
		"code":        "RATE_LIMIT_EXCEEDED",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"retry_after": retryAfter,
	}

	json.NewEncoder(w).Encode(response)
}

func (m *Middleware) writeAuthError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"error":     message,
		"code":      "AUTHENTICATION_FAILED",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(response)
}

func (m *Middleware) writeValidationError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"error":     message,
		"code":      "VALIDATION_ERROR",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(response)
}

func (m *Middleware) writeInternalError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"error":     message,
		"code":      "INTERNAL_ERROR",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(response)
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.statusCode = code
		rw.written = true
		rw.ResponseWriter.WriteHeader(code)
	}
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.statusCode = http.StatusOK
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// gzipResponseWriter implements a gzip response writer
type gzipResponseWriter struct {
	http.ResponseWriter
	gzipWriter *gzip.Writer
}

func newGzipResponseWriter(w http.ResponseWriter) *gzipResponseWriter {
	w.Header().Set("Content-Encoding", "gzip")
	gz := gzip.NewWriter(w)
	return &gzipResponseWriter{ResponseWriter: w, gzipWriter: gz}
}

func (gzw *gzipResponseWriter) Write(b []byte) (int, error) {
	return gzw.gzipWriter.Write(b)
}

func (gzw *gzipResponseWriter) Close() {
	gzw.gzipWriter.Close()
}

// timeoutWriter is a response writer that can be used to detect timeouts
type timeoutWriter struct {
	w     http.ResponseWriter
	h     http.Header
	code  int
	body  []byte
	ready chan struct{}
}

func (tw *timeoutWriter) Header() http.Header {
	return tw.h
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.code = code
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.body = append(tw.body, b...)
	return len(b), nil
}

func (tw *timeoutWriter) copyToResponse() {
	// Copy headers
	for k, v := range tw.h {
		tw.w.Header()[k] = v
	}

	// Set status code
	if tw.code != 0 {
		tw.w.WriteHeader(tw.code)
	}

	// Write body
	if len(tw.body) > 0 {
		tw.w.Write(tw.body)
	}
}
