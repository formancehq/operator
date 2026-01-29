package settings

import (
	v1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
)

// GetElasticsearchEnvVars returns environment variables for Elasticsearch configuration.
// Returns an error if elasticsearch.dsn is not configured (required setting).
func GetElasticsearchEnvVars(ctx core.Context, stackName string) ([]v1.EnvVar, error) {
	esURL, err := RequireURL(ctx, stackName, "elasticsearch", "dsn")
	if err != nil {
		return nil, err
	}

	env := []v1.EnvVar{
		core.Env("ELASTICSEARCH_URL", esURL.WithoutQuery().String()),
	}

	// Username/Password from query params (optional)
	if username := esURL.Query().Get("username"); username != "" {
		env = append(env, core.Env("ELASTICSEARCH_USERNAME", username))
	}

	if password := esURL.Query().Get("password"); password != "" {
		env = append(env, core.Env("ELASTICSEARCH_PASSWORD", password))
	}

	// ILM Configuration with defaults
	env = append(env, core.Env("ELASTICSEARCH_ILM_ENABLED",
		getQueryParamOrDefault(esURL, "ilmEnabled", "true")))

	env = append(env, core.Env("ELASTICSEARCH_ILM_HOT_PHASE_DAYS",
		getQueryParamOrDefault(esURL, "ilmHotPhaseDays", "90")))

	env = append(env, core.Env("ELASTICSEARCH_ILM_WARM_PHASE_ROLLOVER_DAYS",
		getQueryParamOrDefault(esURL, "ilmWarmPhaseRolloverDays", "365")))

	env = append(env, core.Env("ELASTICSEARCH_ILM_DELETE_PHASE_ENABLED",
		getQueryParamOrDefault(esURL, "ilmDeletePhaseEnabled", "false")))

	env = append(env, core.Env("ELASTICSEARCH_ILM_DELETE_PHASE_DAYS",
		getQueryParamOrDefault(esURL, "ilmDeletePhaseDays", "0")))

	return env, nil
}

// getQueryParamOrDefault extracts a query parameter from the URI or returns the default value.
func getQueryParamOrDefault(uri *v1beta1.URI, key, defaultValue string) string {
	if value := uri.Query().Get(key); value != "" {
		return value
	}
	return defaultValue
}
