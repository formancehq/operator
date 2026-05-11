package testutil

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	externaldnsv1alpha1 "sigs.k8s.io/external-dns/apis/v1alpha1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
)

type Context struct {
	context.Context
	Client   client.Client
	Scheme   *runtime.Scheme
	Platform core.Platform
}

func (c *Context) GetClient() client.Client {
	return c.Client
}

func (c *Context) GetScheme() *runtime.Scheme {
	return c.Scheme
}

func (c *Context) GetAPIReader() client.Reader {
	return c.Client
}

func (c *Context) GetPlatform() core.Platform {
	return c.Platform
}

func NewScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	must(corev1.AddToScheme(scheme))
	must(appsv1.AddToScheme(scheme))
	must(batchv1.AddToScheme(scheme))
	must(networkingv1.AddToScheme(scheme))
	must(policyv1.AddToScheme(scheme))
	must(externaldnsv1alpha1.AddToScheme(scheme))
	must(v1beta1.AddToScheme(scheme))
	return scheme
}

func NewContext(objects ...client.Object) *Context {
	scheme := NewScheme()
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...)

	withUnstructuredStackIndex(builder, "Auth")
	withUnstructuredStackIndex(builder, "Gateway")
	withUnstructuredStackIndex(builder, "Ledger")
	withUnstructuredStackIndex(builder, "Orchestration")
	withUnstructuredStackIndex(builder, "Payments")
	withUnstructuredStackIndex(builder, "Reconciliation")
	withUnstructuredStackIndex(builder, "Search")
	withUnstructuredStackIndex(builder, "Stargate")
	withUnstructuredStackIndex(builder, "TransactionPlane")
	withUnstructuredStackIndex(builder, "Wallets")
	withUnstructuredStackIndex(builder, "Webhooks")
	withUnstructuredStackIndex(builder, "Database")
	withUnstructuredStackIndex(builder, "Broker")
	withUnstructuredStackIndex(builder, "BrokerTopic")
	withSettingsIndexes(builder)

	return &Context{
		Context: context.Background(),
		Client:  builder.Build(),
		Scheme:  scheme,
	}
}

func EnvMap(env []corev1.EnvVar) map[string]string {
	ret := make(map[string]string, len(env))
	for _, item := range env {
		ret[item.Name] = item.Value
	}
	return ret
}

func MustParseURI(raw string) *v1beta1.URI {
	uri, err := v1beta1.ParseURL(raw)
	must(err)
	return uri
}

func ObjectMeta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type stackGetter interface {
	GetStack() string
}

func withStackIndex(builder *fake.ClientBuilder, object client.Object) {
	builder.WithIndex(object, "stack", func(obj client.Object) []string {
		if dep, ok := obj.(stackGetter); ok && dep.GetStack() != "" {
			return []string{dep.GetStack()}
		}
		return nil
	})
}

func withUnstructuredStackIndex(builder *fake.ClientBuilder, kind string) {
	object := &unstructured.Unstructured{}
	object.SetAPIVersion("formance.com/v1beta1")
	object.SetKind(kind)
	builder.WithIndex(object, "stack", func(obj client.Object) []string {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return nil
		}
		spec, ok := u.Object["spec"].(map[string]any)
		if !ok {
			return nil
		}
		stack, ok := spec["stack"].(string)
		if !ok || stack == "" {
			return nil
		}
		return []string{stack}
	})
}

func withSettingsIndexes(builder *fake.ClientBuilder) {
	builder.WithIndex(&v1beta1.Settings{}, "stack", func(obj client.Object) []string {
		settings := obj.(*v1beta1.Settings)
		return settings.Spec.Stacks
	})
	builder.WithIndex(&v1beta1.Settings{}, "keylen", func(obj client.Object) []string {
		settings := obj.(*v1beta1.Settings)
		return []string{strconv.Itoa(len(splitKeywordWithDot(settings.Spec.Key)))}
	})
}

func splitKeywordWithDot(key string) []string {
	segments := ""
	inQuote := false
	for _, item := range key {
		switch item {
		case '"':
			inQuote = !inQuote
		case '.':
			if !inQuote {
				segments += " "
				continue
			}
			segments += string(item)
		default:
			segments += string(item)
		}
	}
	return strings.Split(segments, " ")
}

type Manager struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	Platform core.Platform
}

var _ core.Manager = (*Manager)(nil)

func (m *Manager) GetClient() client.Client                        { return m.Client }
func (m *Manager) GetScheme() *runtime.Scheme                      { return m.Scheme }
func (m *Manager) GetAPIReader() client.Reader                     { return m.Client }
func (m *Manager) GetPlatform() core.Platform                      { return m.Platform }
func (m *Manager) GetCache() cache.Cache                           { return nil }
func (m *Manager) GetConfig() *rest.Config                         { return nil }
func (m *Manager) GetEventRecorderFor(string) record.EventRecorder { return nil }
func (m *Manager) GetLogger() logr.Logger                          { return logr.Discard() }
func (m *Manager) GetRESTMapper() meta.RESTMapper                  { return nil }
func (m *Manager) GetFieldIndexer() client.FieldIndexer            { return nil }
func (m *Manager) GetHTTPClient() *http.Client                     { return nil }
func (m *Manager) GetWebhookServer() webhook.Server                { return nil }
func (m *Manager) GetMetricsServer() server.Server                 { return nil }
func (m *Manager) GetControllerOptions() config.Controller         { return config.Controller{} }
func (m *Manager) Add(manager.Runnable) error                      { return nil }
func (m *Manager) Elected() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (m *Manager) SetFields(any) error                      { return nil }
func (m *Manager) AddMetricsExtraHandler(string, any) error { return nil }
func (m *Manager) AddMetricsServerExtraHandler(string, http.Handler) error {
	return nil
}
func (m *Manager) AddHealthzCheck(string, healthz.Checker) error { return nil }
func (m *Manager) AddReadyzCheck(string, healthz.Checker) error  { return nil }
func (m *Manager) Start(context.Context) error                   { return nil }
