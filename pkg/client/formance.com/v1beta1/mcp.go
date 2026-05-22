package v1beta1

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
)

type MCPInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*v1beta1.MCPList, error)
	Get(ctx context.Context, name string, options metav1.GetOptions) (*v1beta1.MCP, error)
	Create(ctx context.Context, MCP *v1beta1.MCP) (*v1beta1.MCP, error)
	Update(ctx context.Context, MCP *v1beta1.MCP) (*v1beta1.MCP, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Delete(ctx context.Context, name string) error
}

type MCPClient struct {
	restClient rest.Interface
}

func (c *MCPClient) List(ctx context.Context, opts metav1.ListOptions) (*v1beta1.MCPList, error) {
	result := v1beta1.MCPList{}
	err := c.restClient.
		Get().
		Resource("MCPs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (c *MCPClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1beta1.MCP, error) {
	result := v1beta1.MCP{}
	err := c.restClient.
		Get().
		Resource("MCPs").
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (c *MCPClient) Create(ctx context.Context, MCP *v1beta1.MCP) (*v1beta1.MCP, error) {
	result := v1beta1.MCP{}
	err := c.restClient.
		Post().
		Resource("MCPs").
		Body(MCP).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (c *MCPClient) Delete(ctx context.Context, name string) error {
	return c.restClient.
		Delete().
		Resource("MCPs").
		Name(name).
		Do(ctx).
		Error()
}

func (c *MCPClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.restClient.
		Get().
		Resource("MCPs").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(ctx)
}

func (c *MCPClient) Update(ctx context.Context, o *v1beta1.MCP) (*v1beta1.MCP, error) {
	result := v1beta1.MCP{}
	err := c.restClient.
		Put().
		Resource("MCPs").
		Name(o.Name).
		Body(o).
		Do(ctx).
		Into(&result)

	return &result, err
}
