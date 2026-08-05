:::warning
This Module is subject to a user license.
:::

## Requirements

Formance Webhooks requires:
- **PostgreSQL**: See configuration guide [here](../05-Infrastructure%20services/01-PostgreSQL.md).
- **Broker**: See configuration guide [here](../05-Infrastructure%20services/02-Message%20broker.md).

Starting with Webhooks `v2.5.0-0`, the API and workers run in separate Deployments. During an update, the Operator stops all workers and waits for their termination before running database migrations. It then rolls out the new API version before recreating the workers.

## Webhooks Object

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#webhooks).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Webhooks
metadata:
  name: formance-dev
spec:
  stack: formance-dev
```
