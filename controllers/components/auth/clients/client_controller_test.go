package clients

import (
	"github.com/google/uuid"
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConditionAware interface {
	client.Object
	Condition(v string) *authcomponentsv1beta1.ConditionClient
}

func Condition(object ConditionAware, condition string) func() *authcomponentsv1beta1.ConditionClient {
	return func() *authcomponentsv1beta1.ConditionClient {
		err := nsClient.Get(ctx, client.ObjectKeyFromObject(object), object)
		if err != nil {
			return nil
		}
		return object.Condition(condition)
	}
}

func ConditionStatus(object ConditionAware, condition string) func() metav1.ConditionStatus {
	return func() metav1.ConditionStatus {
		c := Condition(object, condition)()
		if c == nil {
			return metav1.ConditionUnknown
		}
		return c.Status
	}
}

func NotFound(object client.Object) func() bool {
	return func() bool {
		err := nsClient.Get(ctx, types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		}, object)
		switch {
		case errors.IsNotFound(err):
			return false
		case err != nil:
			panic(err)
		default:
			return true
		}
	}
}

var _ = Describe("Client reconciler", func() {

	newClient := func() *authcomponentsv1beta1.Client {
		return authcomponentsv1beta1.NewClient(uuid.NewString())
	}

	When("Creating a new client object", func() {
		var actualClient *authcomponentsv1beta1.Client
		BeforeEach(func() {
			actualClient = newClient()
			Expect(nsClient.Create(ctx, actualClient)).To(BeNil())
			Eventually(ConditionStatus(actualClient, authcomponentsv1beta1.ConditionTypeClientCreated)).
				Should(Equal(metav1.ConditionTrue))
		})
		AfterEach(func() {
			Expect(client.IgnoreNotFound(nsClient.Delete(ctx, actualClient))).To(BeNil())
		})
		It("Should create a new client on auth server", func() {
			Expect(api.Clients()).To(HaveLen(1))
			Expect(actualClient.Status.AuthServerID).NotTo(BeNil())
			Expect(api.Client(actualClient.Status.AuthServerID)).NotTo(BeNil())
			Expect(api.Client(actualClient.Status.AuthServerID).Name).To(Equal(actualClient.Name))
		})
		Context("Then deleting it", func() {
			BeforeEach(func() {
				Expect(nsClient.Delete(ctx, actualClient)).To(BeNil())
				Eventually(NotFound(actualClient)).Should(BeTrue())
			})
			It("Should be remove on auth server", func() {
				Expect(api.Clients()).To(HaveLen(0))
			})
		})
		Context("Then adding an unknown scope without creating it", func() {
			var scope *authcomponentsv1beta1.Scope
			BeforeEach(func() {
				scope = authcomponentsv1beta1.NewScope(uuid.NewString(), uuid.NewString())

				actualClient.AddScopeSpec(scope)
				Expect(nsClient.Update(ctx, actualClient)).To(BeNil())
			})
			It("Should set the client to not ready state", func() {
				Eventually(ConditionStatus(actualClient, authcomponentsv1beta1.ConditionTypeClientProgressing)).
					Should(Equal(metav1.ConditionTrue))
			})
			Context("Then creating the scope", func() {
				BeforeEach(func() {
					Eventually(ConditionStatus(actualClient, authcomponentsv1beta1.ConditionTypeClientProgressing)).
						Should(Equal(metav1.ConditionTrue))
					Expect(nsClient.Create(ctx, scope)).To(BeNil())
					scope.Status.AuthServerID = "XXX"
					Expect(nsClient.Status().Update(ctx, scope)).To(BeNil())

					Eventually(ConditionStatus(actualClient, authcomponentsv1beta1.ConditionTypeClientProgressing)).
						Should(Equal(metav1.ConditionFalse))
					Expect(actualClient.Status.Scopes).To(Equal(map[string]string{
						scope.Name: scope.Status.AuthServerID,
					}))
				})
				AfterEach(func() {
					Expect(nsClient.Delete(ctx, scope)).To(BeNil())
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
						Expect(nsClient.Update(ctx, actualClient)).To(BeNil())
						Eventually(func() map[string]string {
							_ = nsClient.Get(ctx, client.ObjectKeyFromObject(actualClient), actualClient)
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
