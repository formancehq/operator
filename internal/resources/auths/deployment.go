package auths

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/formancehq/operator/internal/resources/registries"

	"github.com/formancehq/go-libs/v2/collectionutils"
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/resourcereferences"
	"github.com/formancehq/operator/internal/resources/settings"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func HashFromHash(o ...string) string {
	digest := sha256.New()
	for _, h := range o {
		if err := json.NewEncoder(digest).Encode(h); err != nil {
			panic(err)
		}
	}
	return base64.StdEncoding.EncodeToString(digest.Sum(nil))
}

func createDeployment(ctx Context, stack *v1beta1.Stack, auth *v1beta1.Auth, database *v1beta1.Database,
	configMap *corev1.ConfigMap, imageConfiguration *registries.ImageConfiguration, version string, clients []*v1beta1.AuthClient) error {
	annotations := map[string]string{
		"config-hash": HashFromConfigMaps(configMap),
	}

	env := make([]corev1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, LowerCamelCaseKind(ctx, auth), " ")
	if err != nil {
		return err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return err
	}

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return err
	}

	env = append(env, gatewayEnv...)
	env = append(env, GetDevEnvVars(stack, auth)...)
	env = append(env, postgresEnvVar...)
	env = append(env, Env("CONFIG", "/config/config.yaml"))

	authUrl, err := getUrl(ctx, stack.Name)
	if err != nil {
		return err
	}
	env = append(env, Env("BASE_URL", authUrl))

	if auth.Spec.SigningKey != "" && auth.Spec.SigningKeyFromSecret != nil {
		return fmt.Errorf("cannot specify signing key using both .spec.signingKey and .spec.signingKeyFromSecret fields")
	}
	if auth.Spec.SigningKey != "" {
		env = append(env, Env("SIGNING_KEY", auth.Spec.SigningKey))
	}
	if auth.Spec.SigningKeyFromSecret != nil {
		env = append(env, corev1.EnvVar{
			Name: "SIGNING_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: auth.Spec.SigningKeyFromSecret,
			},
		})
	}
	if auth.Spec.DelegatedOIDCServer != nil {
		if auth.Spec.DelegatedOIDCServer.ClientSecret != "" && auth.Spec.DelegatedOIDCServer.ClientSecretFromSecret != nil {
			return fmt.Errorf("cannot specify signing key using both .spec.DelegatedOIDCServer.ClientSecret and .spec.DelegatedOIDCServer.ClientSecretFromSecret fields")
		}
		env = append(env,
			Env("DELEGATED_CLIENT_ID", auth.Spec.DelegatedOIDCServer.ClientID),
			Env("DELEGATED_ISSUER", auth.Spec.DelegatedOIDCServer.Issuer),
		)

		if auth.Spec.DelegatedOIDCServer.ClientSecret != "" {
			env = append(env,
				Env("DELEGATED_CLIENT_SECRET", auth.Spec.DelegatedOIDCServer.ClientSecret),
			)
		}

		ressourceRefName := "auth-delegated-client-secret"
		if auth.Spec.DelegatedOIDCServer.ClientSecretFromSecret != nil {
			clientSecretResourceRef, err := resourcereferences.Create(ctx, auth, ressourceRefName, auth.Spec.DelegatedOIDCServer.ClientSecretFromSecret.Name, &corev1.Secret{})
			if err != nil {
				return err
			}

			annotations[ressourceRefName] = clientSecretResourceRef.Status.Hash

			env = append(env, corev1.EnvVar{
				Name: "DELEGATED_CLIENT_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: auth.Spec.DelegatedOIDCServer.ClientSecretFromSecret,
				},
			})
		} else {
			if err := resourcereferences.Delete(ctx, auth, ressourceRefName); err != nil {
				return err
			}
		}
	}

	if IsGreaterOrEqual(version, "v2.1.0") {
		hashList := make([]string, 0)
		hashList = collectionutils.Reduce(clients, func(acc []string, from *v1beta1.AuthClient) []string {
			if from.Spec.SecretFromSecret != nil {
				acc = append(acc, from.Status.Hash)
			}
			return acc
		}, hashList)
		annotations["auth-clients-secrets"] = HashFromHash(hashList...)
		for _, client := range clients {
			if client.Spec.SecretFromSecret != nil {
				env = append(env, AuthClientSecretToEnvVars(client))
			}
		}
	}

	if stack.Spec.Dev || auth.Spec.Dev {
		env = append(env, Env("CAOS_OIDC_DEV", "1"))
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(auth, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "auth",
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: annotations,
					},
					Spec: corev1.PodSpec{
						ImagePullSecrets: imageConfiguration.PullSecrets,
						Containers: []corev1.Container{{
							Name:  "auth",
							Args:  []string{"serve"},
							Env:   env,
							Image: imageConfiguration.GetFullImageName(),
							VolumeMounts: []corev1.VolumeMount{
								NewVolumeMount("config", "/config", true),
							},
							Ports:         []corev1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe: applications.DefaultLiveness("http"),
						}},
						Volumes: []corev1.Volume{
							NewVolumeFromConfigMap("config", configMap),
						},
						ServiceAccountName: serviceAccountName,
					},
				},
			},
		}).
		IsEE().
		Install(ctx)
}

func getUrl(ctx Context, stackName string) (string, error) {
	gateway := &v1beta1.Gateway{}
	ok, err := GetIfExists(ctx, stackName, gateway)
	if err != nil {
		return "", err
	}

	if ok {
		return gateways.URL(gateway) + "/api/auth", nil
	}

	return "http://auth:8080", nil
}
