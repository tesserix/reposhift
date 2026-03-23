# GitHub App Setup

This guide walks through creating and configuring a GitHub App for use with Reposhift. A GitHub App is the recommended authentication method for production migrations.

---

## Why Use a GitHub App

| Feature | GitHub App | Personal Access Token |
|---------|-----------|----------------------|
| **Rate limit** | 10,000 requests/hr per installation | 5,000 requests/hr |
| **Permissions** | Fine-grained, per-repository | Broad, user-level |
| **Scope** | Organization-level install | Tied to a single user |
| **Token lifecycle** | Auto-rotating (1 hour expiry) | Static until revoked |
| **Audit trail** | Actions attributed to the app | Actions attributed to a user |
| **Security** | Private key + installation token | Single token string |

For migrations involving more than a handful of repositories, or any production use case, a GitHub App avoids rate limit issues and provides better security.

---

## Step 1: Create the GitHub App

1. Navigate to your GitHub organization settings:
   `https://github.com/organizations/<your-org>/settings/apps`

   Or for a personal account:
   `https://github.com/settings/apps`

2. Click **New GitHub App**

---

## Step 2: Configure the App

Fill in the following fields:

### General

| Field | Value |
|-------|-------|
| **GitHub App name** | `Reposhift Migration` (or any name you prefer) |
| **Homepage URL** | Your Reposhift dashboard URL, or `https://github.com/tesserix/reposhift` |
| **Description** | Optional. Example: "Automated migration from Azure DevOps to GitHub" |

### Webhook

- **Active**: Uncheck this box. Reposhift does not require webhooks.

### Permissions

Under **Repository permissions**, set:

| Permission | Access Level | Why |
|-----------|-------------|-----|
| **Contents** | Read & write | Clone repositories, push migrated code, create/update files |
| **Administration** | Read & write | Create new repositories, set default branches, configure settings |
| **Metadata** | Read-only | List repositories, read basic repo info (required by default) |
| **Issues** | Read & write | Create GitHub Issues from migrated work items |
| **Projects** | Read & write | Create and manage GitHub Projects for migrated work items |
| **Workflows** | Read & write | Push GitHub Actions workflow files during pipeline migration |

Under **Organization permissions**, set:

| Permission | Access Level | Why |
|-----------|-------------|-----|
| **Members** | Read-only | Verify organization membership and map users |

### Where can this GitHub App be installed?

Select **Only on this account** (recommended for private use), or **Any account** if you want to use it across multiple organizations.

Click **Create GitHub App**.

---

## Step 3: Generate a Private Key

After creating the app, you will be redirected to the app settings page.

1. Scroll down to the **Private keys** section
2. Click **Generate a private key**
3. A `.pem` file will be downloaded automatically
4. Store this file securely -- it is the equivalent of a password for the app

The private key file will look like this:

```
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA2...
...many lines of base64...
-----END RSA PRIVATE KEY-----
```

---

## Step 4: Note the App ID and Installation ID

### App ID

On the app settings page (`https://github.com/settings/apps/<your-app>`), the **App ID** is displayed near the top of the page under "About". It is a numeric value, for example: `123456`.

### Installation ID

1. In the left sidebar, click **Install App**
2. Click **Install** next to your organization
3. Choose whether to grant access to **All repositories** or **Only select repositories**
4. Click **Install**
5. After installation, look at the URL in your browser. It will be:
   `https://github.com/organizations/<your-org>/settings/installations/<installation-id>`
6. The numeric value at the end of the URL is your **Installation ID**, for example: `78901234`

---

## Step 5: Configure Reposhift with the GitHub App

### Option A: Via the Web UI

1. Open the Reposhift dashboard
2. Navigate to **Secrets**
3. Click **Add Secret**
4. Fill in:
   - **Type**: GitHub App
   - **Name**: `my-github-app` (any descriptive name)
   - **App ID**: The numeric App ID from Step 4
   - **Installation ID**: The numeric Installation ID from Step 4
   - **Private Key**: Paste the full contents of the `.pem` file (including the BEGIN/END lines)
5. Click **Save**

### Option B: Via Kubernetes Secrets

Create a Kubernetes secret containing the GitHub App credentials:

```bash
kubectl create secret generic github-app-secret \
  --namespace reposhift \
  --from-literal=app-id="123456" \
  --from-literal=installation-id="78901234" \
  --from-file=private-key=./your-app-private-key.pem
```

Reference this secret in your migration CRDs:

```yaml
spec:
  target:
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
```

### Option C: Via Helm Values

Pass the credentials during operator installation:

```bash
helm install reposhift-operator reposhift/ado-git-migration \
  --namespace reposhift \
  --set auth.githubApp.appId="123456" \
  --set auth.githubApp.installationId="78901234" \
  --set-file auth.githubApp.privateKey=./your-app-private-key.pem
```

---

## Step 6: Validate Connectivity

### Via the Web UI

1. Navigate to **Secrets**
2. Find your GitHub App secret
3. Click **Test Connection**
4. The UI will attempt to authenticate as the app and list accessible repositories
5. A green checkmark indicates success

### Via kubectl

Create a test migration and observe the validation phase:

```bash
kubectl apply -f - <<EOF
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: test-github-app
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
      sourceId: test-repo-id
      sourceName: test-repo
      targetName: test-repo-migration
  settings:
    maxHistoryDays: 30
    retryAttempts: 1
EOF
```

Check validation results:

```bash
kubectl describe adotogitmigration test-github-app -n reposhift
```

Look for the `Validating` phase and any validation errors in the status.

---

## Troubleshooting

### "Resource not accessible by integration"

The GitHub App does not have sufficient permissions. Go to the app settings and verify the permissions listed in Step 2. After changing permissions, the organization admin may need to approve the new permissions.

### "Could not resolve to a node with the global id..."

The Installation ID is incorrect. Verify it by visiting your organization's installed apps page:
`https://github.com/organizations/<your-org>/settings/installations`

### "Bad credentials" or "A JSON web token could not be decoded"

The private key is invalid or corrupted. Common causes:
- The PEM file was truncated during copy/paste
- The `-----BEGIN RSA PRIVATE KEY-----` and `-----END RSA PRIVATE KEY-----` lines are missing
- The file has Windows-style line endings (CRLF instead of LF)

Regenerate the private key from the GitHub App settings page.

### "App not installed on organization"

The GitHub App was created but not installed on the target organization. Go to the app settings, click **Install App**, and install it on the organization you are migrating to.

### Rate Limit Errors Despite Using a GitHub App

- Verify the app is installed on the correct organization
- Check that the installation has access to the target repositories (not limited to "select repositories" that excludes the targets)
- If running multiple parallel migrations, the 10,000 req/hr limit applies to the entire installation, not per-repository

### Token Refresh Failures

Reposhift automatically refreshes GitHub App installation tokens before they expire (tokens last 1 hour). If you see token refresh errors in the operator logs:

```bash
kubectl logs -n reposhift -l app.kubernetes.io/name=ado-git-migration | grep "token"
```

Common causes:
- The private key was rotated in GitHub but not updated in the Kubernetes secret
- Clock skew between the operator pod and GitHub servers (check NTP sync)
