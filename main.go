/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	benthoscomponentsformancecomv1beta1 "github.com/numary/formance-operator/apis/components/benthos/v1beta1"
	"github.com/numary/formance-operator/controllers/components/benthos"
	"github.com/numary/formance-operator/controllers/components/search"
	traefik "github.com/traefik/traefik/v2/pkg/provider/kubernetes/crd/traefik/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	authcomponentsv1beta1 "github.com/numary/formance-operator/apis/components/auth/v1beta1"
	componentsv1beta1 "github.com/numary/formance-operator/apis/components/v1beta1"
	stackv1beta1 "github.com/numary/formance-operator/apis/stack/v1beta1"
	"github.com/numary/formance-operator/controllers/components/auth"
	"github.com/numary/formance-operator/controllers/components/auth/clients"
	"github.com/numary/formance-operator/controllers/components/auth/scopes"
	"github.com/numary/formance-operator/controllers/components/ledger"
	"github.com/numary/formance-operator/controllers/stack"
	"github.com/numary/formance-operator/internal"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(stackv1beta1.AddToScheme(scheme))
	utilruntime.Must(componentsv1beta1.AddToScheme(scheme))
	utilruntime.Must(authcomponentsv1beta1.AddToScheme(scheme))
	utilruntime.Must(traefik.AddToScheme(scheme))

	utilruntime.Must(benthoscomponentsformancecomv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "68fe8eef.formance.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	stackMutator := stack.NewMutator(mgr.GetClient(), mgr.GetScheme())
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), stackMutator).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Stack")
		os.Exit(1)
	}
	authMutator := auth.NewMutator(mgr.GetClient(), mgr.GetScheme())
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), authMutator).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Auth")
		os.Exit(1)
	}
	ledgerMutator := ledger.NewMutator(mgr.GetClient(), mgr.GetScheme())
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), ledgerMutator).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Ledger")
		os.Exit(1)
	}
	//if err = (&componentscontrollers.PaymentsReconciler{
	//	Client: mgr.GetClient(),
	//	Scheme: mgr.GetScheme(),
	//}).SetupWithManager(mgr); err != nil {
	//	setupLog.Error(err, "unable to create controller", "controller", "Payments")
	//	os.Exit(1)
	//}
	searchMutator := search.NewMutator(mgr.GetClient(), mgr.GetScheme())
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), searchMutator).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Search")
		os.Exit(1)
	}
	//if err = (&componentscontrollers.ControlReconciler{
	//	Client: mgr.GetClient(),
	//	Scheme: mgr.GetScheme(),
	//}).SetupWithManager(mgr); err != nil {
	//	setupLog.Error(err, "unable to create controller", "controller", "Control")
	//	os.Exit(1)
	//}
	clientMutator := clients.NewMutator(mgr.GetClient(), mgr.GetScheme(), clients.DefaultApiFactory)
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), clientMutator).
		SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Client")
		os.Exit(1)
	}
	scopeMutator := scopes.NewMutator(mgr.GetClient(), mgr.GetScheme(), scopes.DefaultApiFactory)
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), scopeMutator).
		SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Scope")
		os.Exit(1)
	}
	serverMutator := benthos.NewServerMutator(mgr.GetClient(), mgr.GetScheme())
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), serverMutator).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Server")
		os.Exit(1)
	}
	streamMutator := benthos.NewStreamMutator(mgr.GetClient(), mgr.GetScheme())
	if err = internal.NewReconciler(mgr.GetClient(), mgr.GetScheme(), streamMutator).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Stream")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
