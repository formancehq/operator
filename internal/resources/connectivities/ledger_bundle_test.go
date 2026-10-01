/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package connectivities

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

func bundleTestOwners() (*v1beta1.Stack, *v1beta1.Connectivity) {
	stack := &v1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("stack-uid")}}
	connectivity := &v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "stack0", UID: types.UID("connectivity-uid")}}
	connectivity.Spec.Stack = stack.Name
	return stack, connectivity
}

func TestReconcileLedgerBundleMaterializesExactClaimsAndConverges(t *testing.T) {
	_, credentials, source := newReadyLedgerPrerequisites()
	ctx := newReconcileTestContext(t, source)
	stack, connectivity := bundleTestOwners()
	keyID, _, _ := unstructured.NestedString(credentials.Object, "status", "keyID")
	for i := 0; i < 2; i++ {
		ready, err := reconcileLedgerBundle(ctx, stack, connectivity, source.Name, keyID)
		if err != nil || !ready {
			t.Fatalf("reconcile Ledger bundle pass %d: ready=%v err=%v", i, ready, err)
		}
	}
	secret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityLedgerBundleSecret}, secret); err != nil {
		t.Fatal(err)
	}
	if secret.Type != corev1.SecretTypeOpaque || len(secret.Data) != 1 || !metav1.IsControlledBy(secret, connectivity) {
		t.Fatalf("unexpected bundle Secret type, keys or owner: type=%q keys=%d owner=%v", secret.Type, len(secret.Data), metav1.GetControllerOf(secret))
	}
	var bundle ledgerBundle
	if err := json.Unmarshal(secret.Data[connectivityLedgerBundleKey], &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SigningKey != string(source.Data["seed.hex"]) || bundle.KeyID != keyID || bundle.Subject != "connectivity" || bundle.God || !reflect.DeepEqual(bundle.Scopes, connectivityLedgerScopes) {
		t.Fatalf("bundle claims do not match the Ready non-god Credentials")
	}
	if !bytes.Contains(secret.Data[connectivityLedgerBundleKey], []byte(`"god":false`)) {
		t.Fatal("bundle JSON must explicitly carry god=false")
	}
}

func TestReconcileLedgerBundleRejectsExistingUnownedOrForeignSecret(t *testing.T) {
	_, credentials, source := newReadyLedgerPrerequisites()
	keyID, _, _ := unstructured.NestedString(credentials.Object, "status", "keyID")
	stack, connectivity := bundleTestOwners()
	for _, tc := range []struct {
		name  string
		owner *metav1.OwnerReference
	}{
		{name: "unowned"},
		{name: "foreign", owner: metav1.NewControllerRef(&v1beta1.Connectivity{ObjectMeta: metav1.ObjectMeta{Name: "other", UID: types.UID("foreign")}}, v1beta1.GroupVersion.WithKind("Connectivity"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: connectivityLedgerBundleSecret, Namespace: stack.Name}, Data: map[string][]byte{"sentinel": []byte("do-not-overwrite")}}
			if tc.owner != nil {
				secret.OwnerReferences = []metav1.OwnerReference{*tc.owner}
			}
			ctx := newReconcileTestContext(t, source.DeepCopy(), secret)
			if ready, err := reconcileLedgerBundle(ctx, stack, connectivity, source.Name, keyID); err == nil || ready {
				t.Fatalf("foreign Secret accepted: ready=%v err=%v", ready, err)
			}
			got := &corev1.Secret{}
			if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(secret), got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Data, secret.Data) {
				t.Fatal("foreign Secret was modified")
			}
		})
	}
}

func TestReconcileLedgerBundleWaitsForMatchingSource(t *testing.T) {
	_, credentials, source := newReadyLedgerPrerequisites()
	keyID, _, _ := unstructured.NestedString(credentials.Object, "status", "keyID")
	stack, connectivity := bundleTestOwners()
	for _, tc := range []struct {
		name string
		edit func(*corev1.Secret)
	}{
		{name: "missing key ID", edit: func(s *corev1.Secret) { delete(s.Data, "key-id") }},
		{name: "changed seed", edit: func(s *corev1.Secret) { s.Data["seed.hex"] = bytes.Repeat([]byte("0"), 64) }},
		{name: "changed public key", edit: func(s *corev1.Secret) { s.Data["pubkey.hex"] = bytes.Repeat([]byte("0"), 64) }},
		{name: "wrong owner label", edit: func(s *corev1.Secret) { s.Labels["ledger.formance.com/credentials-name"] = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := source.DeepCopy()
			tc.edit(changed)
			ctx := newReconcileTestContext(t, changed)
			if ready, err := reconcileLedgerBundle(ctx, stack, connectivity, changed.Name, keyID); err != nil || ready {
				t.Fatalf("invalid source accepted: ready=%v err=%v", ready, err)
			}
		})
	}
}

func TestReconcileLedgerBundleRemovesStaleMaterialAndRecoversAfterRotation(t *testing.T) {
	_, credentials, source := newReadyLedgerPrerequisites()
	keyID, _, _ := unstructured.NestedString(credentials.Object, "status", "keyID")
	stack, connectivity := bundleTestOwners()
	ctx := newReconcileTestContext(t, source.DeepCopy())
	if ready, err := reconcileLedgerBundle(ctx, stack, connectivity, source.Name, keyID); err != nil || !ready {
		t.Fatalf("initial bundle: ready=%v err=%v", ready, err)
	}
	if err := ctx.GetClient().Delete(ctx, source); err != nil {
		t.Fatal(err)
	}
	if ready, err := reconcileLedgerBundle(ctx, stack, connectivity, source.Name, keyID); err != nil || ready {
		t.Fatalf("missing source: ready=%v err=%v", ready, err)
	}
	secret := &corev1.Secret{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityLedgerBundleSecret}, secret); !apierrors.IsNotFound(err) {
		t.Fatalf("stale derived bundle lookup = %v, want NotFound", err)
	}
	rotated := source.DeepCopy()
	seed := bytes.Repeat([]byte{2}, ed25519.SeedSize)
	pubkey := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	keyHash := sha256.Sum256(pubkey)
	rotatedID := hex.EncodeToString(keyHash[:8])
	rotated.Data["seed.hex"] = []byte(hex.EncodeToString(seed))
	rotated.Data["pubkey.hex"] = []byte(hex.EncodeToString(pubkey))
	rotated.Data["key-id"] = []byte(rotatedID)
	if err := ctx.GetClient().Create(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	if ready, err := reconcileLedgerBundle(ctx, stack, connectivity, source.Name, rotatedID); err != nil || !ready {
		t.Fatalf("rotated bundle: ready=%v err=%v", ready, err)
	}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: connectivityLedgerBundleSecret}, secret); err != nil {
		t.Fatal(err)
	}
	var bundle ledgerBundle
	if err := json.Unmarshal(secret.Data[connectivityLedgerBundleKey], &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.KeyID != rotatedID || bundle.SigningKey != hex.EncodeToString(seed) {
		t.Fatal("derived bundle did not converge to the rotated credential")
	}
}

func TestMapLedgerSecretToConnectivityIncludesPartialSourceUpdates(t *testing.T) {
	stack, connectivity := bundleTestOwners()
	ctx := newReconcileTestContext(t, connectivity)
	partial := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: stack.Name,
		Name:      "ledger-connectivity-stack0-credentials-keys",
	}}
	if requests := mapLedgerSecretToConnectivity(ctx, partial); len(requests) != 1 || requests[0].Name != connectivity.Name {
		t.Fatalf("partial source Secret mapped to %v, want Connectivity %q", requests, connectivity.Name)
	}
}

func TestLedgerBundleCRDSupportedRejectsOlderSchema(t *testing.T) {
	ctx := newReconcileTestContext(t)
	if supported, err := ledgerBundleCRDSupported(ctx); err != nil || !supported {
		t.Fatalf("bundle schema not recognized: supported=%v err=%v", supported, err)
	}
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivities.connectivity.formance.com"}, crd); err != nil {
		t.Fatal(err)
	}
	version := &crd.Spec.Versions[0]
	spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
	auth := spec.Properties["auth"]
	delete(auth.Properties, "bundleSecretKeyRef")
	spec.Properties["auth"] = auth
	version.Schema.OpenAPIV3Schema.Properties["spec"] = spec
	if err := ctx.GetClient().Update(ctx, crd); err != nil {
		t.Fatal(err)
	}
	if supported, err := ledgerBundleCRDSupported(ctx); err != nil || supported {
		t.Fatalf("old schema accepted: supported=%v err=%v", supported, err)
	}
}

func TestConnectivityReconcileWaitsForBundleSchema(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	ledger, credentials, source := newReadyLedgerPrerequisites()
	ctx := newReconcileTestContext(t, ledger, credentials, source)
	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := ctx.GetClient().Get(ctx, client.ObjectKey{Name: "connectivities.connectivity.formance.com"}, crd); err != nil {
		t.Fatal(err)
	}
	version := &crd.Spec.Versions[0]
	spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
	auth := spec.Properties["auth"]
	delete(auth.Properties, "bundleSecretKeyRef")
	spec.Properties["auth"] = auth
	version.Schema.OpenAPIV3Schema.Properties["spec"] = spec
	if err := ctx.GetClient().Update(ctx, crd); err != nil {
		t.Fatal(err)
	}
	stack, connectivity := bundleTestOwners()
	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile with old schema = %v, want pending", err)
	}
	condition := connectivity.GetConditions().Get(connectivityReadyCondition)
	if condition == nil || condition.Reason != "LedgerBundleSchemaUnavailable" {
		t.Fatalf("condition = %+v, want LedgerBundleSchemaUnavailable", condition)
	}
	delegated := newDelegatedConnectivity(stack.Name)
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(delegated), delegated); !apierrors.IsNotFound(err) {
		t.Fatalf("delegated CR = %v, want NotFound before bundle-capable schema", err)
	}
}

func TestConnectivityReconcileRemovesBundleAndGatewayWhileCredentialsPending(t *testing.T) {
	previous := connectivityAvailable
	connectivityAvailable = true
	t.Cleanup(func() { connectivityAvailable = previous })
	ledger, credentials, source := newReadyLedgerPrerequisites()
	_ = unstructured.SetNestedField(credentials.Object, "Pending", "status", "phase")
	stack, connectivity := bundleTestOwners()
	owner := metav1.NewControllerRef(connectivity, v1beta1.GroupVersion.WithKind("Connectivity"))
	bundle := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: stack.Name, Name: connectivityLedgerBundleSecret,
		UID: types.UID("bundle-uid"), OwnerReferences: []metav1.OwnerReference{*owner},
	}, Data: map[string][]byte{connectivityLedgerBundleKey: []byte("stale")}}
	gateway := &v1beta1.GatewayHTTPAPI{ObjectMeta: metav1.ObjectMeta{
		Name: "stack0-connectivity", UID: types.UID("gateway-uid"),
		OwnerReferences: []metav1.OwnerReference{*owner},
	}}
	ctx := newReconcileTestContext(t, ledger, credentials, source, bundle, gateway)
	if err := Reconcile(ctx, stack, connectivity, "v1.0.0"); !core.IsApplicationError(err) {
		t.Fatalf("Reconcile with pending Credentials = %v, want pending", err)
	}
	condition := connectivity.GetConditions().Get(connectivityReadyCondition)
	if condition == nil || condition.Reason != "LedgerCredentialsPending" {
		t.Fatalf("condition = %+v, want LedgerCredentialsPending", condition)
	}
	if err := ctx.GetClient().Get(ctx, client.ObjectKeyFromObject(bundle), &corev1.Secret{}); !apierrors.IsNotFound(err) {
		t.Fatalf("bundle lookup = %v, want NotFound while Credentials are pending", err)
	}
	if gatewayHTTPAPIExists(t, ctx, stack.Name) {
		t.Fatal("GatewayHTTPAPI remains exposed while Credentials are pending")
	}
}
