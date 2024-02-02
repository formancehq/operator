package tests_test

import (
	"fmt"

	"github.com/formancehq/operator/api/formance.com/v1beta1"
	"github.com/formancehq/operator/internal/resources/secretreferences"
	"github.com/formancehq/operator/internal/resources/settings"
	. "github.com/formancehq/operator/internal/tests/internal"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("DatabaseController", func() {
	Context("When creating a Database", func() {
		var (
			stack            *v1beta1.Stack
			database         *v1beta1.Database
			databaseSettings *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			database = &v1beta1.Database{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.DatabaseSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
					Service: "ledger",
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(BeNil())
			Expect(Create(databaseSettings)).Should(Succeed())
			Expect(Create(database)).To(Succeed())
		})
		JustAfterEach(func() {
			Expect(client.IgnoreNotFound(Delete(database))).To(Succeed())
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
		})
		shouldBeReady := func() {
			d := &v1beta1.Database{}
			Eventually(func(g Gomega) bool {
				g.Expect(LoadResource("", database.Name, d)).To(Succeed())
				return d.Status.Ready
			}).Should(BeTrue())
		}
		shouldHaveOwnerReferenceOnStack := func() {
			d := &v1beta1.Database{}
			Eventually(func(g Gomega) *v1beta1.Database {
				g.Expect(LoadResource("", database.Name, d)).To(Succeed())
				return d
			}).Should(BeOwnedBy(stack))
		}
		It("Should be set to ready status", shouldBeReady)
		It("Should add the stack in owner references", shouldHaveOwnerReferenceOnStack)
		Context("Then when deleting the Database object", func() {
			JustBeforeEach(func() {
				shouldBeReady()
				Expect(Delete(database)).To(Succeed())
			})
			It("Should create a deletion job", func() {
				Eventually(func() error {
					return LoadResource(stack.Name, fmt.Sprintf("%s-drop-database", database.Spec.Service), &batchv1.Job{})
				}).Should(Succeed())
			})
			It("Should eventually be deleted", func() {
				Eventually(func() error {
					return LoadResource(stack.Name, database.Name, &v1beta1.Database{})
				}).Should(BeNotFound())
			})
		})
		Context("Then when updating the DatabaseConfiguration object", func() {
			JustBeforeEach(func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", database.Name, database)).To(Succeed())
					return database.Status.Ready
				}).Should(BeTrue())

				patch := client.MergeFrom(databaseSettings.DeepCopy())
				databaseSettings.Spec.Value = "postgresql://xxx"
				Expect(Patch(databaseSettings, patch)).To(Succeed())
			})
			It("Should declare the Database object as out of sync", func() {
				Eventually(func(g Gomega) bool {
					g.Expect(LoadResource("", database.Name, database)).To(Succeed())

					return database.Status.OutOfSync
				}).Should(BeTrue())
			})
		})
		Context("using a secret for credentials", func() {
			var (
				secret1 *v1.Secret
			)
			BeforeEach(func() {
				secret1 = &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      uuid.NewString(),
						Namespace: "default",
						Labels: map[string]string{
							v1beta1.StackLabel: stack.Name,
						},
						Annotations: map[string]string{
							secretreferences.RewrittenSecretName: "postgres",
						},
					},
					Data: map[string][]byte{
						"username": []byte("formance"),
						"password": []byte("formance"),
					},
				}
				Expect(Create(secret1)).To(Succeed())
				databaseSettings.Spec.Value = "postgresql://xxx?secret=postgres"
			})
			shouldCreateSecretReference := func() {
				secretReference := &v1beta1.SecretReference{}
				Eventually(func(g Gomega) error {
					return LoadResource("", fmt.Sprintf("%s-postgres", database.Name), secretReference)
				}).Should(Succeed())
			}
			It("Should create a secret reference", shouldCreateSecretReference)
			Context("Then changing the secret", func() {
				var secret2 *v1.Secret
				JustBeforeEach(func() {
					shouldCreateSecretReference()
					secret2 = &v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      uuid.NewString(),
							Namespace: "default",
							Labels: map[string]string{
								v1beta1.StackLabel: stack.Name,
							},
							Annotations: map[string]string{
								secretreferences.RewrittenSecretName: "postgres2",
							},
						},
						Data: map[string][]byte{
							"username": []byte("formance"),
							"password": []byte("formance"),
						},
					}
					Expect(Create(secret2)).To(Succeed())
					patch := client.MergeFrom(databaseSettings.DeepCopy())
					databaseSettings.Spec.Value = "postgresql://xxx?secret=postgres2"
					Expect(Patch(databaseSettings, patch)).To(Succeed())
				})
				It("Should create the new secret and remove the old one", func() {
					shouldCreateSecretReference()
					Eventually(func() error {
						return LoadResource(stack.Name, fmt.Sprintf("%s-postgres", database.Name), secret1)
					}).Should(BeNotFound())
				})
			})
		})
	})
})