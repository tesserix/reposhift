package secrets

import (
	"context"
	"time"
)

// SecretMetadata holds non-sensitive information about a stored secret.
type SecretMetadata struct {
	ID         string
	Name       string
	SecretType string
	Metadata   map[string]string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SecretsProvider abstracts secret storage for Kubernetes-native deployments.
type SecretsProvider interface {
	// Store persists secret data identified by name and type.
	Store(ctx context.Context, name, secretType string, data map[string]string) error

	// Get retrieves secret data for the given name.
	Get(ctx context.Context, name string) (map[string]string, error)

	// Delete removes a secret by name.
	Delete(ctx context.Context, name string) error

	// List returns metadata for all managed secrets.
	List(ctx context.Context) ([]SecretMetadata, error)

	// ResolveK8sSecretName returns the Kubernetes Secret name and namespace
	// that an operator CRD should reference for a given secret.
	ResolveK8sSecretName(ctx context.Context, name string) (secretName string, namespace string, err error)
}
