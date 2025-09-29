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
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reconcile is the manifest-based reconciler for Ledger
func Reconcile(ctx core.Context, stack *v1beta1.Stack, ledger *v1beta1.Ledger, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "ledger", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Create database
	database, err := databases.Create(ctx, stack, ledger)
	if err != nil {
		return err
	}

	if !database.Status.Ready {
		return core.NewPendingError().WithMessage("database not ready")
	}

	// 3. Create gateway API
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_healthcheck"
	}

	if err := gatewayhttpapis.Create(ctx, ledger, gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint)); err != nil {
		return err
	}

	// 4. Load streams (from manifest config)
	if manifest.Spec.Streams.Ingestion != "" {
		if err := benthosstreams.LoadFromFileSystem(ctx, benthos.Streams, ledger, manifest.Spec.Streams.Ingestion, "ingestion"); err != nil {
			return err
		}
	}

	if manifest.Spec.Streams.Reindex != "" {
		if err := benthosstreams.LoadFromFileSystem(ctx, reindexStreams, ledger, manifest.Spec.Streams.Reindex, "reindex"); err != nil {
			return err
		}
	}

	// 5. Handle reindex cronjob if Search dependency exists
	hasDependency, err := core.HasDependency(ctx, stack.Name, &v1beta1.Search{})
	if err != nil {
		return err
	}
	if hasDependency {
		_, err = createReindexCronJob(ctx, ledger)
		if err != nil {
			return err
		}
	} else {
		err = deleteReindexCronJob(ctx, ledger)
		if err != nil {
			return err
		}
	}

	// 6. Handle migration if needed
	isV2 := false
	if !semver.IsValid(version) || semver.Compare(version, "v2.0.0-alpha") > 0 {
		isV2 = true
	}

	if isV2 && manifest.Spec.Migration.Enabled && databases.GetSavedModuleVersion(database) != version {
		// Get image configuration
		imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "ledger", version)
		if err != nil {
			return err
		}

		// Run migration
		err = databases.Migrate(
			ctx,
			stack,
			ledger,
			imageConfiguration,
			database,
			jobs.Mutator(func(t *batchv1.Job) error {
				if core.IsLower(version, "v2.0.0-rc.6") {
					t.Spec.Template.Spec.Containers[0].Command = []string{"buckets", "upgrade-all"}
				}
				t.Spec.Template.Spec.Containers[0].Env = append(t.Spec.Template.Spec.Containers[0].Env,
					core.Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))
				return nil
			}),
			jobs.PreCreate(func() error {
				list := &appsv1.DeploymentList{}
				if err := ctx.GetClient().List(ctx, list, client.InNamespace(stack.Name)); err != nil {
					return err
				}

				for _, item := range list.Items {
					if controller := metav1.GetControllerOf(&item); controller != nil && controller.UID == ledger.GetUID() {
						if err := ctx.GetClient().Delete(ctx, &item); err != nil {
							return err
						}
					}
				}

				return nil
			}),
		)

		if err != nil {
			// Check if we should continue on error
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
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "ledger", version)
	if err != nil {
		return fmt.Errorf("getting image configuration: %w", err)
	}

	// 8. Build additional environment variables (auth, gateway, broker)
	additionalEnv := make([]corev1.EnvVar, 0)

	// Auth env vars
	authEnvVars, err := auths.ProtectedAPIEnvVarsWithPrefix(ctx, stack, "ledger", ledger.Spec.Auth, manifest.Spec.EnvVarPrefix)
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
	if t, err := brokertopics.Find(ctx, stack, "ledger"); err != nil {
		return err
	} else if t != nil && t.Status.Ready {
		broker := &v1beta1.Broker{}
		if err := ctx.GetClient().Get(ctx, types.NamespacedName{Name: stack.Name}, broker); err != nil {
			return err
		}
		if broker.Status.Ready {
			brokerEnvVars, err := brokers.GetEnvVarsWithPrefix(ctx, broker.Status.URI, stack.Name, "ledger", manifest.Spec.EnvVarPrefix)
			if err != nil {
				return err
			}
			additionalEnv = append(additionalEnv, brokerEnvVars...)
			additionalEnv = append(additionalEnv, brokers.GetPublisherEnvVars(stack, broker, "ledger", manifest.Spec.EnvVarPrefix)...)
		}
	}

	// 9. Apply manifest (all the deployment logic is here!)
	return manifests.Apply(ctx, manifests.ManifestContext{
		Stack:              stack,
		Module:             ledger,
		Database:           database,
		Version:            version,
		ImageConfiguration: imageConfiguration,
		AdditionalEnv:      additionalEnv,
	}, manifest)
}
