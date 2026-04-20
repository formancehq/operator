# RFC-001: Plugin Architecture for the Formance Operator

- **Status**: Draft
- **Author**: gfyrag
- **Date**: 2026-04-20

## Problem

The Formance operator is a monolithic binary that deploys all stack components (Ledger, Payments, Wallets, etc.). This creates friction in development:

- **Coupled release cycles**: Changing one module's deployment logic requires releasing the entire operator
- **Slow dev loop**: Contributors working on Ledger must build/test the full operator
- **Code ownership**: Deployment logic for a component lives far from the component's code

## Constraints

- **Single pod in production** (infra team requirement — no N-pod operator sprawl)
- **Hot-pluggable modules** (deploy a new module version without recompiling/redeploying the host operator)
- **No diamond dependency** (each module has its own isolated `go.mod`)
- **Each module owns its CRD** (type definition + YAML manifest + reconciler live in the component repo)

## Decision: gRPC Plugin Architecture (go-plugin style)

Given the constraints above, the architecture is a **host operator + gRPC plugin subprocesses** running in a single pod:

```
┌─────────────────────────────────────────────────────┐
│  Pod: formance-operator                              │
│                                                      │
│  ┌──────────────────────────────────────────────┐   │
│  │  Host Process (formance-operator)                │   │
│  │  - Infra controllers (Stack, Database, ...)   │   │
│  │  - Plugin manager (discovers + launches)      │   │
│  │  - Leader election                            │   │
│  └────┬─────────┬─────────┬─────────┬───────────┘   │
│       │ gRPC    │ gRPC    │ gRPC    │ gRPC          │
│  ┌────▼───┐ ┌───▼────┐ ┌─▼──────┐ ┌▼────────┐     │
│  │ Ledger │ │Payments│ │Wallets │ │  Auth   │ ... │
│  │ plugin │ │ plugin │ │ plugin │ │ plugin  │     │
│  └────────┘ └────────┘ └────────┘ └─────────┘     │
│  (subprocesses, each with own k8s client)           │
└─────────────────────────────────────────────────────┘
```

Each plugin subprocess:
- Is a standalone binary (built from its component repo)
- Has its own `go.mod` (no diamond deps)
- Has its own k8s client (direct API calls, no informer cache)
- Communicates with the host via gRPC over unix socket
- Can be updated independently (host restarts the subprocess)

## CRD Ownership

### Key finding: zero cross-module field coupling

Analysis of the codebase reveals that **no module controller reads fields from another module's CRD**. Cross-module dependencies are purely existence checks (`HasDependency() -> bool`) using unstructured k8s objects. This means module CRD types can be fully decoupled.

### Split strategy

**Infrastructure CRDs** (owned by `operator SDK` / `formance-operator`):
- `Stack`, `Settings`, `Versions` — platform orchestration
- `Database` — shared by 7 modules
- `Broker`, `BrokerTopic`, `BrokerConsumer` — messaging infra
- `GatewayHTTPAPI` — created by any module exposing HTTP, consumed by Gateway
- `AuthClient` — created by modules needing OAuth clients, consumed by Auth
- `ResourceReference` — internal cross-references
- `Benthos`, `BenthosStream` — stream processing infra

**Module CRDs** (owned by each component repo):
- `Ledger` → `formancehq/ledger`
- `Payments` → `formancehq/payments`
- `Wallets` → `formancehq/wallets`
- `Gateway` → `formancehq/gateway`
- `Auth` → `formancehq/auth`
- `Orchestration` → `formancehq/orchestrations`
- `Webhooks` → `formancehq/webhooks`
- `Reconciliation` → `formancehq/reconciliation`
- `Search` → `formancehq/search`
- `Stargate` → `formancehq/stargate`
- `TransactionPlane` → `formancehq/transaction-plane`

Each module repo defines its CRD Go type, generates the CRD YAML, and ships it with its plugin binary.

### Cross-module watches

Modules declare dependencies via GVK constants (strings), not Go type imports:

```go
// In operator SDK — well-known GVK registry
var (
    LedgerGVK = schema.GroupVersionKind{Group: "formance.com", Version: "v1beta1", Kind: "Ledger"}
    AuthGVK   = schema.GroupVersionKind{Group: "formance.com", Version: "v1beta1", Kind: "Auth"}
    // ...
)
```

No module imports another module's Go types. The `operator SDK` provides only infrastructure types and GVK constants.

## Architecture

### Repository Structure

```
formancehq/operator                  <-- SDK + host operator (same repo as today)
├── api/formance.com/v1beta1/        <-- infrastructure CRD types + shared interfaces
│   ├── shared.go                    <-- Module, Dependent, Object, Resource interfaces
│   ├── stack_types.go               <-- Stack, Settings, Versions
│   ├── database_types.go            <-- Database
│   ├── broker_types.go              <-- Broker, BrokerTopic, BrokerConsumer
│   ├── gatewayhttpapi_types.go      <-- GatewayHTTPAPI
│   ├── authclient_types.go          <-- AuthClient
│   ├── resourcereference_types.go   <-- ResourceReference
│   ├── benthos_types.go             <-- Benthos, BenthosStream
│   └── gvk.go                       <-- well-known GVK constants
├── pkg/core/                        <-- framework (reconciler, context, errors)
├── pkg/databases/                   <-- Database CRD helpers
├── pkg/brokers/                     <-- Broker/Topic/Consumer helpers
├── pkg/gateway/                     <-- GatewayHTTPAPI helpers
├── pkg/authclients/                 <-- AuthClient helpers
├── pkg/registries/                  <-- image resolution
├── pkg/jobs/                        <-- Job/CronJob helpers
├── pkg/settings/                    <-- Settings reader
├── pkg/services/                    <-- Service helpers
├── pkg/plugin/                      <-- gRPC plugin framework
│   ├── proto/plugin.proto           <-- protocol definition
│   ├── host/                        <-- host-side: discovery, launch, proxy
│   └── server/                      <-- plugin-side: helpers to build a plugin binary
├── internal/controllers/            <-- infra controllers (Stack, Settings, Database, Broker...)
├── plugins/                         <-- plugin binaries (one per module, built into the image)
│   ├── ledger/main.go
│   ├── payments/main.go
│   └── ...
├── cmd/main.go                      <-- host operator entry point + plugin manager
├── config/                          <-- infrastructure CRD YAMLs, RBAC, etc.
├── Dockerfile                       <-- builds host + all plugins into one image
└── helm/                            <-- Helm charts

formancehq/ledger                    <-- component repo
├── ...                              <-- application code
├── api/v1beta1/                     <-- Ledger CRD type
│   ├── ledger_types.go
│   └── zz_generated.deepcopy.go
├── deploy/crds/                     <-- generated CRD YAML
├── deployments/operator/             <-- deployment logic
│   ├── controller.go                <-- reconciler
│   └── main.go                      <-- plugin binary entry point
└── go.mod                           <-- imports formancehq/operator (SDK)
```

### gRPC Protocol

The host is a **process manager** — it launches plugin subprocesses, monitors their health, and stops them gracefully. It does not proxy reconciliation or route events. Each plugin runs its own controller loop with its own k8s client.

The gRPC surface is minimal:

```protobuf
syntax = "proto3";
package formance.operator.plugin.v1;

service ModulePlugin {
    rpc Shutdown(ShutdownRequest) returns (ShutdownResponse);
    // + standard grpc.health.v1.Health for liveness monitoring
}

message ShutdownRequest {}
message ShutdownResponse {}
```

That's it. No `Describe`, no `Reconcile`. The plugin:
- Installs its own CRD via its own k8s client (CRD YAML embedded via `go:embed`)
- Registers its own controller with its own `ctrl.Manager`
- Watches and reconciles its own CRD independently

The host only needs to know the plugin **name** (from the binary filename or `ModuleExtension` CR name) for deduplication between embedded and hot-plugged plugins.

### Why the plugin runs its own controller loop

Each plugin subprocess uses a direct k8s client (no informer cache). A reconcile cycle does ~5-10 GET calls. With 11 modules, that's ~100 extra API calls per reconcile wave — negligible for any API server.

This means the reconciler code (`controller.go`) stays almost identical to today — it calls `databases.Create()`, `settings.Get()`, `HasDependencyGVK()` directly. No need to proxy k8s reads through gRPC, no need for a complex delegation protocol.

### ModuleExtension CRD (plugin discovery)

Instead of scanning a directory or using init containers, the host discovers plugins via a **`ModuleExtension` CRD**. This is fully Kubernetes-native, GitOps-friendly, and enables true hot-plug.

```yaml
apiVersion: formance.com/v1beta1
kind: ModuleExtension
metadata:
  name: ledger
spec:
  # OCI image containing the plugin binary
  image: ghcr.io/formancehq/ledger-operator-plugin:v2.1.0
status:
  ready: true
  activeVersion: "v2.1.0"
  phase: Running        # Pulling | Starting | Running | Error
  conditions:
    - type: PluginHealthy
      status: "True"
    - type: CRDInstalled
      status: "True"
```

### Plugin Lifecycle

```
Host starts
  │
  ├─ Starts infra controllers (Stack, Database, Broker, Settings...)
  │
  ├─ ModuleExtension controller watches ModuleExtension CRs
  │   │
  │   ├─ On create/update:
  │   │   ├─ Pull OCI image (spec.image)
  │   │   ├─ Extract plugin binary + CRD manifest from image
  │   │   │   (convention: /plugin binary, /crd.yaml manifest)
  │   │   ├─ Install CRD manifest (kubectl apply equivalent)
  │   │   ├─ Launch subprocess (go-plugin)
  │   │   ├─ Wait for gRPC health check to pass
  │   │   ├─ Update ModuleExtension status (phase=Running)
  │   │   └─ On crash: restart subprocess (exponential backoff)
  │   │
  │   ├─ On image tag change:
  │   │   ├─ Pull new image
  │   │   ├─ Graceful shutdown old subprocess
  │   │   ├─ Launch new subprocess
  │   │   └─ Update status (activeVersion)
  │   │
  │   └─ On delete:
  │       └─ Graceful shutdown subprocess
  │
  └─ mgr.Start() (infra controllers run)
     │
     Meanwhile, each plugin subprocess:
       ├─ Creates its own ctrl.Manager (with direct k8s client)
       ├─ Registers its reconciler via Register(mgr)
       ├─ Starts its manager (watches its own CRD)
       └─ Reconciles independently
```

### Plugin Binary (module side)

Each module produces a plugin binary using a helper from the operator SDK:

```go
// formancehq/ledger/deployments/operator/main.go
package main

import (
    "github.com/formancehq/ledger/api/v1beta1"
    "github.com/formancehq/operator/v3/pkg/plugin/server"
)

func main() {
    server.Run(server.Config{
        // Register function — same as standalone mode
        Register: func(mgr ctrl.Manager) error {
            return operator.Register(mgr)
        },

        // Scheme setup — register the module's CRD type
        SchemeSetup: func(s *runtime.Scheme) error {
            return v1beta1.AddToScheme(s)
        },
    })
}
```

`server.Run` does:
1. Creates a `ctrl.Manager` with a direct client (no shared cache)
2. Configures ServiceAccount impersonation (RBAC isolation)
3. Calls `Register(mgr)` to set up the reconciler
4. Starts a gRPC server on a unix socket (Shutdown + health RPCs)
5. Starts the manager (CRD must already exist — installed by Helm or host)
6. Signals readiness to the host via go-plugin handshake

### Host Plugin Manager

```go
// formancehq/operator/cmd/main.go
func main() {
    mgr, _ := ctrl.NewManager(...)

    // Infrastructure controllers
    infra.Register(mgr)

    // ModuleExtension controller — watches CRs, pulls images, launches plugins
    pm := plugin.NewManager(mgr)
    mgr.Add(pm)

    mgr.Start(ctx)
}
```

The plugin manager is itself a controller-runtime reconciler watching `ModuleExtension` CRs:

```go
func (pm *PluginManager) Reconcile(ctx context.Context, ext *v1beta1.ModuleExtension) (reconcile.Result, error) {
    existing := pm.plugins[ext.Name]

    // Skip if an embedded plugin with same name is already running
    if pm.embedded[ext.Name] != nil {
        ext.Status.Phase = "Skipped"
        ext.Status.Ready = true
        return reconcile.Result{}, nil
    }

    // Image changed or first time → (re)start plugin
    if existing == nil || existing.image != ext.Spec.Image {
        if existing != nil {
            existing.Shutdown()
        }

        binary, err := pm.pullBinary(ctx, ext.Spec.Image)
        if err != nil {
            ext.Status.Phase = "Error"
            return reconcile.Result{}, err
        }

        p, err := pm.launchPlugin(ext.Name, binary)
        if err != nil {
            ext.Status.Phase = "Error"
            return reconcile.Result{}, err
        }

        pm.plugins[ext.Name] = p
        ext.Status.Phase = "Running"
        ext.Status.ActiveVersion = imageTag(ext.Spec.Image)
        ext.Status.Ready = true
    }

    // Health check
    if !pm.plugins[ext.Name].Healthy() {
        ext.Status.Phase = "Error"
        ext.Status.Ready = false
        return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
    }

    return reconcile.Result{}, nil
}
```

### OCI Image Format

Each module publishes a minimal OCI image containing just the plugin binary:

```dockerfile
# formancehq/ledger/Dockerfile.operator-plugin
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /plugin ./deployments/operator/

FROM scratch
COPY --from=build /plugin /plugin
COPY deploy/crds/ledger.yaml /crd.yaml
ENTRYPOINT ["/plugin"]
```

The host pulls the image using `go-containerregistry` (same library used by crane/ko), extracts the `/plugin` binary, and writes it to a temp directory before launching.

### Deployment (Helm)

The operator Helm chart deploys the host. Modules are declared as `ModuleExtension` CRs:

```yaml
# values.yaml
modules:
  ledger:
    image: ghcr.io/formancehq/ledger-operator-plugin:v2.1.0
  payments:
    image: ghcr.io/formancehq/payments-operator-plugin:v1.5.0
  wallets:
    image: ghcr.io/formancehq/wallets-operator-plugin:v3.0.0
```

The chart templates generate `ModuleExtension` CRs from values:

```yaml
{{- range $name, $config := .Values.modules }}
apiVersion: formance.com/v1beta1
kind: ModuleExtension
metadata:
  name: {{ $name }}
spec:
  image: {{ $config.image }}
{{- end }}
```

**To update a module:** change the image tag in values → Helm upgrade (or ArgoCD sync) → host reconciles the `ModuleExtension` → pulls new binary → restarts subprocess. **The operator pod does not restart.**

**To add a new module:** add an entry in values → Helm upgrade → new `ModuleExtension` CR created → host pulls and launches. **Zero operator pod disruption.**

### Embedded Mode (air-gapped / offline)

For clients who can't pull images at runtime (network restrictions, air-gapped environments, mirror-only policies), plugin binaries can be **baked into the operator image** at build time.

The `formancehq/operator` repo provides a `Dockerfile` that copies plugin binaries from their OCI images:

```dockerfile
# formancehq/operator/Dockerfile
FROM ghcr.io/formancehq/ledger-operator-plugin:v2.1.0    AS plugin-ledger
FROM ghcr.io/formancehq/payments-operator-plugin:v1.5.0  AS plugin-payments
FROM ghcr.io/formancehq/wallets-operator-plugin:v3.0.0   AS plugin-wallets
# ... more plugins

FROM ghcr.io/formancehq/formance-operator:v1.0.0
COPY --from=plugin-ledger   /plugin /plugins/ledger
COPY --from=plugin-payments /plugin /plugins/payments
COPY --from=plugin-wallets  /plugin /plugins/wallets
# ...
```

The host discovers embedded plugins via a `--plugin-dir=/plugins` flag. At startup, **before** watching `ModuleExtension` CRs, it scans this directory and launches any binaries found:

```
Host starts
  │
  ├─ Scan --plugin-dir for embedded plugin binaries
  │   └─ For each: launch subprocess, wait for health check
  │
  ├─ Watch ModuleExtension CRs (for hot-plug plugins)
  │   └─ Skip if a plugin for this GVK is already running (embedded takes precedence)
  │
  └─ mgr.Start()
```

**Both modes coexist.** A cluster can have some modules embedded and others hot-plugged via `ModuleExtension`. The precedence rule is simple: if an embedded plugin already handles a GVK, the `ModuleExtension` for that GVK is ignored (status reports `Skipped: embedded plugin active`).

The CI pipeline produces a single image containing the host + all configured plugins:
- `ghcr.io/formancehq/formance-operator:v1.0.0` — host + embedded plugins

Additional modules can be hot-plugged at runtime via `ModuleExtension` CRs.

### Dev Experience

In development, a contributor working on Ledger:

```bash
# In formancehq/ledger repo

# Run as standalone operator (no host needed, no gRPC)
cd deployments/operator
go run ./main.go --standalone --kubeconfig ~/.kube/config

# Run as plugin connected to a local host
go run ./main.go --socket=/tmp/ledger.sock

# Tests with envtest
go test ./...
```

The same binary works as both standalone and plugin. The `server.Run` helper detects the mode from flags/env.

## Comparison

| | Standalone | Plugin (ModuleExtension) | Plugin (Embedded) |
|---|---|---|---|
| **Single pod** | No | **Yes** | **Yes** |
| **Hot-pluggable** | Yes (new pod) | **Yes** (image tag change) | No (rebuild image) |
| **Diamond deps** | None | **None** | **None** |
| **k8s caches** | N (1 per pod) | 1 host + N direct clients | 1 host + N direct clients |
| **Network access** | N/A | Needs registry pull | **Air-gapped OK** |
| **Dev experience** | `go run ./deployments/operator` | Same | Same |
| **Release coupling** | Independent | **Independent** | Atomic (rebuild bundled image) |
| **Use case** | Dev / testing | **Production default** | **Air-gapped production** |

## Migration Path

### Phase 1: Reorganize the repo as SDK + host + plugins (in-repo)

Everything stays in `formancehq/operator`. No new repos.

1. Move `internal/core/` → `pkg/core/` (exported SDK package)
2. Move infrastructure helpers (`internal/resources/databases`, `brokers`, `settings`, etc.) → `pkg/`
3. Keep infrastructure controllers in `internal/controllers/`
4. Add GVK constants in `api/formance.com/v1beta1/gvk.go`
5. The Go module path stays `github.com/formancehq/operator/v3`

### Phase 2: Build the plugin framework

1. Define `proto/plugin.proto` (Shutdown + health)
2. Implement `pkg/plugin/server/` — helper for building plugin binaries
3. Implement `pkg/plugin/host/` — plugin manager (ModuleExtension controller, embedded discovery, launch, monitor)
4. Add `ModuleExtension` CRD
5. Test with a mock plugin

### Phase 3: Convert modules to plugins (in-repo)

Each module gets a plugin binary entry point in the operator repo. The reconciler code stays where it is — only the wiring changes.

```
formancehq/operator
├── plugins/                          <-- plugin binaries (one per module)
│   ├── ledger/main.go               <-- wraps internal/resources/ledgers in server.Run
│   ├── payments/main.go
│   ├── wallets/main.go
│   ├── auth/main.go
│   ├── gateway/main.go
│   ├── orchestrations/main.go
│   ├── webhooks/main.go
│   ├── reconciliations/main.go
│   ├── search/main.go
│   ├── stargate/main.go
│   └── transactionplane/main.go
├── internal/resources/               <-- reconciler code (unchanged)
│   ├── ledgers/
│   ├── payments/
│   └── ...
├── pkg/                              <-- SDK (exported)
├── cmd/main.go                       <-- host operator
└── Dockerfile                <-- bakes all plugin binaries into one image
```

Each `plugins/<module>/main.go` is a thin wrapper:

```go
package main

import (
    "github.com/formancehq/operator/v3/internal/resources/ledgers"
    "github.com/formancehq/operator/v3/pkg/plugin/server"
)

func main() {
    server.Run(server.Config{
        Name:        "ledger",
        Register:    ledgers.Register,
        SchemeSetup: ledgers.AddToScheme,
    })
}
```

This gives us:
- All modules runnable as plugins **today**, without moving code to other repos
- The `Dockerfile` produces a single image with host + all embedded plugins (one pod, air-gapped OK)
- Additional or third-party modules can be hot-plugged via `ModuleExtension` CRs without rebuilding the image
- In dev, any plugin can run standalone with `go run ./plugins/ledger --standalone`

### Phase 4 (later): Move modules to component repos

Once the plugin architecture is validated, modules can be progressively moved to their component repos. Order by complexity:

1. **Stargate** (zero dependencies)
2. **Search** (zero dependencies)
3. **Wallets** (depends on Auth via GVK — existence check only)
4. **Ledger**, **Payments** (depend on Database + Broker infra)
5. **Auth**, **Gateway** (depend on AuthClient / GatewayHTTPAPI infra)
6. **Orchestrations**, **Webhooks**, **Reconciliations**, **TransactionPlane** (multi-dependency)

This phase is optional and can happen on each module's own timeline.

## RBAC: Per-Plugin Least Privilege

All plugin subprocesses run in the same pod and share the same ServiceAccount. To enforce least-privilege per module, plugins use **Kubernetes ServiceAccount impersonation**.

### How it works

Each plugin's k8s client impersonates a dedicated ServiceAccount with narrow permissions:

```go
// Inside pkg/plugin/server — automatically configured by server.Run
cfg := ctrl.GetConfigOrDie()
cfg.Impersonate = rest.ImpersonationConfig{
    UserName: "system:serviceaccount:formance-system:plugin-" + pluginName,
}
mgr, _ := ctrl.NewManager(cfg, ctrl.Options{...})
```

### RBAC setup

The host ServiceAccount needs only the `impersonate` verb:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: formance-operator-host
rules:
  # Host can impersonate plugin ServiceAccounts
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    verbs: ["impersonate"]
  # Host's own permissions (Stack, Database, Broker, Settings, ModuleExtension...)
  - apiGroups: ["formance.com"]
    resources: ["stacks", "stacks/status", "settings", "databases", "databases/status",
                "brokers", "brokertopics", "brokerconsumers", "moduleextensions", "moduleextensions/status"]
    verbs: ["*"]
  # ...
```

Each plugin gets a dedicated ServiceAccount + ClusterRole with only what it needs:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: plugin-ledger
  namespace: formance-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: formance-plugin-ledger
rules:
  # Own CRD
  - apiGroups: ["formance.com"]
    resources: ["ledgers", "ledgers/status"]
    verbs: ["get", "list", "watch", "update", "patch"]
  # Infrastructure CRDs (create databases, broker topics, gateway APIs...)
  - apiGroups: ["formance.com"]
    resources: ["databases", "gatewayhttpapis", "brokertopics", "settings", "resourcereferences"]
    verbs: ["get", "list", "watch", "create", "update"]
  # Kubernetes resources it manages
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["services", "configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: formance-plugin-ledger
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: formance-plugin-ledger
subjects:
  - kind: ServiceAccount
    name: plugin-ledger
    namespace: formance-system
```

### Automation

The `server.Run` helper configures impersonation automatically from the plugin name. The Helm chart generates the ServiceAccount + ClusterRole + ClusterRoleBinding for each module, either from values (for `ModuleExtension` plugins) or statically (for embedded plugins).

Each module can declare its RBAC needs in its `server.Config`:

```go
server.Run(server.Config{
    Name:        "ledger",
    Register:    ledgers.Register,
    SchemeSetup: ledgers.AddToScheme,
    // RBAC rules — used by Helm chart generation, not enforced at runtime
    RBACRules: []rbacv1.PolicyRule{
        {APIGroups: []string{"apps"}, Resources: []string{"deployments"}, Verbs: []string{"*"}},
        {APIGroups: []string{""}, Resources: []string{"services", "configmaps"}, Verbs: []string{"*"}},
        // ...
    },
})
```

## Estimated Development Time

~15h of effective work (~2 days).

| Phase | Estimate | Detail |
|---|---|---|
| Phase 1 — Reorganize as SDK + host | ~4h | Move packages `internal/` → `pkg/`, update imports, GVK-based watches, `Register(mgr)` per module |
| Phase 2 — Plugin framework | ~8h | go-plugin integration, `server.Run` helper, ModuleExtension CRD + controller, OCI image pull, RBAC impersonation, embedded discovery |
| Phase 3 — Convert modules to plugins | ~3h | 11 thin wrappers, Dockerfile, Helm chart, CI |

## Open Questions

1. **Versioning**: How to handle breaking changes in `operator SDK`? Semver with compatibility guarantees? The gRPC protocol must be stable — plugins built against sdk v1 must work with a host built against sdk v1.x.

2. **Testing**: Each module tests in isolation with envtest + infrastructure CRDs pre-installed. E2E tests in a separate repo test the full plugin setup.

3. **Logging & observability**: Plugin logs must be aggregated with the host's logs. go-plugin captures subprocess stderr. Structured logging with a `plugin=ledger` field.

4. **Graceful migration**: During the transition period, some modules run in-process (not yet extracted) and others run as plugins. The host must support both modes simultaneously.
