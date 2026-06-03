package core

const (
	DefaultCollectorImage = "otel/opentelemetry-collector-contrib:0.151.0"

	SignalTracesAnnotation  = "formance.com/otel-traces-enabled"
	SignalMetricsAnnotation = "formance.com/otel-metrics-enabled"

	CollectorManagedByLabel = "formance.com/managed-by"
	CollectorManagedByValue = "otelexporterendpoint"
)

type Platform struct {
	// Cloud region where the stack is deployed
	Region string
	// Cloud environment where the stack is deployed: staging, production,
	// sandbox, etc.
	Environment string
	// The licence information
	LicenceSecret string
	// The operator utils image version
	UtilsVersion string
	// Namespace where the licence secret lives
	LicenceNamespace string
	// Licence validation state (computed from the licence secret JWT)
	LicenceState LicenceState
	// Human-readable message about the licence state
	LicenceMessage string
	// The OTel Collector image used by OtelExporterEndpoint resources
	CollectorImage string
}
