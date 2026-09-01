package tests_test

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/google/uuid"
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

	ledgerv1alpha1 "github.com/formancehq/ledger/misc/operator/api/v1alpha1"

	"github.com/formancehq/operator/v3/api/formance.com/v1beta1"
	"github.com/formancehq/operator/v3/internal/core"
	"github.com/formancehq/operator/v3/internal/resources/settings"
	. "github.com/formancehq/operator/v3/internal/tests/internal"
)

var ledgerV3ClusterGVK = schema.GroupVersionKind{
	Group:   "ledger.formance.com",
	Version: "v1alpha1",
	Kind:    "Cluster",
}

var ledgerV3IssuerGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "Issuer",
}

var ledgerV3CertificateGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "Certificate",
}

func newLedgerV3Cluster() *unstructured.Unstructured {
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(ledgerV3ClusterGVK)
	return cluster
}

func newLedgerV3Issuer() *unstructured.Unstructured {
	issuer := &unstructured.Unstructured{}
	issuer.SetGroupVersionKind(ledgerV3IssuerGVK)
	return issuer
}

func newLedgerV3Certificate() *unstructured.Unstructured {
	certificate := &unstructured.Unstructured{}
	certificate.SetGroupVersionKind(ledgerV3CertificateGVK)
	return certificate
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

var _ = Describe("Ledger v3 controller", func() {
	var (
		stack  *v1beta1.Stack
		ledger *v1beta1.Ledger
	)

	BeforeEach(func() {
		stack = &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v3.0.0-alpha.1"},
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
		Expect(Create(ledger)).To(Succeed())
	})

	AfterEach(func() {
		Expect(client.IgnoreNotFound(Delete(ledger))).To(Succeed())
		Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
	})

	It("delegates runtime provisioning to a Ledger v3 Cluster", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) error {
			err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(cluster).To(BeControlledBy(ledger))
			return nil
		}).Should(Succeed())

		repository, found, err := unstructured.NestedString(cluster.Object, "spec", "image", "repository")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(repository).To(Equal("ghcr.io/formancehq/ledger"))

		tag, found, err := unstructured.NestedString(cluster.Object, "spec", "image", "tag")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(tag).To(Equal("v3.0.0-alpha.1"))

		additionalLabels, found, err := unstructured.NestedStringMap(cluster.Object, "spec", "additionalLabels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(additionalLabels).To(Equal(map[string]string{
			"app.kubernetes.io/name":     "ledger",
			"app.kubernetes.io/instance": stack.Name,
		}))
		Expect(cluster.GetLabels()).To(HaveKeyWithValue(v1beta1.LedgerV3Label, "true"))

		_, found, err = unstructured.NestedMap(cluster.Object, "spec", "auth")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
		_, found, err = unstructured.NestedMap(cluster.Object, "spec", "monitoring")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())

		deployment := &appsv1.Deployment{}
		Consistently(func() bool {
			err := Get(core.GetNamespacedResourceName(stack.Name, "ledger"), deployment)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		database := &v1beta1.Database{}
		Consistently(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), database)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		grpcAPI := &v1beta1.GatewayGRPCAPI{}
		Consistently(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), grpcAPI)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})

	It("reuses and normalizes the historical Ledger replica setting", func() {
		replicaSettings := settings.New(uuid.NewString(), "deployments.ledger.replicas", "4", stack.Name)
		Expect(Create(replicaSettings)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(replicaSettings))).To(Succeed())
		})

		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) int64 {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			replicas, found, err := unstructured.NestedInt64(cluster.Object, "spec", "replicas")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return replicas
		}).Should(Equal(int64(5)))
	})

	It("reuses the historical Ledger container resource settings", func() {
		resourceSettings := []*v1beta1.Settings{
			settings.New(uuid.NewString(), "deployments.ledger.containers.ledger.resource-requirements.requests", "cpu=50m,memory=6Gi", stack.Name),
			settings.New(uuid.NewString(), "deployments.ledger.containers.ledger.resource-requirements.limits", "cpu=2,memory=6Gi", stack.Name),
		}
		for _, setting := range resourceSettings {
			Expect(Create(setting)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, setting := range resourceSettings {
				Expect(client.IgnoreNotFound(Delete(setting))).To(Succeed())
			}
		})

		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			resources, found, err := unstructured.NestedMap(cluster.Object, "spec", "resources")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return resources
		}).Should(Equal(map[string]any{
			"requests": map[string]any{
				"cpu":    "50m",
				"memory": "6Gi",
			},
			"limits": map[string]any{
				"cpu":    "2",
				"memory": "6Gi",
			},
		}))

		for _, setting := range resourceSettings {
			Expect(Delete(setting)).To(Succeed())
		}
		Eventually(func(g Gomega) bool {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			_, found, err := unstructured.NestedMap(cluster.Object, "spec", "resources")
			g.Expect(err).NotTo(HaveOccurred())
			return found
		}).Should(BeFalse())
	})

	It("configures TLS before the managed certificate is ready", func() {
		configuration := &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "ports-" + stack.Name},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks: []string{stack.Name},
				Cluster: ledgerv1alpha1.ClusterSpec{Service: ledgerv1alpha1.ServiceSpec{
					HttpPort: 19000,
					GrpcPort: 18888,
					RaftPort: 17777,
				}},
			},
		}
		Expect(Create(configuration)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(configuration))).To(Succeed())
		})

		issuer := newLedgerV3Issuer()
		certificate := newLedgerV3Certificate()
		cluster := newLedgerV3Cluster()
		tlsName := stack.Name + "-ledger-v3-tls"

		Eventually(func(g Gomega) {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name + "-ledger-v3-selfsigned"}, issuer)).To(Succeed())
			g.Expect(issuer).To(BeControlledBy(ledger))
			selfSigned, found, err := unstructured.NestedMap(issuer.Object, "spec", "selfSigned")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(selfSigned).To(BeEmpty())

			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: tlsName}, certificate)).To(Succeed())
			g.Expect(certificate).To(BeControlledBy(ledger))
		}).Should(Succeed())

		commonName, found, err := unstructured.NestedString(certificate.Object, "spec", "commonName")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(commonName).To(Equal("ledger-" + stack.Name + "." + stack.Name + ".svc.cluster.local"))
		isCA, found, err := unstructured.NestedBool(certificate.Object, "spec", "isCA")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(isCA).To(BeTrue())
		secretLabels, found, err := unstructured.NestedStringMap(certificate.Object, "spec", "secretTemplate", "labels")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(secretLabels).To(HaveKeyWithValue(v1beta1.GatewayBackendTLSSecretLabel, "true"))

		dnsNames, found, err := unstructured.NestedStringSlice(certificate.Object, "spec", "dnsNames")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(dnsNames).To(ConsistOf(
			"ledger-"+stack.Name,
			"ledger-"+stack.Name+"."+stack.Name,
			"ledger-"+stack.Name+"."+stack.Name+".svc",
			"ledger-"+stack.Name+"."+stack.Name+".svc.cluster.local",
			"ledger-"+stack.Name+"-grpc",
			"ledger-"+stack.Name+"-grpc."+stack.Name,
			"ledger-"+stack.Name+"-grpc."+stack.Name+".svc",
			"ledger-"+stack.Name+"-grpc."+stack.Name+".svc.cluster.local",
			"ledger-"+stack.Name+"-headless",
			"ledger-"+stack.Name+"-headless."+stack.Name,
			"ledger-"+stack.Name+"-headless."+stack.Name+".svc",
			"ledger-"+stack.Name+"-headless."+stack.Name+".svc.cluster.local",
			"*.ledger-"+stack.Name+"-headless",
			"*.ledger-"+stack.Name+"-headless."+stack.Name,
			"*.ledger-"+stack.Name+"-headless."+stack.Name+".svc",
			"*.ledger-"+stack.Name+"-headless."+stack.Name+".svc.cluster.local",
		))

		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			tls, found, err := unstructured.NestedMap(cluster.Object, "spec", "tls")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return tls
		}).Should(Equal(map[string]any{
			"enabled":     true,
			"secretName":  tlsName,
			"caSecretKey": "ca.crt",
		}))

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: stack.Name, Name: tlsName},
			Data: map[string][]byte{
				"tls.crt": []byte("certificate"),
				"tls.key": []byte("private key"),
				"ca.crt":  []byte("certificate authority"),
			},
		}
		Expect(Create(secret)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(secret))).To(Succeed())
		})

		Eventually(func(g Gomega) error {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: tlsName}, certificate)).To(Succeed())
			certificate.Object["status"] = map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "True", "reason": "Ready"},
				},
			}
			return TestContext().GetClient().Status().Update(TestContext(), certificate)
		}).Should(Succeed())

		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			tls, found, err := unstructured.NestedMap(cluster.Object, "spec", "tls")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return tls
		}).Should(Equal(map[string]any{
			"enabled":     true,
			"secretName":  tlsName,
			"caSecretKey": "ca.crt",
		}))

		Eventually(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			annotations, found, err := unstructured.NestedStringMap(cluster.Object, "spec", "podAnnotations")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return annotations["formance.com/ledger-v3-ca-sha256"]
		}).Should(Equal(sha256Hex(secret.Data["ca.crt"])))

		// A cert-manager CA rotation must change the pod template so that Ledger
		// reloads the client trust pool used for follower-to-leader forwarding.
		rotatedCA := []byte("rotated certificate authority")
		secret.Data["ca.crt"] = rotatedCA
		Expect(Update(secret)).To(Succeed())
		certificate.SetAnnotations(map[string]string{"tests.formance.com/rotation": uuid.NewString()})
		Expect(Update(certificate)).To(Succeed())

		Eventually(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			annotations, found, err := unstructured.NestedStringMap(cluster.Object, "spec", "podAnnotations")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return annotations["formance.com/ledger-v3-ca-sha256"]
		}).Should(Equal(sha256Hex(rotatedCA)))

		httpAPI := &v1beta1.GatewayHTTPAPI{}
		Eventually(func(g Gomega) *v1beta1.GatewayBackendRef {
			g.Expect(Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), httpAPI)).To(Succeed())
			g.Expect(httpAPI.Spec.Rules).To(HaveLen(1))
			g.Expect(httpAPI.Spec.Rules[0].Path).To(BeEmpty())
			return httpAPI.Spec.Rules[0].BackendRef
		}).Should(Equal(&v1beta1.GatewayBackendRef{
			Name: "ledger-" + stack.Name,
			Port: 19000,
		}))
		grpcAPI := &v1beta1.GatewayGRPCAPI{}
		Eventually(func(g Gomega) *v1beta1.GatewayBackendRef {
			g.Expect(Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), grpcAPI)).To(Succeed())
			g.Expect(grpcAPI.Spec.Name).To(Equal("ledger"))
			g.Expect(grpcAPI.Spec.GRPCServices).To(ConsistOf("ledger.BucketService"))
			return grpcAPI.Spec.BackendRef
		}).Should(Equal(&v1beta1.GatewayBackendRef{
			Name: "ledger-" + stack.Name,
			Port: 18888,
			TLS: &v1beta1.GatewayBackendTLS{
				SecretName:  tlsName,
				CASecretKey: "ca.crt",
				ServerName:  "ledger-" + stack.Name + "." + stack.Name + ".svc.cluster.local",
			},
		}))

		certificate.Object["status"] = map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False", "reason": "SecretMissing"},
			},
		}
		Expect(TestContext().GetClient().Status().Update(TestContext(), certificate)).To(Succeed())
		Eventually(func() error {
			return Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.GatewayGRPCAPI{})
		}).Should(BeNotFound())
		Eventually(func() error {
			return Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.GatewayHTTPAPI{})
		}).Should(BeNotFound())
	})

	It("configures authentication and monitoring from the existing stack settings", func() {
		auth := &v1beta1.Auth{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.AuthSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
		stackSettings := []*v1beta1.Settings{
			settings.New(uuid.NewString(), "auth.issuers", "https://issuer-one.example, https://issuer-two.example", stack.Name),
			settings.New(uuid.NewString(), "opentelemetry.traces.dsn", "grpc://otel-traces.monitoring.svc.cluster.local:4317?insecure=true", stack.Name),
			settings.New(uuid.NewString(), "opentelemetry.metrics.dsn", "http://otel-metrics.monitoring.svc.cluster.local:4318?insecure=false", stack.Name),
			settings.New(uuid.NewString(), "opentelemetry.logs.dsn", "grpc://otel-logs.monitoring.svc.cluster.local:4317?insecure=true", stack.Name),
			settings.New(uuid.NewString(), "opentelemetry.traces.resource-attributes", "service.namespace=formance,team=ledger", stack.Name),
		}
		Expect(Create(auth)).To(Succeed())
		for _, setting := range stackSettings {
			Expect(Create(setting)).To(Succeed())
		}
		DeferCleanup(func() {
			for _, setting := range stackSettings {
				Expect(client.IgnoreNotFound(Delete(setting))).To(Succeed())
			}
			Expect(client.IgnoreNotFound(Delete(auth))).To(Succeed())
		})

		Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
		patch := client.MergeFrom(ledger.DeepCopy())
		ledger.Spec.Auth = &v1beta1.AuthConfig{
			CheckScopes:          true,
			ReadKeySetMaxRetries: 7,
		}
		Expect(Patch(ledger, patch)).To(Succeed())

		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			authSpec, found, err := unstructured.NestedMap(cluster.Object, "spec", "auth")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return authSpec
		}).Should(SatisfyAll(
			HaveKeyWithValue("enabled", true),
			HaveKeyWithValue("issuer", "http://auth:8080"),
			HaveKeyWithValue("issuers", []any{"https://issuer-one.example", "https://issuer-two.example"}),
			HaveKeyWithValue("checkScopes", true),
			HaveKeyWithValue("service", "ledger"),
			HaveKeyWithValue("readKeySetMaxRetries", int64(7)),
		))

		monitoring, found, err := unstructured.NestedMap(cluster.Object, "spec", "monitoring")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(monitoring).To(SatisfyAll(
			HaveKeyWithValue("serviceName", "ledger"),
			HaveKeyWithValue("attributes", "pod-name=$(POD_NAME),service.namespace=formance,stack="+stack.Name+",team=ledger"),
			HaveKeyWithValue("traces", map[string]any{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "otel-traces.monitoring.svc.cluster.local",
				"port":     "4317",
				"insecure": "true",
				"mode":     "grpc",
				"batch":    "true",
			}),
			HaveKeyWithValue("metrics", map[string]any{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "otel-metrics.monitoring.svc.cluster.local",
				"port":     "4318",
				"insecure": "false",
				"mode":     "http",
				"runtime":  true,
			}),
			HaveKeyWithValue("logs", map[string]any{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "otel-logs.monitoring.svc.cluster.local",
				"port":     "4317",
				"insecure": "true",
				"mode":     "grpc",
			}),
			Not(HaveKey("pyroscope")),
		))
	})

	It("reacts to the Auth dependency lifecycle", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		auth := &v1beta1.Auth{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.AuthSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
		Expect(Create(auth)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(auth))).To(Succeed())
		})

		Eventually(func(g Gomega) bool {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			enabled, found, err := unstructured.NestedBool(cluster.Object, "spec", "auth", "enabled")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return enabled
		}).Should(BeTrue())

		Expect(Delete(auth)).To(Succeed())
		Eventually(func(g Gomega) bool {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			_, found, err := unstructured.NestedMap(cluster.Object, "spec", "auth")
			g.Expect(err).NotTo(HaveOccurred())
			return found
		}, time.Minute).Should(BeFalse())
	})

	It("combines a managed collector with logs configured in Settings", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		collector := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: stack.Name,
				Name:      "otel-collector",
				Labels: map[string]string{
					core.CollectorManagedByLabel: core.CollectorManagedByValue,
				},
				Annotations: map[string]string{
					core.SignalTracesAnnotation:  "true",
					core.SignalMetricsAnnotation: "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Port: 4318}},
			},
		}
		logsSetting := settings.New(uuid.NewString(), "opentelemetry.logs.dsn", "grpc://otel-logs.monitoring.svc.cluster.local:4317?insecure=true", stack.Name)
		Expect(Create(collector)).To(Succeed())
		Expect(Create(logsSetting)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(logsSetting))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(collector))).To(Succeed())
		})

		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			monitoring, found, err := unstructured.NestedMap(cluster.Object, "spec", "monitoring")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return monitoring
		}).Should(SatisfyAll(
			HaveKeyWithValue("traces", map[string]any{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "otel-collector." + stack.Name,
				"port":     "4318",
				"insecure": "true",
				"mode":     "http",
				"batch":    "true",
			}),
			HaveKeyWithValue("metrics", map[string]any{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "otel-collector." + stack.Name,
				"port":     "4318",
				"insecure": "true",
				"mode":     "http",
				"runtime":  true,
			}),
			HaveKeyWithValue("logs", map[string]any{
				"enabled":  true,
				"exporter": "otlp",
				"endpoint": "otel-logs.monitoring.svc.cluster.local",
				"port":     "4317",
				"insecure": "true",
				"mode":     "grpc",
			}),
		))
	})

	It("mirrors Cluster readiness on the Formance Ledger", func() {
		certificate := newLedgerV3Certificate()
		tlsName := stack.Name + "-ledger-v3-tls"
		Eventually(func(g Gomega) error {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: tlsName}, certificate)).To(Succeed())
			return nil
		}).Should(Succeed())
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: stack.Name, Name: tlsName},
			Data: map[string][]byte{
				"tls.crt": []byte("certificate"),
				"tls.key": []byte("private key"),
				"ca.crt":  []byte("certificate authority"),
			},
		}
		Expect(Create(secret)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(secret))).To(Succeed())
		})
		certificate.Object["status"] = map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True", "reason": "Ready"},
			},
		}
		Expect(TestContext().GetClient().Status().Update(TestContext(), certificate)).To(Succeed())

		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		Eventually(func(g Gomega) bool {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			cluster.Object["status"] = map[string]any{
				"phase":              "Running",
				"readyReplicas":      int64(3),
				"observedGeneration": cluster.GetGeneration(),
				"conditions": []any{
					map[string]any{
						"type":               "SinksSynced",
						"status":             "True",
						"reason":             "Synced",
						"observedGeneration": cluster.GetGeneration(),
					},
				},
			}
			if err := TestContext().GetClient().Status().Update(TestContext(), cluster); err != nil {
				return false
			}
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			return ledger.Status.Ready
		}, time.Minute).Should(BeTrue())
	})

	It("removes stale v2 readiness conditions", func() {
		legacyTransitionTime := metav1.Now()
		Eventually(func() error {
			if err := LoadResource("", ledger.Name, ledger); err != nil {
				return err
			}
			patch := client.MergeFrom(ledger.DeepCopy())
			ledger.Status.Conditions = v1beta1.Conditions{
				{Type: "DatabaseReady", Status: metav1.ConditionTrue, LastTransitionTime: legacyTransitionTime},
				{Type: "DeploymentReady", Status: metav1.ConditionFalse, Reason: "Ledger", LastTransitionTime: legacyTransitionTime},
				{Type: "DeploymentReady", Status: metav1.ConditionTrue, Reason: "LedgerWorker", LastTransitionTime: legacyTransitionTime},
				{Type: "PodDisruptionBudget", Status: metav1.ConditionTrue, Reason: "Ledger", LastTransitionTime: legacyTransitionTime},
			}
			return TestContext().GetClient().Status().Patch(TestContext(), ledger, patch)
		}).Should(Succeed())

		Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
		patch := client.MergeFrom(ledger.DeepCopy())
		ledger.Spec.Debug = true
		Expect(Patch(ledger, patch)).To(Succeed())

		Eventually(func(g Gomega) []v1beta1.Condition {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			return ledger.Status.Conditions
		}).Should(And(
			Not(ContainElement(HaveField("Type", "DatabaseReady"))),
			Not(ContainElement(HaveField("Type", "DeploymentReady"))),
			Not(ContainElement(HaveField("Type", "PodDisruptionBudget"))),
		))
	})

	It("removes the Cluster when the stack is disabled", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		Expect(LoadResource("", stack.Name, stack)).To(Succeed())
		patch := client.MergeFrom(stack.DeepCopy())
		stack.Spec.Disabled = true
		Expect(Patch(stack, patch)).To(Succeed())

		Eventually(func() bool {
			err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		grpcAPI := &v1beta1.GatewayGRPCAPI{}
		Eventually(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), grpcAPI)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})

	It("requires an explicit migration before switching back to v2", func() {
		cluster := newLedgerV3Cluster()
		Eventually(func() error {
			return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
		}).Should(Succeed())

		Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
		patch := client.MergeFrom(ledger.DeepCopy())
		ledger.Spec.Version = "v2.99.0"
		Expect(Patch(ledger, patch)).To(Succeed())

		Consistently(func() bool {
			err := Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		Consistently(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.Database{})
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())

		Eventually(func(g Gomega) string {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			return ledger.Status.Info
		}).Should(ContainSubstring("migration required"))

		Expect(Delete(cluster)).To(Succeed())

		grpcAPI := &v1beta1.GatewayGRPCAPI{}
		Eventually(func() bool {
			err := Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), grpcAPI)
			return apierrors.IsNotFound(err)
		}).Should(BeTrue())
	})

	Context("when legacy resources already exist", func() {
		var databaseSettings *v1beta1.Settings

		BeforeEach(func() {
			stack.Spec.Version = "v2.99.0"
			databaseSettings = settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", stack.Name)
			Expect(Create(databaseSettings)).To(Succeed())
		})

		AfterEach(func() {
			Expect(client.IgnoreNotFound(Delete(databaseSettings))).To(Succeed())
		})

		It("requires an explicit migration before switching to v3", func() {
			Eventually(func() error {
				return Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
			}).Should(Succeed())

			Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			patch := client.MergeFrom(ledger.DeepCopy())
			ledger.Spec.Version = "v3.0.0-alpha.1"
			Expect(Patch(ledger, patch)).To(Succeed())

			cluster := newLedgerV3Cluster()
			Consistently(func() bool {
				err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())

			Eventually(func(g Gomega) string {
				g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
				return ledger.Status.Info
			}).Should(ContainSubstring("migration required"))
		})

		It("rejects a preview version at or below the v3 threshold", func() {
			previewSettings := settings.New(uuid.NewString(), "ledger.v3.preview-version", "v3.0.0-alpha", stack.Name)
			Expect(Create(previewSettings)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(Delete(previewSettings))).To(Succeed())
			})

			cluster := newLedgerV3Cluster()
			Consistently(func() bool {
				err := Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
				return apierrors.IsNotFound(err)
			}).Should(BeTrue())
			Eventually(func(g Gomega) string {
				g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
				return ledger.Status.Info
			}).Should(ContainSubstring("ledger.v3.preview-version must be greater than v3.0.0-alpha"))
		})

		Context("with a Ledger v3 preview version", func() {
			var previewSettings *v1beta1.Settings

			BeforeEach(func() {
				previewSettings = settings.New(uuid.NewString(), "ledger.v3.preview-version", "v3.0.0-alpha.11", stack.Name)
				Expect(Create(previewSettings)).To(Succeed())
			})

			AfterEach(func() {
				Expect(client.IgnoreNotFound(Delete(previewSettings))).To(Succeed())
			})

			It("runs v2 and a separately routed v3 Cluster at the same time", func() {
				EventualDeployment := func() error {
					return Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
				}
				Eventually(EventualDeployment).Should(Succeed())

				cluster := newLedgerV3Cluster()
				Eventually(func(g Gomega) map[string]any {
					g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
					tag, found, err := unstructured.NestedString(cluster.Object, "spec", "image", "tag")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(found).To(BeTrue())
					g.Expect(tag).To(Equal("v3.0.0-alpha.11"))
					labels, found, err := unstructured.NestedStringMap(cluster.Object, "spec", "additionalLabels")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(found).To(BeTrue())
					return map[string]any{"metadata": cluster.GetLabels(), "additional": labels}
				}).Should(Equal(map[string]any{
					"metadata": map[string]string{
						v1beta1.StackLabel:               stack.Name,
						v1beta1.LedgerV3Label:            "true",
						"formance.com/ledger-v3-preview": "true",
					},
					"additional": map[string]string{
						"app.kubernetes.io/name":         "ledger-v3-preview",
						"app.kubernetes.io/instance":     stack.Name,
						"formance.com/ledger-v3-preview": "true",
					},
				}))

				Eventually(EventualDeployment).Should(Succeed())
				Eventually(func() error {
					return Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.Database{})
				}).Should(Succeed())

				certificate := newLedgerV3Certificate()
				tlsName := stack.Name + "-ledger-v3-tls"
				Eventually(func(g Gomega) error {
					g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: tlsName}, certificate)).To(Succeed())
					return nil
				}).Should(Succeed())

				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Namespace: stack.Name, Name: tlsName},
					Data: map[string][]byte{
						"tls.crt": []byte("certificate"),
						"tls.key": []byte("private key"),
						"ca.crt":  []byte("certificate authority"),
					},
				}
				Expect(Create(secret)).To(Succeed())
				DeferCleanup(func() {
					Expect(client.IgnoreNotFound(Delete(secret))).To(Succeed())
				})

				certificate.Object["status"] = map[string]any{
					"conditions": []any{
						map[string]any{"type": "Ready", "status": "True", "reason": "Ready"},
					},
				}
				Expect(TestContext().GetClient().Status().Update(TestContext(), certificate)).To(Succeed())

				httpAPI := &v1beta1.GatewayHTTPAPI{}
				Eventually(func(g Gomega) []v1beta1.GatewayHTTPAPIRule {
					g.Expect(Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), httpAPI)).To(Succeed())
					return httpAPI.Spec.Rules
				}).Should(ContainElement(SatisfyAll(
					HaveField("Path", "/v3"),
					HaveField("BackendRef.Name", "ledger-"+stack.Name),
					HaveField("BackendRef.Port", int32(9000)),
				)))

				grpcAPI := &v1beta1.GatewayGRPCAPI{}
				Eventually(func(g Gomega) *v1beta1.GatewayBackendRef {
					g.Expect(Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), grpcAPI)).To(Succeed())
					g.Expect(grpcAPI.Spec.GRPCServices).To(ConsistOf("ledger.BucketService"))
					return grpcAPI.Spec.BackendRef
				}).Should(Equal(&v1beta1.GatewayBackendRef{
					Name: "ledger-" + stack.Name,
					Port: 8888,
					TLS: &v1beta1.GatewayBackendTLS{
						SecretName:  stack.Name + "-ledger-v3-tls",
						CASecretKey: "ca.crt",
						ServerName:  "ledger-" + stack.Name + "." + stack.Name + ".svc.cluster.local",
					},
				}))

				certificate.Object["status"] = map[string]any{
					"conditions": []any{
						map[string]any{"type": "Ready", "status": "False", "reason": "SecretMissing"},
					},
				}
				Expect(TestContext().GetClient().Status().Update(TestContext(), certificate)).To(Succeed())
				Eventually(func(g Gomega) []v1beta1.GatewayHTTPAPIRule {
					g.Expect(Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), httpAPI)).To(Succeed())
					return httpAPI.Spec.Rules
				}).Should(SatisfyAll(
					HaveLen(1),
					ContainElement(SatisfyAll(HaveField("Path", ""), HaveField("BackendRef", BeNil()))),
				))
				Eventually(func() error {
					return Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.GatewayGRPCAPI{})
				}).Should(BeNotFound())
			})

			It("does not allow the preview Cluster to bypass the migration guard", func() {
				cluster := newLedgerV3Cluster()
				Eventually(func() error {
					return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
				}).Should(Succeed())

				Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
				patch := client.MergeFrom(ledger.DeepCopy())
				ledger.Spec.Version = "v3.0.0-alpha.11"
				Expect(Patch(ledger, patch)).To(Succeed())

				Consistently(func() error {
					return Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
				}).Should(Succeed())
				Eventually(func(g Gomega) string {
					g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
					return ledger.Status.Info
				}).Should(ContainSubstring("migration required"))
			})

			It("removes only the preview resources when the setting is deleted", func() {
				cluster := newLedgerV3Cluster()
				Eventually(func() error {
					return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
				}).Should(Succeed())

				certificate := newLedgerV3Certificate()
				issuer := newLedgerV3Issuer()
				Eventually(func(g Gomega) {
					g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name + "-ledger-v3-tls"}, certificate)).To(Succeed())
					g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name + "-ledger-v3-selfsigned"}, issuer)).To(Succeed())
				}).Should(Succeed())

				Expect(Delete(previewSettings)).To(Succeed())

				Eventually(func() error {
					return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)
				}).Should(BeNotFound())
				Eventually(func() error {
					return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name + "-ledger-v3-tls"}, certificate)
				}).Should(BeNotFound())
				Eventually(func() error {
					return Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name + "-ledger-v3-selfsigned"}, issuer)
				}).Should(BeNotFound())

				Eventually(func() error {
					return Get(core.GetNamespacedResourceName(stack.Name, "ledger"), &appsv1.Deployment{})
				}).Should(Succeed())
				Eventually(func() error {
					return Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.Database{})
				}).Should(Succeed())

				httpAPI := &v1beta1.GatewayHTTPAPI{}
				Eventually(func(g Gomega) []v1beta1.GatewayHTTPAPIRule {
					g.Expect(Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), httpAPI)).To(Succeed())
					return httpAPI.Spec.Rules
				}).ShouldNot(ContainElement(HaveField("Path", "/v3")))
				Eventually(func() error {
					return Get(core.GetResourceName(core.GetObjectName(stack.Name, "ledger")), &v1beta1.GatewayGRPCAPI{})
				}).Should(BeNotFound())
			})
		})
	})
})

var _ = Describe("LedgerConfiguration", Serial, func() {
	It("inherits the Ledger Cluster schema validation", func() {
		invalidEnum := &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-enum"},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks:  []string{"*"},
				Cluster: ledgerv1alpha1.ClusterSpec{LogLevel: "verbose"},
			},
		}
		Expect(apierrors.IsInvalid(Create(invalidEnum))).To(BeTrue())

		invalidCEL := &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-cel"},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks: []string{"*"},
				Cluster: ledgerv1alpha1.ClusterSpec{
					Ingress: &ledgerv1alpha1.IngressSpec{Enabled: true},
				},
			},
		}
		Expect(apierrors.IsInvalid(Create(invalidCEL))).To(BeTrue())

		mixedWildcard := &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "invalid-mixed-wildcard"},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks: []string{"*", "explicit-stack"},
			},
		}
		Expect(apierrors.IsInvalid(Create(mixedWildcard))).To(BeTrue())
	})

	It("uses the default configuration as a live base for Ledger v3 Clusters", func() {
		configuration := &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: v1beta1.DefaultLedgerConfigurationName},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks: []string{"*"},
				Cluster: ledgerv1alpha1.ClusterSpec{
					LogLevel:      "info",
					HashAlgorithm: "blake3",
					NodeSelector:  map[string]string{"disk": "nvme"},
					PodAnnotations: map[string]string{
						"configuration": "preserved",
					},
					Monitoring: &ledgerv1alpha1.MonitoringConfig{
						Pyroscope: &ledgerv1alpha1.PyroscopeConfig{
							Enabled:       true,
							ServerAddress: "http://pyroscope.monitoring.svc:4040",
						},
					},
				},
			},
		}
		Expect(Create(configuration)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(configuration))).To(Succeed())
		})

		stack := &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v3.0.0-alpha.1"},
		}
		ledger := &v1beta1.Ledger{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{Stack: stack.Name},
			},
		}
		Expect(Create(stack)).To(Succeed())
		Expect(Create(ledger)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(ledger))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(stack))).To(Succeed())
		})

		previewStack := &v1beta1.Stack{
			ObjectMeta: RandObjectMeta(),
			Spec:       v1beta1.StackSpec{Version: "v2.99.0"},
		}
		previewLedger := &v1beta1.Ledger{
			ObjectMeta: RandObjectMeta(),
			Spec: v1beta1.LedgerSpec{
				StackDependency: v1beta1.StackDependency{Stack: previewStack.Name},
			},
		}
		previewVersion := settings.New(uuid.NewString(), "ledger.v3.preview-version", "v3.0.0-alpha.11", previewStack.Name)
		previewDatabase := settings.New(uuid.NewString(), "postgres.*.uri", "postgresql://localhost", previewStack.Name)
		Expect(Create(previewStack)).To(Succeed())
		Expect(Create(previewVersion)).To(Succeed())
		Expect(Create(previewDatabase)).To(Succeed())
		Expect(Create(previewLedger)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(Delete(previewLedger))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(previewVersion))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(previewDatabase))).To(Succeed())
			Expect(client.IgnoreNotFound(Delete(previewStack))).To(Succeed())
		})

		cluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			spec, found, err := unstructured.NestedMap(cluster.Object, "spec")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return spec
		}).Should(SatisfyAll(
			HaveKeyWithValue("logLevel", "info"),
			HaveKeyWithValue("hashAlgorithm", "blake3"),
			HaveKeyWithValue("nodeSelector", map[string]any{"disk": "nvme"}),
			HaveKeyWithValue("monitoring", SatisfyAll(
				HaveKeyWithValue("serviceName", "ledger"),
				HaveKeyWithValue("pyroscope", SatisfyAll(
					HaveKeyWithValue("enabled", true),
					HaveKeyWithValue("serverAddress", "http://pyroscope.monitoring.svc:4040"),
				)),
			)),
		))
		previewCluster := newLedgerV3Cluster()
		Eventually(func(g Gomega) map[string]any {
			g.Expect(Get(types.NamespacedName{Namespace: previewStack.Name, Name: previewStack.Name}, previewCluster)).To(Succeed())
			nodeSelector, found, err := unstructured.NestedMap(previewCluster.Object, "spec", "nodeSelector")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return nodeSelector
		}).Should(Equal(map[string]any{"disk": "nvme"}))

		configuration.Spec.Cluster.HashAlgorithm = "xxh3"
		configuration.Spec.Cluster.Monitoring.Pyroscope.ServerAddress = "http://pyroscope-v2.monitoring.svc:4040"
		Expect(Update(configuration)).To(Succeed())
		Eventually(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			hash, found, err := unstructured.NestedString(cluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return hash
		}).Should(Equal("xxh3"))
		Eventually(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: previewStack.Name, Name: previewStack.Name}, previewCluster)).To(Succeed())
			hash, found, err := unstructured.NestedString(previewCluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return hash
		}).Should(Equal("xxh3"))

		specificConfiguration := &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "specific-" + stack.Name},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks: []string{stack.Name},
				Cluster: ledgerv1alpha1.ClusterSpec{
					HashAlgorithm: "blake3",
					LogLevel:      "debug",
				},
			},
		}
		Expect(Create(specificConfiguration)).To(Succeed())
		Eventually(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			hash, found, err := unstructured.NestedString(cluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return hash
		}).Should(Equal("blake3"))
		Consistently(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: previewStack.Name, Name: previewStack.Name}, previewCluster)).To(Succeed())
			hash, found, err := unstructured.NestedString(previewCluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return hash
		}).Should(Equal("xxh3"))
		Expect(Delete(specificConfiguration)).To(Succeed())
		Eventually(func(g Gomega) string {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			hash, found, err := unstructured.NestedString(cluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			return hash
		}).Should(Equal("xxh3"))

		Expect(Delete(configuration)).To(Succeed())
		Eventually(func(g Gomega) bool {
			g.Expect(Get(types.NamespacedName{Namespace: stack.Name, Name: stack.Name}, cluster)).To(Succeed())
			_, found, err := unstructured.NestedString(cluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			return found
		}).Should(BeFalse())
		Eventually(func(g Gomega) bool {
			g.Expect(Get(types.NamespacedName{Namespace: previewStack.Name, Name: previewStack.Name}, previewCluster)).To(Succeed())
			_, found, err := unstructured.NestedString(previewCluster.Object, "spec", "hashAlgorithm")
			g.Expect(err).NotTo(HaveOccurred())
			return found
		}).Should(BeFalse())

		configuration = &v1beta1.LedgerConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: v1beta1.DefaultLedgerConfigurationName},
			Spec: v1beta1.LedgerConfigurationSpec{
				Stacks: []string{"*"},
				Cluster: ledgerv1alpha1.ClusterSpec{
					BindAddr: "127.0.0.1:7777",
				},
			},
		}
		Expect(Create(configuration)).To(Succeed())
		Eventually(func(g Gomega) string {
			g.Expect(LoadResource("", ledger.Name, ledger)).To(Succeed())
			condition := ledger.GetConditions().Get("LedgerV3ClusterReady")
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Reason).To(Equal("ReconcileFailed"))
			return condition.Message
		}).Should(ContainSubstring("bindAddr"))
	})
})
