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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("AuthScopesSettings", func() {
	Context("When configuring scope verification via Settings", func() {
		var (
			stack            *v1beta1.Stack
			ledger           *v1beta1.Ledger
			auth             *v1beta1.Auth
			databaseSettings *v1beta1.Settings
		)

		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			auth = &v1beta1.Auth{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.AuthSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
			ledger = &v1beta1.Ledger{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
		})

		AfterEach(func() {
			Expect(Delete(stack, databaseSettings)).To(Succeed())
		})

		Context("with specific module Settings", func() {
			var (
				scopesSettings *v1beta1.Settings
			)
			BeforeEach(func() {
				scopesSettings = settings.New(uuid.NewString(), "auth.ledger.check-scopes", "true", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, scopesSettings, ledger)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(scopesSettings)).To(Succeed())
			})
			It("Should add AUTH_CHECK_SCOPES env vars to deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElements(
					core.Env("AUTH_CHECK_SCOPES", "true"),
					core.Env("AUTH_SERVICE", "ledger"),
				))
			})
		})

		Context("with wildcard Settings", func() {
			var (
				scopesSettings *v1beta1.Settings
			)
			BeforeEach(func() {
				scopesSettings = settings.New(uuid.NewString(), "auth.*.check-scopes", "true", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, scopesSettings, ledger)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(scopesSettings)).To(Succeed())
			})
			It("Should add AUTH_CHECK_SCOPES env vars to deployment", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElements(
					core.Env("AUTH_CHECK_SCOPES", "true"),
					core.Env("AUTH_SERVICE", "ledger"),
				))
			})
		})

		Context("with Settings priority over module spec", func() {
			var (
				scopesSettings *v1beta1.Settings
			)
			BeforeEach(func() {
				// Settings says false
				scopesSettings = settings.New(uuid.NewString(), "auth.ledger.check-scopes", "false", stack.Name)
				// But module spec says true
				ledger.Spec.Auth = &v1beta1.AuthConfig{
					CheckScopes: true,
				}
			})
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, scopesSettings, ledger)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(scopesSettings)).To(Succeed())
			})
			It("Should NOT add AUTH_CHECK_SCOPES env vars (Settings takes priority)", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).ShouldNot(ContainElements(
					core.Env("AUTH_CHECK_SCOPES", "true"),
				))
			})
		})

		Context("with specific Settings overriding wildcard", func() {
			var (
				wildcardSettings *v1beta1.Settings
				specificSettings *v1beta1.Settings
			)
			BeforeEach(func() {
				// Wildcard says true
				wildcardSettings = settings.New(uuid.NewString(), "auth.*.check-scopes", "true", stack.Name)
				// But specific says false
				specificSettings = settings.New(uuid.NewString(), "auth.ledger.check-scopes", "false", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, wildcardSettings, specificSettings, ledger)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(wildcardSettings, specificSettings)).To(Succeed())
			})
			It("Should NOT add AUTH_CHECK_SCOPES env vars (specific Settings overrides wildcard)", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).ShouldNot(ContainElements(
					core.Env("AUTH_CHECK_SCOPES", "true"),
				))
			})
		})

		Context("with backward compatibility (module spec only)", func() {
			BeforeEach(func() {
				// No Settings, only module spec
				ledger.Spec.Auth = &v1beta1.AuthConfig{
					CheckScopes: true,
				}
			})
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, ledger)).To(Succeed())
			})
			It("Should add AUTH_CHECK_SCOPES env vars (backward compatible)", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElements(
					core.Env("AUTH_CHECK_SCOPES", "true"),
					core.Env("AUTH_SERVICE", "ledger"),
				))
			})
		})

		Context("with default behavior (no Settings, no module spec)", func() {
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, ledger)).To(Succeed())
			})
			It("Should NOT add AUTH_CHECK_SCOPES env vars (default behavior)", func() {
				deployment := &appsv1.Deployment{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
					return deployment.Spec.Template.Spec.Containers[0].Env
				}).ShouldNot(ContainElements(
					core.Env("AUTH_CHECK_SCOPES", "true"),
				))
			})
		})

		Context("when Settings is set to true then updated to false", func() {
			var (
				scopesSettings *v1beta1.Settings
			)
			BeforeEach(func() {
				scopesSettings = settings.New(uuid.NewString(), "auth.ledger.check-scopes", "true", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(stack, databaseSettings, auth, scopesSettings, ledger)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(scopesSettings)).To(Succeed())
			})
			It("Should update deployment when Settings changes", func() {
				deployment := &appsv1.Deployment{}
				By("Initially should have AUTH_CHECK_SCOPES", func() {
					Eventually(func(g Gomega) []corev1.EnvVar {
						g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
						return deployment.Spec.Template.Spec.Containers[0].Env
					}).Should(ContainElements(
						core.Env("AUTH_CHECK_SCOPES", "true"),
						core.Env("AUTH_SERVICE", "ledger"),
					))
				})
				By("After updating Settings to false, should remove AUTH_CHECK_SCOPES", func() {
					// Update Settings value to false
					patch := client.MergeFrom(scopesSettings.DeepCopy())
					scopesSettings.Spec.Value = "false"
					Expect(Patch(scopesSettings, patch)).To(Succeed())

					Eventually(func(g Gomega) []corev1.EnvVar {
						g.Expect(Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)).To(Succeed())
						return deployment.Spec.Template.Spec.Containers[0].Env
					}).ShouldNot(ContainElements(
						core.Env("AUTH_CHECK_SCOPES", "true"),
					))
				})
			})
		})
	})
})
