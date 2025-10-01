package applications

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func TestStandardHTTPPort(t *testing.T) {
	t.Parallel()

	port := StandardHTTPPort()

	require.Equal(t, "http", port.Name)
	require.Equal(t, int32(8080), port.ContainerPort)
}

func TestDefaultLiveness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		port string
		opts []ProbeOpts
	}{
		{
			name: "default liveness with http port",
			port: "http",
			opts: nil,
		},
		{
			name: "liveness with custom port name",
			port: "custom",
			opts: nil,
		},
		{
			name: "liveness with custom path",
			port: "http",
			opts: []ProbeOpts{WithProbePath("/health")},
		},
		{
			name: "liveness with custom host",
			port: "http",
			opts: []ProbeOpts{WithHost("localhost")},
		},
		{
			name: "liveness with multiple options",
			port: "http",
			opts: []ProbeOpts{
				WithHost("localhost"),
				WithProbePath("/custom-health"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := DefaultLiveness(tt.port, tt.opts...)

			require.NotNil(t, probe)
			require.NotNil(t, probe.HTTPGet)

			// Check default values
			require.Equal(t, int32(1), probe.InitialDelaySeconds)
			require.Equal(t, int32(30), probe.TimeoutSeconds)
			require.Equal(t, int32(2), probe.PeriodSeconds)
			require.Equal(t, int32(1), probe.SuccessThreshold)
			require.Equal(t, int32(20), probe.FailureThreshold)
			require.Equal(t, ptr.To[int64](10), probe.TerminationGracePeriodSeconds)

			// Check port
			require.Equal(t, intstr.FromString(tt.port), probe.HTTPGet.Port)
			require.Equal(t, corev1.URISchemeHTTP, probe.HTTPGet.Scheme)
		})
	}
}

func TestLivenessDefaults(t *testing.T) {
	t.Parallel()

	probe := DefaultLiveness("http")

	// Verify the probe has default path from defaultProbeOptions
	require.Equal(t, "/_healthcheck", probe.HTTPGet.Path)
}

func TestWithProbePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{
			name: "standard health path",
			path: "/health",
		},
		{
			name: "custom health path",
			path: "/api/v1/healthz",
		},
		{
			name: "root path",
			path: "/",
		},
		{
			name: "path with query params",
			path: "/health?deep=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := DefaultLiveness("http", WithProbePath(tt.path))

			require.Equal(t, tt.path, probe.HTTPGet.Path)
		})
	}
}

func TestWithHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
	}{
		{
			name: "localhost",
			host: "localhost",
		},
		{
			name: "specific IP",
			host: "127.0.0.1",
		},
		{
			name: "service name",
			host: "my-service.namespace.svc.cluster.local",
		},
		{
			name: "empty host",
			host: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := DefaultLiveness("http", WithHost(tt.host))

			require.Equal(t, tt.host, probe.HTTPGet.Host)
		})
	}
}

func TestProbeOptionsComposition(t *testing.T) {
	t.Parallel()

	// Test that multiple options can be composed
	probe := DefaultLiveness("http",
		WithHost("localhost"),
		WithProbePath("/custom-health"),
	)

	require.Equal(t, "localhost", probe.HTTPGet.Host)
	require.Equal(t, "/custom-health", probe.HTTPGet.Path)
	require.Equal(t, intstr.FromString("http"), probe.HTTPGet.Port)
	require.Equal(t, corev1.URISchemeHTTP, probe.HTTPGet.Scheme)
}

func TestLivenessThresholds(t *testing.T) {
	t.Parallel()

	probe := DefaultLiveness("http")

	// These values are critical for production reliability
	// InitialDelaySeconds: 1 - start checking quickly
	require.Equal(t, int32(1), probe.InitialDelaySeconds,
		"Should start health checks quickly")

	// TimeoutSeconds: 30 - give enough time for slow responses
	require.Equal(t, int32(30), probe.TimeoutSeconds,
		"Should have reasonable timeout")

	// PeriodSeconds: 2 - check frequently
	require.Equal(t, int32(2), probe.PeriodSeconds,
		"Should check frequently")

	// SuccessThreshold: 1 - consider healthy immediately after success
	require.Equal(t, int32(1), probe.SuccessThreshold,
		"Should consider healthy after one success")

	// FailureThreshold: 20 - be lenient before marking as unhealthy
	require.Equal(t, int32(20), probe.FailureThreshold,
		"Should tolerate multiple failures before marking unhealthy")

	// TerminationGracePeriodSeconds: 10 - give time for graceful shutdown
	require.Equal(t, ptr.To[int64](10), probe.TerminationGracePeriodSeconds,
		"Should have termination grace period")
}

func TestLivenessHTTPGetConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		port         string
		opts         []ProbeOpts
		expectedPort intstr.IntOrString
	}{
		{
			name:         "string port",
			port:         "http",
			opts:         nil,
			expectedPort: intstr.FromString("http"),
		},
		{
			name:         "custom port name",
			port:         "metrics",
			opts:         nil,
			expectedPort: intstr.FromString("metrics"),
		},
		{
			name:         "admin port",
			port:         "admin",
			opts:         nil,
			expectedPort: intstr.FromString("admin"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := DefaultLiveness(tt.port, tt.opts...)

			require.NotNil(t, probe.HTTPGet)
			require.Equal(t, tt.expectedPort, probe.HTTPGet.Port)
			require.Equal(t, corev1.URISchemeHTTP, probe.HTTPGet.Scheme,
				"Scheme should always be HTTP")
		})
	}
}

func TestProbeOptionsOverrideDefaults(t *testing.T) {
	t.Parallel()

	// Test that custom options override default options
	customPath := "/my-custom-health"
	probe := DefaultLiveness("http", WithProbePath(customPath))

	// Custom path should override the default "/_healthcheck"
	require.Equal(t, customPath, probe.HTTPGet.Path,
		"Custom path should override default")

	// Other defaults should still be present
	require.Equal(t, int32(1), probe.InitialDelaySeconds)
	require.Equal(t, corev1.URISchemeHTTP, probe.HTTPGet.Scheme)
}