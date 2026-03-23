package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/git"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sanitizeLabelValue sanitizes a string for use as a Kubernetes label value.
// Kubernetes label values must:
// - Be 63 characters or less
// - Contain only alphanumeric characters, '-', '_', or '.'
// - Start and end with an alphanumeric character
func sanitizeLabelValue(value string) string {
	if value == "" {
		return ""
	}

	// Convert to lowercase and trim spaces
	sanitized := strings.ToLower(strings.TrimSpace(value))

	// Replace spaces and invalid characters with hyphens
	re := regexp.MustCompile(`[^a-z0-9._-]+`)
	sanitized = re.ReplaceAllString(sanitized, "-")

	// Remove leading/trailing non-alphanumeric characters
	sanitized = strings.Trim(sanitized, "-._")

	// Collapse multiple consecutive hyphens into one
	re = regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")

	// Truncate to 63 characters (Kubernetes label value limit)
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
		// Ensure we don't end with a non-alphanumeric character after truncation
		sanitized = strings.TrimRight(sanitized, "-._")
	}

	return sanitized
}

// ADO Repository Response
type ADORepositoriesResponse struct {
	Repositories []ADORepository `json:"repositories"`
	Count        int             `json:"count"`
}

type ADORepository struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Project string `json:"project"`
	URL     string `json:"url"`
}

// handleFetchADORepositories fetches repositories from Azure DevOps
func (s *Server) handleFetchADORepositories(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Organization    string `json:"organization"`
		Project         string `json:"project"`
		SecretName      string `json:"secretName,omitempty"`
		SecretNamespace string `json:"secretNamespace,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get PAT from header
	pat := r.Header.Get("X-ADO-PAT")
	if pat == "" {
		http.Error(w, "PAT token required in X-ADO-PAT header", http.StatusUnauthorized)
		return
	}

	// Fetch repositories from Azure DevOps
	repos, err := s.fetchADORepositoriesFromAPI(r.Context(), req.Organization, req.Project, pat)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Convert to response format
	response := ADORepositoriesResponse{
		Repositories: make([]ADORepository, len(repos)),
		Count:        len(repos),
	}

	for i, repo := range repos {
		response.Repositories[i] = ADORepository{
			ID:      repo.Id.String(),
			Name:    *repo.Name,
			Project: req.Project,
			URL:     *repo.WebUrl,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// fetchADORepositoriesFromAPI calls Azure DevOps API to get repositories
func (s *Server) fetchADORepositoriesFromAPI(ctx context.Context, organization, project, pat string) ([]git.GitRepository, error) {
	// Create Azure DevOps connection with PAT
	organizationURL := fmt.Sprintf("https://dev.azure.com/%s", organization)
	connection := azuredevops.NewPatConnection(organizationURL, pat)

	// Create git client
	gitClient, err := git.NewClient(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("failed to create git client: %w", err)
	}

	// Get repositories
	getReposArgs := git.GetRepositoriesArgs{
		Project: &project,
	}

	repositories, err := gitClient.GetRepositories(ctx, getReposArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to get repositories: %w", err)
	}

	if repositories == nil {
		return []git.GitRepository{}, nil
	}

	return *repositories, nil
}

// handleListADOSecrets lists ADO PAT secrets from Kubernetes
func (s *Server) handleListADOSecrets(w http.ResponseWriter, r *http.Request) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "ado-git-migration" // Default to operator namespace
	}

	ctx := r.Context()

	// List secrets with label selector for ADO migration secrets
	var secretList corev1.SecretList
	listOptions := &client.ListOptions{
		Namespace: namespace,
	}

	// Add label selector if provided
	labelSelector := r.URL.Query().Get("labelSelector")
	if labelSelector != "" {
		selector, err := labels.Parse(labelSelector)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, "Invalid label selector: "+err.Error())
			return
		}
		listOptions.LabelSelector = selector
	}

	if err := s.client.List(ctx, &secretList, listOptions); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to list secrets: "+err.Error())
		return
	}

	// Transform secrets to response format
	secrets := make([]map[string]interface{}, 0)
	for _, secret := range secretList.Items {
		// Filter only ADO migration related secrets
		if secret.Labels["type"] == "ado-pat" || secret.Labels["app"] == "ado-migration" {
			secretInfo := map[string]interface{}{
				"name":      secret.Name,
				"namespace": secret.Namespace,
				"labels":    secret.Labels,
				"created":   secret.CreationTimestamp.Time,
			}

			// Add business unit and product if available
			if bu, ok := secret.Labels["businessUnit"]; ok {
				secretInfo["businessUnit"] = bu
			}
			if pn, ok := secret.Labels["productName"]; ok {
				secretInfo["productName"] = pn
			}

			secrets = append(secrets, secretInfo)
		}
	}

	response := map[string]interface{}{
		"secrets": secrets,
		"count":   len(secrets),
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleCheckSecretByLabel checks if a secret exists by label
func (s *Server) handleCheckSecretByLabel(w http.ResponseWriter, r *http.Request) {
	businessUnit := r.URL.Query().Get("businessUnit")
	productName := r.URL.Query().Get("productName")
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "ado-git-migration" // Default to operator namespace
	}

	if businessUnit == "" || productName == "" {
		s.writeError(w, http.StatusBadRequest, "businessUnit and productName are required")
		return
	}

	ctx := r.Context()

	// List secrets with matching labels
	var secretList corev1.SecretList
	labelSelector, err := labels.Parse(fmt.Sprintf("businessUnit=%s,productName=%s,type=ado-pat", businessUnit, productName))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid label selector: "+err.Error())
		return
	}

	listOptions := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	}

	if err := s.client.List(ctx, &secretList, listOptions); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to list secrets: "+err.Error())
		return
	}

	response := map[string]interface{}{
		"exists":       len(secretList.Items) > 0,
		"name":         "",
		"namespace":    namespace,
		"businessUnit": businessUnit,
		"productName":  productName,
		"method":       "label",
	}

	if len(secretList.Items) > 0 {
		response["name"] = secretList.Items[0].Name
		response["created"] = secretList.Items[0].CreationTimestamp.Time
	}

	s.writeJSON(w, http.StatusOK, response)
}

// handleCreateADOSecret creates or updates an ADO PAT secret
func (s *Server) handleCreateADOSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		PAT          string `json:"pat"`
		BusinessUnit string `json:"businessUnit"`
		ProductName  string `json:"productName"`
		Namespace    string `json:"namespace"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" || req.PAT == "" || req.BusinessUnit == "" || req.ProductName == "" {
		s.writeError(w, http.StatusBadRequest, "name, pat, businessUnit, and productName are required")
		return
	}

	if req.Namespace == "" {
		req.Namespace = "ado-git-migration" // Default to operator namespace
	}

	ctx := r.Context()

	// Check if secret already exists
	existingSecret := &corev1.Secret{}
	secretKey := client.ObjectKey{Name: req.Name, Namespace: req.Namespace}
	err := s.client.Get(ctx, secretKey, existingSecret)

	if err != nil && !apierrors.IsNotFound(err) {
		s.writeError(w, http.StatusInternalServerError, "Failed to check existing secret: "+err.Error())
		return
	}

	action := "created"
	if apierrors.IsNotFound(err) {
		// Sanitize label values to comply with Kubernetes label requirements
		sanitizedBU := sanitizeLabelValue(req.BusinessUnit)
		sanitizedProduct := sanitizeLabelValue(req.ProductName)

		// Create new secret
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      req.Name,
				Namespace: req.Namespace,
				Labels: map[string]string{
					"type":         "ado-pat",
					"app":          "ado-migration",
					"businessUnit": sanitizedBU,
					"productName":  sanitizedProduct,
					"managed-by":   "ado-migration-mfe",
				},
			},
			Type: corev1.SecretTypeOpaque,
			StringData: map[string]string{
				"token": req.PAT,
			},
		}

		if err := s.client.Create(ctx, secret); err != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to create secret: "+err.Error())
			return
		}
	} else {
		// Update existing secret
		existingSecret.Data = map[string][]byte{
			"token": []byte(req.PAT),
		}
		// Update labels with sanitized values
		if existingSecret.Labels == nil {
			existingSecret.Labels = make(map[string]string)
		}
		existingSecret.Labels["businessUnit"] = sanitizeLabelValue(req.BusinessUnit)
		existingSecret.Labels["productName"] = sanitizeLabelValue(req.ProductName)
		existingSecret.Labels["managed-by"] = "ado-migration-mfe"

		if err := s.client.Update(ctx, existingSecret); err != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to update secret: "+err.Error())
			return
		}
		action = "updated"
	}

	response := map[string]interface{}{
		"success":      true,
		"name":         req.Name,
		"namespace":    req.Namespace,
		"businessUnit": req.BusinessUnit,
		"productName":  req.ProductName,
		"message":      fmt.Sprintf("Secret %s successfully", action),
		"action":       action,
	}

	s.writeJSON(w, http.StatusCreated, response)
}

// handleGetWorkItemMetadata fetches work item metadata
func (s *Server) handleGetWorkItemMetadata(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Organization    string `json:"organization"`
		Project         string `json:"project"`
		SecretName      string `json:"secretName"`
		SecretNamespace string `json:"secretNamespace"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// TODO: Implement actual work item metadata fetching from Azure DevOps API
	// For now, return common work item types and states
	response := map[string]interface{}{
		"metadata": map[string]interface{}{
			"types":  []string{"Epic", "Feature", "User Story", "Bug", "Task", "Issue"},
			"states": []string{"New", "Active", "Resolved", "Closed", "Removed"},
			"tags":   []string{},
		},
	}

	s.writeJSON(w, http.StatusOK, response)
}
