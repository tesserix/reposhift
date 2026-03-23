package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/tesserix/reposhift/internal/platform/secrets"
)

// CreateMigrationRequest carries the parameters for creating a new migration.
type CreateMigrationRequest struct {
	DisplayName    string                 `json:"displayName"`
	SourceOrg      string                 `json:"sourceOrg"`
	SourceProject  string                 `json:"sourceProject"`
	SourceRepos    []string               `json:"sourceRepos"`
	TargetOwner    string                 `json:"targetOwner"`
	ADOSecretName  string                 `json:"adoSecretName"`
	GitHubSecretName string              `json:"githubSecretName"`
	Settings       map[string]interface{} `json:"settings,omitempty"`
}

// MigrationStatusResponse combines the DB record with live CRD status.
type MigrationStatusResponse struct {
	TenantMigration

	Phase    string           `json:"phase"`
	Progress ProgressSummary  `json:"progress"`
	Resources []ResourceStatusItem `json:"resources,omitempty"`
}

// ProgressSummary is a snapshot of migration progress.
type ProgressSummary struct {
	Total      int `json:"total"`
	Completed  int `json:"completed"`
	Failed     int `json:"failed"`
	Percentage int `json:"percentage"`
}

// ResourceStatusItem tracks a single resource within a migration.
type ResourceStatusItem struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	GitHubURL  string `json:"githubUrl,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Orchestrator coordinates migration lifecycle between the database,
// Kubernetes secrets/CRDs, and the secrets provider.
type Orchestrator struct {
	store     *MigrationStore
	secrets   secrets.SecretsProvider
	k8s       kubernetes.Interface
	namespace string
}

// NewOrchestrator creates an Orchestrator. Pass nil for k8s if running
// outside a cluster (K8s operations will be stubbed).
func NewOrchestrator(store *MigrationStore, sp secrets.SecretsProvider, k8s kubernetes.Interface, namespace string) *Orchestrator {
	if namespace == "" {
		namespace = "reposhift-system"
	}
	return &Orchestrator{
		store:     store,
		secrets:   sp,
		k8s:       k8s,
		namespace: namespace,
	}
}

// CreateMigration provisions the backing K8s resources and persists a
// migration record for the tenant.
func (o *Orchestrator) CreateMigration(ctx context.Context, tenantID string, req CreateMigrationRequest) (*TenantMigration, error) {
	migrationID := uuid.New().String()
	crName := fmt.Sprintf("mig-%s", migrationID[:8])
	ns := o.namespaceForTenant(tenantID)

	// Resolve ADO token from the secrets provider.
	adoData, err := o.secrets.Get(ctx, tenantID, req.ADOSecretName)
	if err != nil {
		return nil, fmt.Errorf("resolve ADO secret %q: %w", req.ADOSecretName, err)
	}

	// Resolve GitHub token from the secrets provider.
	ghData, err := o.secrets.Get(ctx, tenantID, req.GitHubSecretName)
	if err != nil {
		return nil, fmt.Errorf("resolve GitHub secret %q: %w", req.GitHubSecretName, err)
	}

	// Create a K8s Secret containing both sets of credentials so the
	// operator controller can mount them into the migration job.
	k8sSecretName := fmt.Sprintf("%s-creds", crName)
	if err := o.ensureK8sSecret(ctx, ns, k8sSecretName, adoData, ghData); err != nil {
		return nil, fmt.Errorf("create k8s secret: %w", err)
	}

	// TODO: Create the AdoToGitMigration CRD object in the cluster.
	// This will be implemented once the dynamic client helpers are wired up.
	// The CRD spec should reference:
	//   - Source: req.SourceOrg / req.SourceProject / req.SourceRepos
	//   - Target: req.TargetOwner
	//   - Auth secret: k8sSecretName in namespace ns
	//   - Settings: req.Settings

	// Persist the migration record in the tenant_migrations table.
	configBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal migration config: %w", err)
	}

	m := &TenantMigration{
		ID:          migrationID,
		TenantID:    tenantID,
		CRName:      crName,
		CRNamespace: ns,
		CRKind:      "AdoToGitMigration",
		DisplayName: req.DisplayName,
		Config:      configBytes,
		Status:      "Pending",
	}
	if err := o.store.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("persist migration record: %w", err)
	}

	return m, nil
}

// GetMigrationStatus returns the DB record enriched with live CRD status
// when available.
func (o *Orchestrator) GetMigrationStatus(ctx context.Context, tenantID, id string) (*MigrationStatusResponse, error) {
	m, err := o.store.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	resp := &MigrationStatusResponse{
		TenantMigration: *m,
		Phase:           m.Status,
		Progress: ProgressSummary{
			Total:      0,
			Completed:  0,
			Failed:     0,
			Percentage: 0,
		},
	}

	// TODO: Fetch live status from the AdoToGitMigration CRD using the
	// dynamic client. When available, overlay Phase, Progress, and
	// Resources from the CRD status onto the response.

	return resp, nil
}

// PauseMigration annotates the CRD to request a pause.
func (o *Orchestrator) PauseMigration(ctx context.Context, tenantID, id string) error {
	return o.annotateCRD(ctx, tenantID, id, "reposhift.tesserix.io/action", "pause")
}

// ResumeMigration annotates the CRD to request a resume.
func (o *Orchestrator) ResumeMigration(ctx context.Context, tenantID, id string) error {
	return o.annotateCRD(ctx, tenantID, id, "reposhift.tesserix.io/action", "resume")
}

// CancelMigration annotates the CRD to request cancellation.
func (o *Orchestrator) CancelMigration(ctx context.Context, tenantID, id string) error {
	return o.annotateCRD(ctx, tenantID, id, "reposhift.tesserix.io/action", "cancel")
}

// RetryMigration annotates the CRD to request a retry.
func (o *Orchestrator) RetryMigration(ctx context.Context, tenantID, id string) error {
	return o.annotateCRD(ctx, tenantID, id, "reposhift.tesserix.io/action", "retry")
}

// annotateCRD sets an annotation on the backing CRD to signal a lifecycle
// action to the operator controller.
func (o *Orchestrator) annotateCRD(ctx context.Context, tenantID, id, key, value string) error {
	m, err := o.store.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}

	// TODO: Use the dynamic client to PATCH the AdoToGitMigration CRD
	// in m.CRNamespace/m.CRName with the annotation key=value.
	// For now, update the DB status to reflect the requested action.

	var newStatus string
	switch value {
	case "pause":
		newStatus = "Paused"
	case "resume":
		newStatus = "Running"
	case "cancel":
		newStatus = "Cancelled"
	case "retry":
		newStatus = "Pending"
	default:
		return fmt.Errorf("unknown action %q", value)
	}

	_ = m // used above for CRD namespace/name lookup
	return o.store.UpdateStatus(ctx, tenantID, id, newStatus)
}

// ensureK8sSecret creates or updates a Kubernetes Secret containing
// the ADO and GitHub credentials in the target namespace.
func (o *Orchestrator) ensureK8sSecret(ctx context.Context, namespace, name string, adoData, ghData map[string]string) error {
	if o.k8s == nil {
		// Running outside a cluster; skip K8s operations.
		return nil
	}

	stringData := make(map[string]string, len(adoData)+len(ghData))
	for k, v := range adoData {
		stringData[fmt.Sprintf("ado-%s", k)] = v
	}
	for k, v := range ghData {
		stringData[fmt.Sprintf("github-%s", k)] = v
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "reposhift-platform",
			},
		},
		StringData: stringData,
		Type:       corev1.SecretTypeOpaque,
	}

	_, err := o.k8s.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		// If already exists, update it.
		if strings.Contains(err.Error(), "already exists") {
			_, err = o.k8s.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		}
		if err != nil {
			return fmt.Errorf("ensure k8s secret %s/%s: %w", namespace, name, err)
		}
	}
	return nil
}

// namespaceForTenant returns the Kubernetes namespace to use for a tenant's
// migration resources. Multi-tenant deployments isolate per tenant;
// single-tenant falls back to the default namespace.
func (o *Orchestrator) namespaceForTenant(tenantID string) string {
	// For now use a convention-based namespace. A future iteration can
	// look this up from the tenants table.
	_ = tenantID
	return o.namespace
}

// ListMigrations returns paginated migrations for a tenant.
func (o *Orchestrator) ListMigrations(ctx context.Context, tenantID string, limit, offset int) ([]TenantMigration, int, error) {
	return o.store.List(ctx, tenantID, limit, offset)
}

// DeleteMigration removes the migration record and its backing K8s resources.
func (o *Orchestrator) DeleteMigration(ctx context.Context, tenantID, id string) error {
	m, err := o.store.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}

	// Clean up the K8s credentials secret.
	k8sSecretName := fmt.Sprintf("%s-creds", m.CRName)
	if o.k8s != nil {
		_ = o.k8s.CoreV1().Secrets(m.CRNamespace).Delete(ctx, k8sSecretName, metav1.DeleteOptions{})
	}

	// TODO: Delete the AdoToGitMigration CRD from the cluster.

	return o.store.Delete(ctx, tenantID, id)
}

// withTimeout wraps a context with a standard operation timeout.
func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 30*time.Second)
}
