package stack

import (
	"context"
	"fmt"

	authcomponentsv1beta1 "github.com/numary/operator/apis/auth.components/v1beta1"
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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type Mutator struct {
	client   client.Client
	scheme   *runtime.Scheme
	dnsNames []string
}

func (r *Mutator) SetupWithBuilder(mgr ctrl.Manager, bldr *ctrl.Builder) error {

	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &stackv1beta2.Stack{}, ".spec.configuration", func(rawObj client.Object) []string {
			return []string{rawObj.(*stackv1beta2.Stack).Spec.Seed}
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
			&source.Kind{
				Type: &stackv1beta2.Configuration{},
			}, handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
				stacks := &stackv1beta2.StackList{}
				listOps := &client.ListOptions{
					FieldSelector: fields.OneTermEqualSelector(".spec.configuration", object.GetName()),
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
			}),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		)
	return nil
}

func (r *Mutator) Mutate(ctx context.Context, stack *stackv1beta2.Stack) (*ctrl.Result, error) {
	apisv1beta2.SetProgressing(stack)

	configuration := &stackv1beta2.Configuration{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name: stack.Spec.Seed,
	}, configuration); err != nil {
		if errors.IsNotFound(err) {
			return nil, pkgError.New("Configuration object not found")
		}
		return controllerutils.Requeue(), fmt.Errorf("error retrieving configuration object: %s", err)
	}

	configurationSpec := configuration.Spec
	// TODO: Reuse standard validation
	if err := configurationSpec.Validate(); len(err) > 0 {
		return nil, pkgError.Wrap(err.ToAggregate(), "Validating configuration")
	}

	// Add static clients for app needing it (Actually, control)
	if configuration.Spec.Services.Auth != nil {
		if stack.Status.StaticAuthClients == nil {
			stack.Status.StaticAuthClients = map[string]authcomponentsv1beta1.StaticClient{}
		}
		if configuration.Spec.Services.Control != nil {
			stack.Status.StaticAuthClients["control"] = authcomponentsv1beta1.StaticClient{
				ID: "control",
				Secrets: []string{
					// TODO: Need a real secret
					"control",
					// When creating Control CRD later in the code, we trigger a new reconciliation loop on the stack as
					// the Stack object owns Control object, which invalid the current reconciliation (we cannot update the status as the generation as changed).
					//uuid.NewString(),
				},
				ClientConfiguration: authcomponentsv1beta1.ClientConfiguration{
					Scopes: []string{
						"openid",
						"profile",
						"email",
						"offline",
					},
					RedirectUris: []string{
						fmt.Sprintf("%s/auth/login", stack.URL()),
					},
					PostLogoutRedirectUris: []string{
						fmt.Sprintf("%s/auth/destroy", stack.URL()),
					},
				},
			}
		}
	}

	if err := r.reconcileNamespace(ctx, stack); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling namespace")
	}
	if err := r.reconcileMiddleware(ctx, stack); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling middleware")
	}
	if err := r.reconcileAuth(ctx, stack, &configurationSpec); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Auth")
	}
	if err := r.reconcileLedger(ctx, stack, &configurationSpec); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Ledger")
	}
	if err := r.reconcilePayment(ctx, stack, &configurationSpec); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Payment")
	}
	if err := r.reconcileSearch(ctx, stack, &configurationSpec); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Search")
	}
	if err := r.reconcileControl(ctx, stack, &configurationSpec); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Control")
	}
	if err := r.reconcileWebhooks(ctx, stack, &configurationSpec); err != nil {
		return controllerutils.Requeue(), pkgError.Wrap(err, "Reconciling Webhooks")
	}

	apisv1beta2.SetReady(stack)
	return nil, nil
}

func (r *Mutator) reconcileNamespace(ctx context.Context, stack *stackv1beta2.Stack) error {
	log.FromContext(ctx).Info("Reconciling Namespace")

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Name: stack.Spec.Namespace,
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
		Raw: []byte(fmt.Sprintf(`{"Issuer": "%s", "RefreshTime": "%s"}`, stack.Spec.Scheme+"://"+stack.Spec.Host+"/api/auth", "10s")),
	}
	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
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

func (r *Mutator) reconcileAuth(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec) error {
	log.FromContext(ctx).Info("Reconciling Auth")

	if configuration.Services.Auth == nil {
		log.FromContext(ctx).Info("Deleting Auth")
		err := r.client.Delete(ctx, &componentsv1beta2.Auth{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.ServiceName("auth"),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Auth")
		default:
			stack.RemoveAuthStatus()
		}
		return nil
	}

	staticClients := append(configuration.Services.Auth.StaticClients, SliceFromMap(stack.Status.StaticAuthClients)...)
	staticClients = append(staticClients, stack.Spec.Auth.StaticClients...)
	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("auth"),
	}, stack, func(auth *componentsv1beta2.Auth) error {
		auth.Spec = componentsv1beta2.AuthSpec{
			ImageHolder: configuration.Services.Auth.ImageHolder,
			Scalable: apisv1beta2.Scalable{
				Replicas: auth.Spec.Replicas,
			},
			Postgres: componentsv1beta2.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: apisv1beta2.PostgresConfigWithDatabase{
					PostgresConfig: configuration.Services.Auth.Postgres,
					Database:       fmt.Sprintf("%s-auth", stack.Name),
				},
			},
			BaseURL:             fmt.Sprintf("%s://%s/api/auth", stack.Spec.Scheme, stack.Spec.Host),
			DevProperties:       stack.Spec.DevProperties,
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

func (r *Mutator) reconcileLedger(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec) error {
	log.FromContext(ctx).Info("Reconciling Ledger")

	if configuration.Services.Ledger == nil {
		log.FromContext(ctx).Info("Deleting Ledger")
		err := r.client.Delete(ctx, &componentsv1beta2.Ledger{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.ServiceName("ledger"),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Ledger")
		default:
			stack.RemoveAuthStatus()
		}
		return nil
	}

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("ledger"),
	}, stack, func(ledger *componentsv1beta2.Ledger) error {
		var collector *componentsv1beta2.CollectorConfig
		if configuration.Kafka != nil {
			collector = &componentsv1beta2.CollectorConfig{
				KafkaConfig: *configuration.Kafka,
				Topic:       fmt.Sprintf("%s-ledger", stack.Name),
			}
		}
		ledger.Spec = componentsv1beta2.LedgerSpec{
			Scalable:        configuration.Services.Ledger.Scalable.WithReplicas(ledger.Spec.Replicas),
			ImageHolder:     configuration.Services.Ledger.ImageHolder,
			Ingress:         configuration.Services.Ledger.Ingress.Compute(stack, configuration, "/api/ledger"),
			DevProperties:   stack.Spec.DevProperties,
			LockingStrategy: configuration.Services.Ledger.LockingStrategy,
			Postgres: componentsv1beta2.PostgresConfigCreateDatabase{
				PostgresConfigWithDatabase: apisv1beta2.PostgresConfigWithDatabase{
					Database:       fmt.Sprintf("%s-ledger", stack.Name),
					PostgresConfig: configuration.Services.Ledger.Postgres,
				},
				CreateDatabase: true,
			},
			Monitoring: configuration.Monitoring,
			Collector:  collector,
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

func (r *Mutator) reconcilePayment(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec) error {
	log.FromContext(ctx).Info("Reconciling Payment")

	if configuration.Services.Payments == nil {
		log.FromContext(ctx).Info("Deleting Payments")
		err := r.client.Delete(ctx, &componentsv1beta2.Payments{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.ServiceName("payments"),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Payments")
		default:
			stack.RemovePaymentsStatus()
		}
		return nil
	}

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("payments"),
	}, stack, func(payment *componentsv1beta2.Payments) error {
		var collector *componentsv1beta2.CollectorConfig
		if configuration.Kafka != nil {
			collector = &componentsv1beta2.CollectorConfig{
				KafkaConfig: *configuration.Kafka,
				Topic:       fmt.Sprintf("%s-payments", stack.Name),
			}
		}
		payment.Spec = componentsv1beta2.PaymentsSpec{
			Ingress:       configuration.Services.Payments.Ingress.Compute(stack, configuration, "/api/payments"),
			DevProperties: stack.Spec.DevProperties,
			Monitoring:    configuration.Monitoring,
			ImageHolder:   configuration.Services.Payments.ImageHolder,
			Collector:     collector,
			MongoDB: apisv1beta2.MongoDBConfig{
				UseSrv:       configuration.Services.Payments.MongoDB.UseSrv,
				Host:         configuration.Services.Payments.MongoDB.Host,
				HostFrom:     configuration.Services.Payments.MongoDB.HostFrom,
				Port:         configuration.Services.Payments.MongoDB.Port,
				PortFrom:     configuration.Services.Payments.MongoDB.PortFrom,
				Database:     stack.Name,
				Username:     configuration.Services.Payments.MongoDB.Username,
				UsernameFrom: configuration.Services.Payments.MongoDB.UsernameFrom,
				Password:     configuration.Services.Payments.MongoDB.Password,
				PasswordFrom: configuration.Services.Payments.MongoDB.PasswordFrom,
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

func (r *Mutator) reconcileWebhooks(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec) error {
	log.FromContext(ctx).Info("Reconciling Webhooks")

	if configuration.Services.Webhooks == nil {
		log.FromContext(ctx).Info("Deleting Webhooks")
		err := r.client.Delete(ctx, &componentsv1beta2.Webhooks{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.ServiceName("webhooks"),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Webhooks")
		default:
			stack.RemoveWebhooksStatus()
		}
		return nil
	}

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("webhooks"),
	}, stack, func(webhooks *componentsv1beta2.Webhooks) error {
		var collector *componentsv1beta2.CollectorConfig
		if configuration.Kafka != nil {
			collector = &componentsv1beta2.CollectorConfig{
				KafkaConfig: *configuration.Kafka,
				Topic:       fmt.Sprintf("%s-payments,%s-ledger", stack.Name, stack.Name),
			}
		}
		webhooks.Spec = componentsv1beta2.WebhooksSpec{
			Ingress:       configuration.Services.Webhooks.Ingress.Compute(stack, configuration, "/api/webhooks"),
			DevProperties: stack.Spec.DevProperties,
			Monitoring:    configuration.Monitoring,
			ImageHolder:   configuration.Services.Webhooks.ImageHolder,
			Collector:     collector,
			MongoDB: apisv1beta2.MongoDBConfig{
				UseSrv:       configuration.Services.Webhooks.MongoDB.UseSrv,
				Host:         configuration.Services.Webhooks.MongoDB.Host,
				HostFrom:     configuration.Services.Webhooks.MongoDB.HostFrom,
				Port:         configuration.Services.Webhooks.MongoDB.Port,
				PortFrom:     configuration.Services.Webhooks.MongoDB.PortFrom,
				Database:     stack.Name,
				Username:     configuration.Services.Webhooks.MongoDB.Username,
				UsernameFrom: configuration.Services.Webhooks.MongoDB.UsernameFrom,
				Password:     configuration.Services.Webhooks.MongoDB.Password,
				PasswordFrom: configuration.Services.Webhooks.MongoDB.PasswordFrom,
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

func (r *Mutator) reconcileControl(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec) error {
	log.FromContext(ctx).Info("Reconciling Control")

	if configuration.Services.Control == nil {
		log.FromContext(ctx).Info("Deleting Control")
		err := r.client.Delete(ctx, &componentsv1beta2.Control{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.ServiceName("control"),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Control")
		default:
			stack.RemoveControlStatus()
		}
		return nil
	}

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("control"),
	}, stack, func(control *componentsv1beta2.Control) error {
		control.Spec = componentsv1beta2.ControlSpec{
			Scalable: apisv1beta2.Scalable{
				Replicas: control.Spec.Replicas,
			},
			Ingress:       configuration.Services.Control.Ingress.Compute(stack, configuration, "/"),
			DevProperties: stack.Spec.DevProperties,
			ImageHolder:   configuration.Services.Control.ImageHolder,
			Monitoring:    configuration.Monitoring,
			ApiURLFront:   fmt.Sprintf("%s/api", stack.URL()),
			ApiURLBack:    fmt.Sprintf("%s/api", stack.URL()),
			AuthClientConfiguration: func() *componentsv1beta2.AuthClientConfiguration {
				if configuration.Services.Auth == nil {
					return nil
				}
				return &componentsv1beta2.AuthClientConfiguration{
					ClientID:     stack.Status.StaticAuthClients["control"].ID,
					ClientSecret: stack.Status.StaticAuthClients["control"].Secrets[0],
				}
			}(),
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

func (r *Mutator) reconcileSearch(ctx context.Context, stack *stackv1beta2.Stack, configuration *stackv1beta2.ConfigurationSpec) error {
	log.FromContext(ctx).Info("Reconciling Search")

	if configuration.Services.Search == nil {
		log.FromContext(ctx).Info("Deleting Search")
		err := r.client.Delete(ctx, &componentsv1beta2.Search{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.ServiceName("search"),
			},
		})
		switch {
		case errors.IsNotFound(err):
		case err != nil:
			return pkgError.Wrap(err, "Deleting Search")
		default:
			stack.RemoveSearchStatus()
		}
		return nil
	}

	if configuration.Kafka == nil {
		return pkgError.New("collector must be configured to use search service")
	}

	_, operationResult, err := controllerutils.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("search"),
	}, stack, func(search *componentsv1beta2.Search) error {
		search.Spec = componentsv1beta2.SearchSpec{
			Scalable: apisv1beta2.Scalable{
				Replicas: search.Spec.Replicas,
			},
			Ingress:       configuration.Services.Search.Ingress.Compute(stack, configuration, "/api/search"),
			Debug:         stack.Spec.Debug,
			Auth:          nil,
			Monitoring:    configuration.Monitoring,
			ImageHolder:   configuration.Services.Search.ImageHolder,
			ElasticSearch: *configuration.Services.Search.ElasticSearchConfig,
			KafkaConfig:   *configuration.Kafka,
			Index:         stack.Name,
			Batching:      configuration.Services.Search.Batching,
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
