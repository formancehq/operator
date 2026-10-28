## Overview

A Stack represents a set of modules that are deployed together. It is used as a way to group modules and to deploy them together with a consistent set of versions and configurations.

When you deploy a Formance module, such as Ledger or Payments, you deploy them within a Stack. This allows you to ensure that they are all deployed with the same versions and configurations.

## Stack Object
The first object that needs to be created is the Stack object. This allows linking the different modules and deploying them together.

:::info
You can find all the available parameters in [the comprehensive CRD documentation](../09-Configuration%20reference/02-Custom%20Resource%20Definitions.md#stack).
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Stack
metadata:
  name: formance-dev
spec:
  versionsFromFile: v2.0
```

During the deployment of the Operator and for its future upgrades, the Versions files are automatically updated following the semver pattern.
It is possible to specify a specific version by using the `versionFromFile` field in the Stack file.

## Disable and deletion

Disabling a Stack retains its namespace and durable resources. Connectivity uses
its delegated disable contract to stop Core, API and all ConnectorInstance
workloads while retaining configuration and credentials. Other modules keep their
existing owned-runtime removal behavior. See [Connectivity lifecycle](12-Connectivity.md#disable-and-delete)
for rollout requirements and observable completion conditions.

Deleting a Stack requests deletion of all modules without imposing a Connectivity
cleanup order, then follows the existing resource and namespace deletion lifecycle.
The Connectivity controller owns its asynchronous cleanup. Namespace deletion also
deletes externally supplied Secrets stored inside it.

Do not create or reassign module attachments while a Stack is deleting. Normal
reconciliation reads the Stack through the manager's cached client and checks its
deletion timestamp; deletion discovers
existing attachments while requesting cleanup. These checks do not provide a
transaction across independent API writers.
