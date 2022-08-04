package clients

import (
	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/auth.components/v1beta1"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
)

func newClient() *v1beta1.Client {
	return v1beta1.NewClient(uuid.NewString())
}

func apiClientsLength() int {
	return len(api.clients)
}

func getClient(client *v1beta1.Client) error {
	return nsClient.Get(ctx, types.NamespacedName{
		Name: client.Name,
	}, client)
}

func isClientSynchronized(client *v1beta1.Client) (bool, error) {
	if err := getClient(client); err != nil {
		return false, err
	}
	return client.Status.Synchronized, nil
}

func EventuallyClientSynchronized(client *v1beta1.Client) {
	Eventually(func() (bool, error) {
		ok, err := isClientSynchronized(client)
		if err != nil {
			return false, err
		}
		return ok, nil
	}).WithOffset(1).Should(BeTrue())
}

func EventuallyApiHaveClientsLength(len int) {
	Eventually(apiClientsLength).WithOffset(1).Should(Equal(len))
}
