package serviceaccounts

import (
	"fmt"
	"maps"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GetServiceAccountName returns the service account name to use for a deployment/job.
// It first checks if the resource has a ServiceAccountConfig defined, and if so,
// creates/manages the service account and returns its name.
// Otherwise, it falls back to the legacy aws.service-account setting for backward compatibility.
func GetServiceAccountName(ctx core.Context, owner v1beta1.Dependent, serviceAccountConfig *v1beta1.ServiceAccountConfig, serviceAccountName string) (string, error) {
	var err error
	// If ServiceAccountConfig is provided, create and manage the service account
	if serviceAccountConfig != nil {
		// Use the deployment/job name directly as the service account name
		err = CreateOrUpdate(ctx, owner, serviceAccountName, serviceAccountConfig)
		if err != nil {
			return "", fmt.Errorf("failed to create/update service account: %w", err)
		}
		return serviceAccountName, nil
	}

	// Fall back to legacy aws.service-account setting for backward compatibility
	serviceAccountName, err = settings.GetAWSServiceAccount(ctx, owner.GetStack())
	if err != nil {
		return "", err
	}

	return serviceAccountName, nil
}

// CreateOrUpdate creates or updates a service account based on the ServiceAccountConfig
func CreateOrUpdate(ctx core.Context, owner v1beta1.Dependent, serviceAccountName string, config *v1beta1.ServiceAccountConfig) error {
	labels := map[string]string{
		v1beta1.StackLabel: owner.GetStack(),
	}
	if config.Labels != nil {
		maps.Copy(labels, config.Labels)
	}

	annotations := make(map[string]string)
	if config.Annotations != nil {
		maps.Copy(annotations, config.Annotations)
	}

	_, _, err := core.CreateOrUpdate[*corev1.ServiceAccount](ctx, types.NamespacedName{
		Namespace: owner.GetStack(),
		Name:      serviceAccountName,
	}, func(sa *corev1.ServiceAccount) error {
		sa.Labels = labels
		sa.Annotations = annotations
		return nil
	}, core.WithController[*corev1.ServiceAccount](ctx.GetScheme(), owner))
	return err
}
