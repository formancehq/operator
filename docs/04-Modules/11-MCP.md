## Requirements

Formance MCP requires:
- **Gateway**: used for the internal `STACK_URL` and MCP routing.
- **Auth**: bearer tokens are validated through the stack OIDC/JWT issuer.
- **Ledger**, **Payments**, and optionally **Reconciliation**: MCP tools relay the caller bearer token to these backend APIs through the Gateway.

## MCP Object

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#mcp).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: MCP
metadata:
  name: formance-dev-mcp
spec:
  stack: formance-dev
```

The Operator deploys `ghcr.io/formancehq/stack-mcp` as the `mcp` service and exposes:
- `POST /api/mcp/mcp`
- `GET /api/mcp/.well-known/oauth-protected-resource`
- `GET /api/mcp/_healthcheck`

`AUTH_CHECK_SCOPES` is intentionally set to `false`; the MCP server checks scopes at the tool call level because every MCP request enters through `POST /api/mcp/mcp`.
