# ADO to Git Migration Operator Helm Chart

This Helm chart installs the Azure DevOps to GitHub Migration Operator in your Kubernetes cluster.

## Introduction

The ADO to Git Migration Operator is a Kubernetes operator that facilitates the migration of resources from Azure DevOps to GitHub, including repositories, work items, and pipelines.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.2.0+

## Installing the Chart

### Development Environment

To install the chart in development environment:

```bash
# Create namespace
kubectl create namespace ado-migration-operator

# Create ACR secret for dev
kubectl create secret docker-registry acr-dev-secret \
  --namespace ado-migration-operator \
  --docker-server=acrbismuthdevtest.azurecr.io \
  --docker-username=<acr-username> \
  --docker-password=<acr-password>

# Install with dev values
helm install ado-migration ./charts/ado-git-migration \
  -f ./charts/ado-git-migration/values-dev.yaml \
  --namespace ado-migration-operator \
  --set image.tag=<version>
```

### Production Environment

To install the chart in production environment:

```bash
# Create namespace
kubectl create namespace ado-migration-operator

# Create ACR secret for prod
kubectl create secret docker-registry acr-prod-secret \
  --namespace ado-migration-operator \
  --docker-server=acrbismuthprod.azurecr.io \
  --docker-username=<acr-username> \
  --docker-password=<acr-password>

# Install with prod values
helm install ado-migration ./charts/ado-git-migration \
  -f ./charts/ado-git-migration/values-prod.yaml \
  --namespace ado-migration-operator \
  --set image.tag=<version>
```

## Environment-Specific Values

### Development (`values-dev.yaml`)

- Uses `acrbismuthdevtest.azurecr.io` registry
- Debug logging enabled
- Leader election disabled (single replica)
- Autoscaling disabled
- Lower resource limits for cost efficiency
- Namespace: `ado-migrations-dev`

### Production (`values-prod.yaml`)

- Uses `acrbismuthprod.azurecr.io` registry
- Info-level logging
- Leader election enabled for HA
- Autoscaling enabled (2-10 replicas)
- Higher resource limits
- Pod disruption budget enabled
- Network policies enabled
- Service monitor for Prometheus
- Namespace: `ado-migrations-prod`

## Configuration

The following table lists the configurable parameters of the chart and their default values.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Image repository | `acrbismuthdevtest.azurecr.io/platform/idp/microservices/ado-to-git-migration` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag | `""` (defaults to chart appVersion) |
| `imagePullSecrets` | Image pull secrets | `[]` |
| `nameOverride` | Override the name of the chart | `""` |
| `fullnameOverride` | Override the full name of the chart | `""` |
| `operator.logLevel` | Log level (debug, info, warn, error) | `"info"` |
| `operator.enableMetrics` | Enable metrics | `true` |
| `operator.enableLeaderElection` | Enable leader election for high availability | `true` |
| `operator.enableWebhooks` | Enable webhooks | `false` |
| `operator.http.port` | HTTP server port | `8080` |
| `operator.health.port` | Health probe port | `8081` |
| `operator.webhook.port` | Webhook server port | `9443` |
| `operator.rateLimits.perClient` | Requests per minute per client | `100` |
| `operator.rateLimits.global` | Global requests per second | `1000` |
| `operator.rateLimits.azureDevOps` | Requests per minute to Azure DevOps API | `60` |
| `operator.rateLimits.github` | Requests per hour to GitHub API | `5000` |
| `operator.defaultSettings.maxHistoryDays` | Default max history days | `500` |
| `operator.defaultSettings.maxCommitCount` | Default max commit count | `2000` |
| `operator.defaultSettings.batchSize` | Default batch size | `10` |
| `operator.defaultSettings.retryAttempts` | Default retry attempts | `3` |
| `operator.defaultSettings.parallelWorkers` | Default parallel workers | `5` |
| `auth.createSecrets` | Create authentication secrets (set to false for external secrets) | `false` |
| `auth.azure.clientId` | Azure Service Principal Client ID | `""` |
| `auth.azure.clientSecret` | Azure Service Principal Client Secret | `""` |
| `auth.azure.tenantId` | Azure Service Principal Tenant ID | `""` |
| `auth.github.token` | GitHub Personal Access Token | `""` |
| `auth.githubApp.appId` | GitHub App ID | `""` |
| `auth.githubApp.installationId` | GitHub App Installation ID | `""` |
| `auth.githubApp.privateKey` | GitHub App Private Key (base64 encoded) | `""` |
| `serviceAccount.create` | Create service account | `true` |
| `serviceAccount.annotations` | Service account annotations | `{}` |
| `serviceAccount.name` | Service account name | `""` |
| `podSecurityContext` | Pod security context | See values.yaml |
| `securityContext` | Container security context | See values.yaml |
| `resources` | Resource requests and limits | See values.yaml |
| `autoscaling.enabled` | Enable autoscaling | `true` |
| `autoscaling.minReplicas` | Minimum replicas | `1` |
| `autoscaling.maxReplicas` | Maximum replicas | `5` |
| `autoscaling.targetCPUUtilizationPercentage` | Target CPU utilization | `80` |
| `autoscaling.targetMemoryUtilizationPercentage` | Target memory utilization | `80` |
| `podDisruptionBudget.enabled` | Enable pod disruption budget | `true` |
| `podDisruptionBudget.minAvailable` | Minimum available pods | `1` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity | See values.yaml |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `8080` |
| `service.healthPort` | Health port | `8081` |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `"nginx"` |
| `ingress.annotations` | Ingress annotations | See values.yaml |
| `ingress.hosts` | Ingress hosts | See values.yaml |
| `ingress.tls` | Ingress TLS configuration | See values.yaml |
| `networkPolicy.enabled` | Enable network policy | `true` |
| `networkPolicy.ingressRules` | Network policy ingress rules | See values.yaml |
| `networkPolicy.egressRules` | Network policy egress rules | See values.yaml |
| `serviceMonitor.enabled` | Enable Prometheus ServiceMonitor | `false` |
| `serviceMonitor.interval` | ServiceMonitor scrape interval | `30s` |
| `serviceMonitor.scrapeTimeout` | ServiceMonitor scrape timeout | `10s` |
| `serviceMonitor.labels` | Additional ServiceMonitor labels | `{}` |
| `migrationNamespace.create` | Create migration namespace | `true` |
| `migrationNamespace.name` | Migration namespace name | `"ado-git-migration"` |
| `crds.install` | Install CRDs | `true` |
| `crds.keepOnUninstall` | Keep CRDs on uninstall | `true` |

## Authentication Secrets

After installing the chart, you need to create the following secrets in the migration namespace:

### Azure DevOps Service Principal Secret

```bash
kubectl create secret generic azure-sp-secret \
  --namespace=ado-migrations-dev \
  --from-literal=client-id="your-azure-client-id" \
  --from-literal=client-secret="your-azure-client-secret" \
  --from-literal=tenant-id="your-azure-tenant-id"
```

### GitHub Authentication - Choose ONE Option

#### Option A: GitHub App (Recommended for Production)

**Benefits:**
- Higher rate limits (5,000 requests/hour vs 1,000 for PAT)
- Not tied to user accounts
- Better security and audit trails
- Automatic token refresh

```bash
kubectl create secret generic github-app-secret \
  --namespace=ado-migrations-dev \
  --from-literal=app-id="your-github-app-id" \
  --from-literal=installation-id="your-installation-id" \
  --from-literal=private-key="$(cat your-app-private-key.pem)"
```

**Setup Instructions:** See [docs/GITHUB_APP_SETUP_QUICK.md](../../docs/GITHUB_APP_SETUP_QUICK.md) for complete GitHub App setup.

#### Option B: Personal Access Token (Simple Alternative)

**Benefits:**
- Quick 5-minute setup
- Good for testing and development

```bash
kubectl create secret generic github-token-secret \
  --namespace=ado-migrations-dev \
  --from-literal=token="your-github-pat"
```

**Setup Instructions:** See [docs/CREDENTIALS_SETUP_SIMPLE.md](../../docs/CREDENTIALS_SETUP_SIMPLE.md) for PAT setup.

## Creating a Migration

After setting up the secrets, you can create a migration resource:

### Example: Migration with GitHub App (Recommended)

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: example-migration-github-app
  namespace: ado-migrations-dev
spec:
  type: repository

  source:
    organization: "your-ado-org"
    project: "your-ado-project"
    repository: "your-ado-repo"
    adoAuth:
      servicePrincipal:
        clientIdRef:
          name: azure-sp-secret
          key: client-id
        clientSecretRef:
          name: azure-sp-secret
          key: client-secret
        tenantIdRef:
          name: azure-sp-secret
          key: tenant-id

  target:
    owner: "your-github-org"
    repository: "migrated-repo-name"
    visibility: private
    githubAuth:
      appAuth:
        appIdRef:
          name: github-app-secret
          key: app-id
        installationIdRef:
          name: github-app-secret
          key: installation-id
        privateKeyRef:
          name: github-app-secret
          key: private-key

  resources:
    repository: true
    workItems: false
    pipelines: false

  settings:
    maxHistoryDays: 500
    maxCommitCount: 2000
```

### Example: Migration with Personal Access Token

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: example-migration-pat
  namespace: ado-migrations-dev
spec:
  type: repository

  source:
    organization: "your-ado-org"
    project: "your-ado-project"
    repository: "your-ado-repo"
    adoAuth:
      servicePrincipal:
        clientIdRef:
          name: azure-sp-secret
          key: client-id
        clientSecretRef:
          name: azure-sp-secret
          key: client-secret
        tenantIdRef:
          name: azure-sp-secret
          key: tenant-id

  target:
    owner: "your-github-org"
    repository: "migrated-repo-name"
    visibility: private
    githubAuth:
      tokenRef:
        name: github-token-secret
        key: token

  resources:
    repository: true
    workItems: false
    pipelines: false

  settings:
    maxHistoryDays: 500
    maxCommitCount: 2000
```

For more migration templates, see:
- `../../CLAIM_TEMPLATES/01-auto-discovery-repo-migration.yaml` - Auto-discover and migrate all repositories
- `../../CLAIM_TEMPLATES/02-workitems-migration.yaml` - Migrate work items to GitHub Issues
- `../../CLAIM_TEMPLATES/03-complete-migration.yaml` - Complete migration (repos + work items + projects)

## Uninstalling the Chart

To uninstall/delete the `ado-migration` deployment:

```bash
helm uninstall ado-migration
```

If you set `crds.keepOnUninstall` to `false`, the CRDs will be removed. Otherwise, you need to manually remove them:

```bash
kubectl delete crd adotogitmigrations.migration.ado-to-git-migration.io
kubectl delete crd adodiscoveries.migration.ado-to-git-migration.io
kubectl delete crd pipelinetoworkflows.migration.ado-to-git-migration.io
kubectl delete crd migrationjobs.migration.ado-to-git-migration.io
kubectl delete crd workitemmigrations.migration.ado-to-git-migration.io
```

## Upgrading the Chart

To upgrade the chart:

```bash
helm upgrade ado-migration ./charts/ado-git-migration
```

## Scaling

The operator is configured with a Horizontal Pod Autoscaler (HPA) that will automatically scale based on CPU and memory usage. You can adjust the autoscaling parameters in the values file.

## High Availability

For high availability:

1. Set `operator.enableLeaderElection` to `true`
2. Set `autoscaling.minReplicas` to at least `2`
3. Ensure `podDisruptionBudget.enabled` is `true`

## Monitoring

The operator exposes Prometheus metrics at `/metrics`. To enable ServiceMonitor for Prometheus Operator:

```yaml
serviceMonitor:
  enabled: true
```

## Troubleshooting

If you encounter issues:

1. Check the operator logs:
   ```bash
   kubectl logs -l app.kubernetes.io/name=ado-git-migration -n <namespace>
   ```

2. Check the migration status:
   ```bash
   kubectl get adotogitmigration -n ado-git-migration
   kubectl describe adotogitmigration <name> -n ado-git-migration
   ```

3. Verify the CRDs are installed:
   ```bash
   kubectl get crd | grep migration.ado-to-git-migration.io
   ```

4. Check the health endpoint:
   ```bash
   kubectl port-forward svc/<release-name>-ado-git-migration 8080:8080
   curl http://localhost:8080/api/v1/utils/health
   ```