package reconciliations

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestUsesV3Topology(t *testing.T) {
	tests := map[string]bool{
		"v2.3.1":      false,
		"v2.3.1-rc.1": false,
		"v2.4.0-rc.1": false,
		"v2.4.0":      true,
		"v2.4.1":      true,
		"v3.0.0-0":    true,
		"v3.0.0":      true,
		"v3.1.0":      true,
		"main":        true,
	}
	for version, expected := range tests {
		t.Run(version, func(t *testing.T) {
			if got := usesV3Topology(version); got != expected {
				t.Fatalf("usesV3Topology(%q) = %v, want %v", version, got, expected)
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

	oldPodRemaining := ready.DeepCopy()
	oldPodRemaining.Status.Replicas = 3
	if deploymentRolloutComplete(oldPodRemaining) {
		t.Fatal("rollout must wait until every old API pod has terminated")
	}

	unobserved := ready.DeepCopy()
	unobserved.Status.ObservedGeneration = 1
	if deploymentRolloutComplete(unobserved) {
		t.Fatal("rollout must wait for the current generation")
	}
}
