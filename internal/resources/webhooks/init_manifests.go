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

package webhooks

import (
	"fmt"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/manifests"
	"github.com/formancehq/operator/internal/resources/brokerconsumers"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/pkg/errors"
)

// Reconcile is the simplified reconciler using Version Manifests
func Reconcile(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "webhooks", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Create database
	database, err := databases.Create(ctx, stack, webhooks)
	if err != nil {
		return err
	}

	if !database.Status.Ready {
		return core.NewPendingError().WithMessage("database not ready")
	}

	// 3. Create broker consumer
	consumer, err := brokerconsumers.CreateOrUpdateOnAllServices(ctx, webhooks)
	if err != nil {
		return err
	}

	if !consumer.Status.Ready {
		return core.NewPendingError().WithMessage("waiting for consumer to be ready")
	}

	// 4. Create gateway HTTP API
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_healthcheck"
	}

	if err := gatewayhttpapis.Create(ctx, webhooks,
		gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint)); err != nil {
		return err
	}

	// 5. Handle migration
	if manifest.Spec.Migration.Enabled && databases.GetSavedModuleVersion(database) != version {
		image, err := registries.GetFormanceImage(ctx, stack, "webhooks", version)
		if err != nil {
			return errors.Wrap(err, "resolving image")
		}

		err = databases.Migrate(ctx, stack, webhooks, image, database)

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
			return errors.Wrap(err, "saving module version in database object")
		}
	}

	// 6. Create deployment based on version
	// Note: Webhooks has version-specific architecture (dual vs single deployment)
	if core.IsGreaterOrEqual(version, "v0.7.1") {
		// Single deployment with embedded worker
		if err := createSingleDeployment(ctx, stack, webhooks, database, consumer, version); err != nil {
			return err
		}
	} else {
		// Dual deployment (separate API and worker)
		if err := createDualDeployment(ctx, stack, webhooks, database, consumer, version); err != nil {
			return err
		}
	}

	return nil
}
