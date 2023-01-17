package components

import (
	componentsv1beta2 "github.com/formancehq/operator/apis/components/v1beta2"
	apisv1beta2 "github.com/formancehq/operator/pkg/apis/v1beta2"
	"github.com/formancehq/operator/pkg/controllerutils"
	. "github.com/formancehq/operator/pkg/testing"
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
							Postgres: componentsv1beta2.PostgresConfigCreateDatabase{
								PostgresConfigWithDatabase: apisv1beta2.PostgresConfigWithDatabase{
									Database:       "auth",
									PostgresConfig: NewDumpPostgresConfig(),
								},
								CreateDatabase: false,
							},
							BaseURL:    "http://localhost/auth",
							SigningKey: "XXXXX",
							DelegatedOIDCServer: componentsv1beta2.DelegatedOIDCServerConfiguration{
								Issuer:       "http://oidc.server",
								ClientID:     "foo",
								ClientSecret: "bar",
							},
						},
					}
					Expect(Create(auth)).To(BeNil())
				})
				It("Should create a deployment", func() {
					deployment := &appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      auth.Name,
							Namespace: auth.Namespace,
						},
					}
					Eventually(Exists(deployment)).Should(BeTrue())
					Expect(deployment.OwnerReferences).To(HaveLen(1))
					Expect(deployment.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(auth)))
					Expect(deployment.Annotations).To(HaveKey(controllerutils.ReloaderAnnotationKey))
				})
				It("Should create a service", func() {
					service := &corev1.Service{
						ObjectMeta: metav1.ObjectMeta{
							Name:      auth.Name,
							Namespace: auth.Namespace,
						},
					}
					Eventually(Exists(service)).Should(BeTrue())
					Expect(service.OwnerReferences).To(HaveLen(1))
					Expect(service.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(auth)))
				})
				Context("Then enable ingress", func() {
					BeforeEach(func() {
						auth.Spec.Ingress = &apisv1beta2.IngressSpec{
							Path: "/auth",
							Host: "localhost",
						}
						Expect(Update(auth)).To(BeNil())
					})
					It("Should create a ingress", func() {
						ingress := &networkingv1.Ingress{
							ObjectMeta: metav1.ObjectMeta{
								Name:      auth.Name,
								Namespace: auth.Namespace,
							},
						}
						Eventually(Exists(ingress)).Should(BeTrue())
						Expect(ingress.OwnerReferences).To(HaveLen(1))
						Expect(ingress.OwnerReferences).To(ContainElement(controllerutils.OwnerReference(auth)))
					})
					Context("Then disabling ingress support", func() {
						BeforeEach(func() {
							auth.Spec.Ingress = nil
							Expect(Update(auth)).To(BeNil())
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
