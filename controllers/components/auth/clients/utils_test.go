package clients

import (
	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

func newClient() *v1beta1.Client {
	return v1beta1.NewClient(uuid.NewString())
}

func fetchClient(client *v1beta1.Client) bool {
	err := nsClient.Get(ctx, types.NamespacedName{
		Namespace: client.Namespace,
		Name:      client.Name,
	}, client)
	switch {
	case errors.IsNotFound(err):
		return false
	case err != nil:
		panic(err)
	default:
		return true
	}
}

func clientReady(client *v1beta1.Client) func() bool {
	return func() bool {
		if !fetchClient(client) {
			return false
		}
		return client.Status.Ready
	}
}

func clientSynchronizedScopes(client *v1beta1.Client) func() map[string]string {
	return func() map[string]string {
		if !fetchClient(client) {
			return nil
		}
		return client.Status.Scopes
	}
}

func clientNotFound(client *v1beta1.Client) func() bool {
	return func() bool {
		if !fetchClient(client) {
			return true
		}
		return false
	}
}
