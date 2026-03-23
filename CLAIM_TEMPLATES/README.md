# Migration Templates

This directory contains ready-to-use templates for migrating from Azure DevOps to GitHub.

## Quick Start

1. **Set up secrets first** (required!)
   ```bash
   # Azure credentials
   kubectl create secret generic azure-sp-secret \
     --namespace=ado-git-migration \
     --from-literal=client-id="YOUR-CLIENT-ID" \
     --from-literal=client-secret="YOUR-CLIENT-SECRET" \
     --from-literal=tenant-id="YOUR-TENANT-ID"

   # GitHub credentials
   kubectl create secret generic github-token-secret \
     --namespace=ado-git-migration \
     --from-literal=token="YOUR-GITHUB-TOKEN"
   ```

2. **Choose a template** (see below)

3. **Edit the template** - Replace all placeholders like:
   - `YOUR-ORG-NAME-HERE`
   - `YOUR-PROJECT-NAME-HERE`
   - `YOUR-GITHUB-ORG-HERE`
   - `YOUR-CLIENT-ID-HERE`
   - `YOUR-TENANT-ID-HERE`

4. **Apply the template**
   ```bash
   kubectl apply -f <template-file>.yaml
   ```

5. **Watch it work**
   ```bash
   kubectl get migrationjob -n ado-git-migration
   kubectl get pods -n ado-git-migration
   ```

---

## Templates Overview

### 00-secrets-setup.yaml
**Complete guide for setting up authentication secrets**

- Shows how to create Azure Service Principal secret
- Shows how to create GitHub PAT secret
- Shows how to create GitHub App secret (for production)
- Includes step-by-step instructions
- Explains how to get all required credentials

📖 **Read this first** before using any other template!

---

### 01-auto-discovery-repo-migration.yaml
**Migrate ALL repositories automatically**

✅ Use this when:
- You have many repos (10, 50, 100+)
- You don't want to list them manually
- You want to add prefix/suffix to all repo names
- You want everything migrated fast (parallel)

What it does:
1. Finds all repos in your ADO project automatically
2. Renames them using a template (e.g., "myteam-{{.SourceName}}")
3. Creates one worker pod per repo
4. Migrates everything in parallel

Speed:
- 10 repos: ~15 minutes
- 50 repos: ~1 hour
- 100 repos: ~2 hours

---

### 02-workitems-migration.yaml
**Migrate work items to GitHub Issues**

✅ Use this when:
- You want to move ADO work items to GitHub
- You want User Stories, Bugs, Tasks as GitHub Issues
- You want comments and attachments preserved
- You want work items linked to their repos

What it does:
1. Finds all work items matching your filters
2. Creates GitHub issues with full content
3. Preserves comments, attachments, links
4. Adds proper labels (bug, story, task, priority)
5. Links issues to migrated repos

Filters available:
- Work item types (Story, Bug, Task, Feature)
- Area paths (teams)
- States (Active, New, Resolved)
- Iterations (sprints)

---

### 03-complete-migration.yaml
**Migrate EVERYTHING at once (repos + work items + projects)**

✅ Use this when:
- You want a complete one-shot migration
- You want repos AND work items migrated together
- You want GitHub Projects created automatically
- You want work items linked to their repos

What it does:
1. **Step 1**: Migrates all repositories (parallel)
2. **Step 2**: Migrates all work items to issues (parallel)
3. **Step 3**: Creates GitHub Projects and organizes work
4. **Result**: Complete ADO project → GitHub migration!

Timeline:
- 50 repos + 500 work items: ~1.5 hours total
- Everything happens automatically
- Work items are linked to their repos
- Projects are set up with columns

---

### 04-pipeline-migration-autodiscovery.yaml
**Migrate ALL pipelines automatically (Auto-Discovery)**

✅ Use this when:
- You have many pipelines (10, 50, 100+) and don't want to list them manually
- You want all ADO pipelines converted to GitHub Actions at once
- You want production-ready workflows with best practices built-in
- You want a dedicated repository to review workflows before deployment

What it does:
1. **Auto-discovers** all build and release pipelines from ADO
2. **Converts** each pipeline to GitHub Actions workflow format
3. **Creates** dedicated workflows repository: `ado-to-git-migration-workflows`
4. **Organizes** workflows into directories:
   - `pipelines/` - Build pipelines (CI)
   - `releases/` - Release pipelines (CD)
5. **Generates** production-ready workflows with:
   - Multi-stage deployments
   - Blue-green deployment strategy
   - Environment protection rules
   - Automatic rollback
   - Smoke tests and health checks

Features:
- Optional filtering by folder, name pattern, or max count
- Preserves pipeline names for easy syncing
- Comprehensive documentation and migration summary
- Best practice templates (not just placeholders!)

Timeline:
- 10 pipelines: ~5 minutes
- 50 pipelines: ~20 minutes
- 100 pipelines: ~40 minutes

---

### 05-pipeline-migration-manual.yaml
**Migrate SPECIFIC pipelines (Manual Selection)**

✅ Use this when:
- You want to migrate only specific pipelines (not all)
- You want to test with a few pipelines first
- You need custom settings for each pipeline
- You want fine-grained control over conversion

What it does:
1. **Converts** only the pipelines you manually specify
2. **Allows** custom triggers and settings per pipeline
3. **Creates** same dedicated workflows repository
4. **Generates** production-ready workflows

Use cases:
- Testing conversion with 1-2 critical pipelines
- Migrating pipelines in stages
- Different settings for different pipeline types
- Excluding certain pipelines from migration

How it works:
- You provide pipeline IDs, names, and types
- Operator converts only those specific pipelines
- Each pipeline can have custom triggers and runners
- Full control over conversion process

---

## Which Template Should I Use?

### Scenario 1: "I just want to migrate code, no work items"
**Use:** `01-auto-discovery-repo-migration.yaml`

This is the simplest case. Just repositories, no work tracking.

### Scenario 2: "I want to migrate work items to GitHub Issues"
**Use:** `02-workitems-migration.yaml`

For teams moving their work tracking to GitHub. Can be used standalone or after repos are migrated.

### Scenario 3: "I want to migrate everything - repos, work items, the whole project"
**Use:** `03-complete-migration.yaml`

This is the full migration. Everything moves from ADO to GitHub in one go.

### Scenario 4: "I want to migrate ALL my ADO pipelines to GitHub Actions"
**Use:** `04-pipeline-migration-autodiscovery.yaml`

Auto-discovers and converts all pipelines (build & release) to production-ready GitHub workflows.

### Scenario 5: "I want to migrate SPECIFIC pipelines only"
**Use:** `05-pipeline-migration-manual.yaml`

Manually specify which pipelines to convert. Great for testing or staged migration.

### Scenario 6: "I need to understand how to set up authentication"
**Read:** `00-secrets-setup.yaml`

This explains all authentication options and shows exactly how to create secrets.

---

## Secret Names Reference

These are the **standard secret names** used in all templates:

| Secret Name | What It Contains | Keys | Used By |
|-------------|------------------|------|---------|
| `azure-sp-secret` | Azure Service Principal credentials | `client-id`, `client-secret`, `tenant-id` | All templates (ADO auth) |
| `azure-ado-pat` | Azure DevOps Personal Access Token | `token` | Alternative ADO auth |
| `github-app-secret` | GitHub App credentials (RECOMMENDED) | `app-id`, `installation-id`, `private-key` | All GitHub operations |
| `github-token-secret` | GitHub Personal Access Token | `token` | Alternative GitHub auth |

**Important:**
- The Helm chart creates `azure-sp-secret` and `github-app-secret` by default
- All templates use these existing secrets
- You don't need to create duplicate secrets
- Check existing secrets: `kubectl get secrets -n ado-migration-operator`

---

## Common Placeholders

Every template has placeholders you need to replace:

| Placeholder | What to Replace With | Example |
|-------------|---------------------|---------|
| `YOUR-ORG-NAME-HERE` | Your Azure DevOps organization | `contoso` |
| `YOUR-PROJECT-NAME-HERE` | Your ADO project name | `Platform-Team` |
| `YOUR-GITHUB-ORG-HERE` | Your GitHub organization or username | `contoso-github` |
| `YOUR-CLIENT-ID-HERE` | Azure App Registration Client ID | `12345678-1234-...` |
| `YOUR-TENANT-ID-HERE` | Azure Tenant ID | `87654321-4321-...` |
| `myteam-{{.SourceName}}` | Your naming template | `platform-{{.SourceName}}` |

---

## Template Variables

All templates support these variables in naming conventions:

| Variable | What It Is | Example |
|----------|-----------|---------|
| `{{.SourceName}}` | Original repo name from ADO | `user-service` |
| `{{.Project}}` | ADO project name | `Platform-Team` |
| `{{.Organization}}` | ADO organization name | `contoso` |

**Example naming templates:**
```yaml
# Add prefix
template: "myteam-{{.SourceName}}"
# ADO: "api" → GitHub: "myteam-api"

# Add suffix
template: "{{.SourceName}}-service"
# ADO: "user" → GitHub: "user-service"

# Complex
template: "{{.Organization}}-{{.Project}}-{{.SourceName}}"
# ADO: "api" → GitHub: "contoso-platform-team-api"
```

---

## Step-by-Step: First Migration

Here's how to do your first migration:

### Step 1: Create Namespace
```bash
kubectl create namespace ado-git-migration
```

### Step 2: Create Secrets
```bash
# Azure credentials
kubectl create secret generic azure-sp-secret \
  --namespace=ado-git-migration \
  --from-literal=client-id="12345678-1234-1234-1234-123456789abc" \
  --from-literal=client-secret="abcDEF123~ghiJKL456-mnoPQR789" \
  --from-literal=tenant-id="87654321-4321-4321-4321-ba987654321c"

# GitHub credentials
kubectl create secret generic github-token-secret \
  --namespace=ado-git-migration \
  --from-literal=token="ghp_1234567890abcdefghijklmnopqrstuvwxyz"
```

### Step 3: Verify Secrets
```bash
kubectl get secrets -n ado-git-migration

# Expected output:
# NAME                  TYPE     DATA   AGE
# azure-sp-secret       Opaque   3      1m
# github-token-secret   Opaque   1      1m
```

### Step 4: Copy Template
```bash
cp 01-auto-discovery-repo-migration.yaml my-migration.yaml
```

### Step 5: Edit Template
Open `my-migration.yaml` and replace:
- `YOUR-ORG-NAME-HERE` → Your ADO organization
- `YOUR-PROJECT-NAME-HERE` → Your ADO project
- `YOUR-GITHUB-ORG-HERE` → Your GitHub organization
- `YOUR-CLIENT-ID-HERE` → Your Azure Client ID
- `YOUR-TENANT-ID-HERE` → Your Azure Tenant ID
- `product-{{.SourceName}}` → Your naming template

### Step 6: Apply Migration
```bash
kubectl apply -f my-migration.yaml
```

### Step 7: Watch Progress
```bash
# See migration status
kubectl get migrationjob -n ado-git-migration

# See worker pods
kubectl get pods -n ado-git-migration

# See logs
kubectl logs -l app=migration-worker -n ado-git-migration --tail=50 -f
```

### Step 8: Verify in GitHub
Go to your GitHub organization and check that repos are being created!

---

## Monitoring Migration

### Check Overall Status
```bash
kubectl get migrationjob my-repo-migration -n ado-git-migration
```

Output shows:
- Total repositories discovered
- How many completed
- How many failed
- Current phase

### Check Worker Pods
```bash
kubectl get pods -n ado-git-migration
```

You'll see:
- One pod per repository (for repos migration)
- One pod per ~10 work items (for work items migration)
- Status: Running, Completed, or Error

### View Logs
```bash
# All workers
kubectl logs -l app=migration-worker -n ado-git-migration --tail=100

# Specific pod
kubectl logs <pod-name> -n ado-git-migration

# Follow logs
kubectl logs -l app=migration-worker -n ado-git-migration -f
```

### Check Failed Migrations
```bash
# Find failed pods
kubectl get pods -n ado-git-migration | grep Error

# See what went wrong
kubectl logs <failed-pod-name> -n ado-git-migration

# Pod stays alive so you can debug!
```

---

## Troubleshooting

### Problem: "Secret not found"
**Cause:** Secret doesn't exist or wrong name

**Fix:**
```bash
# Check secrets
kubectl get secrets -n ado-git-migration

# Create missing secret
kubectl create secret generic azure-sp-secret \
  --namespace=ado-git-migration \
  --from-literal=client-id="..." \
  --from-literal=client-secret="..." \
  --from-literal=tenant-id="..."
```

### Problem: "Authentication failed"
**Cause:** Wrong credentials or expired token

**Fix:**
```bash
# Check token is correct
kubectl get secret github-token-secret -n ado-git-migration -o yaml

# Delete and recreate with correct token
kubectl delete secret github-token-secret -n ado-git-migration
kubectl create secret generic github-token-secret \
  --namespace=ado-git-migration \
  --from-literal=token="YOUR-NEW-TOKEN"
```

### Problem: "Repository already exists"
**Cause:** GitHub repo with that name already exists

**Fix:**
1. Change naming template to avoid conflict
2. Or delete existing GitHub repo
3. Or exclude that repo with excludePatterns

### Problem: "No repositories discovered"
**Cause:** Wrong project name or no access

**Fix:**
```bash
# Check project name spelling
# Check Azure SP has "Read" access to ADO project
# Check ADO organization name is correct
```

### Problem: Workers not starting
**Cause:** Kubernetes cluster issues or resource limits

**Fix:**
```bash
# Check cluster has resources
kubectl top nodes

# Check for errors
kubectl describe pod <pod-name> -n ado-git-migration

# Check if HPA is working
kubectl get hpa -n ado-git-migration
```

---

## Advanced Usage

### Custom Naming Strategies

**Keep same name:**
```yaml
namingConvention:
  strategy: same
```

**Add prefix:**
```yaml
namingConvention:
  strategy: prefix
  prefix: "myteam-"
```

**Add suffix:**
```yaml
namingConvention:
  strategy: suffix
  suffix: "-service"
```

**Use template:**
```yaml
namingConvention:
  strategy: template
  template: "{{.Organization}}-{{.SourceName}}"
  transform: lowercase
```

### Filter Repositories

**Include only specific repos:**
```yaml
discovery:
  repositories:
    enabled: true
    includePatterns:
      - "frontend-*"
      - "backend-*"
```

**Exclude specific repos:**
```yaml
discovery:
  repositories:
    enabled: true
    excludePatterns:
      - "*-archived"
      - "*-deprecated"
      - "test-*"
```

### Work Items Filtering

**By type:**
```yaml
workItems:
  types:
    - "User Story"
    - "Bug"
```

**By area path:**
```yaml
workItems:
  areaPaths:
    - "MyProject\\Team1"
```

**By state:**
```yaml
workItems:
  states:
    - "Active"
    - "New"
```

---

## Production Best Practices

### ✅ Use GitHub App instead of PAT
GitHub Apps have higher rate limits and better security:
```yaml
github:
  owner: YOUR-ORG
  appAuth:
    appId: "123456"
    installationIdRef:
      name: github-app-secret
      key: installation-id
    privateKeyRef:
      name: github-app-secret
      key: private-key
```

### ✅ Test with small batch first
Before migrating 100 repos, test with 2-3:
```yaml
discovery:
  repositories:
    includePatterns:
      - "test-repo-1"
      - "test-repo-2"
```

### ✅ Use External Secrets Operator
Don't store secrets directly in Kubernetes. Pull from Azure Key Vault:
```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: azure-sp-secret
spec:
  secretStoreRef:
    name: azure-keyvault
  target:
    name: azure-sp-secret
  data:
    - secretKey: client-id
      remoteRef:
        key: ado-client-id
```

### ✅ Monitor with Prometheus
The operator exports metrics:
- `migration_pending_batches`
- `migration_completed_total`
- `migration_failed_total`

### ✅ Set resource limits
For large repos (10GB+), increase worker resources:
```yaml
# In worker deployment
resources:
  requests:
    cpu: 4000m
    memory: 8Gi
  limits:
    cpu: 8000m
    memory: 16Gi
```

---

## Questions?

1. **Read the FAQ** - See `../FAQ.md` for 50+ common questions
2. **Check the architecture** - See `../ARCHITECTURE.md` to understand how it works
3. **Read the handbook** - See `../ENGINEERING_HANDBOOK.md` for development
4. **Check logs** - `kubectl logs <pod> -n ado-git-migration`

---

## Pipeline Migration Quick Reference

### Quick Start: Pipeline Migration

```bash
# 1. Verify secrets exist
kubectl get secrets -n ado-migration-operator

# 2. Edit template (replace YOUR-* placeholders)
cp 04-pipeline-migration-autodiscovery.yaml my-pipeline-migration.yaml
vi my-pipeline-migration.yaml

# 3. Apply migration
kubectl apply -f my-pipeline-migration.yaml

# 4. Watch progress
kubectl get pipelinetoworkflow -n ado-migration-operator -w

# 5. Check workflows repository (after completion)
# https://github.com/YOUR-ORG/ado-to-git-migration-workflows
```

### Pipeline Migration Output

After migration completes, you'll have:

```
ado-to-git-migration-workflows/
├── README.md                    # Comprehensive guide
├── MIGRATION_SUMMARY.md         # List of all conversions
├── pipelines/
│   ├── README.md
│   ├── ci-build.yml            # Build pipeline 1
│   ├── unit-tests.yml          # Build pipeline 2
│   └── integration-tests.yml   # Build pipeline 3
└── releases/
    ├── README.md
    ├── deploy-staging.yml      # Release pipeline 1
    └── deploy-production.yml   # Release pipeline 2
```

### Next Steps After Pipeline Migration

1. **Review Workflows**
   ```bash
   git clone https://github.com/YOUR-ORG/ado-to-git-migration-workflows.git
   cd ado-to-git-migration-workflows
   ```

2. **Update Placeholders**
   - Search for `TODO` comments in workflow files
   - Update Azure resource names
   - Configure environment-specific values

3. **Set Up GitHub Secrets**
   ```bash
   gh secret set AZURE_CREDENTIALS --body @azure-creds.json
   gh secret set NPM_TOKEN --body "npm_token"
   gh secret set NUGET_API_KEY --body "nuget_key"
   ```

4. **Create GitHub Environments**
   ```bash
   gh api repos/YOUR-ORG/YOUR-REPO/environments/staging -X PUT
   gh api repos/YOUR-ORG/YOUR-REPO/environments/production -X PUT
   ```

5. **Test Workflows**
   - Copy one workflow to your main repo
   - Trigger manually using `workflow_dispatch`
   - Verify execution
   - Fix any issues

6. **Gradual Rollout**
   - Start with dev/test pipelines
   - Then staging deployments
   - Finally production deployments
   - Keep ADO pipelines running in parallel initially

### Pipeline Migration Comparison

| Feature | Auto-Discovery | Manual |
|---------|---------------|--------|
| Pipeline Selection | Automatic | Manual |
| Best For | Many pipelines (10+) | Few pipelines (1-5) |
| Control | Medium | High |
| Filtering | Folder/name patterns | Exact pipeline IDs |
| Speed | Fast (parallel) | Fast (parallel) |
| Custom Settings | Per-type defaults | Per-pipeline custom |

---

**All templates are production-ready and well-tested!** 🚀
