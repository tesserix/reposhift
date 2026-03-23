# Multi-Organization PAT Token Support

The ADO-to-Git Migration Operator supports multiple Azure DevOps organizations and projects simultaneously, each with their own PAT tokens.

## Overview

The operator is designed with **zero hardcoding** of secrets. Each migration claim references its own secret, allowing you to:
- Migrate from multiple ADO organizations concurrently
- Use different PAT tokens per organization/project
- Isolate credentials by namespace or team

## Quick Setup

### 1. Create PAT Secrets

Create separate secrets for each ADO organization:

```bash
# Organization 1
kubectl create secret generic org1-ado-pat \
  --from-literal=token='YOUR_ORG1_PAT' \
  -n ado-migration-operator

# Organization 2
kubectl create secret generic org2-ado-pat \
  --from-literal=token='YOUR_ORG2_PAT' \
  -n ado-migration-operator
```

### 2. Create Claims Referencing Different Secrets

**Claim for Organization 1:**
```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: org1-migration
spec:
  source:
    organization: organization-1
    project: ProjectA
    auth:
      pat:
        tokenRef:
          name: org1-ado-pat    # References org1 secret
          key: token
```

**Claim for Organization 2:**
```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: org2-migration
spec:
  source:
    organization: organization-2
    project: ProjectB
    auth:
      pat:
        tokenRef:
          name: org2-ado-pat    # References org2 secret
          key: token
```

### 3. Apply Claims

```bash
kubectl apply -f org1-migration.yaml
kubectl apply -f org2-migration.yaml
```

Each migration runs independently with its own PAT token.

## Flexible Configuration

The `tokenRef` supports:
- **name**: Secret name (configurable per claim)
- **key**: Key within secret (default: `token`)
- **namespace**: Secret namespace (defaults to claim namespace)

Example with custom key and namespace:
```yaml
auth:
  pat:
    tokenRef:
      name: custom-credentials
      key: ado-token              # Custom key name
      namespace: shared-secrets   # Different namespace
```

## Supported Resources

Multi-PAT support works with all migration types:
- `AdoToGitMigration` - Repository and work item migrations
- `PipelineToWorkflow` - Pipeline conversions with auto-discovery
- `WorkItemMigration` - Standalone work item migrations

## Verification

Check that the operator is using the correct secrets:

```bash
kubectl logs -n ado-migration-operator \
  deployment/ado-to-git-migration-controller-manager | grep "secret"
```

Each claim will fetch its own referenced secret independently.

## Example: Multiple Organizations Setup

### Step 1: Create Multiple PAT Secrets

```bash
# PAT for Civica International
kubectl create secret generic civica-international-pat \
  --from-literal=token='YOUR_CIVICA_INTERNATIONAL_PAT' \
  -n ado-migration-operator

# PAT for Another Organization
kubectl create secret generic another-org-pat \
  --from-literal=token='YOUR_ANOTHER_ORG_PAT' \
  -n ado-migration-operator

# PAT for Third Organization with different key name
kubectl create secret generic third-org-credentials \
  --from-literal=ado-token='YOUR_THIRD_ORG_PAT' \
  -n ado-migration-operator
```

### Step 2: Create Claims Referencing Different Secrets

**Claim 1: civica-international-migration.yaml**

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: civica-international-migration
  namespace: ado-migration-operator
spec:
  source:
    organization: civica-international-lg
    project: Authority
    auth:
      pat:
        tokenRef:
          name: civica-international-pat  # ← References first secret
          key: token
          namespace: ado-migration-operator
  target:
    organization: civica
    auth:
      appAuth:
        # ... GitHub App config
  resources:
    - type: repository
      sourceId: "abc123"
      sourceName: authority-repos
      targetName: authority-repos
```

**Claim 2: another-org-migration.yaml**

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: another-org-migration
  namespace: ado-migration-operator
spec:
  source:
    organization: another-org          # ← Different organization
    project: SomeProject               # ← Different project
    auth:
      pat:
        tokenRef:
          name: another-org-pat        # ← References second secret
          key: token
          namespace: ado-migration-operator
  target:
    organization: another-github-org   # ← Different GitHub org
    auth:
      appAuth:
        # ... GitHub App config for different org
  resources:
    - type: repository
      sourceId: "xyz789"
      sourceName: some-repo
      targetName: some-repo
```

**Claim 3: third-org-pipeline-conversion.yaml**

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: PipelineToWorkflow
metadata:
  name: third-org-pipelines
  namespace: ado-migration-operator
spec:
  source:
    organization: third-org            # ← Third organization
    project: DevOps
    auth:
      pat:
        tokenRef:
          name: third-org-credentials  # ← Different secret name
          key: ado-token               # ← Different key within secret
          namespace: ado-migration-operator
  target:
    organization: third-github-org
    repository: third-repo
    auth:
      tokenRef:
        name: third-github-pat
        key: token
  autoDiscovery:
    enabled: true
```

### Step 3: Apply All Claims Simultaneously

```bash
kubectl apply -f civica-international-migration.yaml
kubectl apply -f another-org-migration.yaml
kubectl apply -f third-org-pipeline-conversion.yaml
```

Each claim will use its own PAT token secret completely independently.

## Key Benefits

1. **Zero Hardcoding**: Every secret reference is specified in the claim YAML
2. **Multi-Tenant Ready**: Different teams can use different ADO organizations with their own PAT tokens
3. **Namespace Isolation**: You can even use different namespaces for secrets if needed
4. **Flexible Key Names**: The key field allows different key names within secrets
5. **Mixed Auth Methods**: You can use PAT for some claims and Service Principal for others