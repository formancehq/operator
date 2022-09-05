package v1

import (
	"github.com/numary/formance-operator/apis/stack/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

func init() {
	v1beta1.AddToScheme(scheme.Scheme)
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
