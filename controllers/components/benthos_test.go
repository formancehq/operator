package components

import (
	"encoding/json"

	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/testing"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func assertSearchIngestionStreamCreated(service apisv1beta1.Object) {
	ingester := &componentsv1beta2.SearchIngester{
		ObjectMeta: metav1.ObjectMeta{
			Name:      searchIngesterName(service),
			Namespace: service.GetNamespace(),
		},
	}
	Expect(Exists(ingester)()).To(BeTrue())
	Expect(ingester.OwnerReferences).To(HaveLen(1))
	Expect(ingester.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(service)))

	stream := make(map[string]any)
	Expect(json.Unmarshal(ingester.Spec.Stream, &stream)).To(Succeed())

	expectedStream, err := buildStream(service, GetScheme())
	Expect(err).NotTo(HaveOccurred())
	Expect(stream).To(Equal(expectedStream))
}
