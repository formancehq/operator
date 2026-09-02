package tests_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("Ledger v3 module compatibility", func() {
	testCases := []struct {
		name      string
		newModule func(stack string) v1beta1.Module
	}{
		{
			name: "MCP",
			newModule: func(stack string) v1beta1.Module {
				return &v1beta1.MCP{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.MCPSpec{
						StackDependency: v1beta1.StackDependency{Stack: stack},
					},
				}
			},
		},
		{
			name: "Orchestration",
			newModule: func(stack string) v1beta1.Module {
				return &v1beta1.Orchestration{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.OrchestrationSpec{
						StackDependency: v1beta1.StackDependency{Stack: stack},
					},
				}
			},
		},
		{
			name: "Reconciliation",
			newModule: func(stack string) v1beta1.Module {
				return &v1beta1.Reconciliation{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.ReconciliationSpec{
						StackDependency: v1beta1.StackDependency{Stack: stack},
					},
				}
			},
		},
		{
			name: "TransactionPlane",
			newModule: func(stack string) v1beta1.Module {
				return &v1beta1.TransactionPlane{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.TransactionPlaneSpec{
						StackDependency: v1beta1.StackDependency{Stack: stack},
					},
				}
			},
		},
		{
			name: "Wallets",
			newModule: func(stack string) v1beta1.Module {
				return &v1beta1.Wallets{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.WalletsSpec{
						StackDependency: v1beta1.StackDependency{Stack: stack},
					},
				}
			},
		},
		{
			name: "Webhooks",
			newModule: func(stack string) v1beta1.Module {
				return &v1beta1.Webhooks{
					ObjectMeta: RandObjectMeta(),
					Spec: v1beta1.WebhooksSpec{
						StackDependency: v1beta1.StackDependency{Stack: stack},
					},
				}
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		It("refuses to install "+testCase.name+" with Ledger v3", func() {
			stack := &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v3.0.0"},
			}
			ledger := &v1beta1.Ledger{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}
			module := testCase.newModule(stack.Name)

			Expect(Create(stack, ledger, module)).To(Succeed())
			DeferCleanup(func() {
				Expect(Delete(module, ledger, stack)).To(Succeed())
			})

			Eventually(func(g Gomega) *v1beta1.Condition {
				g.Expect(LoadResource("", module.GetName(), module)).To(Succeed())
				condition := module.GetConditions().Get(core.DependenciesSatisfiedCondition)
				g.Expect(condition).NotTo(BeNil())
				return condition
			}).Should(SatisfyAll(
				WithTransform(func(condition *v1beta1.Condition) metav1.ConditionStatus {
					return condition.Status
				}, Equal(metav1.ConditionFalse)),
				WithTransform(func(condition *v1beta1.Condition) string {
					return condition.Reason
				}, Equal("DependencyVersionMismatch")),
				WithTransform(func(condition *v1beta1.Condition) string {
					return condition.Message
				}, ContainSubstring("must be before v3.0.0-0")),
			))
			Expect(module.IsReady()).To(BeFalse())
		})

		It("removes active "+testCase.name+" runtime resources when Ledger v3 is already materialized", func() {
			stack := &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v3.0.0"},
			}
			ledger := &v1beta1.Ledger{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
				},
			}
			module := testCase.newModule(stack.Name)
			cluster := primaryLedgerV3Cluster(stack.Name)

			Expect(Create(stack)).To(Succeed())
			Eventually(func() error {
				return Get(types.NamespacedName{Name: stack.Name}, &corev1.Namespace{})
			}).Should(Succeed())
			Expect(Create(ledger, module, cluster)).To(Succeed())
			Expect(LoadResource("", module.GetName(), module)).To(Succeed())

			labels := map[string]string{"app": module.GetName()}
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: module.GetName(), Namespace: stack.Name},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{Containers: []corev1.Container{{
							Name: "module", Image: "example.invalid/module:test",
						}}},
					},
				},
			}
			httpAPI := &v1beta1.GatewayHTTPAPI{
				ObjectMeta: metav1.ObjectMeta{Name: module.GetName()},
				Spec: v1beta1.GatewayHTTPAPISpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
					Name:            module.GetName(),
					Rules:           []v1beta1.GatewayHTTPAPIRule{},
				},
			}
			Expect(controllerutil.SetControllerReference(module, deployment, GetScheme())).To(Succeed())
			Expect(controllerutil.SetControllerReference(module, httpAPI, GetScheme())).To(Succeed())
			database := &v1beta1.Database{
				ObjectMeta: metav1.ObjectMeta{Name: module.GetName()},
				Spec: v1beta1.DatabaseSpec{
					StackDependency: v1beta1.StackDependency{Stack: stack.Name},
					Service:         module.GetName(),
				},
			}
			Expect(controllerutil.SetControllerReference(module, database, GetScheme())).To(Succeed())
			Expect(Create(deployment, httpAPI, database)).To(Succeed())

			DeferCleanup(func() {
				for _, object := range []client.Object{deployment, httpAPI, database, module, ledger, cluster, stack} {
					Expect(client.IgnoreNotFound(Delete(object))).To(Succeed())
				}
			})

			Eventually(func() bool {
				err := Get(types.NamespacedName{Namespace: stack.Name, Name: deployment.Name}, &appsv1.Deployment{})
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())
			Eventually(func() bool {
				err := Get(types.NamespacedName{Name: httpAPI.Name}, &v1beta1.GatewayHTTPAPI{})
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())
			Consistently(func() error {
				return Get(types.NamespacedName{Name: database.Name}, &v1beta1.Database{})
			}).Should(Succeed())
			Expect(LoadResource("", module.GetName(), module)).To(Succeed())
		})
	}

	It("blocks a Ledger v2 to v3 transition without stopping legacy module runtimes", func() {
		stack := &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v2.3.0"},
		}
		ledger := &v1beta1.Ledger{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
		modules := make([]v1beta1.Module, 0, len(testCases))
		objects := []client.Object{ledger}
		for _, testCase := range testCases {
			module := testCase.newModule(stack.Name)
			modules = append(modules, module)
			objects = append(objects, module)
		}
		cluster := primaryLedgerV3Cluster(stack.Name)

		Expect(Create(stack)).To(Succeed())
		Eventually(func() error {
			return Get(types.NamespacedName{Name: stack.Name}, &corev1.Namespace{})
		}).Should(Succeed())
		Expect(Create(objects...)).To(Succeed())
		Expect(LoadResource("", modules[0].GetName(), modules[0])).To(Succeed())

		labels := map[string]string{"app": "legacy-module"}
		legacyDeployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "legacy-module", Namespace: stack.Name},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{Containers: []corev1.Container{{
						Name: "module", Image: "example.invalid/module:test",
					}}},
				},
			},
		}
		Expect(controllerutil.SetControllerReference(modules[0], legacyDeployment, GetScheme())).To(Succeed())
		Expect(Create(legacyDeployment)).To(Succeed())

		Expect(LoadResource("", stack.Name, stack)).To(Succeed())
		patch := client.MergeFrom(stack.DeepCopy())
		stack.Spec.Version = "v3.0.0"
		Expect(Patch(stack, patch)).To(Succeed())

		DeferCleanup(func() {
			for _, object := range append(objects, legacyDeployment, cluster, stack) {
				Expect(client.IgnoreNotFound(Delete(object))).To(Succeed())
			}
		})

		Consistently(func() bool {
			err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
		Eventually(func(g Gomega) string {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			condition := ledger.GetConditions().Get("LedgerV3ClusterReady")
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Reason).To(Equal("IncompatibleModules"))
			return condition.Message
		}).Should(ContainSubstring("disable incompatible modules"))
		Consistently(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: legacyDeployment.Name}, &appsv1.Deployment{})
		}).Should(Succeed())

		for _, module := range modules {
			Expect(Delete(module)).To(Succeed())
		}
		Eventually(func(g Gomega) string {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			condition := ledger.GetConditions().Get("LedgerV3ClusterReady")
			g.Expect(condition).NotTo(BeNil())
			return condition.Reason
		}).ShouldNot(Equal("IncompatibleModules"))
	})
})

func primaryLedgerV3Cluster(stack string) *unstructured.Unstructured {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "ledger.formance.com", Version: "v1alpha1", Kind: "Cluster",
	})
	cluster.SetNamespace(stack)
	cluster.SetName(stack)
	return cluster
}
