package registries

import (
	"crypto/sha256"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

// IsEndpointSetting includes wildcard keys supported by Settings resolution.
func IsEndpointSetting(key string) bool {
	parts := settings.SplitKeywordWithDot(key)
	return len(parts) == 3 && (parts[0] == "registries" || parts[0] == "*") && (parts[2] == "endpoint" || parts[2] == "*")
}

func pullSecretNames(ctx core.Context, stack string) ([]string, error) {
	all := &v1beta1.SettingsList{}
	if err := ctx.GetClient().List(ctx, all); err != nil {
		return nil, err
	}
	registries := map[string]bool{}
	for _, setting := range all.Items {
		if !IsEndpointSetting(setting.Spec.Key) || (!setting.IsWildcard() && !slices.Contains(setting.GetStacks(), stack)) {
			continue
		}
		registries[settings.SplitKeywordWithDot(setting.Spec.Key)[1]] = true
	}
	names := map[string]bool{}
	for registry := range registries {
		endpoint, err := settings.GetStringOrEmpty(ctx, stack, "registries", registry, "endpoint")
		if err != nil {
			return nil, err
		}
		_, secrets, err := parseEndpoint(endpoint)
		if err != nil {
			return nil, fmt.Errorf("registry %q endpoint for stack %q: %w", registry, stack, err)
		}
		for _, secret := range secrets {
			names[secret.Name] = true
		}
	}
	ret := make([]string, 0, len(names))
	for name := range names {
		ret = append(ret, name)
	}
	slices.Sort(ret)
	return ret, nil
}

// ReconcilePullSecretReferences provisions credentials independently of module
// readiness. ResourceReference remains responsible for source authorization and
// copying; deleting a reference delegates shared-copy cleanup to Kubernetes GC.
func ReconcilePullSecretReferences(ctx core.Context, stack *v1beta1.Stack, additionalNames ...string) error {
	names, err := pullSecretNames(ctx, stack.Name)
	if err != nil {
		return err
	}
	names = append(names, additionalNames...)
	slices.Sort(names)
	names = slices.Compact(names)
	desired := map[string]bool{}
	pending := false
	for _, name := range names {
		hash := sha256.Sum256([]byte(string(stack.UID) + "/" + name))
		referenceName := fmt.Sprintf("registry-pull-secret-%x", hash[:20])
		secret := &corev1.Secret{}
		err := ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: name}, secret)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err == nil {
			managed, err := resourcereferences.HasResourceReferenceOwner(ctx, secret)
			if err != nil {
				return err
			}
			// Existing local credentials retain their data and ownership.
			if !managed {
				continue
			}
		}
		desired[referenceName] = true
		reference := &v1beta1.ResourceReference{ObjectMeta: metav1.ObjectMeta{Name: referenceName}}
		_, err = controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), reference, func() error {
			if reference.ResourceVersion != "" && !metav1.IsControlledBy(reference, stack) {
				return fmt.Errorf("registry pull secret reference %s has a different owner", reference.Name)
			}
			if reference.Labels == nil {
				reference.Labels = map[string]string{}
			}
			reference.Labels[resourcereferences.RegistryPullSecretLabel] = "true"
			reference.Spec.Stack = stack.Name
			reference.Spec.Name = name
			reference.Spec.GroupVersionKind = &metav1.GroupVersionKind{Version: "v1", Kind: "Secret"}
			return controllerutil.SetControllerReference(stack, reference, ctx.GetScheme())
		})
		if err != nil {
			return err
		}
		pending = pending || !reference.Status.Ready
	}
	// Keep previous credentials until replacements have been copied successfully.
	if pending {
		return core.NewPendingError().WithMessage("Waiting for registry pull secret references")
	}
	references := &v1beta1.ResourceReferenceList{}
	if err := ctx.GetClient().List(ctx, references, client.MatchingLabels{resourcereferences.RegistryPullSecretLabel: "true"}); err != nil {
		return err
	}
	for _, reference := range references.Items {
		if metav1.IsControlledBy(&reference, stack) && !desired[reference.Name] {
			if err := ctx.GetClient().Delete(ctx, &reference); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}
