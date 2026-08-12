package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/gatewayhttpapis"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("GatewayHTTPAPI", func() {
	It("rejects a backendRef that collides with the managed Service name", func() {
		httpAPI := &v1beta1.GatewayHTTPAPI{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.GatewayHTTPAPISpec{
				StackDependency: v1beta1.StackDependency{Stack: "stack0"},
				Name:            "ledger",
				Rules: []v1beta1.GatewayHTTPAPIRule{{
					BackendRef: &v1beta1.GatewayBackendRef{Name: "ledger", Port: 8081},
				}},
			},
		}
		Expect(apierrors.IsInvalid(Create(httpAPI))).To(BeTrue())
	})

	Context("When creating an GatewayHTTPAPI", func() {
		var (
			stack   *v1beta1.Stack
			httpAPI *v1beta1.GatewayHTTPAPI
		)
		BeforeEach(func() {

			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			httpAPI = &v1beta1.GatewayHTTPAPI{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.GatewayHTTPAPISpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
					Name:  "ledger",
					Rules: []v1beta1.GatewayHTTPAPIRule{gatewayhttpapis.RuleSecured()},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(BeNil())
			Expect(Create(httpAPI)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(httpAPI)).To(Succeed())
			Expect(Delete(stack)).To(BeNil())
		})
		It("Should create a k8s service", func() {
			service := &corev1.Service{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", service)
			}).Should(BeNil())
			Expect(service).To(BeControlledBy(httpAPI))
			Expect(service.Spec.Selector).To(Equal(map[string]string{
				"app.kubernetes.io/name": httpAPI.Spec.Name,
			}))
		})
		It("Should delete its default service when every rule switches to a backendRef", func() {
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", &corev1.Service{})
			}).Should(BeNil())

			Eventually(func(g Gomega) error {
				g.Expect(LoadResource("", httpAPI.Name, httpAPI)).To(Succeed())
				httpAPI.Spec.Rules = []v1beta1.GatewayHTTPAPIRule{{
					BackendRef: &v1beta1.GatewayBackendRef{Name: "ledger-cluster", Port: 8081},
				}}
				return Update(httpAPI)
			}).Should(Succeed())

			Eventually(func() bool {
				return apierrors.IsNotFound(LoadResource(stack.Name, "ledger", &corev1.Service{}))
			}).Should(BeTrue())
		})
		Context("With every rule carrying a backendRef", func() {
			BeforeEach(func() {
				httpAPI.Spec.Rules = []v1beta1.GatewayHTTPAPIRule{{
					BackendRef: &v1beta1.GatewayBackendRef{Name: "ledger-cluster", Port: 8081},
				}}
			})
			It("Should not create the default service", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", httpAPI.Name, httpAPI)).To(Succeed())
					return httpAPI.Status.Ready
				}).Should(BeTrue())
				Expect(apierrors.IsNotFound(LoadResource(stack.Name, "ledger", &corev1.Service{}))).To(BeTrue())
			})
		})
		Context("With user defined annotations", func() {
			var (
				annotationsSettings *v1beta1.Settings
			)
			JustBeforeEach(func() {
				annotationsSettings = settings.New(uuid.NewString(), "services.*.annotations", "foo=bar", stack.Name)
				Expect(Create(annotationsSettings)).To(Succeed())
			})
			JustAfterEach(func() {
				Expect(Delete(annotationsSettings)).To(Succeed())
			})
			It("should add annotations to the service", func() {
				Eventually(func(g Gomega) map[string]string {
					service := &corev1.Service{}
					g.Expect(LoadResource(stack.Name, "ledger", service)).To(Succeed())
					return service.Annotations
				}).Should(HaveKeyWithValue("foo", "bar"))
			})
		})
	})
	Context("When every rule carries a backendRef and a same-named service owned by another controller pre-exists", func() {
		var (
			stack          *v1beta1.Stack
			httpAPI        *v1beta1.GatewayHTTPAPI
			foreignService *corev1.Service
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			httpAPI = &v1beta1.GatewayHTTPAPI{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.GatewayHTTPAPISpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
					Name:            "connectivity",
					Rules: []v1beta1.GatewayHTTPAPIRule{{
						BackendRef: &v1beta1.GatewayBackendRef{Name: "connectivity-api", Port: 8080},
					}},
				},
			}
			foreignService = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "connectivity", Namespace: stack.Name},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Name: "grpc", Port: 15200}},
				},
			}
			Expect(Create(stack)).To(Succeed())
			Eventually(func() error {
				return Create(foreignService)
			}).Should(Succeed())
			Expect(Create(httpAPI)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(httpAPI)).To(Succeed())
			Expect(Delete(stack)).To(BeNil())
		})
		It("Should leave the foreign service untouched and still become ready", func() {
			Eventually(func(g Gomega) bool {
				g.Expect(LoadResource("", httpAPI.Name, httpAPI)).To(Succeed())
				return httpAPI.Status.Ready
			}).Should(BeTrue())
			service := &corev1.Service{}
			Expect(LoadResource(stack.Name, "connectivity", service)).To(Succeed())
			Expect(service).NotTo(BeControlledBy(httpAPI))
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).To(Equal("grpc"))
		})
	})
})
