package services

import (
	"regexp"
	"strings"

	migrationv1 "github.com/tesserix/reposhift/api/v1"
)

// GenerateGitHubRepoName generates a GitHub repository name based on migration type and source metadata.
// It intelligently strips duplicate names to avoid patterns like "product-lg-altitude-altitude-api".
//
// Naming patterns:
//   - Product: product-<bu>-<product>-<cleaned-repo>
//   - Platform: platform-<bu>-<cleaned-repo>
//   - Shared: shared-<cleaned-repo>
//
// Example transformations:
//   - ADO: "altitude-api", Product: "altitude", BU: "lg" → "product-lg-altitude-api"
//   - ADO: "altitude-authority-service", Product: "altitude", BU: "lg" → "product-lg-altitude-authority-service"
//   - ADO: "common-utils", BU: "lg" → "platform-lg-common-utils"
func GenerateGitHubRepoName(adoRepoName, businessUnit, productName string, migrationType migrationv1.MigrationType) string {
	// Step 1: Normalize the ADO repo name
	cleanedRepo := normalizeRepoName(adoRepoName)

	// Step 2: Strip duplicate product/project names if applicable
	if productName != "" {
		cleanedRepo = stripDuplicateName(cleanedRepo, productName)
	}

	// Step 3: Apply naming pattern based on migration type
	var parts []string

	switch migrationType {
	case migrationv1.MigrationTypeProduct:
		// Format: product-<bu>-<product>-<cleaned-repo>
		parts = []string{"product"}
		if businessUnit != "" {
			parts = append(parts, normalizeRepoName(businessUnit))
		}
		if productName != "" {
			parts = append(parts, normalizeRepoName(productName))
		}
		if cleanedRepo != "" {
			parts = append(parts, cleanedRepo)
		}

	case migrationv1.MigrationTypePlatform:
		// Format: platform-<bu>-<cleaned-repo>
		parts = []string{"platform"}
		if businessUnit != "" {
			parts = append(parts, normalizeRepoName(businessUnit))
		}
		if cleanedRepo != "" {
			parts = append(parts, cleanedRepo)
		}

	case migrationv1.MigrationTypeShared:
		// Format: shared-<cleaned-repo>
		parts = []string{"shared"}
		if cleanedRepo != "" {
			parts = append(parts, cleanedRepo)
		}

	default:
		// Fallback: just use the cleaned repo name
		return cleanedRepo
	}

	// Step 4: Join parts with hyphens and return
	return strings.Join(parts, "-")
}

// normalizeRepoName converts a repository name to lowercase, replaces invalid characters,
// and ensures it follows GitHub naming conventions.
func normalizeRepoName(name string) string {
	// Convert to lowercase
	normalized := strings.ToLower(strings.TrimSpace(name))

	// Replace spaces, underscores, and other special characters with hyphens
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	normalized = re.ReplaceAllString(normalized, "-")

	// Remove leading/trailing hyphens
	normalized = strings.Trim(normalized, "-")

	// Collapse multiple consecutive hyphens into one
	re = regexp.MustCompile(`-+`)
	normalized = re.ReplaceAllString(normalized, "-")

	return normalized
}

// stripDuplicateName removes duplicate product/project name prefixes or suffixes from the repo name.
// This prevents names like "product-lg-altitude-altitude-api" when the ADO repo is "altitude-api"
// and the product is "altitude".
//
// Examples:
//   - stripDuplicateName("altitude-api", "altitude") → "api"
//   - stripDuplicateName("authority-service", "authority") → "service"
//   - stripDuplicateName("api-altitude", "altitude") → "api"
//   - stripDuplicateName("altitude", "altitude") → "altitude" (keep original if it would be empty)
//   - stripDuplicateName("some-other-service", "altitude") → "some-other-service" (no match)
func stripDuplicateName(repoName, productName string) string {
	normalizedRepo := normalizeRepoName(repoName)
	normalizedProduct := normalizeRepoName(productName)

	if normalizedProduct == "" {
		return normalizedRepo
	}

	// Check if repo name starts with product name followed by a hyphen
	prefixPattern := normalizedProduct + "-"
	if strings.HasPrefix(normalizedRepo, prefixPattern) {
		stripped := strings.TrimPrefix(normalizedRepo, prefixPattern)
		// Only strip if something remains after removal
		if stripped != "" {
			return stripped
		}
	}

	// Check if repo name ends with product name preceded by a hyphen
	suffixPattern := "-" + normalizedProduct
	if strings.HasSuffix(normalizedRepo, suffixPattern) {
		stripped := strings.TrimSuffix(normalizedRepo, suffixPattern)
		// Only strip if something remains after removal
		if stripped != "" {
			return stripped
		}
	}

	// Check if repo name is exactly the product name
	// In this case, keep it to avoid empty string
	if normalizedRepo == normalizedProduct {
		return normalizedRepo
	}

	// No duplicate found, return original
	return normalizedRepo
}

// ValidateGitHubRepoName checks if a repository name follows GitHub naming rules:
//   - Only contains alphanumeric characters, hyphens, and underscores
//   - Cannot start or end with hyphen or underscore
//   - Maximum length of 100 characters
func ValidateGitHubRepoName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}

	// Must not start or end with hyphen or underscore
	if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "_") ||
		strings.HasSuffix(name, "-") || strings.HasSuffix(name, "_") {
		return false
	}

	// Must only contain alphanumeric, hyphens, underscores, and dots
	re := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	return re.MatchString(name)
}
