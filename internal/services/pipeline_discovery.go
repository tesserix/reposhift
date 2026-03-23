package services

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PipelineDiscoveryService handles auto-discovery of ADO pipelines
type PipelineDiscoveryService struct {
	adoService *AzureDevOpsService
	client     client.Client
}

// NewPipelineDiscoveryService creates a new pipeline discovery service
func NewPipelineDiscoveryService(adoService *AzureDevOpsService, k8sClient client.Client) *PipelineDiscoveryService {
	return &PipelineDiscoveryService{
		adoService: adoService,
		client:     k8sClient,
	}
}

// DiscoveryResult represents the result of pipeline discovery
type DiscoveryResult struct {
	BuildPipelines   []migrationv1.Pipeline `json:"buildPipelines"`
	ReleasePipelines []migrationv1.Pipeline `json:"releasePipelines"`
	TotalCount       int                    `json:"totalCount"`
	FilteredCount    int                    `json:"filteredCount"`
}

// DiscoverPipelines auto-discovers pipelines from ADO based on configuration
func (s *PipelineDiscoveryService) DiscoverPipelines(ctx context.Context, conversion *migrationv1.PipelineToWorkflow) (*DiscoveryResult, error) {
	if conversion.Spec.AutoDiscovery == nil || !conversion.Spec.AutoDiscovery.Enabled {
		return nil, fmt.Errorf("auto-discovery is not enabled")
	}

	result := &DiscoveryResult{
		BuildPipelines:   []migrationv1.Pipeline{},
		ReleasePipelines: []migrationv1.Pipeline{},
	}

	// Get ADO credentials
	adoOrg := conversion.Spec.Source.Organization
	adoProject := conversion.Spec.Source.Project

	// Read authentication credentials from Kubernetes secret
	var patToken string
	var clientID, clientSecret, tenantID string

	// Check auth type and read appropriate credentials
	if conversion.Spec.Source.Auth.PAT != nil && conversion.Spec.Source.Auth.PAT.TokenRef.Name != "" {
		// PAT authentication
		tokenRef := conversion.Spec.Source.Auth.PAT.TokenRef
		namespace := tokenRef.Namespace
		if namespace == "" {
			namespace = conversion.Namespace
		}

		secret := &corev1.Secret{}
		if err := s.client.Get(ctx, client.ObjectKey{
			Name:      tokenRef.Name,
			Namespace: namespace,
		}, secret); err != nil {
			return nil, fmt.Errorf("failed to get PAT secret %s/%s: %w", namespace, tokenRef.Name, err)
		}

		tokenBytes, ok := secret.Data[tokenRef.Key]
		if !ok {
			return nil, fmt.Errorf("secret %s/%s does not contain key %s", namespace, tokenRef.Name, tokenRef.Key)
		}
		patToken = string(tokenBytes)

	} else if conversion.Spec.Source.Auth.ServicePrincipal != nil {
		// Service Principal authentication
		sp := conversion.Spec.Source.Auth.ServicePrincipal
		namespace := conversion.Namespace

		// Read client ID
		if sp.ClientIDRef.Name != "" {
			secret := &corev1.Secret{}
			if err := s.client.Get(ctx, client.ObjectKey{
				Name:      sp.ClientIDRef.Name,
				Namespace: namespace,
			}, secret); err != nil {
				return nil, fmt.Errorf("failed to get client ID secret: %w", err)
			}
			clientID = string(secret.Data[sp.ClientIDRef.Key])
		}

		// Read client secret
		if sp.ClientSecretRef.Name != "" {
			secret := &corev1.Secret{}
			if err := s.client.Get(ctx, client.ObjectKey{
				Name:      sp.ClientSecretRef.Name,
				Namespace: namespace,
			}, secret); err != nil {
				return nil, fmt.Errorf("failed to get client secret: %w", err)
			}
			clientSecret = string(secret.Data[sp.ClientSecretRef.Key])
		}

		// Read tenant ID
		if sp.TenantIDRef.Name != "" {
			secret := &corev1.Secret{}
			if err := s.client.Get(ctx, client.ObjectKey{
				Name:      sp.TenantIDRef.Name,
				Namespace: namespace,
			}, secret); err != nil {
				return nil, fmt.Errorf("failed to get tenant ID secret: %w", err)
			}
			tenantID = string(secret.Data[sp.TenantIDRef.Key])
		}
	} else {
		return nil, fmt.Errorf("no authentication method configured (PAT or ServicePrincipal required)")
	}

	config := conversion.Spec.AutoDiscovery

	// Discover build pipelines
	if config.IncludeBuildPipelines {
		var pipelines []migrationv1.Pipeline
		var err error

		if patToken != "" {
			// Use PAT authentication
			pipelines, err = s.adoService.DiscoverPipelinesWithPAT(ctx, adoOrg, adoProject, patToken, "build")
		} else {
			// Use Service Principal authentication
			pipelines, err = s.adoService.DiscoverPipelines(ctx, adoOrg, adoProject, clientID, clientSecret, tenantID, "build")
		}

		if err != nil {
			return nil, fmt.Errorf("failed to discover build pipelines: %w", err)
		}

		filtered := s.filterPipelines(pipelines, config)
		result.BuildPipelines = filtered
		result.TotalCount += len(pipelines)
		result.FilteredCount += len(filtered)
	}

	// Discover release pipelines
	if config.IncludeReleasePipelines {
		var pipelines []migrationv1.Pipeline
		var err error

		if patToken != "" {
			// Use PAT authentication
			pipelines, err = s.adoService.DiscoverPipelinesWithPAT(ctx, adoOrg, adoProject, patToken, "release")
		} else {
			// Use Service Principal authentication
			pipelines, err = s.adoService.DiscoverPipelines(ctx, adoOrg, adoProject, clientID, clientSecret, tenantID, "release")
		}

		if err != nil {
			return nil, fmt.Errorf("failed to discover release pipelines: %w", err)
		}

		filtered := s.filterPipelines(pipelines, config)
		result.ReleasePipelines = filtered
		result.TotalCount += len(pipelines)
		result.FilteredCount += len(filtered)
	}

	// Apply max pipelines limit
	if config.MaxPipelines > 0 {
		result.BuildPipelines, result.ReleasePipelines = s.applyMaxLimit(
			result.BuildPipelines,
			result.ReleasePipelines,
			config.MaxPipelines,
		)
		if len(result.BuildPipelines)+len(result.ReleasePipelines) < result.FilteredCount {
			result.FilteredCount = len(result.BuildPipelines) + len(result.ReleasePipelines)
		}
	}

	return result, nil
}

// filterPipelines filters pipelines based on configuration
func (s *PipelineDiscoveryService) filterPipelines(pipelines []migrationv1.Pipeline, config *migrationv1.AutoDiscoveryConfig) []migrationv1.Pipeline {
	var filtered []migrationv1.Pipeline

	for _, pipeline := range pipelines {
		// Apply folder filter
		if config.FolderFilter != "" {
			if !s.matchesFolderFilter(pipeline.Folder, config.FolderFilter) {
				continue
			}
		}

		// Apply name filter
		if config.NameFilter != "" {
			matched, err := regexp.MatchString(config.NameFilter, pipeline.Name)
			if err != nil || !matched {
				continue
			}
		}

		filtered = append(filtered, pipeline)
	}

	return filtered
}

// matchesFolderFilter checks if a folder matches the filter pattern
func (s *PipelineDiscoveryService) matchesFolderFilter(folder, filter string) bool {
	// Simple wildcard matching
	if strings.HasSuffix(filter, "/*") {
		prefix := strings.TrimSuffix(filter, "/*")
		return strings.HasPrefix(folder, prefix)
	}
	return folder == filter
}

// applyMaxLimit applies max pipelines limit across both types
func (s *PipelineDiscoveryService) applyMaxLimit(buildPipelines, releasePipelines []migrationv1.Pipeline, maxPipelines int) ([]migrationv1.Pipeline, []migrationv1.Pipeline) {
	totalCount := len(buildPipelines) + len(releasePipelines)

	if totalCount <= maxPipelines {
		return buildPipelines, releasePipelines
	}

	// Distribute limit proportionally
	buildRatio := float64(len(buildPipelines)) / float64(totalCount)
	buildLimit := int(float64(maxPipelines) * buildRatio)
	releaseLimit := maxPipelines - buildLimit

	if buildLimit > len(buildPipelines) {
		buildLimit = len(buildPipelines)
		releaseLimit = maxPipelines - buildLimit
	}

	if releaseLimit > len(releasePipelines) {
		releaseLimit = len(releasePipelines)
		buildLimit = maxPipelines - releaseLimit
	}

	return buildPipelines[:buildLimit], releasePipelines[:releaseLimit]
}

// ConvertDiscoveredPipelinesToResources converts discovered pipelines to PipelineResource format
func (s *PipelineDiscoveryService) ConvertDiscoveredPipelinesToResources(discovery *DiscoveryResult) []migrationv1.PipelineResource {
	var resources []migrationv1.PipelineResource

	// Convert build pipelines
	for _, pipeline := range discovery.BuildPipelines {
		resource := migrationv1.PipelineResource{
			ID:                 fmt.Sprintf("%d", pipeline.ID),
			Name:               pipeline.Name,
			Type:               "build",
			TargetWorkflowName: s.sanitizeWorkflowName(pipeline.Name) + ".yml",
		}
		resources = append(resources, resource)
	}

	// Convert release pipelines
	for _, pipeline := range discovery.ReleasePipelines {
		resource := migrationv1.PipelineResource{
			ID:                 fmt.Sprintf("%d", pipeline.ID),
			Name:               pipeline.Name,
			Type:               "release",
			TargetWorkflowName: s.sanitizeWorkflowName(pipeline.Name) + ".yml",
		}
		resources = append(resources, resource)
	}

	return resources
}

// sanitizeWorkflowName sanitizes a pipeline name for use as a workflow file name
func (s *PipelineDiscoveryService) sanitizeWorkflowName(name string) string {
	// Replace special characters with hyphens
	reg := regexp.MustCompile(`[^a-zA-Z0-9-_]+`)
	sanitized := reg.ReplaceAllString(name, "-")

	// Remove leading/trailing hyphens
	sanitized = strings.Trim(sanitized, "-")

	// Convert to lowercase
	sanitized = strings.ToLower(sanitized)

	return sanitized
}
