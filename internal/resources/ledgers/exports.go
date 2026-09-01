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

// HasV3 reports whether the stack effectively runs a Ledger v3 workload:
// either the resolved ledger module version is itself v3 (or later), or a v2
// ledger runs the v3 preview alongside via the ledger.v3.preview-version
// Setting. The preview lookup mirrors the ledger reconciler's own decision —
// including ignoring the Setting when the Ledger Operator CRD is unavailable
// and rejecting values at or below the v3 threshold — so consumers of the v3
// gRPC surface always agree with the ledger module on whether a v3 cluster
// exists.
func HasV3(ctx core.Context, stack *v1beta1.Stack, ledgerVersion string) (bool, error) {
	if isLedgerV3(ledgerVersion) {
		return true, nil
	}
	previewVersion, err := ledgerV3PreviewVersion(ctx, stack)
	if err != nil {
		return false, err
	}
	return previewVersion != "", nil
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
