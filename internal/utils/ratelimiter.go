package utils

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter provides rate limiting functionality
type RateLimiter struct {
	limiter *rate.Limiter
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(requestsPerInterval int, interval time.Duration) *RateLimiter {
	limit := rate.Every(interval / time.Duration(requestsPerInterval))
	return &RateLimiter{
		limiter: rate.NewLimiter(limit, requestsPerInterval),
	}
}

// Wait waits until the rate limiter allows the request
func (r *RateLimiter) Wait(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}

// Allow checks if a request is allowed without waiting
func (r *RateLimiter) Allow() bool {
	return r.limiter.Allow()
}

// Reserve reserves a token for future use
func (r *RateLimiter) Reserve() *rate.Reservation {
	return r.limiter.Reserve()
}
