package components

import (
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta2 "github.com/numary/operator/pkg/apis/v1beta2"
	"github.com/numary/operator/pkg/controllerutils"
	. "github.com/numary/operator/pkg/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Ledger controller", func() {
	mutator := NewLedgerMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a ledger server", func() {
				var (
					ledger *componentsv1beta2.Ledger
				)
				BeforeEach(func() {
					ledger = &componentsv1beta2.Ledger{
						ObjectMeta: metav1.ObjectMeta{
							Name: "ledger",
						},
						Spec: componentsv1beta2.LedgerSpec{
							Postgres: componentsv1beta2.PostgresConfigCreateDatabase{
								PostgresConfigWithDatabase: apisv1beta2.PostgresConfigWithDatabase{
									Database:       "ledger",
									PostgresConfig: NewDumpPostgresConfig(),
								},
								CreateDatabase: true,
							},
							Collector: &componentsv1beta2.CollectorConfig{
								KafkaConfig: apisv1beta2.KafkaConfig{
									Brokers: []string{"http://kafka"},
									TLS:     false,
									SASL:    nil,
								},
								Topic: "xxx",
							},
						},
					}
					Expect(Create(ledger)).To(BeNil())
					Eventually(ConditionStatus(ledger, apisv1beta2.ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(ledger, apisv1beta2.ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      ledger.Name,
							Namespace: ledger.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(ledger)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(ledger, apisv1beta2.ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      ledger.Name,
							Namespace: ledger.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(ledger)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						ledger.Spec.Ingress = &apisv1beta2.IngressSpec{
							Path: "/ledger",
							Host: "localhost",
						}
						Expect(Update(ledger)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(ledger, apisv1beta2.ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      ledger.Name,
								Namespace: ledger.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(ledger)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(ledger, apisv1beta2.ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							ledger.Spec.Ingress = nil
							Expect(Update(ledger)).To(BeNil())
							Eventually(ConditionStatus(ledger, apisv1beta2.ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove the ingress", func() {
							Eventually(NotFound(&networkingv1.Ingress{
								ObjectMeta: metav1.ObjectMeta{
									Name:      ledger.Name,
									Namespace: ledger.Namespace,
								},
							})).Should(BeTrue())
						})
					})
				})
			})
		})
	})
})
