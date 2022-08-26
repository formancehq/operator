package scopes

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/numary/auth/authclient"
	. "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	"github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	pkgInternal "github.com/numary/formance-operator/controllers/components/auth/internal"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	kClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Scope reconciler", func() {

	api := pkgInternal.NewInMemoryAPI()
	mutator := NewMutator(GetClient(), GetScheme(), pkgInternal.ApiFactoryFn(func(referencer pkgInternal.AuthServerReferencer) pkgInternal.API {
		return api
	}))
	AfterEach(func() {
		api.Reset()
	})

	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			var (
				auth                *v1beta1.Auth
				authServerReference string
			)
			BeforeEach(func() {
				authServerReference = uuid.NewString()
				auth = &v1beta1.Auth{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("%s-%s", ActualNamespace().Name, authServerReference),
					},
					Spec: v1beta1.AuthSpec{
						Postgres: v1beta1.PostgresConfigCreateDatabase{
							PostgresConfigWithDatabase: PostgresConfigWithDatabase{
								PostgresConfig: PostgresConfig{
									Port:     8080,
									Host:     "foo",
									Username: "xxx",
									Password: "xxx",
								},
								Database: "xxx",
							},
						},
						BaseURL:    "http://localhost:8080",
						SigningKey: "XXX",
						DelegatedOIDCServer: v1beta1.DelegatedOIDCServerConfiguration{
							Issuer:       "http://issuer",
							ClientID:     "xxx",
							ClientSecret: "xxx",
						},
					},
				}
				Expect(Create(auth)).To(BeNil())
				Eventually(Exists(auth)).Should(BeTrue())
			})
			newScope := func(transient ...string) *Scope {
				return NewScope(uuid.NewString(), uuid.NewString(), authServerReference, transient...)
			}
			When("Creating a new scope object", func() {
				var firstScope *Scope
				BeforeEach(func() {
					firstScope = newScope()
					Expect(Create(firstScope)).To(BeNil())
					Eventually(ConditionStatus(firstScope, ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				AfterEach(func() {
					Expect(kClient.IgnoreNotFound(Delete(firstScope))).To(Succeed())
					Eventually(Exists(firstScope)).Should(BeFalse())
					Expect(api.Scopes()).To(HaveLen(0))
				})
				It("Should create a new scope on auth server", func() {
					Expect(api.Scopes()).To(HaveLen(1))
					Expect(firstScope.Status.AuthServerID).NotTo(BeNil())
					Expect(api.Scope(firstScope.Status.AuthServerID)).NotTo(BeNil())
					Expect(api.Scope(firstScope.Status.AuthServerID).Label).To(Equal(firstScope.Spec.Label))
				})
				It("Should apply correct ownership", func() {
					Expect(firstScope.GetOwnerReferences()).To(Equal([]metav1.OwnerReference{{
						APIVersion:         "components.formance.com/v1beta1",
						Kind:               "Auth",
						Name:               auth.Name,
						UID:                auth.UID,
						BlockOwnerDeletion: pointer.Bool(true),
					}}))
				})
				Context("Then updating with a new label", func() {
					BeforeEach(func() {
						firstScope.Spec.Label = uuid.NewString()
						Expect(Update(firstScope)).To(BeNil())
					})
					It("Should update the label on auth server", func() {
						Eventually(func() bool {
							return api.Scope(firstScope.Status.AuthServerID).Label == firstScope.Spec.Label
						}).Should(BeTrue())
					})
				})
				Context("Then deleting it", func() {
					BeforeEach(func() {
						Expect(api.Scopes()).To(HaveLen(1))
						Expect(Delete(firstScope)).To(BeNil())
						Eventually(Exists(firstScope)).Should(BeFalse())
					})
					It("Should remove the scope auth server side", func() {
						Eventually(func() map[string]*authclient.Scope {
							return api.Scopes()
						}).Should(BeEmpty())
					})
				})
				Context("Then creating a new scope with the first as transient", func() {
					var secondScope *Scope
					BeforeEach(func() {
						secondScope = newScope(firstScope.Name)
						Expect(Create(secondScope)).To(BeNil())
						Eventually(ConditionStatus(secondScope, ConditionTypeReady)).
							Should(Equal(metav1.ConditionTrue))
					})
					AfterEach(func() {
						Expect(kClient.IgnoreNotFound(Delete(secondScope))).To(Succeed())
						Eventually(Exists(secondScope)).Should(BeFalse())
						Expect(api.Scopes()).To(HaveLen(1))
					})
					It("Should create scope with transient on auth server", func() {
						Expect(api.Scopes()).To(HaveLen(2))
						Expect(api.Scope(secondScope.Status.AuthServerID).Transient).To(Equal([]string{
							firstScope.Status.AuthServerID,
						}))
					})
					Context("Then removing transient scope", func() {
						BeforeEach(func() {
							Expect(api.Scope(secondScope.Status.AuthServerID).Transient).To(Equal([]string{
								firstScope.Status.AuthServerID,
							}))
							secondScope.Spec.Transient = make([]string, 0)
							Expect(Update(secondScope)).To(BeNil())
						})
						It("Should remove transient scope auth server side", func() {
							Eventually(func() []string {
								return api.Scope(secondScope.Status.AuthServerID).Transient
							}).Should(BeEmpty())
						})
					})
				})
			})
		})
	})
})
