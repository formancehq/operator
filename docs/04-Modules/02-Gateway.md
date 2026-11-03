The Gateway's role is to centralize all incoming connections to your stack. If you need to expose your Stack to the outside, it is necessary to route all the traffic through it.
## Gateway Object

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#gateway).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Gateway
metadata:
  name: formance-dev
spec:
  stack: formance-dev
```


### Expose the Gateway with Ingress
To expose the Gateway to the outside, we will use an Ingress object. The Ingress will be the entry point to your stack.

```yaml
apiVersion: formance.com/v1beta1
kind: Gateway
metadata:
  name: formance-dev
spec:
  stack: formance-dev
  ingress:
    host: YOUR_DOMAIN
    scheme: http|https
```

### Stack URL Propagation

Modules derive the stack URL from the Gateway ingress:

- With an ingress, the URL is `<scheme>://<host>`, built from `spec.ingress.scheme` and `spec.ingress.host`. Additional `hosts` are served but are not used for the stack URL.
- Without an ingress, modules fall back to the internal Gateway URL, `http://gateway:8080`.

Auth uses this URL as its OIDC issuer (`BASE_URL`, `<url>/api/auth`). Modules protected by Auth (Ledger, Payments, Orchestration, Reconciliation, Wallets, Webhooks, TransactionPlane) receive the same issuer as `AUTH_ISSUER`. Modules that read the stack URL, including Auth and Stargate, receive `STACK_PUBLIC_URL` only while an ingress is set.

When you add, change, or remove `spec.ingress`, the operator re-renders these Deployments with the new URLs, and Kubernetes rolls them out. No manual restart is needed.

:::warning
While Auth and its dependents roll out, a dependent pod can start before the new Auth pod is serving. It then fails OIDC discovery with an error such as `issuer does not match: Expected: http://gateway:8080/api/auth, got: https://<old-host>/api/auth`, exits, and is restarted by Kubernetes until Auth advertises the new issuer. This is temporary, but recovery can take longer if Auth is slow to roll out, because restart backoff grows up to five minutes.
:::

### Serving on Multiple Hosts

You can serve a stack on multiple hostnames by adding the `hosts` field alongside `host`. The ingress will contain a rule for each host, and TLS will cover all of them.

```yaml
apiVersion: formance.com/v1beta1
kind: Gateway
metadata:
  name: formance-dev
spec:
  stack: formance-dev
  ingress:
    host: app.example.com
    hosts:
      - app.example.org
      - app.example.net
    scheme: https
```

Additional hosts can also be provided via a [Settings](../09-Configuration%20reference/01-Settings.md) resource using the `gateway.ingress.hosts` key. Hosts from the setting are merged with those defined on the Gateway CRD. The `{stack}` placeholder is replaced with the stack name.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: gateway-extra-hosts
spec:
  key: gateway.ingress.hosts
  stacks:
    - '*'
  value: "{stack}.example.com, {stack}.example.org"
```