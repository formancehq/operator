package stacks

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/formancehq/go-libs/v2/collectionutils"
	"github.com/pkg/errors"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=formance.com,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=formance.com,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=formance.com,resources=stacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=formance.com,resources=versions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=formance.com,resources=versions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=formance.com,resources=versions/finalizers,verbs=update

var (
	ModuleReconciliation = "ModuleReconciliation"
)

func setModulesCondition(ctx Context, stack *v1beta1.Stack) error {
	for _, rtype := range ctx.GetScheme().AllKnownTypes() {
		v := reflect.New(rtype).Interface()
		r, ok := v.(v1beta1.Module)
		if !ok {
			continue
		}

		gvk, err := apiutil.GVKForObject(r, ctx.GetScheme())
		if err != nil {
			return err
		}
		l := &unstructured.UnstructuredList{}
		l.SetGroupVersionKind(gvk)
		if err := ctx.GetClient().List(ctx, l, client.MatchingFields{
			"stack": stack.Name,
		}); err != nil {
			return err
		}

		switch len(l.Items) {
		case 0:
			stack.GetConditions().Delete(v1beta1.ConditionPredicate(func(condition v1beta1.Condition) bool {
				return condition.Type == ModuleReconciliation && condition.Reason == gvk.Kind && condition.ObservedGeneration == stack.Generation
			}))
			continue
		case 1:
		default:
			return errors.New("multiple modules found")
		}

		func() {
			condition := v1beta1.NewCondition(ModuleReconciliation, stack.Generation).
				SetReason(gvk.Kind)
			defer func() {

				stack.GetConditions().AppendOrReplace(*condition, v1beta1.AndConditions(
					v1beta1.ConditionTypeMatch(ModuleReconciliation),
					v1beta1.ConditionReasonMatch(gvk.Kind),
				))
			}()
			type AnyModule struct {
				Meta   metav1.ObjectMeta `json:"metadata"`
				Status v1beta1.Status    `json:"status"`
			}

			module := AnyModule{}
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(l.Items[0].UnstructuredContent(), &module); err != nil {
				panic(err)
			}

			stackReconcileCondition := module.Status.Conditions.Get("ReconciledWithStack")
			if stackReconcileCondition == nil {
				condition.SetStatus(metav1.ConditionFalse).SetMessage("Module not yet reconciled")
				return
			}
			if stackReconcileCondition.Status != metav1.ConditionTrue {
				condition.SetStatus(metav1.ConditionFalse).SetMessage("Module not declared as reconciled for stack")
				return
			}
			if stackReconcileCondition.Reason == "Spec" && stack.MustSkip() {
				condition.SetStatus(metav1.ConditionFalse).SetMessage("Module should be skipped but is not")
				return
			}
			if stackReconcileCondition.Reason == "Skipped" && !stack.MustSkip() {
				condition.SetStatus(metav1.ConditionFalse).SetMessage("Module is skipped but should not")
				return
			}
			condition.SetMessage("All checks passed")

		}()
	}

	modules := make([]string, 0)
	pendingModules := make([]string, 0)
	for _, condition := range stack.Status.Conditions {
		if condition.Type != ModuleReconciliation || condition.ObservedGeneration != stack.Generation {
			continue
		}
		modules = append(modules, condition.Reason)
		if condition.Status == metav1.ConditionFalse {
			pendingModules = append(pendingModules, condition.Reason)
		}
	}

	sort.Strings(modules)
	stack.Status.Modules = modules
	if len(pendingModules) > 0 {
		return NewPendingError().WithMessage("Pending modules: %s", modules)
	}

	return nil
}

func namespaceLabel(ctx Context, stack string) func(ns *corev1.Namespace) error {
	settigns, err := settings.GetMapOrEmpty(ctx, stack, "namespace", "labels")
	if err != nil {
		return nil
	}

	if len(settigns) == 0 {
		return nil
	}

	return func(ns *corev1.Namespace) error {
		if ns.Labels == nil {
			ns.Labels = map[string]string{}
		}
		for k, v := range settigns {
			ns.Labels[k] = v
		}
		return nil
	}
}

func namespaceAnnotations(ctx Context, stack string) func(ns *corev1.Namespace) error {
	settigns, err := settings.GetMapOrEmpty(ctx, stack, "namespace", "annotations")
	if err != nil {
		return nil
	}
	if len(settigns) == 0 {
		return nil
	}

	return func(ns *corev1.Namespace) error {
		if ns.Annotations == nil {
			ns.Annotations = map[string]string{}
		}
		for k, v := range settigns {
			ns.Annotations[k] = v
		}

		return nil
	}
}

var errAlreadyExist = errors.New("namespace already exists")

func namespaceCreatedByAgent(ctx Context, stack *v1beta1.Stack) func(ns *corev1.Namespace) error {
	return func(ns *corev1.Namespace) error {
		_, stackCreatedByAgent := stack.GetLabels()[v1beta1.CreatedByAgentLabel]
		if ns.ResourceVersion == "" || stackCreatedByAgent {
			return nil
		}

		return errAlreadyExist
	}
}

func Reconcile(ctx Context, stack *v1beta1.Stack) error {
	logger := log.FromContext(ctx)
	logger = logger.WithValues("stack", stack.Name)
	logger.Info("Reconcile stack")
	if _, _, err := CreateOrUpdate(ctx,
		types.NamespacedName{
			Name: stack.Name,
		},
		namespaceLabel(ctx, stack.Name),
		namespaceAnnotations(ctx, stack.Name),
		namespaceCreatedByAgent(ctx, stack),
		core.WithController[*corev1.Namespace](ctx.GetScheme(), stack)); err != nil {
		if !errors.Is(err, errAlreadyExist) {
			return err
		}
	}

	if err := setModulesCondition(ctx, stack); err != nil {
		return err
	}

	if stack.MustSkip() {
		stack.GetConditions().AppendOrReplace(
			*v1beta1.NewCondition("Skipped", stack.Generation).SetMessage("Stack marked as skipped"),
			v1beta1.ConditionTypeMatch("Skipped"),
		)
	} else {
		stack.GetConditions().Delete(v1beta1.ConditionTypeMatch("Skipped"))
	}
	logger.Info("CONDITIONS", "conditions", stack.Status.Conditions)
	return nil
}

func Clean(ctx Context, t *v1beta1.Stack) error {
	logger := log.FromContext(ctx)
	logger = logger.WithValues("stack", t.Name)
	logger.Info("Clean stack")

	if err := deleteModules(ctx, t, logger); err != nil {
		return err
	}

	logger.Info("All modules removed")

	if err := deleteResources(ctx, t, logger); err != nil {
		return err
	}

	logger.Info("All dependencies removed")

	return nil
}

type pendingDeletion struct {
	GroupVersionKind schema.GroupVersionKind
	Name             string
	JustDeleted      bool
}

func (p pendingDeletion) String() string {
	return fmt.Sprintf("%s %s [deleted=%v]", p.GroupVersionKind, p.Name, p.JustDeleted)
}

type pendingDeletions []pendingDeletion

func (p pendingDeletions) String() string {
	return strings.Join(collectionutils.Map(p, pendingDeletion.String), ", ")
}

func deleteModules(ctx Context, stack *v1beta1.Stack, logger logr.Logger) error {
	pendingModuleDeletions := pendingDeletions{}
	for _, rtype := range ctx.GetScheme().AllKnownTypes() {
		v := reflect.New(rtype).Interface()
		module, ok := v.(v1beta1.Module)
		if !ok {
			continue
		}

		gvk, err := apiutil.GVKForObject(module, ctx.GetScheme())
		if err != nil {
			return err
		}

		l := &unstructured.UnstructuredList{}
		l.SetGroupVersionKind(gvk)
		if err := ctx.GetAPIReader().List(ctx, l); err != nil {
			return err
		}

		items := collectionutils.Filter(l.Items, func(u unstructured.Unstructured) bool {
			return u.Object["spec"].(map[string]any)["stack"].(string) == stack.Name
		})

		for _, item := range items {
			pendingModuleDeletion := pendingDeletion{
				GroupVersionKind: gvk,
				Name:             item.GetName(),
			}
			if item.GetDeletionTimestamp().IsZero() {
				logger.Info(fmt.Sprintf("Delete module %s [%s]", item.GetName(), gvk))
				if err := ctx.GetClient().Delete(ctx, &item); client.IgnoreNotFound(err) != nil {
					return err
				}
				pendingModuleDeletion.JustDeleted = true
			}
			pendingModuleDeletions = append(pendingModuleDeletions, pendingModuleDeletion)
		}
	}

	if len(pendingModuleDeletions) > 0 {
		return NewPendingError().WithMessage("Waiting for module deletion: %s", pendingModuleDeletions)
	}

	return nil
}

func deleteResources(ctx Context, stack *v1beta1.Stack, logger logr.Logger) error {
	pendingResourceDeletions := pendingDeletions{}
	for _, rtype := range ctx.GetScheme().AllKnownTypes() {
		v := reflect.New(rtype).Interface()
		resource, ok := v.(v1beta1.Resource)
		if !ok {
			continue
		}
		gvk, err := apiutil.GVKForObject(resource, ctx.GetScheme())
		if err != nil {
			return err
		}

		l := &unstructured.UnstructuredList{}
		l.SetGroupVersionKind(gvk)
		if err := ctx.GetAPIReader().List(ctx, l); err != nil {
			return err
		}

		items := collectionutils.Filter(l.Items, func(u unstructured.Unstructured) bool {
			return u.Object["spec"].(map[string]any)["stack"].(string) == stack.Name
		})

		for _, item := range items {
			pendingResourceDeletion := pendingDeletion{
				GroupVersionKind: gvk,
				Name:             item.GetName(),
			}
			if item.GetDeletionTimestamp().IsZero() {
				pendingResourceDeletion.JustDeleted = true
				logger.Info(fmt.Sprintf("Delete resource %s [%s]", item.GetName(), gvk))
				if err := ctx.GetClient().Delete(ctx, &item); client.IgnoreNotFound(err) != nil {
					return err
				}
			}
			pendingResourceDeletions = append(pendingResourceDeletions, pendingResourceDeletion)
		}
	}

	if len(pendingResourceDeletions) > 0 {
		return NewPendingError().WithMessage("Waiting for resources deletion: %s", pendingResourceDeletions)
	}

	return nil
}

func init() {
	Init(
		WithSimpleIndex(".spec.versionsFromFile", func(t *v1beta1.Stack) string {
			return t.Spec.VersionsFromFile
		}),
		WithStdReconciler(Reconcile,
			WithOwn[*v1beta1.Stack](&corev1.Namespace{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})),
			WithRaw[*v1beta1.Stack](func(ctx Context, b *builder.Builder) error {
				for _, rtype := range ctx.GetScheme().AllKnownTypes() {
					v := reflect.New(rtype).Interface()

					switch v := v.(type) {
					case v1beta1.Module:
						u := &unstructured.Unstructured{}
						gvk, err := apiutil.GVKForObject(v, ctx.GetScheme())
						if err != nil {
							return err
						}
						u.SetGroupVersionKind(gvk)

						b.Watches(u, handler.EnqueueRequestsFromMapFunc(func(watchContext context.Context, object client.Object) []reconcile.Request {
							return []reconcile.Request{{
								NamespacedName: types.NamespacedName{
									Name: object.(*unstructured.Unstructured).Object["spec"].(map[string]any)["stack"].(string),
								},
							}}
						}))
					case v1beta1.Resource:
						u := &unstructured.Unstructured{}
						gvk, err := apiutil.GVKForObject(v, ctx.GetScheme())
						if err != nil {
							return err
						}
						u.SetGroupVersionKind(gvk)

						b.Watches(u, handler.EnqueueRequestsFromMapFunc(func(watchContext context.Context, object client.Object) []reconcile.Request {
							return []reconcile.Request{{
								NamespacedName: types.NamespacedName{
									Name: object.(*unstructured.Unstructured).Object["spec"].(map[string]any)["stack"].(string),
								},
							}}
						}))
					}
				}

				return nil
			}),
			// notes(gfyrag): Some resources need to be properly dropped before the stack is dropped
			WithFinalizer("delete", Clean),
		),
	)
}
