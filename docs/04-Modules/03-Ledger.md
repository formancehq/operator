Formance Ledger is a real-time money tracking microservice that lets you model and record complex financial transactions. It offers atomic, multi-posting transactions and is programmable using Numscript, a dedicated DSL (Domain Specific Language) to model and templatize such transactions.

## Requirements

Formance Ledger requires:
- **PostgreSQL**: See configuration guide [here](../05-Infrastructure%20services/01-PostgreSQL.md).
- (Optional) **Broker**: See configuration guide [here](../05-Infrastructure%20services/02-Message%20broker.md).

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
