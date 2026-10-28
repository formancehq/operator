## Requirements

Formance Connectivity requires:
- **Ledger v3**: Connectivity ingests double-entry transactions into the stack ledger through its gRPC endpoint. The effective Ledger version must be a semantic major v3 version. Ledger v2 migration previews and opaque development references are not supported because they do not select the primary v3 topology.
- **Ledger operator**: its Credentials CRD and controller must support `spec.superuser`. Install the compatible Ledger operator and CRD before deploying this Operator.
- **Connectivity operator**: the module delegates the actual workload to a `connectivity.formance.com/Connectivity` resource; install matching CRDs and a controller implementing the [lifecycle contract](#lifecycle-compatibility-and-recovery) before this Stack Operator.

## Connectivity Object

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#connectivity).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Connectivity
metadata:
  name: formance-dev
spec:
  stack: formance-dev
```

The Operator provisions the delegated resource bound to the stack ledger (gRPC address, TLS material, and an Ed25519 credential registered on the ledger), and exposes the companion `connectivity-api` service through the stack gateway under `/api/connectivity`.

## API authentication

When the stack has an **Auth** module, the `connectivity-api` is protected like the other stack modules: it validates OIDC bearer tokens against the stack auth issuer. Without an Auth module on the stack, the API runs unauthenticated.

The public gateway route is exposed only after the Operator has verified that the delegated API Deployment and Service match the desired authentication state, including an explicitly unauthenticated state. The route is temporarily revoked while that rollout is changing or cannot be verified, so the module fails closed at the cost of brief API unavailability during those transitions.

During reconciliation, the Operator may adopt same-name ownerless delegated Connectivity and Ledger Credentials resources by setting the expected controller reference, after which it manages their desired state and lifecycle. A resource already controlled by another object is rejected. During teardown, direct reads delete only resources whose controller ownership matches the current Connectivity chain; resources that are foreign-owned or ownerless at that point are preserved. Deletes use UID preconditions so a same-name replacement cannot be removed accidentally. This contract also applies to the scoped Ledger credential.

## Ledger credential

The Operator provisions a cluster-scoped `ledger.formance.com/Credentials` named `connectivity-<stack>`, selected onto the stack's ledger Cluster with `spec.selector.matchLabels[formance.com/stack]`, with superuser mode disabled and exactly the scope set Connectivity's ingestion path requires:

- `ledger:AccountRead` — cursor `GetAccount`
- `ledger:LedgerWrite` — `CreateLedger`, `CreateIndex`, `SaveNumscript`
- `ledger:TransactionWrite` — plugin transaction actions and cursor save in `Apply`
- `ledger:MetadataWrite` — plugin metadata actions and cursor save in `Apply`
- `ledger:QueryWrite` — `CreatePreparedQuery`

The scope list is owned by the integration contract (see the [Ledger capability matrix](https://github.com/formancehq/connectivity/blob/main/docs/architecture/ledger-capabilities.md)); it is not configurable through the Connectivity CRD and is updated only when Connectivity's actual Ledger calls change. The delegated binding uses the supported `spec.auth.keyIdSecretKeyRef` and `spec.auth.secretKeyRef`, referencing `key-id` and `seed.hex` in the same Ledger-distributed Secret, with `spec.auth.subject=connectivity`. Reconciliation replaces the auth map to remove old inline or bundle fields. The Connectivity operator watches both Secret keys and restarts Core when either changes. The Operator creates no credential bundle or additional signing Secret.

An existing superuser Credentials converges to the narrowed spec on the next reconciliation, retaining its key ID and distributed Secret. After any credential update, the Operator waits for `status.phase=Ready` and `status.observedGeneration` to match `metadata.generation` before proceeding with delegation. This status confirms credential distribution; Ledger Cluster reconciliation applies the grants separately. Deploy a compatible Connectivity Core that emits scoped tokens with these scopes before narrowing the registered key; older Core versions that emit `superuser=true` will fail authentication after the change. Before releasing this Operator change, exercise the migration with an existing credential against a real Ledger, verify continued ingestion and denial of unrelated privileges, and record the exact Core, Operator and Ledger revisions. Kubernetes readiness alone does not prove authorization.

If Ledger reports authorization errors, check the Credentials' current observed generation, `spec.superuser=false`, the five scopes, and the deployed Core token behavior. A version or scope mismatch requires compatible Core/Operator versions. Rolling back the Operator restores the previous superuser grants on reconciliation; it widens privileges and must follow the deployment approval process.

The rollout proof intentionally matches the Connectivity operator's supported rendering contract: `spec.api.auth` contains exactly `issuer` and `checkScopes`; the `api` container receives literal `AUTH_ENABLED`, `AUTH_ISSUER`, and `AUTH_CHECK_SCOPES` values; and a ClusterIP Service selects and routes to that container. These rendering checks are separate from the lifecycle compatibility requirements below. A future operator version that changes this schema or rendering remains fail closed: the public route stays revoked with a `ConnectivityAPIPending` condition until this integration is updated.

Connectivity currently accepts only the stack's primary auth issuer. Additional issuers configured through the `auth.issuers` Setting are not propagated to `connectivity-api`, so tokens issued exclusively by one of those additional issuers are rejected.

Scope enforcement (`connectivity:read` / `connectivity:write`) follows the platform convention and is enabled by default when authentication is enabled. To disable it explicitly, use the standard check-scopes Setting:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: connectivity-check-scopes
spec:
  key: auth.connectivity.check-scopes
  value: "false"
  stacks:
    - "*"
```

## Disable and delete

| Platform operation | Expected Connectivity lifecycle |
| --- | --- |
| `fctl stack module disable connectivity` | Retain module, delegated Connectivity, instances and credentials; stop workloads. |
| `fctl stack module enable connectivity` | Clear the module disable flag; resume only if the Stack is enabled. |
| `fctl stack disable` | Retain Connectivity configuration and credentials; stop workloads. |
| `fctl stack delete` without force | Soft delete: disable the Stack and retain Connectivity resources for restore. |
| `fctl stack delete --force` | Delete the Stack and its resources; complete delegated cleanup and namespace deletion. |

A disabled Ledger dependency also requests Connectivity shutdown while retaining its
configuration and credentials. This is distinct from deleting the Ledger CR.

The table describes platform operations with compatible Membership, agent and
operators. Kubernetes deletion of a Stack CR is the destructive deletion path,
not the platform's soft delete. Restore the soft-deleted Stack before testing a
subsequent force delete if Membership rejects deleting an already-deleted Stack.

`Connectivity.spec.disabled=true` on the Stack module, or `Stack.spec.disabled=true`,
stops Connectivity through the delegated resource's
`spec.disabled=true`. The Stack Operator retains the delegated Connectivity CR,
all ConnectorInstance configuration and managed Secrets, and Ledger Credentials.
It removes the owned gateway route and returns once the disable request is
accepted. The delegated controller owns stopping Core, API and instance workloads;
its status records actual completion. Repeated enable/disable changes update the
desired state without serializing the requests behind workload shutdown.
Connectivity handles this transition through its Stack-dependent reconciler, then
uses the shared module reconciler for the enabled path. The shared core and other
Stack modules retain their existing behavior.

Re-enabling Connectivity requires both its module and its Stack to have
`spec.disabled=false`. The enabled reconciliation path restores delegated
`spec.disabled=false` after Ledger prerequisites are satisfied. The gateway stays closed until the delegated
controller reports `Ready=True` with a current `observedGeneration` in
`status.conditions` after Core rollout, and the API
Deployment and Service satisfy the authentication rollout checks.

Deleting the `formance.com/Connectivity` module requests deletion of its exact-owned
delegated Connectivity and deletes its Ledger Credentials without waiting for the
delegated resource to disappear. The existing `delete-ledger-credentials` finalizer
covers these API operations only. Cleanup errors are retried, but accepted deletion
does not wait for the delegated finalizer. A missing delegated API or object is
already absent for deletion; other lookup and ownership errors retain credentials
and retry cleanup.

The Connectivity controller owns instance cleanup, workload teardown and its own
finalizer. The Stack Operator does not serialize other module deletions behind
Connectivity. Cleanup must therefore tolerate Ledger, credentials and other Stack
resources becoming unavailable. Only the Connectivity controller reports completion
of its lifecycle; a successful Stack Operator reconciliation confirms the request.

Full Stack deletion also deletes the Stack-owned namespace. Kubernetes namespace
deletion removes all Secrets in that namespace, including externally supplied
Secrets. Connectivity's external-Secret retention guarantee does not extend to
namespace deletion. Store anything that must outlive the Stack outside its
namespace and manage its lifecycle separately.

### Lifecycle compatibility and recovery

Individual module disable requires a compatible agent. Membership synchronizes
active modules: absence of Connectivity means disabled, rather than a separate
module deletion. The agent retains an existing Connectivity module and sets
`spec.disabled=true` when absent, then sets `false` when enabled again. It does
not create Connectivity merely to represent a never-enabled module as disabled.
Older agents delete the absent module CR and invoke deletion instead. Install
the Stack Connectivity CRD and controller supporting this field before the
compatible agent. Stack deletion retains its existing module deletion lifecycle;
all modules whose served CRD declares boolean `spec.disabled` use the same
retain-and-toggle agent process. Their Stack Operator must implement that
field before the compatible agent is installed. The shared module reconciler
uses the existing Stack-disable workload cleanup for these module flags;
Connectivity delegates shutdown while preserving its configuration and credentials.

Roll out compatible Core, Connectivity CRDs and the Connectivity controller before
this Stack Operator. The served Connectivity schema must declare boolean
`spec.disabled`; the controller must implement the disabled, observed-generation,
Core-rollout and instance-cleanup contracts above, and hold
`connectivity.formance.com/connectivity` during cleanup. There is no verified
release-version floor for this combined contract. Verify the installed controller behavior; schema shape and finalizer presence
alone do not prove that contract.
Record the actual paired revisions and exercise disable/re-enable and both module
and Stack deletion before release.

Compatible CRDs and controller are required dependencies. The Stack Operator does
not negotiate lifecycle capability or wait for the delegated lifecycle finalizer to
be installed. API and ownership errors are reported and retried; foreign-owned
resources are preserved. Disable never falls back to deleting the delegated CR.
A delegated resource already deleting cannot be restored by a disable request.
Startup capability discovery requires restarting the Stack Operator after installing
previously missing APIs or permissions.

This changes observable disable behavior: disable preserves the delegated CR instead
of deleting it. Existing automation that relies on delegated-CR absence on disable
must change. Disclose this behavioral compatibility change and obtain required human
approval before merge. Rolling back to an older Stack Operator restores delete-on-disable;
complete cleanup/re-enable with the compatible pair before an approved rollback.
