# Adding a module

This guide describes how to add a new stack module to the operator, as the
process works today. A module is a cluster-scoped custom resource in the
`formance.com` API group, reconciled per stack. A module either runs its own
workload (Deployment/Service/…) or **delegates** to a separate operator
(e.g. Ledger v3 → `ledger.formance.com`, Connectivity → `connectivity.formance.com`)
and simply provisions and tracks a resource owned by that operator.

Use the `Ledger` module (`api/formance.com/v1beta1/ledger_types.go`,
`internal/resources/ledgers`) or the `Connectivity` module
(`internal/resources/connectivities`) as reference implementations.

## 1. Define the module CR type

Create `api/formance.com/v1beta1/<module>_types.go`, mirroring an existing
module:

- `type <Module>Spec struct { ModuleProperties \`json:",inline"\`; StackDependency \`json:",inline"\`; … }`
- `type <Module>Status struct { Status \`json:",inline"\` }`
- Kubebuilder markers on the root type:
  - `+kubebuilder:object:root=true`
  - `+kubebuilder:subresource:status`
  - `+kubebuilder:resource:scope=Cluster`
  - print columns for Stack / Ready / Info / Version
  - **`+kubebuilder:metadata:labels=formance.com/kind=module`** — this label is
    how the platform (membership, tooling, the Stack) discovers the CRD as a
    module. **It is mandatory.** Omit it and the module is invisible to the
    stack machinery even though the controller and RBAC exist.
- Implement the `v1beta1.Module` interface: `IsEE`, `IsReady`, `SetReady`,
  `SetError`, `GetConditions`, `GetVersion`, `GetStack`, `IsDebug`, `IsDev`.
- Register the types: `func init() { SchemeBuilder.Register(&<Module>{}, &<Module>List{}) }`.

## 2. Write the reconciler

Create `internal/resources/<modules>/init.go`:

```go
func Reconcile(ctx Context, stack *v1beta1.Stack, module *v1beta1.<Module>, version string) error {
    // ... reconcile logic ...
}

func init() {
    Init(
        WithModuleReconciler(Reconcile,
            WithOwn[*v1beta1.<Module>](&appsv1.Deployment{}),   // owned resources
            WithWatchSettings[*v1beta1.<Module>](),
            WithWatchDependency[*v1beta1.<Module>](&v1beta1.Ledger{}), // re-reconcile on dependency change
        ),
    )
}
```

Add RBAC markers (they generate `config/rbac/role.yaml` and the chart
ClusterRole):

```go
//+kubebuilder:rbac:groups=formance.com,resources=<modules>,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=<modules>/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=<modules>/finalizers,verbs=update
// plus any owned/foreign resources the reconciler touches
```

## 3. Register the package

Add a blank import in `internal/resources/all.go` so the package `init()` runs
and the reconciler is wired into the manager:

```go
_ "github.com/formancehq/operator/v3/internal/resources/<modules>"
```

## 4. Register the CRD in kustomize

Add the generated base to `config/crd/kustomization.yaml` (non-Helm/kustomize
installs create CRDs from this list):

```yaml
- bases/formance.com_<modules>.yaml
```

## 5. (Delegating modules only) capability detection

If the module delegates to a separate operator, detect that operator's CRD at
controller start-up instead of assuming it is present — mirror
`withLedgerV3ClusterWatch` (ledgers) / `withConnectivityClusterWatch`
(connectivities):

1. List `CustomResourceDefinition`s and check the foreign GVK exists with a
   served version.
2. Run a `SelfSubjectAccessReview` for each verb you need (requires
   `//+kubebuilder:rbac:groups=authorization.k8s.io,resources=selfsubjectaccessreviews,verbs=create`).
3. Only `b.Owns(...)` / watch the foreign resource when available.
4. When unavailable, set a condition and return `NewPendingError()` — never
   fail controller setup.

## 6. Resolve versions correctly

When you need a module's effective version (e.g. to gate on the ledger being
v3), use `core.ResolveModuleVersion(ctx, stack, module)` — **not**
`module.Spec.Version`. `ResolveModuleVersion` also reads `spec.versionsFromFile`
stacks. Every module must have a version resolvable via the module override,
`stack.spec.version`, or the referenced `Versions` file; otherwise
`GetModuleVersion` errors and the module never reconciles.

## 7. Generate and validate

Run the full generation pipeline (or `just pre-commit`, which also lints and
tidies):

```sh
just generate        # deepcopy (zz_generated.deepcopy.go)
just manifests       # CRD bases + config/rbac/role.yaml
just helm-update     # helm chart CRDs + ClusterRole
just generate-docs   # CRD reference docs
just generate-settings-catalog
```

Commit **all** generated files — CI's "Dirty" check runs `just pre-commit` and
fails if the tree is not clean. Then `go build ./...` and `go test ./...`.

## 8. Tests

Add unit tests for the capability-gating / reconcile branches. See
`internal/resources/connectivities/init_test.go` and
`internal/resources/ledgers/v3_test.go` for the fake-`Context` pattern used to
exercise capability detection without envtest.

## Deployment gotchas

- **RBAC only reaches the cluster when the chart re-renders.** The operator
  `HelmRelease` must use `reconcileStrategy: Revision` (not the default
  `ChartVersion`). With `ChartVersion` and an unbumped `Chart.yaml`, Flux keeps
  serving the previously packaged chart, so new RBAC rules are never applied and
  the controller gets `forbidden` on the new resource.
- **The capability probe runs once, at operator start-up.** After installing a
  delegated CRD or granting RBAC, **restart the operator** so the probe
  re-evaluates — otherwise the capability stays cached as unavailable.
- **`versionsFromFile` stacks** must have the module listed in the referenced
  `Versions` file, or the module will not reconcile (see step 6).
