package secrets

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	managedByLabel = "app.kubernetes.io/managed-by"
	managedByValue = "reposhift"
	secretPrefix   = "reposhift-secret-"
)

// K8sProvider implements SecretsProvider for open-source mode, storing
// secrets as native Kubernetes Secret objects.
type K8sProvider struct {
	namespace string
	clientset kubernetes.Interface
}

// NewK8sProvider creates a K8sProvider that manages secrets in the given
// namespace. It uses in-cluster configuration to build the Kubernetes client.
func NewK8sProvider(namespace string) (*K8sProvider, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("secrets: k8s in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("secrets: k8s clientset: %w", err)
	}

	return &K8sProvider{
		namespace: namespace,
		clientset: clientset,
	}, nil
}

// NewK8sProviderWithClient creates a K8sProvider with an explicit Kubernetes
// client, useful for testing.
func NewK8sProviderWithClient(namespace string, clientset kubernetes.Interface) *K8sProvider {
	return &K8sProvider{
		namespace: namespace,
		clientset: clientset,
	}
}

func k8sSecretName(name string) string {
	return secretPrefix + name
}

// Store creates or updates a Kubernetes Secret with the provided data.
func (p *K8sProvider) Store(ctx context.Context, name, secretType string, data map[string]string) error {
	secretName := k8sSecretName(name)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: p.namespace,
			Labels: map[string]string{
				managedByLabel:              managedByValue,
				"reposhift.io/secret-type": secretType,
			},
			Annotations: map[string]string{
				"reposhift.io/secret-name": name,
			},
		},
		StringData: data,
		Type:       corev1.SecretTypeOpaque,
	}

	existing, err := p.clientset.CoreV1().Secrets(p.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		// Update existing secret.
		existing.StringData = data
		existing.Labels = secret.Labels
		existing.Annotations = secret.Annotations
		_, err = p.clientset.CoreV1().Secrets(p.namespace).Update(ctx, existing, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("secrets: k8s update: %w", err)
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("secrets: k8s get for upsert: %w", err)
	}

	// Create new secret.
	_, err = p.clientset.CoreV1().Secrets(p.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("secrets: k8s create: %w", err)
	}
	return nil
}

// Get retrieves a Kubernetes Secret and returns its data.
func (p *K8sProvider) Get(ctx context.Context, name string) (map[string]string, error) {
	secretName := k8sSecretName(name)

	secret, err := p.clientset.CoreV1().Secrets(p.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("secrets: k8s get: %w", err)
	}

	// Convert []byte values to strings.
	result := make(map[string]string, len(secret.Data))
	for k, v := range secret.Data {
		result[k] = string(v)
	}
	return result, nil
}

// Delete removes a Kubernetes Secret.
func (p *K8sProvider) Delete(ctx context.Context, name string) error {
	secretName := k8sSecretName(name)

	err := p.clientset.CoreV1().Secrets(p.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("secrets: k8s delete: %w", err)
	}
	return nil
}

// List returns metadata for all reposhift-managed Kubernetes Secrets.
func (p *K8sProvider) List(ctx context.Context) ([]SecretMetadata, error) {
	labelSelector := fmt.Sprintf("%s=%s", managedByLabel, managedByValue)

	secrets, err := p.clientset.CoreV1().Secrets(p.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("secrets: k8s list: %w", err)
	}

	result := make([]SecretMetadata, 0, len(secrets.Items))
	for _, s := range secrets.Items {
		// Skip secrets missing required labels or annotations.
		secretName, hasName := s.Annotations["reposhift.io/secret-name"]
		secretType, hasType := s.Labels["reposhift.io/secret-type"]
		if !hasName || !hasType {
			continue
		}

		m := SecretMetadata{
			ID:         string(s.UID),
			Name:       secretName,
			SecretType: secretType,
			// K8s secrets don't track update time; use creation timestamp for both.
			CreatedAt: s.CreationTimestamp.Time,
			UpdatedAt: s.CreationTimestamp.Time,
		}
		result = append(result, m)
	}

	return result, nil
}

// ResolveK8sSecretName returns the Kubernetes Secret name and namespace
// directly since secrets are already stored natively in K8s.
func (p *K8sProvider) ResolveK8sSecretName(_ context.Context, name string) (string, string, error) {
	return k8sSecretName(name), p.namespace, nil
}

// compile-time interface check
var _ SecretsProvider = (*K8sProvider)(nil)

// timestampOrDefault returns the timestamp or a zero time — helper for
// cases where K8s objects may not have a meaningful update timestamp.
func timestampOrDefault(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}
