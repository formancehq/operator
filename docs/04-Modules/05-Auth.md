:::warning
This Module is subject to a user license.
:::

## Requirements

Formance Auth requires:
- **PostgreSQL**: See configuration guide [here](../05-Infrastructure%20services/02-Message%20broker.md).

## Auth Object
:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#auth).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Auth
metadata:
  name: formance-dev
spec:
  stack: formance-dev
```

When no signing key is configured, the operator generates a unique RSA private key for the stack in the `auth-signing-key` Secret and injects it into Auth. The Secret is retained across reconciliations. Use `signingKeyFromSecret` when key ownership or rotation is managed externally.

When you upgrade an existing Auth instance that used the service's built-in key, the new key invalidates tokens signed before the upgrade. Clients must request new tokens after the Auth Deployment restarts.

### Crypto key (CRYPTO_KEY)

The auth service needs a symmetric crypto key (`CRYPTO_KEY`) to decrypt the opaque artifacts it persists. The operator provisions it for each stack:

- When no crypto key is configured, the operator generates once a random 32-byte key with `crypto/rand` and stores its standard padded base64 encoding (`base64.StdEncoding` of exactly 32 bytes) in the `auth-crypto-key` Secret, under the `crypto-key` entry. The Auth object owns that Secret and it is retained across reconciliations, restarts and upgrades: the key is generated once and never implicitly rotated.
- A pre-existing Secret value is validated, never silently replaced. A malformed value (invalid base64 padding or a decoded length other than 32 bytes) fails the reconciliation explicitly, so a bad key surfaces at deploy time instead of corrupting artifacts.
- Each stack gets its own key; keys of different stacks are independent and distinct from the RSA signing key.

Use `cryptoKeyFromSecret` (a standard `SecretKeySelector`) to manage the key externally:

```yaml
apiVersion: formance.com/v1beta1
kind: Auth
metadata:
  name: formance-dev
spec:
  stack: formance-dev
  cryptoKeyFromSecret:
    name: my-crypto-key
    key: crypto-key
```

With an external reference, the operator only reads the Secret: it never creates, owns or rewrites it. The referenced entry must hold the same standard padded base64 of 32 bytes. The operator validates the value, tracks the Secret's resourceVersion in the Auth Deployment pod template annotations so that any change of the referenced Secret rolls the Deployment (this is the explicit rotation path), and enqueues the Auth reconciliation when a Secret selected by `cryptoKeyFromSecret` is created, updated or deleted. No `ResourceReference` object is involved in this path, and no stack label is required on the referenced Secret: it is a plain local `SecretKeySelector` read in the stack namespace.

Rotation effect: opaque artifacts sealed with the old key become unreadable after rotation, so affected users must re-authenticate. Ordinary JWT signature verification is unaffected: the signing key is separate (`SIGNING_KEY` / `auth-signing-key`), and no signing-key rotation or database deletion happens. Replacing the `auth-crypto-key` Secret content only affects the decryption of opaque artifacts, not the JWT signature path.

Crypto key lifecycle: the operator watches the `auth-crypto-key` Secret of each stack in addition to owning its generated-mode Secret. Updating the Secret entry — including a pre-seeded Secret that the operator does not own — triggers the Auth reconciliation automatically, which validates the new value and rolls the Deployment; deleting and recreating the Secret under the default name regenerates a key only when the Secret is genuinely absent. The operator never adopts pre-seeded or externally managed Secrets (no ownerReference) and never rewrites their content. Rollback compatibility: an operator build without this watch leaves an unowned pre-seeded Secret untracked (its rotation then requires a manual Auth reconcile), but the key value itself is never altered in either direction.

Older auth service versions that do not read `CRYPTO_KEY` ignore the environment variable, so provisioning the Secret is safe during a coordinated upgrade of the operator first and the auth service second.

### Define oAuth2 clients
You can define oAuth2 clients to secure your stack. The Auth will then be able to authenticate the incoming requests and forward them to the right service.

Here, you can replace `YOUR_ID` and `YOUR_SECRET` with your own arbitrary string values.

```yaml
apiVersion: formance.com/v1beta1
kind: AuthClient
metadata:
  name: formance-dev-clients
spec:
  id: YOUR_ID
  secret: YOUR_SECRET
  scopes:
    - ledger:read
    - ledger:write
    - payments:read
    - payments:write
  stack: formance-dev
```
