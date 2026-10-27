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

`Stack.spec.disabled=true` stops Connectivity through the delegated resource's
`spec.disabled=true`. The Stack Operator retains the delegated Connectivity CR,
all ConnectorInstance configuration and managed Secrets, and Ledger Credentials.
It removes the owned gateway route and waits for the delegated controller to
report `phase=Disabled`, an empty address, and `status.observedGeneration` equal
to `metadata.generation`. The delegated controller owns stopping Core, API and
all instance workloads, and must publish `Disabled` only after verifying that their
workloads have stopped. A patch being accepted does not mean disable completed.
Other Stack modules retain their existing disable behavior.

Re-enabling the Stack explicitly restores delegated `spec.disabled=false` after
Ledger prerequisites are satisfied. The gateway stays closed until the delegated
controller reports the current generation `Ready` after Core rollout, and the API
Deployment and Service satisfy the authentication rollout checks.

Deleting the `formance.com/Connectivity` module requests deletion of its exact-owned
delegated Connectivity and keeps the module finalizer and Ledger Credentials until
that resource disappears. The delegated controller must delete every instance
through `RemoveSource` before removing Core, and garbage-collect managed Secrets;
externally supplied Secrets remain untouched by Connectivity cleanup. Deletion of
an individual instance while its parent is disabled waits; deleting the parent
takes precedence and restores Core for cleanup. The Stack Operator reuses its
existing `delete-ledger-credentials` finalizer name for this ordered cleanup.

Stack deletion drains Connectivity modules first, before requesting deletion of
Ledger or other modules. A disabled Stack is not automatically re-enabled: the
Connectivity controller restores Core for deletion using the retained configuration
and credentials. Core constructs its Ledger client lazily and can remove an absent
source without a Ledger RPC; this does not reconstruct missing credential, TLS or
other startup dependencies. If cleanup cannot run, deletion remains pending and
credentials are retained. Repair the named dependency or compatible controller;
do not remove finalizers to force success. Deleting a Ledger module directly or
changing it away from v3 can make ingestion unavailable before cleanup; hard-gate
teardown still waits for delegated deletion before releasing credentials.

Full Stack deletion also deletes the Stack-owned namespace. Kubernetes namespace
deletion removes all Secrets in that namespace, including externally supplied
Secrets. Connectivity's external-Secret retention guarantee does not extend to
namespace deletion. Store anything that must outlive the Stack outside its
namespace and manage its lifecycle separately.

### Lifecycle compatibility and recovery

Roll out compatible Core, Connectivity CRDs and the Connectivity controller before
this Stack Operator. The served Connectivity schema must declare boolean
`spec.disabled`; the controller must implement the disabled, observed-generation,
Core-rollout and instance-cleanup contracts above, and hold
`connectivity.formance.com/connectivity` during cleanup. There is no verified
release-version floor for this combined contract. CRD shape and finalizer presence
are necessary guards, not proof that the installed controller implements it.
Record the actual paired revisions and exercise disable/re-enable and both module
and Stack deletion before release.

When the schema, lifecycle finalizer, ownership or API access cannot be verified,
the Operator retains configuration and credentials, closes its owned public route
where possible, and reports the blocking condition. It never falls back to deleting
the delegated CR to disable it. If a delegated CR is already deleting, disable
cannot cancel that deletion. Fix discovery/RBAC/schema/controller compatibility and
allow reconciliation to resume. Startup capability discovery requires restarting
the Stack Operator after installing previously missing APIs or permissions.

This changes observable lifecycle behavior: disable now preserves the delegated CR
instead of deleting it; module, hard-gate and Stack deletion wait for cleanup rather
than promptly revoking credentials or deleting every module concurrently. Existing
automation that relies on delegated-CR absence on disable, immediate credential
revocation, or deletion succeeding without a compatible Connectivity controller
must change. These are behavioral compatibility changes and capability restrictions;
there is no Stack CRD field removal. Disclose them and obtain the required human
approval before merge. Rolling back to an older Stack Operator restores its old
delete-on-disable and credential-ordering behavior and is unsafe during a disabled
or terminating Connectivity lifecycle. Complete cleanup/re-enable with the compatible
pair before an approved rollback; do not rely on rollback to bypass a pending state.
