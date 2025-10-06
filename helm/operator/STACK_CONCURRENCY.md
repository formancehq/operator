# Concurrency Control Configuration

## Overview

Control the number of concurrent reconciliations (Stacks, Modules, and other resources) that run in parallel to prevent cluster overload and manage deployment pace.

## Configuration

### Via Helm Values

Edit your `values.yaml` or use `--set`:

```yaml
operator:
  maxConcurrentReconciles: 5  # Max 5 concurrent reconciliations (all resources)
```

Or with Helm command:

```bash
helm install operator ./helm/operator \
  --set operator.maxConcurrentReconciles=5
```

### Default Behavior

- **Default value: `5`** (good balance for most clusters)
- Set to `0` to use controller-runtime default (typically 1 concurrent reconciliation)
- For near-unlimited concurrency, set a high value like `1000`

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
  maxConcurrentReconciles: 3
```

```bash
helm upgrade operator ./helm/operator -f values.yaml
```

### Example 2: Production Cluster

```yaml
# values-prod.yaml
operator:
  maxConcurrentReconciles: 10
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
  --set operator.maxConcurrentReconciles=5 \
  --set operator.region=us-east-1
```

## How It Works

1. The Helm chart sets the `MAX_CONCURRENT_RECONCILES` environment variable
2. The operator reads this value on startup
3. All reconciliations (Stacks, Modules like Ledger/Payments, etc.) are limited to N concurrent executions
4. Additional reconciliations are queued automatically by Kubernetes

### Behavior

**Without limit (when set to 0):**
```text
Stack A ──┐
Stack B ──┤
Stack C ──┼─> All processed in parallel
Stack D ──┤
Stack E ──┘
```

**With limit of 5:**
```text
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
kubectl exec -n formance-system $POD -- env | grep MAX_CONCURRENT_RECONCILES
```

Expected output:
```text
MAX_CONCURRENT_RECONCILES=5
```

## Troubleshooting

### Value not applied

1. **Check Helm values:**
   ```bash
   helm get values operator -n formance-system
   ```

2. **Verify deployment:**
   ```bash
   kubectl get deployment operator-manager -n formance-system -o yaml | grep -A 2 "MAX_CONCURRENT_RECONCILES"
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
  maxConcurrentReconciles: 2
  env: "dev"

# values-staging.yaml
operator:
  maxConcurrentReconciles: 5
  env: "staging"

# values-prod.yaml
operator:
  maxConcurrentReconciles: 10
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
          maxConcurrentReconciles: 5
          region: "eu-west-1"
          env: "production"
```

## Technical Details

### Implementation

- **Environment Variable:** `MAX_CONCURRENT_RECONCILES`
- **Read by:** `internal/core/concurrency.go::GetMaxConcurrentReconciles()`
- **Applied in:** All reconcilers (Stacks, Modules, Resources)
- **Uses:** Native controller-runtime `MaxConcurrentReconciles`

### Source Code

```go
// internal/core/concurrency.go
func GetMaxConcurrentReconciles() int {
    if v := os.Getenv("MAX_CONCURRENT_RECONCILES"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n >= 0 {
            return n
        }
    }
    return 5 // Default: 5 concurrent reconciliations
}
```

## What This Controls

This setting limits **all types** of reconciliations:
- **Stack reconciliations**: Namespace creation, configuration updates
- **Module reconciliations**: Ledger, Payments, Wallets, Gateway, etc. deployments
- **Resource reconciliations**: Database, Broker, BrokerTopic management

This prevents "big bang" deployments where all resources are processed simultaneously.
