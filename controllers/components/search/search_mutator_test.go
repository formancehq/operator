package search

import (
	. "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func ownerReference(search *Search) metav1.OwnerReference {
	return metav1.OwnerReference{
		Kind:               "Search",
		APIVersion:         "components.formance.com/v1beta1",
		Name:               "search",
		UID:                search.UID,
		BlockOwnerDeletion: pointer.Bool(true),
		Controller:         pointer.Bool(true),
	}
}

var _ = Describe("Search controller", func() {
	WithMutator(NewMutator(GetClient(), GetScheme()), func() {
		WithNewNamespace(func() {
			Context("When creating a search server", func() {
				var (
					search *Search
				)
				BeforeEach(func() {
					search = &Search{
						ObjectMeta: metav1.ObjectMeta{
							Name: "search",
						},
						Spec: SearchSpec{
							ElasticSearch: ElasticSearchConfig{
								Host:   "XXX",
								Scheme: "XXX",
								Port:   9200,
							},
							KafkaConfig: KafkaConfig{
								Brokers: []string{""},
								TLS:     false,
								SASL:    nil,
							},
						},
					}
					Expect(Create(search)).To(BeNil())
					Eventually(ConditionStatus(search, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(search, ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      search.Name,
							Namespace: search.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(search)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(search, ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      search.Name,
							Namespace: search.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(ownerReference(search)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						search.Spec.Ingress = &IngressSpec{
							Path: "/search",
							Host: "localhost",
						}
						Expect(Update(search)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(search, ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      search.Name,
								Namespace: search.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(ownerReference(search)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(search, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							search.Spec.Ingress = nil
							Expect(Update(search)).To(BeNil())
							Eventually(ConditionStatus(search, ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove the ingress", func() {
							Eventually(NotFound(&networkingv1.Ingress{
								ObjectMeta: metav1.ObjectMeta{
									Name:      search.Name,
									Namespace: search.Namespace,
								},
							})).Should(BeTrue())
						})
					})
				})
			})
		})
	})
})
