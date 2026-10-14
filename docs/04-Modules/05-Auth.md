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
