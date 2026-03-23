package services

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mutex    sync.RWMutex
	rate     rate.Limit
	burst    int
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requestsPerTimeUnit int, timeUnit time.Duration) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rate:     rate.Limit(float64(requestsPerTimeUnit) / timeUnit.Seconds()),
		burst:    requestsPerTimeUnit,
	}
}

// Wait blocks until the rate limiter allows the request
func (r *RateLimiter) Wait(ctx context.Context, key string) error {
	limiter := r.getLimiter(key)
	return limiter.Wait(ctx)
}

// Allow checks if the request is allowed immediately
func (r *RateLimiter) Allow(key string) bool {
	limiter := r.getLimiter(key)
	return limiter.Allow()
}

func (r *RateLimiter) getLimiter(key string) *rate.Limiter {
	r.mutex.RLock()
	limiter, exists := r.limiters[key]
	r.mutex.RUnlock()

	if !exists {
		r.mutex.Lock()
		// Double check after acquiring write lock
		if limiter, exists = r.limiters[key]; !exists {
			limiter = rate.NewLimiter(r.rate, r.burst)
			r.limiters[key] = limiter
		}
		r.mutex.Unlock()
	}

	return limiter
}

// AdaptiveRateLimiter provides adaptive rate limiting with error feedback
type AdaptiveRateLimiter struct {
	baseLimiter     *RateLimiter
	errorCounts     map[string]int
	errorMutex      sync.RWMutex
	backoffDuration time.Duration
}

// NewAdaptiveRateLimiter creates a new adaptive rate limiter
func NewAdaptiveRateLimiter(requestsPerTimeUnit int, timeUnit time.Duration, backoffDuration time.Duration) *AdaptiveRateLimiter {
	return &AdaptiveRateLimiter{
		baseLimiter:     NewRateLimiter(requestsPerTimeUnit, timeUnit),
		errorCounts:     make(map[string]int),
		backoffDuration: backoffDuration,
	}
}

// Wait blocks until the adaptive rate limiter allows the request
func (a *AdaptiveRateLimiter) Wait(ctx context.Context, key string) error {
	// Check if we need to apply backoff
	a.errorMutex.RLock()
	errorCount := a.errorCounts[key]
	a.errorMutex.RUnlock()

	if errorCount > 0 {
		backoffTime := time.Duration(errorCount) * a.backoffDuration
		select {
		case <-time.After(backoffTime):
			// Continue with rate limiting
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return a.baseLimiter.Wait(ctx, key)
}

// ReportError reports an error for adaptive rate limiting
func (a *AdaptiveRateLimiter) ReportError(key string, statusCode int) {
	a.errorMutex.Lock()
	defer a.errorMutex.Unlock()

	// Increase error count for 5xx errors or rate limit errors
	if statusCode >= 500 || statusCode == 429 {
		a.errorCounts[key]++

		// Cap the error count to prevent excessive backoff
		if a.errorCounts[key] > 10 {
			a.errorCounts[key] = 10
		}
	}
}

// ReportSuccess reports a successful request
func (a *AdaptiveRateLimiter) ReportSuccess(key string) {
	a.errorMutex.Lock()
	defer a.errorMutex.Unlock()

	// Decrease error count on success
	if a.errorCounts[key] > 0 {
		a.errorCounts[key]--
	}
}

// Reset resets the error count for a key
func (a *AdaptiveRateLimiter) Reset(key string) {
	a.errorMutex.Lock()
	defer a.errorMutex.Unlock()

	delete(a.errorCounts, key)
}

// GlobalRateLimiter provides global rate limiting functionality across all clients
type GlobalRateLimiter struct {
	limiter *rate.Limiter
}

// NewGlobalRateLimiter creates a new global rate limiter
func NewGlobalRateLimiter(requestsPerSecond int, burst int) *GlobalRateLimiter {
	return &GlobalRateLimiter{
		limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), burst),
	}
}

// Allow checks if the request is allowed globally
func (g *GlobalRateLimiter) Allow() bool {
	return g.limiter.Allow()
}

// Wait blocks until the global rate limiter allows the request
func (g *GlobalRateLimiter) Wait(ctx context.Context) error {
	return g.limiter.Wait(ctx)
}

// GetBackoff returns the backoff duration for a key (for adaptive rate limiting)
func (a *AdaptiveRateLimiter) GetBackoff(key string) time.Duration {
	a.errorMutex.RLock()
	errorCount := a.errorCounts[key]
	a.errorMutex.RUnlock()

	if errorCount > 0 {
		return time.Duration(errorCount) * a.backoffDuration
	}
	return 0
}
