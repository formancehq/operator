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

package ledgers

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/gatewaygrpcapis"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func ledgerV3PreviewVersion(ctx core.Context, stack *v1beta1.Stack) (string, error) {
	version, err := settings.GetStringOrEmpty(ctx, stack.Name, "ledger", "v3", "preview-version")
	if err != nil {
		return "", err
	}
	if version != "" && !isLedgerV3(version) {
		return "", fmt.Errorf("ledger.v3.preview-version must be greater than %s, got %q", ledgerV3Threshold, version)
	}
	return version, nil
}

func reconcileV3Preview(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	if !ledgerV3ClusterAvailable {
		setLedgerV3PreviewCondition(ledger, metav1.ConditionFalse, "OperatorUnavailable", "Ledger v3 Cluster CRD is not installed")
		return core.NewPendingError().WithMessage("Ledger v3 preview unavailable: Cluster CRD is not installed")
	}

	tlsReady, tlsMessage, err := createOrUpdateV3TLSResources(ctx, stack, ledger, true)
	if err != nil {
		setLedgerV3PreviewCondition(ledger, metav1.ConditionFalse, "TLSReconcileFailed", err.Error())
		return err
	}
	cluster, err := createOrUpdateV3Cluster(ctx, stack, ledger, version, true)
	if err != nil {
		setLedgerV3PreviewCondition(ledger, metav1.ConditionFalse, "ReconcileFailed", err.Error())
		return err
	}
	if !tlsReady {
		if err := core.DeleteIfExists[*v1beta1.GatewayGRPCAPI](ctx, core.GetResourceName(core.GetObjectName(stack.Name, "ledger"))); err != nil {
			return err
		}
		setLedgerV3PreviewCondition(ledger, metav1.ConditionFalse, "TLSCertificatePending", tlsMessage)
		return core.NewPendingError().WithMessage("Ledger v3 preview TLS is not ready: %s", tlsMessage)
	}

	if err := gatewaygrpcapis.Create(ctx, ledger,
		gatewaygrpcapis.WithGRPCServices(ledgerV3PublicGRPCService),
		gatewaygrpcapis.WithPort(ledgerV3GRPCPort),
		gatewaygrpcapis.WithBackendRef(ledgerV3GRPCBackendRef(stack.Name)),
	); err != nil {
		setLedgerV3PreviewCondition(ledger, metav1.ConditionFalse, "GatewayReconcileFailed", err.Error())
		return err
	}

	ready, message, err := isV3ClusterReady(cluster)
	if err != nil {
		return err
	}
	if !ready {
		setLedgerV3PreviewCondition(ledger, metav1.ConditionFalse, "Pending", message)
		return core.NewPendingError().WithMessage("Ledger v3 preview Cluster is not ready: %s", message)
	}

	setLedgerV3PreviewCondition(ledger, metav1.ConditionTrue, "Running", message)
	return nil
}

func ledgerV3HTTPBackendRef(stackName string) v1beta1.GatewayBackendRef {
	return v1beta1.GatewayBackendRef{
		Name: "ledger-" + stackName,
		Port: ledgerV3HTTPPort,
	}
}

func ledgerV3GRPCBackendRef(stackName string) v1beta1.GatewayBackendRef {
	return v1beta1.GatewayBackendRef{
		Name: "ledger-" + stackName,
		Port: ledgerV3GRPCPort,
		TLS: &v1beta1.GatewayBackendTLS{
			SecretName:  ledgerV3TLSName(stackName),
			CASecretKey: ledgerV3TLSCASecretKey,
			ServerName:  "ledger-" + stackName + "." + stackName + ".svc.cluster.local",
		},
	}
}

func setLedgerV3PreviewCondition(ledger *v1beta1.Ledger, status metav1.ConditionStatus, reason, message string) {
	ledger.GetConditions().AppendOrReplace(v1beta1.Condition{
		Type:               ledgerV3PreviewReadyCondition,
		Status:             status,
		ObservedGeneration: ledger.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}, v1beta1.ConditionTypeMatch(ledgerV3PreviewReadyCondition))
}

func isLedgerV3Preview(cluster *unstructured.Unstructured) bool {
	return cluster.GetLabels()[ledgerV3PreviewLabel] == "true"
}

func deleteLedgerV3Preview(ctx core.Context, stack *v1beta1.Stack) error {
	cluster, exists, err := getV3Cluster(ctx, stack)
	if err != nil {
		return err
	}
	if exists && isLedgerV3Preview(cluster) {
		if err := client.IgnoreNotFound(ctx.GetClient().Delete(ctx, cluster)); err != nil {
			return err
		}
	}

	certificate := newLedgerV3Resource(ledgerV3CertificateGVK)
	err = ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: ledgerV3TLSName(stack.Name)}, certificate)
	if err == nil && certificate.GetLabels()[ledgerV3PreviewLabel] == "true" {
		if err := ctx.GetClient().Delete(ctx, certificate); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: stack.Name, Name: ledgerV3TLSName(stack.Name)}}
		if err := ctx.GetClient().Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	issuer := newLedgerV3Resource(ledgerV3IssuerGVK)
	err = ctx.GetClient().Get(ctx, types.NamespacedName{Namespace: stack.Name, Name: ledgerV3IssuerName(stack.Name)}, issuer)
	if err == nil && issuer.GetLabels()[ledgerV3PreviewLabel] == "true" {
		if err := ctx.GetClient().Delete(ctx, issuer); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}
