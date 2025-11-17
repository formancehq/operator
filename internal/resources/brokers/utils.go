package brokers

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/formancehq/go-libs/v2/collectionutils"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
)

func GetBrokerEnvVars(ctx core.Context, brokerURI *v1beta1.URI, stackName, serviceName string) ([]v1.EnvVar, error) {
	ret := make([]v1.EnvVar, 0)

	ret = append(ret, core.Env("BROKER", brokerURI.Scheme))

	if brokerURI.Query().Get("circuitBreakerEnabled") == "true" {
		ret = append(ret, core.Env("PUBLISHER_CIRCUIT_BREAKER_ENABLED", "true"))
		if openInterval := brokerURI.Query().Get("circuitBreakerOpenInterval"); openInterval != "" {
			ret = append(ret, core.Env("PUBLISHER_CIRCUIT_BREAKER_OPEN_INTERVAL_DURATION", openInterval))
		}
	}

	switch brokerURI.Scheme {
	case "kafka":
		ret = append(ret,
			core.Env("BROKER", "kafka"),
			core.Env("PUBLISHER_KAFKA_ENABLED", "true"),
			core.Env("PUBLISHER_KAFKA_BROKER", brokerURI.Host),
		)
		if settings.IsTrue(brokerURI.Query().Get("saslEnabled")) {
			ret = append(ret,
				core.Env("PUBLISHER_KAFKA_SASL_ENABLED", "true"),
				core.Env("PUBLISHER_KAFKA_SASL_USERNAME", brokerURI.Query().Get("saslUsername")),
				core.Env("PUBLISHER_KAFKA_SASL_PASSWORD", brokerURI.Query().Get("saslPassword")),
				core.Env("PUBLISHER_KAFKA_SASL_MECHANISM", brokerURI.Query().Get("saslMechanism")),
				core.Env("PUBLISHER_KAFKA_SASL_SCRAM_SHA_SIZE", brokerURI.Query().Get("saslSCRAMSHASize")),
			)

			serviceAccount, err := settings.GetAWSServiceAccount(ctx, stackName)
			if err != nil {
				return nil, err
			}

			if serviceAccount != "" {
				ret = append(ret, core.Env("PUBLISHER_KAFKA_SASL_IAM_ENABLED", "true"))
			}
		}
		if settings.IsTrue(brokerURI.Query().Get("tls")) {
			ret = append(ret,
				core.Env("PUBLISHER_KAFKA_TLS_ENABLED", "true"),
			)
		}

	case "nats":
		ret = append(ret,
			core.Env("PUBLISHER_NATS_ENABLED", "true"),
			core.Env("PUBLISHER_NATS_URL", brokerURI.Host),
			core.Env("PUBLISHER_NATS_CLIENT_ID", fmt.Sprintf("%s-%s", stackName, serviceName)),
		)
	}

	return ret, nil
}

func GetPublisherEnvVars(stack *v1beta1.Stack, broker *v1beta1.Broker, service string) []v1.EnvVar {
	switch broker.Status.Mode {
	case v1beta1.ModeOneStreamByService:
		return []v1.EnvVar{
			core.Env("PUBLISHER_TOPIC_MAPPING", "*:"+core.GetObjectName(stack.Name, service)),
		}
	case v1beta1.ModeOneStreamByStack:
		ret := []v1.EnvVar{
			core.Env("PUBLISHER_TOPIC_MAPPING", fmt.Sprintf("*:%s.%s", stack.Name, service)),
		}

		if broker.Status.URI.Scheme == "nats" {
			ret = append(ret, core.Env("PUBLISHER_NATS_AUTO_PROVISION", "false"))
		}
		return ret
	default:
		panic(fmt.Sprintf("mode '%s' not handled", broker.Status.Mode))
	}
}

func GetTopicsEnvVars(ctx core.Context, stack *v1beta1.Stack, key string, services ...string) ([]v1.EnvVar, error) {

	broker := &v1beta1.Broker{}
	if err := ctx.GetClient().Get(ctx, types.NamespacedName{
		Name: stack.Name,
	}, broker); err != nil {
		return nil, err
	}

	if !broker.Status.Ready {
		return nil, core.NewPendingError().WithMessage("broker not ready")
	}

	ret := []v1.EnvVar{
		core.Env(key, strings.Join(collectionutils.Map(services, func(from string) string {
			switch broker.Status.Mode {
			case v1beta1.ModeOneStreamByService:
				return fmt.Sprintf("%s-%s", stack.Name, from)
			case v1beta1.ModeOneStreamByStack:
				return fmt.Sprintf("%s.%s", stack.Name, from)
			}
			return ""
		}), " ")),
	}

	if broker.Status.URI.Scheme == "nats" {
		ret = append(ret, core.Env("PUBLISHER_NATS_AUTO_PROVISION", "false"))
	}

	return ret, nil
}
