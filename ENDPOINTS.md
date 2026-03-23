# API Endpoints Reference

Complete reference for all HTTP endpoints provided by the ADO to GitHub Migration Operator.

**Base URL:** `http://localhost:8080` (default)

---

## 📡 Real-Time Endpoints

### Server-Sent Events (SSE)

#### Get Real-Time Events Stream
```http
GET /api/v1/events
```

**Description:** Establishes an SSE connection for real-time migration updates.

**Response:** Event stream (text/event-stream)

**Event Types:**
- `migration_update` - Migration progress updates
- `project_update` - GitHub project updates
- `workitem_update` - Work item migration updates
- `system_update` - System status and heartbeat

**Example Event:**
```
event: migration_update
data: {"type":"migration_update","id":"authority-batch-test","data":{"phase":"Running","progress":{"total":2,"completed":1,"progressSummary":"1/2"}},"timestamp":"2025-11-14T01:30:00Z"}
id: authority-batch-test
```

**Usage:**
```javascript
const eventSource = new EventSource('http://localhost:8080/api/v1/events');
eventSource.addEventListener('migration_update', (event) => {
  const data = JSON.parse(event.data);
  console.log('Migration update:', data);
});
```

### WebSocket

#### WebSocket Connection
```http
GET /ws/migrations
```

**Description:** WebSocket connection for bidirectional communication.

**Protocol:** ws:// or wss://

**Usage:**
```javascript
const ws = new WebSocket('ws://localhost:8080/ws/migrations');
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Message:', data);
};
```

---

## 🔍 Discovery Endpoints

### Resource Discovery

#### Discover Organizations
```http
GET /api/v1/discovery/organizations
```

**Query Parameters:**
- `ado_url` (required) - Azure DevOps organization URL
- `pat` (required) - Personal Access Token

**Response:**
```json
{
  "organizations": [
    {
      "id": "org-id",
      "name": "my-ado-org",
      "url": "https://dev.azure.com/my-ado-org"
    }
  ]
}
```

#### Discover Projects
```http
GET /api/v1/discovery/projects?organization=my-ado-org
```

**Query Parameters:**
- `organization` (required) - Organization name
- `pat` (required) - Personal Access Token

**Response:**
```json
{
  "projects": [
    {
      "id": "project-id",
      "name": "Authority",
      "description": "Authority project",
      "state": "wellFormed"
    }
  ]
}
```

#### Discover Repositories
```http
GET /api/v1/discovery/repositories?organization=my-org&project=Authority
```

**Query Parameters:**
- `organization` (required)
- `project` (required)
- `pat` (required)

**Response:**
```json
{
  "repositories": [
    {
      "id": "repo-id",
      "name": "java-authority",
      "url": "https://dev.azure.com/my-org/Authority/_git/java-authority",
      "defaultBranch": "main",
      "size": 12345678
    }
  ]
}
```

#### Discover Work Items
```http
GET /api/v1/discovery/workitems?organization=my-org&project=Authority
```

**Query Parameters:**
- `organization` (required)
- `project` (required)
- `pat` (required)
- `type` (optional) - Filter by work item type

**Response:**
```json
{
  "workItems": [
    {
      "id": 123,
      "type": "User Story",
      "title": "Implement feature X",
      "state": "Active",
      "assignedTo": "user@example.com"
    }
  ]
}
```

#### Discover Pipelines
```http
GET /api/v1/discovery/pipelines?organization=my-org&project=Authority
```

#### Discover Builds
```http
GET /api/v1/discovery/builds?organization=my-org&project=Authority
```

#### Discover Releases
```http
GET /api/v1/discovery/releases?organization=my-org&project=Authority
```

#### Discover Teams
```http
GET /api/v1/discovery/teams?organization=my-org&project=Authority
```

#### Discover Users
```http
GET /api/v1/discovery/users?organization=my-org
```

### Discovery Management

#### List All Discoveries
```http
GET /api/v1/discovery
```

**Response:**
```json
{
  "discoveries": [
    {
      "id": "discovery-1",
      "type": "repositories",
      "status": "completed",
      "createdAt": "2025-11-14T01:00:00Z"
    }
  ]
}
```

#### Create Discovery
```http
POST /api/v1/discovery
```

**Request Body:**
```json
{
  "type": "repositories",
  "organization": "my-ado-org",
  "project": "Authority",
  "credentials": {
    "pat": "your-pat-token"
  }
}
```

#### Get Discovery
```http
GET /api/v1/discovery/{id}
```

#### Update Discovery
```http
PUT /api/v1/discovery/{id}
```

#### Delete Discovery
```http
DELETE /api/v1/discovery/{id}
```

#### Get Discovery Status
```http
GET /api/v1/discovery/{id}/status
```

**Response:**
```json
{
  "id": "discovery-1",
  "status": "completed",
  "progress": 100,
  "itemsFound": 25,
  "completedAt": "2025-11-14T01:05:00Z"
}
```

#### Get Discovery Results
```http
GET /api/v1/discovery/{id}/results
```

---

## 🚀 Migration Endpoints

### Migration Management

#### List Migrations
```http
GET /api/v1/migrations
```

**Query Parameters:**
- `status` (optional) - Filter by status (Pending, Running, Completed, Failed)
- `type` (optional) - Filter by type (repository, workitems, pipelines, all)
- `limit` (optional) - Number of results (default: 50)
- `offset` (optional) - Pagination offset

**Response:**
```json
{
  "migrations": [
    {
      "id": "authority-batch-test",
      "type": "repository",
      "phase": "Running",
      "progress": {
        "total": 2,
        "completed": 1,
        "failed": 0,
        "progressSummary": "1/2",
        "percentage": 50
      },
      "startTime": "2025-11-14T01:00:00Z"
    }
  ],
  "total": 10,
  "limit": 50,
  "offset": 0
}
```

#### Create Migration
```http
POST /api/v1/migrations
```

**Request Body:**
```json
{
  "name": "my-migration",
  "type": "repository",
  "source": {
    "organization": "my-ado-org",
    "project": "Authority",
    "repository": "java-authority"
  },
  "target": {
    "owner": "my-org",
    "repository": "java-authority"
  },
  "settings": {
    "batchSize": 50,
    "parallelWorkers": 3,
    "retryAttempts": 5
  }
}
```

#### Get Migration
```http
GET /api/v1/migrations/{id}
```

**Response:**
```json
{
  "id": "authority-batch-test",
  "name": "authority-batch-test",
  "type": "repository",
  "phase": "Running",
  "progress": {
    "total": 2,
    "completed": 1,
    "failed": 0,
    "processing": 1,
    "progressSummary": "1/2",
    "percentage": 50,
    "currentStep": "Processing repository: java-authority",
    "currentItem": 1
  },
  "resourceStatuses": [
    {
      "sourceName": "java-authority",
      "targetName": "java-authority",
      "status": "Completed",
      "progress": 100
    }
  ],
  "startTime": "2025-11-14T01:00:00Z",
  "lastReconcileTime": "2025-11-14T01:05:00Z"
}
```

#### Update Migration
```http
PUT /api/v1/migrations/{id}
```

#### Delete Migration
```http
DELETE /api/v1/migrations/{id}
```

#### Get Migration Status
```http
GET /api/v1/migrations/{id}/status
```

**Response:**
```json
{
  "phase": "Running",
  "progress": {
    "progressSummary": "1/2",
    "percentage": 50
  },
  "startTime": "2025-11-14T01:00:00Z",
  "estimatedCompletion": "2025-11-14T01:10:00Z"
}
```

#### Get Migration Progress
```http
GET /api/v1/migrations/{id}/progress
```

**Response:**
```json
{
  "total": 2,
  "completed": 1,
  "failed": 0,
  "processing": 1,
  "skipped": 0,
  "percentage": 50,
  "progressSummary": "1/2",
  "currentItem": 1,
  "currentStep": "Processing repository: java-authority",
  "processingRate": "12 items/min"
}
```

#### Get Migration Logs
```http
GET /api/v1/migrations/{id}/logs
```

**Query Parameters:**
- `limit` (optional) - Number of log entries (default: 100)
- `level` (optional) - Filter by log level (info, warning, error)

**Response:**
```json
{
  "logs": [
    {
      "timestamp": "2025-11-14T01:05:00Z",
      "level": "info",
      "message": "Successfully cloned repository: java-authority"
    }
  ]
}
```

### Migration Actions

#### Pause Migration
```http
POST /api/v1/migrations/{id}/pause
```

**Response:**
```json
{
  "message": "Migration paused successfully",
  "phase": "Paused"
}
```

#### Resume Migration
```http
POST /api/v1/migrations/{id}/resume
```

#### Cancel Migration
```http
POST /api/v1/migrations/{id}/cancel
```

#### Retry Migration
```http
POST /api/v1/migrations/{id}/retry
```

**Request Body (optional):**
```json
{
  "retryFailed": true,
  "resetProgress": false
}
```

#### Validate Migration
```http
POST /api/v1/migrations/{id}/validate
```

**Response:**
```json
{
  "valid": true,
  "warnings": [],
  "errors": []
}
```

---

## 🔄 Pipeline Conversion Endpoints

### Pipeline Management

#### List Pipeline Conversions
```http
GET /api/v1/pipelines
```

#### Create Pipeline Conversion
```http
POST /api/v1/pipelines
```

**Request Body:**
```json
{
  "name": "my-pipeline-conversion",
  "source": {
    "organization": "my-org",
    "project": "Authority",
    "pipelineId": 123
  },
  "target": {
    "owner": "my-org",
    "repository": "java-authority",
    "workflowName": "ci.yml"
  }
}
```

#### Get Pipeline Conversion
```http
GET /api/v1/pipelines/{id}
```

#### Update Pipeline Conversion
```http
PUT /api/v1/pipelines/{id}
```

#### Delete Pipeline Conversion
```http
DELETE /api/v1/pipelines/{id}
```

#### Get Pipeline Conversion Status
```http
GET /api/v1/pipelines/{id}/status
```

#### Preview Pipeline Conversion
```http
GET /api/v1/pipelines/{id}/preview
```

**Response:**
```yaml
name: CI Pipeline
on:
  push:
    branches: [ main ]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - name: Build
        run: npm run build
```

#### Validate Pipeline Conversion
```http
POST /api/v1/pipelines/{id}/validate
```

#### Download Converted Workflows
```http
GET /api/v1/pipelines/{id}/download
```

**Response:** ZIP file containing converted GitHub Actions workflows

### Pipeline Analysis

#### Analyze Pipeline
```http
GET /api/v1/pipelines/analyze?url={pipeline-url}
```

**Query Parameters:**
- `url` (required) - ADO pipeline URL
- `pat` (required) - Personal Access Token

**Response:**
```json
{
  "name": "CI Pipeline",
  "complexity": "medium",
  "tasks": [
    {
      "name": "NpmTask",
      "version": "1.x",
      "supported": true,
      "githubAction": "actions/setup-node@v2"
    }
  ],
  "estimatedConversionTime": "5 minutes"
}
```

#### Get Conversion Templates
```http
GET /api/v1/pipelines/templates
GET /api/v1/pipelines/templates/{type}
```

**Types:** `build`, `deploy`, `test`, `release`

#### Get Task Mappings
```http
GET /api/v1/pipelines/mappings
GET /api/v1/pipelines/mappings/{task_type}
```

**Response:**
```json
{
  "mappings": [
    {
      "adoTask": "NpmTask@1",
      "githubAction": "actions/setup-node@v2",
      "parameters": {
        "command": "script",
        "script": "npm run build"
      }
    }
  ]
}
```

---

## ✅ Validation Endpoints

### Credential Validation

#### Validate ADO Credentials
```http
POST /api/v1/validation/credentials/ado
```

**Request Body:**
```json
{
  "organization": "my-ado-org",
  "pat": "your-pat-token"
}
```

**Response:**
```json
{
  "valid": true,
  "message": "Credentials are valid",
  "scopes": [
    "vso.code",
    "vso.work"
  ]
}
```

#### Validate GitHub Credentials
```http
POST /api/v1/validation/credentials/github
```

**Request Body:**
```json
{
  "token": "ghp_xxxxx",
  "type": "pat"
}
```

or

```json
{
  "appId": "123456",
  "installationId": "789012",
  "privateKey": "-----BEGIN RSA PRIVATE KEY-----...",
  "type": "app"
}
```

### Permission Validation

#### Validate ADO Permissions
```http
POST /api/v1/validation/permissions/ado
```

**Request Body:**
```json
{
  "organization": "my-org",
  "project": "Authority",
  "pat": "your-pat",
  "requiredPermissions": [
    "Code (Read)",
    "Work Items (Read)"
  ]
}
```

**Response:**
```json
{
  "valid": true,
  "permissions": [
    {
      "name": "Code (Read)",
      "granted": true
    },
    {
      "name": "Work Items (Read)",
      "granted": true
    }
  ]
}
```

#### Validate GitHub Permissions
```http
POST /api/v1/validation/permissions/github
```

### Configuration Validation

#### Validate Migration Config
```http
POST /api/v1/validation/migration
```

**Request Body:**
```json
{
  "type": "repository",
  "source": {...},
  "target": {...},
  "settings": {...}
}
```

**Response:**
```json
{
  "valid": true,
  "warnings": [
    {
      "code": "LARGE_REPOSITORY",
      "message": "Repository size exceeds 1GB",
      "suggestion": "Consider using git-lfs"
    }
  ],
  "errors": []
}
```

#### Validate Pipeline Config
```http
POST /api/v1/validation/pipeline
```

#### Validate GitHub Repository
```http
POST /api/v1/validation/repository
```

**Request Body:**
```json
{
  "owner": "my-org",
  "repository": "java-authority",
  "token": "ghp_xxxxx"
}
```

**Response:**
```json
{
  "exists": true,
  "accessible": true,
  "permissions": {
    "admin": true,
    "push": true,
    "pull": true
  }
}
```

---

## 📊 Statistics & Metrics Endpoints

### Migration Statistics

#### Get Migration Statistics
```http
GET /api/v1/stats/migrations
```

**Query Parameters:**
- `from` (optional) - Start date (ISO 8601)
- `to` (optional) - End date (ISO 8601)
- `type` (optional) - Filter by migration type

**Response:**
```json
{
  "total": 150,
  "completed": 120,
  "failed": 10,
  "running": 20,
  "successRate": 0.92,
  "averageDuration": "15m30s",
  "byType": {
    "repository": 100,
    "workitems": 30,
    "pipelines": 20
  }
}
```

#### Get Pipeline Statistics
```http
GET /api/v1/stats/pipelines
```

#### Get Usage Statistics
```http
GET /api/v1/stats/usage
```

**Response:**
```json
{
  "totalMigrations": 150,
  "totalRepositories": 100,
  "totalWorkItems": 5000,
  "totalPipelines": 50,
  "storageUsed": "25GB",
  "apiCallsToday": 12500
}
```

#### Get Performance Metrics
```http
GET /api/v1/stats/performance
```

**Response:**
```json
{
  "averageCloneTime": "2m30s",
  "averagePushTime": "3m15s",
  "averageMigrationTime": "15m45s",
  "throughput": "8 repos/hour"
}
```

### Prometheus Metrics

#### Get Prometheus Metrics
```http
GET /metrics
```

**Format:** Prometheus text format

**Sample Metrics:**
```
# HELP ado_github_migration_operator_migrations_total Total number of migrations
# TYPE ado_github_migration_operator_migrations_total counter
ado_github_migration_operator_migrations_total{migration_type="repository",status="success"} 120
ado_github_migration_operator_migrations_total{migration_type="repository",status="failure"} 10

# HELP ado_github_migration_operator_migrations_active Active migrations
# TYPE ado_github_migration_operator_migrations_active gauge
ado_github_migration_operator_migrations_active{migration_type="repository",phase="Running"} 5

# HELP ado_github_migration_operator_migration_duration_seconds Migration duration
# TYPE ado_github_migration_operator_migration_duration_seconds histogram
ado_github_migration_operator_migration_duration_seconds_bucket{migration_type="repository",le="60"} 10
ado_github_migration_operator_migration_duration_seconds_bucket{migration_type="repository",le="300"} 50

# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",path="/api/v1/migrations",status="200"} 1234
```

---

## ⚙️ Utility Endpoints

### Health & Status

#### Health Check
```http
GET /api/v1/utils/health
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2025-11-14T01:00:00Z",
  "version": "1.0.0",
  "uptime": "24h30m15s"
}
```

#### Readiness Check
```http
GET /api/v1/utils/ready
```

**Response:**
```json
{
  "ready": true,
  "checks": {
    "kubernetes": "ok",
    "database": "ok",
    "cache": "ok"
  }
}
```

#### Get Version
```http
GET /api/v1/utils/version
```

**Response:**
```json
{
  "version": "1.0.0",
  "gitCommit": "abc123",
  "buildDate": "2025-11-14T00:00:00Z",
  "goVersion": "go1.21.5"
}
```

#### Get Configuration
```http
GET /api/v1/utils/config
```

**Response:**
```json
{
  "apiVersion": "v1",
  "features": {
    "discovery": true,
    "migration": true,
    "pipelineConversion": true
  },
  "limits": {
    "maxConcurrentMigrations": 10,
    "maxRepositorySize": "10GB",
    "rateLimitPerHour": 5000
  }
}
```

### Templates

#### Get All Templates
```http
GET /api/v1/utils/templates
```

**Response:**
```json
{
  "templates": [
    {
      "type": "repository",
      "name": "Basic Repository Migration",
      "description": "Migrate a single repository",
      "filename": "repository-migration.yaml"
    },
    {
      "type": "workitems",
      "name": "Work Item Migration",
      "description": "Migrate work items to GitHub issues",
      "filename": "workitem-migration.yaml"
    }
  ]
}
```

#### Get Specific Template
```http
GET /api/v1/utils/templates/{type}
```

**Types:** `repository`, `workitems`, `pipeline`, `batch`, `complete`

**Response:**
```yaml
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: example-migration
spec:
  type: repository
  source:
    organization: your-org
    project: your-project
  ...
```

---

## 🔐 Authentication

Most endpoints require authentication via:

### Option 1: Query Parameters
```http
GET /api/v1/migrations?ado_pat=xxxxx&github_token=yyyyy
```

### Option 2: Headers
```http
Authorization: Bearer github_pat_xxxxx
X-ADO-PAT: your-ado-pat
```

### Option 3: Request Body
```json
{
  "credentials": {
    "ado": {
      "pat": "your-ado-pat"
    },
    "github": {
      "token": "ghp_xxxxx"
    }
  }
}
```

---

## 💾 Database Integration (Optional)

For storing historical data, you can optionally integrate with PostgreSQL:

### Database Schema

```sql
CREATE TABLE migrations (
  id VARCHAR(255) PRIMARY KEY,
  name VARCHAR(255),
  type VARCHAR(50),
  phase VARCHAR(50),
  progress JSONB,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  completed_at TIMESTAMP
);

CREATE TABLE migration_events (
  id SERIAL PRIMARY KEY,
  migration_id VARCHAR(255),
  event_type VARCHAR(50),
  event_data JSONB,
  timestamp TIMESTAMP,
  FOREIGN KEY (migration_id) REFERENCES migrations(id)
);

CREATE INDEX idx_migrations_phase ON migrations(phase);
CREATE INDEX idx_migrations_created_at ON migrations(created_at);
CREATE INDEX idx_events_migration_id ON migration_events(migration_id);
CREATE INDEX idx_events_timestamp ON migration_events(timestamp);
```

### Query Historical Data

The dashboard can fetch from PostgreSQL for:
- Historical migration data
- Trend analysis
- Performance reports
- Audit logs

**Example Query:**
```sql
SELECT
  date_trunc('day', created_at) as date,
  COUNT(*) as total_migrations,
  COUNT(*) FILTER (WHERE phase = 'Completed') as completed,
  COUNT(*) FILTER (WHERE phase = 'Failed') as failed
FROM migrations
WHERE created_at >= NOW() - INTERVAL '30 days'
GROUP BY date_trunc('day', created_at)
ORDER BY date;
```

---

## 🚀 Rate Limits

| Endpoint Type | Rate Limit | Window |
|--------------|------------|--------|
| Discovery | 100 req/min | Per IP |
| Migration | 50 req/min | Per user |
| Statistics | 200 req/min | Per IP |
| Health | Unlimited | - |
| SSE | 10 connections | Per IP |

**Rate Limit Headers:**
```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1699999999
```

---

## 📝 Response Codes

| Code | Meaning |
|------|---------|
| 200 | Success |
| 201 | Created |
| 202 | Accepted (async operation started) |
| 400 | Bad Request |
| 401 | Unauthorized |
| 403 | Forbidden |
| 404 | Not Found |
| 429 | Rate Limit Exceeded |
| 500 | Internal Server Error |
| 503 | Service Unavailable |

---

## 🔗 Related Documentation

- [Dashboard Setup](DASHBOARD_SETUP.md)
- [Examples](EXAMPLES/README.md)
- [API Reference](docs/API_ENDPOINTS_REFERENCE.md)
- [Local Testing](docs/LOCAL_TESTING_GUIDE.md)
