package tests_test

import (
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

// Modules derive their auth issuer and public URL from the Gateway ingress,
// so they must be re-rendered when the ingress changes.
var _ = Describe("Gateway URL propagation", func() {
	var (
		stack            *v1beta1.Stack
		gateway          *v1beta1.Gateway
		auth             *v1beta1.Auth
		ledger           *v1beta1.Ledger
		databaseSettings *v1beta1.Settings
	)
	deploymentEnv := func(name, key string) func(g Gomega) string {
		return func(g Gomega) string {
			deployment := &appsv1.Deployment{}
			g.Expect(LoadResource(stack.Name, name, deployment)).To(Succeed())
			for _, container := range deployment.Spec.Template.Spec.Containers {
				for _, env := range container.Env {
					if env.Name == key {
						return env.Value
					}
				}
			}
			return ""
		}
	}
	BeforeEach(func() {
		stack = &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v2.99.0"},
		}
		databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
		gateway = &v1beta1.Gateway{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.GatewaySpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				Ingress: &v1beta1.GatewayIngress{
					Host:   "example.net",
					Scheme: "https",
				},
			},
		}
		auth = &v1beta1.Auth{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.AuthSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
		ledger = &v1beta1.Ledger{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
	})
	JustBeforeEach(func() {
		Expect(Create(stack)).To(Succeed())
		Expect(Create(databaseSettings)).To(Succeed())
		Expect(Create(gateway)).To(Succeed())
		Expect(Create(auth)).To(Succeed())
		Expect(Create(ledger)).To(Succeed())

		Eventually(deploymentEnv("auth", "BASE_URL")).Should(Equal("https://example.net/api/auth"))
		Eventually(deploymentEnv("auth", "STACK_PUBLIC_URL")).Should(Equal("https://example.net"))
		Eventually(deploymentEnv("ledger", "AUTH_ISSUER")).Should(Equal("https://example.net/api/auth"))

		// Wait for reconciles queued during setup to drain, so the assertions below
		// can only pass if the Gateway change itself re-enqueues the modules.
		resourceVersions := func(g Gomega) []string {
			ret := make([]string, 0, 4)
			for _, obj := range []client.Object{auth, ledger} {
				g.Expect(LoadResource("", obj.GetName(), obj)).To(Succeed())
				ret = append(ret, obj.GetResourceVersion())
			}
			for _, name := range []string{"auth", "ledger"} {
				deployment := &appsv1.Deployment{}
				g.Expect(LoadResource(stack.Name, name, deployment)).To(Succeed())
				ret = append(ret, deployment.GetResourceVersion())
			}
			return ret
		}
		Eventually(func(g Gomega) {
			settled := resourceVersions(g)
			g.Consistently(resourceVersions, 2*time.Second, 200*time.Millisecond).Should(Equal(settled))
		}, 30*time.Second).Should(Succeed())
		Expect(LoadResource("", gateway.Name, gateway)).To(Succeed())
	})
	AfterEach(func() {
		Expect(Delete(databaseSettings)).To(Succeed())
		Expect(Delete(stack)).To(Succeed())
	})
	Context("When removing the gateway ingress", func() {
		JustBeforeEach(func() {
			patch := client.MergeFrom(gateway.DeepCopy())
			gateway.Spec.Ingress = nil
			Expect(Patch(gateway, patch)).To(Succeed())
		})
		It("Should switch auth and ledger to the internal gateway URL", func() {
			Eventually(deploymentEnv("auth", "BASE_URL")).Should(Equal("http://gateway:8080/api/auth"))
			Eventually(deploymentEnv("auth", "STACK_PUBLIC_URL")).Should(BeEmpty())
			Eventually(deploymentEnv("ledger", "AUTH_ISSUER")).Should(Equal("http://gateway:8080/api/auth"))
		})
	})
	Context("When changing the gateway ingress host", func() {
		JustBeforeEach(func() {
			patch := client.MergeFrom(gateway.DeepCopy())
			gateway.Spec.Ingress.Host = "example.com"
			Expect(Patch(gateway, patch)).To(Succeed())
		})
		It("Should switch auth and ledger to the new public URL", func() {
			Eventually(deploymentEnv("auth", "BASE_URL")).Should(Equal("https://example.com/api/auth"))
			Eventually(deploymentEnv("auth", "STACK_PUBLIC_URL")).Should(Equal("https://example.com"))
			Eventually(deploymentEnv("ledger", "AUTH_ISSUER")).Should(Equal("https://example.com/api/auth"))
		})
	})
})
