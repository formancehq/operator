# Stack Concurrency Configuration

## Overview

Control the number of Stack reconciliations that run in parallel to prevent cluster overload.

## Configuration

### Via Helm Values

Edit your `values.yaml` or use `--set`:

```yaml
operator:
  stackMaxConcurrent: 5  # Max 5 concurrent stack reconciliations
```

Or with Helm command:

```bash
helm install operator ./helm/operator \
  --set operator.stackMaxConcurrent=5
```

### Default Behavior

- **Default value: `5`** (good balance for most clusters)
- Set to `0` to disable the limit (unlimited concurrency)

## Recommended Values

| Cluster Size | Stacks | CPU | Recommended Value |
|-------------|--------|-----|-------------------|
| Small | 1-10 | 2-4 CPU | `2-3` |
| Medium | 10-30 | 4-8 CPU | `5` |
| Large | 30-100 | 8-16 CPU | `10` |
| XL | 100+ | 16+ CPU | `20` |

## Examples

### Example 1: Small Cluster

```yaml
# values.yaml
operator:
  stackMaxConcurrent: 3
```

```bash
helm upgrade operator ./helm/operator -f values.yaml
```

### Example 2: Production Cluster

```yaml
# values-prod.yaml
operator:
  stackMaxConcurrent: 10
  enableLeaderElection: true
  region: "eu-west-1"
  env: "production"
```

```bash
helm upgrade operator ./helm/operator -f values-prod.yaml
```

### Example 3: Override with --set

```bash
helm upgrade operator ./helm/operator \
  --set operator.stackMaxConcurrent=5 \
  --set operator.region=us-east-1
```

## How It Works

1. The Helm chart sets the `STACK_MAX_CONCURRENT` environment variable
2. The operator reads this value on startup
3. Stack reconciliations are limited to N concurrent executions
4. Additional stacks are queued automatically by Kubernetes

### Behavior

**Without limit (default):**
```
Stack A ──┐
Stack B ──┤
Stack C ──┼─> All processed in parallel
Stack D ──┤
Stack E ──┘
```

**With limit of 5:**
```
Stack A ──┐
Stack B ──┤
Stack C ──┼─> Max 5 in parallel
Stack D ──┤
Stack E ──┘
Stack F ──> Queued (waiting)
Stack G ──> Queued (waiting)
```

## Verification

Check if the environment variable is set:

```bash
# Get the pod name
POD=$(kubectl get pods -n formance-system -l control-plane=formance-controller-manager -o jsonpath='{.items[0].metadata.name}')

# Check environment variables
kubectl exec -n formance-system $POD -- env | grep STACK_MAX_CONCURRENT
```

Expected output:
```
STACK_MAX_CONCURRENT=5
```

## Troubleshooting

### Value not applied

1. **Check Helm values:**
   ```bash
   helm get values operator -n formance-system
   ```

2. **Verify deployment:**
   ```bash
   kubectl get deployment operator-manager -n formance-system -o yaml | grep -A 2 "STACK_MAX_CONCURRENT"
   ```

3. **Restart pods to apply changes:**
   ```bash
   kubectl rollout restart deployment operator-manager -n formance-system
   ```

### Performance Issues

**Too many concurrent reconciliations:**
- Symptoms: High CPU/memory, slow reconciliations
- Solution: Lower the value (e.g., from 10 to 5)

**Too few concurrent reconciliations:**
- Symptoms: Long queue times, slow stack deployments
- Solution: Increase the value (e.g., from 5 to 10)

## Monitoring

### Check current stack status

```bash
# Total stacks
kubectl get stacks --all-namespaces --no-headers | wc -l

# Ready stacks
kubectl get stacks --all-namespaces -o json | jq '[.items[] | select(.status.ready==true)] | length'

# Not ready stacks (in queue or processing)
kubectl get stacks --all-namespaces -o json | jq '[.items[] | select(.status.ready==false)] | length'
```

### Operator logs

```bash
kubectl logs -n formance-system -l control-plane=formance-controller-manager -f
```

## Advanced Usage

### Environment-specific values

```yaml
# values-dev.yaml
operator:
  stackMaxConcurrent: 2
  env: "dev"

# values-staging.yaml
operator:
  stackMaxConcurrent: 5
  env: "staging"

# values-prod.yaml
operator:
  stackMaxConcurrent: 10
  env: "production"
```

Deploy:
```bash
helm upgrade operator ./helm/operator -f values-prod.yaml
```

### ArgoCD / GitOps

```yaml
# argocd-application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: formance-operator
spec:
  source:
    helm:
      values: |
        operator:
          stackMaxConcurrent: 5
          region: "eu-west-1"
          env: "production"
```

## Technical Details

### Implementation

- **Environment Variable:** `STACK_MAX_CONCURRENT`
- **Read by:** `internal/resources/stacks/config.go::GetStackConcurrency()`
- **Applied in:** `internal/resources/stacks/init.go`
- **Uses:** Native controller-runtime `MaxConcurrentReconciles`

### Source Code

```go
// internal/resources/stacks/config.go
func GetStackConcurrency() int {
    if v := os.Getenv("STACK_MAX_CONCURRENT"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 {
            return n
        }
    }
    return 0 // Default: unlimited
}
```

## Related Documentation

- [How to Limit Concurrent Stacks](../../docs/HOW_TO_LIMIT_CONCURRENT_STACKS.md)
- [Concurrent Limit Implementation](../../CONCURRENT_LIMIT_IMPLEMENTATION.md)
