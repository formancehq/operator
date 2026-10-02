## Requirements

Formance Connectivity requires:
- **Ledger v3**: Connectivity ingests double-entry transactions into the stack ledger through its gRPC endpoint. The effective Ledger version must be a semantic major v3 version. Ledger v2 migration previews and opaque development references are not supported because they do not select the primary v3 topology.
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

The Operator provisions a cluster-scoped `ledger.formance.com/Credentials` named `connectivity-<stack>`, selected onto the stack's ledger Cluster with `spec.selector.matchLabels[formance.com/stack]`, with god mode disabled and exactly the scope set Connectivity's ingestion path requires:

- `ledger:AccountRead` — cursor `GetAccount`
- `ledger:LedgerWrite` — `CreateLedger`, `CreateIndex`, `SaveNumscript`
- `ledger:TransactionWrite` — plugin transaction actions and cursor save in `Apply`
- `ledger:MetadataWrite` — plugin metadata actions and cursor save in `Apply`
- `ledger:QueryWrite` — `CreatePreparedQuery`

The scope list is owned by the integration contract (see the [Ledger capability matrix](https://github.com/formancehq/connectivity/blob/main/docs/architecture/ledger-capabilities.md)); it is not configurable through the Connectivity CRD and is updated only when Connectivity's actual Ledger calls change. An existing god-mode Credentials from a previous Operator version converges to the narrowed spec on the next reconciliation.

For Stack-managed Connectivity, the Operator takes the stack-namespace Secret name from the Ready Credentials' `status.distributedSecretRefs` and writes only `spec.auth.credentialsSecretName` in the delegated Connectivity CR. It does not read or copy the Ledger-distributed `seed.hex` and `key-id`. The Connectivity operator mounts those two keys from that Secret into Core's existing non-god key mode, watches their rotation, and restarts Core when either changes. Ledger still owns the Secret and registers the grants from the Credentials spec. The Stack Operator waits for the Credentials' observed generation and the delegated Core rollout before exposing the public gateway route. It removes a derived bundle Secret if an earlier draft created one and the Secret is owned by this Connectivity.

This changes the Stack-managed delegated CR from `keyId` + `subject` + `secretKeyRef` to one Secret name. The Operator replaces the entire auth map because the Connectivity CRD rejects mixed auth modes. Install a Connectivity Operator release supporting `credentialsSecretName` before deploying this Stack Operator change. Standalone Connectivity CRs can still use key mode or `bundleSecretKeyRef`. The TLS CA Secret and server name mapping are unchanged.

If the ledger reports authorization errors, check that the Credentials' `status.phase` is `Ready`, its observed generation matches, its `spec.god` is `false` with the five scopes above, and the delegated Core rollout is current. A ready Kubernetes rollout alone does not prove Ledger authorization; test a real Ledger RPC with the non-god credential before release.

The rollout proof intentionally matches the Connectivity operator's API rendering contract: `spec.api.auth` contains exactly `issuer` and `checkScopes`; the `api` container receives literal `AUTH_ENABLED`, `AUTH_ISSUER`, and `AUTH_CHECK_SCOPES` values; and a ClusterIP Service selects and routes to that container. The direct Ledger Secret mode additionally requires a newer Connectivity operator release. A future operator version that changes the API rendering remains fail closed: the public route stays revoked with a `ConnectivityAPIPending` condition until this integration is updated.

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
