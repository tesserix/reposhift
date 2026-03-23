# Troubleshooting

Common issues encountered during Reposhift installation and migration, with causes and solutions.

---

## Installation Issues

### Pod CrashLoopBackOff

**Symptom:** The operator pod repeatedly crashes and restarts.

```bash
kubectl get pods -n reposhift
# NAME                              READY   STATUS             RESTARTS   AGE
# reposhift-operator-xxxx           0/1     CrashLoopBackOff   5          3m
```

**Diagnosis:**

```bash
kubectl logs -n reposhift -l app.kubernetes.io/name=ado-git-migration --previous
```

**Common causes:**

| Log Message | Cause | Fix |
|-------------|-------|-----|
| `failed to connect to database` | PostgreSQL is unreachable | Check host, port, password, SSL mode |
| `permission denied` | Pod security context too restrictive | Verify `runAsUser` and `fsGroup` in values |
| `OOMKilled` | Insufficient memory | Increase `resources.limits.memory` |
| `unable to create controller` | CRDs not installed | Run `kubectl apply -f config/crd/bases/` |
| `leader election lost` | Multiple instances competing | Expected during rolling updates; wait for stabilization |

### Failed to Connect to Database

**Symptom:** Operator or Platform API logs show database connection errors.

**Check connectivity from within the cluster:**

```bash
kubectl run pg-test --rm -it --image=postgres:16 --namespace=reposhift -- \
  psql "host=<pg-host> port=5432 dbname=reposhift_db user=reposhift_user password=<password> sslmode=<mode>"
```

**Common fixes:**

- Verify the PostgreSQL hostname resolves from within the cluster (use full DNS: `svc-name.namespace.svc.cluster.local`)
- Check that the PostgreSQL port (default 5432) is not blocked by NetworkPolicy
- If using Cloud SQL or RDS, verify the cluster's network has connectivity (VPC peering, authorized networks, etc.)
- Check SSL mode: use `disable` for local PostgreSQL, `require` for managed services

### Secret Not Found

**Symptom:** Migration fails during validation with "secret not found" errors.

**Check:**

```bash
kubectl get secrets -n reposhift
kubectl describe secret <secret-name> -n reposhift
```

**Common causes:**

- The secret is in a different namespace than the migration CRD
- The secret key name does not match the `key` field in the `SecretReference`
- The secret was not created yet

**Fix:** Ensure the secret exists in the correct namespace with the correct key:

```bash
kubectl create secret generic my-secret \
  --namespace=reposhift \
  --from-literal=token="my-token-value"
```

Verify the key name:

```bash
kubectl get secret my-secret -n reposhift -o jsonpath='{.data}' | jq 'keys'
```

---

## Repository Migration Issues

### Failed to Clone from Azure DevOps

**Symptom:** Migration stuck at "Cloning repository from Azure DevOps" or fails with a clone error.

**Common causes and fixes:**

| Error | Cause | Fix |
|-------|-------|-----|
| `authentication failed` | ADO PAT is expired or invalid | Regenerate the PAT in Azure DevOps |
| `repository not found` | Wrong org/project/repo name | Verify the `sourceId` and `sourceName` match the ADO repository |
| `TF401019: The Git repository does not exist` | Repository does not exist in the specified project | Check ADO project name (case-sensitive) |
| `fatal: early EOF` | Network interruption during large clone | Retry; increase memory limits; use shallow clone |
| `fetch-pack: unexpected disconnect` | Timeout during clone | Check network; use `cloneDepth` for shallow clone |

**Check ADO PAT permissions:**

The PAT must have at minimum:
- **Code**: Read
- **Project and Team**: Read (for auto-discovery)

### Push Declined by GitHub

**Symptom:** Migration fails at the "Pushing repository to GitHub" phase.

**Common causes:**

| Error | Cause | Fix |
|-------|-------|-----|
| `GH006: Protected branch update failed` | Branch protection rules on the target | Temporarily disable branch protection, or add the GitHub App/PAT user as a bypass actor |
| `refusing to allow an OAuth App to create or update workflow` | Missing `workflow` scope on PAT | Create a new PAT with `workflow` scope, or use a GitHub App with Workflows permission |
| `Repository was archived so is read-only` | Target repo is archived | Unarchive the target repository |
| `remote: error: File is X MB; exceeds maximum size of 100 MB` | File too large for GitHub | Add file to `.gitattributes` for LFS, or remove it from history |

### Failed to Set Default Branch

**Symptom:** Migration completes but the default branch on GitHub is wrong.

Reposhift auto-detects the default branch from the source repository and sets it on GitHub. If this fails:

```bash
# Manually set the default branch
gh repo edit <owner>/<repo> --default-branch <branch-name>
```

The most common cause is that the branch has not finished propagating on GitHub's side. Reposhift retries up to 3 times with increasing delays.

---

## Work Item Migration Issues

### Work Items Duplicated

**Symptom:** The same work items appear as multiple GitHub Issues.

**Causes:**
- The migration was run more than once with the same CRD (delete and recreate triggers a fresh migration)
- Two `WorkItemMigration` CRDs target the same repository with overlapping filters

**Fix:**
- Check for duplicate CRDs: `kubectl get workitemmigration -n reposhift`
- Delete duplicate issues manually or with a script
- Before re-running a migration, delete existing issues or use a different target repository

### Rate Limit Exceeded During Work Item Migration

**Symptom:** Migration slows down dramatically or fails with 403/429 errors.

**Immediate fixes:**

```yaml
settings:
  batchSize: 10              # Reduce from default 20
  batchDelaySeconds: 120     # Increase pause between batches
  perItemDelayMs: 2000       # Increase pause between items
  combineComments: true      # Reduces API calls by ~90%
```

**Long-term fix:** Switch to a GitHub App for 10,000 req/hr instead of 5,000 with a PAT.

### WIQL Query Returns Too Many Results

**Symptom:** Error about exceeding the 20,000 item limit.

**Fix:** Add more specific filters to reduce the result set:

```yaml
filters:
  types:
    - Epic          # Start with one type at a time
  states:
    - Active
  dateRange:
    start: "2023-01-01T00:00:00Z"
```

Run multiple migrations with different filters rather than one migration for all work items.

---

## Monorepo Migration Issues

### History Rewrite Fails

**Symptom:** Monorepo migration fails during the "Rewriting" phase.

This phase uses `git filter-repo` to move all files into subdirectories. Common issues:

- **Out of memory**: Large repos with deep history require significant memory during rewrite. Increase pod memory limits.
- **Disk full**: The rewrite creates a temporary copy. Ensure the PVC has at least 3x the total size of all source repos.

### Branch Name Collisions

**Symptom:** Error during merge phase about conflicting branch names.

Branches are automatically prefixed with the source repo name (`repo-name/branch-name`). If you see collisions, check for duplicate `subdirectoryName` values across `sourceRepos`.

---

## Operator Issues

### Migration Stuck in "Running"

**Symptom:** A migration has been in "Running" phase for much longer than expected.

**Diagnosis:**

```bash
# Check operator logs
kubectl logs -n reposhift -l app.kubernetes.io/name=ado-git-migration --tail=100

# Check the migration status
kubectl describe adotogitmigration <name> -n reposhift

# Check resource statuses for individual repo progress
kubectl get adotogitmigration <name> -n reposhift -o jsonpath='{.status.resourceStatuses}' | jq .
```

**Common causes:**

- The git push is still in progress for a very large repository (check logs for progress)
- Rate limiting is causing long backoff delays
- Network connectivity issues causing retries
- The operator pod was restarted during migration (check `kubectl get events -n reposhift`)

**Fix:** If the migration is genuinely stuck (no log activity for 30+ minutes):

```bash
# Cancel and retry
kubectl patch adotogitmigration <name> -n reposhift \
  --type=merge -p '{"spec":{"cancel":true}}'

# Then delete and recreate the CRD
kubectl delete adotogitmigration <name> -n reposhift
kubectl apply -f migration.yaml
```

### Migration Stuck in "Validating"

**Symptom:** Migration never progresses past the Validating phase.

**Common causes:**

- Credentials are invalid (check operator logs for authentication errors)
- The source ADO project does not exist or is not accessible
- The GitHub organization does not exist or the token lacks access
- Network policy is blocking outbound HTTPS

**Check network connectivity:**

```bash
kubectl run curl-test --rm -it --image=curlimages/curl --namespace=reposhift -- \
  curl -s -o /dev/null -w "%{http_code}" https://dev.azure.com

kubectl run curl-test --rm -it --image=curlimages/curl --namespace=reposhift -- \
  curl -s -o /dev/null -w "%{http_code}" https://api.github.com
```

Both should return `200` or `301`.

---

## Network and Access Issues

### RBAC: Access Denied

**Symptom:** Operator logs show RBAC errors when trying to read/write CRDs or secrets.

**Fix:** Verify the operator's ServiceAccount has the necessary ClusterRole bindings:

```bash
kubectl get clusterrolebinding | grep reposhift
kubectl describe clusterrolebinding <binding-name>
```

If using the Helm chart with `serviceAccount.create: true`, the RBAC is created automatically. If you disabled it, ensure manual RBAC grants access to:
- All Reposhift CRDs (get, list, watch, create, update, patch, delete)
- Secrets in the migration namespace (get, list, watch)
- Events (create, patch)

### NetworkPolicy Blocking Connections

**Symptom:** Operator cannot reach Azure DevOps or GitHub APIs.

If your cluster enforces NetworkPolicy, the operator needs outbound HTTPS access:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: reposhift-egress
  namespace: reposhift
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: ado-git-migration
  policyTypes:
    - Egress
  egress:
    # DNS
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
    # HTTPS to external services
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 10.0.0.0/8
              - 172.16.0.0/12
              - 192.168.0.0/16
      ports:
        - port: 443
          protocol: TCP
```

---

## Disk Space Issues

### Disk Space Full

**Symptom:** Migration fails with disk-related errors, or the operator pod is evicted.

**Diagnosis:**

```bash
# Check PVC usage
kubectl exec -n reposhift <operator-pod> -- df -h /workspace/migrations
```

**Fixes:**

- Increase the PVC size: edit the `persistence.size` in Helm values and upgrade the release
- Use shallow clones (`cloneDepth: 50-100`) to reduce per-repo disk usage
- For monorepo migrations, enable `cleanupBetweenRepos: true`
- Reduce `parallelWorkers` to have fewer concurrent clones on disk
- Clean up completed migration workspaces (they should be auto-cleaned, but check logs)

---

## Useful Diagnostic Commands

```bash
# All Reposhift resources
kubectl get adotogitmigration,monorepomigration,workitemmigration,pipelinetoworkflow \
  -n reposhift

# Operator logs (last 200 lines)
kubectl logs -n reposhift -l app.kubernetes.io/name=ado-git-migration --tail=200

# Platform API logs
kubectl logs -n reposhift -l app.kubernetes.io/name=reposhift-platform --tail=200

# Events in the namespace
kubectl get events -n reposhift --sort-by='.lastTimestamp' | tail -30

# Describe a failing migration
kubectl describe adotogitmigration <name> -n reposhift

# Check resource quotas
kubectl describe resourcequota -n reposhift

# Check PVC status
kubectl get pvc -n reposhift

# Check operator metrics (if port-forwarded)
curl http://localhost:8082/metrics | grep migration
```
