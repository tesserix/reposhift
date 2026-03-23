package controller

import (
	"fmt"
	"strings"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// GenerateGitHubRepositoryName generates the GitHub repository name based on migration configuration
//
// Repository Naming Rules:
//
// 1. CUSTOM NAME (Optional):
//   - If target.repository is specified AND autoGenerateName is false
//   - Uses the custom name as-is (normalized)
//   - Example: target.repository="my-custom-repo" → "my-custom-repo"
//
// 2. AUTO-GENERATED NAME (Default):
//   - If autoGenerateName is true OR target.repository is empty
//   - Applies naming convention based on repository type:
//     a) Product Repository (source.isProductRepo = true OR not specified - defaults to true):
//     → "product-<repo_name>" (unless "product" already in name)
//     Example: source.repository="samyak" → "product-samyak"
//     Example: source.repository="product-api" → "product-api" (no duplicate)
//     b) Business Unit Repository (source.isProductRepo = false):
//     → "<bu_name>-<repo_name>" (unless BU name already in repo name)
//     Example: source.repository="samyak", source.businessUnit="platform" → "platform-samyak"
//     Example: source.repository="platform-api", source.businessUnit="platform" → "platform-api" (no duplicate)
//
// Note: All repository names are normalized (lowercase, hyphens instead of spaces/underscores)
func GenerateGitHubRepositoryName(migration *migrationv1.AdoToGitMigration) (string, error) {
	target := &migration.Spec.Target
	source := &migration.Spec.Source

	// Apply defaults: isProductRepo defaults to true if not explicitly set to false
	// This is achieved by checking the boolean value which defaults to false in Go,
	// but we treat absence as "product repo" (true) for user convenience
	// Note: In practice, the CRD should set a default via kubebuilder markers

	// Get source repository name
	sourceRepoName := source.Repository
	if sourceRepoName == "" {
		// Try to get from resources
		if len(migration.Spec.Resources) > 0 {
			for _, res := range migration.Spec.Resources {
				if res.Type == "repository" {
					sourceRepoName = res.SourceName
					break
				}
			}
		}
	}

	if sourceRepoName == "" {
		return "", fmt.Errorf("source repository name not specified in source.repository or resources")
	}

	// Normalize source repository name
	normalizedName := normalizeRepositoryName(sourceRepoName)

	// SCENARIO 1: Custom repository name (optional)
	// User explicitly provided a custom name and disabled auto-generation
	if target.Repository != "" && !target.AutoGenerateName {
		customName := normalizeRepositoryName(target.Repository)
		return customName, nil
	}

	// SCENARIO 2: Auto-generate repository name (default behavior)
	// Either autoGenerateName is explicitly true, or target.repository is empty
	if target.AutoGenerateName || target.Repository == "" {
		return generateNameWithPrefix(source, normalizedName)
	}

	// SCENARIO 3: Fallback - use target repository if specified
	if target.Repository != "" {
		return normalizeRepositoryName(target.Repository), nil
	}

	// Final fallback: use normalized source repository name
	return normalizedName, nil
}

// generateNameWithPrefix applies naming rules based on source metadata
// Implements smart duplicate detection to avoid redundant words
func generateNameWithPrefix(source *migrationv1.AdoSourceConfig, repoName string) (string, error) {
	if source.IsProductRepo {
		// Product repository: product-<repo_name>
		// Smart logic: avoid duplicate "product" if already in source name
		if containsWord(repoName, "product") {
			// Source name already contains "product", don't add prefix
			return repoName, nil
		}
		return fmt.Sprintf("product-%s", repoName), nil
	}

	// Non-product repository: <bu_name>-<repo_name>
	if source.BusinessUnit == "" {
		// If business unit not specified, default behavior:
		// Since repo-type is non-product but no BU, use repo name without prefix
		return repoName, nil
	}

	buName := normalizeRepositoryName(source.BusinessUnit)

	// Smart logic: avoid duplicate BU name if already in source name
	if containsWord(repoName, buName) {
		// Source name already contains the BU name, don't add prefix
		return repoName, nil
	}

	return fmt.Sprintf("%s-%s", buName, repoName), nil
}

// containsWord checks if a word exists as a separate component in the name
// Example: containsWord("product-api", "product") returns true
//
//	containsWord("production-api", "product") returns false (different word)
func containsWord(name string, word string) bool {
	// Split by common separators
	parts := strings.FieldsFunc(name, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})

	for _, part := range parts {
		if strings.EqualFold(part, word) {
			return true
		}
	}

	return false
}

// normalizeRepositoryName normalizes repository name to GitHub standards
// - Converts to lowercase
// - Replaces spaces and underscores with hyphens
// - Removes invalid characters
// - Trims leading/trailing hyphens
func normalizeRepositoryName(name string) string {
	// Convert to lowercase
	normalized := strings.ToLower(name)

	// Replace spaces and underscores with hyphens
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = strings.ReplaceAll(normalized, "_", "-")

	// Remove invalid characters (keep only alphanumeric, hyphens, dots)
	var result strings.Builder
	for _, char := range normalized {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '.' {
			result.WriteRune(char)
		}
	}

	normalized = result.String()

	// Remove consecutive hyphens
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}

	// Trim leading/trailing hyphens
	normalized = strings.Trim(normalized, "-")

	return normalized
}

// ValidateRepositoryNaming validates the repository naming configuration
// This validation runs BEFORE attempting to create the repository
func ValidateRepositoryNaming(migration *migrationv1.AdoToGitMigration) error {
	source := &migration.Spec.Source
	target := &migration.Spec.Target

	// Validation 1: Business Unit required for non-product repos with auto-naming
	if target.AutoGenerateName && !source.IsProductRepo && source.BusinessUnit == "" {
		return fmt.Errorf("businessUnit must be specified when autoGenerateName is true and isProductRepo is false")
	}

	// Validation 2: Repository name must be specified somewhere
	if source.Repository == "" && target.Repository == "" {
		if len(migration.Spec.Resources) == 0 {
			return fmt.Errorf("repository name must be specified in source, target, or resources")
		}
		hasRepo := false
		for _, res := range migration.Spec.Resources {
			if res.Type == "repository" {
				hasRepo = true
				break
			}
		}
		if !hasRepo {
			return fmt.Errorf("no repository resource found in resources")
		}
	}

	// Validation 3: Custom name validation
	if target.Repository != "" && !target.AutoGenerateName {
		// Custom repository name is provided
		// Ensure it's not empty after normalization
		normalized := normalizeRepositoryName(target.Repository)
		if normalized == "" {
			return fmt.Errorf("custom repository name '%s' is invalid after normalization", target.Repository)
		}
	}

	return nil
}

// IsCustomRepositoryName returns true if a custom repository name is being used
// (as opposed to auto-generated name)
func IsCustomRepositoryName(migration *migrationv1.AdoToGitMigration) bool {
	target := &migration.Spec.Target
	// Custom name is used when repository is specified AND auto-generate is disabled
	return target.Repository != "" && !target.AutoGenerateName
}
