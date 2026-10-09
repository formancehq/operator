Formance Payments is a unified API that abstracts over multiple different payment providers like Stripe and Wise, simplifying the process of building money flows that stitch multiple payment providers together.

## Requirements

Formance Payments requires:
- **PostgreSQL**: See configuration guide [here](../05-Infrastructure%20services/01-PostgreSQL.md).
- (Optional) **Broker**: See configuration guide [here](../05-Infrastructure%20services/02-Message%20broker.md).

Payments cannot be installed alongside Ledger v3 (including prereleases).
Remove the Payments module before switching the stack's Ledger from v2 to v3.
Stacks without a Ledger module remain supported.

## Payments Object

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#payments).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Payments
metadata:
  name: formance-dev
spec:
  stack: formance-dev
```
