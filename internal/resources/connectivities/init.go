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
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/ledgers"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const (
	connectivityReadyCondition  = "ConnectivityClusterReady"
	ledgerCredentialsRetryDelay = 5 * time.Second
	// connectivityDelegatedName is the fixed name of the delegated
	// connectivity.formance.com/Connectivity resource. It is namespaced (one per
	// stack namespace), so a constant name stays unique per stack; the connectivity
	// operator derives its API Service name from it ("connectivity-api").
	connectivityDelegatedName = "connectivity"
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

	// ledgerCredentialsWatchAvailable records whether, at controller start-up,
	// the ledger Credentials CRD was present so the reconciler could register a
	// watch on it. When true, changes to the connectivity-<stack> Credentials
	// (notably its status.phase flipping to Ready) re-trigger the owning
	// Connectivity's reconcile.
	ledgerCredentialsWatchAvailable bool
)

// ledgerHasV3 resolves whether the stack runs a Ledger v3 workload the
// connectivity module can bind to (a v3 module version, or the v3 preview
// enabled through the ledger.v3.preview-version Setting). Package variable so
// tests can stub the preview branch, which depends on the ledger controller's
// startup capability discovery.
var ledgerHasV3 = ledgers.HasV3

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
		// The connectivity operator is gone, so we can neither provision nor
		// manage the delegated workload — but if the ledger hard gate is also
		// closed we must still tear down the god-mode Credentials and the gateway
		// route we own, otherwise a closed gate leaves them behind just because
		// this capability check short-circuits before the ledger gates below.
		// Gated on the gate being closed so a transient operator outage (with a
		// healthy ledger) does not flap those resources.
		if ledgerGateClosed(ctx, stack) {
			if err := teardownAccessibleResources(ctx, connectivity); err != nil {
				return err
			}
		}
		return NewPendingError().WithMessage("connectivity operator unavailable: connectivity.formance.com Connectivity CRD is not installed")
	}

	// Connectivity ingests into the stack's Ledger v3 gRPC endpoint, so it can
	// only be provisioned once that ledger is present, running a v3 (as its
	// module version, or as the v3 preview alongside a v2 ledger), and ready.
	ledger, err := getStackLedger(ctx, stack.Name)
	if err != nil {
		return err
	}
	if ledger == nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerNotFound", "stack has no Ledger module")
		// Hard gate: the Ledger module has been removed from the stack. This is a
		// definitive loss of the prerequisite (not a momentary blip), so tear down
		// any previously provisioned delegated Connectivity + GatewayHTTPAPI rather
		// than leaving the workload running and exposed through the gateway with no
		// ledger to ingest into.
		if err := teardownDelegated(ctx, stack, connectivity); err != nil {
			return err
		}
		return NewPendingError().WithMessage("connectivity requires a Ledger module on the stack")
	}

	// Resolve the ledger's effective version through the same path the module
	// reconcilers use (module override, stack version, or the referenced
	// Versions file), so the v3 gate also works for versionsFromFile stacks.
	ledgerVersion, err := ResolveModuleVersion(ctx, stack, ledger)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerVersionUnresolved", err.Error())
		// Transient: we could not resolve the ledger version (e.g. a referenced
		// Versions file not yet present). This is an error *resolving* the
		// prerequisite, not a definitive downgrade below v3, so we deliberately do
		// NOT tear down the delegated resources here — flapping the workload on a
		// transient resolution hiccup would be worse than briefly keeping it up.
		return NewPendingError().WithMessage("cannot resolve the ledger version: %s", err.Error())
	}
	hasV3, err := ledgerHasV3(ctx, stack, ledgerVersion)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerV3PreviewUnresolved", err.Error())
		// Transient: the ledger.v3.preview-version Setting could not be read or
		// carries an invalid value. Like an unresolvable module version above, this
		// is an error *resolving* the prerequisite, not a definitive downgrade
		// below v3 — do NOT tear down the delegated resources.
		return NewPendingError().WithMessage("cannot resolve the ledger v3 preview: %s", err.Error())
	}
	if !hasV3 {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerNotV3",
			fmt.Sprintf("connectivity requires a Ledger v3 (found %q and no v3 preview)", ledgerVersion))
		// Hard gate: the ledger version resolved but is not v3 and no v3 preview
		// runs alongside (i.e. a real downgrade). Connectivity binds to the ledger
		// v3 gRPC surface, so the prerequisite no longer holds — tear down the
		// delegated Connectivity + GatewayHTTPAPI before returning pending.
		if err := teardownDelegated(ctx, stack, connectivity); err != nil {
			return err
		}
		return NewPendingError().WithMessage("connectivity requires a Ledger v3")
	}
	if !ledger.IsReady() {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerNotReady", "waiting for the ledger to be ready")
		// Transient: the ledger exists and is v3 but is momentarily not ready.
		// Unlike the hard gates above (LedgerNotFound / LedgerNotV3), the
		// prerequisite still holds, so we deliberately do NOT tear down the
		// delegated resources — doing so on every ledger readiness blip would flap
		// the connectivity workload. Leave it in place and just report pending.
		return NewPendingError().WithMessage("waiting for the ledger to be ready")
	}

	// Provision a god-mode ledger credential so connectivity-core can
	// authenticate its gRPC calls. The ledger operator registers the public key
	// on the ledger and distributes the private seed as a Secret in the stack
	// namespace; connectivity-core is wired to it via spec.auth below.
	authKeyID, authSecretName, credReady, err := ensureLedgerCredentials(ctx, stack)
	if err != nil {
		if apimeta.IsNoMatchError(err) || apierrors.IsForbidden(err) {
			setCondition(connectivity, metav1.ConditionFalse, "LedgerCredentialsUnavailable",
				"ledger Credentials API unavailable: "+err.Error())
			return ledgerCredentialsUnavailableError("ledger Credentials API unavailable: %s", err.Error())
		}
		setCondition(connectivity, metav1.ConditionFalse, "LedgerCredentialsFailed", err.Error())
		return err
	}
	if !credReady {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerCredentialsPending",
			"waiting for ledger credentials to be provisioned")
		return ledgerCredentialsPendingError("waiting for ledger credentials to be provisioned")
	}

	// Resolve the connectivity core image through the operator's registry
	// translation so it honours the stack's registry settings (e.g. the
	// ghcr.io -> registry.v2.formance.dev rewrite and pull secrets) instead of
	// the connectivity operator's built-in ghcr.io/...:latest default, which
	// would not be pullable on rewritten registries. The image repository is
	// "connectivity" (the former "connectivity-core" name no longer exists).
	image, err := registries.GetFormanceImage(ctx, stack, "connectivity", version)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "ImageResolveFailed", err.Error())
		return err
	}
	apiImage, err := registries.GetFormanceImage(ctx, stack, "connectivity-api", version)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "APIImageResolveFailed", err.Error())
		return err
	}

	// Collector-aware OTEL configuration, embedded inline in spec.monitoring.
	monitoringConfiguration, err := settings.GetOpenTelemetryConfiguration(ctx, stack.Name, "connectivity")
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "MonitoringResolveFailed", err.Error())
		return err
	}

	// Reuse the single source of truth for the ledger v3 gRPC connection: same
	// service, port and backend TLS material (self-signed CA secret + SNI) that
	// the gateway uses to reach the ledger. The port is resolved from the stack's
	// LedgerConfiguration so a stack overriding spec.cluster.service.grpcPort
	// stays reachable, consistently with the gateway backend.
	backend, err := ledgers.V3GRPCBackendRef(ctx, stack.Name)
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "LedgerBackendResolveFailed", err.Error())
		return err
	}
	ledgerAddress := fmt.Sprintf("%s:%d", backend.Name, backend.Port)

	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(connectivityGVK)
	object.SetNamespace(stack.Name)
	object.SetName(connectivityDelegatedName)
	operation, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), object, func() error {
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
		if err := applyConnectivityMonitoring(object, monitoringConfiguration); err != nil {
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
	})
	if err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return err
	}

	// Expose the connectivity-api through the stack gateway: routes
	// /api/connectivity to the connectivity-api Service (named "connectivity-api")
	// the connectivity operator provisions for the delegated Connectivity.
	if err := gatewayhttpapis.Create(ctx, connectivity,
		gatewayhttpapis.WithRules(gatewayhttpapis.RuleSecuredWithBackend("", connectivityAPIBackendRef())),
	); err != nil {
		setCondition(connectivity, metav1.ConditionFalse, "GatewayReconcileFailed", err.Error())
		return err
	}
	if operation != controllerutil.OperationResultNone {
		message := "waiting for the delegated Connectivity to observe its updated specification"
		setCondition(connectivity, metav1.ConditionFalse, "ConnectivityPending", message)
		return NewPendingError().WithMessage("%s", message)
	}

	ready, message := connectivityResourceReady(object)
	if !ready {
		setCondition(connectivity, metav1.ConditionFalse, "ConnectivityPending", message)
		return NewPendingError().WithMessage("%s", message)
	}

	setCondition(connectivity, metav1.ConditionTrue, "Ready", "Connectivity is ready")
	return nil
}

// teardownAccessibleResources revokes the resources owned by this operator
// when the external Connectivity API is unavailable. Deleting the delegated CR
// on this path would turn an RBAC capability gap into a hard Forbidden error;
// leave that CR untouched and independently remove the public route and the
// god-mode ledger credential that remain accessible to this controller.
func teardownAccessibleResources(ctx Context, connectivity *v1beta1.Connectivity) error {
	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.SetName(GetObjectName(connectivity.GetStack(), LowerCaseKind(ctx, connectivity)))
	return errors.Join(
		ignoreAbsent(ctx.GetClient().Delete(ctx, httpAPI)),
		deleteLedgerCredentials(ctx, connectivity),
	)
}

func handleUnsatisfiedLedgerRequirement(ctx Context, stack *v1beta1.Stack, connectivity *v1beta1.Connectivity) error {
	if !ledgerGateClosed(ctx, stack) {
		return nil
	}
	if !connectivityAvailable {
		return teardownAccessibleResources(ctx, connectivity)
	}
	return teardownDelegated(ctx, stack, connectivity)
}

// teardownDelegated deletes the delegated Connectivity resource and the
// GatewayHTTPAPI provisioned for the stack. It is invoked when a hard/persistent
// ledger gate closes (the Ledger module was removed, or its version resolved to
// something below v3) so the connectivity workload is not left running and
// exposed through the gateway once its prerequisite no longer holds. There is no
// automatic pruning of these module-owned objects on a normal reconcile
// early-return — controller-runtime garbage collection only removes them when
// the Connectivity CR itself is deleted — so the teardown must be explicit.
//
// Deletes use client.IgnoreNotFound so the helper is idempotent and safe to call
// on every reconcile while the gate stays closed (including when the resources
// were never created). The names/scopes mirror how they are provisioned in
// Reconcile: the delegated Connectivity is named "connectivity" in the stack
// namespace and the GatewayHTTPAPI is cluster-scoped ("<stack>-connectivity").
func teardownDelegated(ctx Context, stack *v1beta1.Stack, connectivity *v1beta1.Connectivity) error {
	delegated := &unstructured.Unstructured{}
	delegated.SetGroupVersionKind(connectivityGVK)
	delegated.SetNamespace(stack.Name)
	delegated.SetName(connectivityDelegatedName)

	httpAPI := &v1beta1.GatewayHTTPAPI{}
	httpAPI.SetName(GetObjectName(connectivity.GetStack(), LowerCaseKind(ctx, connectivity)))

	// Attempt every deletion independently rather than bailing on the first
	// error: a failure to delete one resource must not leave the public gateway
	// route or the god-mode Credentials behind, since the point of the hard
	// teardown is to stop exposing the workload — and its credential — once the
	// ledger prerequisite no longer holds. Deleting the cluster-scoped
	// Credentials also cascades the ledger operator's key deregistration and the
	// distributed private-key Secret, which stack-namespace GC never reclaims.
	return errors.Join(
		ignoreAbsent(ctx.GetClient().Delete(ctx, delegated)),
		ignoreAbsent(ctx.GetClient().Delete(ctx, httpAPI)),
		deleteLedgerCredentials(ctx, connectivity),
	)
}

func ledgerCredentialsPendingError(message string, args ...any) *ApplicationError {
	err := NewPendingError().WithMessage(message, args...)
	if !ledgerCredentialsWatchAvailable {
		return err.WithRequeueAfter(ledgerCredentialsRetryDelay)
	}
	return err
}

func ledgerCredentialsUnavailableError(message string, args ...any) *ApplicationError {
	return NewPendingError().
		WithMessage(message, args...).
		WithRequeueAfter(ledgerCredentialsRetryDelay)
}

// ignoreAbsent drops NotFound and NoMatch (the whole CRD is not installed)
// errors, so a teardown delete is a no-op when the resource — or its API — is
// already gone. This matters on the capability-unavailable path, where the
// connectivity CRD may have been removed after startup.
func ignoreAbsent(err error) error {
	if apimeta.IsNoMatchError(err) {
		return nil
	}
	return client.IgnoreNotFound(err)
}

// ledgerGateClosed reports whether the ledger prerequisite is definitively gone
// — the module was removed, or resolves to a non-v3 version with no v3 preview
// running alongside — which are the hard gates that warrant tearing down the
// delegated resources. It mirrors the gate decisions in Reconcile and
// deliberately returns false on transient states (an unresolvable version or
// preview Setting, a not-yet-ready ledger) and on any lookup error, so the
// workload is never flapped on a blip.
func ledgerGateClosed(ctx Context, stack *v1beta1.Stack) bool {
	ledger, err := getStackLedger(ctx, stack.Name)
	if err != nil {
		return false
	}
	if ledger == nil {
		return true
	}
	ledgerVersion, err := ResolveModuleVersion(ctx, stack, ledger)
	if err != nil {
		return false
	}
	hasV3, err := ledgerHasV3(ctx, stack, ledgerVersion)
	if err != nil {
		return false
	}
	return !hasV3
}

// connectivityAPIBackendRef points the gateway at the connectivity-api Service
// the connectivity operator provisions for the delegated Connectivity. It is
// named "<delegated-name>-api", i.e. "connectivity-api".
func connectivityAPIBackendRef() v1beta1.GatewayBackendRef {
	return v1beta1.GatewayBackendRef{
		Name: connectivityDelegatedName + "-api",
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

// applyConnectivityMonitoring sets spec.monitoring from the resolved OTEL
// configuration, or prunes it when telemetry is disabled (idempotent).
func applyConnectivityMonitoring(object *unstructured.Unstructured, configuration *settings.OpenTelemetryConfiguration) error {
	monitoring := connectivityMonitoringSpec(configuration)
	if monitoring == nil {
		unstructured.RemoveNestedField(object.Object, "spec", "monitoring")
		return nil
	}
	return unstructured.SetNestedMap(object.Object, monitoring, "spec", "monitoring")
}

// connectivityMonitoringSpec maps the OTEL configuration to the connectivity
// MonitoringConfig JSON shape, mirroring the Ledger v3 mapping.
func connectivityMonitoringSpec(configuration *settings.OpenTelemetryConfiguration) map[string]any {
	if configuration == nil {
		return nil
	}

	monitoring := map[string]any{
		"serviceName": "connectivity",
	}
	if len(configuration.Attributes) > 0 {
		attributes := make([]string, 0, len(configuration.Attributes))
		for key, value := range configuration.Attributes {
			// GetOpenTelemetryConfiguration injects pod-name=$(POD_NAME), which
			// only resolves when a downward-API POD_NAME env var is defined ahead
			// of OTEL_RESOURCE_ATTRIBUTES on the workload. Unlike this operator's
			// own env-var path (settings.otelEnvVars/collectorEnvVars), the
			// connectivity operator emits OTEL_RESOURCE_ATTRIBUTES verbatim from
			// spec.monitoring.attributes and defines no such env var, so any
			// $(...) placeholder would surface literally in the telemetry. Forward
			// only attributes with resolvable (literal) values.
			if strings.Contains(value, "$(") {
				continue
			}
			attributes = append(attributes, key+"="+value)
		}
		if len(attributes) > 0 {
			slices.Sort(attributes)
			monitoring["attributes"] = strings.Join(attributes, ",")
		}
	}

	signal := func(cfg *settings.OpenTelemetrySignalConfiguration, extra map[string]any) map[string]any {
		out := map[string]any{
			"enabled":  true,
			"exporter": "otlp",
			"endpoint": cfg.Endpoint,
			"port":     cfg.Port,
			"insecure": strconv.FormatBool(cfg.Insecure),
			"mode":     cfg.Mode,
		}
		maps.Copy(out, extra)
		return out
	}

	if configuration.Traces != nil {
		monitoring["traces"] = signal(configuration.Traces, map[string]any{"batch": "true"})
	}
	if configuration.Metrics != nil {
		monitoring["metrics"] = signal(configuration.Metrics, map[string]any{"runtime": true})
	}
	if configuration.Logs != nil {
		monitoring["logs"] = signal(configuration.Logs, nil)
	}
	return monitoring
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

// The Credentials is cluster-scoped and owned by the Stack, so neither GC on
// Connectivity deletion nor namespace deletion reclaims it — hence this finalizer.
// Deleting it cascades in the ledger operator (key + distributed Secret).
func deleteLedgerCredentials(ctx Context, connectivity *v1beta1.Connectivity) error {
	cred := &unstructured.Unstructured{}
	cred.SetGroupVersionKind(ledgerCredentialsGVK)
	cred.SetName("connectivity-" + connectivity.GetStack())
	// IsNoMatchError: without the ledger CRD, Delete errors and would block deletion forever.
	if err := ctx.GetClient().Delete(ctx, cred); client.IgnoreNotFound(err) != nil && !apimeta.IsNoMatchError(err) {
		return err
	}
	return nil
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

// withLedgerCredentialsWatch registers a watch on the cluster-scoped ledger
// Credentials so that changes to the connectivity-<stack> Credentials re-trigger
// the owning Connectivity's reconcile.
//
// Reconcile returns a PendingError while the Credentials is not yet Ready
// (LedgerCredentialsPending). This watch lets the cluster-scoped Credentials,
// which is owned by the Stack rather than the namespaced Connectivity, re-trigger
// reconciliation when the ledger operator marks it Ready. If the watch is not
// available, PendingError carries a bounded polling delay instead. API-level
// failures such as a removed CRD or lost RBAC permission always use that polling
// fallback because the watch cannot observe their recovery.
//
// The Credentials is an external, unstructured GVK, so this uses a raw builder
// watch (mirroring withConnectivityClusterWatch) rather than WithWatch, which
// reflect-instantiates a typed WATCHED and cannot carry the GVK an unstructured
// watch needs. The watch is gated on the Credentials CRD being installed so
// controller setup never fails when the ledger operator is absent.
func withLedgerCredentialsWatch() ReconcilerOption[*v1beta1.Connectivity] {
	return func(options *ReconcilerOptions[*v1beta1.Connectivity]) {
		options.Raws = append(options.Raws, func(ctx Context, b *builder.Builder) error {
			crds := &apiextensionsv1.CustomResourceDefinitionList{}
			if err := ctx.GetAPIReader().List(ctx, crds); err != nil {
				ledgerCredentialsWatchAvailable = false
				log.FromContext(ctx).Info("ledger Credentials watch unavailable; continuing without it", "error", err)
				return nil
			}
			ledgerCredentialsWatchAvailable = watchLedgerCredentials(ctx, b, crds)
			return nil
		})
	}
}

func watchLedgerCredentials(ctx Context, b *builder.Builder, crds *apiextensionsv1.CustomResourceDefinitionList) bool {
	for _, crd := range crds.Items {
		if crd.Spec.Group != ledgerCredentialsGVK.Group || crd.Spec.Names.Kind != ledgerCredentialsGVK.Kind {
			continue
		}
		for _, version := range crd.Spec.Versions {
			if version.Name != ledgerCredentialsGVK.Version || !version.Served {
				continue
			}
			credentials := &unstructured.UnstructuredList{}
			credentials.SetGroupVersionKind(ledgerCredentialsGVK.GroupVersion().WithKind(ledgerCredentialsGVK.Kind + "List"))
			if err := ctx.GetAPIReader().List(ctx, credentials, client.Limit(1)); err != nil {
				log.FromContext(ctx).Info("ledger Credentials are inaccessible; continuing without watch", "gvk", ledgerCredentialsGVK, "error", err)
				return false
			}
			if !canWatchLedgerCredentials(ctx, crd.Spec.Names.Plural) {
				return false
			}
			cred := &unstructured.Unstructured{}
			cred.SetGroupVersionKind(ledgerCredentialsGVK)
			// The raw builder callback receives a Context wrapping the manager;
			// its client is long-lived, so it is safe to reuse for the mapping.
			b.Watches(cred, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, object client.Object) []reconcile.Request {
				return mapLedgerCredentialsToConnectivity(ctx, object)
			}))
			log.FromContext(ctx).Info("ledger Credentials watch registered", "gvk", ledgerCredentialsGVK)
			return true
		}
	}
	log.FromContext(ctx).Info("ledger Credentials CRD is not available; connectivity will not watch credentials", "gvk", ledgerCredentialsGVK)
	return false
}

func canWatchLedgerCredentials(ctx Context, resource string) bool {
	for _, verb := range []string{"list", "watch"} {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:    ledgerCredentialsGVK.Group,
					Version:  ledgerCredentialsGVK.Version,
					Resource: resource,
					Verb:     verb,
				},
			},
		}
		if err := ctx.GetClient().Create(ctx, review); err != nil {
			log.FromContext(ctx).Info("ledger Credentials access review failed; continuing without watch", "verb", verb, "error", err)
			return false
		}
		if !review.Status.Allowed {
			log.FromContext(ctx).Info("ledger Credentials watch permission is unavailable; continuing without watch", "verb", verb, "reason", review.Status.Reason)
			return false
		}
	}
	return true
}

// mapLedgerCredentialsToConnectivity maps a ledger Credentials event back to the
// Connectivity module(s) that provisioned it. ensureLedgerCredentials names the
// Credentials "connectivity-<stack>", so the stack is derived from that name and
// the Connectivity in that stack is enqueued (there is one Connectivity per
// stack). It returns nothing when the name is not a connectivity-owned
// Credentials or when the stack has no Connectivity.
func mapLedgerCredentialsToConnectivity(ctx Context, object client.Object) []reconcile.Request {
	stack, ok := connectivityStackFromCredentials(object)
	if !ok {
		return nil
	}
	list := &v1beta1.ConnectivityList{}
	if err := ctx.GetClient().List(ctx, list, client.MatchingFields{"stack": stack}); err != nil {
		log.FromContext(ctx).Error(err, "listing Connectivity for ledger Credentials watch", "stack", stack)
		return nil
	}
	items := make([]*v1beta1.Connectivity, len(list.Items))
	for i := range list.Items {
		items[i] = &list.Items[i]
	}
	return MapObjectToReconcileRequests(items...)
}

// connectivityStackFromCredentials derives the stack a connectivity-owned ledger
// Credentials belongs to. ensureLedgerCredentials names it "connectivity-<stack>"
// (and stamps spec.selector.matchLabels["formance.com/stack"]); requiring the
// "connectivity-" name prefix keeps unrelated ledger Credentials from enqueueing
// a Connectivity.
func connectivityStackFromCredentials(object client.Object) (string, bool) {
	stack, ok := strings.CutPrefix(object.GetName(), "connectivity-")
	if !ok || stack == "" {
		return "", false
	}
	return stack, true
}

func connectivityReconcilerOptions() []ReconcilerOption[*v1beta1.Connectivity] {
	return []ReconcilerOption[*v1beta1.Connectivity]{
		WithFinalizer[*v1beta1.Connectivity]("delete-ledger-credentials", deleteLedgerCredentials),
		WithOwn[*v1beta1.Connectivity](&v1beta1.GatewayHTTPAPI{}),
		withConnectivityClusterWatch(),
		withLedgerCredentialsWatch(),
		WithWatchSettings[*v1beta1.Connectivity](),
		WithUnsatisfiedRequirementsHandler(handleUnsatisfiedLedgerRequirement),
	}
}

func init() {
	Init(WithModuleReconciler(Reconcile,
		Requirements(
			Require(&v1beta1.Ledger{}, VersionAtLeast(v1beta1.LedgerV3Version)),
		),
		connectivityReconcilerOptions()...,
	))
}
