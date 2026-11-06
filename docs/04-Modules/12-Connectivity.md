## Requirements

Formance Connectivity requires:
- **Ledger v3**: Connectivity ingests double-entry transactions into the stack ledger through its gRPC endpoint. The effective Ledger version must be a semantic major v3 version. Ledger v2 migration previews and opaque development references are not supported because they do not select the primary v3 topology.
- **Ledger operator**: its Credentials CRD and controller must support `spec.superuser`. Install the compatible Ledger operator and CRD before deploying this Operator.
- **Connectivity operator**: the module delegates the actual workload to a `connectivity.formance.com/Connectivity` resource; the Connectivity v1 CRDs (`v1.0.0-alpha.1` or later) must be installed on the cluster.

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

- `ledger:ReadAccount` — cursor `GetAccount`
- `ledger:WriteLedger` — `CreateLedger`, `CreateIndex`, `SaveNumscript`
- `ledger:WriteTransaction` — plugin transaction actions in `Apply`
- `ledger:WriteMetadata` — plugin metadata actions and cursor save in `Apply`
- `ledger:WriteQuery` — `CreatePreparedQuery`

The scope list is owned by the integration contract (see the [Ledger capability matrix](https://github.com/formancehq/connectivity/blob/main/docs/architecture/core/ledger-capabilities.md)); it is not configurable through the Connectivity CRD and follows the scopes enforced by the supported Ledger version. The delegated binding uses the supported `spec.auth.keyIdSecretKeyRef` and `spec.auth.secretKeyRef`, referencing `key-id` and `seed.hex` in the same Ledger-distributed Secret, with `spec.auth.subject=connectivity`. Reconciliation replaces the auth map to remove old inline or bundle fields. The Connectivity operator watches both Secret keys and restarts Core when either changes. The Operator creates no credential bundle or additional signing Secret.

An existing Credentials with RC.1 scope names (`ledger:AccountRead`, `ledger:LedgerWrite`, `ledger:TransactionWrite`, `ledger:MetadataWrite`, `ledger:QueryWrite`) or superuser grants converges to the canonical spec on the next reconciliation, retaining its key ID and distributed Secret. After any credential update, the Operator waits for `status.phase=Ready` and `status.observedGeneration` to match `metadata.generation` before proceeding with delegation. This status confirms credential distribution; Ledger Cluster reconciliation applies the grants separately. Deploy a compatible Connectivity Core that emits scoped tokens with these scopes before narrowing the registered key; older Core versions that emit `superuser=true` will fail authentication after the change. Before releasing this Operator change, exercise the migration with an existing credential against a real Ledger, verify continued ingestion and denial of unrelated privileges, and record the exact Core, Operator and Ledger revisions. Kubernetes readiness alone does not prove authorization.

### Migrating RC.1 scope names

Coordinate this Operator with a Connectivity Core that claims the canonical scopes above and Ledger RC.2 or later. A Core that still claims the RC.1 names cannot authenticate with a key narrowed to the new scopes; the canonical Core cannot use RC.1's granular authorization contract. Existing manually supplied credentials and bundles must be migrated by their owner as well.

Reconciliation updates `Credentials.spec.scopes` in place and waits for its new observed generation. Before resuming ingestion, verify that the Ledger Cluster controller has applied the five canonical grants to the existing registered key, then exercise a real authenticated provisioning and ingestion call. Credentials readiness alone confirms distribution and does not prove that the registered Ledger grants have converged. Preserve the existing key and Secret; a scope rename does not require key rotation.

If Ledger reports authorization errors, check the Credentials' current observed generation, `spec.superuser=false`, the five scopes, the registered key grants, and the deployed Core token behavior. A version or scope mismatch requires compatible Core/Operator versions. A rollback must restore matching Core token claims, Operator-desired grants and Ledger authorization semantics together; rolling back only the scope list can interrupt ingestion. Any rollback to a superuser-producing Operator widens privileges and requires deployment approval.

The rollout proof intentionally matches the Connectivity operator's supported rendering contract: `spec.api.auth` contains exactly `issuer` and `checkScopes`; the `api` container receives literal `AUTH_ENABLED`, `AUTH_ISSUER`, and `AUTH_CHECK_SCOPES` values; and a ClusterIP Service selects and routes to that container. These assumptions match the minimum supported Connectivity operator (`v1.0.0-alpha.1`). A future operator version that changes this schema or rendering remains fail closed: the public route stays revoked with a `ConnectivityAPIPending` condition until this integration is updated.

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
