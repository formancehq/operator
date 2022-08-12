package stack

import (
	"context"
	"fmt"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	"github.com/numary/formance-operator/internal"
	"github.com/numary/formance-operator/pkg/resourceutil"
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

type Mutator struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m Mutator) SetupWithBuilder(builder *ctrl.Builder) {
	builder.
		Owns(&authcomponentsv1beta1.Auth{}).
		Owns(&corev1.Namespace{})
}

func (m Mutator) Mutate(ctx context.Context, actual *v1beta1.Stack) (*ctrl.Result, error) {
	actual.Progress()

	if err := m.reconcileNamespace(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling namespace")
	}
	if err := m.reconcileAuth(ctx, actual); err != nil {
		return nil, pkgError.Wrap(err, "Reconciling Auth")
	}

	actual.SetReady()
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
		err := r.client.Delete(ctx, &authcomponentsv1beta1.Auth{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Spec.Namespace,
				Name:      stack.Spec.Auth.Name(stack),
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
		Name:      stack.Spec.Auth.Name(stack),
	}, stack, func(ns *authcomponentsv1beta1.Auth) error {
		var ingress *authcomponentsv1beta1.IngressSpec
		if stack.Spec.Ingress != nil {
			ingress = &authcomponentsv1beta1.IngressSpec{
				Path:        "/auth",
				Host:        stack.Spec.Host,
				Annotations: stack.Spec.Ingress.Annotations,
			}
		}
		ns.Spec = authcomponentsv1beta1.AuthSpec{
			Image:               stack.Spec.Auth.Image,
			Postgres:            stack.Spec.Auth.PostgresConfig,
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
		stack.SetAuthCreated()
	}

	log.FromContext(ctx).Info("Auth ready")
	return nil
}

var _ internal.Mutator[v1beta1.StackCondition, *v1beta1.Stack] = &Mutator{}

func NewMutator(
	client client.Client,
	scheme *runtime.Scheme,
) internal.Mutator[v1beta1.StackCondition, *v1beta1.Stack] {
	return &Mutator{
		client: client,
		scheme: scheme,
	}
}
