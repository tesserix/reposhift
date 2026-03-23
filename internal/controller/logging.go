package controller

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// LogKeys defines standard logging field keys for consistency
const (
	LogKeyNamespace    = "namespace"
	LogKeyName         = "name"
	LogKeyPhase        = "phase"
	LogKeyOperation    = "operation"
	LogKeyResource     = "resource"
	LogKeyResourceType = "resourceType"
	LogKeyProgress     = "progress"
	LogKeyDuration     = "duration"
	LogKeyError        = "error"
	LogKeyRetryAttempt = "retryAttempt"
	LogKeyGeneration   = "generation"
	LogKeyObservedGen  = "observedGeneration"
	LogKeyRequeue      = "requeue"
	LogKeyRequeueAfter = "requeueAfter"
)

// ControllerLogger provides structured logging helpers for controllers
type ControllerLogger struct {
	log logr.Logger
}

// NewControllerLogger creates a new controller logger
func NewControllerLogger(ctx context.Context) *ControllerLogger {
	return &ControllerLogger{
		log: log.FromContext(ctx),
	}
}

// NewControllerLoggerWithName creates a logger with a specific name
func NewControllerLoggerWithName(ctx context.Context, name string) *ControllerLogger {
	return &ControllerLogger{
		log: log.FromContext(ctx).WithName(name),
	}
}

// WithObject returns a logger with object metadata
func (cl *ControllerLogger) WithObject(obj client.Object) *ControllerLogger {
	return &ControllerLogger{
		log: cl.log.WithValues(
			LogKeyNamespace, obj.GetNamespace(),
			LogKeyName, obj.GetName(),
			LogKeyGeneration, obj.GetGeneration(),
		),
	}
}

// WithPhase returns a logger with phase information
func (cl *ControllerLogger) WithPhase(phase string) *ControllerLogger {
	return &ControllerLogger{
		log: cl.log.WithValues(LogKeyPhase, phase),
	}
}

// WithOperation returns a logger with operation information
func (cl *ControllerLogger) WithOperation(operation string) *ControllerLogger {
	return &ControllerLogger{
		log: cl.log.WithValues(LogKeyOperation, operation),
	}
}

// WithResource returns a logger with resource information
func (cl *ControllerLogger) WithResource(resourceName, resourceType string) *ControllerLogger {
	return &ControllerLogger{
		log: cl.log.WithValues(
			LogKeyResource, resourceName,
			LogKeyResourceType, resourceType,
		),
	}
}

// InfoReconcileStart logs the start of reconciliation
func (cl *ControllerLogger) InfoReconcileStart(obj client.Object) {
	cl.log.Info("Reconciliation started",
		LogKeyNamespace, obj.GetNamespace(),
		LogKeyName, obj.GetName(),
		LogKeyGeneration, obj.GetGeneration())
}

// InfoReconcileComplete logs successful completion
func (cl *ControllerLogger) InfoReconcileComplete(obj client.Object, requeue bool, requeueAfter string) {
	cl.log.Info("Reconciliation completed",
		LogKeyNamespace, obj.GetNamespace(),
		LogKeyName, obj.GetName(),
		LogKeyRequeue, requeue,
		LogKeyRequeueAfter, requeueAfter)
}

// InfoPhaseTransition logs a phase transition
func (cl *ControllerLogger) InfoPhaseTransition(oldPhase, newPhase string) {
	cl.log.Info("Phase transition",
		"fromPhase", oldPhase,
		"toPhase", newPhase)
}

// InfoSkipped logs when reconciliation is skipped
func (cl *ControllerLogger) InfoSkipped(reason string, generation, observedGeneration int64) {
	cl.log.V(1).Info("Reconciliation skipped",
		"reason", reason,
		LogKeyGeneration, generation,
		LogKeyObservedGen, observedGeneration)
}

// InfoProgress logs progress updates (use sparingly to avoid log spam)
func (cl *ControllerLogger) InfoProgress(percentage int, currentStep string) {
	// Only log at 10% intervals to avoid log spam
	if percentage%10 == 0 || percentage == 100 {
		cl.log.Info("Migration progress",
			LogKeyProgress, percentage,
			"currentStep", currentStep)
	} else {
		// Use debug level for non-milestone progress
		cl.log.V(1).Info("Migration progress",
			LogKeyProgress, percentage,
			"currentStep", currentStep)
	}
}

// InfoStatusUpdate logs status updates
func (cl *ControllerLogger) InfoStatusUpdate(reason string) {
	cl.log.V(1).Info("Status update",
		"reason", reason)
}

// InfoRetry logs retry attempts
func (cl *ControllerLogger) InfoRetry(attempt int, maxAttempts int, operation string) {
	cl.log.Info("Retrying operation",
		LogKeyRetryAttempt, attempt,
		"maxAttempts", maxAttempts,
		LogKeyOperation, operation)
}

// WarnValidationFailed logs validation failures
func (cl *ControllerLogger) WarnValidationFailed(field, reason string) {
	cl.log.Info("Validation failed",
		"field", field,
		"reason", reason)
}

// WarnRateLimitApproaching logs rate limit warnings
func (cl *ControllerLogger) WarnRateLimitApproaching(service string, remaining, limit int) {
	cl.log.Info("Rate limit approaching",
		"service", service,
		"remaining", remaining,
		"limit", limit,
		"percentage", (remaining*100)/limit)
}

// ErrorReconcile logs reconciliation errors
func (cl *ControllerLogger) ErrorReconcile(err error, operation string) {
	cl.log.Error(err, "Reconciliation error",
		LogKeyOperation, operation,
		"errorType", classifyErrorType(err))
}

// ErrorPhaseTransition logs errors during phase transitions
func (cl *ControllerLogger) ErrorPhaseTransition(err error, fromPhase, toPhase string) {
	cl.log.Error(err, "Phase transition failed",
		"fromPhase", fromPhase,
		"toPhase", toPhase)
}

// ErrorStatusUpdate logs status update failures
func (cl *ControllerLogger) ErrorStatusUpdate(err error) {
	cl.log.Error(err, "Failed to update status")
}

// ErrorFinalizer logs finalizer errors
func (cl *ControllerLogger) ErrorFinalizer(err error, operation string) {
	cl.log.Error(err, "Finalizer operation failed",
		LogKeyOperation, operation)
}

// ErrorAPICall logs API call failures
func (cl *ControllerLogger) ErrorAPICall(err error, service, endpoint string) {
	cl.log.Error(err, "API call failed",
		"service", service,
		"endpoint", endpoint)
}

// DebugAPICall logs API calls (verbose)
func (cl *ControllerLogger) DebugAPICall(service, method, endpoint string) {
	cl.log.V(2).Info("API call",
		"service", service,
		"method", method,
		"endpoint", endpoint)
}

// DebugStatusChange logs status field changes (very verbose)
func (cl *ControllerLogger) DebugStatusChange(field string, oldValue, newValue interface{}) {
	cl.log.V(2).Info("Status field changed",
		"field", field,
		"oldValue", oldValue,
		"newValue", newValue)
}

// DebugCacheHit logs cache operations (very verbose)
func (cl *ControllerLogger) DebugCacheHit(cacheType string, hit bool) {
	cl.log.V(2).Info("Cache operation",
		"cacheType", cacheType,
		"hit", hit)
}

// GetUnderlyingLogger returns the underlying logr.Logger for advanced use
func (cl *ControllerLogger) GetUnderlyingLogger() logr.Logger {
	return cl.log
}

// V returns a logger with the specified verbosity level
func (cl *ControllerLogger) V(level int) *ControllerLogger {
	return &ControllerLogger{
		log: cl.log.V(level),
	}
}

// WithValues returns a logger with additional key-value pairs
func (cl *ControllerLogger) WithValues(keysAndValues ...interface{}) *ControllerLogger {
	return &ControllerLogger{
		log: cl.log.WithValues(keysAndValues...),
	}
}
