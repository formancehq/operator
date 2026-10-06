package jobs

import (
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

// ApplyNodeSelector applies the stack's jobs.nodeSelector setting to a Job pod
// specification, including Job templates in CronJobs.
func ApplyNodeSelector(ctx core.Context, stack string, spec *corev1.PodSpec) error {
	nodeSelector, err := settings.GetMap(ctx, stack, "jobs", "nodeSelector")
	if err != nil {
		return fmt.Errorf("reading jobs.nodeSelector for stack %q: %w", stack, err)
	}
	if len(nodeSelector) == 0 {
		return nil
	}
	if err := metav1validation.ValidateLabels(nodeSelector, field.NewPath("jobs", "nodeSelector")).ToAggregate(); err != nil {
		return fmt.Errorf("invalid jobs.nodeSelector for stack %q: %w", stack, err)
	}
	for key, value := range nodeSelector {
		if existing, ok := spec.NodeSelector[key]; ok && existing != value {
			return fmt.Errorf("jobs.nodeSelector for stack %q conflicts with pod node selector %q: %q != %q", stack, key, value, existing)
		}
	}
	if spec.NodeSelector == nil {
		spec.NodeSelector = make(map[string]string, len(nodeSelector))
	}
	maps.Copy(spec.NodeSelector, nodeSelector)
	return nil
}
