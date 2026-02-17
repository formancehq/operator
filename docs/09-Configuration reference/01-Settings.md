The Settings CRD is one of the most important CRDs in Operator v2. It enables all the necessary adjustments so that the Operator can adapt to your usage and environment.

Settings are encoded as string, but under the hood, each setting can be unmarshalled to a specific type.

While we have some basic types (string, number, bool ...), we also have some complex structures:
* Maps: maps are just one level dictionary with values as string. Repeat `<key>=<value>` pattern for each entry, while separating with comma.
* URIs: URIs are used each time we need to address an external resource (postgres, kafka ...). URIs are convenient to encode a lot of information in a simple, normalized format.

## Available settings

| Key                                                                                      | Type   | Example                                                                                                                                                                                                                | Description                                                                                                                                                                                                                      |
| ---------------------------------------------------------------------------------------- | ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| aws.service-account                                                                      | string |                                                                                                                                                                                                                        | AWS Role                                                                                                                                                                                                                         |
| postgres.`<module-name>`.uri                                                             | URI    |                                                                                                                                                                                                                        | Postgres database configuration                                                                                                                                                                                                  |
| elasticsearch.dsn                                                                        | URI    |                                                                                                                                                                                                                        | Elasticsearch connection URI                                                                                                                                                                                                     |
| temporal.dsn                                                                             | URI    |                                                                                                                                                                                                                        | Temporal URI                                                                                                                                                                                                                     |
| temporal.tls.crt                                                                         | string |                                                                                                                                                                                                                        | Temporal certificate                                                                                                                                                                                                             |
| temporal.tls.key                                                                         | string |                                                                                                                                                                                                                        | Temporal certificate key                                                                                                                                                                                                         |
| broker.dsn                                                                               | URI    |                                                                                                                                                                                                                        | Broker URI                                                                                                                                                                                                                       |
| opentelemetry.traces.dsn                                                                 | URI    |                                                                                                                                                                                                                        | OpenTelemetry collector URI                                                                                                                                                                                                      |
| opentelemetry.traces.resource-attributes                                                 | Map    | key1=value1,key2=value2                                                                                                                                                                                                | Opentelemetry additional resource attributes                                                                                                                                                                                     |
| clear-database                                                                           | bool   | true                                                                                                                                                                                                                   | Whether to remove databases on stack deletion                                                                                                                                                                                    |
| ledger.logs.max-batch-size                                                               | Int    | 1024                                                                                                                                                                                                                   | Ledger logs batching max size                                                                                                                                                                                                    |
| ledger.api.bulk-max-size                                                                 | Int    | 100                                                                                                                                                                                                                    | Max bulk size                                                                                                                                                                                                                    |
| ledger.api.default-page-size                                                             | Int    |                                                                                                                                                                                                                        | Default api page size                                                                                                                                                                                                            |
| ledger.api.max-page-size                                                                 | Int    |                                                                                                                                                                                                                        | Max page size                                                                                                                                                                                                                    |
| ledger.experimental-features                                                             | Bool   | true                                                                                                                                                                                                                   | Enable experimental features                                                                                                                                                                                                     |
| ledger.experimental-numscript                                                            | Bool   | true                                                                                                                                                                                                                   | Enable new numscript interpreter                                                                                                                                                                                                 |
| ledger.experimental-numscript-flags                                                      | Array  | experimental-overdraft-function experimental-get-asset-function experimental-get-amount-function experimental-oneof experimental-account-interpolation experimental-mid-script-function-call experimental-asset-colors | Enable numscript interpreter flags                                                                                                                                                                                               |
| ledger.experimental-exporters                                                            | Bool   | true                                                                                                                                                                                                                   | Enable new exporters feature                                                                                                                                                                                                     |
| payments.encryption-key                                                                  | string |                                                                                                                                                                                                                        | Payments data encryption key                                                                                                                                                                                                     |
| payments.worker.temporal-max-concurrent-workflow-task-pollers                            | Int    |                                                                                                                                                                                                                        | Payments worker max concurrent workflow task pollers configuration                                                                                                                                                               |
| payments.worker.temporal-max-concurrent-activity-task-pollers                            | Int    |                                                                                                                                                                                                                        | Payments worker max concurrent activity task pollers configuration                                                                                                                                                               |
| payments.worker.temporal-max-slots-per-poller                                            | Int    |                                                                                                                                                                                                                        | Payments worker max slots per poller                                                                                                                                                                                             |
| payments.worker.temporal-max-local-activity-slots                                        | Int    |                                                                                                                                                                                                                        | Payments worker max local activity slots                                                                                                                                                                                         |
| deployments.`<deployment-name>`.containers.`<container-name>`.resource-requirements      | Map    | cpu=X, mem=X                                                                                                                                                                                                           |                                                                                                                                                                                                                                  |
| deployments.`<deployment-name>`.containers.`<container-name>`.run-as                     | Map    | user=X, group=X                                                                                                                                                                                                        |                                                                                                                                                                                                                                  |
| deployments.`<deployment-name>`.init-containers.`<container-name>`.resource-requirements | Map    | cpu=X, mem=X                                                                                                                                                                                                           |                                                                                                                                                                                                                                  |
| deployments.`<deployment-name>`.init-containers.`<container-name>`.run-as                | Map    | user=X, group=X                                                                                                                                                                                                        |                                                                                                                                                                                                                                  |
| deployments.`<deployment-name>`.replicas                                                 | string | 2                                                                                                                                                                                                                      |                                                                                                                                                                                                                                  |
| deployments.`<deployment-name>`.semconv-metrics-names                                    | Bool   | true                                                                                                                                                                                                                   | Enable semantic convention metrics names by setting SEMCONV_METRICS_NAME environment variable to true in all containers                                                                                                          |
| deployments.`<deployment-name>`.spec.template.annotations                                | Map    | firstannotations=X, anotherannotation=X                                                                                                                                                                                |                                                                                                                                                                                                                                  |
| deployments.`<deployment-name>`.spec.template.spec.termination-grace-period-seconds      | Int    | 30                                                                                                                                                                                                                     | Specify the termination grace period for the deployment                                                                                                                                                                          |
| deployments.`<deployment-name>`.topology-spread-constraints                              | Bool   | true                                                                                                                                                                                                                   | Enable topology spread constraints in deployments to maximize high availability of deployments                                                                                                                                   |
| caddy.image                                                                              | string |                                                                                                                                                                                                                        | Caddy image                                                                                                                                                                                                                      |
| jobs.`<owner-kind>`.spec.template.annotations                                            | Map    | firstannotations=X, anotherannotations=Y                                                                                                                                                                               | Configure the annotations on specific jobs'modules                                                                                                                                                                               |
| jobs.`<owner-kind>`.init-containers.`<container-name>`.run-as                            | Map    | user=X, group=X                                                                                                                                                                                                        | Configure the security context for init containers in jobs by specifying the user and group IDs to run as                                                                                                                        |
| jobs.`<owner-kind>`.containers.`<container-name>`.run-as                                 | Map    | user=X, group=X                                                                                                                                                                                                        | Configure the security context for containers in jobs by specifying the user and group IDs to run as                                                                                                                             |
| registries.`<name>`.endpoint                                                             | string | example.com?pullSecret=foo                                                                                                                                                                                             | Specify a custom endpoint for a specific docker repository                                                                                                                                                                       |
| registries.`<name>`.images.`<path>`.rewrite                                              | string | formancehq/example                                                                                                                                                                                                     | Allow to rewrite the image path                                                                                                                                                                                                  |
| services.`<service-name>`.annotations                                                    | Map    |                                                                                                                                                                                                                        | Allow to specify custom annotations to apply on created k8s services                                                                                                                                                             |
| services.`<service-name>`.traffic-distribution                                           | string | PreferSameZone, PreferSameNode, PreferClose                                                                                                                                                                            | Configure traffic distribution for Kubernetes services (requires Kubernetes 1.34+). See [Kubernetes documentation](https://kubernetes.io/docs/reference/networking/virtual-ips)                                                  |
| gateway.ingress.annotations                                                              | Map    |                                                                                                                                                                                                                        | Allow to specify custom annotations to apply on the gateway ingress                                                                                                                                                              |
| gateway.ingress.hosts                                                                    | string | app.example.com,app.example.org                                                                                                                                                                                        | Comma-separated list of additional hosts for the gateway ingress. Combined with hosts defined on the Gateway CRD                                                                                                                 |
| gateway.ingress.labels                                                                   | Map    |                                                                                                                                                                                                                        | Allow to specify custom labels to apply on the gateways ingress                                                                                                                                                                  |
| logging.json                                                                             | bool   |                                                                                                                                                                                                                        | Configure services to log as json                                                                                                                                                                                                |
| modules.`<module-name>`.database.connection-pool                                         | Map    | max-idle=10, max-idle-time=10s, max-open=10, max-lifetime=5m                                                                                                                                                           | Configure database connection pool for each module. See [Golang documentation](https://go.dev/doc/database/manage-connections)                                                                                                   |
| orchestration.max-parallel-activities                                                    | Int    | 10                                                                                                                                                                                                                     | Configure max parallel temporal activities on orchestration workers                                                                                                                                                              |
| modules.`<module-name>`.grace-period                                                     | string | 5s                                                                                                                                                                                                                     | Defer application shutdown                                                                                                                                                                                                       |
| namespace.labels                                                                         | Map    | somelabel=somevalue,anotherlabel=anothervalue                                                                                                                                                                          | Add static labels to namespace                                                                                                                                                                                                   |
| namespace.annotations                                                                    | Map    | someannotation=somevalue,anotherannotation=anothervalue                                                                                                                                                                | Add static annotations to namespace                                                                                                                                                                                              |
| gateway.ingress.tls.enabled                                                              | bool   | true                                                                                                                                                                                                                   | Enable TLS if not enabled at Gateway CRD level                                                                                                                                                                                   |
| gateway.caddyfile.trusted-proxies                                                        | string | 10.0.0.0/8,192.168.0.0/16                                                                                                                                                                                              | Comma-separated list of IP ranges (CIDRs) of trusted proxy servers. Caddy will parse the real client IP from HTTP headers when requests come from these proxies. Use `private_ranges` to match all private IPv4 and IPv6 ranges. |
| gateway.caddyfile.trusted-proxies-strict                                                 | bool   | false                                                                                                                                                                                                                  | Enable strict (right-to-left) parsing of the X-Forwarded-For header. Recommended when using upstream proxies like HAProxy, Cloudflare, AWS ALB, or CloudFront.                                                                   |
| gateway.config.idle-timeout                                                              | string | 10m                                                                                                                                                                                                                    | Configure the idle timeout for client connections (default: 5m). Use Go duration format (e.g., 30s, 5m, 1h).                                                                                                                     |
| gateway.dns.private.enabled                                                              | bool   | false                                                                                                                                                                                                                  | Enable generation of private DNS endpoints for the gateway                                                                                                                                                                       |
| gateway.dns.private.dns-names                                                            | string |                                                                                                                                                                                                                        | DNS name pattern(s) for private DNS endpoints. Comma-separated list. Supports `{stack}` placeholder                                                                                                                              |
| gateway.dns.private.targets                                                              | string |                                                                                                                                                                                                                        | Target(s) for private DNS records. Comma-separated list                                                                                                                                                                          |
| gateway.dns.private.record-type                                                          | string | CNAME                                                                                                                                                                                                                  | DNS record type (e.g., CNAME, A, AAAA)                                                                                                                                                                                           |
| gateway.dns.private.provider-specific                                                    | Map    | alias=true,aws/target-hosted-zone=same-zone                                                                                                                                                                            | Provider-specific DNS settings for private endpoints                                                                                                                                                                             |
| gateway.dns.private.annotations                                                          | Map    | service.beta.kubernetes.io/aws-load-balancer-internal=true                                                                                                                                                             | Annotations to add to the private DNSEndpoint resource                                                                                                                                                                           |
| gateway.dns.public.enabled                                                               | bool   | false                                                                                                                                                                                                                  | Enable generation of public DNS endpoints for the gateway                                                                                                                                                                        |
| gateway.dns.public.dns-names                                                             | string |                                                                                                                                                                                                                        | DNS name pattern(s) for public DNS endpoints. Comma-separated list. Supports `{stack}` placeholder                                                                                                                               |
| gateway.dns.public.targets                                                               | string |                                                                                                                                                                                                                        | Target(s) for public DNS records. Comma-separated list                                                                                                                                                                           |
| gateway.dns.public.record-type                                                           | string | CNAME                                                                                                                                                                                                                  | DNS record type (e.g., CNAME, A, AAAA)                                                                                                                                                                                           |
| gateway.dns.public.provider-specific                                                     | Map    | alias=true,aws/target-hosted-zone=same-zone                                                                                                                                                                            | Provider-specific DNS settings for public endpoints                                                                                                                                                                              |
| gateway.dns.public.annotations                                                           | Map    |                                                                                                                                                                                                                        | Annotations to add to the public DNSEndpoint resource                                                                                                                                                                            |

### Postgres URI format

Scheme: postgresql

Query params :

| Name           | Type   | Default | Description                                    |
| -------------- | ------ | ------- | ---------------------------------------------- |
| secret         | string |         | Specify a secret where credentials are defined |
| disableSSLMode | bool   | false   | Disable SSL on Postgres connection             |

### ElasticSearch URI format

Scheme: elasticsearch

Query params :

| Name   | Type   | Default | Description                                    |
| ------ | ------ | ------- | ---------------------------------------------- |
| secret | string |         | Specify a secret where credentials are defined |

### Temporal URI format

Scheme : temporal

Path : Match the temporal namespace

Query params :

| Name                 | Type   | Default | Description                                               |
| -------------------- | ------ | ------- | --------------------------------------------------------- |
| secret               | string |         | Specify a secret where temporal certificates are defined  |
| encryptionKeySecret  | string |         | Specify a secret where temporal encryption key is defined |
| initSearchAttributes | string | false   | Initialize search attributes on temporal namespace        |

### Broker URI format

Scheme : nats | kafka

#### Broker URI format (nats)

Scheme: nats

Query params :

| Name     | Type   | Default | Description                                                               |
| -------- | ------ | ------- | ------------------------------------------------------------------------- |
| replicas | number | 1       | Specify the number of replicas to configure on newly created nats streams |

#### Broker URI format (kafka)

Scheme: kafka

Query params :

| Name             | Type   | Default | Description                                    |
| ---------------- | ------ | ------- | ---------------------------------------------- |
| saslEnabled      | bool   | false   | Specify is sasl authentication must be enabled |
| saslUsername     | string |         | Username on sasl authentication                |
| saslPassword     | string |         | Password on sasl authentication                |
| saslMechanism    | string |         | Mechanism on sasl authentication               |
| saslSCRAMSHASize | string |         | SCRAM SHA size on sasl authentication          |
| tls              | bool   | false   | Whether enable ssl or not                      |



The process is always the same: you create a YAML file, submit it to Kubernetes, and the Operator takes care of the rest.
All the values present in the `Metadata` section are not used by the Operator. Conversely, the `Spec` section is used to define the Operator's parameters.
You will always find 3 parameters there:
- **stacks**: defines the stacks that should use this configuration (you can put a `*` to indicate that all stacks should use this configuration)
- **key**: defines the key of the configuration (you can put a `*` so that it applies to all services)
- **value**: defines the value of the configuration

## Examples
### Define PostgreSQL clusters
In this example, you will set up a configuration for a PostgreSQL cluster that will be used only by the `formance-dev` stack but will apply to all the modules of this stack.
Thus, the different modules of the Stack will use this PostgreSQL cluster while being isolated in their own database.

:::info
This database is created following the format: `{stackName}-{module}`
:::

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-postgres-uri
spec:
  key: postgres.*.uri
  stacks:
    - 'formance-dev'
  value: postgresql://formance:formance@postgresql.formance-system.svc:5432?disableSSLMode=true
```

### Use AWS IAM Role
In this example, you'll use an AWS IAM role to connect to the database. The `formance-dev` stack will use this configuration.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: aws-rds-access-role
  namespace: formance-system
  labels:
    formance.com/stack: any
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::AWS_ACCOUNT_ID:role/AWS_ROLE_NAME
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-postgres-uri
spec:
  key: postgres.*.uri
  stacks:
    - 'formance-dev'
  value: postgresql://formance@postgresql.formance-system.svc:5432
 ```

### Define module resource requests
In this example, you'll set up a configuration for the resource requests of the `formance-dev` stack. This configuration will apply to all the modules of this stack.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-resource-requests
spec:
  key: deployments.*.containers.*.resource-requirements.requests
  stacks:
    - 'formance-dev'
  value: cpu=10m,memory=100Mi
```

### Define a Broker
In this example, you'll set up a configuration for the Broker of the `formance-dev` stack. This configuration will apply to all the modules of this stack.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-broker
spec:
  key: broker.dsn
  stacks:
    - 'formance-dev'
  value: nats://nats.formance-system.svc:4222?replicas=3
```

### Define a OpenTelemetry Collector

In this example, you'll set up a configuration to send traces and metrics to an OpenTelemetry collector. This configuration will apply to all modules in this stack.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: stacks-otel-collector
spec:
  key: opentelemetry.*.dsn
  stacks:
    - "formance-dev"
  value: grpc://opentelemetry-collector.formance-system.svc:4317?insecure=true
```

### Configure Database Jobs Security context - Run As

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-database-create-run-as
spec:
  key: jobs.database.containers.create-database.run-as
  stacks:
    - 'formance-dev'
  value: user=1234,group=1234
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-database-drop-run-as
spec:
  key: jobs.database.containers.drop-database.run-as
  stacks:
    - 'formance-dev'
  value: user=1234,group=1234
```

### Configure ledger migrate job security context - Run As

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-ledger-migrate-run-as
spec:
  key: jobs.ledger.containers.migrate.run-as
  stacks:
    - 'formance-dev'
  value: user=1234,group=1234
```

### Configure DNS Endpoints

The operator can automatically generate `externaldns.k8s.io/v1alpha1` DNSEndpoint resources for your Gateway components. This allows you to configure DNS records that will be managed by the external-dns operator.

DNS endpoints are linked to Gateway components and are created in the stack namespace. You can configure both private and public DNS endpoints independently.

#### Private DNS Endpoint Example

In this example, you'll configure a private DNS endpoint for the `formance-dev` stack. The DNS endpoint will be created with the name `{gateway-name}-private` and will point to the specified targets.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-private-enabled
spec:
  key: gateway.dns.private.enabled
  stacks:
    - 'formance-dev'
  value: "true"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-private-dns-names
spec:
  key: gateway.dns.private.dns-names
  stacks:
    - 'formance-dev'
  value: "{stack}-eks-euw1-01.dev.acme.frmnc.net,{stack}.dev.acme.frmnc.net"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-private-targets
spec:
  key: gateway.dns.private.targets
  stacks:
    - 'formance-dev'
  value: "rp-01-eks-euw1-01.dev.acme.frmnc.net"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-private-provider-specific
spec:
  key: gateway.dns.private.provider-specific
  stacks:
    - 'formance-dev'
  value: "alias=true,aws/target-hosted-zone=same-zone"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-private-annotations
spec:
  key: gateway.dns.private.annotations
  stacks:
    - 'formance-dev'
  value: "service.beta.kubernetes.io/aws-load-balancer-internal=true"
```

#### Public DNS Endpoint Example

In this example, you'll configure a public DNS endpoint for the `formance-dev` stack. The DNS endpoint will be created with the name `{gateway-name}-public`.

```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-public-enabled
spec:
  key: gateway.dns.public.enabled
  stacks:
    - 'formance-dev'
  value: "true"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-public-dns-names
spec:
  key: gateway.dns.public.dns-names
  stacks:
    - 'formance-dev'
  value: "{stack}.acme.frmnc.net"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-public-targets
spec:
  key: gateway.dns.public.targets
  stacks:
    - 'formance-dev'
  value: "rp-01-eks-euw1-01.dev.acme.frmnc.net"
---
apiVersion: formance.com/v1beta1
kind: Settings
metadata:
  name: formance-dev-dns-public-provider-specific
spec:
  key: gateway.dns.public.provider-specific
  stacks:
    - 'formance-dev'
  value: "alias=true,aws/target-hosted-zone=same-zone"
```

#### DNS Settings Details

- **DNS Names**: The `dns-names` setting supports the `{stack}` placeholder which will be replaced with the actual stack name. You can specify multiple DNS names by separating them with commas. Each DNS name will create a separate endpoint in the DNSEndpoint resource.

- **Targets**: Multiple targets can be specified by separating them with commas. All targets will be added to each DNS record endpoint.

- **Record Type**: Defaults to `CNAME` if not specified. Common values include `CNAME`, `A`, `AAAA`, `TXT`, etc.

- **Provider-Specific Settings**: These are provider-specific DNS configurations. For AWS Route53, common settings include:
  - `alias=true`: Enable alias records
  - `aws/target-hosted-zone=same-zone`: Use the same hosted zone for the target

- **Annotations**: Annotations added to the DNSEndpoint resource. Useful for provider-specific configurations or metadata.

:::info
DNS endpoints are created per Gateway component. If you have multiple Gateways in a stack, each will have its own DNS endpoints named `{gateway-name}-private` and `{gateway-name}-public`.
:::

:::warning
The external-dns operator must be installed in your cluster for DNS endpoints to be processed. The operator only creates the DNSEndpoint resources; the external-dns operator is responsible for actually creating the DNS records.
:::

<!-- ### Define a Replicas -->
<!-- In this example, we'll set up a configuration to define the number of replicas for the `formance-dev` stack. This configuration will apply to all modules in this stack. -->

<!-- ```yaml -->
<!-- apiVersion: formance.com/v1beta1 -->
<!-- kind: Settings -->
<!-- metadata: -->
<!--   name: stacks-replicas -->
<!-- spec: -->
<!--   key: replicas -->
<!--   stacks: -->
<!--     - "formance-dev" -->
<!--   value: "2" -->
<!-- ``` -->
