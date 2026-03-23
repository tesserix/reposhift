package services

import (
	"testing"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

func TestGenerateGitHubRepoName(t *testing.T) {
	tests := []struct {
		name          string
		adoRepoName   string
		businessUnit  string
		productName   string
		migrationType migrationv1.MigrationType
		expected      string
	}{
		// Product type tests
		{
			name:          "Product type with duplicate name in ADO repo",
			adoRepoName:   "altitude-api",
			businessUnit:  "lg",
			productName:   "altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-altitude-api",
		},
		{
			name:          "Product type without duplicate name",
			adoRepoName:   "payment-service",
			businessUnit:  "lg",
			productName:   "altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-altitude-payment-service",
		},
		{
			name:          "Product type with authority duplicate",
			adoRepoName:   "authority-backend",
			businessUnit:  "ea",
			productName:   "authority",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-ea-authority-backend",
		},
		{
			name:          "Product type with exact product name",
			adoRepoName:   "altitude",
			businessUnit:  "lg",
			productName:   "altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-altitude-altitude",
		},
		{
			name:          "Product type with spaces and special chars",
			adoRepoName:   "Altitude API Service",
			businessUnit:  "LG",
			productName:   "Altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-altitude-api-service",
		},
		{
			name:          "Product type with underscores",
			adoRepoName:   "altitude_core_api",
			businessUnit:  "lg",
			productName:   "altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-altitude-core-api",
		},

		// Platform type tests
		{
			name:          "Platform type basic",
			adoRepoName:   "common-utils",
			businessUnit:  "lg",
			productName:   "",
			migrationType: migrationv1.MigrationTypePlatform,
			expected:      "platform-lg-common-utils",
		},
		{
			name:          "Platform type with special chars",
			adoRepoName:   "Shared Infrastructure",
			businessUnit:  "EA",
			productName:   "",
			migrationType: migrationv1.MigrationTypePlatform,
			expected:      "platform-ea-shared-infrastructure",
		},

		// Shared type tests
		{
			name:          "Shared type basic",
			adoRepoName:   "common-library",
			businessUnit:  "",
			productName:   "",
			migrationType: migrationv1.MigrationTypeShared,
			expected:      "shared-common-library",
		},
		{
			name:          "Shared type with special chars",
			adoRepoName:   "Logging_Framework",
			businessUnit:  "",
			productName:   "",
			migrationType: migrationv1.MigrationTypeShared,
			expected:      "shared-logging-framework",
		},

		// Edge cases
		{
			name:          "Empty business unit",
			adoRepoName:   "test-repo",
			businessUnit:  "",
			productName:   "altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-altitude-test-repo",
		},
		{
			name:          "Empty product name",
			adoRepoName:   "test-repo",
			businessUnit:  "lg",
			productName:   "",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-test-repo",
		},
		{
			name:          "Multiple hyphens in ADO name",
			adoRepoName:   "my---test---repo",
			businessUnit:  "lg",
			productName:   "altitude",
			migrationType: migrationv1.MigrationTypeProduct,
			expected:      "product-lg-altitude-my-test-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateGitHubRepoName(tt.adoRepoName, tt.businessUnit, tt.productName, tt.migrationType)
			if result != tt.expected {
				t.Errorf("GenerateGitHubRepoName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStripDuplicateName(t *testing.T) {
	tests := []struct {
		name        string
		repoName    string
		productName string
		expected    string
	}{
		{
			name:        "Duplicate at start",
			repoName:    "altitude-api",
			productName: "altitude",
			expected:    "api",
		},
		{
			name:        "Duplicate at end",
			repoName:    "api-altitude",
			productName: "altitude",
			expected:    "api",
		},
		{
			name:        "Exact match - keep original",
			repoName:    "altitude",
			productName: "altitude",
			expected:    "altitude",
		},
		{
			name:        "No duplicate",
			repoName:    "payment-service",
			productName: "altitude",
			expected:    "payment-service",
		},
		{
			name:        "Partial match - no strip when not at boundaries",
			repoName:    "altitude-authority-service",
			productName: "authority",
			expected:    "altitude-authority-service",
		},
		{
			name:        "Case insensitive match",
			repoName:    "Altitude-API",
			productName: "ALTITUDE",
			expected:    "api",
		},
		{
			name:        "Empty product name",
			repoName:    "test-repo",
			productName: "",
			expected:    "test-repo",
		},
		{
			name:        "Multiple word product in repo",
			repoName:    "authority-team-backend",
			productName: "authority",
			expected:    "team-backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripDuplicateName(tt.repoName, tt.productName)
			if result != tt.expected {
				t.Errorf("stripDuplicateName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNormalizeRepoName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Convert to lowercase",
			input:    "MyRepoName",
			expected: "myreponame",
		},
		{
			name:     "Replace spaces with hyphens",
			input:    "my repo name",
			expected: "my-repo-name",
		},
		{
			name:     "Replace underscores with hyphens",
			input:    "my_repo_name",
			expected: "my-repo-name",
		},
		{
			name:     "Remove special characters",
			input:    "my@repo#name!",
			expected: "my-repo-name",
		},
		{
			name:     "Collapse multiple hyphens",
			input:    "my---repo---name",
			expected: "my-repo-name",
		},
		{
			name:     "Trim leading and trailing hyphens",
			input:    "-my-repo-name-",
			expected: "my-repo-name",
		},
		{
			name:     "Complex case",
			input:    "  My_Repo@Name  With--Spaces  ",
			expected: "my-repo-name-with-spaces",
		},
		{
			name:     "Already normalized",
			input:    "my-repo-name",
			expected: "my-repo-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeRepoName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeRepoName() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValidateGitHubRepoName(t *testing.T) {
	tests := []struct {
		name     string
		repoName string
		expected bool
	}{
		{
			name:     "Valid simple name",
			repoName: "my-repo",
			expected: true,
		},
		{
			name:     "Valid with numbers",
			repoName: "my-repo-123",
			expected: true,
		},
		{
			name:     "Valid with underscores",
			repoName: "my_repo",
			expected: true,
		},
		{
			name:     "Valid with dots",
			repoName: "my.repo.name",
			expected: true,
		},
		{
			name:     "Invalid - starts with hyphen",
			repoName: "-my-repo",
			expected: false,
		},
		{
			name:     "Invalid - ends with hyphen",
			repoName: "my-repo-",
			expected: false,
		},
		{
			name:     "Invalid - starts with underscore",
			repoName: "_my-repo",
			expected: false,
		},
		{
			name:     "Invalid - ends with underscore",
			repoName: "my-repo_",
			expected: false,
		},
		{
			name:     "Invalid - empty string",
			repoName: "",
			expected: false,
		},
		{
			name:     "Invalid - special characters",
			repoName: "my@repo",
			expected: false,
		},
		{
			name:     "Invalid - too long (>100 chars)",
			repoName: "this-is-a-very-long-repository-name-that-exceeds-the-maximum-allowed-length-of-one-hundred-characters-for-github-repository-names",
			expected: false,
		},
		{
			name:     "Valid - exactly 100 chars",
			repoName: "a123456789b123456789c123456789d123456789e123456789f123456789g123456789h123456789i123456789j123456789",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateGitHubRepoName(tt.repoName)
			if result != tt.expected {
				t.Errorf("ValidateGitHubRepoName() = %v, want %v", result, tt.expected)
			}
		})
	}
}
