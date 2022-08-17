package stack

import (
	"github.com/google/uuid"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	. "github.com/numary/formance-operator/apis/stack/v1beta1"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("Stack controller (Auth)", func() {
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

			Expect(k8sClient.Create(ctx, stack)).To(Succeed())
			Eventually(ConditionStatus(k8sClient, stack, ConditionTypeReady)).
				Should(Equal(metav1.ConditionTrue))
		})
		It("Should create a new namespace", func() {
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: stack.Spec.Namespace,
			}, &v1.Namespace{})).To(BeNil())
		})
		Context("With ledger service", func() {
			BeforeEach(func() {
				stack.Spec.Services.Ledger = &LedgerSpec{
					Postgres: componentsv1beta1.PostgresConfigCreateDatabase{
						PostgresConfig: PostgresConfig{
							Database: "XXX",
							Port:     1234,
							Host:     "XXX",
							Username: "XXX",
							Password: "XXX",
						},
					},
				}
				// TODO: Actually, Ledger depends on elasticsearch config and take it from search service
				// TODO: Remove when the abstraction will be in place
				stack.Spec.Services.Search = &SearchSpec{
					ElasticSearchConfig: &componentsv1beta1.ElasticSearchConfig{
						Host:   "XXX",
						Scheme: "XXX",
						Port:   9200,
					},
				}
				Expect(k8sClient.Update(ctx, stack)).To(BeNil())
				Eventually(ConditionStatus(k8sClient, stack, ConditionTypeStackLedgerReady)).
					Should(Equal(metav1.ConditionTrue))
			})
			It("Should create a ledger on a new namespace", func() {
				ledger := &componentsv1beta1.Ledger{
					ObjectMeta: metav1.ObjectMeta{
						Name:      stack.ServiceName("ledger"),
						Namespace: stack.Spec.Namespace,
					},
				}
				Expect(Exists(k8sClient, ledger)()).To(BeTrue())
			})
		})
		Context("With auth configuration", func() {
			BeforeEach(func() {
				stack.Spec.Auth = &AuthSpec{
					PostgresConfig: PostgresConfig{
						Database: "test",
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
				Expect(k8sClient.Update(ctx, stack)).To(BeNil())
				Eventually(ConditionStatus(k8sClient, stack, ConditionTypeStackAuthReady)).
					Should(Equal(metav1.ConditionTrue))
			})
			It("Should create a auth server on a new namespace", func() {
				Expect(Exists(k8sClient, &componentsv1beta1.Auth{
					ObjectMeta: metav1.ObjectMeta{
						Name:      stack.ServiceName("auth"),
						Namespace: stack.Spec.Namespace,
					},
				})()).To(BeTrue())
			})
			Context("Then removing auth", func() {
				BeforeEach(func() {
					stack.Spec.Auth = nil
					Expect(k8sClient.Update(ctx, stack)).To(BeNil())
					Eventually(ConditionStatus(k8sClient, stack, ConditionTypeStackAuthReady)).Should(Equal(metav1.ConditionUnknown))
				})
				It("Should remove Auth deployment", func() {
					Expect(Exists(k8sClient, &componentsv1beta1.Auth{
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
