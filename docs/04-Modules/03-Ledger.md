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

## Ledger v3 Mirror

Ledger v3 can run alongside an existing v2 deployment as a **mirror**: it continuously replicates v2 ledger data into its own Raft-based storage. This allows gradual migration or read-offloading without disrupting v2.

When the `modules.ledger.v3-mirror` setting is present, the operator:
1. Deploys v2 normally (database, migrations, Deployment)
2. Deploys a v3 Raft StatefulSet in parallel
3. Runs a provisioning Job that creates mirror ledgers in v3, each sourcing data from the v2 PostgreSQL database

### Enabling v3 Mirror

Create a Settings resource with the key `modules.ledger.v3-mirror`. The value format is:

```
<v3-image-tag>:<ledger1>,<ledger2>,...
```

- **v3-image-tag**: The container image tag of the ledger v3 binary (e.g. `v3.0.0-alpha.1`)
- **ledger names**: Comma-separated list of v2 ledger names to mirror

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-v3-mirror
spec:
  stacks: ["my-stack"]
  key: modules.ledger.v3-mirror
  value: "v3.0.0-alpha.1:default,payments"
```

This example deploys a v3 cluster using image tag `v3.0.0-alpha.1` and creates two mirror ledgers (`default` and `payments`) that replicate from the v2 PostgreSQL database.

### How It Works

The provisioning Job connects to the v3 cluster's gRPC endpoint and calls `ledgerctl ledgers create` for each listed ledger with:
- `--mode mirror` — marks the ledger as a mirror (read-only, no direct writes)
- `--mirror-source-type postgres` — uses direct PostgreSQL access for replication
- `--mirror-dsn` — the PostgreSQL DSN of the v2 database (derived automatically from the Database resource)

The Job is idempotent: if a mirror ledger already exists, the error is ignored. It retries on failure (e.g. if the v3 cluster is not yet ready).

### Architecture

The operator creates the following resources for v3:

| Resource | Purpose |
|----------|---------|
| `StatefulSet/ledger` | Raft cluster nodes with `OrderedReady` pod management |
| `Service/ledger-raft` (headless) | DNS-based peer discovery for Raft consensus |
| `Job/v3-mirror-provision` | Creates mirror ledgers in the v3 cluster |
| 3 PVCs per pod | `wal`, `data`, `cold-cache` |

### Requirements

Ledger v3 does **not** require its own PostgreSQL or message broker. Storage is fully embedded (Pebble LSM). However, the v3 pods need network access to the v2 PostgreSQL database for mirror replication.

### Cluster Settings

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-v3-replicas
spec:
  stacks: ["*"]
  key: module.ledger.v3.replicas
  value: "3"
```

- `module.ledger.v3.replicas`: Number of Raft nodes. **Must be odd** for quorum (default: 3).

The Raft cluster ID is automatically set to the stack name.

### Persistence Settings

Each pod gets three PVCs. Size and storage class are configurable:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-v3-persistence
spec:
  stacks: ["*"]
  key: module.ledger.v3.persistence.wal.size
  value: "5Gi"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-v3-data-size
spec:
  stacks: ["*"]
  key: module.ledger.v3.persistence.data.size
  value: "10Gi"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: ledger-v3-cold-cache-size
spec:
  stacks: ["*"]
  key: module.ledger.v3.persistence.cold-cache.size
  value: "10Gi"
```

| Key | Default | Description |
|-----|---------|-------------|
| `module.ledger.v3.persistence.wal.size` | 5Gi | WAL PVC size |
| `module.ledger.v3.persistence.wal.storage-class` | (cluster default) | WAL storage class |
| `module.ledger.v3.persistence.data.size` | 10Gi | Pebble data PVC size |
| `module.ledger.v3.persistence.data.storage-class` | (cluster default) | Data storage class |
| `module.ledger.v3.persistence.cold-cache.size` | 10Gi | Cold cache PVC size |
| `module.ledger.v3.persistence.cold-cache.storage-class` | (cluster default) | Cold cache storage class |

### Pebble Tunables

All Pebble settings are optional. When unset, the ledger binary defaults apply.

| Key | Example | Description |
|-----|---------|-------------|
| `module.ledger.v3.pebble.cache-size` | 1073741824 | Block cache size in bytes |
| `module.ledger.v3.pebble.memtable-size` | 268435456 | Memtable size in bytes |
| `module.ledger.v3.pebble.memtable-stop-writes-threshold` | 2 | Memtable count before stopping writes |
| `module.ledger.v3.pebble.l0-compaction-threshold` | 4 | L0 files to trigger compaction |
| `module.ledger.v3.pebble.l0-stop-writes-threshold` | 12 | L0 files before stopping writes |
| `module.ledger.v3.pebble.lbase-max-bytes` | 67108864 | L1 max size in bytes |
| `module.ledger.v3.pebble.target-file-size` | 67108864 | SST file target size |
| `module.ledger.v3.pebble.max-concurrent-compactions` | 2 | Compaction parallelism |

### Raft Tunables

All Raft settings are optional. When unset, the ledger binary defaults apply.

| Key | Example | Description |
|-----|---------|-------------|
| `module.ledger.v3.raft.snapshot-threshold` | 5000 | Log entries before snapshot |
| `module.ledger.v3.raft.election-tick` | 10 | Election timeout in ticks |
| `module.ledger.v3.raft.heartbeat-tick` | 1 | Heartbeat interval in ticks |
| `module.ledger.v3.raft.tick-interval` | 100ms | Duration of one tick |
| `module.ledger.v3.raft.max-size-per-msg` | 1048576 | Max message size in bytes |
| `module.ledger.v3.raft.max-inflight-msgs` | 256 | Max in-flight messages |
| `module.ledger.v3.raft.compaction-margin` | 1000 | Log retention after snapshot |
