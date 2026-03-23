# Migration UI Dashboard Helm Chart

Simple Helm chart for deploying the Migration Operator UI Dashboard.

## What This Does

Deploys a web UI dashboard that shows:
- Migration job status and progress
- Real-time metrics from the migration operator
- Worker pod statuses
- Migration history and logs

## Quick Install

```bash
# Install with default values
helm install migration-dashboard ./charts/migration-ui-dashboard \
  --namespace ado-migration-operator \
  --set image.repository=your-registry.azurecr.io/migration-ui-dashboard \
  --set image.tag=latest

# Or with custom values file
helm install migration-dashboard ./charts/migration-ui-dashboard \
  --namespace ado-migration-operator \
  --values my-values.yaml
```

## Configuration

### Required Settings

Update these values before installing:

```yaml
# Your Docker image
image:
  repository: your-registry.azurecr.io/migration-ui-dashboard
  tag: "1.0.0"

# Backend operator service connection
backend:
  operatorService:
    host: ado-migration-operator-operator.ado-migration-operator.svc.cluster.local
    port: 8080
```

### Optional Settings

```yaml
# Number of dashboard replicas
replicaCount: 2

# Enable ingress for external access
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: migration-dashboard.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: migration-dashboard-tls
      hosts:
        - migration-dashboard.example.com

# Resource limits
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

## How It Connects to Backend

The dashboard connects to the migration operator backend in two ways:

### 1. HTTP API (Default)
- Calls the operator's REST API endpoints
- Configured via `backend.operatorService.host` and `port`
- Used for: metrics, health checks, migration status

### 2. Kubernetes API (Optional)
- Directly queries Kubernetes for MigrationJob resources
- Requires RBAC permissions (automatically created)
- Used for: real-time updates, detailed pod status

## Environment Variables

These are automatically set from the ConfigMap:

| Variable | Description | Source |
|----------|-------------|--------|
| `BACKEND_API_URL` | Full URL to operator API | ConfigMap |
| `OPERATOR_SERVICE_HOST` | Operator service hostname | ConfigMap |
| `OPERATOR_SERVICE_PORT` | Operator service port | ConfigMap |
| `MIGRATION_NAMESPACE` | Namespace where migrations run | ConfigMap |
| `NODE_ENV` | Node environment (production/dev) | values.yaml |
| `PORT` | UI server port | values.yaml |

## Accessing the Dashboard

### Option 1: Port Forward (Testing)
```bash
kubectl port-forward svc/migration-dashboard 8080:80 -n ado-migration-operator

# Open browser to http://localhost:8080
```

### Option 2: Ingress (Production)
```bash
# Enable ingress in values.yaml
ingress:
  enabled: true
  hosts:
    - host: migration-dashboard.example.com

# Access at https://migration-dashboard.example.com
```

### Option 3: LoadBalancer
```bash
# Change service type in values.yaml
service:
  type: LoadBalancer

# Get external IP
kubectl get svc migration-dashboard -n ado-migration-operator
```

## Example values.yaml

```yaml
# Simple production setup
image:
  repository: myregistry.azurecr.io/migration-dashboard
  tag: "1.0.0"

replicaCount: 2

backend:
  operatorService:
    host: ado-migration-operator-operator.ado-migration-operator.svc.cluster.local
    port: 8080

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: migration-dashboard.mycompany.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: migration-dashboard-tls
      hosts:
        - migration-dashboard.mycompany.com

resources:
  requests:
    cpu: 200m
    memory: 256Mi
  limits:
    cpu: 1000m
    memory: 1Gi
```

## Troubleshooting

### Dashboard can't connect to backend

Check the operator service is running:
```bash
kubectl get svc -n ado-migration-operator | grep operator
```

Check the ConfigMap has correct values:
```bash
kubectl get configmap migration-dashboard-config -n ado-migration-operator -o yaml
```

### Dashboard pod not starting

Check pod logs:
```bash
kubectl logs -l app.kubernetes.io/name=migration-ui-dashboard -n ado-migration-operator
```

Check pod events:
```bash
kubectl describe pod -l app.kubernetes.io/name=migration-ui-dashboard -n ado-migration-operator
```

### Can't access dashboard externally

If using Ingress, check it's created:
```bash
kubectl get ingress -n ado-migration-operator
```

Check ingress controller is running:
```bash
kubectl get pods -n ingress-nginx
```

## Upgrading

```bash
# Upgrade to new version
helm upgrade migration-dashboard ./charts/migration-ui-dashboard \
  --namespace ado-migration-operator \
  --values my-values.yaml

# Check upgrade status
helm list -n ado-migration-operator
```

## Uninstalling

```bash
helm uninstall migration-dashboard -n ado-migration-operator
```

## What's Included

This chart creates:
- ✅ **Deployment** - Runs your UI dashboard pods
- ✅ **Service** - Exposes the dashboard within the cluster
- ✅ **ConfigMap** - Backend connection configuration
- ✅ **ServiceAccount** - For Kubernetes API access (optional)
- ✅ **RBAC** - Permissions to read migration resources (optional)
- ✅ **Ingress** - External access (optional)

## Chart Values Reference

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Docker image repository | `your-registry.azurecr.io/migration-ui-dashboard` |
| `image.tag` | Image tag | `latest` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `replicaCount` | Number of replicas | `2` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `80` |
| `service.targetPort` | Container port | `3000` |
| `backend.operatorService.host` | Operator service hostname | `ado-migration-operator-operator...` |
| `backend.operatorService.port` | Operator service port | `8080` |
| `backend.kubernetesApi.enabled` | Enable direct K8s API access | `true` |
| `backend.kubernetesApi.namespace` | Migration namespace | `ado-migration-operator` |
| `ingress.enabled` | Enable ingress | `false` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |

---

**Simple, clean, and ready to use!** 🚀
