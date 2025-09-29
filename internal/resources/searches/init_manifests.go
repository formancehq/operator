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

package searches

import (
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/manifests"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/resourcereferences"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// Reconcile is the manifest-based reconciler for Search
func Reconcile(ctx core.Context, stack *v1beta1.Stack, search *v1beta1.Search, version string) error {
	// 1. Load version manifest
	manifest, err := manifests.Load(ctx, "search", version)
	if err != nil {
		return fmt.Errorf("loading manifest for version %s: %w", version, err)
	}

	// 2. Get ElasticSearch URI
	elasticSearchURI, err := settings.RequireURL(ctx, stack.Name, "elasticsearch", "dsn")
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	awsIAMEnabled := serviceAccountName != ""

	var elasticSearchSecretResourceRef *v1beta1.ResourceReference
	if secret := elasticSearchURI.Query().Get("secret"); !awsIAMEnabled && secret != "" {
		elasticSearchSecretResourceRef, err = resourcereferences.Create(ctx, search, "elasticsearch", secret, &corev1.Secret{})
	} else {
		err = resourcereferences.Delete(ctx, search, "elasticsearch")
	}
	if err != nil {
		return err
	}

	// 3. Build environment variables
	env := make([]corev1.EnvVar, 0)
	if awsIAMEnabled {
		env = append(env, core.Env("AWS_IAM_ENABLED", "true"))
	}

	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, search), " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)
	env = append(env, core.GetDevEnvVars(stack, search)...)

	gatewayEnvVars, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return err
	}
	env = append(env, gatewayEnvVars...)

	env = append(env,
		core.Env("OPEN_SEARCH_SERVICE", elasticSearchURI.Host),
		core.Env("OPEN_SEARCH_SCHEME", elasticSearchURI.Scheme),
		core.Env("ES_INDICES", "stacks"),
	)
	if secret := elasticSearchURI.Query().Get("secret"); elasticSearchURI.User != nil || secret != "" {
		if secret == "" {
			password, _ := elasticSearchURI.User.Password()
			env = append(env,
				core.Env("OPEN_SEARCH_USERNAME", elasticSearchURI.User.Username()),
				core.Env("OPEN_SEARCH_PASSWORD", password),
			)
		} else {
			env = append(env,
				core.EnvFromSecret("OPEN_SEARCH_USERNAME", secret, "username"),
				core.EnvFromSecret("OPEN_SEARCH_PASSWORD", secret, "password"),
			)
		}
	}

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "search", search.Spec.Auth)
	if err != nil {
		return err
	}
	env = append(env, authEnvVars...)

	// 4. Get image configuration
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "search", version)
	if err != nil {
		return err
	}

	// 5. Create consumers (search-specific logic)
	if err := createConsumers(ctx, search); err != nil {
		return err
	}

	// 6. Create Benthos instance
	batching := search.Spec.Batching
	if batching == nil {
		batchingMap, err := settings.GetMapOrEmpty(ctx, stack.Name, "search", "batching")
		if err != nil {
			return err
		}

		batching = &v1beta1.Batching{}
		if countString, ok := batchingMap["count"]; ok {
			count, err := strconv.ParseUint(countString, 10, 64)
			if err != nil {
				return err
			}
			batching.Count = int(count)
		}

		if period, ok := batchingMap["period"]; ok {
			batching.Period = period
		}
	}

	_, _, err = core.CreateOrUpdate[*v1beta1.Benthos](ctx, types.NamespacedName{
		Name: core.GetObjectName(stack.Name, "benthos"),
	},
		core.WithController[*v1beta1.Benthos](ctx.GetScheme(), search),
		func(t *v1beta1.Benthos) error {
			t.Spec.Stack = stack.Name
			t.Spec.Batching = batching
			t.Spec.DevProperties = search.Spec.DevProperties
			t.Spec.InitContainers = []corev1.Container{{
				Name:  "init-mapping",
				Image: imageConfiguration.GetFullImageName(),
				Args:  []string{"init-mapping"},
				Env:   env,
			}}
			t.Spec.ImagePullSecrets = imageConfiguration.PullSecrets

			return nil
		},
	)
	if err != nil {
		return err
	}

	// 7. Create deployment
	annotations := map[string]string{}
	if elasticSearchSecretResourceRef != nil {
		annotations["elasticsearch-secret-hash"] = elasticSearchSecretResourceRef.Status.Hash
	}

	err = applications.
		New(search, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "search",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: annotations,
					},
					Spec: corev1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []corev1.Container{{
							Name:          "search",
							Image:         imageConfiguration.GetFullImageName(),
							Ports:         []corev1.ContainerPort{applications.StandardHTTPPort()},
							Env:           env,
							LivenessProbe: applications.DefaultLiveness("http"),
						}},
					},
				},
			},
		}).
		IsEE().
		Install(ctx)
	if err != nil {
		return err
	}

	// 8. Create gateway HTTP API
	healthCheckEndpoint := manifest.Spec.Gateway.HealthCheckEndpoint
	if healthCheckEndpoint == "" {
		healthCheckEndpoint = "_healthcheck"
	}

	if err := gatewayhttpapis.Create(ctx, search,
		gatewayhttpapis.WithHealthCheckEndpoint(healthCheckEndpoint)); err != nil {
		return err
	}

	// 9. Clean legacy consumers (one-time operation)
	if !search.Status.TopicCleaned {
		if err := cleanConsumers(ctx, search); err != nil {
			return fmt.Errorf("failed to clean consumers for search: %w", err)
		}
		search.Status.TopicCleaned = true
	}

	return nil
}
