package tests_test

import (
	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/core"
	"github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("BenthosController", func() {

	Context("When creating a Benthos", func() {
		var (
			benthos          *v1beta1.Benthos
			broker           *v1beta1.Broker
			stack            *v1beta1.Stack
			brokerDSNSetting *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			broker = &v1beta1.Broker{
				ObjectMeta: v1.ObjectMeta{
					Name: stack.Name,
				},
				Spec: v1beta1.BrokerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			benthos = &v1beta1.Benthos{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.BenthosSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			brokerDSNSetting = settings.New(uuid.NewString(), "broker.dsn", "noop://do-nothing", stack.Name)
			Expect(Create(brokerDSNSetting)).To(BeNil())
			Expect(Create(broker)).To(Succeed())
			Expect(Create(benthos)).To(Succeed())
		})
		JustAfterEach(func() {
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should create appropriate resources", func() {
			By("Should create a deployment", func() {
				t := &appsv1.Deployment{}
				Eventually(func() error {
					return Get(core.GetNamespacedResourceName(stack.Name, "benthos"), t)
				}).Should(BeNil())
			})
			By("Should create a ConfigMap for templates configuration", func() {
				t := &corev1.ConfigMap{}
				Eventually(func() error {
					return Get(core.GetNamespacedResourceName(stack.Name, "benthos-templates"), t)
				}).Should(BeNil())
			})
			By("Should create a ConfigMap for resources configuration", func() {
				t := &corev1.ConfigMap{}
				Eventually(func() error {
					return Get(core.GetNamespacedResourceName(stack.Name, "benthos-resources"), t)
				}).Should(BeNil())
			})
		})
	})
})
