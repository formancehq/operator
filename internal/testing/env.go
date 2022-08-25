package testing

import (
	"context"
	"os"
	"path/filepath"
	osRuntime "runtime"
	"sync"

	"github.com/google/uuid"
	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	benthoscomponentsformancecomv1beta1 "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	. "github.com/numary/formance-operator/apis/sharedtypes"
	stackv1beta1 "github.com/numary/formance-operator/apis/stack/v1beta1"
	"github.com/numary/formance-operator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	ctx       context.Context
	cancel    func()
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client

	ns *corev1.Namespace

	once sync.Once
)

func start() {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	_, filename, _, _ := osRuntime.Caller(0)

	//By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(filepath.Dir(filename), "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	Expect(componentsv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(authcomponentsv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(benthoscomponentsformancecomv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())
	Expect(stackv1beta1.AddToScheme(scheme.Scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
}

func ClusterConfig() *rest.Config {
	once.Do(start)
	return cfg
}

func GetClient() client.Client {
	once.Do(start)
	return k8sClient
}

func ActualContext() context.Context {
	once.Do(start)
	return ctx
}

func GetScheme() *runtime.Scheme {
	return scheme.Scheme
}

var _ = SynchronizedAfterSuite(func() {
	cancel()
	Expect(testEnv.Stop()).To(BeNil())
}, func() {})

func WithMutator[T Object](mutator internal.Mutator[T], fn func()) {
	var (
		ctx    context.Context
		cancel func()
		done   chan struct{}
	)
	BeforeEach(func() {
		ctx, cancel = context.WithCancel(ActualContext())
		done = make(chan struct{})
		mgr, err := ctrl.NewManager(ClusterConfig(), ctrl.Options{
			Scheme:             scheme.Scheme,
			MetricsBindAddress: "0",
		})
		Expect(err).ToNot(HaveOccurred())

		err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), mutator).SetupWithManager(mgr)
		Expect(err).ToNot(HaveOccurred())

		go func() {
			defer GinkgoRecover()
			err := mgr.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "failed to run manager")
			close(done)
		}()
	})
	AfterEach(func() {
		cancel()
		select {
		case <-ActualContext().Done():
		case <-done:
		}
	})
	fn()
}

func WithNewNamespace(fn func()) {
	var oldClient client.Client
	BeforeEach(func() {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: uuid.NewString(),
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(BeNil())

		oldClient = k8sClient
		k8sClient = client.NewNamespacedClient(k8sClient, ns.Name)
	})
	AfterEach(func() {
		Expect(k8sClient.Delete(ctx, ns))
		k8sClient = oldClient
	})
	fn()
}

func Create(ob client.Object) error {
	return k8sClient.Create(ctx, ob)
}

func Update(ob client.Object) error {
	return k8sClient.Update(ctx, ob)
}

func Delete(ob client.Object) error {
	return k8sClient.Delete(ctx, ob)
}

func Get(key types.NamespacedName, ob client.Object) error {
	return k8sClient.Get(ctx, key, ob)
}