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

// MonoRepo Migration API handlers

func (s *Server) handleListMonoRepoMigrations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migrationList migrationv1.MonoRepoMigrationList
	if err := s.client.List(ctx, &migrationList, client.InNamespace(namespace)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to list monorepo migrations: "+err.Error())
		return
	}

	// Sort by creation timestamp (most recent first)
	sort.Slice(migrationList.Items, func(i, j int) bool {
		return migrationList.Items[i].CreationTimestamp.After(migrationList.Items[j].CreationTimestamp.Time)
	})

	// Apply filtering
	phase := s.getQueryParam(r, "phase")
	var filtered []migrationv1.MonoRepoMigration
	for _, m := range migrationList.Items {
		if phase != "" && string(m.Status.Phase) != phase {
			continue
		}
		filtered = append(filtered, m)
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"migrations": filtered,
		"count":      len(filtered),
	})
}

func (s *Server) handleCreateMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var migration migrationv1.MonoRepoMigration
	if err := json.NewDecoder(r.Body).Decode(&migration); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if migration.Namespace == "" {
		migration.Namespace = "default"
	}

	// Handle inline GitHub token - create a temporary secret
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
		s.writeError(w, http.StatusInternalServerError, "Failed to create monorepo migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, migration)
}

// createGitHubTokenSecret creates a Kubernetes secret for the inline GitHub token
func (s *Server) createGitHubTokenSecret(ctx context.Context, migration *migrationv1.MonoRepoMigration) error {
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

	if err := s.client.Create(ctx, secret); err != nil {
		if errors.IsAlreadyExists(err) {
			existingSecret := &corev1.Secret{}
			if getErr := s.client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: migration.Namespace}, existingSecret); getErr == nil {
				existingSecret.StringData = secret.StringData
				if updateErr := s.client.Update(ctx, existingSecret); updateErr != nil {
					return updateErr
				}
			}
		} else {
			return err
		}
	}

	migration.Spec.Target.Auth.TokenRef = &migrationv1.SecretReference{
		Name: secretName,
		Key:  "token",
	}
	migration.Spec.Target.Auth.Token = ""
	return nil
}

func (s *Server) handleGetMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	s.writeJSON(w, http.StatusOK, migration)
}

func (s *Server) handleUpdateMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}
	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	var updateData migrationv1.MonoRepoMigration
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	migration.Spec = updateData.Spec
	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to update monorepo migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, migration)
}

func (s *Server) handleDeleteMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	key := client.ObjectKey{Name: id, Namespace: namespace}
	if err := s.client.Get(ctx, key, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	if err := s.client.Delete(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to delete monorepo migration: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetMonoRepoMigrationStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	s.writeJSON(w, http.StatusOK, migration.Status)
}

func (s *Server) handleGetMonoRepoMigrationProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"progress":     migration.Status.Progress,
		"repoStatuses": migration.Status.RepoStatuses,
		"phase":        migration.Status.Phase,
		"statistics":   migration.Status.Statistics,
		"monoRepoUrl":  migration.Status.MonoRepoURL,
	})
}

func (s *Server) handlePauseMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	if migration.Annotations == nil {
		migration.Annotations = make(map[string]string)
	}
	migration.Annotations["migration.ado-to-git-migration.io/pause"] = "true"

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to pause monorepo migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *Server) handleResumeMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	if migration.Annotations != nil {
		delete(migration.Annotations, "migration.ado-to-git-migration.io/pause")
	}

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to resume monorepo migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *Server) handleCancelMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	if migration.Annotations == nil {
		migration.Annotations = make(map[string]string)
	}
	migration.Annotations["migration.ado-to-git-migration.io/cancel"] = "true"

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to cancel monorepo migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) handleRetryMonoRepoMigration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	if migration.Status.Phase != migrationv1.MonoRepoMigrationPhaseFailed {
		s.writeError(w, http.StatusBadRequest, "MonoRepo migration is not in failed state")
		return
	}

	if migration.Annotations == nil {
		migration.Annotations = make(map[string]string)
	}
	migration.Annotations["migration.ado-to-git-migration.io/retry"] = "true"

	if err := s.client.Update(ctx, &migration); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to retry monorepo migration: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "retrying"})
}

// handleGetMonoRepoMigrationLogs builds log entries from MonoRepoMigration status
func (s *Server) handleGetMonoRepoMigrationLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var migration migrationv1.MonoRepoMigration
	if err := s.client.Get(ctx, client.ObjectKey{Name: id, Namespace: namespace}, &migration); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get monorepo migration: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "MonoRepo migration not found")
		return
	}

	logs := []map[string]any{}

	if migration.Status.StartTime != nil {
		logs = append(logs, map[string]any{
			"timestamp": migration.Status.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     "INFO",
			"message":   "MonoRepo migration started",
			"resource":  id,
		})
	}

	for _, condition := range migration.Status.Conditions {
		level := "INFO"
		if condition.Status == "False" {
			level = "WARNING"
		}
		if condition.Reason == "Failed" || condition.Reason == "Error" {
			level = "ERROR"
		}
		logs = append(logs, map[string]any{
			"timestamp": condition.LastTransitionTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     level,
			"message":   fmt.Sprintf("[%s] %s: %s", condition.Type, condition.Reason, condition.Message),
			"resource":  id,
		})
	}

	for _, rs := range migration.Status.RepoStatuses {
		var timestamp *metav1.Time
		if rs.CompletionTime != nil {
			timestamp = rs.CompletionTime
		} else if rs.StartTime != nil {
			timestamp = rs.StartTime
		}
		if timestamp != nil {
			level := "INFO"
			if rs.Phase == migrationv1.MonoRepoRepoPhaseFailed {
				level = "ERROR"
			}
			message := fmt.Sprintf("Repo %s: %s", rs.Name, rs.Phase)
			if rs.ErrorMessage != "" {
				message = fmt.Sprintf("Repo %s: %s - %s", rs.Name, rs.Phase, rs.ErrorMessage)
			}
			logs = append(logs, map[string]any{
				"timestamp": timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"level":     level,
				"message":   message,
				"resource":  id,
			})
		}
	}

	if migration.Status.ErrorMessage != "" {
		timestamp := migration.Status.LastReconcileTime
		if timestamp == nil && migration.Status.CompletionTime != nil {
			timestamp = migration.Status.CompletionTime
		}
		if timestamp != nil {
			logs = append(logs, map[string]any{
				"timestamp": timestamp.Format("2006-01-02T15:04:05Z07:00"),
				"level":     "ERROR",
				"message":   migration.Status.ErrorMessage,
				"resource":  id,
			})
		}
	}

	if migration.Status.CompletionTime != nil {
		level := "INFO"
		message := fmt.Sprintf("MonoRepo migration completed: %s", migration.Status.MonoRepoURL)
		if migration.Status.Phase == migrationv1.MonoRepoMigrationPhaseFailed {
			level = "ERROR"
			message = "MonoRepo migration failed"
		}
		logs = append(logs, map[string]any{
			"timestamp": migration.Status.CompletionTime.Format("2006-01-02T15:04:05Z07:00"),
			"level":     level,
			"message":   message,
			"resource":  id,
		})
	}

	sort.Slice(logs, func(i, j int) bool {
		ti, _ := logs[i]["timestamp"].(string)
		tj, _ := logs[j]["timestamp"].(string)
		return ti < tj
	})

	s.writeJSON(w, http.StatusOK, map[string]any{
		"logs":  logs,
		"count": len(logs),
	})
}
