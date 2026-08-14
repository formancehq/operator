# API Reference

## Packages
- [formance.com/v1beta1](#formancecomv1beta1)


## formance.com/v1beta1

Package v1beta1 contains API Schema definitions for the formance v1beta1 API group.

It lets you configure a Formance stack.

A stack is composed of a [Stack](#stack) resource and some [modules](#modules).

Each module can create multiple resources as needed. See [Other resources](#other-resources).

Various parts of the stack can be configured either using the CRD properties or using some [Settings](#settings).


Modules :
- [Auth](#auth)
- [Connectivity](#connectivity)
- [Gateway](#gateway)
- [Ledger](#ledger)
- [MCP](#mcp)
- [Orchestration](#orchestration)
- [Payments](#payments)
- [Reconciliation](#reconciliation)
- [Search](#search)
- [Stargate](#stargate)
- [TransactionPlane](#transactionplane)
- [Wallets](#wallets)
- [Webhooks](#webhooks)

Other resources :
- [AuthClient](#authclient)
- [Benthos](#benthos)
- [BenthosStream](#benthosstream)
- [Broker](#broker)
- [BrokerConsumer](#brokerconsumer)
- [BrokerTopic](#brokertopic)
- [Database](#database)
- [GatewayGRPCAPI](#gatewaygrpcapi)
- [GatewayHTTPAPI](#gatewayhttpapi)
- [LedgerConfiguration](#ledgerconfiguration)
- [OtelExporterEndpoint](#otelexporterendpoint)
- [ResourceReference](#resourcereference)
- [Versions](#versions)

### Main resources

#### Stack



Stack represents a formance stack.
A Stack is basically a container. It holds some global properties and
creates a namespace if not already existing.

To do more, you need to create some [modules](#modules).

The Stack resource specifies the version of the stack.

It can be specified using either the field `.spec.version` or the `.spec.versionsFromFile` field (see the documentation for the [Versions](#versions) resource).

The `version` field will have priority over `versionFromFile`.

If `versions` and `versionsFromFile` are not specified, modules will fail to reconcile with an explicit error.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Stack` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[StackSpec](#stackspec)_ |  |  |  |
| `status` _[StackStatus](#stackstatus)_ |  |  |  |



##### StackSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version specifies the version of the components<br />Must be a valid Docker tag |  |  |
| `versionsFromFile` _string_ | VersionsFromFile references a formance.com/Versions object which contains individual versions<br />for each component.<br />Must reference a valid formance.com/Versions object |  |  |
| `enableAudit` _boolean_ | EnableAudit enables auditing at the stack level.<br />Currently, it enables auditing on [Gateway](#gateway)<br />deprecated | false |  |
| `disabled` _boolean_ | Disabled indicates that the stack is disabled.<br />A disabled stack disables its modules.<br />It keeps the namespace and the [Database](#database) resources. | false |  |





##### StackStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `modules` _string array_ | Modules register detected modules |  |  |


#### Settings



Settings represents a configurable piece of the stacks.

The purpose of this resource is to configure settings shared across a set of stacks.

Example:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: postgres-uri

spec:

	key: postgres.ledger.uri
	stacks:
	- stack0
	value: postgresql://postgresql.formance.svc.cluster.local:5432

```

This example creates a setting named `postgres-uri` targeting the stack named `stack0` and the service `ledger` (see the key `postgres.ledger.uri`).

Therefore, a [Database](#database) created for the stack `stack0` and the service named 'ledger' will use the uri `postgresql://postgresql.formance.svc.cluster.local:5432`.

Settings supports wildcards in keys and in the stacks list.

For example, if you want to use the same database server for all the modules of a specific stack, you can write:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: postgres-uri

spec:

	key: postgres.*.uri # There, we use a wildcard to indicate we want to use that setting of all services of the stack `stack0`
	stacks:
	- stack0
	value: postgresql://postgresql.formance.svc.cluster.local:5432

```

Also, we could use that setting for all of our stacks using:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: postgres-uri

spec:

	key: postgres.*.uri # There, we use a wildcard to indicate we want to use that setting for all services of all stacks
	stacks:
	- * # There we select all the stacks
	value: postgresql://postgresql.formance.svc.cluster.local:5432

```

Some settings are truly global, while others are used by a specific module.

Refer to the documentation of each module and resource to discover available Settings.

##### Global settings
###### AWS account

A stack can use an AWS account for authentication.

It can be used to connect to any AWS service.

It includes RDS, OpenSearch and MSK. To do so, you can create the following setting:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: aws-service-account

spec:

	key: aws.service-account
	stacks:
	- '*'
	value: aws-access

```
This setting tells the operator that a service account named `aws-access` exists somewhere on the cluster.

So, each time a service has the capability to use AWS, the operator will use this service account.

The service account could look like this:
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:

	annotations:
	  eks.amazonaws.com/role-arn: arn:aws:iam::************:role/staging-eu-west-1-hosting-stack-access
	labels:
	  formance.com/stack: any
	name: aws-access

```
You can note two things:
 1. We have an annotation indicating the role ARN used to connect to AWS. Refer to the AWS documentation to create this role
 2. We have a label `formance.com/stack=any` indicating we are targeting all stacks.
    Refer to the documentation of [ResourceReference](#resourcereference) for further information.

###### JSON logging

You can use the setting `logging.json` with the value `true` to configure eligible services to log as JSON.
Example:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: json-logging

spec:

	key: logging.json
	stacks:
	- '*'
	value: "true"

```

###### Authentication scopes

You can enable scope verification for modules using the setting `auth.<module-name>.check-scopes` or `auth.*.check-scopes` for all modules.
When enabled, modules will verify that authenticated requests include the required scopes for the requested operation.

Example to enable scope verification for all modules:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: enable-scopes-all

spec:

	key: auth.*.check-scopes
	stacks:
	- '*'
	value: "true"

```

Example to enable scope verification only for the ledger module:
```yaml
apiVersion: formance.com/v1beta1
kind: Settings
metadata:

	name: enable-scopes-ledger

spec:

	key: auth.ledger.check-scopes
	stacks:
	- production
	value: "true"

```

Note: The `auth.checkScopes` field in module specifications takes priority over Settings when specified.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Settings` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[SettingsSpec](#settingsspec)_ |  |  |  |



##### SettingsSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stacks` _string array_ | Stacks on which the setting is applied. Can contain `*` to indicate a wildcard. |  |  |
| `key` _string_ | The setting Key. See the documentation of each module or [global settings](#global-settings) to discover them. |  |  |
| `value` _string_ | Value is the setting value. Its required format depends on the Key. |  |  |




### Modules

#### Auth



Auth represents the authentication module of a stack.

It is an OIDC-compliant server.

Creating it for a stack automatically adds authentication to all supported modules.

The auth service is basically a proxy to another OIDC compliant server.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Auth` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[AuthSpec](#authspec)_ |  |  |  |
| `status` _[AuthStatus](#authstatus)_ |  |  |  |



##### AuthSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `delegatedOIDCServer` _[DelegatedOIDCServerConfiguration](#delegatedoidcserverconfiguration)_ | Contains information about a delegated authentication server to use to delegate authentication |  |  |
| `signingKey` _string_ | Overrides the default signing key used to sign JWT tokens. |  |  |
| `signingKeyFromSecret` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#secretkeyselector-v1-core)_ | Overrides the default signing key used to sign JWT tokens, using a k8s secret |  |  |
| `enableScopes` _boolean_ | Enables scope checking during authentication.<br />If not enabled, each service will check the authentication but will not restrict access according to scopes.<br />In that case, being authenticated is sufficient. | false |  |

###### DelegatedOIDCServerConfiguration



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `issuer` _string_ | Issuer is the url of the delegated oidc server |  |  |
| `clientID` _string_ | ClientID is the client id to use for authentication |  |  |
| `clientSecret` _string_ | ClientSecret is the client secret to use for authentication |  |  |
| `clientSecretFromSecret` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#secretkeyselector-v1-core)_ | ClientSecretFromSecret is the client secret to use for authentication |  |  |





##### AuthStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `clients` _string array_ | Clients contains the list of clients created using [AuthClient](#authclient) |  |  |


#### Connectivity



Connectivity is the module that installs a connectivity instance.

Connectivity ingests data from external sources (blockchains, payment
providers, ...) through a plugin system and writes double-entry
transactions into the stack ledger. It delegates the actual workload to the
connectivity operator (connectivity.formance.com), bound to the stack's
Ledger v3 gRPC endpoint.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Connectivity` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ConnectivitySpec](#connectivityspec)_ |  |  |  |
| `status` _[ConnectivityStatus](#connectivitystatus)_ |  |  |  |



##### ConnectivitySpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |





##### ConnectivityStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Gateway



Gateway is the Schema for the gateways API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Gateway` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[GatewaySpec](#gatewayspec)_ |  |  |  |
| `status` _[GatewayStatus](#gatewaystatus)_ |  |  |  |



##### GatewaySpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `ingress` _[GatewayIngress](#gatewayingress)_ | Customizes the generated ingress |  |  |

###### GatewayIngress



GatewayIngress represents the ingress configuration for the gateway.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `host` _string_ | Indicates the hostname on which the stack will be served.<br />Example: `formance.example.com` |  |  |
| `hosts` _string array_ | Additional hosts for the ingress. Combined with Host. |  |  |
| `scheme` _string_ | Indicates the scheme.<br />It should be `https` unless you know what you are doing. | https |  |
| `ingressClassName` _string_ | Ingress class to use |  |  |
| `annotations` _object (keys:string, values:string)_ | Custom annotations to add on the ingress |  |  |
| `tls` _[GatewayIngressTLS](#gatewayingresstls)_ | Customizes the TLS part of the ingress |  |  |

###### GatewayIngressTLS



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | Specify the secret name used for the tls configuration on the ingress |  |  |





##### GatewayStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `syncHTTPAPIs` _string array_ | Detected http apis. See [GatewayHTTPAPI](#gatewayhttpapi) |  |  |
| `syncGRPCAPIs` _string array_ | Detected grpc apis. See [GatewayGRPCAPI](#gatewaygrpcapi) |  |  |


#### Ledger



Ledger is the module that installs a ledger instance.

The ledger is a stateful application that manages financial transactions
and maintains an immutable audit trail.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Ledger` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[LedgerSpec](#ledgerspec)_ |  |  |  |
| `status` _[LedgerStatus](#ledgerstatus)_ |  |  |  |



##### LedgerSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |





##### LedgerStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### MCP



MCP is the Formance Model Context Protocol server module.

It exposes the MCP endpoint for a stack and delegates authorization to the
backend services using the caller bearer token.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `MCP` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[MCPSpec](#mcpspec)_ |  |  |  |
| `status` _[MCPStatus](#mcpstatus)_ |  |  |  |



##### MCPSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |





##### MCPStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Orchestration



Orchestration is the Schema for the orchestrations API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Orchestration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[OrchestrationSpec](#orchestrationspec)_ |  |  |  |
| `status` _[OrchestrationStatus](#orchestrationstatus)_ |  |  |  |



##### OrchestrationSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |





##### OrchestrationStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `temporalURI` _string_ |  |  | Type: string <br /> |


#### Payments



Payments is the Schema for the payments API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Payments` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[PaymentsSpec](#paymentsspec)_ |  |  |  |
| `status` _[PaymentsStatus](#paymentsstatus)_ |  |  |  |



##### PaymentsSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `encryptionKey` _string_ |  |  |  |





##### PaymentsStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Reconciliation



Reconciliation is the Schema for the reconciliations API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Reconciliation` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ReconciliationSpec](#reconciliationspec)_ |  |  |  |
| `status` _[ReconciliationStatus](#reconciliationstatus)_ |  |  |  |



##### ReconciliationSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |





##### ReconciliationStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Search



Search is the Schema for the searches API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Search` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[SearchSpec](#searchspec)_ |  |  |  |
| `status` _[SearchStatus](#searchstatus)_ |  |  |  |



##### SearchSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |





##### SearchStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `elasticSearchURI` _string_ |  |  | Type: string <br /> |


#### Stargate



Stargate is the Schema for the stargates API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Stargate` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[StargateSpec](#stargatespec)_ |  |  |  |
| `status` _[StargateStatus](#stargatestatus)_ |  |  |  |



##### StargateSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `serverURL` _string_ |  |  |  |
| `organizationID` _string_ |  |  |  |
| `stackID` _string_ |  |  |  |
| `auth` _[StargateAuthSpec](#stargateauthspec)_ |  |  |  |
| `tls` _[StargateTLSConfig](#stargatetlsconfig)_ |  |  |  |

###### StargateAuthSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `clientID` _string_ |  |  |  |
| `clientSecret` _string_ |  |  |  |
| `issuer` _string_ |  |  |  |

###### StargateTLSConfig



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `disable` _boolean_ | Disable TLS protocol -- use at your own risk; the transmission will be in cleartext. |  |  |





##### StargateStatus



StargateStatus defines the observed state of Stargate















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### TransactionPlane



TransactionPlane is the Schema for the transactionplanes API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `TransactionPlane` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[TransactionPlaneSpec](#transactionplanespec)_ |  |  |  |
| `status` _[TransactionPlaneStatus](#transactionplanestatus)_ |  |  |  |



##### TransactionPlaneSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |





##### TransactionPlaneStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Wallets



Wallets is the Schema for the wallets API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Wallets` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[WalletsSpec](#walletsspec)_ |  |  |  |
| `status` _[WalletsStatus](#walletsstatus)_ |  |  |  |



##### WalletsSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |





##### WalletsStatus



WalletsStatus defines the observed state of Wallets















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Webhooks



Webhooks is the Schema for the webhooks API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Webhooks` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[WebhooksSpec](#webhooksspec)_ |  |  |  |
| `status` _[WebhooksStatus](#webhooksstatus)_ |  |  |  |



##### WebhooksSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `version` _string_ | Version overrides, for a specific module, the global version defined at stack level |  |  |





##### WebhooksStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


### Other resources

#### AuthClient



AuthClient creates OAuth2/OIDC clients on the auth server (see [Auth](#auth))















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `AuthClient` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[AuthClientSpec](#authclientspec)_ |  |  |  |
| `status` _[AuthClientStatus](#authclientstatus)_ |  |  |  |



##### AuthClientSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `id` _string_ | ID indicates the client id<br />It must be used with oauth2 `client_id` parameter |  |  |
| `public` _boolean_ | Public indicates whether the client is a public client.<br />Confidential clients (the default) are clients whose secret can be kept secret.<br />Public clients cannot hold a secret (a single-page application, for example) | false |  |
| `description` _string_ | Description represents an optional description of the client |  |  |
| `redirectUris` _string array_ | RedirectUris lists the allowed redirect URIs for the client |  |  |
| `postLogoutRedirectUris` _string array_ | PostLogoutRedirectUris lists the allowed post-logout redirect URIs for the client |  |  |
| `scopes` _string array_ | Scopes lists the scopes granted to the client |  |  |
| `secret` _string_ | Secret configures a secret for the client.<br />It is not required, since some clients use oauth2 flows that do not require a client secret |  |  |
| `secretFromSecret` _[SecretKeySelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#secretkeyselector-v1-core)_ |  |  |  |





##### AuthClientStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `hash` _string_ |  |  |  |


#### Benthos



Benthos is the Schema for the benthos API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Benthos` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BenthosSpec](#benthosspec)_ |  |  |  |
| `status` _[BenthosStatus](#benthosstatus)_ |  |  |  |



##### BenthosSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `debug` _boolean_ | Enables debug mode on the module | false |  |
| `dev` _boolean_ | Enables dev mode on the module<br />Dev mode lets an application do custom setup in development mode (allowing insecure certificates, for example) | false |  |
| `resourceRequirements` _[ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#resourcerequirements-v1-core)_ |  |  |  |
| `initContainers` _[Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#container-v1-core) array_ |  |  |  |
| `imagePullSecrets` _[LocalObjectReference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#localobjectreference-v1-core) array_ |  |  |  |
| `resources` _object (keys:string, values:string)_ |  |  |  |
| `templates` _object (keys:string, values:string)_ |  |  |  |
| `envFrom` _[EnvFromSource](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#envfromsource-v1-core) array_ |  |  |  |





##### BenthosStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `elasticSearchURI` _string_ |  |  | Type: string <br /> |


#### BenthosStream



BenthosStream is the Schema for the benthosstreams API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `BenthosStream` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BenthosStreamSpec](#benthosstreamspec)_ |  |  |  |
| `status` _[BenthosStreamStatus](#benthosstreamstatus)_ |  |  |  |



##### BenthosStreamSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `data` _string_ |  |  |  |
| `name` _string_ |  |  |  |





##### BenthosStreamStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `configMapHash` _string_ |  |  |  |


#### Broker



Broker is the Schema for the brokers API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Broker` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BrokerSpec](#brokerspec)_ |  |  |  |
| `status` _[BrokerStatus](#brokerstatus)_ |  |  |  |



##### BrokerSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |





##### BrokerStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `uri` _string_ |  |  | Type: string <br /> |
| `mode` _[Mode](#mode)_ | Mode indicating the configuration of the NATS streams<br />Two modes are defined:<br />- ModeOneStreamByService: In this case, each service will have a dedicated stream created<br />- ModeOneStreamByStack: In this case, a stream will be created for the stack and each service will use a specific subject inside this stream |  | Enum: [OneStreamByService OneStreamByStack] <br /> |
| `streams` _string array_ | Streams list streams created when Mode == ModeOneStreamByService |  |  |

###### Mode

_Underlying type:_ _string_

Mode defines how streams are created on the broker (mainly NATS)

















#### BrokerConsumer



BrokerConsumer is the Schema for the brokerconsumers API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `BrokerConsumer` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BrokerConsumerSpec](#brokerconsumerspec)_ |  |  |  |
| `status` _[BrokerConsumerStatus](#brokerconsumerstatus)_ |  |  |  |



##### BrokerConsumerSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `services` _string array_ |  |  |  |
| `queriedBy` _string_ |  |  |  |
| `name` _string_ | As the name is optional, if not provided, the name will be the QueriedBy property<br />This is only applied when using one stream by stack see Mode |  |  |





##### BrokerConsumerStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### BrokerTopic



BrokerTopic is the Schema for the brokertopics API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `BrokerTopic` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[BrokerTopicSpec](#brokertopicspec)_ |  |  |  |
| `status` _[BrokerTopicStatus](#brokertopicstatus)_ |  |  |  |



##### BrokerTopicSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `service` _string_ |  |  |  |





##### BrokerTopicStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### Database



Database represents a concrete database on a PostgreSQL server. Modules that require a database create it ([Ledger](#ledger), for example).

It uses the settings `postgres.<module-name>.uri` which must have the following uri format: `postgresql://[<username>:<password>@]<host>[:<port>]`.
The database name is not part of the setting: the operator derives it from the stack and the service.
Additionally, the uri can define a query param `secret` indicating a k8s secret that must be used to retrieve database credentials.
Credentials in the secret are expected to be URL-encoded by default. Set `secretCredentialsEncoding=raw` to let the operator encode them.

On creation, the reconciler behind the Database object will create the database on the postgresql server using a k8s job.
On Deletion, by default, the reconciler leaves the database untouched.
You can allow the reconciler to drop the database on the server by using the [Settings](#settings) `clear-database` with the value `true`.
If you use that setting, the reconciler will use another job to drop the database.
Be careful: no backup is performed!

Database resource honors `aws.service-account` setting, so, you can create databases on an AWS server if you need.
See [AWS accounts](#aws-account)

Once a database is fully configured, it retains the postgres uri used.
If the setting that specifies the server uri changes, the Database object will set the field `.status.outOfSync` to true
and will not change anything.

Therefore, to switch to a new server, you must change the setting value, then drop the Database object.
It will be recreated with the correct uri.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Database` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[DatabaseSpec](#databasespec)_ |  |  |  |
| `status` _[DatabaseStatus](#databasestatus)_ |  |  |  |



##### DatabaseSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `service` _string_ | Service is a discriminator for the created database.<br />In practice, it is the module name (ledger, payments...).<br />Therefore, the created database will be named `<stack-name>-<service>` |  |  |
| `debug` _boolean_ |  | false |  |





##### DatabaseStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `uri` _string_ |  |  | Type: string <br /> |
| `database` _string_ | The generated database name |  |  |
| `outOfSync` _boolean_ | OutOfSync indicates that a setting changed the uri of the postgres server<br />The Database object must be removed so that it can be recreated |  |  |


#### GatewayGRPCAPI



GatewayGRPCAPI is the Schema for the GRPCAPIs API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `GatewayGRPCAPI` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[GatewayGRPCAPISpec](#gatewaygrpcapispec)_ |  |  |  |
| `status` _[GatewayGRPCAPIStatus](#gatewaygrpcapistatus)_ |  |  |  |



##### GatewayGRPCAPISpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `name` _string_ | Name indicates the module name (e.g. "ledger") |  |  |
| `grpcServices` _string array_ | GRPCServices is the list of fully-qualified gRPC service names<br />exposed by this module (e.g. "formance.ledger.v1.LedgerService") |  |  |
| `port` _integer_ | Port is the gRPC port on the backend service | 8081 |  |
| `backendRef` _[GatewayBackendRef](#gatewaybackendref)_ | BackendRef overrides the historical <name>-grpc Service. |  |  |

###### GatewayBackendRef



GatewayBackendRef selects the Kubernetes Service used by a Gateway route.
When omitted, Gateway keeps using the module's historical Service.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `name` _string_ | Name is the backend Service name in the Stack namespace. |  | MaxLength: 253 <br />MinLength: 1 <br /> |
| `port` _integer_ | Port is the backend Service port. |  | Maximum: 65535 <br />Minimum: 1 <br /> |
| `tls` _[GatewayBackendTLS](#gatewaybackendtls)_ | TLS enables a verified TLS connection to the backend. |  |  |

###### GatewayBackendTLS



GatewayBackendTLS configures TLS when Gateway connects to a backend.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `secretName` _string_ | SecretName contains the CA used to verify the backend certificate.<br />The Secret must carry the `formance.com/gateway-backend-tls: "true"`<br />label so that certificate rotations trigger a Gateway rollout. |  | MinLength: 1 <br /> |
| `caSecretKey` _string_ | CASecretKey is the key containing the CA certificate. | ca.crt |  |
| `serverName` _string_ | ServerName is used for backend certificate verification. |  | MinLength: 1 <br /> |





##### GatewayGRPCAPIStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### GatewayHTTPAPI



GatewayHTTPAPI is the Schema for the HTTPAPIs API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `GatewayHTTPAPI` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[GatewayHTTPAPISpec](#gatewayhttpapispec)_ |  |  |  |
| `status` _[GatewayHTTPAPIStatus](#gatewayhttpapistatus)_ |  |  |  |



##### GatewayHTTPAPISpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `name` _string_ | Name indicates prefix api |  | MaxLength: 253 <br /> |
| `rules` _[GatewayHTTPAPIRule](#gatewayhttpapirule) array_ | Rules |  | MaxItems: 100 <br /> |
| `healthCheckEndpoint` _string_ | Health check endpoint |  |  |

###### GatewayHTTPAPIRule



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `path` _string_ |  |  |  |
| `methods` _string array_ |  |  |  |
| `secured` _boolean_ |  | false |  |
| `backendRef` _[GatewayBackendRef](#gatewaybackendref)_ | BackendRef overrides the historical module Service for this rule. |  |  |





##### GatewayHTTPAPIStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |


#### LedgerConfiguration



LedgerConfiguration defines the base specification applied to every Ledger v3
Cluster targeted by spec.stacks. A configuration targeting a stack by name
takes priority over a configuration targeting all stacks with `*`.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `LedgerConfiguration` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[LedgerConfigurationSpec](#ledgerconfigurationspec)_ |  |  |  |



##### LedgerConfigurationSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stacks` _string array_ | Stacks on which the configuration is applied. Can contain `*` to<br />indicate a wildcard, following the same convention as Settings. |  |  |
| `cluster` _[ClusterSpec](https://github.com/formancehq/ledger/blob/release/v3.0/misc/operator/api/v1alpha1/cluster_types.go)_ | Cluster is the base Ledger v3 Cluster specification. Stack-specific<br />Settings and values owned by the Operator are applied on top of it. |  |  |




#### OtelExporterEndpoint



OtelExporterEndpoint configures an OpenTelemetry collector proxy for exporting traces and metrics.
Multiple OtelExporterEndpoints can target the same stacks — the collector fans out to all matching destinations.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `OtelExporterEndpoint` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[OtelExporterEndpointSpec](#otelexporterendpointspec)_ |  |  |  |
| `status` _[OtelExporterEndpointStatus](#otelexporterendpointstatus)_ |  |  |  |



##### OtelExporterEndpointSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stackSelector` _[LabelSelector](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#labelselector-v1-meta)_ | StackSelector is a standard Kubernetes LabelSelector (matchLabels/matchExpressions).<br />One CRD can target all current and future stacks with a single selector.<br />Matches the pattern established by Settings. |  |  |
| `traces` _[OtelSignalConfig](#otelsignalconfig)_ | Traces configures the traces signal. At least one of traces or metrics must be set.<br />Logs are intentionally out of scope. |  |  |
| `metrics` _[OtelSignalConfig](#otelsignalconfig)_ | Metrics configures the metrics signal. At least one of traces or metrics must be set.<br />Logs are intentionally out of scope. |  |  |
| `resourceAttributes` _object (keys:string, values:string)_ | ResourceAttributes are injected into outgoing telemetry via a collector processor. |  |  |

###### OtelSignalConfig



OtelSignalConfig configures a single signal type (traces or metrics).
Each signal type has its own endpoint and authentication block, allowing
different destinations or credentials per signal.
Protocol is inferred from the URL scheme: grpc:// for gRPC, http:// or https:// for HTTP/protobuf (default).















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `endpoint` _string_ | Endpoint URL for the signal (e.g., "http://my-collector:4318", "grpc://my-collector:4317").<br />Supported schemes: http, https, grpc.<br />Protocol is inferred from the URL scheme. HTTP/protobuf is the default for firewall compatibility. |  | MinLength: 1 <br />Pattern: `^(https?://|grpc://)` <br /> |
| `auth` _[OtelExporterAuth](#otelexporterauth)_ | Auth is the optional per-signal authentication configuration. |  |  |

###### OtelExporterAuth



OtelExporterAuth configures per-signal authentication.
Auth is per-signal so traces and metrics can use different credentials if needed.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `type` _string_ | Type is the authentication type. |  | Enum: [bearer] <br /> |
| `fromSecret` _string_ | FromSecret references a Secret name.<br />The controller creates a ResourceReference to replicate the secret into each target stack namespace.<br />The source secret must have a "formance.com/stack" label set to "any" or a specific stack name. |  | MinLength: 1 <br /> |
| `fromSecretKey` _string_ | FromSecretKey is the key within the Secret that contains the token. Defaults to "token". | token |  |





##### OtelExporterEndpointStatus



OtelExporterEndpointStatus represents the observed state of an OtelExporterEndpoint.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `stacks` _string array_ | Stacks is a sorted list of stack names currently targeted by this endpoint.<br />Includes stacks with successful reconciliation and stacks with transient errors or pending cleanup.<br />Used by the finalizer to find previously matched stacks during deletion. |  |  |


#### ResourceReference



ResourceReference is a special resource used to refer to externally created resources.

It includes k8s service accounts and secrets.

Why? Because the operator creates one namespace per stack, so a stack does not have access to secrets and service
accounts created externally.

A ResourceReference is created by another resource that needs a specific secret or service account.
For example, if you want to use a secret for your database connection (see [Database](#database)), you will
create a setting indicating a secret name. You will need to create this secret yourself, and you will put this
secret inside the namespace you want (`default` maybe).

The Database reconciler will create a ResourceReference that looks like this:
```
apiVersion: formance.com/v1beta1
kind: ResourceReference
metadata:

	name: jqkuffjxcezj-qlii-auth-postgres
	ownerReferences:
	- apiVersion: formance.com/v1beta1
	  blockOwnerDeletion: true
	  controller: true
	  kind: Database
	  name: jqkuffjxcezj-qlii-auth
	  uid: 2cc4b788-3ffb-4e3d-8a30-07ed3941c8d2

spec:

	gvk:
	  group: ""
	  kind: Secret
	  version: v1
	name: postgres
	stack: jqkuffjxcezj-qlii

status:

	...

```
The reconciler behind this ResourceReference searches all namespaces for a secret named "postgres".
The secret must have a label `formance.com/stack` with the value matching either a specific stack or `any` to target any stack.

Once the reconciler has found the secret, it will copy it inside the stack namespace, allowing the ResourceReconciler owner to use it.















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `ResourceReference` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ResourceReferenceSpec](#resourcereferencespec)_ |  |  |  |
| `status` _[ResourceReferenceStatus](#resourcereferencestatus)_ |  |  |  |



##### ResourceReferenceSpec



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `stack` _string_ | Stack indicates the stack on which the module is installed |  |  |
| `gvk` _[GroupVersionKind](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#groupversionkind-v1-meta)_ |  |  |  |
| `name` _string_ |  |  |  |





##### ResourceReferenceStatus



















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `ready` _boolean_ | Ready indicates if the resource is seen as completely reconciled |  |  |
| `info` _string_ | Info can contain any additional detail, such as reconciliation errors |  |  |
| `syncedResource` _string_ |  |  |  |
| `hash` _string_ |  |  |  |


#### Versions



Versions is the Schema for the versions API















| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `formance.com/v1beta1` | | |
| `kind` _string_ | `Versions` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.27/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _object (keys:string, values:string)_ |  |  |  |





