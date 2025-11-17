package databases

import (
	"net/url"
	"strconv"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
)

func GetPostgresEnvVars(ctx core.Context, stack *v1beta1.Stack, database *v1beta1.Database) ([]corev1.EnvVar, error) {
	ret := []corev1.EnvVar{
		core.Env("POSTGRES_HOST", database.Status.URI.Hostname()),
		core.Env("POSTGRES_PORT", database.Status.URI.Port()),
		core.Env("POSTGRES_DATABASE", database.Status.Database),
	}
	if database.Status.URI.User.Username() != "" || database.Status.URI.Query().Get("secret") != "" {
		if database.Status.URI.User.Username() != "" {
			password, _ := database.Status.URI.User.Password()
			ret = append(ret,
				core.Env("POSTGRES_USERNAME", database.Status.URI.User.Username()),
				core.Env("POSTGRES_PASSWORD", url.QueryEscape(password)),
			)
		} else {
			secret := database.Status.URI.Query().Get("secret")
			ret = append(ret,
				core.EnvFromSecret("POSTGRES_USERNAME", secret, "username"),
				core.EnvFromSecret("POSTGRES_PASSWORD", secret, "password"),
			)
		}
		ret = append(ret,
			core.Env("POSTGRES_NO_DATABASE_URI", core.ComputeEnvVar("postgresql://%s:%s@%s:%s",
				"POSTGRES_USERNAME",
				"POSTGRES_PASSWORD",
				"POSTGRES_HOST",
				"POSTGRES_PORT",
			)),
		)
	} else {
		ret = append(ret,
			core.Env("POSTGRES_NO_DATABASE_URI", core.ComputeEnvVar("postgresql://%s:%s",
				"POSTGRES_HOST",
				"POSTGRES_PORT",
			)),
		)
	}

	awsRole, err := settings.GetAWSServiceAccount(ctx, stack.Name)
	if err != nil {
		return nil, err
	}

	if awsRole != "" {
		ret = append(ret, core.Env("POSTGRES_AWS_ENABLE_IAM", "true"))
	}

	f := "%s/%s"
	if settings.IsTrue(database.Status.URI.Query().Get("disableSSLMode")) {
		f += "?sslmode=disable"
	}
	ret = append(ret,
		core.Env("POSTGRES_URI", core.ComputeEnvVar(f,
			"POSTGRES_NO_DATABASE_URI",
			"POSTGRES_DATABASE")),
	)

	config, err := settings.GetAs[connectionPoolConfiguration](ctx, stack.Name, "modules", database.Spec.Service, "database", "connection-pool")
	if err != nil {
		return nil, err
	}

	if config.MaxIdle != "" {
		_, err := strconv.ParseUint(config.MaxIdle, 10, 64)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse max idle value")
		}
		ret = append(ret, core.Env("POSTGRES_MAX_IDLE_CONNS", config.MaxIdle))
	}
	if config.MaxIdleTime != "" {
		_, err := time.ParseDuration(config.MaxIdleTime)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse max idle time value")
		}
		ret = append(ret, core.Env("POSTGRES_CONN_MAX_IDLE_TIME", config.MaxIdleTime))
	}
	if config.MaxOpen != "" {
		_, err := strconv.ParseUint(config.MaxOpen, 10, 64)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse max open conns value")
		}
		ret = append(ret, core.Env("POSTGRES_MAX_OPEN_CONNS", config.MaxOpen))
	}
	if config.MaxLifetime != "" {
		_, err := time.ParseDuration(config.MaxLifetime)
		if err != nil {
			return nil, errors.Wrap(err, "cannot parse max lifetime value")
		}
		ret = append(ret, core.Env("POSTGRES_CONN_MAX_LIFETIME", config.MaxLifetime))
	}

	return ret, nil
}

type connectionPoolConfiguration struct {
	MaxIdle     string `json:"max-idle,omitempty"`
	MaxIdleTime string `json:"max-idle-time,omitempty"`
	MaxOpen     string `json:"max-open,omitempty"`
	MaxLifetime string `json:"max-lifetime,omitempty"`
}
