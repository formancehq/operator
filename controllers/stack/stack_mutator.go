package stack

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	authcomponentsv1beta1 "github.com/numary/operator/apis/auth.components/v1beta1"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/typeutils"
	traefik "github.com/traefik/traefik/v2/pkg/provider/kubernetes/crd/traefik/v1alpha1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	stackv1beta2 "github.com/numary/operator/apis/stack/v1beta2"
	pkgError "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=traefik.containo.us,resources=middlewares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=stack.formance.com,resources=configurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=stack.formance.com,resources=versions,verbs=get;list;watch

type Mutator struct {
	client   client.Client
	scheme   *runtime.Scheme
	dnsNames []string
}

func watch(mgr ctrl.Manager, field string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
		stacks := &stackv1beta2.StackList{}
		listOps := &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(field, object.GetName()),
			Namespace:     object.GetNamespace(),
		}
		err := mgr.GetClient().List(context.TODO(), stacks, listOps)
		if err != nil {
			return []reconcile.Request{}
		}

		return Map(stacks.Items, func(s stackv1beta2.Stack) reconcile.Request {
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      s.GetName(),
					Namespace: s.GetNamespace(),
				},
			}
		})
	})
}

func (r *Mutator) SetupWithBuilder(mgr ctrl.Manager, bldr *ctrl.Builder) error {

	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &stackv1beta2.Stack{}, ".spec.configuration", func(rawObj client.Object) []string {
			return []string{rawObj.(*stackv1beta2.Stack).Spec.Seed}
		}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &stackv1beta2.Stack{}, ".spec.versions", func(rawObj client.Object) []string {
			return []string{rawObj.(*stackv1beta2.Stack).Spec.Versions}
		}); err != nil {
		return err
	}

	bldr.
		Owns(&componentsv1beta2.Auth{}).
		Owns(&componentsv1beta2.Ledger{}).
		Owns(&componentsv1beta2.Search{}).
		Owns(&componentsv1beta2.Payments{}).
		Owns(&componentsv1beta2.Webhooks{}).
		Owns(&corev1.Namespace{}).
		Owns(&traefik.Middleware{}).
		Watches(
			&source.Kind{Type: &stackv1beta2.Configuration{}},
			watch(mgr, ".spec.configuration"),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&source.Kind{Type: &stackv1beta2.Versions{}},
			watch(mgr, ".spec.versions"),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)

	return nil
}

func (r *Mutator) Mutate(ctx context.Context, stack *stackv1beta2.Stack) (*ctrl.Result, error) {
	apisv1beta1.SetProgressing(stack)

	configuration := &stackv1beta2.Configuration{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name: stack.Spec.Seed,
	}, configuration); err != nil {
		if errors.IsNotFound(err) {
			return nil, pkgError.New("Configuration object not found")
		}
		return controllerutils.Requeue(), fmt.Errorf("error retrieving Configuration object: %s", err)
	}

	configurationSpec := configuration.Spec
	// TODO: Reuse standard validation
	if err := configurationSpec.Validate(); len(err) > 0 {
		return nil, pkgError.Wrap(err.ToAggregate(), "Validating configuration")
	}

	version := &stackv1beta2.Versions{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name: stack.Spec.Versions,
	}, version); err != nil {
		if errors.IsNotFound(err) {
			return nil, pkgError.New("Versions object not found")
		}
		return controllerutils.Requeue(), fmt.Errorf("error retrieving Versions object: %s", err)
	}

	// Add static clients for app needing it (Actually, control)
	if stack.Status.StaticAuthClients == nil {
		stack.Status.StaticAuthClients = map[string]authcomponentsv1beta1.StaticClient{}
	}

	if _, ok := stack.Status.StaticAuthClients["control"]; !ok {
		stack.Status.StaticAuthClients["control"] = authcomponentsv1beta1.StaticClient{
			ID:      "control",
			Secrets: []string{uuid.NewString()},
			ClientConfiguration: authcomponentsv1beta1.ClientConfiguration{
				Scopes: []string{"openid", "profile", "email", "offline"},
				RedirectUris: []string{
					fmt.Sprintf("%s/auth/login", stack.URL()),
				},
				PostLogoutRedirectUris: []string{
					fmt.Sprintf("%s/auth/destroy", stack.URL()),
				},
			},
		}
	}

	if err := r.reconcileNamespace(ctx, stack); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling namespace")
	}
	if err := r.reconcileMiddleware(ctx, stack); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling middleware")
	}
	if err := r.reconcileAuth(ctx, stack, &configurationSpec, version.Spec.Auth); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Auth")
	}
	if err := r.reconcileLedger(ctx, stack, &configurationSpec, version.Spec.Ledger); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Ledger")
	}
	if err := r.reconcilePayment(ctx, stack, &configurationSpec, version.Spec.Payments); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Payment")
	}
	if err := r.reconcileSearch(ctx, stack, &configurationSpec, version.Spec.Search); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Search")
	}
	if err := r.reconcileControl(ctx, stack, &configurationSpec, version.Spec.Control); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Control")
	}
	if err := r.reconcileWebhooks(ctx, stack, &configurationSpec, version.Spec.Webhooks); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Webhooks")
	}
	if err := r.reconcileWallets(ctx, stack, &configurationSpec, version.Spec.Wallets); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Wallets")
	}
	if err := r.reconcileCounterparties(ctx, stack, &configurationSpec, version.Spec.Counterparties); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Counterparties")
	}

	apisv1beta1.SetReady(stack)
	return nil, nil
}

func (r *Mutator) reconcileNamespace(ctx context.Context, stack *stackv1beta2.Stack) error {
	log.FromContext(ctx).Info("Reconciling Namespace")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Name: stack.Name,
	}, stack, func(ns *corev1.Namespace) error {
		// No additional mutate needed
		return nil
	})
	switch {
	case err != nil:
		stack.SetNamespaceError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetNamespaceCreated()
	}

	log.FromContext(ctx).Info("Namespace ready")
	return nil
}

func (r *Mutator) reconcileMiddleware(ctx context.Context, stack *stackv1beta2.Stack) error {
	log.FromContext(ctx).Info("Reconciling Middleware")

	m := make(map[string]apiextensionv1.JSON)
	m["auth"] = apiextensionv1.JSON{
		Raw: []byte(fmt.Sprintf(`{"Issuer": "%s", "RefreshTime": "%s", "ExcludePaths": ["/_health", "/_healthcheck", "/.well-known/openid-configuration"]}`, stack.Spec.Scheme+"://"+stack.Spec.Host+"/api/auth", "10s")),
	}
	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      "auth-middleware",
	}, stack, func(middleware *traefik.Middleware) error {
		middleware.Spec = traefik.MiddlewareSpec{
			Plugin: m,
		}
		return nil
	})

	switch {
	case err != nil:
		stack.SetMiddlewareError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetMiddlewareReady()
	}

	log.FromContext(ctx).Info("Middleware ready")
	return nil
}

func (r *Mutator) reconcileAuth(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Auth")

	staticClients := append(configuration.Services.Auth.StaticClients, SliceFromMap(stack.Status.StaticAuthClients)...)
	staticClients = append(staticClients, stack.Spec.Auth.StaticClients...)
	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("auth"),
	}, stack, func(auth *componentsv1beta2.Auth) error {
		auth.Spec = componentsv1beta2.AuthSpec{
			Scalable: apisv1beta1.Scalable{
				Replicas: auth.Spec.Replicas,
			},
			Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Auth.Postgres,
					Database:       fmt.Sprintf("%s-auth", stack.Name),
				},
			},
			BaseURL: fmt.Sprintf("%s://%s/api/auth", stack.Spec.Scheme, stack.Spec.Host),
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version, //TODO
			},
			Ingress:             configuration.Services.Auth.Ingress.Compute(stack, configuration, "/api/auth"),
			DelegatedOIDCServer: stack.Spec.Auth.DelegatedOIDCServer,
			Monitoring:          configuration.Monitoring,
			StaticClients:       staticClients,
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetAuthError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetAuthReady()
	}

	log.FromContext(ctx).Info("Auth ready")
	return nil
}

func (r *Mutator) reconcileLedger(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Ledger")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("ledger"),
	}, stack, func(ledger *componentsv1beta2.Ledger) error {
		ledger.Spec = componentsv1beta2.LedgerSpec{
			Scalable: configuration.Services.Ledger.Scalable.WithReplicas(ledger.Spec.Replicas),
			Ingress:  configuration.Services.Ledger.Ingress.Compute(stack, configuration, "/api/ledger"),
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			LockingStrategy: configuration.Services.Ledger.LockingStrategy,
			Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
				PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
					Database:       fmt.Sprintf("%s-ledger", stack.Name),
					PostgresConfig: configuration.Services.Ledger.Postgres,
				},
				CreateDatabase: true,
			},
			Monitoring: configuration.Monitoring,
			Collector: &componentsv1beta1.CollectorConfig{
				KafkaConfig: configuration.Kafka,
				Topic:       fmt.Sprintf("%s-ledger", stack.Name),
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetLedgerError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetLedgerReady()
	}

	log.FromContext(ctx).Info("Ledger ready")
	return nil
}

func (r *Mutator) reconcilePayment(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Payment")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("payments"),
	}, stack, func(payment *componentsv1beta2.Payments) error {
		payment.Spec = componentsv1beta2.PaymentsSpec{
			Ingress: configuration.Services.Payments.Ingress.Compute(stack, configuration, "/api/payments"),
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			Monitoring: configuration.Monitoring,
			Collector: &componentsv1beta1.CollectorConfig{
				KafkaConfig: configuration.Kafka,
				Topic:       fmt.Sprintf("%s-payments", stack.Name),
			},
			Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Payments.Postgres,
					Database:       fmt.Sprintf("%s-payments", stack.Name),
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetPaymentError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetPaymentReady()
	}

	log.FromContext(ctx).Info("Payment ready")
	return nil
}

func (r *Mutator) reconcileWebhooks(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Webhooks")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("webhooks"),
	}, stack, func(webhooks *componentsv1beta2.Webhooks) error {
		webhooks.Spec = componentsv1beta2.WebhooksSpec{
			Ingress:    configuration.Services.Webhooks.Ingress.Compute(stack, configuration, "/api/webhooks"),
			Monitoring: configuration.Monitoring,
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			Collector: &componentsv1beta1.CollectorConfig{
				KafkaConfig: configuration.Kafka,
				Topic:       fmt.Sprintf("%s-payments %s-ledger", stack.Name, stack.Name),
			},
			Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Webhooks.Postgres,
					Database:       fmt.Sprintf("%s-webhooks", stack.Name),
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetWebhooksError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetWebhooksReady()
	}

	log.FromContext(ctx).Info("Webhooks ready")
	return nil
}

func (r *Mutator) reconcileWallets(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Wallets")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("wallets"),
	}, stack, func(wallets *componentsv1beta2.Wallets) error {
		wallets.Spec = componentsv1beta2.WalletsSpec{
			Ingress:    configuration.Services.Wallets.Ingress.Compute(stack, configuration, "/api/wallets"),
			Monitoring: configuration.Monitoring,
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Wallets.Postgres,
					Database:       fmt.Sprintf("%s-wallets", stack.Name),
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetWalletsError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetWalletsReady()
	}

	log.FromContext(ctx).Info("Wallets ready")
	return nil
}

func (r *Mutator) reconcileCounterparties(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Counterparties")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("counterparties"),
	}, stack, func(counterparties *componentsv1beta2.Counterparties) error {
		counterparties.Spec = componentsv1beta2.CounterpartiesSpec{
			Ingress:    configuration.Services.Counterparties.Ingress.Compute(stack, configuration, "/api/counterparties"),
			Monitoring: configuration.Monitoring,
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Counterparties.Postgres,
					Database:       fmt.Sprintf("%s-counterparties", stack.Name),
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetCounterpartiesError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetCounterpartiesReady()
	}

	log.FromContext(ctx).Info("Counterparties ready")
	return nil
}

func (r *Mutator) reconcileControl(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Control")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("control"),
	}, stack, func(control *componentsv1beta2.Control) error {
		control.Spec = componentsv1beta2.ControlSpec{
			Scalable: apisv1beta1.Scalable{
				Replicas: control.Spec.Replicas,
			},
			Ingress: configuration.Services.Control.Ingress.Compute(stack, configuration, "/"),
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			Monitoring:  configuration.Monitoring,
			ApiURLFront: fmt.Sprintf("%s/api", stack.URL()),
			ApiURLBack:  fmt.Sprintf("%s/api", stack.URL()),
			AuthClientConfiguration: &componentsv1beta2.AuthClientConfiguration{
				ClientID:     stack.Status.StaticAuthClients["control"].ID,
				ClientSecret: stack.Status.StaticAuthClients["control"].Secrets[0],
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetControlError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetControlReady()
	}

	log.FromContext(ctx).Info("Control ready")
	return nil
}

func (r *Mutator) reconcileSearch(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec, version string) error {
	log.FromContext(ctx).Info("Reconciling Search")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Name,
		Name:      stack.ServiceName("search"),
	}, stack, func(search *componentsv1beta2.Search) error {
		search.Spec = componentsv1beta2.SearchSpec{
			Scalable: apisv1beta1.Scalable{
				Replicas: search.Spec.Replicas,
			},
			Ingress:    configuration.Services.Search.Ingress.Compute(stack, configuration, "/api/search"),
			Monitoring: configuration.Monitoring,
			CommonServiceProperties: apisv1beta2.CommonServiceProperties{
				DevProperties: stack.Spec.DevProperties,
				Version:       version,
			},
			ElasticSearch: configuration.Services.Search.ElasticSearchConfig,
			KafkaConfig:   configuration.Kafka,
			Index:         stack.Name,
			Batching:      configuration.Services.Search.Batching,
			PostgresConfigs: componentsv1beta2.SearchPostgresConfigs{
				Ledger: apisv1beta1.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Ledger.Postgres,
					Database:       fmt.Sprintf("%s-ledger", stack.Name),
				},
			},
		}
		return nil
	})
	switch {
	case err != nil:
		stack.SetSearchError(err.Error())
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetSearchReady()
	}

	log.FromContext(ctx).Info("Search ready")
	return nil
}

var _ controllerutils.Mutator[*stackv1beta2.Stack] = &Mutator{}

func NewMutator(
	client client.Client,
	scheme *runtime.Scheme,
	dnsNames []string,
) controllerutils.Mutator[*stackv1beta2.Stack] {
	return &Mutator{
		client:   client,
		scheme:   scheme,
		dnsNames: dnsNames,
	}
}
