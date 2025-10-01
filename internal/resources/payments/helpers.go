package payments

import (
	"fmt"
	"strings"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/resourcereferences"
	"github.com/formancehq/operator/internal/resources/settings"
	corev1 "k8s.io/api/core/v1"
)

func getEncryptionKey(ctx core.Context, payments *v1beta1.Payments) (string, error) {
	encryptionKey := payments.Spec.EncryptionKey
	if encryptionKey == "" {
		return settings.GetStringOrEmpty(ctx, payments.Spec.Stack, "payments", "encryption-key")
	}
	return "", nil
}

func temporalEnvVars(ctx core.Context, stack *v1beta1.Stack, payments *v1beta1.Payments) ([]corev1.EnvVar, error) {
	temporalURI, err := settings.RequireURL(ctx, stack.Name, "temporal", "dsn")
	if err != nil {
		return nil, err
	}

	if err := validateTemporalURI(temporalURI); err != nil {
		return nil, err
	}

	if secret := temporalURI.Query().Get("secret"); secret != "" {
		_, err = resourcereferences.Create(ctx, payments, "payments-temporal", secret, &corev1.Secret{})
	} else {
		err = resourcereferences.Delete(ctx, payments, "payments-temporal")
	}
	if err != nil {
		return nil, err
	}

	if secret := temporalURI.Query().Get("encryptionKeySecret"); secret != "" {
		_, err = resourcereferences.Create(ctx, payments, "payments-temporal-encryption-key", secret, &corev1.Secret{})
	} else {
		err = resourcereferences.Delete(ctx, payments, "payments-temporal-encryption-key")
	}
	if err != nil {
		return nil, err
	}

	env := make([]corev1.EnvVar, 0)
	env = append(env,
		core.Env("TEMPORAL_ADDRESS", temporalURI.Host),
		core.Env("TEMPORAL_NAMESPACE", temporalURI.Path[1:]),
	)

	if secret := temporalURI.Query().Get("secret"); secret == "" {
		temporalTLSCrt, err := settings.GetStringOrEmpty(ctx, stack.Name, "temporal", "tls", "crt")
		if err != nil {
			return nil, err
		}

		temporalTLSKey, err := settings.GetStringOrEmpty(ctx, stack.Name, "temporal", "tls", "key")
		if err != nil {
			return nil, err
		}

		env = append(env,
			core.Env("TEMPORAL_SSL_CLIENT_KEY", temporalTLSKey),
			core.Env("TEMPORAL_SSL_CLIENT_CERT", temporalTLSCrt),
		)
	} else {
		env = append(env,
			core.EnvFromSecret("TEMPORAL_SSL_CLIENT_KEY", secret, "tls.key"),
			core.EnvFromSecret("TEMPORAL_SSL_CLIENT_CERT", secret, "tls.crt"),
		)
	}

	if secret := temporalURI.Query().Get("encryptionKeySecret"); secret != "" {
		env = append(env,
			core.Env("TEMPORAL_ENCRYPTION_ENABLED", "true"),
			core.EnvFromSecret("TEMPORAL_ENCRYPTION_KEY", secret, "key"),
		)
	}

	if initSearchAttributes := temporalURI.Query().Get("initSearchAttributes"); initSearchAttributes == "true" {
		env = append(env, core.Env("TEMPORAL_INIT_SEARCH_ATTRIBUTES", "true"))
	}

	temporalMaxConcurrentWorkflowTaskPollers, err := settings.GetIntOrDefault(ctx, stack.Name, 4, "payments", "worker", "temporal-max-concurrent-workflow-task-pollers")
	if err != nil {
		return nil, err
	}

	temporalMaxConcurrentActivityTaskPollers, err := settings.GetIntOrDefault(ctx, stack.Name, 4, "payments", "worker", "temporal-max-concurrent-activity-task-pollers")
	if err != nil {
		return nil, err
	}

	temporalMaxSlotsPerPoller, err := settings.GetIntOrDefault(ctx, stack.Name, 10, "payments", "worker", "temporal-max-slots-per-poller")
	if err != nil {
		return nil, err
	}

	temporalMaxLocalActivitySlots, err := settings.GetIntOrDefault(ctx, stack.Name, 50, "payments", "worker", "temporal-max-local-activity-slots")
	if err != nil {
		return nil, err
	}

	env = append(env,
		core.Env("TEMPORAL_MAX_CONCURRENT_WORKFLOW_TASK_POLLERS", fmt.Sprintf("%d", temporalMaxConcurrentWorkflowTaskPollers)),
		core.Env("TEMPORAL_MAX_CONCURRENT_ACTIVITY_TASK_POLLERS", fmt.Sprintf("%d", temporalMaxConcurrentActivityTaskPollers)),
		core.Env("TEMPORAL_MAX_SLOTS_PER_POLLER", fmt.Sprintf("%d", temporalMaxSlotsPerPoller)),
		core.Env("TEMPORAL_MAX_LOCAL_ACTIVITY_SLOTS", fmt.Sprintf("%d", temporalMaxLocalActivitySlots)),
	)

	return env, nil
}

func validateTemporalURI(temporalURI *v1beta1.URI) error {
	if temporalURI.Scheme != "temporal" {
		return fmt.Errorf("invalid temporal uri: %s", temporalURI.String())
	}

	if temporalURI.Path == "" {
		return fmt.Errorf("invalid temporal uri: %s", temporalURI.String())
	}

	if !strings.HasPrefix(temporalURI.Path, "/") {
		return fmt.Errorf("invalid temporal uri: %s", temporalURI.String())
	}

	return nil
}
