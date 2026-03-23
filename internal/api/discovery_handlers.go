package api

import (
	"encoding/json"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// Discovery API handlers

func (s *Server) handleDiscoverOrganizations(w http.ResponseWriter, r *http.Request) {
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")

	if clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required authentication parameters")
		return
	}

	ctx := r.Context()
	organizations, err := s.azureDevOpsService.DiscoverOrganizations(ctx, clientID, clientSecret, tenantID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover organizations: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"organizations": organizations,
		"count":         len(organizations),
	})
}

func (s *Server) handleDiscoverProjects(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")

	if organization == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	projects, err := s.azureDevOpsService.DiscoverProjects(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover projects: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects":     projects,
		"count":        len(projects),
		"organization": organization,
	})
}

func (s *Server) handleDiscoverRepositories(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")
	includeEmpty := s.getQueryParamBool(r, "include_empty", false)
	includeDisabled := s.getQueryParamBool(r, "include_disabled", false)

	if organization == "" || project == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	repositories, err := s.azureDevOpsService.DiscoverRepositories(ctx, organization, project, clientID, clientSecret, tenantID, includeEmpty, includeDisabled)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover repositories: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"repositories": repositories,
		"count":        len(repositories),
		"organization": organization,
		"project":      project,
	})
}

func (s *Server) handleDiscoverWorkItems(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")
	workItemTypes := s.getQueryParam(r, "types")
	states := s.getQueryParam(r, "states")
	limit := s.getQueryParamInt(r, "limit", 1000)

	if organization == "" || project == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	workItems, err := s.azureDevOpsService.DiscoverWorkItems(ctx, organization, project, clientID, clientSecret, tenantID, workItemTypes, states, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover work items: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"workItems":    workItems,
		"count":        len(workItems),
		"organization": organization,
		"project":      project,
	})
}

func (s *Server) handleDiscoverPipelines(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")
	pipelineType := s.getQueryParam(r, "type") // build, release, all

	if organization == "" || project == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	pipelines, err := s.azureDevOpsService.DiscoverPipelines(ctx, organization, project, clientID, clientSecret, tenantID, pipelineType)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover pipelines: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"pipelines":    pipelines,
		"count":        len(pipelines),
		"organization": organization,
		"project":      project,
		"type":         pipelineType,
	})
}

func (s *Server) handleDiscoverBuilds(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")
	limit := s.getQueryParamInt(r, "limit", 100)

	if organization == "" || project == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	builds, err := s.azureDevOpsService.DiscoverBuilds(ctx, organization, project, clientID, clientSecret, tenantID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover builds: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"builds":       builds,
		"count":        len(builds),
		"organization": organization,
		"project":      project,
	})
}

func (s *Server) handleDiscoverReleases(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")
	limit := s.getQueryParamInt(r, "limit", 100)

	if organization == "" || project == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	releases, err := s.azureDevOpsService.DiscoverReleases(ctx, organization, project, clientID, clientSecret, tenantID, limit)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover releases: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"releases":     releases,
		"count":        len(releases),
		"organization": organization,
		"project":      project,
	})
}

func (s *Server) handleDiscoverTeams(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	project := s.getQueryParam(r, "project")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")

	if organization == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	teams, err := s.azureDevOpsService.DiscoverTeams(ctx, organization, project, clientID, clientSecret, tenantID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover teams: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"teams":        teams,
		"count":        len(teams),
		"organization": organization,
		"project":      project,
	})
}

func (s *Server) handleDiscoverUsers(w http.ResponseWriter, r *http.Request) {
	organization := s.getQueryParam(r, "organization")
	clientID := s.getQueryParam(r, "client_id")
	clientSecret := s.getQueryParam(r, "client_secret")
	tenantID := s.getQueryParam(r, "tenant_id")

	if organization == "" || clientID == "" || clientSecret == "" || tenantID == "" {
		s.writeError(w, http.StatusBadRequest, "Missing required parameters")
		return
	}

	ctx := r.Context()
	users, err := s.azureDevOpsService.DiscoverUsers(ctx, organization, clientID, clientSecret, tenantID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to discover users: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"users":        users,
		"count":        len(users),
		"organization": organization,
	})
}

// Discovery resource management handlers

func (s *Server) handleListDiscoveries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var discoveryList migrationv1.AdoDiscoveryList
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}

	if err := s.client.List(ctx, &discoveryList, listOpts...); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to list discoveries: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"discoveries": discoveryList.Items,
		"count":       len(discoveryList.Items),
	})
}

func (s *Server) handleCreateDiscovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var discovery migrationv1.AdoDiscovery
	if err := json.NewDecoder(r.Body).Decode(&discovery); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if discovery.Namespace == "" {
		discovery.Namespace = "default"
	}

	if err := s.client.Create(ctx, &discovery); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to create discovery: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, discovery)
}

func (s *Server) handleGetDiscovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var discovery migrationv1.AdoDiscovery
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &discovery); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get discovery: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Discovery not found")
		return
	}

	s.writeJSON(w, http.StatusOK, discovery)
}

func (s *Server) handleUpdateDiscovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var discovery migrationv1.AdoDiscovery
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &discovery); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get discovery: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Discovery not found")
		return
	}

	var updateData migrationv1.AdoDiscovery
	if err := json.NewDecoder(r.Body).Decode(&updateData); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// Update only the spec
	discovery.Spec = updateData.Spec

	if err := s.client.Update(ctx, &discovery); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to update discovery: "+err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, discovery)
}

func (s *Server) handleDeleteDiscovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var discovery migrationv1.AdoDiscovery
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &discovery); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get discovery: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Discovery not found")
		return
	}

	if err := s.client.Delete(ctx, &discovery); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to delete discovery: "+err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetDiscoveryStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var discovery migrationv1.AdoDiscovery
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &discovery); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get discovery: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Discovery not found")
		return
	}

	s.writeJSON(w, http.StatusOK, discovery.Status)
}

func (s *Server) handleGetDiscoveryResults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := getPathParam(r, "id")
	namespace := s.getQueryParam(r, "namespace")
	if namespace == "" {
		namespace = "default"
	}

	var discovery migrationv1.AdoDiscovery
	key := client.ObjectKey{Name: id, Namespace: namespace}

	if err := s.client.Get(ctx, key, &discovery); err != nil {
		if client.IgnoreNotFound(err) != nil {
			s.writeError(w, http.StatusInternalServerError, "Failed to get discovery: "+err.Error())
			return
		}
		s.writeError(w, http.StatusNotFound, "Discovery not found")
		return
	}

	s.writeJSON(w, http.StatusOK, discovery.Status.DiscoveredResources)
}
