## Requirements

Formance Connectivity requires:
- **Ledger v3**: Connectivity ingests double-entry transactions into the stack ledger through its gRPC endpoint. The module stays pending until a ready v3 Ledger is present on the stack.
- **Connectivity operator**: the module delegates the actual workload to a `connectivity.formance.com/Connectivity` resource; the connectivity operator and its CRDs must be installed on the cluster.

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

The Operator provisions the delegated resource bound to the stack ledger (gRPC address, TLS material, and an Ed25519 god-mode credential registered on the ledger), and exposes the companion `connectivity-api` service through the stack gateway under `/api/connectivity`.

## API authentication

When the stack has an **Auth** module, the `connectivity-api` is protected like the other stack modules: it validates OIDC bearer tokens against the stack auth issuer. Without an Auth module on the stack, the API runs unauthenticated.

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

## JSON logging

Connectivity honours the platform-wide [`logging.json`](../09-Configuration%20reference/01-Settings.md) Setting. When it is `true`, the Operator sets `spec.monitoring.logs.format` to `json` on the delegated resource, and the connectivity operator switches the core, `connectivity-api` and connector workloads to JSON-encoded logs. Otherwise they keep the human-readable format.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: json-logging
spec:
  key: logging.json
  value: "true"
  stacks:
    - "*"
```

The Setting is independent of telemetry export: it applies whether or not an OpenTelemetry collector is configured on the stack.
