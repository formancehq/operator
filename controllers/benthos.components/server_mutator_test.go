package benthos_components

import (
	benthoscomponentsv1beta2 "github.com/formancehq/operator/apis/benthos.components/v1beta2"
	apisv1beta2 "github.com/formancehq/operator/pkg/apis/v1beta2"
	"github.com/formancehq/operator/pkg/controllerutils"
	. "github.com/formancehq/operator/pkg/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	kClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Server controller", func() {
	mutator := NewServerMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a Benthos server", func() {
				var (
					server *benthoscomponentsv1beta2.Server
				)
				BeforeEach(func() {
					server = &benthoscomponentsv1beta2.Server{
						ObjectMeta: metav1.ObjectMeta{
							Name: "server",
						},
						Spec: benthoscomponentsv1beta2.ServerSpec{
							ResourcesConfigMap: "resources",
							TemplatesConfigMap: "templates",
						},
					}
					Expect(Create(server)).To(BeNil())
					Eventually(ConditionStatus(server, apisv1beta2.ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a pod", func() {
					Eventually(ConditionStatus(server, apisv1beta2.ConditionTypePodReady)).Should(Equal(metav1.ConditionTrue))

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
					Expect(pod.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(server)))

					Expect(pod.Spec.Volumes).NotTo(BeEmpty())
					Expect(pod.Spec.Containers[0].VolumeMounts).NotTo(BeEmpty())
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(server, apisv1beta2.ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      server.Name,
							Namespace: server.Namespace,
						},
					}

					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(server)))
				})
			})
		})
	})
})
