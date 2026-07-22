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
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/ledgers"
	"github.com/formancehq/operator/v3/internal/resources/registries"
)

const (
	connectivityReadyCondition = "ConnectivityClusterReady"
	// connectivityAPIPort is the port the connectivity-api HTTP server (and its
	// Service, provisioned by the connectivity operator) listens on.
	connectivityAPIPort = int32(8080)
)

var (
	// connectivityGVK is the delegated resource, owned by the connectivity
	// operator (connectivity.formance.com), that carries the actual workload.
	// The Connectivity module mirrors the Ledger v3 pattern: it does not run
	// the workload itself, it provisions this resource bound to the stack's
	// ledger and reflects its readiness.
	connectivityGVK = schema.GroupVersionKind{
		Group:   "connectivity.formance.com",
		Version: "v1alpha1",
		Kind:    "Connectivity",
	}
	connectivityAvailable bool

	// ledgerCredentialsGVK is the cluster-scoped ledger.formance.com/Credentials
	// resource. The connectivity module provisions a god-mode credential so
	// connectivity-core can authenticate its gRPC calls to the stack's Ledger v3:
	// the ledger operator generates the Ed25519 keypair, registers the public key
	// on the matched ledger Cluster, and distributes the private seed as a Secret.
	ledgerCredentialsGVK = schema.GroupVersionKind{
		Group:   "ledger.formance.com",
		Version: "v1alpha1",
		Kind:    "Credentials",
	}
)

var connectivityRequiredVerbs = []string{"get", "list", "watch", "create", "update", "patch", "delete"}

//+kubebuilder:rbac:groups=formance.com,resources=connectivities,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=formance.com,resources=connectivities/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=formance.com,resources=connectivities/finalizers,verbs=update
//+kubebuilder:rbac:groups=connectivity.formance.com,resources=connectivities,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=selfsubjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=ledger.formance.com,resources=credentials,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ledger.formance.com,resources=credentials/status,verbs=get

func Reconcile(ctx Context, stack *v1beta1.Stack, connectivity *v1beta1.Connectivity, version string) error {
	if !connectivityAvailable {
		setCondition(connectivity, metav1.ConditionFalse, "OperatorUnavailable",
			"connectivity operator unavailable: connectivity.formance.com Connectivity CRD is not installed")
		return NewPendingError().WithMessage("connectivity operator unavailable: connectivity.formance.com Connectivity CRD is not installed")
	}

	// Connectivity ingests into the stack's Ledger v3 gRPC endpoint, so it can
	// only be provisioned once that ledger is present, on a v3 version, and
	// ready.
	ledger, err := getStackLedger(ctx, stack.Name)
	if err != nil {
		return err
	}
	if ledger == nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerNotFound", "stack has no Ledger module")
		return NewPendingError().WithMessage("connectivity requires a Ledger module on the stack")
	}

	// Resolve the ledger's effective version through the same path the module
	// reconcilers use (module override, stack version, or the referenced
	// Versions file), so the v3 gate also works for versionsFromFile stacks.
	ledgerVersion, err := ResolveModuleVersion(ctx, stack, ledger)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerVersionUnresolved", err.Error())
		return NewPendingError().WithMessage("cannot resolve the ledger version: %s", err.Error())
	}
	if !ledgers.IsV3(ledgerVersion) {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerNotV3",
			fmt.Sprintf("connectivity requires a Ledger v3 (found %q)", ledgerVersion))
		return NewPendingError().WithMessage("connectivity requires a Ledger v3")
	}
	if !ledger.IsReady() {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerNotReady", "waiting for the ledger to be ready")
		return NewPendingError().WithMessage("waiting for the ledger to be ready")
	}

	// Provision a god-mode ledger credential so connectivity-core can
	// authenticate its gRPC calls. The ledger operator registers the public key
	// on the ledger and distributes the private seed as a Secret in the stack
	// namespace; connectivity-core is wired to it via spec.auth below.
	authKeyID, authSecretName, credReady, err := ensureLedgerCredentials(ctx, stack)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerCredentialsFailed", err.Error())
		return err
	}
	if !credReady {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerCredentialsPending",
			"waiting for ledger credentials to be provisioned")
		return NewPendingError().WithMessage("waiting for ledger credentials to be provisioned")
	}

	// Resolve the connectivity-core image through the operator's registry
	// translation so it honours the stack's registry settings (e.g. the
	// ghcr.io -> registry.v2.formance.dev rewrite and pull secrets) instead of
	// the connectivity operator's built-in ghcr.io/...:latest default, which
	// would not be pullable on rewritten registries.
	image, err := registries.GetFormanceImage(ctx, stack, "connectivity-core", version)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "ImageResolveFailed", err.Error())
		return err
	}
	apiImage, err := registries.GetFormanceImage(ctx, stack, "connectivity-api", version)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "APIImageResolveFailed", err.Error())
		return err
	}

	// Reuse the single source of truth for the ledger v3 gRPC connection: same
	// service, port and backend TLS material (self-signed CA secret + SNI) that
	// the gateway uses to reach the ledger.
	backend := ledgers.V3GRPCBackendRef(stack.Name)
	ledgerAddress := fmt.Sprintf("%s:%d", backend.TLS.ServerName, backend.Port)

	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(connectivityGVK)
	object.SetNamespace(stack.Name)
	object.SetName(stack.Name)
	if _, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), object, func() error {
		if err := controllerutil.SetControllerReference(connectivity, object, ctx.GetScheme()); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, ledgerAddress, "spec", "ledgerAddress"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, backend.TLS.SecretName, "spec", "ledgerTLS", "secretName"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, backend.TLS.ServerName, "spec", "ledgerTLS", "serverName"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, image.GetFullImageName(), "spec", "image"); err != nil {
			return err
		}
		if err := unstructured.SetNestedSlice(object.Object, pullSecretsToUnstructured(image.PullSecrets), "spec", "imagePullSecrets"); err != nil {
			return err
		}
		// connectivity-api companion is always enabled; its image is resolved
		// through the same registry translation so it is pullable on rewritten
		// registries (the connectivity operator would otherwise default to
		// ghcr.io/formancehq/connectivity-api:latest).
		if err := unstructured.SetNestedField(object.Object, true, "spec", "api", "enabled"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, apiImage.GetFullImageName(), "spec", "api", "image"); err != nil {
			return err
		}
		if err := unstructured.SetNestedSlice(object.Object, pullSecretsToUnstructured(apiImage.PullSecrets), "spec", "api", "imagePullSecrets"); err != nil {
			return err
		}
		// Ledger auth: connectivity-core signs its gRPC tokens with the Ed25519
		// seed distributed by the ledger Credentials (key "seed.hex"), using the
		// registered key ID. The connectivity operator turns this into the
		// --auth-key-id / --auth-key-file flags.
		if err := unstructured.SetNestedField(object.Object, authKeyID, "spec", "auth", "keyId"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, "connectivity", "spec", "auth", "subject"); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(object.Object, authSecretName, "spec", "auth", "secretKeyRef", "name"); err != nil {
			return err
		}
		return unstructured.SetNestedField(object.Object, "seed.hex", "spec", "auth", "secretKeyRef", "key")
	}); err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return err
	}

	// Expose the connectivity-api through the stack gateway: routes
	// /api/connectivity to the connectivity-api Service (named "<stack>-api")
	// the connectivity operator provisions for the delegated Connectivity.
	if err := gatewayhttpapis.Create(ctx, connectivity,
		gatewayhttpapis.WithRules(gatewayhttpapis.RuleSecuredWithBackend("", connectivityAPIBackendRef(stack.Name))),
	); err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "GatewayReconcileFailed", err.Error())
		return err
	}

	ready, message := connectivityResourceReady(object)
	if !ready {
		setCondition(connectivity, metav1.ConditionFalse, "ConnectivityPending", message)
		return NewPendingError().WithMessage("%s", message)
	}

	setCondition(connectivity, metav1.ConditionTrue, "Ready", "Connectivity is ready")
	return nil
}

// connectivityAPIBackendRef points the gateway at the connectivity-api Service
// the connectivity operator provisions for the delegated Connectivity, which is
// named "<connectivity-name>-api" (the delegated resource is named after the
// stack).
func connectivityAPIBackendRef(stackName string) v1beta1.GatewayBackendRef {
	return v1beta1.GatewayBackendRef{
		Name: stackName + "-api",
		Port: connectivityAPIPort,
	}
}

func pullSecretsToUnstructured(secrets []corev1.LocalObjectReference) []any {
	out := make([]any, 0, len(secrets))
	for _, ps := range secrets {
		out = append(out, map[string]any{"name": ps.Name})
	}
	return out
}

func getStackLedger(ctx Context, stackName string) (*v1beta1.Ledger, error) {
	list := &v1beta1.LedgerList{}
	if err := ctx.GetClient().List(ctx, list, client.MatchingFields{"stack": stackName}); err != nil {
		return nil, err
	}
	if len(list.Items) == 0 {
		return nil, nil
	}
	return &list.Items[0], nil
}

// ensureLedgerCredentials provisions the cluster-scoped ledger Credentials that
// authorizes connectivity-core against the stack's Ledger v3, and reports the
// registered key ID plus the distributed private-key Secret once ready.
//
// The Credentials is cluster-scoped, so it is owned by the (cluster-scoped)
// Stack rather than the namespaced Connectivity — a namespaced owner cannot own
// a cluster-scoped resource. The ledger operator generates the Ed25519 keypair,
// registers the public key on the Cluster matched by the stack selector, and
// distributes the private seed as a Secret into the stack namespace.
func ensureLedgerCredentials(ctx Context, stack *v1beta1.Stack) (keyID, secretName string, ready bool, err error) {
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(ledgerCredentialsGVK)
	cred.SetName("connectivity-" + stack.Name)
	if _, err = controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), cred, func() error {
		if err := controllerutil.SetControllerReference(stack, cred, ctx.GetScheme()); err != nil {
			return err
		}
		if err := unstructured.SetNestedField(cred.Object, true, "spec", "god"); err != nil {
			return err
		}
		if err := unstructured.SetNestedStringMap(cred.Object,
			map[string]string{"formance.com/stack": stack.Name}, "spec", "selector", "matchLabels"); err != nil {
			return err
		}
		return unstructured.SetNestedStringSlice(cred.Object, []string{stack.Name}, "spec", "additionalNamespaces")
	}); err != nil {
		return "", "", false, err
	}

	if phase, _, _ := unstructured.NestedString(cred.Object, "status", "phase"); phase != "Ready" {
		return "", "", false, nil
	}
	keyID, _, _ = unstructured.NestedString(cred.Object, "status", "keyID")
	// The Secret is distributed to several namespaces; pick the one in the stack
	// namespace, where connectivity-core runs.
	refs, _, _ := unstructured.NestedSlice(cred.Object, "status", "distributedSecretRefs")
	for _, r := range refs {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if ns, _ := m["namespace"].(string); ns == stack.Name {
			secretName, _ = m["name"].(string)
			break
		}
	}
	if keyID == "" || secretName == "" {
		return "", "", false, nil
	}
	return keyID, secretName, true, nil
}

func connectivityResourceReady(object *unstructured.Unstructured) (bool, string) {
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	if phase == "Ready" {
		return true, "Connectivity is ready"
	}
	message, _, _ := unstructured.NestedString(object.Object, "status", "message")
	if message == "" {
		message = fmt.Sprintf("connectivity resource phase is %q", phase)
	}
	return false, message
}

func setCondition(connectivity *v1beta1.Connectivity, status metav1.ConditionStatus, reason, message string) {
	connectivity.GetConditions().AppendOrReplace(v1beta1.Condition{
		Type:               connectivityReadyCondition,
		Status:             status,
		ObservedGeneration: connectivity.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}, v1beta1.ConditionTypeMatch(connectivityReadyCondition))
}

// withConnectivityClusterWatch detects, at controller start-up, whether the
// connectivity operator (connectivity.formance.com Connectivity CRD) is
// installed and reachable with the required RBAC. When present the reconciler
// watches and owns the delegated resource; otherwise the module reports the
// capability as unavailable — the same mechanism the Ledger v3 module uses.
func withConnectivityClusterWatch() ReconcilerOption[*v1beta1.Connectivity] {
	return func(options *ReconcilerOptions[*v1beta1.Connectivity]) {
		options.Raws = append(options.Raws, func(ctx Context, b *builder.Builder) error {
			crds := &apiextensionsv1.CustomResourceDefinitionList{}
			if err := ctx.GetAPIReader().List(ctx, crds); err != nil {
				connectivityAvailable = false
				log.FromContext(ctx).Info("Connectivity capability is unavailable; continuing without it", "error", err)
				return nil
			}
			connectivityAvailable = watchConnectivityResource(ctx, b, options, crds, connectivityGVK)
			return nil
		})
	}
}

func watchConnectivityResource(
	ctx Context,
	b *builder.Builder,
	options *ReconcilerOptions[*v1beta1.Connectivity],
	crds *apiextensionsv1.CustomResourceDefinitionList,
	gvk schema.GroupVersionKind,
) bool {
	for _, crd := range crds.Items {
		if crd.Spec.Group != gvk.Group || crd.Spec.Names.Kind != gvk.Kind {
			continue
		}
		for _, version := range crd.Spec.Versions {
			if version.Name != gvk.Version || !version.Served {
				continue
			}
			resourceList := &unstructured.UnstructuredList{}
			resourceList.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
			if err := ctx.GetAPIReader().List(ctx, resourceList, client.Limit(1)); err != nil {
				log.FromContext(ctx).Info("Connectivity dependency is inaccessible; continuing without it", "gvk", gvk, "error", err)
				return false
			}
			if !canAccessConnectivityResource(ctx, gvk, crd.Spec.Names.Plural) {
				return false
			}
			resource := &unstructured.Unstructured{}
			resource.SetGroupVersionKind(gvk)
			options.Owns[resource] = nil
			b.Owns(resource)
			log.FromContext(ctx).Info("Connectivity dependency CRD is available", "gvk", gvk)
			return true
		}
	}
	log.FromContext(ctx).Info("Connectivity dependency CRD is not available", "gvk", gvk)
	return false
}

func canAccessConnectivityResource(ctx Context, gvk schema.GroupVersionKind, resource string) bool {
	for _, verb := range connectivityRequiredVerbs {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:    gvk.Group,
					Version:  gvk.Version,
					Resource: resource,
					Verb:     verb,
				},
			},
		}
		if err := ctx.GetClient().Create(ctx, review); err != nil {
			log.FromContext(ctx).Info("Connectivity dependency access review failed; continuing without it", "gvk", gvk, "verb", verb, "error", err)
			return false
		}
		if !review.Status.Allowed {
			log.FromContext(ctx).Info("Connectivity dependency permission is unavailable; continuing without it", "gvk", gvk, "verb", verb, "reason", review.Status.Reason)
			return false
		}
	}
	return true
}

func init() {
	Init(
		WithModuleReconciler(Reconcile,
			WithOwn[*v1beta1.Connectivity](&v1beta1.GatewayHTTPAPI{}),
			withConnectivityClusterWatch(),
			WithWatchSettings[*v1beta1.Connectivity](),
			WithWatchDependency[*v1beta1.Connectivity](&v1beta1.Ledger{}),
		),
	)
}
