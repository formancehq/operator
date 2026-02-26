package licence

import (
	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/resourcereferences"
)

func GetLicenceEnvVars(ctx core.Context, ownerName string, owner v1beta1.Dependent) (*v1beta1.ResourceReference, []v1.EnvVar, error) {
	ret := make([]v1.EnvVar, 0)

	platform := ctx.GetPlatform()

	var resourceReference *v1beta1.ResourceReference
	var err error
	if platform.LicenceSecret != "" {
		resourceReference, err = resourcereferences.Create(ctx, owner, ownerName+"-licence", platform.LicenceSecret, &v1.Secret{})
		if err != nil {
			return nil, nil, err
		}
	} else {
		err := resourcereferences.Delete(ctx, owner, ownerName+"-licence")
		if err != nil {
			return nil, nil, err
		}

		ret = append(ret, core.Env("LICENCE_ENABLED", "false"))
		return nil, ret, nil
	}

	ret = append(ret, core.Env("LICENCE_ENABLED", "false"))

	ret = append(ret, core.EnvFromSecret("LICENCE_TOKEN", platform.LicenceSecret, "token"))
	ret = append(ret, core.EnvFromSecret("LICENCE_ISSUER", platform.LicenceSecret, "issuer"))
	ret = append(ret, core.Env("LICENCE_VALIDATE_TICK", "24h"))

	return resourceReference, ret, nil
}
