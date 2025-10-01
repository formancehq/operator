/*
Copyright 2023.

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

package wallets

import (
	"fmt"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/manifests"
	"github.com/formancehq/operator/internal/resources/authclients"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
)

// Reconcile is the simplified reconciler using Version Manifests
func Reconcile(ctx core.Context, stack *v1beta1.Stack, wallets *v1beta1.Wallets, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "wallets", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Create auth client if auth is enabled
	hasAuth, err := core.HasDependency(ctx, wallets.Spec.Stack, &v1beta1.Auth{})
	if err != nil {
		return err
	}
	var authClient *v1beta1.AuthClient
	if hasAuth {
		authClient, err = authclients.Create(ctx, stack, wallets, "wallets", func(spec *v1beta1.AuthClientSpec) {
			spec.Scopes = []string{"ledger:read", "ledger:write"}
		})
		if err != nil {
			return err
		}
	}

	// 3. Create deployment
	// Note: Wallets is simple - no database, no migration, just deployment with auth client
	if err := createDeployment(ctx, stack, wallets, authClient, version); err != nil {
		return err
	}

	// 4. Create gateway HTTP API
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_healthcheck"
	}

	if err := gatewayhttpapis.Create(ctx, wallets,
		gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint)); err != nil {
		return err
	}

	return nil
}
