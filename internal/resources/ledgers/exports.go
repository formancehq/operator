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
	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

// IsV3 reports whether the given ledger version resolves to a Ledger v3 (or
// later) version. Consumers that bind to the ledger v3 gRPC surface (e.g. the
// connectivity module) use this to gate their behaviour.
func IsV3(version string) bool {
	return isLedgerV3(version)
}

// V3PreviewActive reports whether the stack has the Ledger v3 preview enabled
// through the ledger.v3.preview-version Setting. The lookup mirrors the ledger
// reconciler's own decision — including ignoring the Setting when the Ledger
// Operator CRD is unavailable and rejecting values below the v3 threshold — so
// consumers of the v3 gRPC surface (e.g. the connectivity module) always agree
// with the ledger module on whether a preview is configured. An active preview
// only means the Setting is in force, not that the preview Cluster exists or
// runs: gate provisioning on V3PreviewReady as well.
func V3PreviewActive(ctx core.Context, stack *v1beta1.Stack) (bool, error) {
	previewVersion, err := ledgerV3PreviewVersion(ctx, stack)
	if err != nil {
		return false, err
	}
	return previewVersion != "", nil
}

// V3PreviewReady reports whether the preview Cluster for the currently
// configured ledger.v3.preview-version is running. It is the signal consumers
// must require before provisioning against the preview's v3 gRPC endpoint:
// status carried by the Ledger CR (aggregate status.ready, or the
// LedgerV3PreviewReady condition) can be stale across Setting changes — a
// v2-only reconcile predating the Setting, or a previous preview surviving a
// rapid remove/re-add. Reading the Cluster itself avoids that: a deleting
// Cluster is not ready, the version annotation ties it to the Setting value
// currently in force, and readiness requires the ledger operator to have
// observed the Cluster's current generation.
func V3PreviewReady(ctx core.Context, stack *v1beta1.Stack) (bool, error) {
	previewVersion, err := ledgerV3PreviewVersion(ctx, stack)
	if err != nil {
		return false, err
	}
	if previewVersion == "" {
		return false, nil
	}
	cluster, exists, err := getV3Cluster(ctx, stack)
	if err != nil {
		return false, err
	}
	if !exists || !isLedgerV3Preview(cluster) || !cluster.GetDeletionTimestamp().IsZero() {
		return false, nil
	}
	if cluster.GetAnnotations()[ledgerV3PreviewVersionAnnotation] != previewVersion {
		return false, nil
	}
	ready, _, err := isV3ClusterReady(cluster)
	if err != nil {
		return false, err
	}
	return ready, nil
}

// V3GRPCBackendRef returns the connection details of the ledger v3 gRPC service
// for the given stack: service name, port, and backend TLS material
// (self-signed CA secret and SNI server name). It is the single source of
// truth for how in-cluster clients reach the ledger v3 gRPC endpoint. The gRPC
// service port is resolved from the stack's LedgerConfiguration
// (spec.cluster.service.grpcPort) exactly as the gateway backend resolves it,
// so stacks overriding the ledger Cluster service port stay reachable through
// this helper; it falls back to the default gRPC port when unset.
func V3GRPCBackendRef(ctx core.Context, stackName string) (v1beta1.GatewayBackendRef, error) {
	baseSpec, err := ledgerV3BaseSpec(ctx, stackName)
	if err != nil {
		return v1beta1.GatewayBackendRef{}, err
	}
	return ledgerV3GRPCBackendRef(stackName, baseSpec.Service.GrpcPort), nil
}
