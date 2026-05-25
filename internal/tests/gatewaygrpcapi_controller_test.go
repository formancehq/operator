package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("GatewayGRPCAPI", func() {
	Context("When creating a GatewayGRPCAPI", func() {
		var (
			stack   *v1beta1.Stack
			grpcAPI *v1beta1.GatewayGRPCAPI
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			grpcAPI = &v1beta1.GatewayGRPCAPI{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.GatewayGRPCAPISpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
					Name:         "mymodule",
					GRPCServices: []string{"formance.mymodule.v1.MyService"},
					Port:         8081,
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(BeNil())
			Expect(Create(grpcAPI)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(grpcAPI)).To(Succeed())
			Expect(Delete(stack)).To(BeNil())
		})
		It("Should create a k8s service with -grpc suffix", func() {
			service := &corev1.Service{}
			Eventually(func() error {
				return LoadResource(stack.Name, "mymodule-grpc", service)
			}).Should(BeNil())
			Expect(service).To(BeControlledBy(grpcAPI))
			Expect(service.Spec.Selector).To(Equal(map[string]string{
				"app.kubernetes.io/name": grpcAPI.Spec.Name,
			}))
			Expect(service.Spec.Ports).To(HaveLen(1))
			Expect(service.Spec.Ports[0].Name).To(Equal("grpc"))
			Expect(service.Spec.Ports[0].Port).To(Equal(int32(8081)))
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
					g.Expect(LoadResource(stack.Name, "mymodule-grpc", service)).To(Succeed())
					return service.Annotations
				}).Should(HaveKeyWithValue("foo", "bar"))
			})
		})
	})
})
