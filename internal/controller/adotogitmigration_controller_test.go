package controller

import (
	"testing"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

func TestHandleExistingRepository(t *testing.T) {
	reconciler := &AdoToGitMigrationReconciler{}

	tests := []struct {
		name                   string
		resource               migrationv1.MigrationResource
		expectedErrorCount     int
		expectedWarningCount   int
		expectValidationToPass bool
	}{
		{
			name: "No settings - should allow existing repo",
			resource: migrationv1.MigrationResource{
				SourceName: "test-repo",
				TargetName: "test-repo",
				Settings:   nil,
			},
			expectedErrorCount:     0,
			expectedWarningCount:   1,
			expectValidationToPass: true,
		},
		{
			name: "With settings but no repository settings - should allow existing repo",
			resource: migrationv1.MigrationResource{
				SourceName: "test-repo",
				TargetName: "test-repo",
				Settings: &migrationv1.ResourceSettings{
					Repository: nil,
				},
			},
			expectedErrorCount:     0,
			expectedWarningCount:   1,
			expectValidationToPass: true,
		},
		{
			name: "With repository settings - should allow existing repo",
			resource: migrationv1.MigrationResource{
				SourceName: "test-repo",
				TargetName: "test-repo",
				Settings: &migrationv1.ResourceSettings{
					Repository: &migrationv1.RepositorySettings{
						CreateIfNotExists: false, // Even if set to false, we allow existing repos
					},
				},
			},
			expectedErrorCount:     0,
			expectedWarningCount:   1,
			expectValidationToPass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := &migrationv1.AdoToGitMigration{
				Spec: migrationv1.AdoToGitMigrationSpec{
					Target: migrationv1.GitHubTargetConfig{
						Owner: "testowner",
					},
				},
			}

			validationResults := &migrationv1.ValidationResults{
				Valid:    true,
				Errors:   []migrationv1.ValidationError{},
				Warnings: []migrationv1.ValidationWarning{},
			}

			reconciler.handleExistingRepository(migration, tt.resource, validationResults)

			if len(validationResults.Errors) != tt.expectedErrorCount {
				t.Errorf("Expected %d errors, got %d", tt.expectedErrorCount, len(validationResults.Errors))
			}

			if len(validationResults.Warnings) != tt.expectedWarningCount {
				t.Errorf("Expected %d warnings, got %d", tt.expectedWarningCount, len(validationResults.Warnings))
			}

			if validationResults.Valid != tt.expectValidationToPass {
				t.Errorf("Expected validation to pass: %v, got: %v", tt.expectValidationToPass, validationResults.Valid)
			}
		})
	}
}
