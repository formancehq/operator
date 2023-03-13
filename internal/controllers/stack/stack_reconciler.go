package stack

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/formancehq/operator/internal/collectionutils"
	"github.com/formancehq/operator/internal/controllerutils"
	"github.com/formancehq/operator/internal/modules"
	traefik "github.com/traefik/traefik/v2/pkg/provider/kubernetes/crd/traefik/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	stackv1beta3 "github.com/formancehq/operator/apis/stack/v1beta3"
	pkgError "github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	_ "github.com/formancehq/operator/internal/handlers"
)

const (
	DefaultVersions = "default"
)

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=traefik.containo.us,resources=middlewares,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=stack.formance.com,resources=stacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=stack.formance.com,resources=configurations,verbs=get;list;watch
// +kubebuilder:rbac:groups=stack.formance.com,resources=versions,verbs=get;list;watch

// Reconciler reconciles a Stack object
type Reconciler struct {
	// Cloud region where the stack is deployed
	region string
	// Cloud environment where the stack is deployed: staging, production,
	// sandbox, etc.
	environment string
	client      client.Client
	scheme      *runtime.Scheme
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	log := log.FromContext(ctx, "stack", req.NamespacedName)
	log.Info("Starting reconciliation")

	stack := &stackv1beta3.Stack{}
	if err := r.client.Get(ctx, req.NamespacedName, stack); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Object not found, skip")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, pkgError.Wrap(err, "Reading target")
	}
	stack.SetProgressing()

	var (
		reconcileError error
	)
	func() {
		defer func() {
			if reconcileError != nil {
				log.Info("reconciliation terminated with error", "error", reconcileError)
				stack.SetError(reconcileError)
			} else {
				log.Info("reconciliation terminated with success")
				stack.SetReady()
			}
		}()
		defer func() {
			if e := recover(); e != nil {
				reconcileError = fmt.Errorf("%s", e)
				fmt.Println(reconcileError)
				debug.PrintStack()
			}
		}()

		reconcileError = r.reconcileStack(ctx, stack)
	}()

	if reconcileError != nil {
		log.Info("reconcile failed with error", "error", reconcileError)
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second,
		}, nil
	}

	if patchErr := r.client.Status().Update(ctx, stack); patchErr != nil {
		log.Info("unable to update status", "error", patchErr)
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second,
		}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &stackv1beta3.Stack{}, ".spec.seed", func(rawObj client.Object) []string {
			return []string{rawObj.(*stackv1beta3.Stack).Spec.Seed}
		}); err != nil {
		return err
	}
	if err := mgr.GetFieldIndexer().
		IndexField(context.Background(), &stackv1beta3.Stack{}, ".spec.versions", func(rawObj client.Object) []string {
			return []string{rawObj.(*stackv1beta3.Stack).Spec.Versions}
		}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&stackv1beta3.Stack{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&appsv1.Deployment{}).
		Watches(
			&source.Kind{Type: &stackv1beta3.Configuration{}},
			watch(mgr, ".spec.seed"),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&source.Kind{Type: &stackv1beta3.Versions{}},
			watch(mgr, ".spec.versions"),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *Reconciler) reconcileStack(ctx context.Context, stack *stackv1beta3.Stack) error {

	configuration := &stackv1beta3.Configuration{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Name: stack.Spec.Seed,
	}, configuration); err != nil {
		if errors.IsNotFound(err) {
			return pkgError.New("Configuration object not found")
		}
		return fmt.Errorf("error retrieving Configuration object: %s", err)
	}

	if err := configuration.Validate(); err != nil {
		return err
	}

	versionsString := stack.Spec.Versions
	if versionsString == "" {
		versionsString = DefaultVersions
	}

	versions := &stackv1beta3.Versions{}
	if err := r.client.Get(ctx, types.NamespacedName{Name: versionsString}, versions); err != nil {
		if errors.IsNotFound(err) {
			return pkgError.New("Versions object not found")
		}
		return fmt.Errorf("error retrieving Versions object: %s", err)
	}

	_, _, err := controllerutils.CreateOrUpdate(ctx, r.client, types.NamespacedName{
		Name: stack.Name,
	}, controllerutils.WithController[*corev1.Namespace](stack, r.scheme), func(ns *corev1.Namespace) {})
	if err != nil {
		return err
	}

	_, _, err = controllerutils.CreateOrUpdate(ctx, r.client, types.NamespacedName{
		Name:      "auth-middleware",
		Namespace: stack.Name,
	}, controllerutils.WithController[*traefik.Middleware](stack, r.scheme), func(t *traefik.Middleware) {
		t.Spec.Plugin = map[string]apiextensionv1.JSON{
			"auth": {
				Raw: []byte(fmt.Sprintf(`{"Issuer": "%s", "RefreshTime": "%s", "ExcludePaths": ["/_health", "/_healthcheck", "/.well-known/openid-configuration"]}`, stack.Spec.Scheme+"://"+stack.Spec.Host+"/api/auth", "10s")),
			},
		}
	})
	if err != nil {
		return err
	}

	deployer := modules.NewDeployer(r.client, r.scheme, stack, configuration)
	resolveContext := modules.Context{
		Context:       ctx,
		Region:        r.region,
		Environment:   r.environment,
		Stack:         stack,
		Configuration: configuration,
		Versions:      versions,
	}

	return modules.HandleStack(resolveContext, deployer)
}

func NewReconciler(client client.Client, scheme *runtime.Scheme, region, environment string) *Reconciler {
	return &Reconciler{
		region:      region,
		environment: environment,
		client:      client,
		scheme:      scheme,
	}
}

func watch(mgr ctrl.Manager, field string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(object client.Object) []reconcile.Request {
		stacks := &stackv1beta3.StackList{}
		listOps := &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(field, object.GetName()),
			Namespace:     object.GetNamespace(),
		}
		err := mgr.GetClient().List(context.TODO(), stacks, listOps)
		if err != nil {
			return []reconcile.Request{}
		}

		return collectionutils.Map(stacks.Items, func(s stackv1beta3.Stack) reconcile.Request {
			return reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      s.GetName(),
					Namespace: s.GetNamespace(),
				},
			}
		})
	})
}