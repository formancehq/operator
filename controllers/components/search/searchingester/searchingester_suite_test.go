package searchingester

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	"github.com/numary/formance-operator/internal"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestSearchIngester(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Search Ingester Suite")
}

var (
	ctx       context.Context
	cancel    func()
	nsClient  client.Client
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
)

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = componentsv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	Expect(v1beta1.AddToScheme(scheme.Scheme)).To(Succeed())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).ToNot(HaveOccurred())

	mutator := NewMutator(mgr.GetClient(), mgr.GetScheme())
	err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), mutator).
		SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

}, 60)

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = BeforeEach(func() {
	ns := uuid.NewString()
	Expect(k8sClient.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	})).To(BeNil())

	nsClient = client.NewNamespacedClient(k8sClient, ns)
})
