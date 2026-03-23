package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"

	"github.com/tesserix/reposhift/internal/services"
)

// Validation request/response structures

type AdoCredentialsValidationRequest struct {
	ClientID     string `json:"clientId" validate:"required,uuid"`
	ClientSecret string `json:"clientSecret" validate:"required,min=10"`
	TenantID     string `json:"tenantId" validate:"required,uuid"`
	Organization string `json:"organization" validate:"required,min=1"`
	Project      string `json:"project,omitempty"`
}

type GitHubCredentialsValidationRequest struct {
	Token      string `json:"token" validate:"required,min=10"`
	Owner      string `json:"owner" validate:"required,min=1"`
	Repository string `json:"repository,omitempty"`
}

type MigrationConfigValidationRequest struct {
	Source   interface{} `json:"source" validate:"required"`
	Target   interface{} `json:"target" validate:"required"`
	Settings interface{} `json:"settings" validate:"required"`
}

type PipelineConfigValidationRequest struct {
	Source    interface{}   `json:"source" validate:"required"`
	Target    interface{}   `json:"target" validate:"required"`
	Pipelines []interface{} `json:"pipelines" validate:"required,min=1"`
}

type ValidationResponse struct {
	Valid       bool                `json:"valid"`
	Errors      []ValidationError   `json:"errors,omitempty"`
	Warnings    []ValidationWarning `json:"warnings,omitempty"`
	Permissions []string            `json:"permissions,omitempty"`
	Scopes      []string            `json:"scopes,omitempty"`
	RateLimit   *RateLimitInfo      `json:"rateLimit,omitempty"`
	Timestamp   time.Time           `json:"timestamp"`
}

type ValidationError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	Value   string `json:"value,omitempty"`
}

type ValidationWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

type RateLimitInfo struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
}

// GitHubRepositoryValidationRequest for validating GitHub repositories
type GitHubRepositoryValidationRequest struct {
	Token         string `json:"token" validate:"required,min=10"`
	Owner         string `json:"owner" validate:"required,min=1"`
	Repository    string `json:"repository" validate:"required,min=1"`
	Type          string `json:"type" validate:"required,oneof=repository work-item pipeline"` // Type of migration
	AllowExisting bool   `json:"allowExisting"`                                                // Whether to allow existing repositories
}

// Validation handlers

func (s *Server) handleValidateAdoCredentials(w http.ResponseWriter, r *http.Request) {
	var req AdoCredentialsValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Validate request structure
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		s.writeValidationErrors(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response := ValidationResponse{
		Valid:     true,
		Timestamp: time.Now().UTC(),
	}

	// Test Azure DevOps connectivity and permissions
	permissions, err := s.azureDevOpsService.ValidateCredentials(ctx, req.ClientID, req.ClientSecret, req.TenantID, req.Organization)
	if err != nil {
		response.Valid = false
		response.Errors = append(response.Errors, ValidationError{
			Code:    "INVALID_CREDENTIALS",
			Message: "Failed to authenticate with Azure DevOps: " + err.Error(),
		})
	} else {
		response.Permissions = permissions
	}

	// Check required permissions
	requiredPermissions := []string{"vso.code_read", "vso.work_read", "vso.build_read"}
	for _, required := range requiredPermissions {
		found := false
		for _, permission := range permissions {
			if permission == required {
				found = true
				break
			}
		}
		if !found {
			response.Warnings = append(response.Warnings, ValidationWarning{
				Code:    "MISSING_PERMISSION",
				Message: "Missing recommended permission: " + required,
				Field:   "permissions",
			})
		}
	}

	// Test project access if specified
	if req.Project != "" {
		if err := s.azureDevOpsService.ValidateProjectAccess(ctx, req.Organization, req.Project, req.ClientID, req.ClientSecret, req.TenantID); err != nil {
			response.Errors = append(response.Errors, ValidationError{
				Code:    "PROJECT_ACCESS_DENIED",
				Message: "Cannot access project: " + err.Error(),
				Field:   "project",
				Value:   req.Project,
			})
			response.Valid = false
		}
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleValidateGitHubCredentials(w http.ResponseWriter, r *http.Request) {
	var req GitHubCredentialsValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Validate request structure
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		s.writeValidationErrors(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response := ValidationResponse{
		Valid:     true,
		Timestamp: time.Now().UTC(),
	}

	// Test GitHub connectivity and permissions
	scopes, rateLimit, err := s.githubService.ValidateCredentials(ctx, req.Token, req.Owner)
	if err != nil {
		response.Valid = false
		response.Errors = append(response.Errors, ValidationError{
			Code:    "INVALID_TOKEN",
			Message: "Failed to authenticate with GitHub: " + err.Error(),
		})
	} else {
		response.Scopes = scopes
		response.RateLimit = &RateLimitInfo{
			Limit:     rateLimit.Limit,
			Remaining: rateLimit.Remaining,
			Reset:     rateLimit.Reset,
		}
	}

	// Check required scopes
	requiredScopes := []string{"repo", "admin:org"}
	for _, required := range requiredScopes {
		found := false
		for _, scope := range scopes {
			if scope == required {
				found = true
				break
			}
		}
		if !found {
			response.Warnings = append(response.Warnings, ValidationWarning{
				Code:    "MISSING_SCOPE",
				Message: "Missing recommended scope: " + required,
				Field:   "scopes",
			})
		}
	}

	// Test repository access if specified
	if req.Repository != "" {
		if err := s.githubService.ValidateRepositoryAccess(ctx, req.Token, req.Owner, req.Repository); err != nil {
			response.Errors = append(response.Errors, ValidationError{
				Code:    "REPOSITORY_ACCESS_DENIED",
				Message: "Cannot access repository: " + err.Error(),
				Field:   "repository",
				Value:   req.Repository,
			})
			response.Valid = false
		}
	}

	// Check rate limit
	if rateLimit.Remaining < 100 {
		response.Warnings = append(response.Warnings, ValidationWarning{
			Code:    "LOW_RATE_LIMIT",
			Message: "GitHub API rate limit is low. Consider using a different token or waiting.",
			Field:   "rateLimit",
		})
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleValidateGitHubRepository validates a GitHub repository for migration
func (s *Server) handleValidateGitHubRepository(w http.ResponseWriter, r *http.Request) {
	var req GitHubRepositoryValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Validate request structure
	validate := validator.New()
	if err := validate.Struct(req); err != nil {
		s.writeValidationErrors(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var result *services.GitHubValidationResult
	var err error

	// Validate based on migration type
	switch req.Type {
	case "repository":
		options := services.RepositoryValidationOptions{
			CheckExistence:      true,
			CheckPermissions:    true,
			CheckNamingRules:    true,
			CheckLimits:         true,
			AllowExisting:       req.AllowExisting,
			RequiredPermissions: []string{"contents:write"},
		}
		result, err = s.githubService.ValidateRepositoryForMigration(ctx, req.Token, req.Owner, req.Repository, options)

	case "work-item":
		result, err = s.githubService.ValidateWorkItemMigration(ctx, req.Token, req.Owner, req.Repository)

	case "pipeline":
		result, err = s.githubService.ValidatePipelineMigration(ctx, req.Token, req.Owner, req.Repository)

	default:
		s.writeError(w, http.StatusBadRequest, "Invalid migration type: "+req.Type)
		return
	}

	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Validation failed: "+err.Error())
		return
	}

	// Convert to API response format
	response := ValidationResponse{
		Valid:     result.Valid,
		Timestamp: time.Now().UTC(),
		Scopes:    []string{}, // GitHubValidationResult doesn't have AvailableScopes
	}

	// Convert errors
	for _, validationError := range result.Errors {
		response.Errors = append(response.Errors, ValidationError{
			Code:    validationError.Code,
			Message: validationError.Message,
			Field:   validationError.Field,
		})
	}

	// Convert warnings
	for _, validationWarning := range result.Warnings {
		response.Warnings = append(response.Warnings, ValidationWarning{
			Code:    validationWarning.Code,
			Message: validationWarning.Message,
			Field:   "", // GitHubValidationWarning doesn't have Field
		})
	}

	// Add rate limit info
	if result.RateLimit != nil {
		response.RateLimit = &RateLimitInfo{
			Limit:     result.RateLimit.Limit,
			Remaining: result.RateLimit.Remaining,
			Reset:     result.RateLimit.Reset,
		}
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleValidateAdoPermissions(w http.ResponseWriter, r *http.Request) {
	var req AdoCredentialsValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response := ValidationResponse{
		Valid:     true,
		Timestamp: time.Now().UTC(),
	}

	// Test specific permissions
	permissions := []string{
		"vso.code_read",
		"vso.code_write",
		"vso.work_read",
		"vso.work_write",
		"vso.build_read",
		"vso.release_read",
		"vso.identity_read",
	}

	validPermissions := []string{}
	for _, permission := range permissions {
		if s.azureDevOpsService.HasPermission(ctx, req.ClientID, req.ClientSecret, req.TenantID, req.Organization, permission) {
			validPermissions = append(validPermissions, permission)
		}
	}

	response.Permissions = validPermissions

	// Check for minimum required permissions
	requiredPermissions := []string{"vso.code_read", "vso.work_read"}
	for _, required := range requiredPermissions {
		found := false
		for _, valid := range validPermissions {
			if valid == required {
				found = true
				break
			}
		}
		if !found {
			response.Valid = false
			response.Errors = append(response.Errors, ValidationError{
				Code:    "MISSING_REQUIRED_PERMISSION",
				Message: "Missing required permission: " + required,
				Field:   "permissions",
			})
		}
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleValidateGitHubPermissions(w http.ResponseWriter, r *http.Request) {
	var req GitHubCredentialsValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response := ValidationResponse{
		Valid:     true,
		Timestamp: time.Now().UTC(),
	}

	// Test specific GitHub permissions
	permissions, err := s.githubService.GetPermissions(ctx, req.Token, req.Owner, req.Repository)
	if err != nil {
		response.Valid = false
		response.Errors = append(response.Errors, ValidationError{
			Code:    "PERMISSION_CHECK_FAILED",
			Message: "Failed to check GitHub permissions: " + err.Error(),
		})
	} else {
		// Convert map[string]bool to []string
		var permissionList []string
		for perm, hasPermission := range permissions {
			if hasPermission {
				permissionList = append(permissionList, perm)
			}
		}
		response.Permissions = permissionList
	}

	// Check organization membership and permissions
	if req.Owner != "" {
		membership, err := s.githubService.GetOrganizationMembership(ctx, req.Token, req.Owner)
		if err != nil {
			response.Warnings = append(response.Warnings, ValidationWarning{
				Code:    "ORG_MEMBERSHIP_CHECK_FAILED",
				Message: "Could not verify organization membership: " + err.Error(),
				Field:   "owner",
			})
		} else {
			role, hasRole := membership["role"]
			if !hasRole || (role != "admin" && role != "member") {
				response.Warnings = append(response.Warnings, ValidationWarning{
					Code:    "LIMITED_ORG_ACCESS",
					Message: "Limited access to organization. Some operations may fail.",
					Field:   "owner",
				})
			}
		}
	}

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleValidateMigrationConfig(w http.ResponseWriter, r *http.Request) {
	var req MigrationConfigValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	response := ValidationResponse{
		Valid:     true,
		Timestamp: time.Now().UTC(),
	}

	// Validate migration configuration structure
	if err := s.validateMigrationConfigStructure(req); err != nil {
		response.Valid = false
		response.Errors = append(response.Errors, ValidationError{
			Code:    "INVALID_CONFIG_STRUCTURE",
			Message: err.Error(),
		})
	}

	// Add configuration-specific warnings
	response.Warnings = append(response.Warnings, s.getMigrationConfigWarnings(req)...)

	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleValidatePipelineConfig(w http.ResponseWriter, r *http.Request) {
	var req PipelineConfigValidationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	response := ValidationResponse{
		Valid:     true,
		Timestamp: time.Now().UTC(),
	}

	// Validate pipeline configuration structure
	if err := s.validatePipelineConfigStructure(req); err != nil {
		response.Valid = false
		response.Errors = append(response.Errors, ValidationError{
			Code:    "INVALID_PIPELINE_CONFIG",
			Message: err.Error(),
		})
	}

	// Add pipeline-specific warnings
	response.Warnings = append(response.Warnings, s.getPipelineConfigWarnings(req)...)

	s.writeJSON(w, http.StatusOK, response)
}

// Helper functions

func (s *Server) writeValidationErrors(w http.ResponseWriter, err error) {
	var errors []ValidationError

	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		for _, fieldError := range validationErrors {
			errors = append(errors, ValidationError{
				Code:    "FIELD_VALIDATION_ERROR",
				Message: s.getValidationErrorMessage(fieldError),
				Field:   fieldError.Field(),
				Value:   fieldError.Value().(string),
			})
		}
	} else {
		errors = append(errors, ValidationError{
			Code:    "VALIDATION_ERROR",
			Message: err.Error(),
		})
	}

	response := ValidationResponse{
		Valid:     false,
		Errors:    errors,
		Timestamp: time.Now().UTC(),
	}

	s.writeJSON(w, http.StatusBadRequest, response)
}

func (s *Server) getValidationErrorMessage(fieldError validator.FieldError) string {
	switch fieldError.Tag() {
	case "required":
		return "Field is required"
	case "uuid":
		return "Field must be a valid UUID"
	case "min":
		return "Field must be at least " + fieldError.Param() + " characters long"
	case "max":
		return "Field must be at most " + fieldError.Param() + " characters long"
	case "email":
		return "Field must be a valid email address"
	case "url":
		return "Field must be a valid URL"
	case "oneof":
		return "Field must be one of: " + fieldError.Param()
	default:
		return "Field validation failed"
	}
}

func (s *Server) validateMigrationConfigStructure(req MigrationConfigValidationRequest) error {
	// Add specific migration configuration validation logic here
	return nil
}

func (s *Server) validatePipelineConfigStructure(req PipelineConfigValidationRequest) error {
	// Add specific pipeline configuration validation logic here
	return nil
}

func (s *Server) getMigrationConfigWarnings(req MigrationConfigValidationRequest) []ValidationWarning {
	var warnings []ValidationWarning

	// Add migration-specific warnings based on configuration

	return warnings
}

func (s *Server) getPipelineConfigWarnings(req PipelineConfigValidationRequest) []ValidationWarning {
	var warnings []ValidationWarning

	// Add pipeline-specific warnings based on configuration

	return warnings
}
