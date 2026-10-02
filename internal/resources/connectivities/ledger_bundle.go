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
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
)

const (
	connectivityLedgerBundleSecret = "connectivity-ledger-bundle"
	connectivityLedgerBundleKey    = "bundle.json"
)

// Older Connectivity CRDs prune unknown fields. Never write a bundle reference
// until the installed served schema can preserve it.
func ledgerBundleCRDSupported(ctx Context) (bool, error) {
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
		if bundle, ok := auth.Properties["bundleSecretKeyRef"]; ok && bundle.Type == "object" {
			return true, nil
		}
	}
	return false, nil
}

type ledgerBundle struct {
	SigningKey string   `json:"signingKey"`
	KeyID      string   `json:"keyId"`
	Scopes     []string `json:"scopes"`
	Subject    string   `json:"subject"`
	God        bool     `json:"god"`
}

// The ledger operator owns the distributed seed Secret. Materialize a separate
// bundle so its key can rotate without two controllers writing the same object.
// A missing or not-yet-synchronized source is pending; no stale bundle is
// published as the desired credential.
func reconcileLedgerBundle(ctx Context, stack *v1beta1.Stack, connectivity *v1beta1.Connectivity, sourceName, keyID string) (bool, error) {
	if sourceName == connectivityLedgerBundleSecret {
		return false, fmt.Errorf("distributed Ledger credential Secret conflicts with Connectivity bundle Secret name")
	}
	source := &corev1.Secret{}
	if err := ctx.GetAPIReader().Get(ctx, client.ObjectKey{Namespace: stack.Name, Name: sourceName}, source); err != nil {
		if apierrors.IsNotFound(err) {
			return false, deleteLedgerBundle(ctx, connectivity)
		}
		return false, fmt.Errorf("read distributed Ledger credential Secret: %w", err)
	}
	seed := strings.TrimSpace(string(source.Data["seed.hex"]))
	decoded, err := hex.DecodeString(seed)
	if err != nil || len(decoded) != 32 {
		return false, errors.Join(fmt.Errorf("distributed Ledger credential Secret %s/%s has an invalid seed.hex", stack.Name, sourceName), deleteLedgerBundle(ctx, connectivity))
	}
	pubkey := ed25519.NewKeyFromSeed(decoded).Public().(ed25519.PublicKey)
	keyHash := sha256.Sum256(pubkey)
	derivedKeyID := hex.EncodeToString(keyHash[:8])
	if source.Labels["ledger.formance.com/credentials-name"] != "connectivity-"+stack.Name ||
		keyID == "" || string(source.Data["key-id"]) != keyID || derivedKeyID != keyID ||
		!strings.EqualFold(string(source.Data["pubkey.hex"]), hex.EncodeToString(pubkey)) {
		return false, deleteLedgerBundle(ctx, connectivity)
	}
	bundleJSON, err := json.Marshal(ledgerBundle{
		SigningKey: seed,
		KeyID:      keyID,
		Scopes:     connectivityLedgerScopes,
		Subject:    "connectivity",
		God:        false,
	})
	if err != nil {
		return false, err
	}
	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{Name: connectivityLedgerBundleSecret, Namespace: stack.Name}
	if err := ctx.GetClient().Get(ctx, secretKey, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("read Connectivity Ledger bundle Secret: %w", err)
		}
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretKey.Name, Namespace: secretKey.Namespace}}
		if err := controllerutil.SetControllerReference(connectivity, secret, ctx.GetScheme()); err != nil {
			return false, err
		}
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{connectivityLedgerBundleKey: bundleJSON}
		if err := ctx.GetClient().Create(ctx, secret); err != nil {
			return false, fmt.Errorf("create Connectivity Ledger bundle Secret: %w", err)
		}
		return true, nil
	}
	if !controllerRefMatches(metav1.GetControllerOf(secret), v1beta1.GroupVersion.WithKind("Connectivity"), connectivity) {
		return false, fmt.Errorf("Connectivity Ledger bundle Secret %s/%s is not controlled by Connectivity UID %q", stack.Name, connectivityLedgerBundleSecret, connectivity.UID)
	}
	if secret.Type == corev1.SecretTypeOpaque && reflect.DeepEqual(secret.Data, map[string][]byte{connectivityLedgerBundleKey: bundleJSON}) {
		return true, nil
	}
	before := secret.DeepCopy()
	secret.Type = corev1.SecretTypeOpaque
	secret.Data = map[string][]byte{connectivityLedgerBundleKey: bundleJSON}
	if err := ctx.GetClient().Patch(ctx, secret, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		return false, fmt.Errorf("update Connectivity Ledger bundle Secret: %w", err)
	}
	return true, nil
}

func deleteLedgerBundle(ctx Context, connectivity *v1beta1.Connectivity) error {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: connectivity.GetStack(), Name: connectivityLedgerBundleSecret}
	if err := ctx.GetAPIReader().Get(ctx, key, secret); err != nil {
		return ignoreAbsent(err)
	}
	if !controllerRefMatches(metav1.GetControllerOf(secret), v1beta1.GroupVersion.WithKind("Connectivity"), connectivity) {
		return nil
	}
	uid := secret.UID
	return ignoreAbsent(ctx.GetClient().Delete(ctx, secret, client.Preconditions{UID: &uid}))
}

// Only a distributed Ledger seed or our derived bundle can affect this module.
// Secret changes trigger reconciliation of the Connectivity in that namespace.
func mapLedgerSecretToConnectivity(ctx Context, secret *corev1.Secret) []reconcile.Request {
	if secret.Name != connectivityLedgerBundleSecret &&
		secret.Name != "ledger-connectivity-"+secret.Namespace+"-credentials-keys" &&
		secret.Labels["ledger.formance.com/credentials-name"] != "connectivity-"+secret.Namespace {
		return nil
	}
	list := &v1beta1.ConnectivityList{}
	if err := ctx.GetClient().List(ctx, list, client.MatchingFields{"stack": secret.Namespace}); err != nil {
		log.FromContext(ctx).Error(err, "listing Connectivity for Ledger Secret watch", "stack", secret.Namespace)
		return nil
	}
	items := make([]*v1beta1.Connectivity, len(list.Items))
	for i := range list.Items {
		items[i] = &list.Items[i]
	}
	return MapObjectToReconcileRequests(items...)
}
