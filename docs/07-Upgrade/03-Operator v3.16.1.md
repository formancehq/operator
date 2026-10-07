# Operator v3.16.1 candidate

This targeted OVH sandbox candidate starts from Operator v3.16.0 (`5c92b82d3d1c33f6e28f674ab476c589ecc5be33`) and backports only two changes already merged on main:

| Change | Upstream commit | Candidate commit |
| --- | --- | --- |
| #548: align Credentials with the Ledger superuser API and beta.9 API dependency | `0cdf98803609dd1510ce02bd79e44e23d209378d` | `265f63e5` |
| #544 / EN-2490: reconcile scoped Connectivity credentials and preserve Secret binding | `be3c687a68ba13c8096ca1b5cc943f5c7a0696c8` | `e3501912` |

The operator and operator-crds charts both become `3.16.1`, with application version `v3.16.1`. The image references intended for a separately authorized publication are `ghcr.io/formancehq/operator:v3.16.1` and `ghcr.io/formancehq/operator-utils:v3.16.1`, including the existing scratch variants and amd64/arm64 manifests. Neither these images nor the charts are published by this preparation PR. Record their immutable digests and build/source provenance before selecting the Regions pin.

This candidate excludes main's #547 (Job node selectors), #545 (Ledger cluster-ID preservation), and #542 (deployment/operator and operator-utils security dependency updates). It therefore does not deliver those changes. Assess the excluded security update with its owner before authorizing publication; this is not an assertion that the previous dependency set is safe.

## Compatibility and migration gates

This is a behavior and integration-contract change despite the requested patch version. An existing superuser credential is narrowed to five scopes; a Core emitting superuser tokens becomes incompatible. The Ledger API dependency and generated LedgerConfiguration schemas also change: `coldStorage`, `dnsEndpoint`, `receiptSigning`, and `monitoring.traces.sampling` are removed; `dnsEndpoints`, `clusterPolicyRevision`, and five `metadataMax*` fields are added; `clusterID` loses its `default` value. Kubernetes can prune removed fields when objects pass through the updated schema; replacing the CRD alone is not proof that all stored objects have already changed. The Ledger reconciler replaces the full delegated Cluster spec from its typed configuration, so reconciliation can also remove fields from existing Clusters. Inventory the actual LedgerConfiguration and Cluster objects and preserve their intended configuration through an approved migration before applying the new CRD or Operator. Human approval must explicitly cover these impacts before integration or publication; the selected version number does not certify compatibility.

1. Install a compatible Ledger operator and Credentials CRD supporting `spec.superuser`, before deploying this Stack Operator. The source API dependency is Ledger beta.9 commit `4fe8ed8c07726da05eb678591e0e4960a821d919`. Regions main currently locks ledger-operator `3.0.0-beta.1`, whose Credentials API uses `spec.god`; that historical pin is not compatible evidence. The Helm lane owns the final compatible chart pin and corresponding runtime image.
2. Install compatible Connectivity CRDs supporting `spec.auth.keyIdSecretKeyRef` and `spec.auth.secretKeyRef`. Bind `key-id` and `seed.hex` from the same Ledger-distributed Secret, with subject `connectivity`. No bundle or derived signing Secret is introduced.
3. Deploy and verify a compatible Core emitting `superuser=false` and the fixed five required scopes before narrowing an existing registered key. Select and record the exact Core/Connectivity operator revisions, images and protocol-compatible Ledger tuple; beta.10 adoption is a separate owned change, not implied by this patch.
4. Exercise existing-credential migration against an actual Ledger. Preserve key ID, Secret identity and namespace isolation. Verify the applied five-scope grant, continued source ingestion, durable cursor progress and denial of unrelated privileges. Credentials Ready plus observedGeneration proves distribution/spec observation; it does not acknowledge applied Ledger grants.
5. Validate the stack Auth issuer and authenticated API access, including allowed and denied tokens. During Auth transitions, route exposure must wait for the delegated Deployment/Service rollout proof. Without a Stack Auth module the existing contract is unauthenticated; explicitly select the intended sandbox configuration.

If Connectivity remains in `LedgerCredentialsPending`, inspect the served Credentials CRD and Ledger operator revision first. A legacy CRD can prune `superuser`; the Operator may then repeatedly report an update and remain pending while legacy god grants remain active. This failure signature is inferred from source and must be exercised or rejected by runtime evidence; do not treat an old Ready status as success.

Rolling back to v3.16.0 restores the previous superuser grants on reconciliation and the old Ledger API behavior. Treat this as privilege widening with a version-skew risk, requiring explicit operational approval and an exercised recovery plan that states the Ledger operator version and the LedgerConfiguration/Cluster schema used before and after recovery. Never infer safe rollback from Helm readiness.

## Exact removed schema paths and OVH evidence limits

Comparing the v1beta1 structural schema in `config/crd/bases/formance.com_ledgerconfigurations.yaml` at v3.16.0 and #548 (`0cdf98803609dd1510ce02bd79e44e23d209378d`) yields the following 35 removed property paths. `[]` marks array items; parent and child paths are listed separately. This inventory describes source schema changes, not effective live configuration.

```text
spec.cluster.coldStorage
spec.cluster.coldStorage.bucketId
spec.cluster.coldStorage.driver
spec.cluster.coldStorage.path
spec.cluster.coldStorage.s3
spec.cluster.coldStorage.s3.bucket
spec.cluster.coldStorage.s3.endpoint
spec.cluster.coldStorage.s3.region
spec.cluster.dnsEndpoint
spec.cluster.dnsEndpoint.annotations
spec.cluster.dnsEndpoint.enabled
spec.cluster.dnsEndpoint.endpoints
spec.cluster.dnsEndpoint.endpoints[].dnsName
spec.cluster.dnsEndpoint.endpoints[].providerSpecific
spec.cluster.dnsEndpoint.endpoints[].providerSpecific[].name
spec.cluster.dnsEndpoint.endpoints[].providerSpecific[].value
spec.cluster.dnsEndpoint.endpoints[].recordTTL
spec.cluster.dnsEndpoint.endpoints[].recordType
spec.cluster.dnsEndpoint.endpoints[].targets
spec.cluster.monitoring.pyroscope.authToken
spec.cluster.monitoring.pyroscope.basicAuthPassword
spec.cluster.monitoring.traces.sampling
spec.cluster.monitoring.traces.sampling.enabled
spec.cluster.monitoring.traces.sampling.successRatio
spec.cluster.persistence.coldCache
spec.cluster.persistence.coldCache.accessMode
spec.cluster.persistence.coldCache.hostPath
spec.cluster.persistence.coldCache.hostPath.path
spec.cluster.persistence.coldCache.hostPath.type
spec.cluster.persistence.coldCache.size
spec.cluster.persistence.coldCache.storageClass
spec.cluster.persistence.coldCache.volumeAttributesClassName
spec.cluster.receiptSigning
spec.cluster.receiptSigning.secretKey
spec.cluster.receiptSigning.secretName
```

`spec.cluster.monitoring.traces` remains present; only its `sampling` subtree is removed. `spec.cluster.persistence.data.accessMode` retains the same type and `ReadWriteOnce` default. Their apparent removal in a textual diff must not be treated as a removed schema path. The `spec.cluster.clusterID` property remains present but loses its `default: default` value; path comparison alone does not detect that changed default.

The Infra owner's read-only OVH checkpoint reports zero explicit LedgerConfiguration objects. This does not prove that Settings, private Helm values, chart defaults, future manifests, or existing Ledger Cluster specs do not use removed configuration. Regions consumes a Secret through valuesFrom; no secret content was read or decrypted. Owners must compare authorized redacted effective values and Cluster specs with this schema before approving migration.

The OVH Flux CI includes a shared `fluxcd.yml` template; its effective validation contract has not been inspected in this lane. No passing render/schema gate is inferred from that include. The Flux writer/grant, exact Stack, compatible published tuple, effective-values render, independent reviews and exercised runtime/recovery remain gates. No Flux edit, branch, push, manual deployment, AWS diff or cluster mutation is performed by this inventory.

## Preparation branch and delivery evidence

The preparation PR targets `chore/v3.16.1-baseline`, anchored at the existing v3.16.0 tag. It is not a PR to main: merging the selected backport into main would not remove #547 and cannot produce this frozen patch. The baseline is a review anchor, not an authorized release. The eventual source/ref to tag requires a separate integration decision. No `build-images` or `deploy-staging` label is part of preparation.

The target is OVH sandbox. Changes are prepared for the owning GitOps path only; no manual deployment to these clusters is authorized, and GitOps merge/reconciliation/sync require separate authorization. Its actual Stack, GitOps source and effective context remain with the infrastructure lane to verify; AWS hosting/production is excluded.

Track the retained credential contract through EN-2490 and the sandbox qualification through EN-2227 / EN-2231. This PR prepares a candidate only. Required evidence remains: pre-commit and relevant tests on the final tree, exact-head CI, independent Principal Engineer/Product Engineer/SRE review, code-owner approval, explicitly linked approval of the compatibility impacts, release authorization, published artifact digests, aligned Regions locks, and runtime qualification. No merge, tag, release, publication, deployment or environment synchronization is authorized by this document.
