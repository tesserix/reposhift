# Engineering Handbook

How to develop, test, and maintain this migration tool. Written in simple English for developers.

## Table of Contents
- [Setup Development Environment](#setup-development-environment)
- [Code Structure](#code-structure)
- [Making Changes](#making-changes)
- [Testing](#testing)
- [Debugging](#debugging)
- [Common Tasks](#common-tasks)
- [Best Practices](#best-practices)

---

## Setup Development Environment

### Prerequisites

You need:
- Go 1.21+ installed
- Docker installed
- Kubernetes cluster (kind, minikube, or remote)
- kubectl configured
- Basic understanding of Kubernetes

### First Time Setup

```bash
# 1. Clone the repo
git clone <repo-url>
cd ado-to-git-migration

# 2. Install dependencies
go mod download

# 3. Install kubebuilder tools
make install-tools

# 4. Generate code
make manifests generate

# 5. Install CRDs
make install
```

### Development Tools

```bash
# Build the binary
make build

# Run tests
make test

# Run locally (outside cluster)
make run

# Build Docker image
make docker-build

# Deploy to cluster
make deploy
```

---

## Code Structure

### Overview

```
Project Layout:
/
├── api/v1/              ← Define what users can create (CRDs)
├── internal/
│   ├── controller/      ← The brain (business logic)
│   └── services/        ← Talk to external APIs (ADO, GitHub)
├── config/              ← Kubernetes manifests
├── CLAIM_TEMPLATES/     ← Migration templates for users
└── docs/                ← Documentation (you are here!)
```

### Where to Find Things

| I want to... | Look in... |
|-------------|-----------|
| Add a new field to MigrationJob | `api/v1/migrationjob_types.go` |
| Change how discovery works | `internal/controller/migrationjob_controller.go` |
| Change how repos are migrated | `internal/controller/batchmigration_controller.go` |
| Fix ADO API calls | `internal/services/ado_service.go` |
| Fix GitHub API calls | `internal/services/github_service.go` |
| Change auto-scaling rules | `config/autoscaling/hpa.yaml` |
| Change worker resources | `config/manager/worker-deployment.yaml` |

---

## Making Changes

### Adding a New Field to CRD

**Example:** Add a `timeout` field to MigrationJob

**Step 1: Update the type definition**
```go
// File: api/v1/migrationjob_types.go

type MigrationJobSpec struct {
    // ... existing fields ...

    // Add new field
    // +optional
    Timeout int `json:"timeout,omitempty"` // in minutes
}
```

**Step 2: Regenerate CRD manifests**
```bash
make manifests
```

**Step 3: Update the CRD in cluster**
```bash
make install
```

**Step 4: Use the new field in controller**
```go
// File: internal/controller/migrationjob_controller.go

func (r *MigrationJobReconciler) Reconcile(ctx, req) {
    job := &MigrationJob{}
    r.Get(ctx, req.NamespacedName, job)

    // Use the new field
    timeout := job.Spec.Timeout
    if timeout == 0 {
        timeout = 60 // default
    }

    // ... rest of logic
}
```

### Adding a New Controller Function

**Example:** Add validation before migration

**Step 1: Add the function**
```go
// File: internal/controller/migrationjob_controller.go

func (r *MigrationJobReconciler) validateBeforeMigration(ctx, job) error {
    // Check if ADO project exists
    exists, err := r.ADOService.CheckProjectExists(job.Spec.AzureDevOps.Project)
    if err != nil {
        return fmt.Errorf("failed to check project: %w", err)
    }
    if !exists {
        return fmt.Errorf("project %s does not exist", job.Spec.AzureDevOps.Project)
    }

    // Check if GitHub org exists
    exists, err = r.GitHubService.CheckOrgExists(job.Spec.GitHub.Owner)
    if err != nil {
        return fmt.Errorf("failed to check org: %w", err)
    }
    if !exists {
        return fmt.Errorf("org %s does not exist", job.Spec.GitHub.Owner)
    }

    return nil
}
```

**Step 2: Call it from Reconcile**
```go
func (r *MigrationJobReconciler) Reconcile(ctx, req) {
    job := &MigrationJob{}
    r.Get(ctx, req.NamespacedName, job)

    // Add validation
    if job.Status.Phase == "" {
        if err := r.validateBeforeMigration(ctx, job); err != nil {
            return r.failJob(ctx, job, err.Error())
        }
    }

    // ... rest of logic
}
```

### Adding a New Service Function

**Example:** Add function to check if repo exists in ADO

**Step 1: Add to service**
```go
// File: internal/services/ado_service.go

func (s *AzureDevOpsService) CheckRepositoryExists(
    ctx context.Context,
    org, project, repoName string,
) (bool, error) {
    // Build URL
    url := fmt.Sprintf(
        "https://dev.azure.com/%s/%s/_apis/git/repositories/%s?api-version=6.0",
        org, project, repoName,
    )

    // Make request
    resp, err := s.client.Get(url)
    if err != nil {
        return false, err
    }
    defer resp.Body.Close()

    // Check status
    if resp.StatusCode == 404 {
        return false, nil // doesn't exist
    }
    if resp.StatusCode != 200 {
        return false, fmt.Errorf("unexpected status: %d", resp.StatusCode)
    }

    return true, nil // exists!
}
```

**Step 2: Use it in controller**
```go
// File: internal/controller/migrationjob_controller.go

func (r *MigrationJobReconciler) discoverRepositories(ctx, job) {
    // ... discovery logic ...

    for _, repo := range repos {
        // Check if repo exists
        exists, err := r.ADOService.CheckRepositoryExists(
            job.Spec.AzureDevOps.Organization,
            job.Spec.AzureDevOps.Project,
            repo.Name,
        )
        if !exists {
            log.Info("Skipping non-existent repo", "name", repo.Name)
            continue
        }

        // ... rest of logic
    }
}
```

---

## Testing

### Unit Tests

**Location:** `*_test.go` files next to the code

**Example:** Test naming convention logic

```go
// File: internal/controller/naming_test.go

package controller

import "testing"

func TestApplyNamingConvention(t *testing.T) {
    tests := []struct {
        name       string
        sourceName string
        convention NamingConvention
        want       string
    }{
        {
            name:       "prefix strategy",
            sourceName: "my-repo",
            convention: NamingConvention{
                Strategy: "prefix",
                Prefix:   "team-",
            },
            want: "team-my-repo",
        },
        {
            name:       "template strategy",
            sourceName: "my-repo",
            convention: NamingConvention{
                Strategy: "template",
                Template: "{{.Project}}-{{.SourceName}}",
            },
            want: "platform-my-repo",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := applyNamingConvention(tt.sourceName, tt.convention)
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

**Run tests:**
```bash
# All tests
make test

# Specific package
go test ./internal/controller/...

# With coverage
go test -cover ./...

# Verbose
go test -v ./...
```

### Integration Tests

**Purpose:** Test the whole flow end-to-end

**Example test:**
```go
// File: test/e2e/migration_test.go

func TestFullMigration(t *testing.T) {
    // 1. Create MigrationJob
    job := &migrationv1.MigrationJob{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-migration",
            Namespace: "default",
        },
        Spec: migrationv1.MigrationJobSpec{
            AzureDevOps: migrationv1.AzureDevOpsConfig{
                Organization: "test-org",
                Project:      "test-project",
            },
            // ... rest of spec
        },
    }
    err := k8sClient.Create(ctx, job)
    require.NoError(t, err)

    // 2. Wait for batches to be created
    time.Sleep(10 * time.Second)
    batches := &migrationv1.BatchMigrationList{}
    err = k8sClient.List(ctx, batches)
    require.NoError(t, err)
    assert.Greater(t, len(batches.Items), 0)

    // 3. Verify status updated
    err = k8sClient.Get(ctx, client.ObjectKeyFromObject(job), job)
    require.NoError(t, err)
    assert.Equal(t, "Running", string(job.Status.Phase))
}
```

**Run integration tests:**
```bash
# Requires a Kubernetes cluster
make test-e2e
```

### Manual Testing

**Quick local test:**

```bash
# 1. Start controller locally
make run

# 2. In another terminal, create test migration
kubectl apply -f CLAIM_TEMPLATES/01-auto-discovery-repo-migration.yaml

# 3. Watch logs
# You'll see output in the terminal running `make run`

# 4. Check status
kubectl get migrationjob -o yaml
kubectl get batchmigrations
kubectl get pods
```

---

## Debugging

### Debugging Controller Logic

**Method 1: Add log statements**
```go
// File: internal/controller/migrationjob_controller.go

func (r *MigrationJobReconciler) Reconcile(ctx, req) {
    log := log.FromContext(ctx)
    log.Info("Starting reconcile", "name", req.Name)

    // Your code here
    repos := discoverRepositories(...)
    log.Info("Discovered repos", "count", len(repos))

    for _, repo := range repos {
        log.Info("Processing repo", "name", repo.Name)
        // ... process
    }

    log.Info("Reconcile complete")
    return ctrl.Result{}, nil
}
```

**View logs:**
```bash
# If running locally
# Logs appear in terminal

# If running in cluster
kubectl logs deployment/ado-to-git-migration-controller -n migration-system
```

**Method 2: Use debugger (Delve)**
```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Run with debugger
dlv debug ./cmd/main.go -- --zap-log-level=debug

# Set breakpoints in code
(dlv) break internal/controller/migrationjob_controller.go:150
(dlv) continue
```

### Debugging Worker Issues

**Check worker logs:**
```bash
# Find worker pods
kubectl get pods -n migration-system -l component=worker

# View logs
kubectl logs <worker-pod-name> -n migration-system

# Follow logs
kubectl logs -f <worker-pod-name> -n migration-system

# Previous logs (if pod restarted)
kubectl logs <worker-pod-name> -n migration-system --previous
```

**Exec into worker pod:**
```bash
# Get shell access
kubectl exec -it <worker-pod-name> -n migration-system -- sh

# Check files
ls -la /tmp/
cat /tmp/migration.log

# Check git repo (if debugging git issues)
cd /tmp/cloned-repo
git status
git log
```

### Common Issues

#### Issue: "CRD not found"
**Symptom:** Error creating MigrationJob

**Fix:**
```bash
# Install CRDs
make install

# Verify installed
kubectl get crd migrationjobs.migration.ado-to-git-migration.io
```

#### Issue: "Controller not reconciling"
**Symptom:** MigrationJob stays in same state

**Debug:**
```bash
# Check controller logs
kubectl logs deployment/ado-to-git-migration-controller -n migration-system

# Check controller is running
kubectl get pods -n migration-system

# Describe the MigrationJob
kubectl describe migrationjob <name> -n migration-system
```

#### Issue: "Workers not scaling up"
**Symptom:** Only 2 workers despite 50 pending batches

**Debug:**
```bash
# Check HPA exists
kubectl get hpa -n migration-system

# Check HPA status
kubectl describe hpa ado-to-git-migration-worker-hpa -n migration-system

# Check metrics-server
kubectl get deployment metrics-server -n kube-system
```

---

## Common Tasks

### Task 1: Add New Naming Strategy

**Goal:** Add "uppercase" strategy that converts names to uppercase

**Steps:**

**1. Update discovery types:**
```go
// File: api/v1/discovery_config_types.go

type NamingConvention struct {
    // +kubebuilder:validation:Enum=same;prefix;suffix;template;uppercase
    Strategy string `json:"strategy"`
    // ... rest
}
```

**2. Implement strategy:**
```go
// File: internal/controller/naming.go

func applyNamingConvention(sourceName, convention) string {
    switch convention.Strategy {
    case "uppercase":
        return strings.ToUpper(sourceName)
    case "prefix":
        return convention.Prefix + sourceName
    // ... other cases
    default:
        return sourceName
    }
}
```

**3. Add test:**
```go
// File: internal/controller/naming_test.go

func TestApplyNamingConvention(t *testing.T) {
    // ... existing tests ...

    {
        name:       "uppercase strategy",
        sourceName: "my-repo",
        convention: NamingConvention{
            Strategy: "uppercase",
        },
        want: "MY-REPO",
    },
}
```

**4. Update manifests and deploy:**
```bash
make manifests generate
make docker-build docker-push
kubectl rollout restart deployment/ado-to-git-migration-controller -n migration-system
```

### Task 2: Add Progress Callback

**Goal:** Send progress updates to external webhook

**Steps:**

**1. Add webhook config to CRD:**
```go
// File: api/v1/migrationjob_types.go

type MigrationJobSpec struct {
    // ... existing fields ...

    // +optional
    Webhook *WebhookConfig `json:"webhook,omitempty"`
}

type WebhookConfig struct {
    URL     string `json:"url"`
    Headers map[string]string `json:"headers,omitempty"`
}
```

**2. Add webhook caller:**
```go
// File: internal/services/webhook_service.go

type WebhookService struct {
    client *http.Client
}

func (s *WebhookService) SendProgress(ctx, url string, progress ProgressUpdate) error {
    body, _ := json.Marshal(progress)
    req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := s.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return fmt.Errorf("webhook failed: %d", resp.StatusCode)
    }

    return nil
}
```

**3. Call from controller:**
```go
// File: internal/controller/migrationjob_controller.go

func (r *MigrationJobReconciler) updateProgress(ctx, job) {
    // Update status
    job.Status.Progress.Percentage = calculatePercentage(job)
    r.Status().Update(ctx, job)

    // Send to webhook if configured
    if job.Spec.Webhook != nil {
        r.WebhookService.SendProgress(
            ctx,
            job.Spec.Webhook.URL,
            ProgressUpdate{
                JobName:    job.Name,
                Percentage: job.Status.Progress.Percentage,
                Phase:      job.Status.Phase,
            },
        )
    }
}
```

### Task 3: Change Worker Resources

**Goal:** Give workers more memory for large repos

**Edit file:**
```yaml
# File: config/manager/worker-deployment.yaml

spec:
  template:
    spec:
      containers:
      - name: worker
        resources:
          requests:
            cpu: 2000m      # Was: 1000m
            memory: 4Gi     # Was: 2Gi
          limits:
            cpu: 4000m      # Was: 2000m
            memory: 8Gi     # Was: 4Gi
```

**Apply changes:**
```bash
kubectl apply -f config/manager/worker-deployment.yaml
kubectl rollout restart deployment/ado-to-git-migration-worker -n migration-system
```

---

## Best Practices

### Code Style

**1. Use descriptive names**
```go
// Bad
func p(r *Repo) {}

// Good
func processRepository(repo *Repository) {}
```

**2. Add comments for exported functions**
```go
// DiscoverRepositories finds all repositories in the specified ADO project.
// It applies filters and naming conventions before returning.
func (r *MigrationJobReconciler) DiscoverRepositories(ctx, job) ([]Repository, error) {
    // ...
}
```

**3. Handle errors properly**
```go
// Bad
repos, _ := service.ListRepos()

// Good
repos, err := service.ListRepos()
if err != nil {
    return fmt.Errorf("failed to list repos: %w", err)
}
```

**4. Use context for cancellation**
```go
func (r *MigrationJobReconciler) longOperation(ctx context.Context) error {
    for _, item := range items {
        // Check if cancelled
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Do work
        process(item)
    }
    return nil
}
```

### Controller Best Practices

**1. Always update status, never spec**
```go
// Bad - modifying spec
job.Spec.Settings.BatchSize = 10
r.Update(ctx, job)

// Good - modifying status
job.Status.Phase = "Running"
r.Status().Update(ctx, job)
```

**2. Use optimistic locking**
```go
// Get latest version
job := &MigrationJob{}
r.Get(ctx, req.NamespacedName, job)

// Modify
job.Status.Phase = "Running"

// Update (will fail if someone else modified it)
err := r.Status().Update(ctx, job)
if err != nil {
    // Requeue and try again
    return ctrl.Result{Requeue: true}, nil
}
```

**3. Requeue with delay for long operations**
```go
func (r *MigrationJobReconciler) Reconcile(ctx, req) {
    // ... do work ...

    if needsRetry {
        // Don't busy-loop, wait before retrying
        return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
    }

    return ctrl.Result{}, nil
}
```

**4. Use finalizers for cleanup**
```go
const finalizerName = "migration.ado-to-git-migration.io/finalizer"

func (r *MigrationJobReconciler) Reconcile(ctx, req) {
    job := &MigrationJob{}
    r.Get(ctx, req.NamespacedName, job)

    // Handle deletion
    if !job.DeletionTimestamp.IsZero() {
        if controllerutil.ContainsFinalizer(job, finalizerName) {
            // Cleanup
            r.cleanup(ctx, job)

            // Remove finalizer
            controllerutil.RemoveFinalizer(job, finalizerName)
            r.Update(ctx, job)
        }
        return ctrl.Result{}, nil
    }

    // Add finalizer if not present
    if !controllerutil.ContainsFinalizer(job, finalizerName) {
        controllerutil.AddFinalizer(job, finalizerName)
        r.Update(ctx, job)
        return ctrl.Result{}, nil
    }

    // Normal processing
    // ...
}
```

### Testing Best Practices

**1. Test one thing at a time**
```go
// Bad - testing multiple things
func TestController(t *testing.T) {
    // Tests discovery, naming, batch creation, etc.
}

// Good - separate tests
func TestDiscovery(t *testing.T) { /* ... */ }
func TestNaming(t *testing.T) { /* ... */ }
func TestBatchCreation(t *testing.T) { /* ... */ }
```

**2. Use table-driven tests**
```go
func TestNaming(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  string
    }{
        {"lowercase", "MyRepo", "myrepo"},
        {"with dash", "My-Repo", "my-repo"},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := transform(tt.input)
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

**3. Use mocks for external services**
```go
// File: internal/services/ado_service_test.go

type mockADOClient struct {
    repos []Repository
    err   error
}

func (m *mockADOClient) ListRepositories() ([]Repository, error) {
    return m.repos, m.err
}

func TestListRepos(t *testing.T) {
    client := &mockADOClient{
        repos: []Repository{{Name: "test-repo"}},
    }

    service := NewAzureDevOpsService(client)
    repos, err := service.ListRepositories()

    assert.NoError(t, err)
    assert.Len(t, repos, 1)
    assert.Equal(t, "test-repo", repos[0].Name)
}
```

---

## Release Process

### 1. Version Bump

```bash
# Update version in Makefile
# Example: VERSION ?= v1.2.0

# Tag the release
git tag v1.2.0
git push origin v1.2.0
```

### 2. Build and Push Image

```bash
# Build
make docker-build IMG=your-registry/ado-to-git-migration:v1.2.0

# Push
make docker-push IMG=your-registry/ado-to-git-migration:v1.2.0
```

### 3. Update Deployment

```bash
# Update image in deployment
kubectl set image deployment/ado-to-git-migration-controller \
  manager=your-registry/ado-to-git-migration:v1.2.0 \
  -n migration-system

# Or apply new manifests
make deploy IMG=your-registry/ado-to-git-migration:v1.2.0
```

### 4. Verify

```bash
# Check new pods are running
kubectl get pods -n migration-system

# Check version
kubectl get deployment ado-to-git-migration-controller -o yaml | grep image:
```

---

## Summary

### Quick Reference Card

| Task | Command |
|------|---------|
| Build code | `make build` |
| Run tests | `make test` |
| Generate manifests | `make manifests` |
| Install CRDs | `make install` |
| Run locally | `make run` |
| Build Docker image | `make docker-build` |
| Deploy to cluster | `make deploy` |
| View controller logs | `kubectl logs deployment/ado-to-git-migration-controller -n migration-system` |
| View worker logs | `kubectl logs <pod-name> -n migration-system` |
| Debug in pod | `kubectl exec -it <pod-name> -n migration-system -- sh` |

### Remember

1. **Always test locally** before deploying
2. **Add logs** for debugging
3. **Update CRDs** after changing types (`make manifests install`)
4. **Write tests** for new features
5. **Check logs** when things go wrong
6. **Use simple, clear variable names**
7. **Comment complex logic**
8. **Handle errors properly**

---

**Happy coding!** 🚀
