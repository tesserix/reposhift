package services

import (
	"context"
	"fmt"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// applyDefaults applies default settings if not specified by the user
// Uses existing getDefaultTypeMapping() and getDefaultStateMapping() from workitem.go
func applyDefaults(settings *migrationv1.WorkItemMigrationSettings, filters *migrationv1.WorkItemFilters) {
	// Apply default type mapping if not specified
	if settings.TypeMapping == nil || len(settings.TypeMapping) == 0 {
		settings.TypeMapping = getDefaultTypeMapping()
	}

	// Apply default state mapping if not specified
	if settings.StateMapping == nil || len(settings.StateMapping) == 0 {
		settings.StateMapping = getDefaultStateMapping()
	}

	// Apply default batch size if not specified
	// OPTIMIZATION (Task 1.2): Increased from 80 to 100 to align with workitem.go changes
	if settings.BatchSize == 0 {
		settings.BatchSize = 100
	}

	// Enable defaults for common options if not explicitly set
	// With pointer bools, nil means "not set", so we can distinguish from explicit false
	if settings.IncludeHistory == nil {
		trueVal := true
		settings.IncludeHistory = &trueVal
	}
	if settings.IncludeAttachments == nil {
		trueVal := true
		settings.IncludeAttachments = &trueVal
	}
	if settings.PreserveRelationships == nil {
		trueVal := true
		settings.PreserveRelationships = &trueVal
	}
	if settings.IncludeTags == nil {
		trueVal := true
		settings.IncludeTags = &trueVal
	}
}

// MigrateWorkItemsFromADO is a wrapper function that adapts the controller's call to the existing service implementation
// This function provides a simpler interface for the controller
func (s *WorkItemService) MigrateWorkItemsFromADO(
	ctx context.Context,
	organization string,
	project string,
	team string,
	targetOwner string,
	targetRepo string,
	adoToken string,
	githubToken string,
	settings migrationv1.WorkItemMigrationSettings,
	filters migrationv1.WorkItemFilters,
) ([]MigratedWorkItem, error) {

	// Apply default settings
	applyDefaults(&settings, &filters)

	// If team is specified and no area paths are provided, use the team as area path
	// In ADO, teams are typically associated with area paths (e.g., "Authority\Authority Team" or just "Authority")
	if team != "" && (filters.AreaPaths == nil || len(filters.AreaPaths) == 0) {
		// Try common area path patterns: just the project name or project\team
		filters.AreaPaths = []string{project}
	}

	// Convert CRD types to service types
	request := WorkItemMigrationRequest{
		SourceOrganization: organization,
		SourceProject:      project,
		TargetOwner:        targetOwner,
		TargetRepository:   targetRepo,
		Settings: WorkItemMigrationSettings{
			TypeMapping:           settings.TypeMapping,
			StateMapping:          settings.StateMapping,
			IncludeHistory:        settings.IncludeHistory != nil && *settings.IncludeHistory,
			IncludeAttachments:    settings.IncludeAttachments != nil && *settings.IncludeAttachments,
			PreserveRelationships: settings.PreserveRelationships != nil && *settings.PreserveRelationships,
			IncludeTags:           settings.IncludeTags != nil && *settings.IncludeTags,
			BatchSize:             settings.BatchSize,
			AdoToken:              adoToken,
			GitHubToken:           githubToken,
		},
		Filters: WorkItemFilters{
			Types:          filters.Types,
			States:         filters.States,
			AreaPaths:      filters.AreaPaths,
			IterationPaths: filters.IterationPaths,
			Tags:           filters.Tags,
			AssignedTo:     filters.AssignedTo,
			WIQLQuery:      filters.WIQLQuery,
		},
	}

	// Convert date range if present
	if filters.DateRange != nil {
		request.Filters.DateRange = &DateRange{}
		if filters.DateRange.Start != nil {
			startTime := filters.DateRange.Start.Time
			request.Filters.DateRange.Start = &startTime
		}
		if filters.DateRange.End != nil {
			endTime := filters.DateRange.End.Time
			request.Filters.DateRange.End = &endTime
		}
	}

	// Call the existing MigrateWorkItems function
	result, err := s.MigrateWorkItems(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("work item migration failed: %w", err)
	}

	// Return the migrated items directly - they're already the right type
	return result.MigratedItems, nil
}
