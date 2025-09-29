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

package auths

import (
	"fmt"

	. "github.com/formancehq/go-libs/v2/collectionutils"
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/manifests"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/pkg/errors"
)

// Reconcile is the simplified reconciler using Version Manifests
func Reconcile(ctx core.Context, stack *v1beta1.Stack, auth *v1beta1.Auth, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "auth", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Check auth clients reconciliation
	authClients, err := checkAuthClientsReconciliation(ctx, auth)
	if err != nil {
		return err
	}

	// 3. Create configuration ConfigMap
	configMap, err := createConfiguration(ctx, stack, auth, authClients, version)
	if err != nil {
		return errors.Wrap(err, "creating configuration")
	}

	// 4. Create database
	database, err := databases.Create(ctx, stack, auth)
	if err != nil {
		return errors.Wrap(err, "creating database")
	}

	if !database.Status.Ready {
		return core.NewPendingError().WithMessage("database is not ready")
	}

	// 5. Handle migration
	if manifest.Spec.Migration.Enabled && databases.GetSavedModuleVersion(database) != version {
		imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "auth", version)
		if err != nil {
			return errors.Wrap(err, "resolving image configuration")
		}

		err = databases.Migrate(ctx, stack, auth, imageConfiguration, database)

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

	// 6. Get image configuration
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "auth", version)
	if err != nil {
		return errors.Wrap(err, "resolving image configuration")
	}

	// 7. Create deployment (using custom logic due to auth's complex requirements)
	// Note: Auth has special needs (config map, signing key, auth clients)
	// that aren't yet supported by the generic manifest engine
	if err := createDeployment(ctx, stack, auth, database, configMap, imageConfiguration, version, authClients); err != nil {
		return errors.Wrap(err, "creating deployment")
	}

	// 8. Create gateway HTTP API
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_healthcheck"
	}

	if err := gatewayhttpapis.Create(ctx, auth,
		gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint),
		gatewayhttpapis.WithRules(gatewayhttpapis.RuleUnsecured())); err != nil {
		return errors.Wrap(err, "creating http api")
	}

	// 9. Update status
	auth.Status.Clients = Map(authClients, (*v1beta1.AuthClient).GetName)

	return nil
}
