package ledgers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

const (
	ledgerV3TLSCertificateDuration    = "87600h"
	ledgerV3TLSCertificateRenewBefore = "720h"
	ledgerV3TLSCASecretKey            = "ca.crt"
	ledgerV3TLSCAHashAnnotation       = "formance.com/ledger-v3-ca-sha256"
)

var (
	ledgerV3IssuerGVK = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Issuer",
	}
	ledgerV3CertificateGVK = schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Certificate",
	}
)

//+kubebuilder:rbac:groups=cert-manager.io,resources=issuers;certificates,verbs=get;list;watch;create;update;patch;delete

func newLedgerV3Resource(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(gvk)
	return resource
}

func createOrUpdateV3TLSResources(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, preview bool) (bool, string, string, error) {
	if !ledgerV3CertManagerAvailable {
		return false, "cert-manager Issuer and Certificate CRDs are not installed", "", nil
	}

	issuer := newLedgerV3Resource(ledgerV3IssuerGVK)
	issuer.SetNamespace(stack.Name)
	issuer.SetName(ledgerV3IssuerName(stack.Name))
	if _, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), issuer, func() error {
		setLedgerV3TLSResourceMetadata(issuer, stack.Name, preview)
		if err := controllerutil.SetControllerReference(ledger, issuer, ctx.GetScheme()); err != nil {
			return err
		}
		return unstructured.SetNestedMap(issuer.Object, map[string]any{}, "spec", "selfSigned")
	}); err != nil {
		return false, "", "", err
	}

	certificate := newLedgerV3Resource(ledgerV3CertificateGVK)
	certificate.SetNamespace(stack.Name)
	certificate.SetName(ledgerV3TLSName(stack.Name))
	if _, err := controllerutil.CreateOrUpdate(ctx, ctx.GetClient(), certificate, func() error {
		setLedgerV3TLSResourceMetadata(certificate, stack.Name, preview)
		if err := controllerutil.SetControllerReference(ledger, certificate, ctx.GetScheme()); err != nil {
			return err
		}
		certificate.Object["spec"] = ledgerV3CertificateSpec(stack.Name)
		return nil
	}); err != nil {
		return false, "", "", err
	}

	ready, message, err := ledgerV3CertificateReady(certificate)
	if err != nil || !ready {
		return ready, message, "", err
	}

	secret := &corev1.Secret{}
	err = ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: ledgerV3TLSName(stack.Name)}, secret)
	if apierrors.IsNotFound(err) {
		return false, fmt.Sprintf("TLS Secret %s/%s does not exist", stack.Name, ledgerV3TLSName(stack.Name)), "", nil
	}
	if err != nil {
		return false, "", "", err
	}
	for _, key := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey, ledgerV3TLSCASecretKey} {
		if len(secret.Data[key]) == 0 {
			return false, fmt.Sprintf("TLS Secret %s/%s is missing %q", stack.Name, secret.Name, key), "", nil
		}
	}

	return true, "Certificate and TLS Secret are ready", ledgerV3TLSCAHash(secret.Data[ledgerV3TLSCASecretKey]), nil
}

func ledgerV3TLSCAHash(ca []byte) string {
	sum := sha256.Sum256(ca)
	return hex.EncodeToString(sum[:])
}

func setLedgerV3TLSResourceMetadata(resource *unstructured.Unstructured, stackName string, preview bool) {
	labels := resource.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[v1beta1.StackLabel] = stackName
	if preview {
		labels[ledgerV3PreviewLabel] = "true"
	} else {
		delete(labels, ledgerV3PreviewLabel)
	}
	resource.SetLabels(labels)
}

func ledgerV3IssuerName(stackName string) string {
	return stackName + "-selfsigned"
}

func ledgerV3TLSName(stackName string) string {
	return stackName + "-tls"
}

func ledgerV3CertificateSpec(stackName string) map[string]any {
	serviceName := "ledger-" + stackName
	return map[string]any{
		"secretName":  ledgerV3TLSName(stackName),
		"duration":    ledgerV3TLSCertificateDuration,
		"renewBefore": ledgerV3TLSCertificateRenewBefore,
		"isCA":        true,
		"secretTemplate": map[string]any{
			"labels": map[string]any{
				v1beta1.GatewayBackendTLSSecretLabel: "true",
			},
		},
		"issuerRef": map[string]any{
			"name": ledgerV3IssuerName(stackName),
			"kind": "Issuer",
		},
		"commonName": serviceName + "." + stackName + ".svc.cluster.local",
		"dnsNames":   ledgerV3TLSDNSNames(stackName),
	}
}

func ledgerV3TLSDNSNames(stackName string) []any {
	serviceName := "ledger-" + stackName
	headlessServiceName := serviceName + "-headless"
	dnsNames := make([]any, 0, 16)
	for _, name := range []string{serviceName, serviceName + "-grpc", headlessServiceName, "*." + headlessServiceName} {
		dnsNames = append(dnsNames, ledgerV3ServiceDNSNames(name, stackName)...)
	}
	return dnsNames
}

func ledgerV3ServiceDNSNames(serviceName, namespace string) []any {
	return []any{
		serviceName,
		serviceName + "." + namespace,
		serviceName + "." + namespace + ".svc",
		serviceName + "." + namespace + ".svc.cluster.local",
	}
}

func ledgerV3CertificateReady(certificate *unstructured.Unstructured) (bool, string, error) {
	conditions, found, err := unstructured.NestedSlice(certificate.Object, "status", "conditions")
	if err != nil {
		return false, "", err
	}
	if !found {
		return false, fmt.Sprintf("Certificate %s/%s has no Ready condition", certificate.GetNamespace(), certificate.GetName()), nil
	}
	for _, rawCondition := range conditions {
		condition, ok := rawCondition.(map[string]any)
		if !ok || condition["type"] != "Ready" {
			continue
		}
		if condition["status"] == "True" {
			return true, "Certificate is ready", nil
		}
		message, _ := condition["message"].(string)
		if message == "" {
			message = fmt.Sprintf("Certificate Ready condition is %v", condition["status"])
		}
		return false, message, nil
	}
	return false, fmt.Sprintf("Certificate %s/%s has no Ready condition", certificate.GetNamespace(), certificate.GetName()), nil
}
