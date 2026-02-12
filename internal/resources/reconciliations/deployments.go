package reconciliations

import (
	"fmt"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/applications"
	"github.com/formancehq/operator/internal/resources/authclients"
	"github.com/formancehq/operator/internal/resources/auths"
	"github.com/formancehq/operator/internal/resources/brokers"
	"github.com/formancehq/operator/internal/resources/databases"
	"github.com/formancehq/operator/internal/resources/gateways"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
)

const (
	DeploymentTypeAPI              = "api"
	DeploymentTypeWorkerIngestion  = "worker-ingestion"
	DeploymentTypeWorkerMatching   = "worker-matching"
)

func commonEnvVars(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
) ([]v1.EnvVar, error) {
	brokerURI, err := settings.RequireURL(ctx, stack.Name, "broker", "dsn")
	if err != nil {
		return nil, err
	}
	if brokerURI == nil {
		return nil, errors.New("missing broker configuration")
	}

	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, reconciliation), " ")
	if err != nil {
		return nil, err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return nil, err
	}

	brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, brokerURI, stack.Name, "reconciliation")
	if err != nil {
		return nil, err
	}

	env = append(env, gatewayEnv...)
	env = append(env, core.GetDevEnvVars(stack, reconciliation)...)
	env = append(env, postgresEnvVar...)
	env = append(env, core.Env("POSTGRES_DATABASE_NAME", "$(POSTGRES_DATABASE)"))
	env = append(env, authclients.GetEnvVars(authClient)...)
	env = append(env, brokerEnvVar...)

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "reconciliation", reconciliation.Spec.Auth)
	if err != nil {
		return nil, err
	}
	env = append(env, authEnvVars...)

	return env, nil
}

func createDeployments(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
	ingestionConsumer *v1beta1.BrokerConsumer,
	matchingConsumer *v1beta1.BrokerConsumer,
	imageConfiguration *registries.ImageConfiguration,
) error {
	// Deploy workers first, then API
	// This ensures workers are ready to process events before API starts accepting requests
	if err := createDeployment(ctx, stack, reconciliation, database, authClient, ingestionConsumer, imageConfiguration, DeploymentTypeWorkerIngestion); err != nil {
		return err
	}

	if err := createDeployment(ctx, stack, reconciliation, database, authClient, matchingConsumer, imageConfiguration, DeploymentTypeWorkerMatching); err != nil {
		return err
	}

	if err := createDeployment(ctx, stack, reconciliation, database, authClient, nil, imageConfiguration, DeploymentTypeAPI); err != nil {
		return err
	}

	return nil
}

func createDeployment(
	ctx core.Context,
	stack *v1beta1.Stack,
	reconciliation *v1beta1.Reconciliation,
	database *v1beta1.Database,
	authClient *v1beta1.AuthClient,
	consumer *v1beta1.BrokerConsumer,
	imageConfiguration *registries.ImageConfiguration,
	deploymentType string,
) error {
	var (
		containerName string
		metaName      string
		args          []string
	)

	switch deploymentType {
	case DeploymentTypeWorkerIngestion:
		containerName = "reconciliation-worker-ingestion"
		metaName = "reconciliation-worker-ingestion"
		args = []string{"worker", "ingestion"}
	case DeploymentTypeWorkerMatching:
		containerName = "reconciliation-worker-matching"
		metaName = "reconciliation-worker-matching"
		args = []string{"worker", "matching"}
	case DeploymentTypeAPI:
		containerName = "reconciliation"
		metaName = "reconciliation"
		args = []string{"serve"}
	default:
		return errors.Errorf("unknown deployment type: %s", deploymentType)
	}

	env, err := commonEnvVars(ctx, stack, reconciliation, database, authClient)
	if err != nil {
		return err
	}

	// Add Elasticsearch env vars for both API and Worker
	esEnvVars, err := settings.GetElasticsearchEnvVars(ctx, stack.Name)
	if err != nil {
		return err
	}
	env = append(env, esEnvVars...)

	// Workers: inject KAFKA_TOPICS from their respective consumer
	if (deploymentType == DeploymentTypeWorkerIngestion || deploymentType == DeploymentTypeWorkerMatching) && consumer != nil {
		topics, err := brokers.GetTopicsEnvVars(ctx, stack, "KAFKA_TOPICS", consumer.Spec.Services...)
		if err != nil {
			return err
		}
		env = append(env, topics...)
	}

	// Ingestion worker: publish to {stack}.reconciliation
	if deploymentType == DeploymentTypeWorkerIngestion {
		env = append(env, core.Env("PUBLISHER_TOPIC_MAPPING", fmt.Sprintf("*:%s.reconciliation", stack.Name)))
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(reconciliation, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: metaName,
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						ServiceAccountName: serviceAccountName,
						Containers: []v1.Container{{
							Name:           containerName,
							Env:            env,
							Image:          imageConfiguration.GetFullImageName(),
							Args:           args,
							Ports:          []v1.ContainerPort{applications.StandardHTTPPort()},
							LivenessProbe:  applications.DefaultLiveness("http"),
							ReadinessProbe: applications.DefaultReadiness("http"),
						}},
					},
				},
			},
		}).
		IsEE().
		Install(ctx)
}

