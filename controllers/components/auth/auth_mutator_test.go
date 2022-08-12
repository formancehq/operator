package auth

import (
	"github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func condition(object *v1beta1.Auth, conditionType string) func() *v1beta1.AuthCondition {
	return func() *v1beta1.AuthCondition {
		err := nsClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			return nil
		}
		return object.Condition(conditionType)
	}
}

func conditionStatus(object *v1beta1.Auth, conditionType string) func() metav1.ConditionStatus {
	return func() metav1.ConditionStatus {
		c := condition(object, conditionType)()
		if c == nil {
			return metav1.ConditionUnknown
		}
		return c.Status
	}
}

func notFound(key client.ObjectKey, object client.Object) func() bool {
	return func() bool {
		return errors.IsNotFound(nsClient.Get(ctx, key, object))
	}
}

func exists(key client.ObjectKey, object client.Object) func() bool {
	return func() bool {
		return nsClient.Get(ctx, key, object) == nil
	}
}

func ownerReference(auth *v1beta1.Auth) metav1.OwnerReference {
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
			auth *v1beta1.Auth
		)
		BeforeEach(func() {
			auth = &v1beta1.Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth",
				},
				Spec: v1beta1.AuthSpec{
					Postgres: v1beta1.PostgresConfig{
						Database: "auth",
						Port:     5432,
						Host:     "postgres",
						Username: "auth",
						Password: "auth",
					},
					BaseURL:    "http://localhost/auth",
					SigningKey: "XXXXX",
					DelegatedOIDCServer: v1beta1.DelegatedOIDCServerConfiguration{
						Issuer:       "http://oidc.server",
						ClientID:     "foo",
						ClientSecret: "bar",
					},
				},
			}
			Expect(nsClient.Create(ctx, auth)).To(BeNil())
			Eventually(conditionStatus(auth, v1beta1.ConditionTypeReady)).Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a deployment", func() {
			Eventually(conditionStatus(auth, v1beta1.ConditionTypeDeploymentCreated)).Should(Equal(metav1.ConditionTrue))
			deployment := &appsv1.Deployment{}
			Expect(exists(client.ObjectKeyFromObject(auth), deployment)()).To(BeTrue())
			Expect(deployment.OwnerReferences).To(HaveLen(1))
			Expect(deployment.OwnerReferences).To(ContainElement(ownerReference(auth)))
		})
		It("Should create a service", func() {
			Eventually(conditionStatus(auth, v1beta1.ConditionTypeServiceCreated)).Should(Equal(metav1.ConditionTrue))
			service := &corev1.Service{}
			Expect(exists(client.ObjectKeyFromObject(auth), service)()).To(BeTrue())
			Expect(service.OwnerReferences).To(HaveLen(1))
			Expect(service.OwnerReferences).To(ContainElement(ownerReference(auth)))
		})
		Context("Then enable ingress", func() {
			BeforeEach(func() {
				Eventually(conditionStatus(auth, v1beta1.ConditionTypeServiceCreated)).Should(Equal(metav1.ConditionTrue))
				auth.Spec.Ingress = &v1beta1.IngressSpec{
					Path: "/auth",
					Host: "localhost",
				}
				Expect(nsClient.Update(ctx, auth)).To(BeNil())
			})
			It("Should create a ingress", func() {
				Eventually(conditionStatus(auth, v1beta1.ConditionTypeIngressCreated)).Should(Equal(metav1.ConditionTrue))
				ingress := &networkingv1.Ingress{}
				Expect(exists(client.ObjectKeyFromObject(auth), ingress)()).To(BeTrue())
				Expect(ingress.OwnerReferences).To(HaveLen(1))
				Expect(ingress.OwnerReferences).To(ContainElement(ownerReference(auth)))
			})
			Context("Then disabling ingress support", func() {
				BeforeEach(func() {
					Eventually(conditionStatus(auth, v1beta1.ConditionTypeIngressCreated)).
						Should(Equal(metav1.ConditionTrue))
					auth.Spec.Ingress = nil
					Expect(nsClient.Update(ctx, auth)).To(BeNil())
					Eventually(conditionStatus(auth, v1beta1.ConditionTypeIngressCreated)).
						Should(Equal(metav1.ConditionUnknown))
				})
				It("Should remove the ingress", func() {
					Eventually(notFound(client.ObjectKeyFromObject(auth), &networkingv1.Ingress{})).Should(BeTrue())
				})
			})
		})
	})
})
