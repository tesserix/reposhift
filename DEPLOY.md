# Reposhift Deployment Guide

Reposhift supports two deployment modes:
- **SaaS** — Multi-tenant, GitHub OAuth required, secrets encrypted in DB
- **Self-Hosted** — Single-tenant, admin token or GitHub OAuth, secrets in Kubernetes

---

## Self-Hosted Deployment

### Prerequisites

- Kubernetes cluster (1.26+)
- PostgreSQL database
- Helm 3
- `kubectl` access to the cluster

### Quick Start

```bash
# 1. Create a namespace
kubectl create namespace reposhift

# 2. Generate secrets
export ADMIN_TOKEN=$(openssl rand -hex 32)
export DB_PASSWORD="your-postgres-password"

# 3. Deploy with Helm
helm install reposhift-platform ./charts/reposhift-platform \
  -n reposhift \
  -f charts/reposhift-platform/values-selfhosted.yaml \
  --set adminToken=$ADMIN_TOKEN \
  --set postgresPassword=$DB_PASSWORD \
  --set postgresql.host=your-db-host

# 4. Access the UI
kubectl port-forward -n reposhift svc/reposhift-platform 8090:8090
# Open http://localhost:3005 (frontend) or use the API directly
```

### Authentication Options

#### Option A: Admin Token Only (Simplest)

Set `adminToken` — this gives you a single admin user with full access.

```bash
helm install reposhift-platform ./charts/reposhift-platform \
  -f charts/reposhift-platform/values-selfhosted.yaml \
  --set adminToken=$(openssl rand -hex 32)
```

Use the token in API calls:
```bash
curl -H "Authorization: Bearer $ADMIN_TOKEN" http://localhost:8090/platform/v1/tenant
```

#### Option B: GitHub OAuth (Multi-User)

Create a GitHub OAuth App at https://github.com/settings/developers, then:

```bash
helm install reposhift-platform ./charts/reposhift-platform \
  -f charts/reposhift-platform/values-selfhosted.yaml \
  --set github.clientID=Iv1.abc123 \
  --set github.clientSecret=secret123 \
  --set github.redirectURL=https://reposhift.example.com/api/platform/v1/auth/github/callback \
  --set jwtSecret=$(openssl rand -hex 32)
```

#### Option C: Both (Recommended)

Use admin token for CLI/automation and GitHub OAuth for the web UI.

### Environment Variables

| Variable | Required | Mode | Description |
|----------|----------|------|-------------|
| `REPOSHIFT_MODE` | Yes | Both | `saas` or `selfhosted` |
| `ADMIN_TOKEN` | Self-hosted | Self-hosted | Static bearer token for admin access |
| `JWT_SECRET` | If OAuth | Both | Min 32 chars, for signing JWTs |
| `ENCRYPTION_KEY` | SaaS | SaaS | 64 hex chars (32 bytes) for AES-256-GCM |
| `POSTGRES_HOST` | Yes | Both | PostgreSQL host |
| `POSTGRES_PORT` | No | Both | Default: 5432 |
| `POSTGRES_USER` | Yes | Both | Database user |
| `POSTGRES_PASSWORD` | Yes | Both | Database password |
| `POSTGRES_DATABASE` | Yes | Both | Database name |
| `POSTGRES_SSLMODE` | No | Both | Default: disable |
| `GITHUB_CLIENT_ID` | SaaS | Both | GitHub OAuth App client ID |
| `GITHUB_CLIENT_SECRET` | SaaS | Both | GitHub OAuth App client secret |
| `GITHUB_REDIRECT_URL` | SaaS | Both | OAuth callback URL |
| `K8S_NAMESPACE` | No | Both | Default: reposhift-system |
| `CORS_ALLOWED_ORIGINS` | No | Both | Default: http://localhost:3005 |

### Secrets Management

**Self-hosted with Kubernetes:** Secrets are stored as native K8s Secrets in the operator namespace, managed by Reposhift with labels for isolation.

**Self-hosted without K8s access:** Falls back to DB-encrypted storage (requires `ENCRYPTION_KEY`).

### Docker Compose (Non-Kubernetes)

```yaml
version: "3.8"
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: reposhift_db
      POSTGRES_USER: reposhift
      POSTGRES_PASSWORD: changeme
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  platform:
    image: ghcr.io/tesserix/reposhift-platform:latest
    ports:
      - "8090:8090"
    environment:
      REPOSHIFT_MODE: selfhosted
      ADMIN_TOKEN: your-admin-token-here
      POSTGRES_HOST: postgres
      POSTGRES_PORT: "5432"
      POSTGRES_USER: reposhift
      POSTGRES_PASSWORD: changeme
      POSTGRES_DATABASE: reposhift_db
      ENCRYPTION_KEY: "$(openssl rand -hex 32)"
      K8S_NAMESPACE: default
    depends_on:
      - postgres

  frontend:
    image: ghcr.io/tesserix/reposhift-web:latest
    ports:
      - "3005:3005"
    environment:
      PLATFORM_API_URL: http://platform:8090

volumes:
  pgdata:
```

---

## SaaS Deployment

### Prerequisites

- Kubernetes cluster with the Reposhift operator installed
- PostgreSQL database
- GitHub OAuth App configured
- Domain with TLS

### Deploy

```bash
helm install reposhift-platform ./charts/reposhift-platform \
  -n reposhift \
  --set mode=saas \
  --set jwtSecret=$(openssl rand -hex 32) \
  --set encryptionKey=$(openssl rand -hex 32) \
  --set postgresPassword=$DB_PASSWORD \
  --set github.clientID=Iv1.abc123 \
  --set github.clientSecret=secret123 \
  --set github.redirectURL=https://reposhift.example.com/api/platform/v1/auth/github/callback \
  --set postgresql.host=your-db-host \
  --set postgresql.sslmode=require
```

### Multi-Tenancy

In SaaS mode:
- Each GitHub user who signs in gets their own tenant
- Secrets are AES-256-GCM encrypted per-tenant in PostgreSQL
- Migrations are isolated per-tenant via K8s namespace scoping
- Tenant members can be invited by the owner

---

## API Reference

### Public Endpoints (No Auth)

```
GET  /platform/v1/config/mode          # Returns deployment mode and auth options
POST /platform/v1/auth/github          # Initiate GitHub OAuth (SaaS)
GET  /platform/v1/auth/github/callback # OAuth callback
```

### Protected Endpoints (Bearer Token)

```
# Tenant
GET  /platform/v1/tenant               # Get current tenant
PUT  /platform/v1/tenant               # Update tenant settings
GET  /platform/v1/tenant/members       # List members

# Secrets
GET  /platform/v1/secrets              # List all secrets (metadata only)
POST /platform/v1/secrets              # Create a secret
GET  /platform/v1/secrets/:name        # Get secret metadata
PUT  /platform/v1/secrets/:name        # Update secret data
DELETE /platform/v1/secrets/:name      # Delete a secret
POST /platform/v1/secrets/:name/validate  # Test secret connectivity

# Migrations
GET  /platform/v1/migrations           # List migrations
POST /platform/v1/migrations           # Create migration
GET  /platform/v1/migrations/:id       # Get migration status
DELETE /platform/v1/migrations/:id     # Delete migration
POST /platform/v1/migrations/:id/pause
POST /platform/v1/migrations/:id/resume
POST /platform/v1/migrations/:id/cancel
POST /platform/v1/migrations/:id/retry

# Dashboard
GET  /platform/v1/dashboard/stats      # Migration statistics
```

### Secret Data Format

When creating secrets, the `data` field varies by type:

| Type | Required Keys | Optional Keys |
|------|--------------|---------------|
| `ado_pat` | `token` | `organization` |
| `github_token` | `token` | `owner` |
| `github_app` | `app_id`, `installation_id`, `private_key` | — |
| `azure_sp` | `client_id`, `client_secret`, `tenant_id` | `organization` |
