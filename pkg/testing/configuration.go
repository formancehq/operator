package testing

import (
	"github.com/google/uuid"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	"github.com/numary/operator/apis/stack/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewDumbVersions() *v1beta2.Versions {
	return &v1beta2.Versions{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: v1beta2.VersionsSpec{
			Control:  uuid.NewString(),
			Ledger:   uuid.NewString(),
			Payments: uuid.NewString(),
			Search:   uuid.NewString(),
			Auth:     uuid.NewString(),
			Webhooks: uuid.NewString(),
		},
	}
}

func NewDumbConfiguration() *v1beta2.Configuration {
	return &v1beta2.Configuration{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
		},
		Spec: v1beta2.ConfigurationSpec{
			Services: v1beta2.ConfigurationServicesSpec{
				Auth: v1beta2.AuthSpec{
					Postgres: NewDumpPostgresConfig(),
				},
				Control: v1beta2.ControlSpec{},
				Ledger: v1beta2.LedgerSpec{
					Postgres: NewDumpPostgresConfig(),
				},
				Payments: v1beta2.PaymentsSpec{
					MongoDB: NewDumpMongoDBConfig(),
				},
				Search: v1beta2.SearchSpec{
					ElasticSearchConfig: NewDumpElasticSearchConfig(),
				},
				Webhooks: v1beta2.WebhooksSpec{
					MongoDB: NewDumpMongoDBConfig(),
				},
			},
			Kafka: NewDumpKafkaConfig(),
		},
	}
}

func NewDumpKafkaConfig() apisv1beta1.KafkaConfig {
	return apisv1beta1.KafkaConfig{
		Brokers: []string{"kafka:1234"},
	}
}

func NewDumpElasticSearchConfig() componentsv1beta1.ElasticSearchConfig {
	return componentsv1beta1.ElasticSearchConfig{
		Scheme: "http",
		Host:   "elasticsearch",
		Port:   9200,
	}
}

func NewDumpMongoDBConfig() apisv1beta1.MongoDBConfig {
	return apisv1beta1.MongoDBConfig{
		Host:     "mongodb",
		Port:     27017,
		Username: "username",
		Password: "password",
		Database: "database",
	}
}

func NewDumpPostgresConfig() apisv1beta1.PostgresConfig {
	return apisv1beta1.PostgresConfig{
		Port:     5432,
		Host:     "postgres",
		Username: "username",
		Password: "password",
	}
}
