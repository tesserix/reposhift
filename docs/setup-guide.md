# Setup Guide

This guide walks through a complete Reposhift installation on Kubernetes, from prerequisites to your first migration.

---

## Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| Kubernetes | 1.26+ | Any distribution (GKE, EKS, AKS, k3s, minikube) |
| Helm | 3.x | Used to install all three charts |
| PostgreSQL | 14+ | Managed service recommended; bundled option available |
| kubectl | 1.26+ | Configured for your target cluster |
| Git | 2.30+ | Required on your local machine for troubleshooting |

---

## Step 1: Create the Namespace

```bash
kubectl create namespace reposhift
```

All Reposhift components will run in this namespace. You can choose a different name, but you must pass `--namespace <name>` to every Helm install command below.

---

## Step 2: Set Up PostgreSQL

Reposhift Platform API stores migration state, secrets metadata, and audit logs in PostgreSQL.

### Option A: Install PostgreSQL with Helm (quick start)

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update

helm install reposhift-db bitnami/postgresql \
  --namespace reposhift \
  --set auth.postgresPassword="changeme-pg-admin" \
  --set auth.database="reposhift_db" \
  --set auth.username="reposhift_user" \
  --set auth.password="changeme-pg-user" \
  --set primary.persistence.size=10Gi
```

Wait for the pod to become ready:

```bash
kubectl get pods -n reposhift -l app.kubernetes.io/name=postgresql --watch
```

The internal hostname will be `reposhift-db-postgresql.reposhift.svc.cluster.local`.

### Option B: Use an Existing PostgreSQL Instance

If you already have a managed PostgreSQL (Cloud SQL, RDS, Azure Database), create the database and user:

```sql
CREATE DATABASE reposhift_db;
CREATE USER reposhift_user WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE reposhift_db TO reposhift_user;
```

Note the host, port, and SSL mode for step 5.

---

## Step 3: Generate the Admin Token

The admin token is used to log in to the Reposhift dashboard and to authenticate API calls.

```bash
ADMIN_TOKEN=$(openssl rand -hex 32)
echo "Save this token securely: $ADMIN_TOKEN"
```

Store this value somewhere safe. You will need it for the Platform API install and for logging in to the web UI.

---

## Step 4: Install the Operator

The operator watches for migration CRDs and executes the actual clone/push operations.

```bash
helm install reposhift-operator reposhift/ado-git-migration \
  --namespace reposhift \
  --set image.repository=ghcr.io/tesserix/reposhift \
  --set image.tag=latest \
  --set operator.logLevel=info \
  --set persistence.enabled=true \
  --set persistence.size=250Gi \
  --set persistence.storageClassName=standard
```

Verify the operator is running:

```bash
kubectl get pods -n reposhift -l app.kubernetes.io/name=ado-git-migration
```

### Operator Values Reference

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `ghcr.io/tesserix/reposhift` | Operator container image |
| `image.tag` | `latest` | Image tag |
| `operator.logLevel` | `info` | Log level: debug, info, warn, error |
| `operator.enableLeaderElection` | `true` | HA leader election |
| `operator.http.port` | `8080` | HTTP server port |
| `operator.metrics.port` | `8082` | Prometheus metrics port |
| `operator.health.port` | `8081` | Health probe port |
| `operator.rateLimits.azureDevOps` | `60` | ADO requests per minute |
| `operator.rateLimits.github` | `5000` | GitHub requests per hour |
| `operator.defaultSettings.parallelWorkers` | `5` | Default concurrent workers |
| `operator.defaultSettings.retryAttempts` | `3` | Default retry count |
| `operator.defaultSettings.batchSize` | `10` | Default batch size |
| `persistence.enabled` | `true` | Enable PVC for migration workspace |
| `persistence.size` | `250Gi` | PVC size |
| `persistence.storageClassName` | `managed-premium` | Storage class |
| `persistence.mountPath` | `/workspace/migrations` | Mount path inside container |
| `resources.requests.cpu` | `250m` | CPU request |
| `resources.requests.memory` | `512Mi` | Memory request |
| `resources.limits.cpu` | `1000m` | CPU limit |
| `resources.limits.memory` | `2Gi` | Memory limit |
| `autoscaling.enabled` | `true` | Enable HPA |
| `autoscaling.minReplicas` | `1` | Minimum replicas |
| `autoscaling.maxReplicas` | `5` | Maximum replicas |
| `auth.github.token` | `""` | GitHub PAT (Option A) |
| `auth.githubApp.appId` | `""` | GitHub App ID (Option B) |
| `auth.githubApp.installationId` | `""` | GitHub App Installation ID |
| `auth.githubApp.privateKey` | `""` | Base64-encoded private key PEM |

---

## Step 5: Install the Platform API

The Platform API provides the REST interface between the web UI and the operator.

```bash
helm install reposhift-platform reposhift/reposhift-platform \
  --namespace reposhift \
  --set adminToken="$ADMIN_TOKEN" \
  --set postgresPassword="changeme-pg-user" \
  --set postgresql.host="reposhift-db-postgresql.reposhift.svc.cluster.local" \
  --set postgresql.port=5432 \
  --set postgresql.database="reposhift_db" \
  --set postgresql.user="reposhift_user" \
  --set postgresql.sslmode="disable" \
  --set operatorURL="http://reposhift-operator-ado-git-migration:8080"
```

Verify:

```bash
kubectl get pods -n reposhift -l app.kubernetes.io/name=reposhift-platform
```

### Platform API Values Reference

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `ghcr.io/tesserix/reposhift-platform` | Platform API image |
| `image.tag` | `latest` | Image tag |
| `port` | `8090` | HTTP port |
| `adminToken` | `""` | Admin authentication token |
| `postgresPassword` | `""` | PostgreSQL password |
| `postgresql.host` | `""` | PostgreSQL host |
| `postgresql.port` | `5432` | PostgreSQL port |
| `postgresql.database` | `reposhift_db` | Database name |
| `postgresql.user` | `reposhift_user` | Database user |
| `postgresql.sslmode` | `require` | SSL mode (disable, require, verify-full) |
| `jwtSecret` | `""` | JWT signing secret (auto-generated if empty) |
| `encryptionKey` | `""` | Encryption key for stored secrets |
| `operatorURL` | `http://ado-git-migration:8080` | Internal URL of the operator |
| `cors.allowedOrigins` | `*` | CORS allowed origins |
| `mode` | `saas` | Operating mode |

---

## Step 6: Install the Web UI

```bash
helm install reposhift-web reposhift/reposhift-web \
  --namespace reposhift \
  --set platformApiUrl="http://reposhift-platform:8090"
```

Verify:

```bash
kubectl get pods -n reposhift -l app.kubernetes.io/name=reposhift-web
```

### Web UI Values Reference

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `ghcr.io/tesserix/reposhift-web` | Web UI image |
| `image.tag` | `latest` | Image tag |
| `port` | `3005` | HTTP port |
| `platformApiUrl` | `http://reposhift-platform:8090` | Platform API internal URL |
| `resources.requests.cpu` | `100m` | CPU request |
| `resources.requests.memory` | `128Mi` | Memory request |
| `resources.limits.cpu` | `500m` | CPU limit |
| `resources.limits.memory` | `512Mi` | Memory limit |

---

## Step 7: Access the Dashboard

### Option A: Port Forwarding (quick access)

```bash
kubectl port-forward svc/reposhift-web 3005:3005 -n reposhift
```

Open `http://localhost:3005` in your browser.

### Option B: Ingress

Add ingress configuration to the web chart:

```bash
helm upgrade reposhift-web reposhift/reposhift-web \
  --namespace reposhift \
  --set ingress.enabled=true \
  --set ingress.host="reposhift.example.com" \
  --set ingress.className="nginx" \
  --set ingress.tls=true
```

### Option C: Istio VirtualService

If your cluster uses Istio, enable the Istio integration:

```bash
helm upgrade reposhift-web reposhift/reposhift-web \
  --namespace reposhift \
  --set istio.enabled=true \
  --set istio.gateway="istio-ingress/my-gateway" \
  --set istio.host="reposhift.example.com"
```

---

## Step 8: Log In

Open the dashboard and enter the admin token you generated in Step 3.

---

## Step 9: Add Secrets

In the dashboard, navigate to **Secrets** and add your credentials.

### Azure DevOps PAT

1. In Azure DevOps, go to **User Settings** > **Personal Access Tokens**
2. Create a token with these scopes:
   - **Code**: Read
   - **Work Items**: Read (if migrating work items)
   - **Build**: Read (if migrating pipelines)
3. In the Reposhift dashboard, add a new ADO secret with the token value

### GitHub Authentication

Choose one of:

- **GitHub App** (recommended) -- See [docs/github-app-setup.md](github-app-setup.md)
- **Personal Access Token** -- Create a classic PAT with `repo` and `admin:org` scopes

---

## Step 10: Create Your First Migration

1. In the dashboard, click **New Migration**
2. Select the source ADO organization and project
3. Select the target GitHub organization
4. Choose repositories to migrate
5. Configure settings (history depth, branch filters, parallel workers)
6. Click **Start Migration**

Monitor progress in real time on the dashboard, or via kubectl:

```bash
kubectl get adotogitmigration -n reposhift --watch
```

---

## Docker Compose Alternative

For local development or evaluation without Kubernetes, you can run Reposhift with Docker Compose.

Create a `docker-compose.yml`:

```yaml
version: "3.9"

services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: reposhift_db
      POSTGRES_USER: reposhift_user
      POSTGRES_PASSWORD: changeme
    ports:
      - "5432:5432"
    volumes:
      - pg_data:/var/lib/postgresql/data

  platform:
    image: ghcr.io/tesserix/reposhift-platform:latest
    ports:
      - "8090:8090"
    environment:
      DATABASE_HOST: postgres
      DATABASE_PORT: "5432"
      DATABASE_NAME: reposhift_db
      DATABASE_USER: reposhift_user
      DATABASE_PASSWORD: changeme
      DATABASE_SSLMODE: disable
      ADMIN_TOKEN: your-admin-token-here
    depends_on:
      - postgres

  web:
    image: ghcr.io/tesserix/reposhift-web:latest
    ports:
      - "3005:3005"
    environment:
      PLATFORM_API_URL: http://platform:8090
    depends_on:
      - platform

volumes:
  pg_data:
```

```bash
docker compose up -d
```

Open `http://localhost:3005` and log in with the admin token.

Note: The Docker Compose setup does not include the Kubernetes operator. CRD-based migrations require a Kubernetes cluster. The Platform API can still be used to manage secrets and trigger migrations via the REST API.

---

## Next Steps

- [Set up a GitHub App](github-app-setup.md) for production-grade authentication
- [Review migration patterns](migration-patterns.md) to plan your migration strategy
- [Tune performance](performance-tuning.md) for large-scale migrations
