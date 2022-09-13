package stack

import (
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	componentsv1beta1 "github.com/numary/operator/apis/components/v1beta1"
	. "github.com/numary/operator/apis/sharedtypes"
	. "github.com/numary/operator/apis/stack/v1beta1"
	. "github.com/numary/operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Stack controller (Auth)", func() {
	mutator := NewMutator(GetClient(), GetScheme(), []string{"*.example.com"}, certmanagermetav1.ObjectReference{
		Name:  "name",
		Kind:  "kind",
		Group: "group",
	})
	WithMutator(mutator, func() {
		Context("When creating stack", func() {
			var (
				stack *Stack
			)
			BeforeEach(func() {
				name := uuid.NewString()
				stack = &Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
					Spec: StackSpec{
						Namespace: name,
					},
				}

				Expect(Create(stack)).To(Succeed())
				Eventually(ConditionStatus(stack, ConditionTypeReady)).
					Should(Equal(metav1.ConditionTrue))
			})
			It("Should create a new namespace", func() {
				Expect(Get(types.NamespacedName{
					Name: stack.Spec.Namespace,
				}, &v1.Namespace{})).To(BeNil())
			})
			Context("With ingress", func() {
				BeforeEach(func() {
					stack.Spec.Ingress = IngressGlobalConfig{
						TLS: &IngressTLS{
							SecretName: uuid.NewString(),
						},
						Enabled: true,
					}
					Expect(Update(stack)).To(BeNil())
				})
			})
			Context("With ledger service", func() {
				BeforeEach(func() {
					stack.Spec.Services.Ledger = &LedgerSpec{
						Postgres: PostgresConfig{
							Port:     1234,
							Host:     "XXX",
							Username: "XXX",
							Password: "XXX",
						},
					}
					Expect(Update(stack)).To(BeNil())
					Eventually(ConditionStatus(stack, ConditionTypeStackLedgerReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a ledger on a new namespace", func() {
					ledger := &componentsv1beta1.Ledger{
						ObjectMeta: metav1.ObjectMeta{
							Name:      stack.ServiceName("ledger"),
							Namespace: stack.Spec.Namespace,
						},
					}
					Expect(Exists(ledger)()).To(BeTrue())
				})
			})
			Context("With auth configuration", func() {
				BeforeEach(func() {
					stack.Spec.Auth = &AuthSpec{
						Postgres: PostgresConfig{
							Port:     5432,
							Host:     "postgres",
							Username: "admin",
							Password: "admin",
						},
						SigningKey: "XXX",
						DelegatedOIDCServer: componentsv1beta1.DelegatedOIDCServerConfiguration{
							Issuer:       "http://example.net",
							ClientID:     "clientId",
							ClientSecret: "clientSecret",
						},
					}
					Expect(Update(stack)).To(BeNil())
					Eventually(ConditionStatus(stack, ConditionTypeStackAuthReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				It("Should create a auth server on a new namespace", func() {
					Expect(Exists(&componentsv1beta1.Auth{
						ObjectMeta: metav1.ObjectMeta{
							Name:      stack.ServiceName("auth"),
							Namespace: stack.Spec.Namespace,
						},
					})()).To(BeTrue())
				})
				Context("Then removing auth", func() {
					BeforeEach(func() {
						stack.Spec.Auth = nil
						Expect(Update(stack)).To(BeNil())
						Eventually(ConditionStatus(stack, ConditionTypeStackAuthReady)).Should(Equal(metav1.ConditionUnknown))
					})
					It("Should remove Auth deployment", func() {
						Expect(Exists(&componentsv1beta1.Auth{
							ObjectMeta: metav1.ObjectMeta{
								Name:      stack.Name,
								Namespace: stack.Spec.Namespace,
							},
						})()).To(BeFalse())
					})
				})
			})
		})
	})
})
