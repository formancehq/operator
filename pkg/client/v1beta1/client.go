package v1beta1

import (
	"github.com/formancehq/operator/apis/stack/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func init() {
	if err := v1beta1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
}

type Client struct {
	rest.Interface
}

func NewClient(restClient rest.Interface) *Client {
	return &Client{
		Interface: restClient,
	}
}

func (c *Client) Stacks() StackInterface {
	return &stackClient{
		restClient: c.Interface,
	}
}
