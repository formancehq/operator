package benthos

import (
	. "github.com/formancehq/operator/apis/components/benthos/v1beta1"
	. "github.com/formancehq/operator/apis/sharedtypes"
	. "github.com/formancehq/operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/utils/pointer"
	kClient "sigs.k8s.io/controller-runtime/pkg/client"
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
						Spec: ServerSpec{
							ResourcesConfigMap: "resources",
							TemplatesConfigMap: "templates",
						},
					}
					Expect(Create(server)).To(BeNil())
					Eventually(ConditionStatus(server, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a pod", func() {
					Eventually(ConditionStatus(server, ConditionTypePodReady)).Should(Equal(metav1.ConditionTrue))

					pods := &corev1.PodList{}
					requirement, err := labels.NewRequirement(serverLabel, selection.Equals, []string{server.Name})
					Expect(err).To(BeNil())
					Expect(GetClient().List(ActualContext(), pods, &kClient.ListOptions{
						Namespace:     server.Namespace,
						LabelSelector: labels.NewSelector().Add(*requirement),
					})).To(BeNil())
					Expect(pods.Items).To(HaveLen(1))

					pod := pods.Items[0]
					Expect(pod.OwnerReferences).To(HaveLen(1))
					Expect(pod.OwnerReferences).To(ContainElement(ownerReference(server)))

					Expect(pod.Spec.Volumes).NotTo(BeEmpty())
					Expect(pod.Spec.Containers[0].VolumeMounts).NotTo(BeEmpty())
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
