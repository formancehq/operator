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

Additional hosts can also be provided via a [Settings](../09-Configuration%20reference/01-Settings.md) resource using the `gateway.ingress.hosts` key. Hosts from the setting are merged with those defined on the Gateway CRD.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: gateway-extra-hosts
spec:
  key: gateway.ingress.hosts
  stacks:
    - '*'
  value: "extra.example.com, extra.example.org"
```