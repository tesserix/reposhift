package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// IgnoreStatusUpdatePredicate ignores status-only updates to prevent reconciliation loops
// This is critical for avoiding unnecessary reconciliations when only status is updated
func IgnoreStatusUpdatePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Only reconcile if generation changed (spec was modified)
			// Status-only updates don't change generation
			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration()
		},
		CreateFunc: func(e event.CreateEvent) bool {
			// Always reconcile on create
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Always reconcile on delete (for finalizers)
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			// Generic events are usually reconciled
			return true
		},
	}
}

// IgnoreDeletePredicate ignores delete events (useful if no finalizers)
func IgnoreDeletePredicate() predicate.Predicate {
	return predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Don't reconcile on delete
			return false
		},
	}
}

// IgnoreDeletionPredicate ignores objects being deleted (when DeletionTimestamp is set)
// Use this when you don't need to reconcile during deletion
func IgnoreDeletionPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Don't reconcile if object is being deleted
			return e.ObjectNew.GetDeletionTimestamp().IsZero()
		},
		CreateFunc: func(e event.CreateEvent) bool {
			// Always reconcile new objects
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Don't reconcile delete events
			return false
		},
	}
}

// PhaseTransitionPredicate only reconciles on phase transitions or new objects
// This requires the object to have a Status.Phase field
type PhaseTransitionPredicate struct {
	GetPhase func(obj interface{}) string
}

func (p PhaseTransitionPredicate) Create(e event.CreateEvent) bool {
	// Always reconcile new objects
	return true
}

func (p PhaseTransitionPredicate) Update(e event.UpdateEvent) bool {
	// Reconcile if:
	// 1. Generation changed (spec updated)
	// 2. Phase changed
	// 3. Object is being deleted

	if e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() {
		return true
	}

	if !e.ObjectNew.GetDeletionTimestamp().IsZero() {
		return true
	}

	// Check if phase changed
	if p.GetPhase != nil {
		oldPhase := p.GetPhase(e.ObjectOld)
		newPhase := p.GetPhase(e.ObjectNew)
		return oldPhase != newPhase
	}

	return false
}

func (p PhaseTransitionPredicate) Delete(e event.DeleteEvent) bool {
	// Reconcile deletes for finalizer handling
	return true
}

func (p PhaseTransitionPredicate) Generic(e event.GenericEvent) bool {
	return true
}

// ResourceVersionChangedPredicate ignores updates where only the resource version changed
// This can happen with updates from other controllers
func ResourceVersionChangedPredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Only reconcile if something other than ResourceVersion changed
			// Generation change indicates spec change
			// DeletionTimestamp indicates deletion started
			return e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() ||
				!e.ObjectNew.GetDeletionTimestamp().IsZero()
		},
	}
}

// AnnotationChangedPredicate reconciles when specific annotation changes
type AnnotationChangedPredicate struct {
	AnnotationKey string
}

func (p AnnotationChangedPredicate) Create(e event.CreateEvent) bool {
	return true
}

func (p AnnotationChangedPredicate) Update(e event.UpdateEvent) bool {
	// Reconcile if generation changed or specific annotation changed
	if e.ObjectNew.GetGeneration() != e.ObjectOld.GetGeneration() {
		return true
	}

	oldAnnotations := e.ObjectOld.GetAnnotations()
	newAnnotations := e.ObjectNew.GetAnnotations()

	oldValue, oldExists := oldAnnotations[p.AnnotationKey]
	newValue, newExists := newAnnotations[p.AnnotationKey]

	// Reconcile if annotation was added, removed, or changed
	return oldExists != newExists || oldValue != newValue
}

func (p AnnotationChangedPredicate) Delete(e event.DeleteEvent) bool {
	return true
}

func (p AnnotationChangedPredicate) Generic(e event.GenericEvent) bool {
	return true
}

// NamespacePredicate filters events by namespace
type NamespacePredicate struct {
	Namespace string
}

func (p NamespacePredicate) Create(e event.CreateEvent) bool {
	return e.Object.GetNamespace() == p.Namespace
}

func (p NamespacePredicate) Update(e event.UpdateEvent) bool {
	return e.ObjectNew.GetNamespace() == p.Namespace
}

func (p NamespacePredicate) Delete(e event.DeleteEvent) bool {
	return e.Object.GetNamespace() == p.Namespace
}

func (p NamespacePredicate) Generic(e event.GenericEvent) bool {
	return e.Object.GetNamespace() == p.Namespace
}

// AndPredicate combines multiple predicates with AND logic
func AndPredicate(predicates ...predicate.Predicate) predicate.Predicate {
	return predicate.And(predicates...)
}

// OrPredicate combines multiple predicates with OR logic
func OrPredicate(predicates ...predicate.Predicate) predicate.Predicate {
	return predicate.Or(predicates...)
}

// GenerationChangedWithLogging provides comprehensive event logging for debugging
// This helps track exactly what triggers reconciliation in production
func GenerationChangedWithLogging() predicate.Predicate {
	logger := log.Log.WithName("predicate")

	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldGen := e.ObjectOld.GetGeneration()
			newGen := e.ObjectNew.GetGeneration()
			oldRV := e.ObjectOld.GetResourceVersion()
			newRV := e.ObjectNew.GetResourceVersion()
			generationChanged := oldGen != newGen

			// Check if finalizers changed (important for initialization flow)
			oldFinalizers := e.ObjectOld.GetFinalizers()
			newFinalizers := e.ObjectNew.GetFinalizers()
			finalizersChanged := len(oldFinalizers) != len(newFinalizers)

			// Check if deletion timestamp was set
			deletionStarted := e.ObjectOld.GetDeletionTimestamp().IsZero() && !e.ObjectNew.GetDeletionTimestamp().IsZero()

			// Reconcile if generation, finalizers, or deletion changed
			shouldReconcile := generationChanged || finalizersChanged || deletionStarted

			// Log update events for debugging
			logger.V(1).Info("Update event received",
				"name", e.ObjectNew.GetName(),
				"namespace", e.ObjectNew.GetNamespace(),
				"oldGeneration", oldGen,
				"newGeneration", newGen,
				"oldResourceVersion", oldRV,
				"newResourceVersion", newRV,
				"generationChanged", generationChanged,
				"finalizersChanged", finalizersChanged,
				"deletionStarted", deletionStarted,
				"willReconcile", shouldReconcile,
			)

			// Reconcile if:
			// 1. Generation changed (spec was modified)
			// 2. Finalizers changed (initialization or cleanup)
			// 3. Deletion started (for finalizer cleanup)
			return shouldReconcile
		},
		CreateFunc: func(e event.CreateEvent) bool {
			logger.Info("Create event received",
				"name", e.Object.GetName(),
				"namespace", e.Object.GetNamespace(),
				"generation", e.Object.GetGeneration(),
			)
			// Always reconcile on create
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			logger.Info("Delete event received",
				"name", e.Object.GetName(),
				"namespace", e.Object.GetNamespace(),
				"deletionTimestamp", e.Object.GetDeletionTimestamp(),
			)
			// Always reconcile on delete (for finalizers)
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			logger.V(1).Info("Generic event received",
				"name", e.Object.GetName(),
				"namespace", e.Object.GetNamespace(),
			)
			// Generic events are usually reconciled
			return true
		},
	}
}
