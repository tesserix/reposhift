package services

import (
	"testing"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

func TestDetermineTargetDefaultBranch(t *testing.T) {
	service := NewRepositoryService()

	tests := []struct {
		name                string
		specifiedBranch     string
		sourceDefaultBranch string
		expected            string
	}{
		{
			name:                "User specified branch takes precedence",
			specifiedBranch:     "custom-main",
			sourceDefaultBranch: "main",
			expected:            "custom-main",
		},
		{
			name:                "Source main branch used when not specified",
			specifiedBranch:     "",
			sourceDefaultBranch: "main",
			expected:            "main",
		},
		{
			name:                "Source master branch used when not specified",
			specifiedBranch:     "",
			sourceDefaultBranch: "master",
			expected:            "master",
		},
		{
			name:                "Source develop branch used when not specified",
			specifiedBranch:     "",
			sourceDefaultBranch: "develop",
			expected:            "develop",
		},
		{
			name:                "Uncommon source branch still used",
			specifiedBranch:     "",
			sourceDefaultBranch: "feature-branch",
			expected:            "feature-branch",
		},
		{
			name:                "Fallback to main when no source branch",
			specifiedBranch:     "",
			sourceDefaultBranch: "",
			expected:            "main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.determineTargetDefaultBranch(tt.specifiedBranch, tt.sourceDefaultBranch)
			if result != tt.expected {
				t.Errorf("determineTargetDefaultBranch() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestRepositoryStateValidation(t *testing.T) {
	// Test that our repository state constants are properly defined
	states := []migrationv1.RepositoryState{
		migrationv1.RepositoryStateNotExists,
		migrationv1.RepositoryStateEmpty,
		migrationv1.RepositoryStateNonEmpty,
		migrationv1.RepositoryStateCreated,
	}

	expectedStates := []string{"NotExists", "Empty", "NonEmpty", "Created"}

	for i, state := range states {
		if string(state) != expectedStates[i] {
			t.Errorf("RepositoryState %d = %v, expected %v", i, state, expectedStates[i])
		}
	}
}
