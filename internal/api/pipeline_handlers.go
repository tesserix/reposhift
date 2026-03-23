package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// Pipeline conversion API handlers

func (s *Server) handleListPipelineConversions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineList migrationv1.PipelineToWorkflowList
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}

	// Add filtering options
	if phase := s.getQueryParam(r, "phase"); phase != "" {
		listOpts = append(listOpts, client.MatchingFields{"status.phase": phase})
	}

	if conversionType := s.getQueryParam(r, "type"); conversionType != "" {
		listOpts = append(listOpts, client.MatchingFields{"spec.type": conversionType})
	}

	if err := s.client.List(ctx, &pipelineList, listOpts...); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to list pipeline conversions: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"pipelineConversions": pipelineList.Items,
		"count":               len(pipelineList.Items),
	})
}

func (s *Server) handleCreatePipelineConversion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var pipelineConversion migrationv1.PipelineToWorkflow
	if err := json.NewDecoder(r.Body).Decode(&pipelineConversion); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if pipelineConversion.Namespace == "" {
		pipelineConversion.Namespace = "default"
	}

	// Set default values
	if pipelineConversion.Spec.Settings.ParallelJobs == 0 {
		pipelineConversion.Spec.Settings.ParallelJobs = 3
	}
	if pipelineConversion.Spec.Settings.RetryAttempts == 0 {
		pipelineConversion.Spec.Settings.RetryAttempts = 2
	}

	// Validate pipeline conversion configuration
	if err := s.validatePipelineConversion(&pipelineConversion); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid pipeline conversion configuration: "+err.Error())
		return
	}

	if err := s.client.Create(ctx, &pipelineConversion); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to create pipeline conversion: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, pipelineConversion)
}

func (s *Server) handleGetPipelineConversion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	s.writeJSON(w, http.StatusOK, pipelineConversion)
}

func (s *Server) handleUpdatePipelineConversion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	var updateData migrationv1.PipelineToWorkflow
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Update only the spec
	pipelineConversion.Spec = updateData.Spec

	// Validate updated configuration
	if err := s.validatePipelineConversion(&pipelineConversion); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid pipeline conversion configuration: "+err.Error())
		return
	}

	if err := s.client.Update(ctx, &pipelineConversion); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to update pipeline conversion: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, pipelineConversion)
}

func (s *Server) handleDeletePipelineConversion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	if err := s.client.Delete(ctx, &pipelineConversion); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to delete pipeline conversion: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetPipelineConversionStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	s.writeJSON(w, http.StatusOK, pipelineConversion.Status)
}

func (s *Server) handlePreviewPipelineConversion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	// Generate preview of converted workflows
	preview, err := s.pipelineService.PreviewConversion(ctx, &pipelineConversion)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to generate preview: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, preview)
}

func (s *Server) handleValidatePipelineConversion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	// Validate pipeline conversion configuration
	validationResult, err := s.pipelineService.ValidateConversion(ctx, &pipelineConversion)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to validate pipeline conversion: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, validationResult)
}

func (s *Server) handleDownloadConvertedWorkflows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var pipelineConversion migrationv1.PipelineToWorkflow
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &pipelineConversion); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get pipeline conversion: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Pipeline conversion not found")
		return
	}

	// Check if conversion is completed
	if pipelineConversion.Status.Phase != migrationv1.ConversionPhaseCompleted {
		s.writeError(w, http.StatusBadRequest, "Pipeline conversion is not completed yet")
		return
	}

	// Generate ZIP file with converted workflows
	zipData, err := s.pipelineService.GenerateWorkflowsZip(ctx, &pipelineConversion)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to generate workflows ZIP: "+err.Error())
		return
	}

	// Set headers for file download
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+id+"-workflows.zip\"")
	w.Header().Set("Content-Length", strconv.Itoa(len(zipData)))

	// Write ZIP data
	w.Write(zipData)
}

// Pipeline analysis endpoints

func (s *Server) handleAnalyzePipeline(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	pipelineID := s.getQueryParam(r, "pipeline_id")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")

	if organization == "" || project == "" || pipelineID == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	analysis, err := s.pipelineService.AnalyzePipeline(ctx, organization, project, pipelineID, clientID, clientSecret, tenantID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to analyze pipeline: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, analysis)
}

func (s *Server) handleGetConversionTemplates(w http.ResponseWriter, r *http.Request) {
	templateType := s.getQueryParam(r, "type")

	templates, err := s.pipelineService.GetConversionTemplates(templateType)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get conversion templates: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"templates": templates,
		"count":     len(templates),
		"type":      templateType,
	})
}

func (s *Server) handleGetTaskMappings(w http.ResponseWriter, r *http.Request) {
	taskType := s.getQueryParam(r, "task_type")

	mappings, err := s.pipelineService.GetTaskMappings(taskType)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to get task mappings: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"mappings": mappings,
		"count":    len(mappings),
		"taskType": taskType,
	})
}

// Helper functions

func (s *Server) validatePipelineConversion(pipelineConversion *migrationv1.PipelineToWorkflow) error {
	// Validate source configuration
	if pipelineConversion.Spec.Source.Organization == "" {
		return fmt.Errorf("source organization is required")
	}
	if pipelineConversion.Spec.Source.Project == "" {
		return fmt.Errorf("source project is required")
	}

	// Validate target configuration
	if pipelineConversion.Spec.Target.Owner == "" {
		return fmt.Errorf("target owner is required")
	}
	if pipelineConversion.Spec.Target.Repository == "" {
		return fmt.Errorf("target repository is required")
	}

	// Validate pipelines
	if len(pipelineConversion.Spec.Pipelines) == 0 {
		return fmt.Errorf("at least one pipeline is required")
	}

	for i, pipeline := range pipelineConversion.Spec.Pipelines {
		if pipeline.ID == "" {
			return fmt.Errorf("pipeline[%d]: ID is required", i)
		}
		if pipeline.Name == "" {
			return fmt.Errorf("pipeline[%d]: name is required", i)
		}
		if pipeline.TargetWorkflowName == "" {
			return fmt.Errorf("pipeline[%d]: target workflow name is required", i)
		}
		if pipeline.Type != "build" && pipeline.Type != "release" {
			return fmt.Errorf("pipeline[%d]: type must be 'build' or 'release'", i)
		}
	}

	// Validate settings
	if pipelineConversion.Spec.Settings.ParallelJobs < 1 || pipelineConversion.Spec.Settings.ParallelJobs > 20 {
		return fmt.Errorf("parallel jobs must be between 1 and 20")
	}
	if pipelineConversion.Spec.Settings.RetryAttempts < 0 || pipelineConversion.Spec.Settings.RetryAttempts > 5 {
		return fmt.Errorf("retry attempts must be between 0 and 5")
	}

	return nil
}
