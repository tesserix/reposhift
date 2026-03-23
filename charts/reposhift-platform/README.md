# Reposhift Platform — Helm Chart

The Reposhift Platform API backend. Manages tenants, secrets, and migrations on top of the Reposhift operator.

## Install

```bash
# SaaS mode (multi-tenant, GitHub OAuth required)
helm install reposhift-platform ./charts/reposhift-platform \
  -n git-migrator --create-namespace \
  --set jwtSecret=$(openssl rand -hex 32) \
  --set encryptionKey=$(openssl rand -hex 32) \
  --set postgresPassword=<db-password> \
  --set github.clientID=<id> \
  --set github.clientSecret=<secret> \
  --set github.redirectURL=https://reposhift.example.com/api/platform/v1/auth/github/callback

# Self-hosted mode (single-tenant, admin token)
helm install reposhift-platform ./charts/reposhift-platform \
  -n git-migrator --create-namespace \
  -f charts/reposhift-platform/values-selfhosted.yaml \
  --set adminToken=$(openssl rand -hex 32) \
  --set postgresPassword=<db-password>
```

## ArgoCD

```bash
kubectl apply -f argocd/apps/reposhift-platform.yaml
```

## Database Migrations

Migrations run automatically as a Helm pre-install/pre-upgrade hook. The job:
1. Waits for PostgreSQL to be ready (init container)
2. Runs all `.up.sql` files in order via `psql`

## Parameters

### General

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full release name | `""` |
| `replicaCount` | Number of platform pods | `1` |
| `mode` | Deployment mode: `saas` or `selfhosted` | `saas` |
| `port` | HTTP port for the platform API | `8090` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Container image | `ghcr.io/tesserix/reposhift-platform` |
| `image.tag` | Image tag | `latest` |
| `image.pullPolicy` | Pull policy | `IfNotPresent` |

### Secrets (set via `--set` or external secrets)

| Parameter | Description | Required |
|-----------|-------------|----------|
| `jwtSecret` | JWT signing secret (min 32 chars) | SaaS + OAuth modes |
| `encryptionKey` | AES-256-GCM key (64 hex chars) | SaaS mode |
| `adminToken` | Static admin bearer token | Self-hosted (no OAuth) |
| `postgresPassword` | PostgreSQL password | Always |

### PostgreSQL

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgresql.host` | Database host | `10.117.240.3` |
| `postgresql.port` | Database port | `5432` |
| `postgresql.database` | Database name | `reposhift_db` |
| `postgresql.user` | Database user | `reposhift_user` |
| `postgresql.sslmode` | SSL mode | `require` |

### GitHub OAuth

| Parameter | Description | Default |
|-----------|-------------|---------|
| `github.clientID` | GitHub OAuth App client ID | `""` |
| `github.clientSecret` | GitHub OAuth App client secret | `""` |
| `github.redirectURL` | OAuth callback URL | `""` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a ServiceAccount | `true` |
| `serviceAccount.annotations` | SA annotations (e.g. Workload Identity) | `{}` |

### Ingress (NGINX)

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable Kubernetes Ingress | `false` |
| `ingress.className` | Ingress class name | `""` |
| `ingress.annotations` | Additional annotations | `{}` |
| `ingress.hosts` | List of `{host}` entries | `[]` |
| `ingress.tls` | TLS config `[{secretName, hosts}]` | `[]` |

### Istio VirtualService

| Parameter | Description | Default |
|-----------|-------------|---------|
| `istio.enabled` | Enable Istio VirtualService | `false` |
| `istio.gateway` | Istio Gateway reference | `istio-system/default-gateway` |
| `istio.host` | Hostname for routing | `reposhift.example.com` |

### Other

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cors.allowedOrigins` | CORS allowed origins | `*` |
| `operatorURL` | Reposhift operator API URL | `http://ado-git-migration:8080` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `env` | Additional environment variables | `{}` |

## Modes

### SaaS (Multi-Tenant)

Required: `jwtSecret`, `encryptionKey`, `postgresPassword`, `github.*`

Each user signs in via GitHub OAuth, gets their own tenant. Secrets are AES-256-GCM encrypted in PostgreSQL.

### Self-Hosted (Single-Tenant)

Required: `adminToken` OR `github.*`, plus `postgresPassword`

A default tenant and admin user are auto-created on startup. Secrets stored in Kubernetes Secrets or DB (with `encryptionKey`).

## API Endpoints

```
GET  /health                              Health check
GET  /ready                               Readiness check
GET  /platform/v1/config/mode             Deployment mode (public)
POST /platform/v1/auth/github             GitHub OAuth initiation
GET  /platform/v1/auth/github/callback    OAuth callback
POST /platform/v1/auth/refresh            Refresh JWT
GET  /platform/v1/tenant                  Current tenant
PUT  /platform/v1/tenant                  Update tenant
GET  /platform/v1/tenant/members          List members
GET  /platform/v1/secrets                 List secrets
POST /platform/v1/secrets                 Create secret
GET  /platform/v1/secrets/:name           Get secret metadata
PUT  /platform/v1/secrets/:name           Update secret
DELETE /platform/v1/secrets/:name         Delete secret
POST /platform/v1/secrets/:name/validate  Test secret connectivity
GET  /platform/v1/migrations              List migrations
POST /platform/v1/migrations              Create migration
GET  /platform/v1/migrations/:id          Migration status
DELETE /platform/v1/migrations/:id        Delete migration
POST /platform/v1/migrations/:id/pause    Pause
POST /platform/v1/migrations/:id/resume   Resume
POST /platform/v1/migrations/:id/cancel   Cancel
POST /platform/v1/migrations/:id/retry    Retry
GET  /platform/v1/dashboard/stats         Dashboard stats
```
