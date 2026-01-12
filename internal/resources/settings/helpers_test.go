package settings

import (
	"context"
	"fmt"
	"testing"

	"net/http"

	. "github.com/formancehq/go-libs/v2/collectionutils"
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestSplitKeywordWithDot(t *testing.T) {
	t.Parallel()
	type testCase struct {
		key            string
		expectedResult []string
	}
	testCases := []testCase{
		{
			key:            `"postgres.payments.dsn"`,
			expectedResult: []string{"postgres.payments.dsn"},
		},
		{
			key:            `resource-requirements."payments.io".containers.payments.limits`,
			expectedResult: []string{"resource-requirements", "payments.io", "containers", "payments", "limits"},
		},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			t.Parallel()
			result := SplitKeywordWithDot(tc.key)
			require.Equal(t, tc.expectedResult, result)
		})
	}

}

func TestFindMatchingSettings(t *testing.T) {
	t.Parallel()
	type settings struct {
		key        string
		value      string
		isWildcard bool
	}
	type testCase struct {
		settings       []settings
		key            string
		expectedResult string
	}
	testCases := []testCase{
		{
			settings: []settings{
				{"postgres.ledger.dsn", "postgresql://localhost:5433", false},
				{"postgres.*.dsn", "postgresql://localhost:5432", false},
			},
			key:            "postgres.payments.dsn",
			expectedResult: "postgresql://localhost:5432",
		},
		{
			settings: []settings{
				{"postgres.*.dsn", "postgresql://localhost:5432", false},
				{"postgres.ledger.dsn", "postgresql://localhost:5433", false},
			},
			key:            "postgres.ledger.dsn",
			expectedResult: "postgresql://localhost:5433",
		},
		{
			settings: []settings{
				{"resource-requirements.*.containers.*.limits", "vvv", false},
				{"resource-requirements.ledger.containers.*.limits", "xxx", false},
			},
			key:            "resource-requirements.ledger.containers.ledger.limits",
			expectedResult: "xxx",
		},
		{
			settings: []settings{
				{"resource-requirements.*.containers.*.limits", "vvv", false},
				{"resource-requirements.*.containers.ledger.limits", "xxx", false},
			},
			key:            "resource-requirements.payments.containers.payments.limits",
			expectedResult: "vvv",
		},
		{
			settings: []settings{
				{"resource-requirements.*.containers.ledger.limits", "xxx", false},
				{"resource-requirements.*.containers.*.limits", "vvv", false},
			},
			key:            "resource-requirements.ledger.containers.ledger.limits",
			expectedResult: "xxx",
		},
		{
			settings: []settings{
				{"resource-requirements.*.containers.*.limits", "memory=512Mi", true},
				{"resource-requirements.*.containers.*.limits", "memory=1024Mi", false},
			},
			key:            "resource-requirements.ledger.containers.ledger.limits",
			expectedResult: "memory=1024Mi",
		},
		{
			settings: []settings{
				{"resource-requirements.ledger.containers.ledger.limits", "memory=512Mi", true},
				{"resource-requirements.*.containers.*.limits", "memory=1024Mi", false},
			},
			key:            "resource-requirements.ledger.containers.ledger.limits",
			expectedResult: "memory=1024Mi",
		},
		{
			settings: []settings{
				{"resource-requirements.ledger.containers.ledger.limits", "memory=512Mi", true},
				{"resource-requirements.*.containers.*.limits", "memory=1024Mi", false},
			},
			key:            "resource-requirements.payments.containers.payments.limits",
			expectedResult: "memory=1024Mi",
		},
		{
			settings: []settings{
				{"resource-requirements.*.containers.payments.limits", "memory=512Mi", true},
				{"resource-requirements.*.containers.*.limits", "memory=1024Mi", false},
			},
			key:            "resource-requirements.payments.containers.payments.limits",
			expectedResult: "memory=1024Mi",
		},
		{
			settings: []settings{
				{
					key:        `registries."ghcr.io".images.ledger.rewrite`,
					value:      "example",
					isWildcard: false,
				},
			},
			key:            `registries."ghcr.io".images.ledger.rewrite`,
			expectedResult: "example",
		},
		{
			settings: []settings{
				{
					key:        "registries.*.images.ledger.rewrite",
					value:      "example",
					isWildcard: false,
				},
			},
			key:            `registries."ghcr.io".images.ledger.rewrite`,
			expectedResult: "example",
		},
		{
			settings: []settings{
				{
					key:        "registries.*.images.caddy/caddy.rewrite",
					value:      "example",
					isWildcard: false,
				},
			},
			key:            `registries."docker.io".images.caddy/caddy.rewrite`,
			expectedResult: "example",
		},
		{
			settings: []settings{
				{
					key:   "registries.*.endpoint",
					value: "example.com",
				},
			},
			key:            `registries."ghcr.io".endpoint`,
			expectedResult: "example.com",
		},
		{
			settings: []settings{
				{
					key:   "registries.*.endpoint",
					value: "example.com",
				},
			},
			key:            `registries."public.ecr.aws".endpoint`,
			expectedResult: "example.com",
		},
		{
			settings: []settings{
				{
					key:   "registries.*.endpoint",
					value: "example.com",
				},
			},
			key:            `registries."docker.io".endpoint`,
			expectedResult: "example.com",
		},
	}
	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			t.Parallel()
			value, err := findMatchingSettings(core.NewContext(nil, context.Background()), "test", Map(tc.settings, func(from settings) v1beta1.Settings {
				ret := v1beta1.Settings{
					Spec: v1beta1.SettingsSpec{
						Key:   from.key,
						Value: from.value,
					},
				}
				if from.isWildcard {
					ret.Spec.Stacks = []string{"*"}
				}
				return ret
			}), SplitKeywordWithDot(tc.key)...)
			require.NoError(t, err)
			require.NotNil(t, value)
			require.Equal(t, tc.expectedResult, *value)
		})
	}

}

func TestParseKeyValuePair(t *testing.T) {
	ret, err := parseKeyValueList(`a=b,c="d e", f=g,h="i,j"`)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"a": "b",
		"c": "d e",
		"f": "g",
		"h": "i,j",
	}, ret)
}

func TestFindMatchingSettingsWithValueFrom(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	tests := []struct {
		name           string
		stack          string
		setting        v1beta1.Settings
		secrets        []*corev1.Secret
		configMaps     []*corev1.ConfigMap
		expectedResult string
		expectedError  string
	}{
		{
			name:  "resolve from secret in stack namespace",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "postgres-secret",
							},
							Key: "connection-string",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-secret",
						Namespace: "test-stack",
					},
					Data: map[string][]byte{
						"connection-string": []byte("postgresql://localhost:5432/test"),
					},
				},
			},
			expectedResult: "postgresql://localhost:5432/test",
		},
		{
			name:  "resolve from configmap in stack namespace",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "postgres-config",
							},
							Key: "uri",
						},
					},
				},
			},
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-config",
						Namespace: "test-stack",
					},
					Data: map[string]string{
						"uri": "postgresql://localhost:5432/test",
					},
				},
			},
			expectedResult: "postgresql://localhost:5432/test",
		},
		{
			name:  "fallback to formance-system namespace when not found in stack namespace",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "postgres-secret",
							},
							Key: "connection-string",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-secret",
						Namespace: "formance-system",
					},
					Data: map[string][]byte{
						"connection-string": []byte("postgresql://localhost:5432/test"),
					},
				},
			},
			expectedResult: "postgresql://localhost:5432/test",
		},
		// Note: Test case "value takes precedence over valueFrom" removed because
		// CEL validation now enforces exactly one of value/valueFrom to be set.
		// Having both set is an impossible state at runtime.
		{
			name:  "optional secret not found returns empty string",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "missing-secret",
							},
							Key:      "connection-string",
							Optional: func() *bool { b := true; return &b }(),
						},
					},
				},
			},
			expectedResult: "", // Optional resources return empty string when not found
		},
		{
			name:  "optional configmap not found returns empty string",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "missing-configmap",
							},
							Key:      "uri",
							Optional: func() *bool { b := true; return &b }(),
						},
					},
				},
			},
			expectedResult: "", // Optional resources return empty string when not found
		},
		{
			name:  "secret not found returns error",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "missing-secret",
							},
							Key: "connection-string",
						},
					},
				},
			},
			expectedError: "resource not found",
		},
		{
			name:  "key not found in secret returns error",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "postgres-secret",
							},
							Key: "missing-key",
						},
					},
				},
			},
			secrets: []*corev1.Secret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-secret",
						Namespace: "test-stack",
					},
					Data: map[string][]byte{
						"connection-string": []byte("postgresql://localhost:5432/test"),
					},
				},
			},
			expectedError: "not found in secret",
		},
		{
			name:  "key not found in configmap returns error",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "postgres-config",
							},
							Key: "missing-key",
						},
					},
				},
			},
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-config",
						Namespace: "test-stack",
					},
					Data: map[string]string{
						"uri": "postgresql://localhost:5432/test",
					},
				},
			},
			expectedError: "not found in configmap",
		},
		{
			name:  "configmap binary data",
			stack: "test-stack",
			setting: v1beta1.Settings{
				ObjectMeta: metav1.ObjectMeta{Name: "test-setting"},
				Spec: v1beta1.SettingsSpec{
					Key: "postgres.ledger.uri",
					ValueFrom: &v1beta1.ValueFrom{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "postgres-config",
							},
							Key: "uri",
						},
					},
				},
			},
			configMaps: []*corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "postgres-config",
						Namespace: "test-stack",
					},
					BinaryData: map[string][]byte{
						"uri": []byte("postgresql://localhost:5432/test"),
					},
				},
			},
			expectedResult: "postgresql://localhost:5432/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build list of objects for fake client
			objects := make([]client.Object, 0)
			for _, secret := range tt.secrets {
				objects = append(objects, secret)
			}
			for _, cm := range tt.configMaps {
				objects = append(objects, cm)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			// Create a mock manager
			mockMgr := &mockManager{
				client: fakeClient,
				scheme: scheme,
			}

			coreMgr := core.NewDefaultManager(mockMgr, core.Platform{
				Region:      "test",
				Environment: "test",
			})

			ctx := core.NewContext(coreMgr, context.Background())

			value, err := findMatchingSettings(ctx, tt.stack, []v1beta1.Settings{tt.setting}, SplitKeywordWithDot(tt.setting.Spec.Key)...)

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectedError)
				require.Nil(t, value)
			} else {
				require.NoError(t, err)
				require.NotNil(t, value)
				require.Equal(t, tt.expectedResult, *value)
			}
		})
	}
}

// mockManager implements ctrl.Manager for testing
type mockManager struct {
	client client.Client
	scheme *runtime.Scheme
}

func (m *mockManager) GetClient() client.Client {
	return m.client
}

func (m *mockManager) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *mockManager) GetAPIReader() client.Reader {
	return m.client
}

// Implement other required methods with no-ops or panics
func (m *mockManager) Add(_ manager.Runnable) error                          { return nil }
func (m *mockManager) SetFields(_ interface{}) error                         { return nil }
func (m *mockManager) AddMetricsExtraHandler(_ string, _ http.Handler) error { return nil }
func (m *mockManager) AddHealthzCheck(_ string, _ healthz.Checker) error     { return nil }
func (m *mockManager) AddReadyzCheck(_ string, _ healthz.Checker) error      { return nil }
func (m *mockManager) Start(_ context.Context) error                         { return nil }
func (m *mockManager) GetWebhookServer() webhook.Server                      { return nil }
func (m *mockManager) GetLogger() logr.Logger                                { return logr.Discard() }
func (m *mockManager) GetControllerOptions() config.Controller               { return config.Controller{} }
func (m *mockManager) GetCache() cache.Cache                                 { return nil }
func (m *mockManager) GetEventRecorderFor(_ string) record.EventRecorder     { return nil }
func (m *mockManager) GetRESTMapper() meta.RESTMapper                        { return nil }
func (m *mockManager) GetHTTPClient() *http.Client                           { return nil }
func (m *mockManager) GetConfig() *rest.Config                               { return nil }
func (m *mockManager) Elected() <-chan struct{}                              { return nil }
func (m *mockManager) GetFieldIndexer() client.FieldIndexer                  { return nil }
