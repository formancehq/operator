package benthos

import (
	. "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
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
			Expect(nsClient.Create(ctx, server)).To(BeNil())
			Eventually(ConditionStatus(nsClient, server, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a deployment", func() {
			Eventually(ConditionStatus(nsClient, server, ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      server.Name,
					Namespace: server.Namespace,
				},
			}
			Expect(Exists(nsClient, deployment)()).To(BeTrue())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(server)))
		})
		It("Should create a service", func() {
			Eventually(ConditionStatus(nsClient, server, ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      server.Name,
					Namespace: server.Namespace,
				},
			}
			Expect(Exists(nsClient, service)()).To(BeTrue())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences).To(ContainElement(ownerReference(server)))
		})
	})
})
