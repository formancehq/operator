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

func (m *Mutator) SetupWithBuilder(builder *ctrl.Builder) {
	builder.
		Owns(&authcomponentsv1beta1.Auth{}).
		Owns(&corev1.Namespace{})
}

func (m *Mutator) Mutate(ctx context.Context, actual *v1beta1.Stack) (*ctrl.Result, error) {
	SetProgressing(actual)

	if err := m.reconcileNamespace(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling namespace")
	}
	if err := m.reconcileAuth(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling Auth")
	}
	if err := m.reconcileLedger(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling Ledger")
	}
	if err := m.reconcileSearch(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling Search")
	}
	if err := m.reconcileControl(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling Control")
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
	}, stack, func(ns *authcomponentsv1beta1.Auth) error {
		var ingress *IngressSpec
		if stack.Spec.Ingress != nil {
			ingress = &IngressSpec{
				Path:        "/auth",
				Host:        stack.Spec.Host,
				Annotations: stack.Spec.Ingress.Annotations,
			}
		}
		ns.Spec = authcomponentsv1beta1.AuthSpec{
			Image: stack.Spec.Auth.Image,
			Postgres: authcomponentsv1beta1.PostgresConfigCreateDatabase{
				CreateDatabase: true,
				PostgresConfig: stack.Spec.Auth.PostgresConfig,
			},
			BaseURL:             fmt.Sprintf("%s://%s/auth", stack.Scheme(), stack.Spec.Host),
			SigningKey:          stack.Spec.Auth.SigningKey,
			DevMode:             stack.Spec.Debug,
			Ingress:             ingress,
			DelegatedOIDCServer: stack.Spec.Auth.DelegatedOIDCServer,
			Monitoring:          stack.Spec.Monitoring,
		}
		return nil
	})
	switch {
	case err != nil:
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
		var ingress *IngressSpec
		if stack.Spec.Ingress != nil {
			ingress = &IngressSpec{
				Path:        "/ledger",
				Host:        stack.Spec.Host,
				Annotations: stack.Spec.Ingress.Annotations,
			}
		}
		var authConfig *AuthConfigSpec
		// TODO: Reconfigure properly when the gateway will be in place
		//if stack.Spec.Auth != nil {
		//	authConfig = &sharedtypes.AuthConfigSpec{
		//		OAuth2: &sharedtypes.OAuth2ConfigSpec{
		//			//TODO: Not hardcode port
		//			// TODO: Discover on operator, or discover on ledger
		//			IntrospectUrl: fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/oauth/introspect", stack.ServiceName("auth"), stack.Spec.Namespace),
		//			Audiences: []string{
		//				fmt.Sprintf("%s://%s", stack.Spec.Scheme, stack.Spec.Host),
		//			},
		//			ProtectedByScopes: false, // TODO: Maybe later...
		//		},
		//	}
		//}
		ledger.Spec = authcomponentsv1beta1.LedgerSpec{
			Ingress:    ingress,
			Debug:      stack.Spec.Services.Ledger.Debug,
			Redis:      stack.Spec.Services.Ledger.Redis,
			Postgres:   stack.Spec.Services.Ledger.Postgres,
			Auth:       authConfig,
			Monitoring: stack.Spec.Monitoring,
			Image:      stack.Spec.Services.Ledger.Image,
			Collector:  stack.Spec.Collector,
		}
		return nil
	})
	switch {
	case err != nil:
		return err
	case operationResult == controllerutil.OperationResultNone:
	default:
		stack.SetLedgerReady()
	}

	log.FromContext(ctx).Info("Ledger ready")
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
		var ingress *IngressSpec
		if stack.Spec.Ingress != nil {
			ingress = &IngressSpec{
				Path:        "/",
				Host:        "localhost", //stack.Spec.Host,
				Annotations: stack.Spec.Ingress.Annotations,
			}
		}
		control.Spec = authcomponentsv1beta1.ControlSpec{
			Ingress: ingress,
			Debug:   stack.Spec.Services.Control.Debug,
			Image:   stack.Spec.Services.Control.Image,
		}
		return nil
	})
	switch {
	case err != nil:
		// TODO: Miss condition
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

	if stack.Spec.Collector == nil {
		return pkgError.New("collector must be configured to use search service")
	}

	_, operationResult, err := resourceutil.CreateOrUpdateWithController(ctx, r.client, r.scheme, types.NamespacedName{
		Namespace: stack.Spec.Namespace,
		Name:      stack.ServiceName("search"),
	}, stack, func(search *authcomponentsv1beta1.Search) error {
		var ingress *IngressSpec
		if stack.Spec.Ingress != nil {
			ingress = &IngressSpec{
				Path:        "/search",
				Host:        stack.Spec.Host,
				Annotations: stack.Spec.Ingress.Annotations,
			}
		}
		search.Spec = authcomponentsv1beta1.SearchSpec{
			Ingress:       ingress,
			Debug:         stack.Spec.Services.Search.Debug,
			Auth:          nil,
			Monitoring:    stack.Spec.Monitoring,
			Image:         stack.Spec.Services.Search.Image,
			ElasticSearch: *stack.Spec.Services.Search.ElasticSearchConfig,
			KafkaConfig:   *stack.Spec.Collector.KafkaConfig,
		}
		return nil
	})
	switch {
	case err != nil:
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
