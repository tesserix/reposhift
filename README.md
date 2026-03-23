# Reposhift

**Migrate Azure DevOps repos, work items, and pipelines to GitHub -- automatically.**

Reposhift is a Kubernetes-native migration platform that moves your entire Azure DevOps organization to GitHub with full history preservation, real-time progress tracking, and a web dashboard. It runs as a set of controllers and CRDs on any Kubernetes cluster.

---

## Features

- **Repository Migration** -- Clone from Azure DevOps and push to GitHub with full branch, tag, and commit history
- **Monorepo Assembly** -- Merge multiple ADO repositories into a single GitHub monorepo with proper subdirectory layout
- **Work Item Migration** -- Convert ADO work items (Epics, User Stories, Bugs, Tasks) to GitHub Issues with comments, attachments, and labels
- **Pipeline Conversion** -- Transform ADO YAML pipelines into GitHub Actions workflows with auto-discovery
- **Branch Filtering** -- Include or exclude branches using glob patterns (`feature/*`, `release/*`)
- **Shallow Cloning** -- Configurable clone depth to reduce time and disk usage for large repositories
- **GitHub App Support** -- First-class support for GitHub App authentication with automatic token refresh (10,000 req/hr)
- **Real-Time Dashboard** -- Web UI for creating, monitoring, and managing migrations
- **Kubernetes Operator** -- Declarative CRD-based migrations with reconciliation, retry, and status tracking
- **Auto-Discovery** -- Automatically find all repositories and pipelines in an ADO project
- **Continuous Sync** -- Optionally keep ADO and GitHub in sync on a schedule after initial migration
- **Batch Processing** -- Parallel workers with configurable concurrency and rate limiting

---

## Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌───────────────┐
│  Web UI      │────>│  Platform API     │────>│  K8s Operator  │
│  (Next.js)   │     │  (Go/Gin)        │     │  (Go)          │
│  :3005       │     │  :8090           │     │  :8080         │
└─────────────┘     └──────────────────┘     └───────────────┘
                            │                       │
                            v                       v
                     ┌──────────┐           ┌───────────────┐
                     │PostgreSQL│           │ K8s Secrets    │
                     └──────────┘           │ + CRDs         │
                                            └───────────────┘
```

| Component | Description | Image |
|-----------|-------------|-------|
| **Web UI** | Next.js dashboard for managing migrations | `ghcr.io/tesserix/reposhift-web` |
| **Platform API** | Go/Gin REST API, manages state in PostgreSQL | `ghcr.io/tesserix/reposhift-platform` |
| **Operator** | Kubernetes controller that reconciles CRDs and executes migrations | `ghcr.io/tesserix/reposhift` |

---

## Quick Start

### Prerequisites

- Kubernetes 1.26+ cluster
- Helm 3.x
- PostgreSQL 14+ (or use the bundled install)
- `kubectl` configured for your cluster

### 1. Install the Operator

```bash
helm repo add reposhift https://tesserix.github.io/reposhift
helm repo update

kubectl create namespace reposhift

helm install reposhift-operator reposhift/ado-git-migration \
  --namespace reposhift \
  --set auth.github.token="ghp_your_github_pat" \
  --set auth.azure.clientId="your-client-id" \
  --set auth.azure.clientSecret="your-client-secret" \
  --set auth.azure.tenantId="your-tenant-id"
```

### 2. Install the Platform API

```bash
ADMIN_TOKEN=$(openssl rand -hex 32)

helm install reposhift-platform reposhift/reposhift-platform \
  --namespace reposhift \
  --set adminToken="$ADMIN_TOKEN" \
  --set postgresPassword="your-pg-password" \
  --set postgresql.host="your-pg-host"
```

### 3. Install the Web UI

```bash
helm install reposhift-web reposhift/reposhift-web \
  --namespace reposhift
```

### 4. Access the Dashboard

```bash
kubectl port-forward svc/reposhift-web 3005:3005 -n reposhift
```

Open `http://localhost:3005` and log in with the admin token from step 2.

### 5. Create Your First Migration

In the dashboard, click **New Migration** and fill in:
- **Source**: Your ADO organization and project
- **Target**: Your GitHub organization
- **Repositories**: Select which repos to migrate

Or apply a CRD directly:

```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: my-first-migration
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
      tokenRef:
        name: github-token-secret
        key: token
  resources:
    - type: repository
      sourceId: my-repo-id
      sourceName: my-ado-repo
      targetName: my-github-repo
  settings:
    maxHistoryDays: 730
    retryAttempts: 3
    parallelWorkers: 3
```

```bash
kubectl apply -f my-migration.yaml
kubectl get adotogitmigration -n reposhift --watch
```

---

## Migration Patterns

| Pattern | CRD Kind | Description |
|---------|----------|-------------|
| **1:1 Repository** | `AdoToGitMigration` | One ADO repo to one GitHub repo with full history |
| **Many:1 Monorepo** | `MonoRepoMigration` | Multiple ADO repos merged into one GitHub monorepo |
| **Work Items** | `WorkItemMigration` | ADO work items to GitHub Issues with labels and projects |
| **Pipelines** | `PipelineToWorkflow` | ADO pipelines to GitHub Actions workflows |
| **Batch Discovery** | `MigrationJob` | Auto-discover and migrate all repos in a project |

See [docs/migration-patterns.md](docs/migration-patterns.md) for detailed examples of each pattern.

---

## Authentication

Reposhift supports two GitHub authentication methods:

| Method | Rate Limit | Best For |
|--------|-----------|----------|
| **GitHub App** (recommended) | 10,000 req/hr per installation | Production, large migrations |
| **Personal Access Token (PAT)** | 5,000 req/hr | Testing, small migrations |

For Azure DevOps, authentication is via **Personal Access Token** or **Service Principal**.

See [docs/github-app-setup.md](docs/github-app-setup.md) for GitHub App configuration.

---

## Documentation

| Document | Description |
|----------|-------------|
| [Setup Guide](docs/setup-guide.md) | Detailed installation and configuration |
| [GitHub App Setup](docs/github-app-setup.md) | Step-by-step GitHub App configuration |
| [Migration Patterns](docs/migration-patterns.md) | Guides for every migration type |
| [Performance Tuning](docs/performance-tuning.md) | Optimize for large-scale migrations |
| [Troubleshooting](docs/troubleshooting.md) | Common issues and solutions |

---

## CRD Reference

Reposhift installs the following Custom Resource Definitions:

| CRD | Purpose |
|-----|---------|
| `AdoToGitMigration` | Standard 1:1 repository migration |
| `MonoRepoMigration` | Many:1 monorepo assembly |
| `WorkItemMigration` | Work item to GitHub Issues migration |
| `PipelineToWorkflow` | Pipeline to GitHub Actions conversion |
| `MigrationJob` | Batch migration with auto-discovery |
| `BatchMigration` | Individual batch unit (managed by MigrationJob) |
| `GitHubProject` | GitHub Project board creation |
| `AdoDiscovery` | ADO project discovery results |

---

## Project Structure

```
reposhift/
├── api/v1/                    # CRD type definitions
├── internal/
│   ├── controller/            # Kubernetes controllers
│   └── services/              # ADO, GitHub, migration logic
├── charts/
│   ├── ado-git-migration/     # Operator Helm chart
│   ├── reposhift-platform/    # Platform API Helm chart
│   └── reposhift-web/         # Web UI Helm chart
├── config/
│   └── crd/bases/             # Generated CRD manifests
└── EXAMPLES/                  # Ready-to-use migration examples
```

---

## Contributing

Contributions are welcome. To get started:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Make your changes
4. Run tests (`make test`)
5. Submit a pull request

Please ensure all tests pass and CRDs are regenerated (`make manifests generate`) before submitting.

---

## License

Reposhift is licensed under the [Apache License 2.0](LICENSE).
