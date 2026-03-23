package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// Migration API handlers

func (s *Server) handleListMigrations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}

	// Build a combined list of all migrations
	allMigrations := []map[string]interface{}{}

	// List AdoToGitMigration resources
	var adoMigrationList migrationv1.AdoToGitMigrationList
	if err := s.client.List(ctx, &adoMigrationList, listOpts...); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to list ADO migrations: "+err.Error())
		return
	}

	for _, m := range adoMigrationList.Items {
		allMigrations = append(allMigrations, map[string]interface{}{
			"name":              m.Name,
			"namespace":         m.Namespace,
			"kind":              "AdoToGitMigration",
			"phase":             string(m.Status.Phase),
			"startTime":         m.Status.StartTime,
			"completionTime":    m.Status.CompletionTime,
			"progress":          m.Status.Progress,
			"labels":            m.Labels,
			"creationTimestamp": m.CreationTimestamp,
		})
	}

	// List WorkItemMigration resources
	var workItemMigrationList migrationv1.WorkItemMigrationList
	if err := s.client.List(ctx, &workItemMigrationList, listOpts...); err != nil {
		// Log warning but continue - WorkItemMigration CRD might not exist
		fmt.Printf("Warning: Failed to list WorkItemMigrations: %v\n", err)
	} else {
		for _, m := range workItemMigrationList.Items {
			allMigrations = append(allMigrations, map[string]interface{}{
				"name":           m.Name,
				"namespace":      m.Namespace,
				"kind":           "WorkItemMigration",
				"phase":          string(m.Status.Phase),
				"startTime":      m.Status.StartTime,
				"completionTime": m.Status.CompletionTime,
				"progress": map[string]interface{}{
					"itemsDiscovered": m.Status.Progress.ItemsDiscovered,
					"itemsMigrated":   m.Status.Progress.ItemsMigrated,
					"itemsFailed":     m.Status.Progress.ItemsFailed,
					"itemsSkipped":    m.Status.Progress.ItemsSkipped,
					"percentage":      m.Status.Progress.Percentage,
					"currentBatch":    m.Status.Progress.CurrentBatch,
					"totalBatches":    m.Status.Progress.TotalBatches,
					"currentStep":     m.Status.Progress.CurrentStep,
				},
				"labels":            m.Labels,
				"creationTimestamp": m.CreationTimestamp,
			})
		}
	}

	// List MonoRepoMigration resources
	var monoRepoMigrationList migrationv1.MonoRepoMigrationList
	if err := s.client.List(ctx, &monoRepoMigrationList, listOpts...); err != nil {
		// Log warning but continue - MonoRepoMigration CRD might not exist
		fmt.Printf("Warning: Failed to list MonoRepoMigrations: %v\n", err)
	} else {
		for _, m := range monoRepoMigrationList.Items {
			allMigrations = append(allMigrations, map[string]interface{}{
				"name":              m.Name,
				"namespace":         m.Namespace,
				"kind":              "MonoRepoMigration",
				"phase":             string(m.Status.Phase),
				"startTime":         m.Status.StartTime,
				"completionTime":    m.Status.CompletionTime,
				"progress":          m.Status.Progress,
				"labels":            m.Labels,
				"creationTimestamp": m.CreationTimestamp,
			})
		}
	}

	// Sort by creation timestamp (most recent first)
	sort.Slice(allMigrations, func(i, j int) bool {
		ti, _ := allMigrations[i]["creationTimestamp"].(metav1.Time)
		tj, _ := allMigrations[j]["creationTimestamp"].(metav1.Time)
		return ti.After(tj.Time)
	})

	// Apply filtering if requested
	phase := s.getQueryParam(r, "phase")
	migType := s.getQueryParam(r, "type")

	filteredMigrations := []map[string]interface{}{}
	for _, m := range allMigrations {
		// Filter by phase
		if phase != "" && m["phase"] != phase {
			continue
		}
		// Filter by type (kind)
		if migType != "" {
			kind := m["kind"].(string)
			labels, _ := m["labels"].(map[string]string)
			labelType := ""
			if labels != nil {
				labelType = labels["migrationType"]
			}
			// Check if type matches kind or migrationType label
			if kind != migType && labelType != migType {
				continue
			}
		}
		filteredMigrations = append(filteredMigrations, m)
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"migrations": filteredMigrations,
		"count":      len(filteredMigrations),
	})
}

func (s *Server) handleCreateMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Decode into a generic map first to check the kind
	var rawData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rawData); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Check the kind field to determine which CRD type to create
	kind, _ := rawData["kind"].(string)

	// Route to appropriate handler based on kind
	if kind == "WorkItemMigration" {
		s.createWorkItemMigration(ctx, w, rawData)
		return
	}

	if kind == "MonoRepoMigration" {
		s.createMonoRepoMigrationFromRaw(ctx, w, rawData)
		return
	}

	// Default to AdoToGitMigration for backward compatibility
	s.createAdoToGitMigration(ctx, w, rawData)
}

// createWorkItemMigration handles creation of WorkItemMigration CRDs
func (s *Server) createWorkItemMigration(ctx context.Context, w http.ResponseWriter, rawData map[string]interface{}) {
	// Re-marshal and unmarshal to get the proper type
	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Failed to process request: "+err.Error())
		return
	}

	var migration migrationv1.WorkItemMigration
	if err := json.Unmarshal(jsonBytes, &migration); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid WorkItemMigration body: "+err.Error())
		return
	}

	if migration.Namespace == "" {
		migration.Namespace = "default"
	}

	// Handle inline GitHub token - create a temporary secret
	if migration.Spec.Target.Auth.Token != "" {
		secretName := migration.Name + "-github-token"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: migration.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by":                "ado-migration-api",
					"migration.ado-to-git-migration.io/migration": migration.Name,
					"migration.ado-to-git-migration.io/type":      "github-oauth-token",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"token": migration.Spec.Target.Auth.Token,
			},
		}

		// Create the secret
		if err := s.client.Create(ctx, secret); err != nil {
			// If secret already exists, update it
			if errors.IsAlreadyExists(err) {
				existingSecret := &corev1.Secret{}
				if getErr := s.client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: migration.Namespace}, existingSecret); getErr == nil {
					existingSecret.StringData = secret.StringData
					if updateErr := s.client.Update(ctx, existingSecret); updateErr != nil {
						s.writeError(w, http.StatusInternalServerError, "Failed to update GitHub token secret: "+updateErr.Error())
						return
					}
				}
			} else {
				s.writeError(w, http.StatusInternalServerError, "Failed to create GitHub token secret: "+err.Error())
				return
			}
		}

		// Update migration to use tokenRef instead of inline token
		migration.Spec.Target.Auth.TokenRef = &migrationv1.SecretReference{
			Name: secretName,
			Key:  "token",
		}
		migration.Spec.Target.Auth.Token = "" // Clear inline token
	}

	// Set default values for work item migration
	if migration.Spec.Settings.BatchSize == 0 {
		migration.Spec.Settings.BatchSize = 50
	}
	if migration.Spec.Settings.BatchDelaySeconds == 0 {
		migration.Spec.Settings.BatchDelaySeconds = 60
	}
	if migration.Spec.Settings.TimeoutMinutes == 0 {
		migration.Spec.Settings.TimeoutMinutes = 360 // 6 hours default
	}

	if err := s.client.Create(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to create WorkItemMigration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, migration)
}

// createAdoToGitMigration handles creation of AdoToGitMigration CRDs (repository/pipeline migrations)
func (s *Server) createAdoToGitMigration(ctx context.Context, w http.ResponseWriter, rawData map[string]interface{}) {
	// Re-marshal and unmarshal to get the proper type
	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Failed to process request: "+err.Error())
		return
	}

	var migration migrationv1.AdoToGitMigration
	if err := json.Unmarshal(jsonBytes, &migration); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid AdoToGitMigration body: "+err.Error())
		return
	}

	if migration.Namespace == "" {
		migration.Namespace = "default"
	}

	// Handle inline GitHub token - create a temporary secret
	if migration.Spec.Target.Auth.Token != "" {
		secretName := migration.Name + "-github-token"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: migration.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by":                "ado-migration-api",
					"migration.ado-to-git-migration.io/migration": migration.Name,
					"migration.ado-to-git-migration.io/type":      "github-oauth-token",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"token": migration.Spec.Target.Auth.Token,
			},
		}

		// Create the secret
		if err := s.client.Create(ctx, secret); err != nil {
			// If secret already exists, update it
			if errors.IsAlreadyExists(err) {
				existingSecret := &corev1.Secret{}
				if getErr := s.client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: migration.Namespace}, existingSecret); getErr == nil {
					existingSecret.StringData = secret.StringData
					if updateErr := s.client.Update(ctx, existingSecret); updateErr != nil {
						s.writeError(w, http.StatusInternalServerError, "Failed to update GitHub token secret: "+updateErr.Error())
						return
					}
				}
			} else {
				s.writeError(w, http.StatusInternalServerError, "Failed to create GitHub token secret: "+err.Error())
				return
			}
		}

		// Update migration to use tokenRef instead of inline token
		migration.Spec.Target.Auth.TokenRef = &migrationv1.SecretReference{
			Name: secretName,
			Key:  "token",
		}
		migration.Spec.Target.Auth.Token = "" // Clear inline token
	}

	// Set default values
	if migration.Spec.Settings.MaxHistoryDays == 0 {
		migration.Spec.Settings.MaxHistoryDays = 730 // Default to 2 years (matches CRD default)
	}
	if migration.Spec.Settings.MaxCommitCount == 0 {
		migration.Spec.Settings.MaxCommitCount = 2000
	}
	if migration.Spec.Settings.BatchSize == 0 {
		migration.Spec.Settings.BatchSize = 10
	}
	if migration.Spec.Settings.RetryAttempts == 0 {
		migration.Spec.Settings.RetryAttempts = 3
	}
	if migration.Spec.Settings.ParallelWorkers == 0 {
		migration.Spec.Settings.ParallelWorkers = 5
	}

	if err := s.client.Create(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to create migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, migration)
}

// createMonoRepoMigrationFromRaw handles creation of MonoRepoMigration CRDs via the unified create endpoint
func (s *Server) createMonoRepoMigrationFromRaw(ctx context.Context, w http.ResponseWriter, rawData map[string]interface{}) {
	jsonBytes, err := json.Marshal(rawData)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Failed to process request: "+err.Error())
		return
	}

	var migration migrationv1.MonoRepoMigration
	if err := json.Unmarshal(jsonBytes, &migration); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid MonoRepoMigration body: "+err.Error())
		return
	}

	if migration.Namespace == "" {
		migration.Namespace = "default"
	}

	// Handle inline GitHub token
	if migration.Spec.Target.Auth.Token != "" {
		if err := s.createGitHubTokenSecret(ctx, &migration); err != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to create GitHub token secret: "+err.Error())
			return
		}
	}

	// Set defaults
	if migration.Spec.Target.DefaultBranch == "" {
		migration.Spec.Target.DefaultBranch = "main"
	}
	if migration.Spec.Target.Visibility == "" {
		migration.Spec.Target.Visibility = "private"
	}

	if err := s.client.Create(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to create MonoRepoMigration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, migration)
}

func (s *Server) handleGetMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	s.writeJSON(w, http.StatusOK, migration)
}

func (s *Server) handleUpdateMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	var updateData migrationv1.AdoToGitMigration
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Update only the spec
	migration.Spec = updateData.Spec

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to update migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, migration)
}

func (s *Server) handleDeleteMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	if err := s.client.Delete(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to delete migration: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetMigrationStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	key := client.ObjectKey{Name: id, Namespace: namespace}

	// First try AdoToGitMigration
	var adoMigration migrationv1.AdoToGitMigration
	if err := s.client.Get(ctx, key, &adoMigration); err == nil {
		s.writeJSON(w, http.StatusOK, adoMigration.Status)
		return
	}

	// Try WorkItemMigration
	var workItemMigration migrationv1.WorkItemMigration
	if err := s.client.Get(ctx, key, &workItemMigration); err == nil {
		// Convert WorkItemMigration status to a compatible format
		status := map[string]interface{}{
			"phase":          workItemMigration.Status.Phase,
			"conditions":     workItemMigration.Status.Conditions,
			"startTime":      workItemMigration.Status.StartTime,
			"completionTime": workItemMigration.Status.CompletionTime,
			"progress":       workItemMigration.Status.Progress,
			"errorMessage":   workItemMigration.Status.ErrorMessage,
			"warnings":       workItemMigration.Status.Warnings,
			"statistics":     workItemMigration.Status.Statistics,
			"migratedItems":  workItemMigration.Status.MigratedItems,
		}
		s.writeJSON(w, http.StatusOK, status)
		return
	}

	// Try MonoRepoMigration
	var monoRepoMigration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, key, &monoRepoMigration); err == nil {
		s.writeJSON(w, http.StatusOK, monoRepoMigration.Status)
		return
	}

	s.writeError(w, http.StatusNotFound, "Migration not found")
}

func (s *Server) handleGetMigrationProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	response := map[string]interface{}{
		"progress":         migration.Status.Progress,
		"resourceStatuses": migration.Status.ResourceStatuses,
		"phase":            migration.Status.Phase,
		"statistics":       migration.Status.Statistics,
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleGetMigrationLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	key := client.ObjectKey{Name: id, Namespace: namespace}

	// First try AdoToGitMigration
	var adoMigration migrationv1.AdoToGitMigration
	if err := s.client.Get(ctx, key, &adoMigration); err == nil {
		s.buildAndSendAdoMigrationLogs(w, id, &adoMigration)
		return
	}

	// Try WorkItemMigration
	var workItemMigration migrationv1.WorkItemMigration
	if err := s.client.Get(ctx, key, &workItemMigration); err == nil {
		s.buildAndSendWorkItemMigrationLogs(w, id, &workItemMigration)
		return
	}

	s.writeError(w, http.StatusNotFound, "Migration not found")
}

// buildAndSendWorkItemMigrationLogs builds log entries from WorkItemMigration status
func (s *Server) buildAndSendWorkItemMigrationLogs(w http.ResponseWriter, id string, migration *migrationv1.WorkItemMigration) {
	logs := []map[string]interface{}{}

	// Add start time log entry if available
	if migration.Status.StartTime != nil {
		logs = append(logs, map[string]interface{}{
			"timestamp": migration.Status.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     "INFO",
			"message":   "Work item migration started",
			"resource":  id,
		})
	}

	// Add condition-based log entries
	for _, condition := range migration.Status.Conditions {
		level := "INFO"
		if condition.Status == "False" {
			level = "WARNING"
		}
		if condition.Reason == "Failed" || condition.Reason == "Error" {
			level = "ERROR"
		}

		logs = append(logs, map[string]interface{}{
			"timestamp": condition.LastTransitionTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     level,
			"message":   fmt.Sprintf("[%s] %s: %s", condition.Type, condition.Reason, condition.Message),
			"resource":  id,
		})
	}

	// Add progress updates
	if migration.Status.Progress.ItemsDiscovered > 0 {
		timestamp := migration.Status.LastReconcileTime.Format("2006-01-02T15:04:05Z07:00")

		// Add discovery log
		if migration.Status.Progress.TotalBatches > 0 {
			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp,
				"level":     "INFO",
				"message":   fmt.Sprintf("Discovered %d work items to migrate in %d batches", migration.Status.Progress.ItemsDiscovered, migration.Status.Progress.TotalBatches),
				"resource":  id,
			})
		}

		// Add current batch/step log
		currentStep := migration.Status.Progress.CurrentStep
		if currentStep != "" {
			stepMsg := currentStep
			if migration.Status.Progress.CurrentBatch > 0 && migration.Status.Progress.TotalBatches > 0 {
				stepMsg = fmt.Sprintf("Processing batch %d of %d - %s", migration.Status.Progress.CurrentBatch, migration.Status.Progress.TotalBatches, currentStep)
			}
			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp,
				"level":     "INFO",
				"message":   stepMsg,
				"resource":  id,
			})
		}

		// Add progress summary
		logs = append(logs, map[string]interface{}{
			"timestamp": timestamp,
			"level":     "INFO",
			"message":   fmt.Sprintf("Progress: %d/%d items migrated (%d%%), %d failed, %d skipped", migration.Status.Progress.ItemsMigrated, migration.Status.Progress.ItemsDiscovered, migration.Status.Progress.Percentage, migration.Status.Progress.ItemsFailed, migration.Status.Progress.ItemsSkipped),
			"resource":  id,
		})
	}

	// Add error message if present
	if migration.Status.ErrorMessage != "" {
		timestamp := migration.Status.LastReconcileTime
		if timestamp == nil && migration.Status.CompletionTime != nil {
			timestamp = migration.Status.CompletionTime
		}
		if timestamp != nil {
			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"level":     "ERROR",
				"message":   migration.Status.ErrorMessage,
				"resource":  id,
			})
		}
	}

	// Add warnings
	for _, warning := range migration.Status.Warnings {
		logs = append(logs, map[string]interface{}{
			"timestamp": migration.Status.LastReconcileTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     "WARNING",
			"message":   warning,
			"resource":  id,
		})
	}

	// Add completion time log entry if available
	if migration.Status.CompletionTime != nil {
		phase := string(migration.Status.Phase)
		level := "INFO"
		message := fmt.Sprintf("Work item migration completed. %d items migrated successfully.",
			migration.Status.Progress.ItemsMigrated)
		if phase == "Failed" {
			level = "ERROR"
			message = "Work item migration failed"
		}
		logs = append(logs, map[string]interface{}{
			"timestamp": migration.Status.CompletionTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     level,
			"message":   message,
			"resource":  id,
		})
	}

	// Sort logs by timestamp
	sort.Slice(logs, func(i, j int) bool {
		ti, _ := logs[i]["timestamp"].(string)
		tj, _ := logs[j]["timestamp"].(string)
		return ti < tj
	})

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

// buildAndSendAdoMigrationLogs builds log entries from AdoToGitMigration status
func (s *Server) buildAndSendAdoMigrationLogs(w http.ResponseWriter, id string, migration *migrationv1.AdoToGitMigration) {
	// Build logs from migration status conditions and events
	logs := []map[string]interface{}{}

	// Add start time log entry if available
	if migration.Status.StartTime != nil {
		logs = append(logs, map[string]interface{}{
			"timestamp": migration.Status.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     "INFO",
			"message":   "Migration started",
			"resource":  id,
		})
	}

	// Add condition-based log entries
	for _, condition := range migration.Status.Conditions {
		level := "INFO"
		if condition.Status == "False" {
			level = "WARNING"
		}
		if condition.Reason == "Failed" || condition.Reason == "Error" {
			level = "ERROR"
		}

		logs = append(logs, map[string]interface{}{
			"timestamp": condition.LastTransitionTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     level,
			"message":   fmt.Sprintf("[%s] %s: %s", condition.Type, condition.Reason, condition.Message),
			"resource":  id,
		})
	}

	// Add resource status updates as log entries
	for _, rs := range migration.Status.ResourceStatuses {
		// Use StartTime or CompletionTime as the timestamp
		var timestamp *metav1.Time
		if rs.CompletionTime != nil {
			timestamp = rs.CompletionTime
		} else if rs.StartTime != nil {
			timestamp = rs.StartTime
		}

		if timestamp != nil {
			level := "INFO"
			if rs.Status == "Failed" {
				level = "ERROR"
			} else if rs.Status == "InProgress" {
				level = "INFO"
			}

			message := fmt.Sprintf("Resource %s (%s): %s", rs.SourceName, rs.Type, rs.Status)
			if rs.ErrorMessage != "" {
				message = fmt.Sprintf("Resource %s (%s): %s - %s", rs.SourceName, rs.Type, rs.Status, rs.ErrorMessage)
			}

			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"level":     level,
				"message":   message,
				"resource":  id,
			})
		}
	}

	// Add error message if present
	if migration.Status.ErrorMessage != "" {
		timestamp := migration.Status.LastReconcileTime
		if timestamp == nil && migration.Status.CompletionTime != nil {
			timestamp = migration.Status.CompletionTime
		}
		if timestamp != nil {
			logs = append(logs, map[string]interface{}{
				"timestamp": timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"level":     "ERROR",
				"message":   migration.Status.ErrorMessage,
				"resource":  id,
			})
		}
	}

	// Add completion time log entry if available
	if migration.Status.CompletionTime != nil {
		phase := string(migration.Status.Phase)
		level := "INFO"
		message := "Migration completed successfully"
		if phase == "Failed" {
			level = "ERROR"
			message = "Migration failed"
		}
		logs = append(logs, map[string]interface{}{
			"timestamp": migration.Status.CompletionTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     level,
			"message":   message,
			"resource":  id,
		})
	}

	// Sort logs by timestamp (most recent last)
	sort.Slice(logs, func(i, j int) bool {
		ti, _ := logs[i]["timestamp"].(string)
		tj, _ := logs[j]["timestamp"].(string)
		return ti < tj
	})

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

func (s *Server) handlePauseMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	if migration.Status.Phase != migrationv1.MigrationPhaseRunning {
		s.writeError(w, http.StatusBadRequest, "Migration is not in running state")
		return
	}

	// Add pause annotation
	if migration.Annotations == nil {
		migration.Annotations = make(map[string]string)
	}
	migration.Annotations["migration.ado-to-git-migration.io/pause"] = "true"

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to pause migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	if migration.Status.Phase != migrationv1.MigrationPhasePaused {
		s.writeError(w, http.StatusBadRequest, "Migration is not in paused state")
		return
	}

	// Remove pause annotation
	if migration.Annotations != nil {
		delete(migration.Annotations, "migration.ado-to-git-migration.io/pause")
	}

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to resume migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *Server) handleCancelMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	if migration.Status.Phase == migrationv1.MigrationPhaseCompleted ||
		migration.Status.Phase == migrationv1.MigrationPhaseFailed ||
		migration.Status.Phase == migrationv1.MigrationPhaseCancelled {
		s.writeError(w, http.StatusBadRequest, "Migration is already in terminal state")
		return
	}

	// Add cancel annotation
	if migration.Annotations == nil {
		migration.Annotations = make(map[string]string)
	}
	migration.Annotations["migration.ado-to-git-migration.io/cancel"] = "true"

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to cancel migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleRetryMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	if migration.Status.Phase != migrationv1.MigrationPhaseFailed {
		s.writeError(w, http.StatusBadRequest, "Migration is not in failed state")
		return
	}

	// Add retry annotation
	if migration.Annotations == nil {
		migration.Annotations = make(map[string]string)
	}
	migration.Annotations["migration.ado-to-git-migration.io/retry"] = "true"

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to retry migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

func (s *Server) handleValidateMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.AdoToGitMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Migration not found")
		return
	}

	// Perform validation using the migration service
	validationResult, err := s.migrationService.ValidateMigration(ctx, &migration)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to validate migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, validationResult)
}
