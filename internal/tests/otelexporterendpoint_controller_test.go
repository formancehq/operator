package tests_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1beta1 "github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("OtelExporterEndpointController", func() {
	Context("When creating an OtelExporterEndpoint with stackSelector matching a stack", func() {
		var (
			stack    *v1beta1.Stack
			endpoint *v1beta1.OtelExporterEndpoint
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: RandObjectMeta().Name,
					Labels: map[string]string{
						"formance.com/stack": "sdymzzszghxw-ryeg",
					},
				},
				Spec: v1beta1.StackSpec{Version: "v99.0.0"},
			}
			endpoint = &v1beta1.OtelExporterEndpoint{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.OtelExporterEndpointSpec{
					StackSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"formance.com/stack": "sdymzzszghxw-ryeg",
						},
					},
					Traces: &v1beta1.OtelSignalConfig{
						Endpoint: "http://my-collector:4318",
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(endpoint)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(endpoint)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should create a ConfigMap, Deployment, and Service", func() {
			By("Should set the status to ready", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", endpoint.Name, endpoint)).To(Succeed())
					return endpoint.Status.Ready
				}).Should(BeTrue())
			})
			By("Should track the matching stack", func() {
				Expect(endpoint.Status.Stacks).To(ContainElement(stack.Name))
			})
			By("Should create a ConfigMap with collector config", func() {
				cm := &corev1.ConfigMap{}
				Eventually(func() error {
					return LoadResource(stack.Name, "otel-collector-config", cm)
				}).Should(Succeed())
				Expect(cm.Data).To(HaveKey("otel-collector-config.yaml"))
				Expect(cm.Data["otel-collector-config.yaml"]).To(ContainSubstring("http://my-collector:4318"))
			})
			By("Should create a Deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func() error {
					return LoadResource(stack.Name, "otel-collector", deployment)
				}).Should(Succeed())
			})
			By("Should create a Service", func() {
				svc := &corev1.Service{}
				Eventually(func() error {
					return LoadResource(stack.Name, "otel-collector", svc)
				}).Should(Succeed())
			})
		})
	})

	Context("When no OtelExporterEndpoints exist and no Settings", func() {
		var (
			stack *v1beta1.Stack
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: RandObjectMeta().Name,
					Labels: map[string]string{
						"formance.com/stack": "sdymzzszghxw-ryeg",
					},
				},
				Spec: v1beta1.StackSpec{Version: "v99.0.0"},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should not create a collector service", func() {
			svc := &corev1.Service{}
			Consistently(func() error {
				return LoadResource(stack.Name, "otel-collector", svc)
			}).ShouldNot(Succeed())
		})
	})

	Context("When creating an OtelExporterEndpoint targeting all stacks", func() {
		var (
			stack      *v1beta1.Stack
			endpoint   *v1beta1.OtelExporterEndpoint
			authSecret *corev1.Secret
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: RandObjectMeta().Name,
					Labels: map[string]string{
						"formance.com/stack": "sdymzzszghxw-ryeg",
					},
				},
				Spec: v1beta1.StackSpec{Version: "v99.0.0"},
			}
			endpoint = &v1beta1.OtelExporterEndpoint{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.OtelExporterEndpointSpec{
					StackSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "formance.com/stack",
								Operator: metav1.LabelSelectorOpExists,
							},
						},
					},
					Traces: &v1beta1.OtelSignalConfig{
						Endpoint: "https://support.frmnc.net",
						Auth: &v1beta1.OtelAuthConfig{
							Type:       "bearer",
							FromSecret: "formance-license",
						},
					},
					ResourceAttributes: map[string]string{
						"cluster.id": "test-cluster",
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Eventually(func() error {
				ns := &corev1.Namespace{}
				return LoadResource("", stack.Name, ns)
			}).Should(Succeed())
			authSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "formance-license",
					Namespace: stack.Name,
				},
				Data: map[string][]byte{
					"token": []byte("test-token"),
				},
			}
			Expect(Create(authSecret)).To(Succeed())
			Expect(Create(endpoint)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(endpoint)).To(Succeed())
			Expect(Delete(authSecret)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should create a collector with auth headers", func() {
			Eventually(func(g Gomega) bool {
				g.Expect(LoadResource("", endpoint.Name, endpoint)).To(Succeed())
				return endpoint.Status.Ready
			}).Should(BeTrue())

			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return LoadResource(stack.Name, "otel-collector-config", cm)
			}).Should(Succeed())
			Expect(cm.Data["otel-collector-config.yaml"]).To(ContainSubstring("https://support.frmnc.net"))
			Expect(cm.Data["otel-collector-config.yaml"]).To(ContainSubstring("authorization"))
		})
	})

	Context("When creating an OtelExporterEndpoint with Settings fallback", func() {
		var (
			stack    *v1beta1.Stack
			setting  *v1beta1.Settings
			endpoint *v1beta1.OtelExporterEndpoint
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name: RandObjectMeta().Name,
					Labels: map[string]string{
						"formance.com/stack": "sdymzzszghxw-ryeg",
					},
				},
				Spec: v1beta1.StackSpec{Version: "v99.0.0"},
			}
			setting = &v1beta1.Settings{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.SettingsSpec{
					Stacks: []string{"*"},
					Key:    "opentelemetry.traces.dsn",
					Value:  "http://settings-collector:4318",
				},
			}
			endpoint = &v1beta1.OtelExporterEndpoint{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.OtelExporterEndpointSpec{
					StackSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"formance.com/stack": "sdymzzszghxw-ryeg",
						},
					},
					Traces: &v1beta1.OtelSignalConfig{
						Endpoint: "http://my-collector:4318",
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(setting)).To(Succeed())
			Expect(Create(endpoint)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(setting)).To(Succeed())
			Expect(Delete(endpoint)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})
		It("Should include both CRD and Settings endpoints in collector config", func() {
			Eventually(func(g Gomega) bool {
				g.Expect(LoadResource("", endpoint.Name, endpoint)).To(Succeed())
				return endpoint.Status.Ready
			}).Should(BeTrue())

			cm := &corev1.ConfigMap{}
			Eventually(func() error {
				return LoadResource(stack.Name, "otel-collector-config", cm)
			}).Should(Succeed())
			Expect(cm.Data["otel-collector-config.yaml"]).To(ContainSubstring("http://my-collector:4318"))
			Expect(cm.Data["otel-collector-config.yaml"]).To(ContainSubstring("settings-collector:4318"))
		})
	})
})
