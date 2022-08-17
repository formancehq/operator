package clients

import (
	"github.com/google/uuid"
	. "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	pkgInternal "github.com/numary/formance-operator/controllers/components/auth/internal"
	. "github.com/numary/formance-operator/internal/testing"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Client reconciler", func() {
	api := pkgInternal.NewInMemoryAPI()
	mutator := NewMutator(GetClient(), GetScheme(), pkgInternal.ApiFactoryFn(func(referencer pkgInternal.AuthServerReferencer) pkgInternal.API {
		return api
	}))
	AfterEach(func() {
		api.Reset()
	})
	WithMutator(mutator, func() {
		WithNewNamespace(func() {
			newClient := func() *Client {
				return NewClient(uuid.NewString())
			}

			When("Creating a new client object", func() {
				var actualClient *Client
				BeforeEach(func() {
					actualClient = newClient()
					Expect(Create(actualClient)).To(BeNil())
					Eventually(ConditionStatus(actualClient, ConditionTypeReady)).
						Should(Equal(metav1.ConditionTrue))
				})
				AfterEach(func() {
					Expect(client.IgnoreNotFound(Delete(actualClient))).To(BeNil())
					Eventually(Exists(actualClient)).Should(BeFalse())
				})
				It("Should create a new client on auth server", func() {
					Expect(api.Clients()).To(HaveLen(1))
					Expect(actualClient.Status.AuthServerID).NotTo(BeNil())
					Expect(api.Client(actualClient.Status.AuthServerID)).NotTo(BeNil())
					Expect(api.Client(actualClient.Status.AuthServerID).Name).To(Equal(actualClient.Name))
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
					var scope *Scope
					BeforeEach(func() {
						scope = NewScope(uuid.NewString(), uuid.NewString())

						actualClient.AddScopeSpec(scope)
						Expect(Update(actualClient)).To(BeNil())
					})
					It("Should set the client to not ready state", func() {
						Eventually(ConditionStatus(actualClient, ConditionTypeProgressing)).
							Should(Equal(metav1.ConditionTrue))
					})
					Context("Then creating the scope", func() {
						BeforeEach(func() {
							Eventually(ConditionStatus(actualClient, ConditionTypeProgressing)).
								Should(Equal(metav1.ConditionTrue))
							Expect(Create(scope)).To(BeNil())
							scope.Status.AuthServerID = "XXX"
							Expect(GetClient().Status().Update(ActualContext(), scope)).To(BeNil())

							Eventually(ConditionStatus(actualClient, ConditionTypeReady)).
								Should(Equal(metav1.ConditionTrue))
							Expect(actualClient.Status.Scopes).To(Equal(map[string]string{
								scope.Name: scope.Status.AuthServerID,
							}))
						})
						AfterEach(func() {
							Expect(Delete(scope)).To(BeNil())
							Eventually(Exists(scope)).Should(BeFalse())
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
