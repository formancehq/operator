package auth_components

import (
	"fmt"

	authcomponentsv1beta2 "github.com/formancehq/operator/apis/auth.components/v1beta2"
	componentsv1beta2 "github.com/formancehq/operator/apis/components/v1beta2"
	"github.com/formancehq/operator/controllers/components"
	apisv1beta2 "github.com/formancehq/operator/pkg/apis/v1beta2"
	. "github.com/formancehq/operator/pkg/testing"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Client reconciler", func() {
	api := components.NewInMemoryAPI()
	mutator := NewClientsMutator(GetClient(), GetScheme(), components.ApiFactoryFn(func(referencer components.AuthServerReferencer) components.API {
		return api
	}))
	AfterEach(func() {
		api.Reset()
	})
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			var (
				authServerReference = uuid.NewString()
				auth                *componentsv1beta2.Auth
			)
			BeforeEach(func() {
				auth = &componentsv1beta2.Auth{
					ObjectMeta: metav1.ObjectMeta{
						Name: fmt.Sprintf("%s-%s", ActualNamespace().Name, authServerReference),
					},
					Spec: componentsv1beta2.AuthSpec{
						Postgres: componentsv1beta2.PostgresConfigCreateDatabase{
							PostgresConfigWithDatabase: apisv1beta2.PostgresConfigWithDatabase{
								PostgresConfig: NewDumpPostgresConfig(),
								Database:       "xxx",
							},
						},
						BaseURL:    "http://localhost:8080",
						SigningKey: "XXX",
						DelegatedOIDCServer: componentsv1beta2.DelegatedOIDCServerConfiguration{
							Issuer:       "http://issuer",
							ClientID:     "xxx",
							ClientSecret: "xxx",
						},
					},
				}
				Expect(Create(auth)).To(BeNil())
			})
			newClient := func() *authcomponentsv1beta2.Client {
				return authcomponentsv1beta2.NewClient(uuid.NewString(), authServerReference)
			}

			When("Creating a new client object", func() {
				var actualClient *authcomponentsv1beta2.Client
				BeforeEach(func() {
					actualClient = newClient()
					Expect(Create(actualClient)).To(BeNil())
					Eventually(ConditionStatus(actualClient, apisv1beta2.ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				AfterEach(func() {
					Expect(client.IgnoreNotFound(Delete(actualClient))).To(BeNil())
					Eventually(Exists(actualClient)).Should(BeFalse())
					Expect(api.Clients()).To(HaveLen(0))
				})
				It("Should create a new client on auth server", func() {
					Expect(api.Clients()).To(HaveLen(1))
					Expect(actualClient.Status.AuthServerID).NotTo(BeNil())
					Expect(api.Client(actualClient.Status.AuthServerID)).NotTo(BeNil())
					Expect(api.Client(actualClient.Status.AuthServerID).Name).To(Equal(actualClient.Name))
				})
				It("Should apply correct ownership", func() {
					Expect(actualClient.GetOwnerReferences()).To(Equal([]metav1.OwnerReference{{
						APIVersion:         "components.formance.com/v1beta2",
						Kind:               "Auth",
						Name:               auth.Name,
						UID:                auth.UID,
						BlockOwnerDeletion: pointer.Bool(true),
					}}))
				})
				Context("Then deleting it", func() {
					BeforeEach(func() {
						Expect(Delete(actualClient)).To(BeNil())
						Eventually(NotFound(actualClient)).Should(BeTrue())
					})
					It("Should be remove on auth server", func() {
						Expect(api.Clients()).To(HaveLen(0))
					})
				})
				Context("Then adding an unknown scope without creating it", func() {
					var scope *authcomponentsv1beta2.Scope
					BeforeEach(func() {
						scope = authcomponentsv1beta2.NewScope(uuid.NewString(), uuid.NewString(), auth.Name)

						actualClient.AddScopeSpec(scope)
						Expect(Update(actualClient)).To(BeNil())
					})
					It("Should set the client to not ready state", func() {
						Eventually(ConditionStatus(actualClient, apisv1beta2.ConditionTypeProgressing)).
							Should(Equal(metav1.ConditionTrue))
					})
					Context("Then creating the scope", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(actualClient, apisv1beta2.ConditionTypeProgressing)).
								Should(Equal(metav1.ConditionTrue))
							Expect(Create(scope)).To(BeNil())
							scope.Status.AuthServerID = "XXX"
							Expect(GetClient().Status().Update(ActualContext(), scope)).To(BeNil())

							Eventually(ConditionStatus(actualClient, apisv1beta2.ConditionTypeReady)).
								Should(Equal(metav1.ConditionTrue))
							Expect(actualClient.Status.Scopes).To(Equal(map[string]string{
								scope.Name: scope.Status.AuthServerID,
							}))
						})
						AfterEach(func() {
							Expect(Delete(scope)).To(BeNil())
							Eventually(Exists(scope)).Should(BeFalse())
							Expect(api.Scopes()).To(HaveLen(0))
						})
						It("Should add scopes to the auth server client", func() {
							Expect(api.Clients()).To(HaveLen(1))
							client := api.Client(actualClient.Status.AuthServerID)
							Expect(client.Scopes).To(HaveLen(1))
							Expect(client.Scopes[0]).To(Equal("XXX"))
						})
						Context("Then remove the scope from the client", func() {
							BeforeEach(func() {
								actualClient.Spec.Scopes = []string{}
								Expect(Update(actualClient)).To(BeNil())
								Eventually(func() map[string]string {
									_ = Get(client.ObjectKeyFromObject(actualClient), actualClient)
									return actualClient.Status.Scopes
								}).Should(Equal(map[string]string{}))
							})
							It("Should delete the scope auth server side", func() {
								Expect(api.Clients()).To(HaveLen(1))
								client := api.Client(actualClient.Status.AuthServerID)
								Expect(client.Scopes).To(HaveLen(0))
							})
						})
					})
				})
			})
		})
	})
})
