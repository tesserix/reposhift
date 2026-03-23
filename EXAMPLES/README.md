# Migration Examples

This directory contains comprehensive examples for migrating from Azure DevOps to GitHub.

## 📁 Available Examples

### `complete-authority-migration.yaml`
**Complete end-to-end migration example** that demonstrates:

1. **Repository Migration**:
   - Migrates `java-authority` and `devops-external-infra-automation` repos
   - From: Azure DevOps (`civica-international-lg` org, `Authority` project)
   - To: GitHub (`civica` org)

2. **GitHub Project Creation**:
   - Automatically creates GitHub Projects with proper naming
   - **Naming Convention**: Special characters (-, _) → spaces, capitalize first word
   - Examples:
     - `java-authority` → `"Java Authority"`
     - `devops-external-infra-automation` → `"Devops External Infra Automation"`
   - Links projects to repositories automatically

3. **Work Item Migration**:
   - Migrates work items from ADO `Altitude` project
   - **Optimized batch processing** (20 items per batch)
   - Preserves relationships, attachments, and history
   - Custom field and state mapping

## 🚀 Quick Start

### Prerequisites

1. **Create Required Secrets**:
   ```bash
   # Azure DevOps PAT
   kubectl create secret generic azure-ado-pat \
     --namespace=ado-migration-operator \
     --from-literal=token="YOUR_ADO_PAT"

   # GitHub App credentials (recommended for higher rate limits)
   kubectl create secret generic github-app-secret \
     --namespace=ado-migration-operator \
     --from-literal=app-id="YOUR_APP_ID" \
     --from-literal=installation-id="YOUR_INSTALLATION_ID" \
     --from-literal=private-key="$(cat your-private-key.pem)"
   ```

2. **Verify Operator is Running**:
   ```bash
   kubectl get pods -n ado-migration-operator
   ```

### Run the Migration

```bash
# Apply the complete migration
kubectl apply -f EXAMPLES/complete-authority-migration.yaml

# Monitor progress in real-time
kubectl get adotogitmigration -n ado-migration-operator --watch
kubectl get githubproject -n ado-migration-operator --watch
kubectl get workitemmigration -n ado-migration-operator --watch
```

### View Progress

The migrations now show **real-time progress** in "X/Y" format:

```bash
$ kubectl get adotogitmigration -n ado-migration-operator
NAME                        PHASE     PROGRESS  COMPLETED  FAILED  TYPE        AGE
authority-repos-migration   Running   1/2       1          0       repository  5m
```

Progress updates as each repository migrates: `1/2` → `2/2` ✅

## 📊 Monitoring

### Check Detailed Status

```bash
# Repository migration
kubectl describe adotogitmigration authority-repos-migration -n ado-migration-operator

# GitHub Projects
kubectl describe githubproject java-authority-project -n ado-migration-operator

# Work items
kubectl describe workitemmigration altitude-project-workitems -n ado-migration-operator
```

### View Migration Logs

```bash
# Get operator pod name
POD=$(kubectl get pods -n ado-migration-operator -l app.kubernetes.io/name=ado-git-migration -o jsonpath='{.items[0].metadata.name}')

# View logs
kubectl logs -f $POD -n ado-migration-operator
```

## ⚙️ Performance Settings

The example includes **optimized settings** for production use:

### Repository Migration
- **Batch Size**: 50 commits per batch
- **Parallel Workers**: 3 concurrent operations
- **Retry**: 5 attempts with 30s delay
- **History**: 2 years (730 days)

### Work Item Migration
- **Batch Size**: 20 items per batch
- **Batch Delay**: 5 seconds between batches (rate limit management)
- **Parallel Workers**: 3 concurrent operations
- **Retry**: 5 attempts with 30s delay

## 🔧 Customization

### Modify Repository List

Edit the `resources` section in the YAML:

```yaml
resources:
  - type: repository
    sourceId: your-repo-id
    sourceName: your-source-repo-name
    targetName: your-target-repo-name
```

### Change Work Item Filters

Modify the `filters` section:

```yaml
filters:
  types:
    - User Story
    - Bug
    - Task
  states:
    - Active
    - New
```

### Adjust Performance Settings

Tune based on your needs:

```yaml
settings:
  batchSize: 30  # Increase/decrease based on repo size
  parallelWorkers: 5  # More workers = faster (if rate limits allow)
  retryAttempts: 3  # Reduce for faster failure
```

## 🎯 Project Naming Convention

GitHub Projects are created with proper formatting:

| ADO Repository Name             | GitHub Project Name                    |
|---------------------------------|----------------------------------------|
| `java-authority`                | `Java Authority`                       |
| `devops-external-infra-automation` | `Devops External Infra Automation`  |
| `my-cool-service`               | `My Cool Service`                      |
| `api_gateway`                   | `Api Gateway`                          |

**Rules**:
- Replace `-` and `_` with spaces
- Capitalize first word
- Keep subsequent words as-is

## ❓ Troubleshooting

### Migration Stuck

```bash
# Check if it's rate limited
kubectl describe adotogitmigration authority-repos-migration -n ado-migration-operator | grep -A 5 "Error"

# Restart if needed
kubectl delete pod $POD -n ado-migration-operator
```

### View Errors

```bash
# Get migration status
kubectl get adotogitmigration -n ado-migration-operator -o yaml

# Check events
kubectl get events -n ado-migration-operator --sort-by='.lastTimestamp'
```

### Delete and Retry

```bash
# Delete failed migration
kubectl delete -f EXAMPLES/complete-authority-migration.yaml

# Wait a moment, then retry
kubectl apply -f EXAMPLES/complete-authority-migration.yaml
```

## 📚 Additional Resources

- [API Endpoints Reference](../docs/API_ENDPOINTS_REFERENCE.md)
- [Azure DevOps Discovery API](../docs/AZURE_DEVOPS_DISCOVERY_API.md)
- [Local Testing Guide](../docs/LOCAL_TESTING_GUIDE.md)
- [Migration Templates](../CLAIM_TEMPLATES/)

## 🤝 Support

For issues or questions:
1. Check the operator logs
2. Review the [troubleshooting guide](../docs/TROUBLESHOOTING.md)
3. File an issue in the repository
