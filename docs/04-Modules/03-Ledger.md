Formance Ledger is a real-time money tracking microservice that lets you model and record complex financial transactions. It offers atomic, multi-posting transactions and is programmable using Numscript, a dedicated DSL (Domain Specific Language) to model and templatize such transactions.

## Requirements

Ledger versions up to and including `v3.0.0-alpha` require:

- **PostgreSQL**: See configuration guide [here](../05-Infrastructure%20services/01-PostgreSQL.md).
- (Optional) **Broker**: See configuration guide [here](../05-Infrastructure%20services/02-Message%20broker.md).

Ledger versions newer than `v3.0.0-alpha` require the Ledger Operator and its
`ledger.formance.com/v1alpha1` CRDs to be installed in the cluster. They use the
Ledger v3 native storage and do not require a PostgreSQL `Database` resource.

## Ledger Object

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#ledger).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Ledger
metadata:
  name: formance-dev
spec:
  stack: formance-dev
```

## Ledger v3 delegation

When the stack version is strictly newer than `v3.0.0-alpha`, the Formance
Operator delegates Ledger provisioning to the Ledger Operator. It creates a
`ledger.formance.com/v1alpha1` `Cluster` with the same name and namespace as the
stack instead of creating the legacy Ledger Deployments, Database, migration
jobs, and CronJobs.

The Ledger Operator must be installed before the Formance Operator starts so
that the latter can watch `Cluster` resources. If the CRD is unavailable, the
Ledger remains pending and no legacy resources are created.

Automatic in-place migration is intentionally not supported. If legacy Ledger
Deployments or a Database already exist when switching a stack to v3, the
Ledger reports that an explicit migration is required and does not create the
v3 `Cluster`. The reverse transition is guarded in the same way: legacy
resources are not created while a v3 `Cluster` still exists.

The v3 cluster size defaults to three replicas and can be configured per stack:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-v3-replicas
spec:
  stacks: ["formance-dev"]
  key: module.ledger.v3.replicas
  value: "3"
```

The replica count must be a positive odd number.

## Settings (v2.4+)

### Schema Enforcement Mode

Configure the schema enforcement mode for both the Ledger API and worker:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-schema-enforcement-mode
spec:
  stacks: ["*"]
  key: ledger.schema-enforcement-mode
  value: strict
```

### Disable Ledger Scope Optimization

By default, the Ledger skips the `ledger = ?` predicate on read queries when a
ledger is the only one in its bucket (the "alone-in-bucket" optimization). Set
this to `true` to always emit the predicate, as a performance/safety escape
hatch:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-disable-ledger-scope-optimization
spec:
  stacks: ["*"]
  key: ledger.disable-ledger-scope-optimization
  value: "true"
```

## Worker Settings (v2.3+)

Starting with Ledger v2.3, a separate worker process is deployed alongside the main Ledger API. The worker can be configured using the Settings CRD.

### Async Block Hasher

Configure the async block hasher behavior:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-worker-async-block-hasher
spec:
  stacks: ["*"]
  key: ledger.worker.async-block-hasher
  value: max-block-size=500, schedule="0 */5 * * * *"
```

Available fields:
- `max-block-size`: Maximum block size for the async block hasher
- `schedule`: Cron schedule for the async block hasher

### Bucket Cleanup (v2.4+)

Configure the worker bucket cleanup behavior:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-worker-bucket-cleanup
spec:
  stacks: ["*"]
  key: ledger.worker.bucket-cleanup
  value: retention-period=720h, schedule="0 0 * * *"
```

Available fields:
- `retention-period`: Retention period before bucket deletion
- `schedule`: Cron schedule for the bucket cleanup job

### Pipelines

Configure the worker pipelines behavior:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-worker-pipelines
spec:
  stacks: ["*"]
  key: ledger.worker.pipelines
  value: pull-interval=5s, push-retry-period=10s, sync-period=1m, logs-page-size=100
```

Available fields:
- `pull-interval`: Interval between pipeline pulls
- `push-retry-period`: Retry period for failed pushes
- `sync-period`: Synchronization period
- `logs-page-size`: Number of logs per page
