# Declaring module requirements

## Status

This document defines the intended design for declaring and enforcing dependencies between Stack-bound objects. The first implementation is deliberately a focused proof of concept. Existing implicit checks should be migrated only after their current lifecycle behaviour is covered by tests.

## Context

Some module reconcilers currently coordinate with another object in the same Stack:

- Webhooks and Orchestration eventually require a ready `Broker`, but also create resources that participate in provisioning it;
- Connectivity requires a ready Ledger whose effective version supports the Ledger v3 integration;
- future versions of a module may support only a bounded range of another module's versions.

The first implementation declares seven confirmed compatibility requirements:

- Connectivity requires Ledger `>= v3.0.0-0`;
- MCP, Orchestration (Flows), Reconciliation, TransactionPlane, Wallets, and Webhooks require Ledger `< v3.0.0-0`.

These declarations require presence and version compatibility. They do not enforce Ledger readiness. Connectivity retains its existing Ledger readiness check; the other six modules do not add one in this change.

These rules are currently implicit in individual reconcilers. Each caller is responsible for finding the dependency, resolving versions, checking readiness, installing watches, reporting errors, and deciding whether reconciliation can continue. The resulting behaviour is difficult to discover and is not consistent across modules.

Stack release names and `Versions.metadata.name` do not describe these relationships. A Stack release is a selection of artifact versions. Compatibility must be evaluated from the objects actually installed in the Stack and their effective versions. `GetModuleVersion` currently applies a separate minimum-support policy to a `Versions` resource name; dependency constraints must use `ResolveModuleVersion` so that policy is not confused with artifact compatibility.

## Decision

Requirements are declared in Go when a module reconciler is registered. Their evaluation is implemented once in the core reconciliation module.

Requirements are not stored in a CRD. The Operator supports a closed set of compiled reconcilers, so externally configurable compatibility rules would introduce a second, mutable source of truth without allowing the Operator to implement new deployment behaviour dynamically.

The declaration is code, but it is separate from the reconciler implementation:

```go
WithModuleReconciler(
	Reconcile,
	Requirements(
		Require(
			&v1beta1.Broker{},
			Ready(),
		),
	),
	// Existing reconciler options.
)
```

A module without dependencies declares that choice explicitly:

```go
WithModuleReconciler(
	Reconcile,
	NoRequirements(),
)
```

Making `Requirements(...)` or `NoRequirements()` a required argument ensures that a new module cannot omit the decision accidentally.

## Domain model

### Requirement

A **requirement** is a condition declared by a module about one other Stack-bound object. The first implementation supports:

- **presence**: exactly one matching, non-deleting object exists in the same Stack;
- **readiness**: the dependency reports `status.ready=true`;
- **effective version**: a dependency that implements `v1beta1.Module` resolves to a version inside a declared SemVer range.

All requirements are mandatory and combined with logical AND. Optional relationships remain ordinary reconciler behaviour until a concrete optional-requirement use case justifies extending the interface.

### Dependency

A **dependency** is the concrete object that satisfies a requirement. It may be a module, such as `Ledger`, or an infrastructure resource, such as `Broker`.

The declaration references the Kubernetes type directly. It does not introduce an abstract capability name when an existing domain object already expresses the relationship.

### Effective version

The **effective version** of a module is resolved through the existing precedence:

1. the module's `spec.version` override;
2. `stack.spec.version`;
3. the entry for the module kind in the referenced `Versions` resource.

Requirement evaluation must use `ResolveModuleVersion`; it must never read a dependency's `spec.version` directly.

### Compatibility and availability

Compatibility and operational availability are distinct:

- a missing dependency or a known version outside the allowed range is an unsatisfied requirement;
- an unresolved version is an unknown result, because the Operator cannot prove compatibility;
- a present but unready dependency is compatible but not operationally available.

The initial implementation exposes these states through one positive-polarity condition while retaining distinct reasons.

## Interface

The public interface of the core requirements module should remain small:

```go
type ModuleRequirements struct {
	// private implementation
}

type Requirement struct {
	// private implementation
}

type RequirementOption interface {
	apply(*Requirement)
}

func Requirements(requirements ...Requirement) ModuleRequirements
func NoRequirements() ModuleRequirements

func Require(
	dependency v1beta1.Dependent,
	options ...RequirementOption,
) Requirement

func Ready() RequirementOption
func VersionAtLeast(minInclusive string) RequirementOption
func VersionBefore(maxExclusive string) RequirementOption
func VersionBetween(minInclusive, maxExclusive string) RequirementOption

func WithUnsatisfiedRequirementsHandler[T v1beta1.Module](
	handler func(Context, *v1beta1.Stack, T) error,
) ReconcilerOption[T]
```

The concrete fields remain private so callers cannot construct invalid ranges through the supported constructors. Go still permits a zero struct value, but controller setup rejects it.

Examples:

```go
// Webhooks requires an operational Broker.
Requirements(
	Require(&v1beta1.Broker{}, Ready()),
)

// Example of a legacy consumer of Ledger versions before v3.
Requirements(
	Require(
		&v1beta1.Ledger{},
		VersionBefore("v3.0.0-0"),
		Ready(),
	),
)

// Example of a Ledger v3 consumer.
Requirements(
	Require(
		&v1beta1.Ledger{},
		VersionAtLeast("v3.0.0-0"),
		Ready(),
	),
)
```

## Validation invariants

The Operator must reject an invalid declaration during controller setup:

- the zero value of `ModuleRequirements` is invalid;
- `Requirements()` with no entries is invalid; use `NoRequirements()`;
- every dependency prototype must be a pointer registered in the runtime Scheme and implement `v1beta1.Dependent`;
- version constraints may target only dependencies implementing `v1beta1.Module`;
- all SemVer bounds must be valid after normalizing an optional leading `v`;
- a lower bound must be strictly lower than an upper bound;
- the same dependency GroupKind may appear at most once in one declaration.

Invalid declarations are programming errors and prevent the controller manager from starting.

## Reconciliation order

Requirement evaluation belongs in the existing module reconciliation seam. The intended order is:

1. run finalizers before the normal object controller when the module is deleting;
2. load the referenced Stack and maintain the Stack label;
3. honour the Stack skip annotation and Stack deletion;
4. maintain the owner reference and honour disabled Stack behaviour;
5. resolve the module's own effective version;
6. evaluate declared requirements;
7. update `DependenciesSatisfied` and, on failure, `ReconciledWithStack`;
8. stop on `False` or `Unknown` without calling the module reconciler;
9. evaluate the EE licence when applicable;
10. invoke the module reconciler;
11. set `ReconciledWithStack=True` after successful reconciliation.

Skip, disabled, and `NoRequirements()` paths remove a previous `DependenciesSatisfied` condition so a stale failure cannot keep the module unready.

Requirement failures are desired-state results, not controller failures. They return an application pending error so status is persisted without producing a noisy controller-runtime error. Relevant watches trigger a new evaluation when the state changes. In the absence of a simultaneous definite compatibility failure, Kubernetes lookup failures remain controller errors after writing an `Unknown` condition so controller-runtime retries transient infrastructure failures.

## Conditions

Every module with a non-empty `Requirements(...)` declaration receives a `DependenciesSatisfied` condition. `NoRequirements()` removes a condition left by an older declaration.

| Status | Reason | Meaning |
| --- | --- | --- |
| `True` | `RequirementsSatisfied` | Every dependency exists and satisfies all declared checks. |
| `False` | `DependencyNotFound` | A required object does not exist in the Stack. |
| `False` | `MultipleDependenciesFound` | More than one object of the required kind exists. |
| `False` | `DependencyNotReady` | A required dependency exists but is not ready. |
| `False` | `DependencyVersionMismatch` | A known effective version is outside the declared range. |
| `Unknown` | `DependencyVersionUnresolved` | The effective version could not be resolved. |
| `Unknown` | `DependencyVersionNotSemVer` | A resolved opaque tag cannot be ordered against a SemVer range. |
| `Unknown` | `DependencyLookupFailed` | The dependency could not be read from Kubernetes. |

Messages include the dependency kind, observed value, and expected constraint. Requirements are evaluated and reported in stable GroupKind order so tests and operator output are deterministic. The message aggregates the first violation reported for each dependency; the reason is the first definite failure in GroupKind order, or the first unknown result when no definite failure exists.

When a previously satisfied requirement becomes unsatisfied, `ReconciledWithStack` must become `False`. This prevents an old successful condition from leaving the parent Stack ready.

## Watches

The core implementation derives watches from declarations:

- creation, update, or deletion of a dependency requeues consumers in the same Stack;
- Stack generation and annotation changes already requeue its modules;
- an update to a referenced `Versions` resource requeues a consumer when either its own entry or a version-constrained dependency entry changes.

Callers should not also register a manual watch for a dependency already covered by a requirement. The generated requirement watch replaces an existing watcher for the same Go type, including a custom handler, so a module that needs custom watch mapping must keep that relationship outside the generic requirement declaration until the interface supports composition of handlers.

## Lifecycle behaviour

An unsatisfied requirement blocks the normal module reconciler, but it does not delete existing workloads by default. Automatic deletion is unsafe as a generic policy because modules have different migration and data-retention requirements.

Modules such as Connectivity may need explicit cleanup after a certain incompatibility, while retaining resources for transient states such as an unresolved version or temporary unready status. `WithUnsatisfiedRequirementsHandler` provides this module-specific hook and invokes it for every definite `False` evaluation. The handler must filter the reasons that are destructive in its own domain. Connectivity reuses `ledgerGateClosed` so cleanup occurs only when Ledger is missing or resolves below the v3 boundary, never for an unresolved version, multiple Ledger objects, or temporary unready status.

Legacy Ledger consumers require an additional transition guard. Changing the
desired Ledger version to v3 does not immediately delete their working v2
runtimes. Instead, the Ledger reconciler refuses to materialize the primary v3
`Cluster` while MCP, Orchestration, Reconciliation, TransactionPlane, Wallets,
or Webhooks objects still exist. The module objects are watched, so deleting the
last incompatible module automatically resumes Ledger reconciliation.

If a primary Ledger v3 `Cluster` already exists alongside one of these legacy
modules, its unsatisfied-requirements handler removes only active runtime and
exposure resources, such as Deployments, Jobs, Gateway routes, consumers, and
credentials. Databases, underlying broker streams, and other durable data are
retained for an explicit migration or recovery. A v3 preview cluster does not
trigger this cleanup and can continue to run alongside Ledger v2.

Finalizers continue to run even when requirements are unsatisfied, so removing an incompatible module can always unblock a Stack.

A pre-reconcile requirement must not target an object that can only be created by the blocked reconciler. Such a declaration would deadlock a fresh Stack. Webhooks and Orchestration currently create `BrokerConsumer` and `BrokerTopic` resources before waiting for Broker-dependent work. Migrating those relationships requires a separate preparation phase before the requirements gate, or proof that Broker provisioning is independent of the consumer.

Readiness cycles are invalid for the same reason: two modules that both require the other to be ready can never enter their normal reconcilers. The initial proof detects the individual states but does not attempt to solve provisioning or readiness cycles.

## What remains in module implementations

Requirements describe whether reconciliation may proceed. They do not describe how dependencies are provisioned or consumed. The following behaviour stays local:

- creating `BrokerConsumer` and `BrokerTopic` resources;
- creating NATS streams or configuring another Broker adapter;
- database migrations;
- API and worker rollout ordering;
- credentials and module-specific cleanup.

For example, a future ready `Broker` requirement could remove the repeated presence/readiness gate and manual watch after Broker preparation is independent. It would not replace the Webhooks or Orchestration logic that creates consumers and topics.

## Rollout strategy

1. Implement and unit-test the requirements model.
2. Integrate it into `WithModuleReconciler` and require every module registration to choose `Requirements(...)` or `NoRequirements()`.
3. Declare the confirmed Ledger version relationships for Connectivity, MCP, TransactionPlane, Orchestration, Reconciliation, Wallets, and Webhooks.
4. Demonstrate Broker presence/readiness and Ledger version constraints with focused core tests.
5. Add focused tests for effective-version overrides, `Versions` lookup, invalid SemVer, range boundaries, generated dependency watches, and module-specific cleanup hooks.
6. Separate preparation from gating before migrating Broker readiness or another dependency provisioned by its consumer.
7. Migrate remaining implicit dependencies one at a time.

A declaration must reflect a confirmed product constraint; an existing watch alone is not evidence of a mandatory dependency. A watch may support optional event discovery rather than compatibility.

## Alternatives considered

### Compatibility CRD

Rejected for the current architecture. It would add a mutable source of truth, a new authorization surface, CRD versioning, and runtime validation without allowing the compiled Operator to implement unknown deployment behaviour.

### Named capabilities

Deferred. Names such as `messaging.durable-consumers/v1` are not part of the current domain language and obscure direct relationships such as “Webhooks requires Broker”. A capability seam becomes useful only when multiple distinct domain objects can satisfy the same requirement while callers must remain independent of their concrete kinds.

### Checks inside each reconciler

Rejected as the default. This is the current implicit model and duplicates discovery, version resolution, watches, conditions, and error semantics across callers.

## Proof-of-concept acceptance criteria

The design is demonstrated when:

- module registrations must explicitly declare requirements or their absence;
- a missing or unready required Broker blocks a test consumer before its reconciler runs;
- Ledger effective-version constraints work for a module override, Stack version, and `Versions` resource;
- dependency and relevant `Versions` changes requeue the consumer;
- conditions distinguish missing, unready, mismatched, and unresolved dependencies;
- an unsatisfied requirement never triggers generic workload deletion;
- Connectivity cleanup runs only for definite requirement failures;
- a Ledger v2 to v3 transition keeps legacy module runtimes active while blocking primary v3 Cluster materialization;
- an already materialized primary Ledger v3 Cluster removes incompatible module runtimes and exposure without deleting durable data;
- Connectivity, MCP, TransactionPlane, Orchestration, Reconciliation, Wallets, and Webhooks declare their confirmed Ledger ranges;
- existing module tests continue to pass.
