package webhooks

import (
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/applications"
	"github.com/formancehq/operator/v3/internal/resources/auths"
	"github.com/formancehq/operator/v3/internal/resources/brokers"
	"github.com/formancehq/operator/v3/internal/resources/databases"
	"github.com/formancehq/operator/v3/internal/resources/gateways"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

const workerDrainAnnotation = "formance.com/webhooks-workers-draining"

func deploymentEnvVars(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database) ([]v1.EnvVar, error) {

	brokerURI, err := settings.RequireURL(ctx, stack.Name, "broker", "dsn")
	if err != nil {
		return nil, err
	}
	if brokerURI == nil {
		return nil, errors.New("missing broker configuration")
	}

	env := make([]v1.EnvVar, 0)
	otlpEnv, err := settings.GetOTELEnvVars(ctx, stack.Name, core.LowerCamelCaseKind(ctx, webhooks), " ")
	if err != nil {
		return nil, err
	}
	env = append(env, otlpEnv...)

	gatewayEnv, err := gateways.EnvVarsIfEnabled(ctx, stack.Name)
	if err != nil {
		return nil, err
	}
	env = append(env, gatewayEnv...)

	env = append(env, core.GetDevEnvVars(stack, webhooks)...)

	authEnvVars, err := auths.ProtectedEnvVars(ctx, stack, "webhooks", webhooks.Spec.Auth)
	if err != nil {
		return nil, err
	}

	postgresEnvVar, err := databases.GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return nil, err
	}

	brokerEnvVar, err := brokers.GetBrokerEnvVars(ctx, brokerURI, stack.Name, "webhooks")
	if err != nil {
		return nil, err
	}

	env = append(env, authEnvVars...)
	env = append(env, postgresEnvVar...)
	env = append(env, brokerEnvVar...)
	env = append(env, core.Env("STORAGE_POSTGRES_CONN_STRING", "$(POSTGRES_URI)"))

	return env, nil
}

func createAPIDeployment(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database, consumer *v1beta1.BrokerConsumer, version string, withWorker bool) error {

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "webhooks", version)
	if err != nil {
		return err
	}

	env, err := deploymentEnvVars(ctx, stack, webhooks, database)
	if err != nil {
		return err
	}

	args := []string{"serve"}

	if withWorker {
		env = append(env, core.Env("WORKER", "true"))

		topics, err := brokers.GetTopicsEnvVars(ctx, stack, "KAFKA_TOPICS", consumer.Spec.Services...)
		if err != nil {
			return err
		}
		env = append(env, topics...)
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(webhooks, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "webhooks",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:           "api",
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

func createSingleDeployment(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database, consumer *v1beta1.BrokerConsumer, version string) error {
	return createAPIDeployment(ctx, stack, webhooks, database, consumer, version, true)
}

func createWorkerDeployment(ctx core.Context, stack *v1beta1.Stack, webhooks *v1beta1.Webhooks, database *v1beta1.Database, consumer *v1beta1.BrokerConsumer, version string) error {
	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "webhooks", version)
	if err != nil {
		return err
	}

	env, err := deploymentEnvVars(ctx, stack, webhooks, database)
	if err != nil {
		return err
	}
	topics, err := brokers.GetTopicsEnvVars(ctx, stack, "KAFKA_TOPICS", consumer.Spec.Services...)
	if err != nil {
		return err
	}
	env = append(env, topics...)

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return err
	}

	return applications.
		New(webhooks, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "webhooks-worker",
			},
			Spec: appsv1.DeploymentSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						ServiceAccountName: serviceAccountName,
						ImagePullSecrets:   imageConfiguration.PullSecrets,
						Containers: []v1.Container{{
							Name:           "worker",
							Env:            env,
							Image:          imageConfiguration.GetFullImageName(),
							Args:           []string{"worker"},
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

func stopWorkers(ctx core.Context, namespace string) error {
	if err := deleteWorkerDeployment(ctx, namespace); err != nil {
		return err
	}

	deployment := &appsv1.Deployment{}
	err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Namespace: namespace, Name: "webhooks"}, deployment)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if deploymentHasEmbeddedWorker(deployment) {
		removeEmbeddedWorker(deployment)
		markWorkerDrain(deployment)
		if err := ctx.GetClient().Update(ctx, deployment); err != nil {
			return err
		}
		return core.NewPendingError().WithMessage("waiting for embedded webhooks workers to terminate")
	}

	if !workerDrainPending(deployment) {
		return nil
	}
	deployment, err = getDeploymentAfterRollout(ctx, namespace, "webhooks")
	if err != nil {
		return err
	}
	if err := waitForEmbeddedWorkerPodsTermination(ctx, deployment); err != nil {
		return err
	}
	clearWorkerDrain(deployment)
	return ctx.GetClient().Update(ctx, deployment)
}

func waitForEmbeddedWorkerPodsTermination(ctx core.Context, deployment *appsv1.Deployment) error {
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return err
	}

	pods := &v1.PodList{}
	if err := ctx.GetAPIReader().List(ctx, pods,
		client.InNamespace(deployment.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return err
	}

	for index := range pods.Items {
		if podHasActiveEmbeddedWorker(&pods.Items[index]) {
			return core.NewPendingError().WithMessage("waiting for embedded webhooks worker pods to terminate")
		}
	}
	return nil
}

func podHasActiveEmbeddedWorker(pod *v1.Pod) bool {
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return false
	}
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "WORKER" && env.Value == "true" {
				return true
			}
		}
	}
	return false
}

func deleteWorkerDeployment(ctx core.Context, namespace string) error {
	if err := client.IgnoreNotFound(ctx.GetClient().Delete(ctx, &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "webhooks-worker"},
	})); err != nil {
		return err
	}

	deployment := &appsv1.Deployment{}
	err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Namespace: namespace, Name: "webhooks-worker"}, deployment)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !deployment.DeletionTimestamp.IsZero() {
		return core.NewPendingError().WithMessage("waiting for webhooks workers to terminate")
	}

	propagationPolicy := metav1.DeletePropagationForeground
	core.LogDeletion(ctx, deployment, "webhooks.deleteWorkerDeployment")
	if err := ctx.GetClient().Delete(ctx, deployment, &client.DeleteOptions{PropagationPolicy: &propagationPolicy}); err != nil {
		return err
	}
	return core.NewPendingError().WithMessage("waiting for webhooks workers to terminate")
}

func waitForDeploymentRollout(ctx core.Context, namespace, name string) error {
	_, err := getDeploymentAfterRollout(ctx, namespace, name)
	return err
}

func getDeploymentAfterRollout(ctx core.Context, namespace, name string) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	if err := ctx.GetAPIReader().Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, deployment); err != nil {
		return nil, err
	}
	if !deploymentRolloutComplete(deployment) {
		return nil, core.NewPendingError().WithMessage("waiting for deployment %s rollout to complete", name)
	}
	return deployment, nil
}

func deploymentRolloutComplete(deployment *appsv1.Deployment) bool {
	if deployment.Spec.Replicas == nil || deployment.Status.ObservedGeneration != deployment.Generation {
		return false
	}
	desired := *deployment.Spec.Replicas
	return deployment.Status.UpdatedReplicas >= desired &&
		deployment.Status.Replicas == deployment.Status.UpdatedReplicas &&
		deployment.Status.AvailableReplicas >= desired
}

func deploymentHasEmbeddedWorker(deployment *appsv1.Deployment) bool {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "WORKER" && env.Value == "true" {
				return true
			}
		}
	}
	return false
}

func removeEmbeddedWorker(deployment *appsv1.Deployment) {
	for containerIndex := range deployment.Spec.Template.Spec.Containers {
		env := deployment.Spec.Template.Spec.Containers[containerIndex].Env
		filtered := env[:0]
		for _, variable := range env {
			if variable.Name != "WORKER" {
				filtered = append(filtered, variable)
			}
		}
		deployment.Spec.Template.Spec.Containers[containerIndex].Env = filtered
	}
}

func clearWorkerDeploymentConditions(webhooks *v1beta1.Webhooks) {
	conditions := webhooks.GetConditions()
	for _, conditionType := range []string{
		"DeploymentReady",
		"PodDisruptionBudget",
		"PodDisruptionBudgetConfigured",
	} {
		conditions.Delete(v1beta1.AndConditions(
			v1beta1.ConditionTypeMatch(conditionType),
			v1beta1.ConditionReasonMatch("WebhooksWorker"),
		))
	}
}

func markWorkerDrain(deployment *appsv1.Deployment) {
	if deployment.Annotations == nil {
		deployment.Annotations = map[string]string{}
	}
	deployment.Annotations[workerDrainAnnotation] = "true"
}

func workerDrainPending(deployment *appsv1.Deployment) bool {
	return deployment.Annotations[workerDrainAnnotation] == "true"
}

func clearWorkerDrain(deployment *appsv1.Deployment) {
	delete(deployment.Annotations, workerDrainAnnotation)
}
