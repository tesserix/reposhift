# Reposhift Platform — Helm Chart

The Reposhift Platform API backend. Manages secrets and migrations on top of the Reposhift operator.

## Install

```bash
helm install reposhift-platform ./charts/reposhift-platform \
  -n reposhift --create-namespace \
  --set adminToken=$(openssl rand -hex 32) \
  --set postgresPassword=<db-password>
```

## Parameters

### General

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full release name | `""` |
| `replicaCount` | Number of platform pods | `1` |
| `port` | HTTP port for the platform API | `8090` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Container image | `ghcr.io/tesserix/reposhift-platform` |
| `image.tag` | Image tag | `latest` |
| `image.pullPolicy` | Pull policy | `IfNotPresent` |
| `imagePullSecrets` | Image pull secrets | `[]` |

### Secrets (set via `--set` or external secrets)

| Parameter | Description | Required |
|-----------|-------------|----------|
| `adminToken` | Admin bearer token (generate with `openssl rand -hex 32`) | Yes |
| `postgresPassword` | PostgreSQL password | Yes |

### PostgreSQL

| Parameter | Description | Default |
|-----------|-------------|---------|
| `postgresql.host` | Database host | `localhost` |
| `postgresql.port` | Database port | `5432` |
| `postgresql.database` | Database name | `reposhift_db` |
| `postgresql.user` | Database user | `reposhift_user` |
| `postgresql.sslmode` | SSL mode | `disable` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a ServiceAccount | `true` |
| `serviceAccount.annotations` | SA annotations (e.g. Workload Identity) | `{}` |

### Ingress

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable Kubernetes Ingress | `false` |
| `ingress.className` | Ingress class name | `""` |
| `ingress.annotations` | Additional annotations | `{}` |
| `ingress.hosts` | List of `{host}` entries | `[]` |
| `ingress.tls` | TLS config `[{secretName, hosts}]` | `[]` |

### Other

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cors.allowedOrigins` | CORS allowed origins | `http://localhost:3005` |
| `operatorURL` | Reposhift operator API URL | `http://reposhift-operator:8080` |
| `k8sNamespace` | Namespace for migration CRDs and secrets | `reposhift` |
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.limits.cpu` | CPU limit | `500m` |

## API Endpoints

```
GET    /health                              Health check
GET    /ready                               Readiness check
GET    /platform/v1/secrets                 List secrets
POST   /platform/v1/secrets                 Create secret
GET    /platform/v1/secrets/:name           Get secret metadata
PUT    /platform/v1/secrets/:name           Update secret
DELETE /platform/v1/secrets/:name           Delete secret
POST   /platform/v1/secrets/:name/validate  Test secret connectivity
GET    /platform/v1/migrations              List migrations
POST   /platform/v1/migrations              Create migration
GET    /platform/v1/migrations/:id          Migration status
DELETE /platform/v1/migrations/:id          Delete migration
POST   /platform/v1/migrations/:id/pause    Pause
POST   /platform/v1/migrations/:id/resume   Resume
POST   /platform/v1/migrations/:id/cancel   Cancel
POST   /platform/v1/migrations/:id/retry    Retry
GET    /platform/v1/dashboard/stats         Dashboard stats
```
