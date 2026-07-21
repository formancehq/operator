package nodeisolation

import (
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// Reconcile materializes (or cleans up) the dedicated Karpenter EC2NodeClass and NodePool
// for a stack, based on its resolved isolation Config. It is a no-op when the Karpenter
// CRDs are not installed.
func Reconcile(ctx core.Context, stack *v1beta1.Stack) error {
	if !IsAvailable() {
		return nil
	}

	cfg, err := Resolve(ctx, stack)
	if err != nil {
		return err
	}

	if !cfg.Enabled {
		return cleanup(ctx, stack, cfg)
	}

	if err := reconcileEC2NodeClass(ctx, stack, cfg); err != nil {
		return err
	}
	if err := reconcileNodePool(ctx, stack, cfg); err != nil {
		return err
	}

	return cleanup(ctx, stack, cfg)
}

func reconcileEC2NodeClass(ctx core.Context, stack *v1beta1.Stack, cfg *Config) error {
	refSpec, err := referenceSpec(ctx, cfg.EC2NodeClassGVK, cfg.EC2NodeClassRef)
	if err != nil {
		return err
	}

	// Inject the customer/organization/stack tags onto the cloned spec.
	tags, _, _ := unstructured.NestedStringMap(refSpec, "tags")
	if tags == nil {
		tags = map[string]string{}
	}
	for k, v := range cfg.Tags {
		tags[k] = v
	}
	if err := unstructured.SetNestedStringMap(refSpec, tags, "tags"); err != nil {
		return err
	}

	return applyKarpenterObject(ctx, stack, cfg, cfg.EC2NodeClassGVK, refSpec)
}

func reconcileNodePool(ctx core.Context, stack *v1beta1.Stack, cfg *Config) error {
	refSpec, err := referenceSpec(ctx, cfg.NodePoolGVK, cfg.NodePoolRef)
	if err != nil {
		return err
	}

	// Node labels on the provisioned nodes.
	nodeLabels, _, _ := unstructured.NestedStringMap(refSpec, "template", "metadata", "labels")
	if nodeLabels == nil {
		nodeLabels = map[string]string{}
	}
	for k, v := range cfg.Tags {
		nodeLabels[k] = v
	}
	if err := unstructured.SetNestedStringMap(refSpec, nodeLabels, "template", "metadata", "labels"); err != nil {
		return err
	}

	// Taints repel non-dedicated workloads.
	if err := unstructured.SetNestedSlice(refSpec, taintsToUnstructured(cfg.Taints), "template", "spec", "taints"); err != nil {
		return err
	}

	// Point the NodePool at the cloned EC2NodeClass.
	nodeClassRef := map[string]any{
		"group": cfg.EC2NodeClassGVK.Group,
		"kind":  cfg.EC2NodeClassGVK.Kind,
		"name":  cfg.PoolName,
	}
	if err := unstructured.SetNestedMap(refSpec, nodeClassRef, "template", "spec", "nodeClassRef"); err != nil {
		return err
	}

	return applyKarpenterObject(ctx, stack, cfg, cfg.NodePoolGVK, refSpec)
}

// applyKarpenterObject creates or updates the Karpenter object named cfg.PoolName, setting
// its spec to the injected clone, stamping management labels, and adding the stack as a
// non-controller owner (so an organization pool shared by several stacks is only garbage
// collected once the last owning stack is gone).
func applyKarpenterObject(ctx core.Context, stack *v1beta1.Stack, cfg *Config, gvk schema.GroupVersionKind, spec map[string]any) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(cfg.PoolName)

	_, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), obj, func() error {
		obj.Object["spec"] = runtime.DeepCopyJSON(spec)

		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[ManagedByLabel] = ManagedByValue
		labels[v1beta1.OrganizationLabel] = cfg.Organization
		if cfg.Mode == ModeStack {
			labels[v1beta1.StackLabel] = cfg.Stack
		}
		obj.SetLabels(labels)

		hasOwner, err := core.HasOwnerReference(ctx, stack, obj)
		if err != nil {
			return err
		}
		if !hasOwner {
			if err := controllerutil.SetOwnerReference(stack, obj, ctx.GetScheme()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errors.Wrapf(err, "creating/updating %s %s", gvk.Kind, cfg.PoolName)
	}
	return nil
}

// cleanup releases (and deletes when orphaned) any Karpenter object this stack participates
// in that is no longer the desired one — e.g. after an isolation-mode change, an
// organization relabel, or disabling the feature. It selects by management label rather
// than owner reference because the module cleanup path cannot see cluster-scoped objects.
func cleanup(ctx core.Context, stack *v1beta1.Stack, cfg *Config) error {
	desired := ""
	if cfg.Enabled {
		desired = cfg.PoolName
	}

	for _, gvk := range []schema.GroupVersionKind{cfg.EC2NodeClassGVK, cfg.NodePoolGVK} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		if err := ctx.GetClient().List(ctx, list, client.MatchingLabels{ManagedByLabel: ManagedByValue}); err != nil {
			return errors.Wrapf(err, "listing %s for cleanup", gvk.Kind)
		}

		for i := range list.Items {
			item := &list.Items[i]
			if item.GetName() == desired {
				continue
			}
			// Cheap pre-filter on the (possibly stale) list item; releaseOrDelete re-fetches
			// and re-checks under optimistic concurrency.
			owned, err := core.HasOwnerReference(ctx, stack, item)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}
			if err := releaseOrDelete(ctx, stack, gvk, item.GetName()); err != nil {
				return err
			}
		}
	}

	return nil
}

// releaseOrDelete drops this stack's owner reference from the named object and deletes it
// only if no owners remain. It re-fetches the latest object and uses optimistic concurrency
// (Update carries the resourceVersion; Delete is guarded by UID+resourceVersion
// preconditions) so a concurrent reconcile adding another owner cannot be clobbered or lose
// its pool to a stale-state deletion.
func releaseOrDelete(ctx core.Context, stack *v1beta1.Stack, gvk schema.GroupVersionKind, name string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: name}, obj); err != nil {
			return client.IgnoreNotFound(err)
		}

		owned, err := core.HasOwnerReference(ctx, stack, obj)
		if err != nil {
			return err
		}
		if !owned {
			return nil
		}
		if err := controllerutil.RemoveOwnerReference(stack, obj, ctx.GetScheme()); err != nil {
			return errors.Wrapf(err, "removing owner reference from %s", name)
		}

		if len(obj.GetOwnerReferences()) > 0 {
			return ctx.GetClient().Update(ctx, obj)
		}

		core.LogDeletion(ctx, obj, "nodeisolation.cleanup")
		uid := obj.GetUID()
		resourceVersion := obj.GetResourceVersion()
		err = ctx.GetClient().Delete(ctx, obj, client.Preconditions{UID: &uid, ResourceVersion: &resourceVersion})
		return client.IgnoreNotFound(err)
	})
}

func referenceSpec(ctx core.Context, gvk schema.GroupVersionKind, name string) (map[string]any, error) {
	ref := &unstructured.Unstructured{}
	ref.SetGroupVersionKind(gvk)
	if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Name: name}, ref); err != nil {
		return nil, errors.Wrapf(err, "getting reference %s %q", gvk.Kind, name)
	}
	spec, ok := core.GetSpecFromUnstructured(ref)
	if !ok {
		return nil, core.NewPendingError().WithMessage("reference %s %q has no spec", gvk.Kind, name)
	}
	return runtime.DeepCopyJSON(spec), nil
}

func taintsToUnstructured(taints []corev1.Taint) []any {
	out := make([]any, 0, len(taints))
	for _, t := range taints {
		out = append(out, map[string]any{
			"key":    t.Key,
			"value":  t.Value,
			"effect": string(t.Effect),
		})
	}
	return out
}
