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

- `ledger:AccountRead` — cursor `GetAccount`
- `ledger:LedgerWrite` — `CreateLedger`, `CreateIndex`, `SaveNumscript`
- `ledger:TransactionWrite` — plugin transaction actions and cursor save in `Apply`
- `ledger:MetadataWrite` — plugin metadata actions and cursor save in `Apply`
- `ledger:QueryWrite` — `CreatePreparedQuery`

The scope list is owned by the integration contract (see the [Ledger capability matrix](https://github.com/formancehq/connectivity/blob/main/docs/architecture/ledger-capabilities.md)); it is not configurable through the Connectivity CRD and is updated only when Connectivity's actual Ledger calls change. The delegated binding uses the supported `spec.auth.keyIdSecretKeyRef` and `spec.auth.secretKeyRef`, referencing `key-id` and `seed.hex` in the same Ledger-distributed Secret, with `spec.auth.subject=connectivity`. Reconciliation replaces the auth map to remove old inline or bundle fields. The Connectivity operator watches both Secret keys and restarts Core when either changes. The Operator creates no credential bundle or additional signing Secret.

An existing superuser Credentials converges to the narrowed spec on the next reconciliation, retaining its key ID and distributed Secret. After any credential update, the Operator waits for `status.phase=Ready` and `status.observedGeneration` to match `metadata.generation` before proceeding with delegation. This status confirms credential distribution; Ledger Cluster reconciliation applies the grants separately. Deploy a compatible Connectivity Core that emits scoped tokens with these scopes before narrowing the registered key; older Core versions that emit `superuser=true` will fail authentication after the change. Before releasing this Operator change, exercise the migration with an existing credential against a real Ledger, verify continued ingestion and denial of unrelated privileges, and record the exact Core, Operator and Ledger revisions. Kubernetes readiness alone does not prove authorization.

If Ledger reports authorization errors, check the Credentials' current observed generation, `spec.superuser=false`, the five scopes, and the deployed Core token behavior. A version or scope mismatch requires compatible Core/Operator versions. Rolling back the Operator restores the previous superuser grants on reconciliation; it widens privileges and must follow the deployment approval process.

The rollout proof intentionally matches the Connectivity operator's supported rendering contract: `spec.api.auth` contains exactly `issuer` and `checkScopes`; the `api` container receives literal `AUTH_ENABLED`, `AUTH_ISSUER`, and `AUTH_CHECK_SCOPES` values; and a ClusterIP Service selects and routes to that container. These assumptions match the minimum supported Connectivity operator (`v1.0.0-alpha.1`). A future operator version that changes this schema or rendering remains fail closed: the public route stays revoked with a `ConnectivityAPIPending` condition until this integration is updated.

Connectivity currently accepts only the stack's primary auth issuer. Additional issuers configured through the `auth.issuers` Setting are not propagated to `connectivity-api`, so tokens issued exclusively by one of those additional issuers are rejected.

Scope enforcement (`connectivity:read` / `connectivity:write`) follows the platform convention and is disabled by default. Enable it with the standard check-scopes Setting:

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: connectivity-check-scopes
spec:
  key: auth.connectivity.check-scopes
  value: "true"
  stacks:
    - "*"
```
