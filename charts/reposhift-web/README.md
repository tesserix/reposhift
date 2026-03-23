# Reposhift Web — Helm Chart

The Reposhift web dashboard. A Next.js frontend that connects to the Reposhift Platform API.

## Install

```bash
helm install reposhift-web ./charts/reposhift-web \
  -n reposhift --create-namespace \
  --set platformApiUrl=http://reposhift-platform:8090
```

## Parameters

### General

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full release name | `""` |
| `replicaCount` | Number of frontend pods | `1` |
| `port` | HTTP port | `3005` |
| `platformApiUrl` | Backend platform API URL (internal) | `http://reposhift-platform:8090` |

### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | Container image | `ghcr.io/tesserix/reposhift-web` |
| `image.tag` | Image tag | `latest` |
| `image.pullPolicy` | Pull policy | `Always` |

### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.requests.memory` | Memory request | `128Mi` |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.limits.memory` | Memory limit | `512Mi` |
| `resources.limits.cpu` | CPU limit | `500m` |

### Service Account

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceAccount.create` | Create a ServiceAccount | `true` |
| `serviceAccount.name` | SA name override | `""` |
| `serviceAccount.annotations` | SA annotations | `{}` |

### Ingress

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable Kubernetes Ingress | `false` |
| `ingress.host` | Hostname | `""` |
| `ingress.className` | Ingress class name | `""` |
| `ingress.annotations` | Additional annotations | `{}` |
| `ingress.tls` | Enable TLS | `false` |

### Scheduling

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nodeSelector` | Node selector labels | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Affinity rules | `{}` |
| `env` | Additional environment variables | `{}` |

## Pages

| Route | Description |
|-------|-------------|
| `/login` | Admin token login |
| `/` | Dashboard — migration stats, recent activity |
| `/migrations` | List all migrations with status/progress |
| `/migrations/new` | Create migration wizard with branch filtering |
| `/migrations/[id]` | Migration detail with real-time progress |
| `/secrets` | Manage ADO PATs, GitHub tokens with validate/test |

## Architecture

```
Browser -> Reposhift Web (Next.js :3005)
            | /api/platform/* (proxy)
          Reposhift Platform (Go :8090)
            | K8s API
          Reposhift Operator (Go :8080)
            | CRDs
          Migration Jobs
```

The Next.js app proxies all `/api/platform/*` requests to the platform backend. This avoids CORS issues and keeps the backend URL internal.
