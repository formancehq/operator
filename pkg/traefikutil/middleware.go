package traefikutil

import (
	"context"

	"github.com/numary/formance-operator/pkg/resourceutil"
	"github.com/traefik/traefik/v2/pkg/config/dynamic"
	traefik "github.com/traefik/traefik/v2/pkg/provider/kubernetes/crd/traefik/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func CreateMiddlewareWithOwner(ctx context.Context, client client.Client, scheme *runtime.Scheme, key types.NamespacedName, owner client.Object, mutate func(t *traefik.Middleware) error) (*traefik.Middleware, controllerutil.OperationResult, error) {
	return resourceutil.CreateOrUpdateWithOwner[*traefik.Middleware](ctx, client, scheme, key, owner, mutate)
}

func CreateStripPrefixMiddlewareWithOwner(ctx context.Context, client client.Client, scheme *runtime.Scheme,
	key types.NamespacedName, owner client.Object, prefixes ...string) (*traefik.Middleware, controllerutil.OperationResult, error) {
	return CreateMiddlewareWithOwner(ctx, client, scheme, key, owner, func(t *traefik.Middleware) error {
		t.Spec = traefik.MiddlewareSpec{
			StripPrefix: &dynamic.StripPrefix{
				Prefixes: prefixes,
			},
		}
		return nil
	})
}
