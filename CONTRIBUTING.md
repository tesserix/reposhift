# Contributing to Reposhift

Thank you for your interest in contributing to Reposhift! We welcome contributions from everyone.

## How to Contribute

### Reporting Bugs

- Check [existing issues](https://github.com/tesserix/reposhift/issues) first
- Open a new issue with a clear title and description
- Include steps to reproduce, expected behavior, and actual behavior
- Include your environment (OS, K8s version, Go version)

### Suggesting Features

- Open a [Discussion](https://github.com/tesserix/reposhift/discussions) first to gauge interest
- Describe the use case and why it would be valuable
- If there's consensus, open an issue to track implementation

### Pull Requests

1. **Fork** the repository
2. **Create a branch** from `main`:
   ```bash
   git checkout -b feat/your-feature
   ```
3. **Make your changes** — keep commits focused and atomic
4. **Write tests** for new functionality
5. **Run tests locally**:
   ```bash
   go test ./... -v
   cd web && npm run build
   ```
6. **Push** to your fork and open a PR against `main`

### Branch Naming

```
feat/short-description     # New features
fix/short-description      # Bug fixes
docs/short-description     # Documentation
refactor/short-description # Code refactoring
test/short-description     # Tests
```

### Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add branch filtering UI to migration wizard
fix: handle empty branches in FilterBranches
docs: add self-hosted deployment guide
refactor: extract CORS middleware from go-common
test: add FilterBranches test for ADO hyphen branches
```

## Development Setup

### Prerequisites

- Go 1.26+
- Node.js 22+
- Docker
- kubectl + Helm 3
- A Kubernetes cluster (minikube, kind, or cloud)

### Backend (Go)

```bash
# Run tests
go test ./internal/platform/... ./internal/services/... -v

# Build the platform binary
go build -o bin/platform ./cmd/platform/

# Build the operator binary
go build -o bin/operator ./cmd/main.go
```

### Frontend (Next.js)

```bash
cd web
npm install
npm run dev    # http://localhost:3005
npm run build  # Production build
```

### Docker

```bash
# Operator
docker build -t reposhift:dev -f Dockerfile .

# Platform API
docker build -t reposhift-platform:dev -f Dockerfile.platform .

# Web UI
docker build -t reposhift-web:dev -f web/Dockerfile web/
```

## Code Structure

```
├── cmd/
│   ├── main.go                 # Operator entrypoint
│   └── platform/main.go        # Platform API entrypoint
├── internal/
│   ├── api/                    # Operator HTTP API
│   ├── controller/             # K8s controllers
│   ├── services/               # Migration business logic
│   ├── platform/               # Multi-tenant platform layer
│   │   ├── auth/               # JWT + GitHub OAuth + middleware
│   │   ├── config/             # Mode detection (saas/selfhosted)
│   │   ├── tenant/             # Tenant/User models + store
│   │   ├── secrets/            # DB + K8s secrets providers
│   │   └── migration/          # Orchestrator + store
│   └── websocket/              # Real-time updates
├── api/v1/                     # CRD type definitions
├── web/                        # Next.js frontend
├── charts/                     # Helm charts
│   ├── ado-git-migration/      # Operator chart
│   ├── reposhift-platform/     # Platform API chart
│   └── reposhift-web/          # Web dashboard chart
├── migrations/                 # SQL schema + seed data
└── argocd/                     # ArgoCD app manifests
```

## Code Guidelines

- **Go**: Follow standard Go conventions. Run `go vet` and `go fmt`.
- **TypeScript**: Use TypeScript strict mode. Keep components in single files.
- **SQL**: Use `IF NOT EXISTS` and `ON CONFLICT DO NOTHING` for idempotency.
- **Helm**: All optional features must be conditional (`{{- if .Values.feature.enabled }}`).
- **Tests**: Add tests for new business logic. Test both include and exclude paths.

## Review Process

1. All PRs require at least 1 approving review
2. CI must pass (Go tests + Docker builds)
3. PRs should be focused — one feature or fix per PR
4. Large changes should be discussed in an issue first

## License

By contributing, you agree that your contributions will be licensed under the project's license.

## Questions?

Open a [Discussion](https://github.com/tesserix/reposhift/discussions) or reach out via Issues.
