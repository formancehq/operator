package tests_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var _ = Describe("LedgerV3Controller", func() {
	Context("When creating a Ledger with v3-mirror setting", func() {
		var (
			stack            *v1beta1.Stack
			ledger           *v1beta1.Ledger
			databaseSettings *v1beta1.Settings
			v3MirrorSetting  *v1beta1.Settings
		)
		BeforeEach(func() {
			stack = &v1beta1.Stack{
				ObjectMeta: RandObjectMeta(),
				Spec:       v1beta1.StackSpec{Version: "v99.0.0"},
			}
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			v3MirrorSetting = settings.New(uuid.NewString(), "modules.ledger.v3-mirror", "v3.0.0:default,payments", stack.Name)
			ledger = &v1beta1.Ledger{
				ObjectMeta: RandObjectMeta(),
				Spec: v1beta1.LedgerSpec{
					StackDependency: v1beta1.StackDependency{
						Stack: stack.Name,
					},
				},
			}
		})
		JustBeforeEach(func() {
			Expect(Create(stack)).To(Succeed())
			Expect(Create(databaseSettings)).To(Succeed())
			Expect(Create(v3MirrorSetting)).To(Succeed())
			Expect(Create(ledger)).To(Succeed())
		})
		AfterEach(func() {
			Expect(Delete(ledger)).To(Succeed())
			Expect(Delete(v3MirrorSetting)).To(Succeed())
			Expect(Delete(databaseSettings)).To(Succeed())
			Expect(Delete(stack)).To(Succeed())
		})

		It("Should create a StatefulSet", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(sts).To(BeControlledBy(ledger))
		})

		It("Should create a StatefulSet with 3 replicas by default", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(*sts.Spec.Replicas).To(Equal(int32(3)))
		})

		It("Should create a StatefulSet with OrderedReady pod management", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(sts.Spec.PodManagementPolicy).To(Equal(appsv1.OrderedReadyPodManagement))
		})

		It("Should create a StatefulSet using the headless service", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(sts.Spec.ServiceName).To(Equal("ledger-raft"))
		})

		It("Should create 3 volume claim templates (wal, data, cold-cache)", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(sts.Spec.VolumeClaimTemplates).To(HaveLen(3))
			Expect(sts.Spec.VolumeClaimTemplates[0].Name).To(Equal("wal"))
			Expect(sts.Spec.VolumeClaimTemplates[1].Name).To(Equal("data"))
			Expect(sts.Spec.VolumeClaimTemplates[2].Name).To(Equal("cold-cache"))
		})

		It("Should configure the container with 3 ports (http, grpc, raft)", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			container := sts.Spec.Template.Spec.Containers[0]
			Expect(container.Ports).To(HaveLen(3))
			Expect(container.Ports).To(ContainElements(
				HaveField("Name", "http"),
				HaveField("Name", "grpc"),
				HaveField("Name", "raft"),
			))
		})

		It("Should configure 3 volume mounts", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			container := sts.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(ConsistOf(
				corev1.VolumeMount{Name: "wal", MountPath: "/data/raft"},
				corev1.VolumeMount{Name: "data", MountPath: "/data/app"},
				corev1.VolumeMount{Name: "cold-cache", MountPath: "/data/cold-cache"},
			))
		})

		It("Should configure liveness, readiness, and startup probes", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			container := sts.Spec.Template.Spec.Containers[0]
			Expect(container.LivenessProbe).NotTo(BeNil())
			Expect(container.LivenessProbe.HTTPGet.Path).To(Equal("/livez"))
			Expect(container.ReadinessProbe).NotTo(BeNil())
			Expect(container.ReadinessProbe.HTTPGet.Path).To(Equal("/readyz"))
			Expect(container.StartupProbe).NotTo(BeNil())
			Expect(container.StartupProbe.HTTPGet.Path).To(Equal("/livez"))
		})

		It("Should configure a preStop lifecycle hook for Raft deregistration", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			container := sts.Spec.Template.Spec.Containers[0]
			Expect(container.Lifecycle).NotTo(BeNil())
			Expect(container.Lifecycle.PreStop).NotTo(BeNil())
			Expect(container.Lifecycle.PreStop.Exec).NotTo(BeNil())
			Expect(container.Lifecycle.PreStop.Exec.Command).To(HaveLen(3))
			Expect(container.Lifecycle.PreStop.Exec.Command[2]).To(ContainSubstring("/_admin/deregister"))
			Expect(container.Lifecycle.PreStop.Exec.Command[2]).To(ContainSubstring("rm -rf"))
		})

		It("Should set CLUSTER_ID env var to the stack name", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(sts.Spec.Template.Spec.Containers[0].Env).To(
				ContainElement(core.Env("CLUSTER_ID", stack.Name)),
			)
		})

		It("Should set downward API env vars (POD_NAME, POD_NAMESPACE)", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			env := sts.Spec.Template.Spec.Containers[0].Env
			Expect(env).To(ContainElement(HaveField("Name", "POD_NAME")))
			Expect(env).To(ContainElement(HaveField("Name", "POD_NAMESPACE")))
		})

		It("Should create a headless service for Raft peer discovery", func() {
			svc := &corev1.Service{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger-raft", svc)
			}).Should(Succeed())
			Expect(svc).To(BeControlledBy(ledger))
			Expect(svc.Spec.ClusterIP).To(Equal("None"))
			Expect(svc.Spec.PublishNotReadyAddresses).To(BeTrue())
			Expect(svc.Spec.Ports).To(ContainElements(
				HaveField("Name", "raft"),
				HaveField("Name", "grpc"),
			))
		})

		It("Should also create a v2 GatewayHTTPAPI with _healthcheck endpoint", func() {
			httpAPI := &v1beta1.GatewayHTTPAPI{}
			Eventually(func() error {
				return LoadResource("", core.GetObjectName(stack.Name, "ledger"), httpAPI)
			}).Should(Succeed())
			Expect(httpAPI.Spec.HealthCheckEndpoint).To(Equal("_healthcheck"))
		})

		It("Should also create a Database object for the v2 path", func() {
			database := &v1beta1.Database{}
			Eventually(func() error {
				return LoadResource("", core.GetObjectName(stack.Name, "ledger"), database)
			}).Should(Succeed())
		})

		It("Should use the correct v3 image", func() {
			sts := &appsv1.StatefulSet{}
			Eventually(func() error {
				return LoadResource(stack.Name, "ledger", sts)
			}).Should(Succeed())
			Expect(sts.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring("ledger-v3"))
			Expect(sts.Spec.Template.Spec.Containers[0].Image).To(ContainSubstring("v3.0.0"))
		})

		It("Should create a mirror provisioning job", func() {
			jobList := &batchv1.JobList{}
			Eventually(func(g Gomega) {
				g.Expect(List(jobList)).To(Succeed())
				found := false
				for _, j := range jobList.Items {
					if j.Namespace == stack.Name {
						for _, c := range j.Spec.Template.Spec.Containers {
							if c.Name == "provision-mirrors" {
								found = true
								g.Expect(c.Image).To(ContainSubstring("ledger-v3"))
								g.Expect(c.Args[0]).To(ContainSubstring("default"))
								g.Expect(c.Args[0]).To(ContainSubstring("payments"))
							}
						}
					}
				}
				g.Expect(found).To(BeTrue())
			}).Should(Succeed())
		})

		Context("with custom replicas setting", func() {
			var replicasSetting *v1beta1.Settings
			BeforeEach(func() {
				replicasSetting = settings.New(uuid.NewString(), "module.ledger.v3.replicas", "5", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(replicasSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(replicasSetting)).To(Succeed())
			})
			It("Should create a StatefulSet with 5 replicas", func() {
				sts := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) int32 {
					g.Expect(LoadResource(stack.Name, "ledger", sts)).To(Succeed())
					return *sts.Spec.Replicas
				}).Should(Equal(int32(5)))
			})
		})

		Context("with custom persistence sizes", func() {
			var walSizeSetting *v1beta1.Settings
			BeforeEach(func() {
				walSizeSetting = settings.New(uuid.NewString(), "module.ledger.v3.persistence.wal.size", "20Gi", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(walSizeSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(walSizeSetting)).To(Succeed())
			})
			It("Should create WAL PVC with custom size", func() {
				sts := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) string {
					g.Expect(LoadResource(stack.Name, "ledger", sts)).To(Succeed())
					return sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()
				}).Should(Equal("20Gi"))
			})
		})

		Context("with pebble settings", func() {
			var cacheSizeSetting *v1beta1.Settings
			BeforeEach(func() {
				cacheSizeSetting = settings.New(uuid.NewString(), "module.ledger.v3.pebble.cache-size", "2147483648", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(cacheSizeSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(cacheSizeSetting)).To(Succeed())
			})
			It("Should set PEBBLE_CACHE_SIZE env var", func() {
				sts := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(LoadResource(stack.Name, "ledger", sts)).To(Succeed())
					return sts.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElement(core.Env("PEBBLE_CACHE_SIZE", "2147483648")))
			})
		})

		Context("with raft settings", func() {
			var snapshotSetting *v1beta1.Settings
			BeforeEach(func() {
				snapshotSetting = settings.New(uuid.NewString(), "module.ledger.v3.raft.snapshot-threshold", "10000", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(snapshotSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(snapshotSetting)).To(Succeed())
			})
			It("Should set RAFT_SNAPSHOT_THRESHOLD env var", func() {
				sts := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(LoadResource(stack.Name, "ledger", sts)).To(Succeed())
					return sts.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElement(core.Env("RAFT_SNAPSHOT_THRESHOLD", "10000")))
			})
		})

		Context("with monitoring enabled", func() {
			var otelTracesDSNSetting *v1beta1.Settings
			BeforeEach(func() {
				otelTracesDSNSetting = settings.New(uuid.NewString(), "opentelemetry.traces.dsn", "grpc://collector", stack.Name)
			})
			JustBeforeEach(func() {
				Expect(Create(otelTracesDSNSetting)).To(Succeed())
			})
			AfterEach(func() {
				Expect(Delete(otelTracesDSNSetting)).To(Succeed())
			})
			It("Should add OTEL env vars to the StatefulSet", func() {
				sts := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) []corev1.EnvVar {
					g.Expect(LoadResource(stack.Name, "ledger", sts)).To(Succeed())
					return sts.Spec.Template.Spec.Containers[0].Env
				}).Should(ContainElement(HaveField("Name", "OTEL_SERVICE_NAME")))
			})
		})
	})
})
