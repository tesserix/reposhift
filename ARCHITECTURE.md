# Architecture Guide

How the migration tool works, explained simply.

## Table of Contents
- [Big Picture](#big-picture)
- [Main Components](#main-components)
- [How Migration Works](#how-migration-works)
- [Auto-Discovery Flow](#auto-discovery-flow)
- [Parallel Processing](#parallel-processing)
- [Auto-Scaling](#auto-scaling)
- [Data Flow](#data-flow)
- [Code Organization](#code-organization)

---

## Big Picture

### What is a Kubernetes Operator?

Think of it as a smart robot that runs in Kubernetes and automates tasks.

**Normal way:**
- You manually do steps 1, 2, 3...
- You watch and fix problems
- You track progress yourself

**Operator way:**
- You say "migrate these repos"
- Operator does all the steps automatically
- Operator watches for problems and fixes them
- Operator reports progress

### Our Operator

```
┌─────────────────────────────────────────────┐
│            You (Human)                      │
│   "Migrate all repos from Platform-Team"   │
└──────────────────┬──────────────────────────┘
                   │ (kubectl apply)
                   ▼
┌─────────────────────────────────────────────┐
│         MigrationJob Controller             │
│   - Discovers repos                         │
│   - Creates worker batches                  │
│   - Monitors progress                       │
└──────────────────┬──────────────────────────┘
                   │ (creates)
                   ▼
┌─────────────────────────────────────────────┐
│         BatchMigration Workers              │
│   Pod 1: Migrates repo A                    │
│   Pod 2: Migrates repo B                    │
│   Pod 3: Migrates repo C                    │
│   ... (up to 100 pods)                      │
└─────────────────────────────────────────────┘
```

---

## Main Components

### 1. Custom Resources (CRDs)

These are like forms you fill out to tell the operator what to do.

#### MigrationJob
**What it is:** The main form you fill out

**What it contains:**
- Where to get repos from (ADO project)
- Where to put them (GitHub org)
- How to name them (naming rules)
- What to migrate (repos, work items, etc.)

**Example:**
```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: MigrationJob
metadata:
  name: my-migration
spec:
  azureDevOps:
    organization: my-org
    project: Platform-Team
  github:
    owner: my-org
  discovery:
    repositories:
      enabled: true
```

#### BatchMigration
**What it is:** One unit of work (usually 1 repo)

**What it contains:**
- Which repos to migrate
- Which worker claimed it
- Current status (pending, processing, completed, failed)
- Progress percentage

**Example:**
```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: BatchMigration
metadata:
  name: batch-1
spec:
  batchNumber: 1
  resources:
  - type: repository
    sourceName: "java-authority"
    targetName: "product-lg-authority-java-authority"
status:
  phase: Processing
  claimedBy: worker-abc123
  progress:
    total: 1
    completed: 0
    percentage: 45
```

### 2. Controllers

Controllers are the brain of the operator. They watch for changes and take action.

#### MigrationJob Controller
**Location:** `internal/controller/migrationjob_controller.go`

**Job:** Orchestrate the whole migration

**What it does:**
1. Receives your MigrationJob
2. Connects to ADO API
3. Discovers all repos
4. Applies naming rules
5. Creates BatchMigration for each repo
6. Monitors overall progress
7. Updates status

**Think of it as:** The project manager

#### BatchMigration Controller
**Location:** `internal/controller/batchmigration_controller.go`

**Job:** Do the actual migration work

**What it does:**
1. Watches for pending BatchMigrations
2. Claims one (locks it for this worker)
3. Migrates the repos in that batch
4. Updates progress
5. Marks as complete or failed

**Think of it as:** The worker

### 3. Services

Services handle API calls to external systems.

#### ADO Service
**Location:** `internal/services/ado_service.go`

**Job:** Talk to Azure DevOps

**Functions:**
- `ListRepositories()` - Get all repos
- `GetRepository()` - Get one repo details
- `CloneRepository()` - Clone repo to local disk

#### GitHub Service
**Location:** `internal/services/github_service.go`

**Job:** Talk to GitHub

**Functions:**
- `CreateRepository()` - Create new repo
- `CheckRepositoryExists()` - See if repo exists
- `PushRepository()` - Push code to GitHub

---

## How Migration Works

### Step-by-Step Flow

```
Step 1: You Create MigrationJob
   ↓
   You write: my-migration.yaml
   You run: kubectl apply -f my-migration.yaml
   ↓
Step 2: Controller Receives It
   ↓
   Controller sees: "New MigrationJob!"
   Controller reads: Project name, discovery settings
   ↓
Step 3: Discovery Phase
   ↓
   Controller connects to ADO API
   Controller lists all repos in project
   Result: ["repo1", "repo2", "repo3", ...]
   ↓
Step 4: Apply Naming Rules
   ↓
   For each repo:
   - Input: "java-authority"
   - Template: "product-lg-authority-{{.SourceName}}"
   - Output: "product-lg-authority-java-authority"
   ↓
Step 5: Create Batches
   ↓
   For each repo:
   - Create BatchMigration
   - Set status: Pending
   ↓
Step 6: Workers Claim Batches
   ↓
   Worker 1: "I'll take batch-1!"
   Worker 2: "I'll take batch-2!"
   Worker 3: "I'll take batch-3!"
   ...
   ↓
Step 7: Workers Migrate
   ↓
   Each worker:
   1. Clone from ADO
   2. Push to GitHub
   3. Update progress
   4. Mark complete
   ↓
Step 8: All Done!
   ↓
   Controller sees: All batches complete
   Controller updates: MigrationJob status = Complete
```

### Code Flow

```go
// 1. User creates MigrationJob
// File: CLAIM_TEMPLATES/01-auto-discovery-repo-migration.yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: MigrationJob

// 2. Controller receives it
// File: internal/controller/migrationjob_controller.go
func (r *MigrationJobReconciler) Reconcile(ctx, req) {
    // Get the MigrationJob
    job := &MigrationJob{}
    r.Get(ctx, req.NamespacedName, job)

    // Start discovery
    repos := discoverRepositories(job.Spec.AzureDevOps)

    // Create batches
    for _, repo := range repos {
        batch := createBatchMigration(repo)
        r.Create(ctx, batch)
    }
}

// 3. Worker claims batch
// File: internal/controller/batchmigration_controller.go
func (r *BatchMigrationReconciler) Reconcile(ctx, req) {
    // Get batch
    batch := &BatchMigration{}
    r.Get(ctx, req.NamespacedName, batch)

    // Claim it
    if batch.Status.Phase == "Pending" {
        batch.Status.ClaimedBy = r.WorkerID
        batch.Status.Phase = "Processing"
        r.Update(ctx, batch)

        // Do migration
        migrateRepository(batch.Spec.Resources[0])

        // Mark complete
        batch.Status.Phase = "Completed"
        r.Update(ctx, batch)
    }
}
```

---

## Auto-Discovery Flow

### How It Finds Repos

```
1. Read Config
   ↓
   Read: azureDevOps.organization = "my-org"
   Read: azureDevOps.project = "Platform-Team"
   ↓
2. Connect to ADO
   ↓
   API Call: GET /my-org/Platform-Team/_apis/git/repositories
   ↓
3. Get Response
   ↓
   Response: [
     {id: "abc-123", name: "java-authority"},
     {id: "def-456", name: "devops-infra"},
     ...
   ]
   ↓
4. Apply Filters (if any)
   ↓
   includePatterns: ["java-*"]
   Result: Keep "java-authority", skip "devops-infra"
   ↓
5. Apply Naming
   ↓
   Template: "product-lg-authority-{{.SourceName}}"
   Result: "product-lg-authority-java-authority"
   ↓
6. Create Batch
   ↓
   BatchMigration {
     sourceName: "java-authority",
     targetName: "product-lg-authority-java-authority"
   }
```

### Code Implementation

```go
// File: internal/controller/migrationjob_controller.go

func (r *MigrationJobReconciler) discoverRepositories(ctx, job) []Repository {
    // 1. Get ADO client
    adoService := services.NewAzureDevOpsService()

    // 2. List all repos
    repos, err := adoService.ListRepositories(
        job.Spec.AzureDevOps.Organization,
        job.Spec.AzureDevOps.Project,
    )

    // 3. Apply filters
    filtered := filterRepositories(repos, job.Spec.Discovery.Repositories)

    // 4. Apply naming
    for i, repo := range filtered {
        filtered[i].TargetName = applyNamingConvention(
            repo.Name,
            job.Spec.Discovery.Repositories.NamingConvention,
        )
    }

    return filtered
}

func applyNamingConvention(sourceName, convention) string {
    switch convention.Strategy {
    case "template":
        // Replace {{.SourceName}} with actual name
        return strings.ReplaceAll(
            convention.Template,
            "{{.SourceName}}",
            sourceName,
        )
    case "prefix":
        return convention.Prefix + sourceName
    case "suffix":
        return sourceName + convention.Suffix
    default:
        return sourceName
    }
}
```

---

## Parallel Processing

### The Problem with Sequential

```
Old way (sequential):
Time: 0min → Migrate repo1 → 30min → Migrate repo2 → 60min → ...
Total: 50 repos × 30min = 1500min (25 hours!)
```

### The Solution: Parallel

```
New way (parallel):
Time: 0min → All 50 repos migrate at once → 30min
Total: 30min!

How?
- Each repo gets its own worker pod
- All workers run at the same time
- Kubernetes schedules them across nodes
```

### Architecture

```
┌──────────────────────────────────────────┐
│      MigrationJob Controller             │
│                                          │
│  Creates 50 BatchMigrations              │
└──────────┬───────────────────────────────┘
           │
           ├─── Creates ──→ BatchMigration-1 (Repo A)
           ├─── Creates ──→ BatchMigration-2 (Repo B)
           ├─── Creates ──→ BatchMigration-3 (Repo C)
           └─── ... (47 more)

           ↓ (all at once!)

┌──────────┴───────────────────────────────┐
│         Worker Pods                      │
│                                          │
│  ┌────────┐  ┌────────┐  ┌────────┐    │
│  │Worker 1│  │Worker 2│  │Worker 3│    │
│  │Repo A  │  │Repo B  │  │Repo C  │    │
│  └────────┘  └────────┘  └────────┘    │
│  ... (47 more workers)                  │
└─────────────────────────────────────────┘
```

### How Workers Claim Batches

```go
// File: internal/controller/batchmigration_controller.go

func (r *BatchMigrationReconciler) claimBatch(ctx, batch) bool {
    // Try to claim (optimistic locking)
    batch.Status.Phase = "Claimed"
    batch.Status.ClaimedBy = r.WorkerID
    batch.Status.ClaimedAt = time.Now()

    // Try to save (only one worker succeeds)
    err := r.Status().Update(ctx, batch)
    if err != nil {
        // Another worker already claimed it!
        return false
    }

    // We got it!
    return true
}
```

**Race condition protection:**
- Multiple workers try to claim the same batch
- Kubernetes ensures only ONE succeeds (optimistic locking)
- Others see "already claimed" and move to next batch

---

## Auto-Scaling

### How It Works

```
Scenario: 50 repos need to migrate

1. Initially: 2 worker pods running
   ↓
2. Controller creates: 50 BatchMigrations (all "Pending")
   ↓
3. HPA sees: 48 pending batches (50 - 2 processing)
   ↓
4. HPA calculates: Need 48 more workers
   ↓
5. HPA tells Kubernetes: "Scale to 50 pods"
   ↓
6. Kubernetes: Creates 48 new worker pods (takes ~30 seconds)
   ↓
7. All 50 workers: Each claims a batch and starts migrating
   ↓
8. After completion: HPA scales back down to 2 pods
```

### HPA Configuration

```yaml
# File: config/autoscaling/hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  minReplicas: 2        # Always keep 2 running
  maxReplicas: 100      # Never go above 100

  metrics:
  - type: Pods
    pods:
      metric:
        name: migration_pending_batches
      target:
        averageValue: "1"   # 1 pending batch = add 1 pod
```

**Translation:**
- If 1 pending batch → Add 1 pod
- If 48 pending batches → Add 48 pods
- But never go above 100 total

### Scaling Timeline

```
T+0s   : Submit MigrationJob
T+5s   : Controller discovers 50 repos
T+10s  : Controller creates 50 BatchMigrations
T+15s  : HPA detects 48 pending batches
T+20s  : HPA requests scale-up to 50 pods
T+30s  : Kubernetes creates new pods
T+45s  : All 50 pods running and claiming batches
T+60s  : All repos migrating in parallel!
```

---

## Data Flow

### Migration Data Flow

```
┌──────────────┐
│  Azure DevOps│
│              │
│  ┌────────┐  │
│  │ Repo A │  │ ←─── 1. Clone
│  └────────┘  │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ Worker Pod   │
│              │
│ /tmp/repo-a  │ ←─── 2. Temporary storage
│              │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│   GitHub     │
│              │
│  ┌────────┐  │
│  │ Repo A │  │ ←─── 3. Push
│  └────────┘  │
└──────────────┘
```

### Status Data Flow

```
┌──────────────┐
│ Worker Pod   │
│              │
│ - Migrating  │
│ - 45% done   │
└──────┬───────┘
       │ (updates status)
       ▼
┌──────────────────┐
│ BatchMigration   │
│                  │
│ status:          │
│   phase: Processing │
│   progress: 45%  │
└──────┬───────────┘
       │ (aggregates)
       ▼
┌──────────────────┐
│ MigrationJob     │
│                  │
│ status:          │
│   progress: 30%  │
│   (15/50 done)   │
└──────────────────┘
```

### API Call Flow

```
Worker needs to migrate repo "java-authority"

1. Get ADO credentials from secret
   API: Kubernetes Secrets API

2. Clone from ADO
   API: Azure DevOps Git API
   Call: git clone https://dev.azure.com/org/project/_git/java-authority

3. Get GitHub credentials from secret
   API: Kubernetes Secrets API

4. Check if repo exists
   API: GitHub REST API
   Call: GET /repos/my-org/product-lg-authority-java-authority

5. Create repo if doesn't exist
   API: GitHub REST API
   Call: POST /orgs/my-org/repos

6. Push to GitHub
   API: GitHub Git API
   Call: git push https://github.com/my-org/product-lg-authority-java-authority

7. Update status
   API: Kubernetes API
   Call: PATCH /apis/migration.../batchmigrations/batch-1/status
```

---

## Code Organization

### Directory Structure

```
/
├── api/v1/                          # What you can create (CRDs)
│   ├── migrationjob_types.go       # Main job definition
│   ├── batchmigration_types.go     # Worker batch definition
│   └── discovery_config_types.go   # Auto-discovery config
│
├── internal/
│   ├── controller/                 # The brain (logic)
│   │   ├── migrationjob_controller.go      # Orchestrator
│   │   └── batchmigration_controller.go    # Worker
│   │
│   └── services/                   # API integrations
│       ├── ado_service.go          # Talk to Azure DevOps
│       ├── github_service.go       # Talk to GitHub
│       └── migration_service.go    # Actual migration logic
│
├── config/                         # Kubernetes configs
│   ├── crd/bases/                  # CRD definitions
│   ├── manager/                    # How to run the operator
│   │   └── worker-deployment.yaml  # Pod configuration
│   └── autoscaling/                # Auto-scaling rules
│       └── hpa.yaml                # HPA configuration
│
└── CLAIM_TEMPLATES/                # Migration templates
    ├── 00-secrets-setup.yaml
    ├── 01-auto-discovery-repo-migration.yaml
    ├── 02-workitems-migration.yaml
    └── 03-complete-migration.yaml
```

### Key Files Explained

#### api/v1/migrationjob_types.go
**What:** Defines MigrationJob structure

**Why:** Tells Kubernetes "this is what a MigrationJob looks like"

**Key parts:**
```go
type MigrationJobSpec struct {
    AzureDevOps AzureDevOpsConfig      // Where to get repos from
    GitHub      GitHubConfig            // Where to put them
    Discovery   *DiscoveryConfig        // How to find repos
    Settings    MigrationJobSettings    // Migration options
}
```

#### internal/controller/migrationjob_controller.go
**What:** Main orchestration logic

**Why:** Coordinates the whole migration

**Key functions:**
- `Reconcile()` - Main loop, called when anything changes
- `discoverRepositories()` - Finds repos in ADO
- `createBatchMigrations()` - Creates worker batches
- `updateProgress()` - Tracks overall progress

#### internal/controller/batchmigration_controller.go
**What:** Worker logic

**Why:** Does the actual migration work

**Key functions:**
- `Reconcile()` - Main loop for worker
- `claimBatch()` - Grab a batch to work on
- `processResource()` - Migrate one repo
- `updateStatus()` - Report progress

#### internal/services/migration_service.go
**What:** Actual migration code

**Why:** Handles git operations

**Key functions:**
- `MigrateRepository()` - Main migration function
- `cloneFromADO()` - Clone repo from ADO
- `pushToGitHub()` - Push repo to GitHub
- `handleProgress()` - Report progress during migration

---

## Summary

### Simple Mental Model

Think of it like a restaurant:

**You (Customer):**
- Order: "I want 50 meals (repos migrated)"
- Fill form: MigrationJob YAML

**Manager (MigrationJob Controller):**
- Receives order
- Breaks into tasks: 50 individual orders
- Assigns to cooks: Creates 50 BatchMigrations

**Cooks (Worker Pods):**
- Each grabs one order
- Prepares the meal (migrates repo)
- Marks done when finished

**Kitchen Automation (HPA):**
- Sees 50 orders
- Hires more cooks
- Fires cooks when done

**Result:**
- All 50 meals ready at once (30 min)
- Instead of one by one (25 hours)

---

**Key Concepts to Remember:**

1. **MigrationJob** = Your order (what to migrate)
2. **BatchMigration** = One unit of work (one repo)
3. **Controller** = Manager (orchestrates everything)
4. **Worker** = Cook (does the actual work)
5. **HPA** = Hiring manager (adds workers when needed)
6. **Parallel** = All repos migrate at once (fast!)

Simple! 🚀
