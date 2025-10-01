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

package orchestrations

import (
	"fmt"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/manifests"
	"github.com/formancehq/operator/internal/resources/brokerconsumers"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
)

// Reconcile is the manifest-based reconciler for Orchestration
func Reconcile(ctx core.Context, stack *v1beta1.Stack, orchestration *v1beta1.Orchestration, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "orchestration", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Create database
	database, err := databases.Create(ctx, stack, orchestration)
	if err != nil {
		return err
	}

	if !database.Status.Ready {
		return core.NewPendingError().WithMessage("database not ready")
	}

	// 3. Create auth client (if auth is enabled)
	authClient, err := createAuthClient(ctx, stack, orchestration)
	if err != nil {
		return err
	}

	// 4. Create broker consumer
	consumer, err := brokerconsumers.CreateOrUpdateOnAllServices(ctx, orchestration)
	if err != nil {
		return err
	}

	if !consumer.Status.Ready {
		return core.NewPendingError().WithMessage("waiting for consumers to be ready")
	}

	// 5. Create gateway HTTP APIs
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_healthcheck"
	}

	if err := gatewayhttpapis.Create(ctx, orchestration,
		gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint)); err != nil {
		return err
	}

	// 6. Handle migration
	if manifest.Spec.Migration.Enabled && databases.GetSavedModuleVersion(database) != version {
		imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "orchestration", version)
		if err != nil {
			return err
		}

		err = databases.Migrate(ctx, stack, orchestration, imageConfiguration, database)

		if err != nil {
			if manifest.Spec.Migration.Strategy == "continue-on-error" {
				if core.IsApplicationError(err) {
					// Continue with deployment despite migration error
					// TODO: Add proper logging here
				} else {
					return err
				}
			} else {
				return err
			}
		}

		if err := databases.SaveModuleVersion(ctx, database, version); err != nil {
			return fmt.Errorf("saving module version: %w", err)
		}
	}

	// 7. Get image configuration
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "orchestration", version)
	if err != nil {
		return err
	}

	// 8. Create deployment (using custom logic due to orchestration's complex requirements)
	// Note: Orchestration has special needs (auth client, broker consumer, temporal config)
	// that aren't yet supported by the generic manifest engine
	return createDeployment(ctx, stack, orchestration, database, authClient, consumer, imageConfiguration)
}
