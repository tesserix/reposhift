# Makefile Commands - UI Dashboard

Quick reference for all UI dashboard make commands.

## Configuration Variables

Set these before running commands (optional):

```bash
# UI Docker image
UI_IMG=myregistry.azurecr.io/migration-ui-dashboard:1.0.0

# Helm release name
UI_HELM_RELEASE=migration-dashboard

# Kubernetes namespace
UI_HELM_NAMESPACE=ado-migration-operator

# Example usage
make ui-helm-install UI_IMG=myregistry.azurecr.io/migration-ui-dashboard:1.0.0
```

---

## UI Dashboard Commands

### Docker Image Management

```bash
# Build UI dashboard Docker image
make ui-docker-build

# Build with custom image name
make ui-docker-build UI_IMG=myregistry.azurecr.io/dashboard:1.0.0

# Push UI dashboard image to registry
make ui-docker-push

# Push with custom image name
make ui-docker-push UI_IMG=myregistry.azurecr.io/dashboard:1.0.0
```

**Note:** Your UI code should be in the `./ui/` directory for the build to work.

---

### Helm Chart Operations

```bash
# Lint UI dashboard Helm chart (validate syntax)
make ui-helm-lint

# Template UI dashboard Helm chart (dry-run, see generated YAML)
make ui-helm-template

# Install UI dashboard
make ui-helm-install

# Install with custom image
make ui-helm-install UI_IMG=myregistry.azurecr.io/dashboard:1.0.0

# Upgrade UI dashboard (or install if not exists)
make ui-helm-upgrade

# Upgrade with custom image
make ui-helm-upgrade UI_IMG=myregistry.azurecr.io/dashboard:1.0.0

# Uninstall UI dashboard
make ui-helm-uninstall
```

---

### UI Dashboard Debugging

```bash
# Check UI dashboard status (pods + service)
make ui-status

# View UI dashboard logs (follow mode)
make ui-logs

# Port forward UI to localhost:8080
make ui-port-forward
# Then open: http://localhost:8080
```

---

## Full Stack Deployment

Deploy/manage both operator AND UI dashboard together:

```bash
# Deploy operator + UI dashboard
make deploy-all

# Upgrade both operator + UI dashboard
make upgrade-all

# Check status of both operator + UI dashboard
make status-all

# Uninstall both operator + UI dashboard
make uninstall-all
```

---

## Quick Start Examples

### Example 1: Fresh Install Everything

```bash
# 1. Install operator
make helm-install

# 2. Install UI dashboard
make ui-helm-install

# 3. Check status
make status-all

# 4. Access UI
make ui-port-forward
```

### Example 2: Full Stack with Custom Image

```bash
# Deploy everything with custom UI image
make deploy-all \
  UI_IMG=myregistry.azurecr.io/migration-dashboard:1.0.0
```

### Example 3: Update UI Only

```bash
# Build new UI image
make ui-docker-build UI_IMG=myregistry.azurecr.io/dashboard:2.0.0

# Push to registry
make ui-docker-push UI_IMG=myregistry.azurecr.io/dashboard:2.0.0

# Upgrade UI in cluster
make ui-helm-upgrade UI_IMG=myregistry.azurecr.io/dashboard:2.0.0

# Check if update succeeded
make ui-status
```

### Example 4: Debug UI Issues

```bash
# Check UI pod status
make ui-status

# View UI logs
make ui-logs

# Port forward to test locally
make ui-port-forward

# Check backend connection
kubectl exec -n ado-migration-operator deployment/migration-dashboard -- \
  wget -qO- http://ado-migration-operator-operator:8080/health
```

---

## Command Summary Table

| Command | Description | Quick Example |
|---------|-------------|---------------|
| `ui-docker-build` | Build UI Docker image | `make ui-docker-build` |
| `ui-docker-push` | Push UI image to registry | `make ui-docker-push` |
| `ui-helm-lint` | Validate Helm chart | `make ui-helm-lint` |
| `ui-helm-template` | Preview generated YAML | `make ui-helm-template` |
| `ui-helm-install` | Install UI dashboard | `make ui-helm-install` |
| `ui-helm-upgrade` | Upgrade UI dashboard | `make ui-helm-upgrade` |
| `ui-helm-uninstall` | Remove UI dashboard | `make ui-helm-uninstall` |
| `ui-port-forward` | Access UI on localhost:8080 | `make ui-port-forward` |
| `ui-logs` | View UI logs | `make ui-logs` |
| `ui-status` | Check UI health | `make ui-status` |
| `deploy-all` | Install operator + UI | `make deploy-all` |
| `upgrade-all` | Upgrade operator + UI | `make upgrade-all` |
| `uninstall-all` | Remove operator + UI | `make uninstall-all` |
| `status-all` | Check all components | `make status-all` |

---

## Typical Workflow

### Development Workflow

```bash
# 1. Make code changes to UI
vim ui/src/App.tsx

# 2. Build new image
make ui-docker-build UI_IMG=localhost:5000/dashboard:dev

# 3. Push to local registry (optional)
make ui-docker-push UI_IMG=localhost:5000/dashboard:dev

# 4. Upgrade in cluster
make ui-helm-upgrade UI_IMG=localhost:5000/dashboard:dev

# 5. Test
make ui-port-forward
```

### Production Deployment

```bash
# 1. Build production image
make ui-docker-build UI_IMG=prodregistry.azurecr.io/dashboard:1.0.0

# 2. Push to production registry
make ui-docker-push UI_IMG=prodregistry.azurecr.io/dashboard:1.0.0

# 3. Upgrade in production cluster
make ui-helm-upgrade \
  UI_IMG=prodregistry.azurecr.io/dashboard:1.0.0 \
  UI_HELM_NAMESPACE=production

# 4. Verify deployment
make ui-status UI_HELM_NAMESPACE=production
```

---

## Troubleshooting

### UI Pod Not Starting

```bash
# Check pod status
make ui-status

# View logs for errors
make ui-logs

# Check helm release
helm list -n ado-migration-operator

# Describe pod for events
kubectl describe pod -n ado-migration-operator \
  -l app.kubernetes.io/name=migration-ui-dashboard
```

### UI Can't Connect to Backend

```bash
# Check operator is running
kubectl get pods -n ado-migration-operator | grep operator

# Check backend service exists
kubectl get svc -n ado-migration-operator | grep operator

# Check UI config
kubectl get configmap -n ado-migration-operator migration-dashboard-config -o yaml

# Test connection from UI pod
kubectl exec -n ado-migration-operator deployment/migration-dashboard -- \
  curl http://ado-migration-operator-operator:8080/health
```

### Need to Reinstall

```bash
# Uninstall
make ui-helm-uninstall

# Wait for cleanup
kubectl wait --for=delete pod \
  -l app.kubernetes.io/name=migration-ui-dashboard \
  -n ado-migration-operator \
  --timeout=60s

# Reinstall
make ui-helm-install
```

---

## Tips & Tricks

### Override Multiple Variables

```bash
make ui-helm-upgrade \
  UI_IMG=myregistry.azurecr.io/dashboard:2.0.0 \
  UI_HELM_RELEASE=my-dashboard \
  UI_HELM_NAMESPACE=my-namespace
```

### Use Custom Values File

```bash
# Create custom values
cat > my-ui-values.yaml <<EOF
image:
  repository: myregistry.azurecr.io/dashboard
  tag: 1.0.0
replicaCount: 3
resources:
  requests:
    cpu: 500m
    memory: 512Mi
EOF

# Install with custom values
helm install migration-dashboard charts/migration-ui-dashboard \
  -f my-ui-values.yaml \
  -n ado-migration-operator
```

### Quick Restart

```bash
# Restart UI pods
kubectl rollout restart deployment/migration-dashboard \
  -n ado-migration-operator

# Or delete pods (they'll be recreated)
kubectl delete pods -n ado-migration-operator \
  -l app.kubernetes.io/name=migration-ui-dashboard
```

---

**All commands are production-ready!** 🚀
