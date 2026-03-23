# Migration Patterns

Reposhift supports four migration patterns, each backed by a dedicated Kubernetes Custom Resource Definition (CRD). This guide covers each pattern with detailed configuration and examples.

---

## 1:1 Repository Migration

**CRD Kind:** `AdoToGitMigration`

Migrates one Azure DevOps repository to one GitHub repository, preserving full git history including branches, tags, and commits.

### What Gets Migrated

| Item | Migrated | Notes |
|------|----------|-------|
| Commit history | Yes | Full or shallow (configurable via `cloneDepth`) |
| Branches | Yes | All by default; filterable with include/exclude patterns |
| Tags | Yes | All annotated and lightweight tags |
| Default branch | Yes | Auto-detected from source, set on target |
| LFS objects | Yes | When `handleLFS: true` |
| Pull requests | No | ADO PRs are not transferred (merge commits are preserved in history) |
| Build definitions | No | Use `PipelineToWorkflow` for pipeline migration |
| Branch policies | No | Must be reconfigured manually on GitHub |

### Basic Example

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: backend-repo-migration
  namespace: reposhift
spec:
  type: repository

  source:
    organization: my-ado-org
    project: MyProject
    auth:
      pat:
        tokenRef:
          name: ado-pat-secret
          key: token

  target:
    owner: my-github-org
    auth:
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
    - type: repository
      sourceId: backend-api
      sourceName: backend-api
      targetName: backend-api

  settings:
    maxHistoryDays: 730
    retryAttempts: 3
    parallelWorkers: 3
    includeTags: true
    handleLFS: true
```

### Branch Filtering

Control which branches are migrated using glob patterns:

```yaml
settings:
  repository:
    includeBranches:
      - "main"
      - "release/*"
      - "develop"
    excludeBranches:
      - "feature/*"
      - "bugfix/*"
      - "personal/*"
```

Rules:
- If `includeBranches` is specified, only matching branches are migrated
- If `excludeBranches` is specified, matching branches are excluded
- If both are specified, a branch must match an include pattern AND not match an exclude pattern
- Patterns use glob syntax: `*` matches any characters, `?` matches a single character

### Branch Mapping

Rename branches during migration:

```yaml
settings:
  repository:
    branchMapping:
      "master": "main"
      "develop": "development"
```

### Shallow Cloning

For large repositories where full history is not needed:

```yaml
settings:
  cloneDepth: 100  # Only the most recent 100 commits per branch
```

When `cloneDepth` is set to a value greater than 0:
- Branches and tags are still fetched
- Commit history is truncated to the specified depth
- Clone time and disk usage are significantly reduced
- Set to `0` (default) for full history

### Auto-Generated Repository Names

Reposhift can generate target repository names based on metadata:

```yaml
source:
  businessUnit: "lg"
  productName: "authority"
  migrationType: product  # generates: product-lg-authority-<sourceName>

target:
  autoGenerateName: true
```

Naming conventions by `migrationType`:

| Type | Pattern | Example |
|------|---------|---------|
| `product` | `product-<bu>-<product>-<sourceName>` | `product-lg-authority-backend-api` |
| `platform` | `platform-<bu>-<sourceName>` | `platform-lg-infra-tools` |
| `shared` | `shared-<sourceName>` | `shared-common-utils` |

### Multiple Repositories in One CRD

Migrate several repositories in a single CRD by listing them under `resources`:

```yaml
resources:
  - type: repository
    sourceId: repo-1-id
    sourceName: backend-api
    targetName: product-lg-authority-backend-api

  - type: repository
    sourceId: repo-2-id
    sourceName: frontend-app
    targetName: product-lg-authority-frontend-app

  - type: repository
    sourceId: repo-3-id
    sourceName: shared-libs
    targetName: product-lg-authority-shared-libs
```

Repositories are migrated according to the `parallelWorkers` setting. With `parallelWorkers: 3`, all three would be migrated concurrently.

### Target Repository Creation

By default, the target GitHub repository is created automatically if it does not exist. You can configure this per-resource:

```yaml
resources:
  - type: repository
    sourceId: my-repo
    sourceName: my-repo
    targetName: my-repo
    settings:
      repository:
        createIfNotExists: true
        visibility: private
```

---

## Many:1 Monorepo Migration

**CRD Kind:** `MonoRepoMigration`

Merges multiple Azure DevOps repositories into a single GitHub monorepo. Each source repository becomes a subdirectory in the target.

### How It Works

1. **Clone** -- Each source repository is cloned from Azure DevOps (in parallel)
2. **Rewrite** -- Git history is rewritten so that all files appear under a subdirectory (using `git filter-repo`)
3. **Merge** -- All rewritten repositories are merged into a single repository with combined history
4. **Push** -- The combined repository is pushed to GitHub

### How Branches Are Handled

Branches from each source repo are prefixed with the repository name to avoid collisions:

| Source Repo | Source Branch | Monorepo Branch |
|-------------|--------------|-----------------|
| `backend-api` | `main` | `backend-api/main` |
| `backend-api` | `develop` | `backend-api/develop` |
| `frontend-app` | `main` | `frontend-app/main` |

The monorepo's default branch (e.g., `main`) contains the merged default branches from all source repos.

### How Tags Are Handled

Tags are prefixed with the source repository name:

| Source Repo | Source Tag | Monorepo Tag |
|-------------|-----------|--------------|
| `backend-api` | `v1.0.0` | `backend-api/v1.0.0` |
| `frontend-app` | `v2.3.1` | `frontend-app/v2.3.1` |

### Example

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: MonoRepoMigration
metadata:
  name: platform-monorepo
  namespace: reposhift
spec:
  source:
    organization: my-ado-org
    project: PlatformTeam
    auth:
      pat:
        tokenRef:
          name: ado-pat-secret
          key: token

  target:
    owner: my-github-org
    repository: platform-monorepo
    visibility: private
    defaultBranch: main
    auth:
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

  sourceRepos:
    - name: infra-terraform
      subdirectoryName: infrastructure
      includeBranches:
        - "main"
        - "release/*"
      priority: 1

    - name: shared-go-libs
      subdirectoryName: libs
      priority: 2

    - name: deploy-scripts
      subdirectoryName: deploy
      excludeBranches:
        - "experimental/*"
      priority: 3

  settings:
    parallelClones: 3
    retryAttempts: 3
    continueOnError: true
    cleanupBetweenRepos: true
    cloneDepth: 0
```

### Subdirectory Layout

The resulting monorepo will have the following structure:

```
platform-monorepo/
  infrastructure/    # from infra-terraform
  libs/              # from shared-go-libs
  deploy/            # from deploy-scripts
```

### Best Practices

- Keep monorepo merges to **10 or fewer source repositories** for manageable history
- Use `priority` to control processing order (lower number = processed first)
- Set `continueOnError: true` so a failure in one repo does not block the rest
- Enable `cleanupBetweenRepos: true` to free disk space between clones
- Use `cloneDepth` for shallow history if full history of all repos is not needed

---

## Work Item Migration

**CRD Kind:** `WorkItemMigration`

Migrates Azure DevOps work items to GitHub Issues. Work items are created as issues in a specified GitHub repository and optionally added to a GitHub Project board.

### Supported Work Item Types

| ADO Type | Default GitHub Label | Notes |
|----------|---------------------|-------|
| Epic | `epic` | Maps to a GitHub Issue with "epic" label |
| Feature | `feature` | Maps to a GitHub Issue with "feature" label |
| User Story | `user-story` | Maps to a GitHub Issue with "user-story" label |
| Bug | `bug` | Maps to a GitHub Issue with "bug" label |
| Task | `task` | Maps to a GitHub Issue with "task" label |
| Issue | `issue` | Maps to a GitHub Issue with "issue" label |

Type mapping is fully customizable via the `typeMapping` field.

### State Mapping

| ADO State | GitHub Issue State | Notes |
|-----------|-------------------|-------|
| New | `open` | Newly created items |
| Active | `open` | In progress |
| In Progress | `open` | Being worked on |
| To Do | `open` | Queued for work |
| Resolved | `closed` | Completed, pending verification |
| Closed | `closed` | Verified complete |
| Done | `closed` | Finished |
| Removed | `closed` | Cancelled or removed |

State mapping is fully customizable via the `stateMapping` field.

### What Gets Preserved

| ADO Field | GitHub Equivalent | Behavior |
|-----------|-------------------|----------|
| Title | Issue title | Direct mapping |
| Description | Issue body | HTML converted to Markdown |
| State | Issue state (open/closed) | Via state mapping |
| Work item type | Label | Via type mapping |
| Tags | Labels | Each ADO tag becomes a GitHub label |
| Comments/History | Issue comments | All comments preserved with author/date metadata |
| Attachments | Issue comment links | Attachments referenced in comments |
| Area Path | Label (`area:<path>`) | Converted to labels |
| Iteration Path | Label (`iteration:<path>`) | Converted to labels |
| Priority | Label (`priority:<value>`) | Converted to labels |
| Story Points | Issue body section | Appended to body |
| Acceptance Criteria | Issue body section | Appended to body |
| Relationships | Issue body references | Parent/child links noted in body |

### Filtering

Control which work items are migrated:

```yaml
filters:
  # By work item type
  types:
    - Epic
    - "User Story"
    - Bug

  # By state
  states:
    - Active
    - New

  # By area path
  areaPaths:
    - "MyProject\\Team A"
    - "MyProject\\Team B"

  # By iteration path
  iterationPaths:
    - "MyProject\\Sprint 1"
    - "MyProject\\Sprint 2"

  # By tag
  tags:
    - "high-priority"
    - "migration-ready"

  # By assigned user
  assignedTo:
    - "user@example.com"

  # By date range
  dateRange:
    start: "2023-01-01T00:00:00Z"
    end: "2024-12-31T23:59:59Z"

  # Advanced: raw WIQL query
  wiqlQuery: |
    SELECT [System.Id]
    FROM WorkItems
    WHERE [System.TeamProject] = 'MyProject'
      AND [System.WorkItemType] = 'Bug'
      AND [System.State] = 'Active'
    ORDER BY [System.ChangedDate] DESC
```

### Example

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: WorkItemMigration
metadata:
  name: project-workitems
  namespace: reposhift
spec:
  source:
    organization: my-ado-org
    project: MyProject
    team: "Backend Team"
    auth:
      pat:
        tokenRef:
          name: ado-pat-secret
          key: token

  target:
    owner: my-github-org
    repository: my-github-repo
    projectRef: my-github-project
    auth:
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

  filters:
    types:
      - Epic
      - "User Story"
      - Bug
      - Task
    states:
      - Active
      - New
      - "In Progress"

  settings:
    batchSize: 20
    batchDelaySeconds: 60
    perItemDelayMs: 1000
    timeoutMinutes: 360
    includeHistory: true
    includeAttachments: true
    includeTags: true
    preserveRelationships: true
    combineComments: true

    typeMapping:
      Epic: "epic"
      Feature: "feature"
      "User Story": "user-story"
      Bug: "bug"
      Task: "task"

    stateMapping:
      New: "open"
      Active: "open"
      "In Progress": "open"
      Resolved: "closed"
      Closed: "closed"
      Removed: "closed"

    fieldMapping:
      System.Title: "title"
      System.Description: "body"
      System.State: "state"
      System.Tags: "labels"
      System.AreaPath: "label:area"
      System.IterationPath: "label:iteration"
      Microsoft.VSTS.Common.Priority: "label:priority"
```

### Comment Combining

By default (`combineComments: true`), all ADO work item comments are combined into a single GitHub issue comment. This:
- Reduces API calls by approximately 90%
- Avoids GitHub secondary rate limits
- Preserves author and date metadata in a formatted block

Set `combineComments: false` to create a separate GitHub comment for each ADO comment (legacy behavior).

---

## Pipeline Migration

**CRD Kind:** `PipelineToWorkflow`

Converts Azure DevOps YAML pipelines into GitHub Actions workflow files and commits them to the target repository's `.github/workflows/` directory.

### Supported Pipeline Types

| ADO Pipeline Type | Supported | Notes |
|-------------------|-----------|-------|
| YAML pipelines | Yes | Converted to GitHub Actions syntax |
| Classic build pipelines | Partial | Best-effort conversion, manual review recommended |
| Classic release pipelines | Partial | Best-effort conversion, manual review recommended |

### What Gets Converted

| ADO Concept | GitHub Actions Equivalent |
|-------------|--------------------------|
| `trigger` | `on.push` / `on.pull_request` |
| `pool` | `runs-on` |
| `steps` | `steps` |
| `variables` | `env` |
| `stages` | `jobs` |
| `jobs` | `jobs` |
| `templates` | Composite actions or reusable workflows |
| `resources` | Manually configured |
| Service connections | Manually configured secrets |

### What Requires Manual Review

After pipeline migration, review and update:

- **Secrets** -- ADO service connections and variable groups must be recreated as GitHub repository or organization secrets
- **Self-hosted agents** -- ADO agent pools must be replaced with GitHub self-hosted runners
- **Marketplace tasks** -- ADO pipeline tasks must be replaced with equivalent GitHub Actions
- **Variable groups** -- Must be converted to GitHub environments or repository variables
- **Approval gates** -- Must be reconfigured as GitHub environment protection rules

### Example with Auto-Discovery

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: PipelineToWorkflow
metadata:
  name: pipeline-migration
  namespace: reposhift
spec:
  type: all

  source:
    organization: my-ado-org
    project: MyProject
    auth:
      pat:
        tokenRef:
          name: ado-pat-secret
          key: token

  target:
    owner: my-github-org
    repository: my-github-repo
    auth:
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

  autoDiscovery:
    enabled: true
    includeBuildPipelines: true
    includeReleasePipelines: true
    folderFilter: "/Production/*"
    nameFilter: ".*-ci$"
    maxPipelines: 50

  settings:
    convertToActions: true
```

### Example with Explicit Pipeline List

```yaml
spec:
  type: yaml
  pipelines:
    - name: backend-ci
      sourceId: "12345"
      targetWorkflowName: backend-ci.yml

    - name: frontend-ci
      sourceId: "67890"
      targetWorkflowName: frontend-ci.yml
```

---

## Migration Lifecycle

All migration CRDs follow the same lifecycle phases:

```
Pending --> Validating --> Running --> Completed
                 |            |
                 v            v
              Failed       Failed
```

### Phase Descriptions

| Phase | Description |
|-------|-------------|
| `Pending` | CRD created, waiting for controller to pick it up |
| `Validating` | Checking credentials, permissions, source/target existence |
| `Running` | Migration in progress |
| `Completed` | All resources migrated successfully |
| `Failed` | One or more resources failed (check `errorMessage` and `resourceStatuses`) |
| `Cancelled` | Migration cancelled by user |
| `Paused` | Migration paused (can be resumed) |

### Monitoring Progress

```bash
# Watch all migrations
kubectl get adotogitmigration,monorepomigration,workitemmigration,pipelinetoworkflow \
  -n reposhift --watch

# Detailed status of a specific migration
kubectl describe adotogitmigration my-migration -n reposhift

# JSON progress for scripting
kubectl get adotogitmigration my-migration -n reposhift \
  -o jsonpath='{.status.progress}'
```
