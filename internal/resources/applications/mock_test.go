package applications

import (
	"context"
	"net/http"

	"github.com/formancehq/operator/internal/core"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// mockManager is a minimal implementation of core.Manager for testing
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

func (m *mockManager) GetCache() cache.Cache {
	return nil
}

func (m *mockManager) GetConfig() *rest.Config {
	return nil
}

func (m *mockManager) GetEventRecorderFor(string) record.EventRecorder {
	return nil
}

func (m *mockManager) GetLogger() logr.Logger {
	return logr.Discard()
}

func (m *mockManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (m *mockManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (m *mockManager) GetHTTPClient() *http.Client {
	return nil
}

func (m *mockManager) GetWebhookServer() webhook.Server {
	return nil
}

func (m *mockManager) GetMetricsServer() server.Server {
	return nil
}

func (m *mockManager) GetControllerOptions() config.Controller {
	return config.Controller{}
}

func (m *mockManager) GetPlatform() core.Platform {
	return core.Platform{}
}

// Implement the required manager.Manager methods with minimal implementations
func (m *mockManager) Add(runnable manager.Runnable) error {
	return nil
}

func (m *mockManager) Elected() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (m *mockManager) SetFields(interface{}) error {
	return nil
}

func (m *mockManager) AddMetricsExtraHandler(string, interface{}) error {
	return nil
}

func (m *mockManager) AddHealthzCheck(string, healthz.Checker) error {
	return nil
}

func (m *mockManager) AddReadyzCheck(string, healthz.Checker) error {
	return nil
}

func (m *mockManager) Start(context.Context) error {
	return nil
}

// mockContext is a mock implementation of core.Context
type mockContext struct {
	context.Context
	client   client.Client
	scheme   *runtime.Scheme
	platform core.Platform
}

func (m *mockContext) GetClient() client.Client {
	return m.client
}

func (m *mockContext) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *mockContext) GetAPIReader() client.Reader {
	return m.client
}

func (m *mockContext) GetPlatform() core.Platform {
	return m.platform
}
