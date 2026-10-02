package connectivities

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
)

const legacyConnectivityLedgerBundleSecret = "connectivity-ledger-bundle"

// Older Connectivity CRDs prune unknown fields. Wait until the served schema
// can preserve the single reference to Ledger's distributed credential Secret.
func ledgerCredentialsSecretCRDSupported(ctx Context) (bool, error) {
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetAPIReader().Get(ctx, client.ObjectKey{Name: "connectivities.connectivity.formance.com"}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("read Connectivity CRD: %w", err)
	}
	for _, version := range crd.Spec.Versions {
		if version.Name != connectivityGVK.Version || !version.Served || version.Schema == nil || version.Schema.OpenAPIV3Schema == nil {
			continue
		}
		spec, ok := version.Schema.OpenAPIV3Schema.Properties["spec"]
		if !ok {
			continue
		}
		auth, ok := spec.Properties["auth"]
		if !ok {
			continue
		}
		if secretName, ok := auth.Properties["credentialsSecretName"]; ok && secretName.Type == "string" {
			return true, nil
		}
	}
	return false, nil
}

// An earlier draft wrote a derived bundle Secret. Remove only a Secret owned
// by this Connectivity while moving to Ledger's distributed Secret directly.
func deleteLegacyLedgerBundle(ctx Context, connectivity *v1beta1.Connectivity) error {
	secret := &metav1.PartialObjectMetadata{}
	secret.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	key := client.ObjectKey{Namespace: connectivity.GetStack(), Name: legacyConnectivityLedgerBundleSecret}
	if err := ctx.GetAPIReader().Get(ctx, key, secret); err != nil {
		return ignoreAbsent(err)
	}
	if !controllerRefMatches(metav1.GetControllerOf(secret), v1beta1.GroupVersion.WithKind("Connectivity"), connectivity) {
		return nil
	}
	uid := secret.UID
	return ignoreAbsent(ctx.GetClient().Delete(ctx, secret, client.Preconditions{UID: &uid}))
}
