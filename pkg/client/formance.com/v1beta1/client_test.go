package v1beta1

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	apiv1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
)

func TestClientAccessorsShareRESTClient(t *testing.T) {
	t.Parallel()

	restClient := newRecordingRESTClient(t, "{}")
	client := NewClient(restClient)

	require.Same(t, restClient, client.Interface)
	require.IsType(t, &stackClient{}, client.Stacks())
	require.IsType(t, &AuthClient{}, client.Auths())
	require.IsType(t, &gatewayClient{}, client.Gateways())
	require.IsType(t, &LedgerClient{}, client.Ledgers())
	require.IsType(t, &OrchestrationClient{}, client.Orchestrations())
	require.IsType(t, &paymentsClient{}, client.Payments())
	require.IsType(t, &reconciliationClient{}, client.Reconciliations())
	require.IsType(t, &SearchClient{}, client.Searches())
	require.IsType(t, &walletsClient{}, client.Wallets())
	require.IsType(t, &webhooksClient{}, client.Webhooks())
	require.IsType(t, &VersionsClient{}, client.Versions())
	require.IsType(t, &databasesClient{}, client.Databases())
}

func TestStackClientRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resource := "stacks"
	t.Run("list", func(t *testing.T) {
		recorder, client := newStackClient(t, listJSON("Stack"))
		got, err := client.List(ctx, metav1.ListOptions{LabelSelector: "app=stack"})
		require.NoError(t, err)
		require.NotNil(t, got)
		recorder.requireRequest(t, http.MethodGet, "/stacks", "labelSelector=app%3Dstack")
	})
	t.Run("get", func(t *testing.T) {
		recorder, client := newStackClient(t, objectJSON("Stack", "stack0"))
		got, err := client.Get(ctx, "stack0", metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, "stack0", got.Name)
		recorder.requireRequest(t, http.MethodGet, "/"+resource+"/stack0", "")
	})
	t.Run("create", func(t *testing.T) {
		recorder, client := newStackClient(t, objectJSON("Stack", "stack0"))
		got, err := client.Create(ctx, &apiv1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}})
		require.NoError(t, err)
		require.Equal(t, "stack0", got.Name)
		recorder.requireRequest(t, http.MethodPost, "/"+resource, "")
	})
	t.Run("update", func(t *testing.T) {
		recorder, client := newStackClient(t, objectJSON("Stack", "stack0"))
		got, err := client.Update(ctx, &apiv1beta1.Stack{ObjectMeta: metav1.ObjectMeta{Name: "stack0"}})
		require.NoError(t, err)
		require.Equal(t, "stack0", got.Name)
		recorder.requireRequest(t, http.MethodPut, "/"+resource+"/stack0", "")
	})
	t.Run("patch", func(t *testing.T) {
		recorder, client := newStackClient(t, objectJSON("Stack", "stack0"))
		got, err := client.Patch(ctx, "stack0", types.MergePatchType, []byte(`{"metadata":{"labels":{"a":"b"}}}`))
		require.NoError(t, err)
		require.Equal(t, "stack0", got.Name)
		recorder.requireRequest(t, http.MethodPatch, "/"+resource+"/stack0", "")
	})
	t.Run("delete", func(t *testing.T) {
		recorder, client := newStackClient(t, statusJSON())
		require.NoError(t, client.Delete(ctx, "stack0"))
		recorder.requireRequest(t, http.MethodDelete, "/"+resource+"/stack0", "")
	})
	t.Run("watch", func(t *testing.T) {
		recorder, client := newStackClient(t, watchJSON("Stack", "stack0"))
		watcher, err := client.Watch(ctx, metav1.ListOptions{})
		require.NoError(t, err)
		require.NotNil(t, watcher)
		watcher.Stop()
		recorder.requireRequest(t, http.MethodGet, "/"+resource, "watch=true")
	})
}

func TestGeneratedModuleClientsRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tests := []struct {
		name     string
		resource string
		kind     string
		run      func(context.Context, rest.Interface) error
	}{
		{
			name:     "auth",
			resource: "Auths",
			kind:     "Auth",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &AuthClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "auth0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Auth{ObjectMeta: metav1.ObjectMeta{Name: "auth0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Auth{ObjectMeta: metav1.ObjectMeta{Name: "auth0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "auth0")
			},
		},
		{
			name:     "gateway",
			resource: "Gateways",
			kind:     "Gateway",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &gatewayClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "gateway0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "gateway0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "gateway0")
			},
		},
		{
			name:     "ledger",
			resource: "Ledgers",
			kind:     "Ledger",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &LedgerClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "ledger0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Ledger{ObjectMeta: metav1.ObjectMeta{Name: "ledger0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "ledger0")
			},
		},
		{
			name:     "orchestration",
			resource: "Orchestrations",
			kind:     "Orchestration",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &OrchestrationClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "orchestration0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Orchestration{ObjectMeta: metav1.ObjectMeta{Name: "orchestration0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Orchestration{ObjectMeta: metav1.ObjectMeta{Name: "orchestration0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "orchestration0")
			},
		},
		{
			name:     "payments",
			resource: "Payments",
			kind:     "Payments",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &paymentsClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "payments0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Payments{ObjectMeta: metav1.ObjectMeta{Name: "payments0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Payments{ObjectMeta: metav1.ObjectMeta{Name: "payments0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "payments0")
			},
		},
		{
			name:     "reconciliation",
			resource: "Reconciliations",
			kind:     "Reconciliation",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &reconciliationClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "reconciliation0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Reconciliation{ObjectMeta: metav1.ObjectMeta{Name: "reconciliation0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Reconciliation{ObjectMeta: metav1.ObjectMeta{Name: "reconciliation0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "reconciliation0")
			},
		},
		{
			name:     "search",
			resource: "Searchs",
			kind:     "Search",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &SearchClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "search0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Search{ObjectMeta: metav1.ObjectMeta{Name: "search0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Search{ObjectMeta: metav1.ObjectMeta{Name: "search0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "search0")
			},
		},
		{
			name:     "wallets",
			resource: "Wallets",
			kind:     "Wallets",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &walletsClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "wallets0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Wallets{ObjectMeta: metav1.ObjectMeta{Name: "wallets0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Wallets{ObjectMeta: metav1.ObjectMeta{Name: "wallets0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "wallets0")
			},
		},
		{
			name:     "webhooks",
			resource: "Webhooks",
			kind:     "Webhooks",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &webhooksClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "webhooks0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Webhooks{ObjectMeta: metav1.ObjectMeta{Name: "webhooks0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Webhooks{ObjectMeta: metav1.ObjectMeta{Name: "webhooks0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "webhooks0")
			},
		},
		{
			name:     "versions",
			resource: "Versions",
			kind:     "Versions",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &VersionsClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "versions0", metav1.GetOptions{})
				if err != nil {
					return err
				}
				_, err = client.Create(ctx, &apiv1beta1.Versions{ObjectMeta: metav1.ObjectMeta{Name: "versions0"}})
				if err != nil {
					return err
				}
				_, err = client.Update(ctx, &apiv1beta1.Versions{ObjectMeta: metav1.ObjectMeta{Name: "versions0"}})
				if err != nil {
					return err
				}
				return client.Delete(ctx, "versions0")
			},
		},
		{
			name:     "databases",
			resource: "Databases",
			kind:     "Database",
			run: func(ctx context.Context, restClient rest.Interface) error {
				client := &databasesClient{restClient: restClient}
				_, err := client.List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}
				_, err = client.Get(ctx, "databases0", metav1.GetOptions{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &requestRecorder{}
			restClient := newRecordingRESTClientWithRecorder(t, recorder, func(req *http.Request) string {
				if req.Method == http.MethodDelete {
					return statusJSON()
				}
				if req.URL.Path == "/"+strings.ToLower(tt.resource) {
					return listJSON(tt.kind)
				}
				return objectJSON(tt.kind, tt.name+"0")
			})

			require.NoError(t, tt.run(ctx, restClient))
			require.Equal(t, http.MethodGet, recorder.requests[0].method)
			require.Equal(t, "/"+strings.ToLower(tt.resource), recorder.requests[0].path)
			require.Equal(t, http.MethodGet, recorder.requests[1].method)
			require.Equal(t, "/"+strings.ToLower(tt.resource)+"/"+tt.name+"0", recorder.requests[1].path)
		})
	}
}

type recordedRequest struct {
	method string
	path   string
	query  string
}

type requestRecorder struct {
	requests []recordedRequest
}

func (r *requestRecorder) requireRequest(t *testing.T, method, path, query string) {
	t.Helper()
	require.NotEmpty(t, r.requests)
	last := r.requests[len(r.requests)-1]
	require.Equal(t, method, last.method)
	require.Equal(t, path, last.path)
	require.Equal(t, query, last.query)
}

func newStackClient(t *testing.T, body string) (*requestRecorder, StackInterface) {
	t.Helper()
	recorder := &requestRecorder{}
	return recorder, &stackClient{restClient: newRecordingRESTClientWithRecorder(t, recorder, func(*http.Request) string {
		return body
	})}
}

func newRecordingRESTClient(t *testing.T, body string) rest.Interface {
	t.Helper()
	return newRecordingRESTClientWithRecorder(t, &requestRecorder{}, func(*http.Request) string {
		return body
	})
}

func newRecordingRESTClientWithRecorder(t *testing.T, recorder *requestRecorder, bodyFor func(*http.Request) string) rest.Interface {
	t.Helper()

	return &restfake.RESTClient{
		GroupVersion:         schema.GroupVersion{Group: "formance.com", Version: "v1beta1"},
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		Client: restfake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
			recorder.requests = append(recorder.requests, recordedRequest{
				method: req.Method,
				path:   req.URL.Path,
				query:  req.URL.RawQuery,
			})
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{runtime.ContentTypeJSON},
				},
				Body: io.NopCloser(strings.NewReader(bodyFor(req))),
			}, nil
		}),
	}
}

func objectJSON(kind, name string) string {
	return fmt.Sprintf(`{"apiVersion":"formance.com/v1beta1","kind":%q,"metadata":{"name":%q}}`, kind, name)
}

func listJSON(kind string) string {
	return fmt.Sprintf(`{"apiVersion":"formance.com/v1beta1","kind":%q,"items":[]}`, kind+"List")
}

func statusJSON() string {
	return `{"apiVersion":"v1","kind":"Status","status":"Success"}`
}

func watchJSON(kind, name string) string {
	return fmt.Sprintf(`{"type":"ADDED","object":%s}`, objectJSON(kind, name))
}
