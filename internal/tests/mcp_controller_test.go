package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("MCPController", func() {
	Context("When creating an MCP module", func() {
		var (
			stack           *v1beta1.Stack
			gateway         *v1beta1.Gateway
			ledger          *v1beta1.Ledger
			mcp             *v1beta1.MCP
			otelTracesDSN   *v1beta1.Settings
			otelMetricsDSN  *v1beta1.Settings
			serviceAccounts *v1beta1.Settings
		)

		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			gateway = &v1beta1.Gateway{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.GatewaySpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
					Ingress: &v1beta1.GatewayIngress{
						Host:   "example.net",
						Scheme: "https",
					},
				},
			}
			ledger = &v1beta1.Ledger{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.LedgerSpec{
					StackDependency:  v1beta1.StackDependency{Stack: stack.Name},
					ModuleProperties: v1beta1.ModuleProperties{Version: "v2.99.0"},
				},
			}
			mcp = &v1beta1.MCP{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.MCPSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			otelTracesDSN = settings.New(uuid.NewString(), "opentelemetry.traces.dsn", "grpc://collector", stack.Name)
			otelMetricsDSN = settings.New(uuid.NewString(), "opentelemetry.metrics.dsn", "grpc://collector", stack.Name)
			serviceAccounts = settings.New(uuid.NewString(), "aws.service-account", "default", stack.Name)
		})

		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(gateway)).To(Succeed())
			Expect(Create(otelTracesDSN)).To(Succeed())
			Expect(Create(otelMetricsDSN)).To(Succeed())
			Expect(Create(serviceAccounts)).To(Succeed())
			Expect(Create(ledger)).To(Succeed())
			Expect(Create(mcp)).To(Succeed())
		})

		AfterEach(func() {
			Expect(Delete(mcp)).To(Succeed())
			Expect(Delete(ledger)).To(Succeed())
			Expect(Delete(serviceAccounts)).To(Succeed())
			Expect(Delete(otelMetricsDSN)).To(Succeed())
			Expect(Delete(otelTracesDSN)).To(Succeed())
			Expect(Delete(gateway)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})

		It("Should create the deployment with MCP configuration", func() {
			deployment := &appsv1.Deployment{}
			Eventually(func(g Gomega) corev1.Container {
				g.Expect(LoadResource(stack.Name, "mcp", deployment)).To(Succeed())
				g.Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
				return deployment.Spec.Template.Spec.Containers[0]
			}).Should(SatisfyAll(
				WithTransform(func(c corev1.Container) []string { return c.Args }, Equal([]string{"serve"})),
				WithTransform(func(c corev1.Container) string { return c.Image }, ContainSubstring("ghcr.io/formancehq/stack-mcp:v99.0.0")),
				WithTransform(func(c corev1.Container) []corev1.EnvVar { return c.Env }, ContainElements(
					core.Env("BIND", ":8080"),
					core.Env("STACK_URL", "http://gateway:8080"),
					core.Env("STACK_PUBLIC_URL", "https://example.net"),
					core.Env("AUTH_ENABLED", "true"),
					core.Env("AUTH_ISSUER", "https://example.net/api/auth"),
					core.Env("AUTH_CHECK_SCOPES", "false"),
					core.Env("OTEL_SERVICE_NAME", "mcp"),
				)),
				WithTransform(func(c corev1.Container) corev1.ResourceList { return c.Resources.Requests }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				})),
				WithTransform(func(c corev1.Container) corev1.ResourceList { return c.Resources.Limits }, Equal(corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				})),
			))

			Expect(deployment).To(BeControlledBy(mcp))
		})

		It("Should create a GatewayHTTPAPI exposing MCP routes", func() {
			httpAPI := &v1beta1.GatewayHTTPAPI{}
			Eventually(func(g Gomega) []v1beta1.GatewayHTTPAPIRule {
				g.Expect(LoadResource("", core.GetObjectName(stack.Name, "mcp"), httpAPI)).To(Succeed())
				g.Expect(httpAPI.Spec.Name).To(Equal("mcp"))
				return httpAPI.Spec.Rules
			}).Should(ConsistOf(
				v1beta1.GatewayHTTPAPIRule{
					Methods: []string{"POST"},
				},
				v1beta1.GatewayHTTPAPIRule{
					Path:    "/.well-known/oauth-protected-resource",
					Methods: []string{"GET"},
				},
				v1beta1.GatewayHTTPAPIRule{
					Path:    "/_healthcheck",
					Methods: []string{"GET"},
				},
			))
		})

		It("Should render Gateway routes under the API prefix", func() {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega) string {
				g.Expect(LoadResource(stack.Name, "gateway", cm)).To(Succeed())
				return cm.Data["Caddyfile"]
			}).Should(SatisfyAll(
				ContainSubstring("handle /api/mcp*"),
				ContainSubstring("handle /api/mcp/.well-known/oauth-protected-resource*"),
				ContainSubstring("handle /api/mcp/_healthcheck*"),
				ContainSubstring("reverse_proxy mcp:8080"),
			))
		})
	})
})
