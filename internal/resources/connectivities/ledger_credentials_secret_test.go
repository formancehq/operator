package connectivities

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func TestLedgerCredentialsSecretCRDSupportedRejectsOlderSchema(t *testing.T) {
	ctx := newReconcileTestContext(t)
	if supported, err := ledgerCredentialsSecretCRDSupported(ctx); err != nil || !supported {
		t.Fatalf("Credentials Secret schema not recognized: supported=%v err=%v", supported, err)
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivities.connectivity.formance.com"}, crd); err != nil {
		t.Fatal(err)
	}
	version := &crd.Spec.Versions[0]
	spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
	auth := spec.Properties["auth"]
	delete(auth.Properties, "credentialsSecretName")
	spec.Properties["auth"] = auth
	version.Schema.OpenAPIV3Schema.Properties["spec"] = spec
	if err := ctx.GetClient().Update(ctx, crd); err != nil {
		t.Fatal(err)
	}
	if supported, err := ledgerCredentialsSecretCRDSupported(ctx); err != nil || supported {
		t.Fatalf("old schema accepted: supported=%v err=%v", supported, err)
	}
}

func TestConnectivityReconcileWaitsForCredentialsSecretSchema(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	ledger, credentials, _ := newReadyLedgerPrerequisites()
	ctx := newReconcileTestContext(t, ledger, credentials)
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivities.connectivity.formance.com"}, crd); err != nil {
		t.Fatal(err)
	}
	version := &crd.Spec.Versions[0]
	spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
	auth := spec.Properties["auth"]
	delete(auth.Properties, "credentialsSecretName")
	spec.Properties["auth"] = auth
	version.Schema.OpenAPIV3Schema.Properties["spec"] = spec
	if err := ctx.GetClient().Update(ctx, crd); err != nil {
		t.Fatal(err)
	}
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name
	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile with old schema = %v, want pending", err)
	}
	condition := connectivity.GetConditions().Get(connectivityReadyCondition)
	if condition == nil || condition.Reason != "LedgerCredentialsSchemaUnavailable" {
		t.Fatalf("condition = %+v, want LedgerCredentialsSchemaUnavailable", condition)
	}
}

func TestDeleteLegacyLedgerBundlePreservesForeignSecret(t *testing.T) {
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = "stack0"
	foreign := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "stack0", Name: legacyConnectivityLedgerBundleSecret,
		UID: types.UID("foreign-uid")}, Data: map[string][]byte{"bundle.json": []byte("leave alone")}}
	ctx := newReconcileTestContext(t, foreign)
	if err := deleteLegacyLedgerBundle(ctx, connectivity); err != nil {
		t.Fatal(err)
	}
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(foreign), &corev1.Secret{}); err != nil {
		t.Fatalf("foreign Secret was removed: %v", err)
	}
	owned := foreign.DeepCopy()
	owned.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity"))}
	if err := ctx.GetClient().Update(ctx, owned); err != nil {
		t.Fatal(err)
	}
	if err := deleteLegacyLedgerBundle(ctx, connectivity); err != nil {
		t.Fatal(err)
	}
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(foreign), &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned legacy Secret remains: %v", err)
	}
}
