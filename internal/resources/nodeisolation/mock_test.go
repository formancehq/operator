package nodeisolation

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/internal/core"
)

var _ core.Context = (*mockContext)(nil)

// mockContext is a minimal core.Context backed by a fake client, for unit tests.
type mockContext struct {
	context.Context
	client client.Client
	scheme *runtime.Scheme
}

func (m *mockContext) GetClient() client.Client    { return m.client }
func (m *mockContext) GetScheme() *runtime.Scheme  { return m.scheme }
func (m *mockContext) GetAPIReader() client.Reader { return m.client }
func (m *mockContext) GetPlatform() core.Platform  { return core.Platform{} }
