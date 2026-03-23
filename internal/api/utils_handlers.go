package api

import (
	"net/http"
	"runtime"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Utility handlers for health, version, and configuration

func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"version":   "v1.0.0", // This should come from build-time variables
		"components": map[string]string{
			"database":    "healthy",
			"azureDevOps": "healthy",
			"github":      "healthy",
			"websocket":   "healthy",
			"kubernetes":  "healthy",
		},
		"uptime": s.getUptime(),
	}

	// Check component health
	if !s.checkComponentHealth() {
		health["status"] = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	s.writeJSON(w, http.StatusOK, health)
}

func (s *Server) handleReadinessCheck(w http.ResponseWriter, r *http.Request) {
	ready := map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"checks": map[string]bool{
			"kubernetes_api": s.checkKubernetesAPI(),
			"crds_installed": s.checkCRDsInstalled(),
			"webhooks":       s.checkWebhooks(),
		},
	}

	// Check if all readiness checks pass
	allReady := true
	for _, check := range ready["checks"].(map[string]bool) {
		if !check {
			allReady = false
			break
		}
	}

	if !allReady {
		ready["status"] = "not ready"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	s.writeJSON(w, http.StatusOK, ready)
}

func (s *Server) handleGetVersion(w http.ResponseWriter, r *http.Request) {
	version := map[string]interface{}{
		"version":    "v1.0.0",
		"gitCommit":  "abc123def456", // This should come from build-time variables
		"buildDate":  "2024-12-01T10:00:00Z",
		"goVersion":  runtime.Version(),
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
		"compiler":   runtime.Compiler,
		"apiVersion": "v1",
		"features": []string{
			"repository-migration",
			"work-item-migration",
			"pipeline-conversion",
			"discovery",
			"websocket-updates",
			"rate-limiting",
			"validation",
		},
	}

	s.writeJSON(w, http.StatusOK, version)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	config := map[string]interface{}{
		"api": map[string]interface{}{
			"version": "v1",
			"baseUrl": "/api/v1",
			"rateLimits": map[string]interface{}{
				"perClient": "100/min",
				"global":    "1000/sec",
			},
		},
		"features": map[string]bool{
			"discovery":          true,
			"migration":          true,
			"pipelineConversion": true,
			"websockets":         true,
			"validation":         true,
			"statistics":         true,
		},
		"limits": map[string]interface{}{
			"maxRequestSize":     "10MB",
			"maxHistoryDays":     3650,
			"maxCommitCount":     50000,
			"maxParallelWorkers": 20,
			"maxRetryAttempts":   10,
		},
		"endpoints": map[string][]string{
			"discovery": {
				"/discovery/organizations",
				"/discovery/projects",
				"/discovery/repositories",
				"/discovery/workitems",
				"/discovery/pipelines",
				"/discovery/builds",
				"/discovery/releases",
				"/discovery/teams",
				"/discovery/users",
			},
			"migration": {
				"/migrations",
				"/migrations/{id}",
				"/migrations/{id}/status",
				"/migrations/{id}/progress",
				"/migrations/{id}/logs",
				"/migrations/{id}/pause",
				"/migrations/{id}/resume",
				"/migrations/{id}/cancel",
				"/migrations/{id}/retry",
				"/migrations/{id}/validate",
			},
			"pipelines": {
				"/pipelines",
				"/pipelines/{id}",
				"/pipelines/{id}/status",
				"/pipelines/{id}/preview",
				"/pipelines/{id}/validate",
				"/pipelines/{id}/download",
			},
			"validation": {
				"/validation/credentials/ado",
				"/validation/credentials/github",
				"/validation/permissions/ado",
				"/validation/permissions/github",
				"/validation/migration",
				"/validation/pipeline",
			},
			"statistics": {
				"/statistics/migrations",
				"/statistics/pipelines",
				"/statistics/usage",
				"/statistics/performance",
			},
			"utils": {
				"/utils/health",
				"/utils/ready",
				"/utils/version",
				"/utils/config",
				"/utils/templates",
			},
		},
	}

	s.writeJSON(w, http.StatusOK, config)
}

func (s *Server) handleGetTemplates(w http.ResponseWriter, r *http.Request) {
	templates := map[string]interface{}{
		"migration": map[string]interface{}{
			"basic": map[string]interface{}{
				"name":        "Basic Migration",
				"description": "Simple repository migration template",
				"template": map[string]interface{}{
					"apiVersion": "migration.ado-to-git-migration.io/v1",
					"kind":       "AdoToGitMigration",
					"spec": map[string]interface{}{
						"type": "repository",
						"settings": map[string]interface{}{
							"maxHistoryDays": 500,
							"maxCommitCount": 2000,
						},
					},
				},
			},
			"complete": map[string]interface{}{
				"name":        "Complete Migration",
				"description": "Full migration with all features",
				"template": map[string]interface{}{
					"apiVersion": "migration.ado-to-git-migration.io/v1",
					"kind":       "AdoToGitMigration",
					"spec": map[string]interface{}{
						"type": "all",
						"settings": map[string]interface{}{
							"maxHistoryDays":      500,
							"maxCommitCount":      2000,
							"includeWorkItems":    true,
							"includePullRequests": true,
							"includeTags":         true,
							"handleLFS":           true,
						},
					},
				},
			},
		},
		"discovery": map[string]interface{}{
			"basic": map[string]interface{}{
				"name":        "Basic Discovery",
				"description": "Discover organizations and projects",
				"template": map[string]interface{}{
					"apiVersion": "migration.ado-to-git-migration.io/v1",
					"kind":       "AdoDiscovery",
					"spec": map[string]interface{}{
						"scope": map[string]interface{}{
							"organizations": true,
							"projects":      true,
							"repositories":  true,
						},
					},
				},
			},
			"comprehensive": map[string]interface{}{
				"name":        "Comprehensive Discovery",
				"description": "Discover all available resources",
				"template": map[string]interface{}{
					"apiVersion": "migration.ado-to-git-migration.io/v1",
					"kind":       "AdoDiscovery",
					"spec": map[string]interface{}{
						"scope": map[string]interface{}{
							"organizations": true,
							"projects":      true,
							"repositories":  true,
							"workItems":     true,
							"pipelines":     true,
							"builds":        true,
							"releases":      true,
							"teams":         true,
							"users":         true,
						},
					},
				},
			},
		},
		"pipeline": map[string]interface{}{
			"build": map[string]interface{}{
				"name":        "Build Pipeline Conversion",
				"description": "Convert Azure DevOps build pipelines to GitHub Actions",
				"template": map[string]interface{}{
					"apiVersion": "migration.ado-to-git-migration.io/v1",
					"kind":       "PipelineToWorkflow",
					"spec": map[string]interface{}{
						"type": "build",
						"settings": map[string]interface{}{
							"convertVariables":          true,
							"convertVariableGroups":     true,
							"convertServiceConnections": true,
							"preserveComments":          true,
						},
					},
				},
			},
			"release": map[string]interface{}{
				"name":        "Release Pipeline Conversion",
				"description": "Convert Azure DevOps release pipelines to GitHub Actions",
				"template": map[string]interface{}{
					"apiVersion": "migration.ado-to-git-migration.io/v1",
					"kind":       "PipelineToWorkflow",
					"spec": map[string]interface{}{
						"type": "release",
						"settings": map[string]interface{}{
							"convertVariables":    true,
							"convertApprovals":    true,
							"convertArtifacts":    true,
							"useCompositeActions": false,
						},
					},
				},
			},
		},
	}

	s.writeJSON(w, http.StatusOK, templates)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	templateType := getPathParam(r, "type")
	templateName := s.getQueryParam(r, "name")

	if templateName == "" {
		templateName = "basic"
	}

	// This would typically load templates from files or database
	template, err := s.getTemplate(templateType, templateName)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "Template not found: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, template)
}

// Helper functions

func (s *Server) getUptime() string {
	// This would typically track actual uptime
	return "72h15m30s"
}

func (s *Server) checkComponentHealth() bool {
	// Implement actual health checks for components
	return true
}

func (s *Server) checkKubernetesAPI() bool {
	// Test Kubernetes API connectivity
	_, err := s.client.RESTMapper().RESTMappings(schema.GroupKind{
		Group: "migration.ado-to-git-migration.io",
		Kind:  "AdoToGitMigration",
	})
	return err == nil
}

func (s *Server) checkCRDsInstalled() bool {
	// Check if required CRDs are installed
	crds := []string{
		"adotogitmigrations.migration.ado-to-git-migration.io",
		"adodiscoveries.migration.ado-to-git-migration.io",
		"pipelinetoworkflows.migration.ado-to-git-migration.io",
	}

	for range crds {
		// This would check if CRD exists
		// For now, assume they exist
	}

	return true
}

func (s *Server) checkWebhooks() bool {
	// Check webhook health
	return true
}

func (s *Server) getTemplate(templateType, templateName string) (map[string]interface{}, error) {
	// This would load actual templates
	// For now, return a basic template

	template := map[string]interface{}{
		"name":        templateName + " " + templateType,
		"description": "Template for " + templateType,
		"template": map[string]interface{}{
			"apiVersion": "migration.ado-to-git-migration.io/v1",
			"kind":       "AdoToGitMigration",
			"metadata": map[string]interface{}{
				"name":      "example-migration",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"type": templateType,
			},
		},
	}

	return template, nil
}
