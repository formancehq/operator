package gateways

import (
	"testing"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		gateway  *v1beta1.Gateway
		expected string
	}{
		{
			name: "with HTTPS ingress",
			gateway: &v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
				},
				Spec: v1beta1.GatewaySpec{
					Ingress: &v1beta1.GatewayIngress{
						Host:   "api.example.com",
						Scheme: "https",
					},
				},
			},
			expected: "https://api.example.com",
		},
		{
			name: "with HTTP ingress",
			gateway: &v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
				},
				Spec: v1beta1.GatewaySpec{
					Ingress: &v1beta1.GatewayIngress{
						Host:   "api.staging.example.com",
						Scheme: "http",
					},
				},
			},
			expected: "http://api.staging.example.com",
		},
		{
			name: "without ingress - default internal service",
			gateway: &v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
				},
				Spec: v1beta1.GatewaySpec{
					Ingress: nil,
				},
			},
			expected: "http://gateway:8080",
		},
		{
			name: "with custom domain",
			gateway: &v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "custom-gateway",
				},
				Spec: v1beta1.GatewaySpec{
					Ingress: &v1beta1.GatewayIngress{
						Host:   "custom.domain.io",
						Scheme: "https",
					},
				},
			},
			expected: "https://custom.domain.io",
		},
		{
			name: "with subdomain and path",
			gateway: &v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
				},
				Spec: v1beta1.GatewaySpec{
					Ingress: &v1beta1.GatewayIngress{
						Host:   "payments.api.example.com",
						Scheme: "https",
					},
				},
			},
			expected: "https://payments.api.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := URL(tt.gateway)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestURLSchemeSelection(t *testing.T) {
	t.Parallel()

	// Test that the scheme is properly extracted from ingress config
	httpsGateway := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "secure-gateway",
		},
		Spec: v1beta1.GatewaySpec{
			Ingress: &v1beta1.GatewayIngress{
				Host:   "secure.example.com",
				Scheme: "https",
			},
		},
	}

	httpGateway := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "insecure-gateway",
		},
		Spec: v1beta1.GatewaySpec{
			Ingress: &v1beta1.GatewayIngress{
				Host:   "insecure.example.com",
				Scheme: "http",
			},
		},
	}

	require.Contains(t, URL(httpsGateway), "https://")
	require.Contains(t, URL(httpGateway), "http://")
}

func TestURLFallbackToInternalService(t *testing.T) {
	t.Parallel()

	// When no ingress is configured, should fallback to internal service
	gateway := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-gateway",
		},
		Spec: v1beta1.GatewaySpec{
			Ingress: nil,
		},
	}

	result := URL(gateway)

	// Should use internal service URL
	require.Equal(t, "http://gateway:8080", result)
	require.Contains(t, result, "http://")
	require.Contains(t, result, "gateway")
	require.Contains(t, result, "8080")
}