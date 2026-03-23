# Frequently Asked Questions (FAQ)

Simple answers to common questions about the migration tool.

## General Questions

### What is this tool?

It's a program that automatically moves your code from Azure DevOps to GitHub. Instead of doing it manually (which takes days), this tool does it in hours.

### Do I need to know Kubernetes?

Basic knowledge helps, but not required. You just need to:
- Run a few `kubectl` commands
- Create a YAML file
- Watch it work

### Will it delete my ADO repos?

**No!** Your original ADO repos stay exactly as they are. This tool only copies to GitHub.

### How long does migration take?

**It depends on repo count:**
- 10 repos: ~15 minutes
- 50 repos: ~1 hour
- 100 repos: ~2 hours

Each repo runs in parallel, so more repos doesn't mean much longer.

### Does it cost money?

The tool itself is free. You only pay for:
- Kubernetes cluster (where it runs)
- Network transfer (copying repos)

**Cost example:** Migrating 100 repos on AWS costs about $5-10.

### Can I test it first?

Yes! Start with 2-3 test repos to make sure everything works before migrating all repos.

---

## Setup Questions

### How do I install it?

Three simple steps:
```bash
# 1. Install on Kubernetes
kubectl apply -f config/crd/bases/
kubectl apply -f config/manager/worker-deployment.yaml

# 2. Add your tokens
kubectl create secret generic github-token \
  --from-literal=token=your_token

# 3. Create migration
kubectl apply -f my-migration.yaml
```

### What credentials do I need?

You need:
1. **GitHub Token** - Personal Access Token or GitHub App
2. **ADO Token** - Personal Access Token or Service Principal

### How do I get a GitHub token?

1. Go to GitHub → Settings → Developer Settings → Personal Access Tokens
2. Click "Generate new token"
3. Select scopes: `repo`, `workflow`, `write:org`
4. Copy the token

### How do I get an ADO token?

1. Go to ADO → User Settings → Personal Access Tokens
2. Click "New Token"
3. Select scopes: `Code (Read)`, `Project and Team (Read)`
4. Copy the token

### Do I need a powerful Kubernetes cluster?

**For small migrations (10-20 repos):**
- 2-3 nodes
- 4 CPU, 8GB RAM per node

**For large migrations (100+ repos):**
- 3-5 nodes initially
- Cluster auto-scaler recommended
- Worker pods will scale up automatically

---

## Auto-Discovery Questions

### What is auto-discovery?

Instead of listing every single repo manually, the tool finds them all automatically.

**Without auto-discovery:**
```yaml
resources:
  - sourceName: "repo1"
    targetName: "new-repo1"
  - sourceName: "repo2"
    targetName: "new-repo2"
  # ... 98 more! 😰
```

**With auto-discovery:**
```yaml
discovery:
  repositories:
    enabled: true
    namingConvention:
      template: "prefix-{{.SourceName}}"
# Done! Finds all repos automatically 🎉
```

### How does auto-discovery find repos?

1. Connects to Azure DevOps API
2. Lists all repos in your specified project
3. Applies your naming rules
4. Creates migrations for each repo

### Can I filter which repos to migrate?

Yes! Use patterns:

```yaml
discovery:
  repositories:
    enabled: true
    includePatterns:
    - "java-*"        # Only Java repos
    - "frontend-*"    # And frontend repos
    excludePatterns:
    - "*-archived"    # But skip archived ones
```

### Can I migrate repos from multiple projects?

Yes! Create one MigrationJob per project:

```yaml
# Project 1
apiVersion: migration.ado-to-git-migration.io/v1
kind: MigrationJob
metadata:
  name: project1-migration
spec:
  azureDevOps:
    project: Project-1
  # ...

---
# Project 2
apiVersion: migration.ado-to-git-migration.io/v1
kind: MigrationJob
metadata:
  name: project2-migration
spec:
  azureDevOps:
    project: Project-2
  # ...
```

---

## Naming Questions

### How do I rename repos during migration?

Use naming templates:

```yaml
namingConvention:
  strategy: template
  template: "product-lg-authority-{{.SourceName}}"
```

**Result:**
- ADO: `java-authority` → GitHub: `product-lg-authority-java-authority`
- ADO: `devops-infra` → GitHub: `product-lg-authority-devops-infra`

### What naming strategies are available?

| Strategy | Example | ADO Name | GitHub Name |
|----------|---------|----------|-------------|
| **same** | N/A | `my-repo` | `my-repo` |
| **prefix** | `prefix: "team-"` | `my-repo` | `team-my-repo` |
| **suffix** | `suffix: "-v2"` | `my-repo` | `my-repo-v2` |
| **template** | `{{.Project}}-{{.SourceName}}` | `my-repo` | `platform-my-repo` |

### What variables can I use in templates?

- `{{.SourceName}}` - Original repo name from ADO
- `{{.Project}}` - ADO project name
- `{{.Organization}}` - ADO organization name

**Example:**
```yaml
template: "{{.Organization}}-{{.Project}}-{{.SourceName}}"
```
Result: `my-org-platform-team-my-repo`

### Can I change letter casing?

Yes! Use transforms:

```yaml
namingConvention:
  strategy: template
  template: "prefix-{{.SourceName}}"
  transform: lowercase  # Options: lowercase, uppercase, kebab-case, snake_case
```

### What if target name already exists in GitHub?

The migration will fail for that repo with an error message. You can:
1. Choose a different naming convention
2. Manually delete/rename the existing GitHub repo
3. Use patterns to exclude that repo

---

## Performance Questions

### Why is it faster than manual migration?

**Parallel processing!** Each repo gets its own worker pod.

**Manual way:**
- Do repo 1 (30 min)
- Do repo 2 (30 min)
- ...
- Total: 50 repos × 30 min = 25 hours

**This tool:**
- All 50 repos at once!
- Total: ~1 hour

### How many repos can it handle?

Tested with up to 500 repos. Theoretically unlimited if your cluster can scale.

### How many workers run at once?

Controlled by auto-scaling:
- **Minimum:** 2 pods
- **Maximum:** 100 pods (configurable)
- **Scales based on:** Number of pending repos

### Can I control how many workers run?

Yes! Edit `config/autoscaling/hpa.yaml`:

```yaml
spec:
  minReplicas: 2      # Always have at least 2
  maxReplicas: 50     # Never go above 50
```

### What if a repo is very large (10GB+)?

Increase worker resources in `config/manager/worker-deployment.yaml`:

```yaml
resources:
  requests:
    cpu: 4000m      # 4 CPUs
    memory: 8Gi     # 8 GB RAM
  limits:
    cpu: 8000m      # 8 CPUs
    memory: 16Gi    # 16 GB RAM
```

---

## Migration Questions

### What gets migrated?

**Repositories:**
- Git history
- All branches
- All tags
- LFS files (if enabled)

**Work Items (optional):**
- Tasks, Bugs, Stories → GitHub Issues
- Comments
- Attachments
- Links between items

**Pipelines (optional):**
- ADO Pipelines → GitHub Actions workflows

### Does it migrate git history?

Yes! By default it migrates:
- Last 500 days of history
- Up to 5000 commits

You can change this:
```yaml
settings:
  maxHistoryDays: 1000   # More history
  maxCommitCount: 10000  # More commits
```

### What about pull requests?

**No.** Pull requests are not migrated because:
1. They're tied to specific commits that may not exist yet
2. GitHub doesn't have an API to import historical PRs
3. You want a fresh start in GitHub anyway

**Tip:** Close all PRs in ADO before migrating, or merge them first.

### What about ADO work items?

Work items can be migrated to GitHub Issues:

```yaml
discovery:
  workItems:
    enabled: true
    types: ["User Story", "Bug", "Task"]
```

Each work item becomes a GitHub Issue with:
- Title
- Description
- Comments
- Attachments (if enabled)
- Labels (from work item type)

### Can I test migration without changing anything?

Not yet, but recommended approach:
1. Create a test GitHub organization
2. Migrate 2-3 repos there first
3. Verify everything looks good
4. Then do the real migration

---

## Troubleshooting Questions

### How do I know if migration is working?

Check status:
```bash
# See overall progress
kubectl get migrationjob my-migration -n migration-system

# See discovered repos
kubectl get batchmigrations -n migration-system

# See worker pods
kubectl get pods -n migration-system
```

### What if a migration fails?

**Failed pods stay alive** so you can debug!

```bash
# Find failed migration
kubectl get batchmigrations | grep Failed

# See what went wrong
kubectl describe batchmigration <name> -n migration-system

# Check logs
kubectl logs <pod-name> -n migration-system

# Fix issue and retry
kubectl delete batchmigration <name> -n migration-system
```

### Common error: "Failed to authenticate with ADO"

**Fix:**
1. Check your ADO token is correct
2. Verify token hasn't expired
3. Make sure token has correct permissions: `Code (Read)`

```bash
# Update token
kubectl delete secret ado-token -n migration-system
kubectl create secret generic ado-token \
  --from-literal=token=new_token
```

### Common error: "Failed to authenticate with GitHub"

**Fix:**
1. Check your GitHub token is correct
2. Verify token hasn't expired
3. Make sure token has correct scopes: `repo`, `workflow`

```bash
# Update token
kubectl delete secret github-token -n migration-system
kubectl create secret generic github-token \
  --from-literal=token=new_token
```

### Common error: "Repository already exists"

The target repo name already exists in GitHub.

**Fix:**
1. Change naming convention to avoid conflict
2. Manually delete the existing GitHub repo
3. Use a different target name

### What if no repos are discovered?

**Checks:**
1. Is the ADO project name spelled correctly?
2. Does your token have access to that project?
3. Are there actually repos in that project?

```bash
# Check discovery status
kubectl get migrationjob my-migration -o yaml | grep -A 20 discovery
```

### What if workers don't scale up?

**Checks:**
1. Is HPA installed? `kubectl get hpa -n migration-system`
2. Is metrics-server running? `kubectl get deployment metrics-server -n kube-system`
3. Check HPA status: `kubectl describe hpa worker-hpa -n migration-system`

### How do I cancel a migration?

```bash
# Delete the migration job
kubectl delete migrationjob my-migration -n migration-system

# Workers will stop automatically
```

### Can I pause and resume a migration?

**Pause:**
```bash
kubectl patch migrationjob my-migration -n migration-system \
  --type=merge -p '{"spec":{"paused":true}}'
```

**Resume:**
```bash
kubectl patch migrationjob my-migration -n migration-system \
  --type=merge -p '{"spec":{"paused":false}}'
```

---

## Advanced Questions

### Can I run multiple migrations at once?

Yes! Each MigrationJob is independent:

```bash
kubectl apply -f migration1.yaml
kubectl apply -f migration2.yaml
kubectl apply -f migration3.yaml
```

All will run in parallel.

### What if I want to migrate to GitHub Enterprise?

Change the GitHub API URL:

```yaml
github:
  apiUrl: "https://github.your-company.com/api/v3"
  owner: your-org
  tokenRef:
    name: github-token
    key: token
```

### Can I customize the migration logic?

Yes! The code is open and can be modified:
- **Controller logic:** `internal/controller/migrationjob_controller.go`
- **Worker logic:** `internal/controller/batchmigration_controller.go`
- **API calls:** `internal/services/`

See `ENGINEERING_HANDBOOK.md` for details.

### How do I monitor migrations in production?

The tool exports Prometheus metrics:

```bash
# Install Prometheus
helm install prometheus prometheus-community/prometheus

# Metrics are available at:
# http://<worker-pod>:8080/metrics
```

**Key metrics:**
- `migration_pending_batches` - Repos waiting to migrate
- `migration_processing_batches` - Repos currently migrating
- `migration_completed_total` - Total repos migrated
- `migration_failed_total` - Total failures

### Can I get Slack notifications?

Yes! Add to your migration spec:

```yaml
notifications:
  slack:
    enabled: true
    webhookURL: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"
    notifyOn: ["started", "completed", "failed"]
```

### What about rate limits?

**GitHub:**
- Usually no strict limits

**ADO:**
- Usually no strict limits

The tool automatically handles rate limits with retries and backoff.

### Can I use this in CI/CD?

Yes! You can trigger migrations from CI/CD:

```bash
# In your CI/CD pipeline
kubectl apply -f migration.yaml

# Wait for completion
kubectl wait --for=condition=Complete migrationjob/my-migration \
  --timeout=2h
```

---

## Still Have Questions?

1. **Check the code** - It's well-commented!
2. **Check logs** - `kubectl logs <pod> -n migration-system`
3. **Read the architecture** - See `ARCHITECTURE.md`
4. **Read the handbook** - See `ENGINEERING_HANDBOOK.md`
5. **File an issue** - Create a GitHub issue with details

---

**Most questions can be answered by checking pod logs!** 📝
