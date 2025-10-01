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

package payments

import (
	"fmt"
	"net/http"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/manifests"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/benthosstreams"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/brokertopics"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/jobs"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/search/benthos"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Reconcile is the manifest-based reconciler for Payments
func Reconcile(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "payments", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Create database
	database, err := databases.Create(ctx, stack, payments)
	if err != nil {
		return err
	}

	if !database.Status.Ready {
		return core.NewPendingError().WithMessage("database not ready")
	}

	// 3. Handle migration
	if manifest.Spec.Migration.Enabled && databases.GetSavedModuleVersion(database) != version {
		imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "payments", version)
		if err != nil {
			return err
		}

		// Get encryption key
		encryptionKey, err := getEncryptionKey(ctx, payments)
		if err != nil {
			return err
		}

		// Run migration
		err = databases.Migrate(ctx, stack, payments, imageConfiguration, database,
			jobs.WithEnvVars(core.Env("CONFIG_ENCRYPTION_KEY", encryptionKey)),
		)

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

	// 4. Load streams (from manifest config)
	if manifest.Spec.Streams.Ingestion != "" {
		if err := benthosstreams.LoadFromFileSystem(ctx, benthos.Streams, payments, manifest.Spec.Streams.Ingestion, "ingestion"); err != nil {
			return err
		}
	}

	// 5. Create gateway API
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_health"
	}

	if err := gatewayhttpapis.Create(ctx, payments,
		gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint),
		gatewayhttpapis.WithRules(
			v1beta1.GatewayHTTPAPIRule{
				Path:    "/connectors/webhooks",
				Methods: []string{http.MethodPost},
				Secured: true,
			},
			gatewayhttpapis.RuleSecured(),
		)); err != nil {
		return err
	}

	// 6. Get image configuration
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "payments", version)
	if err != nil {
		return fmt.Errorf("getting image configuration: %w", err)
	}

	// 7. Build additional environment variables (auth, gateway, broker, temporal)
	additionalEnv := make([]corev1.EnvVar, 0)

	// Auth env vars
	authEnvVars, err := auths.ProtectedAPIEnvVarsWithPrefix(ctx, stack, "payments", payments.Spec.Auth, manifest.Spec.EnvVarPrefix)
	if err != nil {
		return err
	}
	additionalEnv = append(additionalEnv, authEnvVars...)

	// Gateway env vars
	gatewayEnv, err := gateways.EnvVarsIfEnabledWithPrefix(ctx, stack.Name, manifest.Spec.EnvVarPrefix)
	if err != nil {
		return err
	}
	additionalEnv = append(additionalEnv, gatewayEnv...)

	// Broker env vars
	if t, err := brokertopics.Find(ctx, stack, "payments"); err != nil {
		return err
	} else if t != nil && t.Status.Ready {
		broker := &v1beta1.Broker{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: stack.Name}, broker); err != nil {
			return err
		}
		if broker.Status.Ready {
			brokerEnvVars, err := brokers.GetEnvVarsWithPrefix(ctx, broker.Status.URI, stack.Name, "payments", manifest.Spec.EnvVarPrefix)
			if err != nil {
				return err
			}
			additionalEnv = append(additionalEnv, brokerEnvVars...)
			additionalEnv = append(additionalEnv, brokers.GetPublisherEnvVars(stack, broker, "payments", manifest.Spec.EnvVarPrefix)...)
		}
	}

	// Temporal env vars (specific to payments)
	temporalEnv, err := temporalEnvVars(ctx, stack, payments)
	if err != nil {
		return err
	}
	additionalEnv = append(additionalEnv, temporalEnv...)

	// Encryption key env var
	encryptionKey, err := getEncryptionKey(ctx, payments)
	if err != nil {
		return err
	}
	additionalEnv = append(additionalEnv, core.Env("CONFIG_ENCRYPTION_KEY", encryptionKey))

	// 8. Apply manifest (all deployment logic is here!)
	return manifests.Apply(ctx, manifests.ManifestContext{
		Stack:              stack,
		Module:             payments,
		Database:           database,
		Version:            version,
		ImageConfiguration: imageConfiguration,
		AdditionalEnv:      additionalEnv,
	}, manifest)
}
