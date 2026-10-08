# Operator v3.16.1 candidate

This release-preparation PR is based on `main` at `12eac9b10344d59cae66d626d9cbd97f98736c26`. The scoped Connectivity credential changes (#548 and #544) are already integrated there; this PR adds the version and migration notes rather than backporting their implementation.

The operator and operator-crds charts both become `3.16.1`, with application version `v3.16.1`. The image references intended for a separately authorized publication are `ghcr.io/formancehq/operator:v3.16.1` and `ghcr.io/formancehq/operator-utils:v3.16.1`, including the existing scratch variants and amd64/arm64 manifests. Neither these images nor the charts are published by this preparation PR. Record their immutable digests and build/source provenance before selecting the Regions pin.

The rebased candidate includes the existing main changes: #547 (Job node selectors), #545 (Ledger cluster-ID preservation), #542 (security dependency updates), and #550 (shared GoReleaser Docker v2 preset), as well as #548/#544. It is no longer the earlier isolated v3.16.0 + #548/#544 candidate. Assess the complete main-based release delta before publication.

## Compatibility and migration gates

This is a behavior and integration-contract change despite the requested patch version. An existing superuser credential is narrowed to five scopes; a Core emitting superuser tokens becomes incompatible. The Ledger API dependency and generated LedgerConfiguration schemas also change: `coldStorage`, `dnsEndpoint`, `receiptSigning`, and `monitoring.traces.sampling` are removed; `dnsEndpoints`, `clusterPolicyRevision`, and five `metadataMax*` fields are added; `clusterID` loses its schema `default` value; #545 preserves the delegated Ledger operator's generated cluster ID when configuration leaves it unset. Kubernetes can prune removed fields when objects pass through the updated schema; replacing the CRD alone is not proof that all stored objects have already changed. The Ledger reconciler replaces the full delegated Cluster spec from its typed configuration, so reconciliation can also remove fields from existing Clusters. Inventory the actual LedgerConfiguration and Cluster objects and preserve their intended configuration through an approved migration before applying the new CRD or Operator. Human approval must explicitly cover these impacts before integration or publication; the selected version number does not certify compatibility.

1. Install a compatible Ledger operator and Credentials CRD supporting `spec.superuser`, before deploying this Stack Operator. Ledger beta.10 is the selected runtime target. The Stack Operator Go API dependency still points to beta.9 commit `4fe8ed8c07726da05eb678591e0e4960a821d919`; that compile-time pin does not establish the deployed Ledger version or qualify the beta.10 controller/CRD tuple. The last verified OVH Ledger operator/CRD checkpoint was beta.1 with `spec.god`; refresh the target's actual image and served schema before rollout. The Helm lane owns the final compatible chart pin and corresponding runtime image.
2. Install compatible Connectivity CRDs supporting `spec.auth.keyIdSecretKeyRef` and `spec.auth.secretKeyRef`. Bind `key-id` and `seed.hex` from the same Ledger-distributed Secret, with subject `connectivity`. No bundle or derived signing Secret is introduced.
3. Deploy and verify a compatible Core emitting `superuser=false` and the fixed five required scopes before narrowing an existing registered key. Select and record the exact Core/Connectivity operator revisions, images and protocol-compatible Ledger tuple; Core beta.2 at `ce2324887f5b5ec4e3c2ec934ac874656d4c5348` requires Discovery protocol 20 and matching reflected descriptors before every Apply; its synced protobuf source matches Ledger beta.10. Qualify the selected beta.10 image, credentials and data compatibility on the exact Stack; this preparation does not authorize a Ledger upgrade.
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

## Pyroscope credential migration before schema adoption

The new schema replaces inline profiling credentials with Secret references. Each reference requires a non-empty Secret `name` and `key` and resolves in the delegated Ledger Cluster namespace:

| Removed field under `spec.cluster.monitoring.pyroscope` | Replacement |
| --- | --- |
| `authToken` | `authTokenFrom.name` and `authTokenFrom.key` |
| `basicAuthPassword` | `basicAuthPasswordFrom.name` and `basicAuthPasswordFrom.key` |

Keep `basicAuthUser` where basic authentication is used; that field remains supported. Retain the existing authentication method rather than enabling both modes as part of this migration.

1. The configuration owner inventories which profiling authentication mode is used in the approved effective configuration, including Settings, private Helm values and existing Cluster specs. Record only field/reference presence and namespace ownership; never print credential values or copy them into this repository or review comments.
2. Before adopting the new schema, the owning GitOps lane prepares the existing credential material through its approved Secret-management mechanism in each relevant Cluster namespace. Do not remove the old configuration until the compatible Ledger operator can resolve the new references. This preparation document authorizes no Secret creation, decryption or cluster mutation.
3. Prepare the Secret selectors and the matching compatible operator/CRD rollout together. Do not apply the selectors to an old schema that prunes them, or drop the inline fields before the references can be used. Review the effective configuration without secrets and establish the explicit ordering/recovery plan before any separately authorized GitOps integration or reconciliation.
4. During separately authorized qualification, verify that profiling delivery still succeeds and that a missing Secret/key produces an actionable failure. Check that reference-only configuration survives admission and reconciliation without silently disabling profiling. Readiness alone does not establish successful authentication to the profiling service.
5. Recover using the approved operator/schema/configuration tuple if reference resolution or delivery fails. Preserve the original credential material in the approved Secret-management system for that recovery window; do not reconstruct inline credentials from this document or assume a downgrade understands the new selectors.

This is a required migration procedure for existing profiling users, not evidence that OVH uses these fields or that any credential has been moved. The actual configuration inventory, Secret owner, rollout ordering and exercised recovery remain gates.

## Preparation branch and delivery evidence

The preparation PR targets `main`. A main-based release includes changes beyond the earlier isolated two-fix candidate; record the final source SHA and complete delta against v3.16.0 before tagging. The earlier baseline branch is historical preparation evidence, not the integration target. No `build-images` or `deploy-staging` label is part of preparation.

The target is OVH sandbox. Changes are prepared for the owning GitOps path only; no manual deployment to these clusters is authorized, and GitOps merge/reconciliation/sync require separate authorization. Its actual Stack, GitOps source and effective context remain with the infrastructure lane to verify; AWS hosting/production is excluded.

Track the retained credential contract through EN-2490 and the sandbox qualification through EN-2227 / EN-2231. This PR prepares a candidate only. Required evidence remains: pre-commit and relevant tests on the final tree, exact-head CI, independent Principal Engineer/Product Engineer/SRE review, code-owner approval, explicitly linked approval of the compatibility impacts, release authorization, published artifact digests, aligned Regions locks, and runtime qualification. No merge, tag, release, publication, deployment or environment synchronization is authorized by this document.
