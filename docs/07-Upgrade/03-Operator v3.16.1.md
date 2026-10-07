# Operator v3.16.1 candidate

This targeted OVH sandbox candidate starts from Operator v3.16.0 (`5c92b82d3d1c33f6e28f674ab476c589ecc5be33`) and backports only two changes already merged on main:

| Change | Upstream commit | Candidate commit |
| --- | --- | --- |
| #548: align Credentials with the Ledger superuser API and beta.9 API dependency | `0cdf98803609dd1510ce02bd79e44e23d209378d` | `265f63e5` |
| #544 / EN-2490: reconcile scoped Connectivity credentials and preserve Secret binding | `be3c687a68ba13c8096ca1b5cc943f5c7a0696c8` | `e3501912` |

The operator and operator-crds charts both become `3.16.1`, with application version `v3.16.1`. The image references intended for a separately authorized publication are `ghcr.io/formancehq/operator:v3.16.1` and `ghcr.io/formancehq/operator-utils:v3.16.1`, including the existing scratch variants and amd64/arm64 manifests. Neither these images nor the charts are published by this preparation PR. Record their immutable digests and build/source provenance before selecting the Regions pin.

This candidate excludes main's #547 (Job node selectors), #545 (Ledger cluster-ID preservation), and #542 (deployment/operator and operator-utils security dependency updates). It therefore does not deliver those changes. Assess the excluded security update with its owner before authorizing publication; this is not an assertion that the previous dependency set is safe.

## Compatibility and migration gates

This is a behavior and integration-contract change despite the requested patch version. An existing superuser credential is narrowed to five scopes; a Core emitting superuser tokens becomes incompatible. The Ledger API dependency and generated LedgerConfiguration schemas also change. Human approval must explicitly cover these impacts before integration or publication; the selected version number does not certify compatibility.

1. Install a compatible Ledger operator and Credentials CRD supporting `spec.superuser`, before deploying this Stack Operator. The source API dependency is Ledger beta.9 commit `4fe8ed8c07726da05eb678591e0e4960a821d919`. Regions main currently locks ledger-operator `3.0.0-beta.1`, whose Credentials API uses `spec.god`; that historical pin is not compatible evidence. The Helm lane owns the final compatible chart pin and corresponding runtime image.
2. Install compatible Connectivity CRDs supporting `spec.auth.keyIdSecretKeyRef` and `spec.auth.secretKeyRef`. Bind `key-id` and `seed.hex` from the same Ledger-distributed Secret, with subject `connectivity`. No bundle or derived signing Secret is introduced.
3. Deploy and verify a compatible Core emitting `superuser=false` and the fixed five required scopes before narrowing an existing registered key. Select and record the exact Core/Connectivity operator revisions, images and protocol-compatible Ledger tuple; beta.10 adoption is a separate owned change, not implied by this patch.
4. Exercise existing-credential migration against an actual Ledger. Preserve key ID, Secret identity and namespace isolation. Verify the applied five-scope grant, continued source ingestion, durable cursor progress and denial of unrelated privileges. Credentials Ready plus observedGeneration proves distribution/spec observation; it does not acknowledge applied Ledger grants.
5. Validate the stack Auth issuer and authenticated API access, including allowed and denied tokens. During Auth transitions, route exposure must wait for the delegated Deployment/Service rollout proof. Without a Stack Auth module the existing contract is unauthenticated; explicitly select the intended sandbox configuration.

Rolling back to v3.16.0 restores the previous superuser grants on reconciliation and the old Ledger API behavior. Treat this as privilege widening with a version-skew risk, requiring explicit operational approval and an exercised recovery plan. Never infer safe rollback from Helm readiness.

## Preparation branch and delivery evidence

The preparation PR targets `chore/v3.16.1-baseline`, anchored at the existing v3.16.0 tag. It is not a PR to main: merging the selected backport into main would not remove #547 and cannot produce this frozen patch. The baseline is a review anchor, not an authorized release. The eventual source/ref to tag requires a separate integration decision. No `build-images` or `deploy-staging` label is part of preparation.

The target is OVH sandbox. Its actual Stack, GitOps source and effective context remain with the infrastructure lane to verify; AWS hosting/production is excluded.

Track the retained credential contract through EN-2490 and the sandbox qualification through EN-2227 / EN-2231. This PR prepares a candidate only. Required evidence remains: pre-commit and relevant tests on the final tree, exact-head CI, independent Principal Engineer/Product Engineer/SRE review, code-owner approval, explicitly linked approval of the compatibility impacts, release authorization, published artifact digests, aligned Regions locks, and runtime qualification. No merge, tag, release, publication, deployment or environment synchronization is authorized by this document.
