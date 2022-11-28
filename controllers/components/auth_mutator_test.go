package components

import (
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	componentsv1beta2 "github.com/numary/operator/apis/components/v1beta2"
	apisv1beta1 "github.com/numary/operator/pkg/apis/v1beta1"
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

var _ = Describe("Auth controller", func() {
	mutator := NewMutator(GetClient(), GetScheme())
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			Context("When creating a auth server", func() {
				var (
					auth *componentsv1beta2.Auth
				)
				BeforeEach(func() {
					auth = &componentsv1beta2.Auth{
						ObjectMeta: metav1.ObjectMeta{
							Name: "auth",
						},
						Spec: componentsv1beta2.AuthSpec{
							Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
								PostgresConfigWithDatabase: apisv1beta1.PostgresConfigWithDatabase{
									Database:       "auth",
									PostgresConfig: NewDumpPostgresConfig(),
								},
								CreateDatabase: false,
							},
							BaseURL:    "http://localhost/auth",
							SigningKey: "XXXXX",
							DelegatedOIDCServer: componentsv1beta1.DelegatedOIDCServerConfiguration{
								Issuer:       "http://oidc.server",
								ClientID:     "foo",
								ClientSecret: "bar",
							},
						},
					}
					Expect(Create(auth)).To(BeNil())
					Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a deployment", func() {
					Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeDeploymentReady)).Should(Equal(metav1.ConditionTrue))
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      auth.Name,
							Namespace: auth.Namespace,
						},
					}
					Expect(Exists(deployment)()).To(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(auth)))
				})
				It("Should create a service", func() {
					Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      auth.Name,
							Namespace: auth.Namespace,
						},
					}
					Expect(Exists(service)()).To(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(auth)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeServiceReady)).Should(Equal(metav1.ConditionTrue))
						auth.Spec.Ingress = &apisv1beta2.IngressSpec{
							Path: "/auth",
							Host: "localhost",
						}
						Expect(Update(auth)).To(BeNil())
					})
					It("Should create a ingress", func() {
						Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeIngressReady)).Should(Equal(metav1.ConditionTrue))
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      auth.Name,
								Namespace: auth.Namespace,
							},
						}
						Expect(Exists(ingress)()).To(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(auth)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionTrue))
							auth.Spec.Ingress = nil
							Expect(Update(auth)).To(BeNil())
							Eventually(ConditionStatus(auth, apisv1beta1.ConditionTypeIngressReady)).
								Should(Equal(metav1.ConditionUnknown))
						})
						It("Should remove the ingress", func() {
							Eventually(NotFound(&networkingv1.Ingress{
								ObjectMeta: metav1.ObjectMeta{
									Name:      auth.Name,
									Namespace: auth.Namespace,
								},
							})).Should(BeTrue())
						})
					})
				})
			})
		})
	})
})
