package payments

import (
	"errors"

	"golang.org/x/mod/semver"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/jobs"
	"github.com/formancehq/operator/v3/internal/resources/registries"
	"github.com/formancehq/operator/v3/internal/resources/settings"
)

func Clean(ctx core.Context, t *v1beta1.Payments) error {
	stack := &v1beta1.Stack{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: t.GetStack(),
	}, stack); err != nil {
		return err
	}

	clearTemporal, err := settings.GetBoolOrTrue(ctx, stack.Name, "payments", "clear-temporal")
	if err != nil {
		return err
	}
	if !clearTemporal {
		log.FromContext(ctx).Info("payments.clear-temporal is false, skipping temporal cleanup")
		return nil
	}

	version, err := core.GetModuleVersion(ctx, stack, t)
	if err != nil {
		if errors.Is(err, core.ErrNoVersionFound) {
			log.FromContext(ctx).Info("No version configured, skipping version-gated finalizer logic")
			return nil
		}
		return err
	}
	if semver.IsValid(version) && semver.Compare(version, "v3.0.0-beta.1") < 0 {
		// Nothing to do here
		log.FromContext(ctx).Info("skipping finalizer for payments")
		return nil
	}

	imageConfiguration, err := registries.GetFormanceImage(ctx, stack, "payments", version)
	if err != nil {
		return err
	}

	_, env, err := temporalEnvVars(ctx, stack, t)
	if err != nil {
		return err
	}

	return jobs.Handle(ctx, t, "clean-payments-temporal", corev1.Container{
		Name: "clean-payments-temporal",
		Args: []string{"purge"},
		Env: append(env,
			core.Env("STACK", t.GetStack()),
		),
		Image: imageConfiguration.GetFullImageName(),
	}, jobs.WithImagePullSecrets(imageConfiguration.PullSecrets))
}
