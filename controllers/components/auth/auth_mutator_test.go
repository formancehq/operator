package auth

import (
	. "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func ownerReference(auth *Auth) metav1.OwnerReference {
	return metav1.OwnerReference{
		Kind:               "Auth",
		APIVersion:         "components.formance.com/v1beta1",
		Name:               "auth",
		UID:                auth.UID,
		BlockOwnerDeletion: pointer.Bool(true),
		Controller:         pointer.Bool(true),
	}
}

var _ = Describe("Auth controller", func() {
	Context("When creating a auth server", func() {
		var (
			auth *Auth
		)
		BeforeEach(func() {
			auth = &Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth",
				},
				Spec: AuthSpec{
					Postgres: sharedtypes.PostgresConfig{
						Database: "auth",
						Port:     5432,
						Host:     "postgres",
						Username: "auth",
						Password: "auth",
					},
					BaseURL:    "http://localhost/auth",
					SigningKey: "XXXXX",
					DelegatedOIDCServer: DelegatedOIDCServerConfiguration{
						Issuer:       "http://oidc.server",
						ClientID:     "foo",
						ClientSecret: "bar",
					},
				},
			}
			Expect(nsClient.Create(ctx, auth)).To(BeNil())
			Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a deployment", func() {
			Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeDeploymentCreated)).Should(Equal(metav1.ConditionTrue))
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      auth.Name,
					Namespace: auth.Namespace,
				},
			}
			Expect(Exists(nsClient, deployment)()).To(BeTrue())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(auth)))
		})
		It("Should create a service", func() {
			Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeServiceCreated)).Should(Equal(metav1.ConditionTrue))
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      auth.Name,
					Namespace: auth.Namespace,
				},
			}
			Expect(Exists(nsClient, service)()).To(BeTrue())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences).To(ContainElement(ownerReference(auth)))
		})
		Context("Then enable ingress", func() {
			BeforeEach(func() {
				Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeServiceCreated)).Should(Equal(metav1.ConditionTrue))
				auth.Spec.Ingress = &sharedtypes.IngressSpec{
					Path: "/auth",
					Host: "localhost",
				}
				Expect(nsClient.Update(ctx, auth)).To(BeNil())
			})
			It("Should create a ingress", func() {
				Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeIngressCreated)).Should(Equal(metav1.ConditionTrue))
				ingress := &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      auth.Name,
						Namespace: auth.Namespace,
					},
				}
				Expect(Exists(nsClient, ingress)()).To(BeTrue())
				Expect(ingress.OwnerReferences).To(HaveLen(1))
				Expect(ingress.OwnerReferences).To(ContainElement(ownerReference(auth)))
			})
			Context("Then disabling ingress support", func() {
				BeforeEach(func() {
					Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeIngressCreated)).
						Should(Equal(metav1.ConditionTrue))
					auth.Spec.Ingress = nil
					Expect(nsClient.Update(ctx, auth)).To(BeNil())
					Eventually(ConditionStatus[AuthCondition](nsClient, auth, ConditionTypeIngressCreated)).
						Should(Equal(metav1.ConditionUnknown))
				})
				It("Should remove the ingress", func() {
					Eventually(NotFound(nsClient, &networkingv1.Ingress{
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
