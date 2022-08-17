package benthos

import (
	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func ownerReference(server *Server) metav1.OwnerReference {
	return metav1.OwnerReference{
		Kind:               "Server",
		APIVersion:         "benthos.components.formance.com/v1beta1",
		Name:               "server",
		UID:                server.UID,
		BlockOwnerDeletion: pointer.Bool(true),
		Controller:         pointer.Bool(true),
	}
}

var _ = Describe("Server controller", func() {
	mutator := NewServerMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a Benthos server", func() {
				var (
					server *Server
				)
				BeforeEach(func() {
					server = &Server{
						ObjectMeta: metav1.ObjectMeta{
							Name: "server",
						},
					}
					Expect(Create(server)).To(BeNil())
					Eventually(ConditionStatus(server, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a pod", func() {
					Eventually(ConditionStatus(server, ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					pod := &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      server.Name,
							Namespace: server.Namespace,
						},
					}
					Expect(Exists(pod)()).To(BeTrue())
					Expect(pod.OwnerReferences).To(HaveLen(1))
					Expect(pod.OwnerReferences).To(ContainElement(ownerReference(server)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(server, ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      server.Name,
							Namespace: server.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(ownerReference(server)))
				})
			})
		})
	})
})
