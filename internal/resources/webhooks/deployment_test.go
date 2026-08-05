package webhooks

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestUsesSeparateWorkerDeployment(t *testing.T) {
	tests := map[string]bool{
		"v2.4.9":         false,
		"2.4.9":          false,
		"v2.5.0-0":       true,
		"2.5.0-0":        true,
		"v2.5.0-alpha.1": true,
		"v2.5.0":         true,
		"v3.0.0":         true,
		"main":           true,
	}

	for version, expected := range tests {
		t.Run(version, func(t *testing.T) {
			if got := usesSeparateWorkerDeployment(version); got != expected {
				t.Fatalf("usesSeparateWorkerDeployment(%q) = %v, want %v", version, got, expected)
			}
		})
	}
}

func TestDeploymentRolloutComplete(t *testing.T) {
	ready := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: ptr.To[int32](2)},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Replicas:           2,
			UpdatedReplicas:    2,
			AvailableReplicas:  2,
		},
	}
	if !deploymentRolloutComplete(ready) {
		t.Fatal("expected completed rollout")
	}

	oldWorkerRemaining := ready.DeepCopy()
	oldWorkerRemaining.Status.Replicas = 3
	if deploymentRolloutComplete(oldWorkerRemaining) {
		t.Fatal("rollout must wait until every old worker pod has terminated")
	}

	unobserved := ready.DeepCopy()
	unobserved.Status.ObservedGeneration = 1
	if deploymentRolloutComplete(unobserved) {
		t.Fatal("rollout must wait for the current generation")
	}
}

func TestDeploymentHasEmbeddedWorker(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Env: []corev1.EnvVar{{Name: "WORKER", Value: "true"}},
					}},
				},
			},
		},
	}
	if !deploymentHasEmbeddedWorker(deployment) {
		t.Fatal("expected embedded worker to be detected")
	}

	deployment.Spec.Template.Spec.Containers[0].Env[0].Value = "false"
	if deploymentHasEmbeddedWorker(deployment) {
		t.Fatal("disabled worker must not be detected as embedded")
	}
}

func TestRemoveEmbeddedWorker(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Env: []corev1.EnvVar{
							{Name: "BEFORE", Value: "before"},
							{Name: "WORKER", Value: "true"},
							{Name: "AFTER", Value: "after"},
						},
					}},
				},
			},
		},
	}

	removeEmbeddedWorker(deployment)

	got := deployment.Spec.Template.Spec.Containers[0].Env
	want := []corev1.EnvVar{{Name: "BEFORE", Value: "before"}, {Name: "AFTER", Value: "after"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unexpected environment after disabling worker: %#v", got)
	}
}
