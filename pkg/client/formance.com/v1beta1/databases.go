package v1beta1

import (
	"context"
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

type DatabaseInterface interface {
	List(ctx context.Context, opts metav1.ListOptions) (*v1beta1.DatabaseList, error)
	Get(ctx context.Context, name string, options metav1.GetOptions) (*v1beta1.Database, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
}

type databasesClient struct {
	restClient rest.Interface
}

func (c *databasesClient) List(ctx context.Context, opts metav1.ListOptions) (*v1beta1.DatabaseList, error) {
	result := v1beta1.DatabaseList{}
	err := c.restClient.
		Get().
		Resource("Databases").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (c *databasesClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1beta1.Database, error) {
	result := v1beta1.Database{}
	err := c.restClient.
		Get().
		Resource("Databases").
		Name(name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func (c *databasesClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.restClient.
		Get().
		Resource("Databases").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch(ctx)
}