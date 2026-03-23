package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ErrorType represents the category of an error
type ErrorType string

const (
	// ErrorTypeRetryable indicates a transient error that should be retried
	ErrorTypeRetryable ErrorType = "Retryable"
	// ErrorTypeTerminal indicates a permanent error that won't be fixed by retrying
	ErrorTypeTerminal ErrorType = "Terminal"
	// ErrorTypeValidation indicates a configuration or validation error
	ErrorTypeValidation ErrorType = "Validation"
)

// ClassifiedError wraps an error with its type classification
type ClassifiedError struct {
	Type       ErrorType
	Err        error
	Message    string
	RetryAfter time.Duration
}

func (e *ClassifiedError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("[%s] %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %v", e.Type, e.Err)
}

func (e *ClassifiedError) Unwrap() error {
	return e.Err
}

// NewRetryableError creates a retryable error
func NewRetryableError(err error, message string) *ClassifiedError {
	return &ClassifiedError{
		Type:       ErrorTypeRetryable,
		Err:        err,
		Message:    message,
		RetryAfter: 0, // Use exponential backoff
	}
}

// NewTerminalError creates a terminal error
func NewTerminalError(err error, message string) *ClassifiedError {
	return &ClassifiedError{
		Type:    ErrorTypeTerminal,
		Err:     err,
		Message: message,
	}
}

// NewValidationError creates a validation error
func NewValidationError(err error, message string) *ClassifiedError {
	return &ClassifiedError{
		Type:    ErrorTypeValidation,
		Err:     err,
		Message: message,
	}
}

// IsRetryable checks if an error is retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	if classErr, ok := err.(*ClassifiedError); ok {
		return classErr.Type == ErrorTypeRetryable
	}

	// Default: treat unknown errors as retryable for safety
	return true
}

// IsTerminal checks if an error is terminal
func IsTerminal(err error) bool {
	if err == nil {
		return false
	}

	if classErr, ok := err.(*ClassifiedError); ok {
		return classErr.Type == ErrorTypeTerminal || classErr.Type == ErrorTypeValidation
	}

	return false
}

// HandleReconcileError handles errors consistently across controllers
// It returns the appropriate ctrl.Result and error for the reconcile function
func HandleReconcileError(ctx context.Context, log logr.Logger, err error, operation string) (ctrl.Result, error) {
	if err == nil {
		return ctrl.Result{}, nil
	}

	// Log with context
	log.Error(err, "Reconciliation error occurred",
		"operation", operation,
		"errorType", classifyErrorType(err))

	// Handle based on error type
	if classErr, ok := err.(*ClassifiedError); ok {
		switch classErr.Type {
		case ErrorTypeRetryable:
			// Return error to trigger exponential backoff
			return ctrl.Result{}, err
		case ErrorTypeTerminal, ErrorTypeValidation:
			// Don't requeue terminal errors
			return ctrl.Result{}, nil
		}
	}

	// Default: return error to trigger retry with backoff
	return ctrl.Result{}, err
}

// classifyErrorType returns the error type as a string for logging
func classifyErrorType(err error) string {
	if classErr, ok := err.(*ClassifiedError); ok {
		return string(classErr.Type)
	}
	return "Unknown"
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// DefaultRetryConfig returns sensible defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:     5,
		InitialInterval: 5 * time.Second,
		MaxInterval:     5 * time.Minute,
		Multiplier:      2.0,
	}
}

// CalculateBackoff calculates the next retry interval using exponential backoff
func CalculateBackoff(attempt int, config RetryConfig) time.Duration {
	if attempt <= 0 {
		return config.InitialInterval
	}

	// Calculate exponential backoff
	interval := float64(config.InitialInterval)
	for i := 0; i < attempt && i < config.MaxAttempts; i++ {
		interval *= config.Multiplier
	}

	result := time.Duration(interval)
	if result > config.MaxInterval {
		result = config.MaxInterval
	}

	return result
}

// ShouldSkipReconcile determines if reconciliation should be skipped
// based on observedGeneration
func ShouldSkipReconcile(generation int64, observedGeneration int64) bool {
	// Skip if spec hasn't changed (generation matches observedGeneration)
	return generation > 0 && generation == observedGeneration
}

// ReconcileResult holds the result of a reconciliation operation
type ReconcileResult struct {
	Requeue      bool
	RequeueAfter time.Duration
	UpdateStatus bool
}

// ResultSuccess returns a successful result with no requeue
func ResultSuccess() ReconcileResult {
	return ReconcileResult{
		Requeue:      false,
		RequeueAfter: 0,
		UpdateStatus: false,
	}
}

// ResultSuccessWithStatusUpdate returns success but requests status update
func ResultSuccessWithStatusUpdate() ReconcileResult {
	return ReconcileResult{
		Requeue:      false,
		RequeueAfter: 0,
		UpdateStatus: true,
	}
}

// ResultRequeue returns a result that requeues immediately
func ResultRequeue() ReconcileResult {
	return ReconcileResult{
		Requeue:      true,
		RequeueAfter: 0,
		UpdateStatus: false,
	}
}

// ResultRequeueAfter returns a result that requeues after a duration
func ResultRequeueAfter(duration time.Duration) ReconcileResult {
	return ReconcileResult{
		Requeue:      true,
		RequeueAfter: duration,
		UpdateStatus: false,
	}
}

// ResultRequeueWithStatus returns a result that requeues and updates status
func ResultRequeueWithStatus(duration time.Duration) ReconcileResult {
	return ReconcileResult{
		Requeue:      true,
		RequeueAfter: duration,
		UpdateStatus: true,
	}
}

// ToControllerResult converts ReconcileResult to ctrl.Result
func (r ReconcileResult) ToControllerResult() ctrl.Result {
	return ctrl.Result{
		Requeue:      r.Requeue,
		RequeueAfter: r.RequeueAfter,
	}
}
