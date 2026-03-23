package secrets

import (
	"context"
	"time"
)

// SecretMetadata holds non-sensitive information about a stored secret.
type SecretMetadata struct {
	ID        string
	TenantID  string
	Name      string
	SecretType string
	Metadata  map[string]string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SecretsProvider abstracts secret storage across SaaS (DB-backed) and
// self-hosted (Kubernetes-native) deployment modes.
type SecretsProvider interface {
	// Store persists secret data identified by tenant, name, and type.
	Store(ctx context.Context, tenantID, name, secretType string, data map[string]string) error

	// Get retrieves decrypted secret data for the given tenant and name.
	Get(ctx context.Context, tenantID, name string) (map[string]string, error)

	// Delete removes a secret for the given tenant and name.
	Delete(ctx context.Context, tenantID, name string) error

	// List returns metadata for all secrets belonging to a tenant.
	List(ctx context.Context, tenantID string) ([]SecretMetadata, error)

	// ResolveK8sSecretName returns the Kubernetes Secret name and namespace
	// that an operator CRD should reference for a given tenant secret.
	ResolveK8sSecretName(ctx context.Context, tenantID, name string) (secretName string, namespace string, err error)
}
