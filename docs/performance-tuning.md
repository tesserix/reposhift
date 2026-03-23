# Performance Tuning

This guide covers how to optimize Reposhift for large-scale migrations, covering parallelism, rate limits, resource allocation, and disk management.

---

## Parallel Workers

The `parallelWorkers` setting controls how many repositories are migrated concurrently within a single `AdoToGitMigration` CRD.

```yaml
settings:
  parallelWorkers: 5  # default
```

| Scenario | Recommended Workers | Notes |
|----------|-------------------|-------|
| Small repos (< 100 MB each) | 5-10 | Network is the bottleneck |
| Medium repos (100 MB - 1 GB) | 3-5 | Balance between speed and resources |
| Large repos (> 1 GB) | 1-2 | Disk and memory constrained |
| Monorepo assembly | 3 | `parallelClones` in MonoRepoMigration |

Increasing parallel workers proportionally increases CPU, memory, and disk usage. Monitor resource utilization before increasing beyond 5.

For `MonoRepoMigration`, the equivalent setting is `parallelClones` (1-10, default 3):

```yaml
settings:
  parallelClones: 3
```

---

## Clone Depth

Shallow cloning reduces clone time and disk usage by truncating commit history.

```yaml
settings:
  cloneDepth: 0    # Full history (default)
  cloneDepth: 100  # Last 100 commits per branch
  cloneDepth: 1    # Only the latest commit per branch
```

### When to Use Shallow Clones

| Scenario | Recommended Depth | Rationale |
|----------|-------------------|-----------|
| Full history preservation needed | `0` | Keeps all commits |
| History not important, speed critical | `1` | Fastest possible clone |
| Recent history sufficient | `100-500` | Good balance |
| Very large repos (> 5 GB) | `50-100` | Avoids disk/timeout issues |
| Monorepo assembly | `0` or `50-100` | Depends on whether merged history matters |

With shallow clones:
- All branches and tags are still fetched
- Only commit depth is truncated
- Clone time can drop from hours to minutes for large repos

---

## Rate Limits

### GitHub Rate Limits

| Auth Method | Limit | Per |
|-------------|-------|-----|
| Personal Access Token | 5,000 requests | Per hour, per user |
| GitHub App (installation) | 10,000 requests | Per hour, per installation |
| GitHub App (user-to-server) | 5,000 requests | Per hour, per user |
| Unauthenticated | 60 requests | Per hour, per IP |

A single repository migration typically uses 10-30 API calls (create repo, set default branch, verify). Work item migration uses 2-5 API calls per work item (create issue, add labels, add comments).

### Azure DevOps Rate Limits

| Limit | Value | Notes |
|-------|-------|-------|
| Per PAT | ~60 requests/min | Varies by organization plan |
| Global burst | ~200 requests in 5 min | Then throttled |
| Work item queries | 20,000 items max per WIQL | Break large queries into smaller batches |

### How Reposhift Handles Rate Limits

Reposhift uses adaptive backoff when rate limits are encountered:

1. On a `429 Too Many Requests` response, the operator reads the `Retry-After` header
2. If no header is present, it uses exponential backoff (1s, 2s, 4s, 8s, ... up to 5 minutes)
3. The retry count is configurable per migration:

```yaml
settings:
  retryAttempts: 3  # default
```

### Configuring Rate Limits

Operator-level rate limits are set in the Helm chart values:

```yaml
operator:
  rateLimits:
    perClient: 100      # Requests per minute per client
    global: 1000        # Global requests per second
    azureDevOps: 60     # Requests per minute to ADO
    github: 5000        # Requests per hour to GitHub
```

Per-migration rate limits can override the global defaults:

```yaml
settings:
  rateLimit:
    adoRequestsPerMinute: 30     # More conservative
    githubRequestsPerMinute: 100  # Throttle GitHub calls
```

### Work Item Rate Limit Tuning

Work item migration is the most API-intensive operation. Key settings:

```yaml
settings:
  batchSize: 20           # Items per batch
  batchDelaySeconds: 60   # Pause between batches
  perItemDelayMs: 1000    # Pause between individual items
  combineComments: true   # Reduces API calls by ~90%
```

| Migration Size | Recommended `batchSize` | Recommended `batchDelaySeconds` | Recommended `perItemDelayMs` |
|---------------|----------------------|------------------------------|----------------------------|
| < 100 items | 20-50 | 5-30 | 500 |
| 100-1,000 items | 20 | 60 | 1000 |
| 1,000-5,000 items | 10 | 60-120 | 1000-1500 |
| > 5,000 items | 10 | 120-300 | 1500-2000 |

Always enable `combineComments: true` for migrations with history enabled.

---

## Resource Limits

### Operator Pod

The operator pod performs the actual git clone and push operations. These are CPU and memory intensive for large repositories.

#### Default Resources (light migrations, < 2 years of history)

```yaml
resources:
  requests:
    cpu: 250m
    memory: 512Mi
  limits:
    cpu: 1000m
    memory: 2Gi
```

#### Production Resources (5-10 years of history, large repos)

```yaml
resources:
  requests:
    cpu: 1000m
    memory: 2Gi
  limits:
    cpu: 4000m
    memory: 8Gi
```

### Sizing Guidelines

| Repo Size | History | Recommended CPU Limit | Recommended Memory Limit |
|-----------|---------|----------------------|-------------------------|
| < 100 MB | < 2 years | 500m | 1Gi |
| 100 MB - 1 GB | < 2 years | 1000m | 2Gi |
| 100 MB - 1 GB | 2-5 years | 2000m | 4Gi |
| > 1 GB | Any | 4000m | 8Gi |
| Monorepo (5+ repos) | Any | 4000m | 8Gi |

### Platform API and Web UI

These components are lightweight and rarely need tuning:

```yaml
# Platform API
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi

# Web UI
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

---

## Disk Space

### Estimating Disk Requirements

During migration, the operator clones repositories to local storage. Estimate required disk space as:

```
Required disk = (sum of repo sizes) * 2.5 * parallelWorkers
```

The 2.5x multiplier accounts for:
- The bare clone (1x)
- The working copy during preparation (1x)
- Git object overhead and temporary files (0.5x)

For monorepo migrations, add all source repos plus the merged result.

### Persistent Volume Configuration

```yaml
persistence:
  enabled: true
  size: 250Gi                         # Adjust based on estimate
  storageClassName: standard           # Use SSD for faster I/O
  accessModes:
    - ReadWriteOnce                    # RWX if running multiple replicas
  mountPath: /workspace/migrations
```

### Reducing Disk Usage

- **Shallow clones**: Set `cloneDepth` to reduce repository size
- **Cleanup between repos**: For monorepo migrations, `cleanupBetweenRepos: true` frees disk after each repo is processed
- **Reduce parallelism**: Fewer parallel workers means fewer concurrent clones on disk
- **Use SSD storage**: Faster I/O reduces the time clones occupy disk

---

## Network Considerations

### Proximity

Place your Kubernetes cluster in a region close to both Azure DevOps and GitHub for the best throughput:

| ADO Region | Recommended Cluster Region | GitHub Region |
|------------|--------------------------|---------------|
| US (Central/East) | US East or US Central | US East (GitHub is primarily US-based) |
| Europe (West) | Europe West | US East (GitHub) or Europe West |
| Asia Pacific | Asia Southeast | US West or Asia |

### Bandwidth

Large repository migrations are bandwidth-intensive. A 1 GB repository requires downloading 1 GB from ADO and uploading 1 GB to GitHub. For 50 repositories of 500 MB each, that is approximately 50 GB of data transfer.

### Egress Costs

If running in a cloud provider, be aware of egress charges for data leaving your cluster's region. The operator downloads from ADO and uploads to GitHub, both external services.

---

## Autoscaling

The operator Helm chart includes an HPA configuration:

```yaml
autoscaling:
  enabled: true
  minReplicas: 1
  maxReplicas: 5
  targetCPUUtilizationPercentage: 80
  targetMemoryUtilizationPercentage: 80
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
```

For advanced scaling based on migration queue depth, KEDA is supported:

```yaml
keda:
  enabled: true
  pollingInterval: 30
  cooldownPeriod: 300
```

Note: When using `ReadWriteOnce` persistent volumes, only one operator pod can mount the volume at a time. Use `ReadWriteMany` storage or separate PVCs per replica for multi-pod scaling.

---

## Batch Delay

When migrating multiple repositories sequentially, `batchDelayMinutes` introduces a pause between each repository to avoid overwhelming APIs:

```yaml
settings:
  batchDelayMinutes: 2  # 2-minute pause between repos
```

| Number of Repos | Recommended Delay | Total Overhead |
|----------------|-------------------|----------------|
| 1-5 | 0-1 minute | Minimal |
| 5-20 | 2 minutes | 10-40 minutes |
| 20-50 | 2-5 minutes | 40-250 minutes |
| 50+ | 5 minutes | Plan for hours |

For large batch migrations, consider using `MigrationJob` with auto-discovery and parallel workers instead of sequential processing.

---

## Summary of Key Settings

| Setting | Location | Default | Tuning Direction |
|---------|----------|---------|-----------------|
| `parallelWorkers` | `settings` | 5 | Increase for speed, decrease for stability |
| `cloneDepth` | `settings` | 0 (full) | Increase from 0 to reduce time/disk |
| `retryAttempts` | `settings` | 3 | Increase for unreliable networks |
| `batchSize` | `settings` | 10 | Decrease for large work item migrations |
| `batchDelayMinutes` | `settings` | 30 | Decrease for speed, increase for rate limits |
| `maxHistoryDays` | `settings` | 730 | Increase up to 3650 for full history |
| `maxCommitCount` | `settings` | 2000 | Increase up to 50000 for large repos |
| `resources.limits.memory` | Helm values | 2Gi | Increase for large repos |
| `persistence.size` | Helm values | 250Gi | Increase for many/large repos |
