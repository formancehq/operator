package databases

import (
	"fmt"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/jobs"
	"github.com/formancehq/operator/internal/resources/registries"
	"github.com/formancehq/operator/internal/resources/settings"
)

func Migrate(
	ctx core.Context,
	stack *v1beta1.Stack,
	owner v1beta1.Dependent,
	imageConfiguration *registries.ImageConfiguration,
	database *v1beta1.Database,
	options ...jobs.HandleJobOption,
) error {
	args := []string{"migrate"}

	env, err := GetPostgresEnvVars(ctx, stack, database)
	if err != nil {
		return err
	}

	serviceAccountName, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return errors.Wrap(err, "getting service account name")
	}

	return jobs.Handle(ctx, owner, fmt.Sprintf("%s-migration", database.Spec.Service), v1.Container{
		Name:  "migrate",
		Image: imageConfiguration.GetFullImageName(),
		Args:  args,
		Env:   env,
	},
		append(options,
			jobs.WithImagePullSecrets(imageConfiguration.PullSecrets),
			jobs.WithServiceAccount(serviceAccountName),
		)...,
	)
}
