package stack

import (
	"context"
	"fmt"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/internal/resourceutil"
	pkgError "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/finalizers,verbs=update

type Mutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *Mutator) SetupWithBuilder(mgr ctrl.Manager, builder *ctrl.Builder) error {
	builder.
		Owns(&authcomponentsv1beta1.Auth{}).
		Owns(&authcomponentsv1beta1.Ledger{}).
		Owns(&authcomponentsv1beta1.Search{}).
		Owns(&authcomponentsv1beta1.Payments{}).
		Owns(&corev1.Namespace{})
	return nil
}

func (m *Mutator) Mutate(ctx context.Context, actual *v1beta1.Stack) (*ctrl.Result, error) {
	SetProgressing(actual)

	if err := m.reconcileNamespace(ctx, actual); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling namespace")
	}
	if err := m.reconcileAuth(ctx, actual); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling Auth")
	}
	if err := m.reconcileLedger(ctx, actual); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling Ledger")
	}
	if err := m.reconcilePayment(ctx, actual); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling Payment")
	}
	if err := m.reconcileSearch(ctx, actual); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling Search")
	}
	if err := m.reconcileControl(ctx, actual); err != nil {
		return Requeue(), pkgError.Wrap(err, "Reconciling Control")
	}

	SetReady(actual)
	return nil, nil
}

func (r *Mutator) reconcileNamespace(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Namespace")

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
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

func (r *Mutator) reconcileAuth(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Auth")

	if stack.Spec.Auth == nil {
		log.FromContext(ctx).Info("Deleting Auth")
		err := r.client.Delete(ctx, &authcomponentsv1beta1.Auth{
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

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("auth"),
	}, stack, func(auth *authcomponentsv1beta1.Auth) error {
		auth.Spec = authcomponentsv1beta1.AuthSpec{
			ImageHolder: stack.Spec.Auth.ImageHolder,
			Postgres: authcomponentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfigWithDatabase: PostgresConfigWithDatabase{
					PostgresConfig: stack.Spec.Auth.PostgresConfig,
					Database:       fmt.Sprintf("%s-auth", stack.Name),
				},
			},
			BaseURL:             fmt.Sprintf("%s://%s/api/auth", stack.Spec.Auth.GetScheme(), stack.Spec.Auth.Host),
			SigningKey:          stack.Spec.Auth.SigningKey,
			DevMode:             stack.Spec.Debug || stack.Spec.Auth.Debug,
			Ingress:             stack.Spec.Auth.Ingress.Compute(stack, "/api/auth"),
			DelegatedOIDCServer: stack.Spec.Auth.DelegatedOIDCServer,
			Monitoring:          stack.Spec.Monitoring,
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

func (r *Mutator) reconcileLedger(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Ledger")

	if stack.Spec.Services.Ledger == nil {
		log.FromContext(ctx).Info("Deleting Ledger")
		err := r.client.Delete(ctx, &authcomponentsv1beta1.Ledger{
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

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("ledger"),
	}, stack, func(ledger *authcomponentsv1beta1.Ledger) error {
		var collector *authcomponentsv1beta1.CollectorConfig
		if stack.Spec.Kafka != nil {
			collector = &authcomponentsv1beta1.CollectorConfig{
				KafkaConfig: *stack.Spec.Kafka,
				Topic:       fmt.Sprintf("%s-ledger", stack.Name),
			}
		}
		ledger.Spec = authcomponentsv1beta1.LedgerSpec{
			Ingress: stack.Spec.Services.Ledger.Ingress.Compute(stack, "/api/ledger"),
			Debug:   stack.Spec.Debug || stack.Spec.Services.Ledger.Debug,
			Redis:   stack.Spec.Services.Ledger.Redis,
			Postgres: authcomponentsv1beta1.PostgresConfigCreateDatabase{
				PostgresConfigWithDatabase: PostgresConfigWithDatabase{
					Database:       fmt.Sprintf("%s-ledger", stack.Name),
					PostgresConfig: stack.Spec.Services.Ledger.Postgres,
				},
				CreateDatabase: true,
			},
			Monitoring:         stack.Spec.Monitoring,
			ImageHolder:        stack.Spec.Services.Ledger.ImageHolder,
			Collector:          collector,
			ElasticSearchIndex: stack.Name,
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

func (r *Mutator) reconcilePayment(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Payment")

	if stack.Spec.Services.Payments == nil {
		log.FromContext(ctx).Info("Deleting Payments")
		err := r.client.Delete(ctx, &authcomponentsv1beta1.Payments{
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
			stack.RemoveAuthStatus()
		}
		return nil
	}

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("payments"),
	}, stack, func(payment *authcomponentsv1beta1.Payments) error {
		var collector *authcomponentsv1beta1.CollectorConfig
		if stack.Spec.Kafka != nil {
			collector = &authcomponentsv1beta1.CollectorConfig{
				KafkaConfig: *stack.Spec.Kafka,
				Topic:       fmt.Sprintf("%s-payments", stack.Name),
			}
		}
		payment.Spec = authcomponentsv1beta1.PaymentsSpec{
			Ingress:            stack.Spec.Services.Payments.Ingress.Compute(stack, "/api/payments"),
			Debug:              stack.Spec.Debug || stack.Spec.Services.Payments.Debug,
			Monitoring:         stack.Spec.Monitoring,
			ImageHolder:        stack.Spec.Services.Payments.ImageHolder,
			Collector:          collector,
			ElasticSearchIndex: stack.Name,
			MongoDB: authcomponentsv1beta1.MongoDBConfig{
				UseSrv:   stack.Spec.Services.Payments.MongoDB.UseSrv,
				Host:     stack.Spec.Services.Payments.MongoDB.Host,
				Port:     stack.Spec.Services.Payments.MongoDB.Port,
				Database: stack.Name,
				Username: stack.Spec.Services.Payments.MongoDB.Username,
				Password: stack.Spec.Services.Payments.MongoDB.Password,
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

func (r *Mutator) reconcileControl(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Control")

	if stack.Spec.Services.Control == nil {
		log.FromContext(ctx).Info("Deleting Control")
		err := r.client.Delete(ctx, &authcomponentsv1beta1.Control{
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

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("control"),
	}, stack, func(control *authcomponentsv1beta1.Control) error {
		control.Spec = authcomponentsv1beta1.ControlSpec{
			Ingress:     stack.Spec.Services.Control.Ingress.Compute(stack, "/"),
			Debug:       stack.Spec.Debug || stack.Spec.Services.Control.Debug,
			ImageHolder: stack.Spec.Services.Control.ImageHolder,
			ApiURLFront: fmt.Sprintf("%s://%s/api", stack.GetScheme(), stack.Spec.Ingress.Host),
			ApiURLBack:  fmt.Sprintf("%s://%s/api", stack.GetScheme(), stack.Spec.Ingress.Host),
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

func (r *Mutator) reconcileSearch(ctx context.Context, stack *v1beta1.Stack) error {
	log.FromContext(ctx).Info("Reconciling Search")

	if stack.Spec.Services.Search == nil {
		log.FromContext(ctx).Info("Deleting Search")
		err := r.client.Delete(ctx, &authcomponentsv1beta1.Search{
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

	if stack.Spec.Kafka == nil {
		return pkgError.New("collector must be configured to use search service")
	}

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("search"),
	}, stack, func(search *authcomponentsv1beta1.Search) error {
		search.Spec = authcomponentsv1beta1.SearchSpec{
			Ingress:       stack.Spec.Services.Search.Ingress.Compute(stack, "/api/search"),
			Debug:         stack.Spec.Debug || stack.Spec.Services.Search.Debug,
			Auth:          nil,
			Monitoring:    stack.Spec.Monitoring,
			ImageHolder:   stack.Spec.Services.Search.ImageHolder,
			ElasticSearch: *stack.Spec.Services.Search.ElasticSearchConfig,
			KafkaConfig:   *stack.Spec.Kafka,
			Index:         stack.Name,
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

var _ internal.Mutator[*v1beta1.Stack] = &Mutator{}

func NewMutator(
	client client.Client,
	scheme *runtime.Scheme,
) internal.Mutator[*v1beta1.Stack] {
	return &Mutator{
		client: client,
		scheme: scheme,
	}
}
