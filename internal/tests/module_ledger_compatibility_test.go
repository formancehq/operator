package tests_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	}
})
