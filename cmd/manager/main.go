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

//nolint:gci
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/controllers"
	"github.com/telekom/das-schiff-network-operator/pkg/agent"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
	"github.com/telekom/das-schiff-network-operator/pkg/bpf"
	grpcclient "github.com/telekom/das-schiff-network-operator/pkg/clients/grpc"
	"github.com/telekom/das-schiff-network-operator/pkg/config"
	"github.com/telekom/das-schiff-network-operator/pkg/healthcheck"
	"github.com/telekom/das-schiff-network-operator/pkg/macvlan"
	"github.com/telekom/das-schiff-network-operator/pkg/managerconfig"
	"github.com/telekom/das-schiff-network-operator/pkg/monitoring"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
	"github.com/telekom/das-schiff-network-operator/pkg/notrack"
	"github.com/telekom/das-schiff-network-operator/pkg/reconciler"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.) //nolint:gci
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	//nolint:gci // kubebuilder import
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func initCollectors() error {
	var err error
	collector, err := monitoring.NewDasSchiffNetworkOperatorCollector(map[string]bool{})
	if err != nil {
		return fmt.Errorf("failed to create collector: %w", err)
	}
	setupLog.Info("initialize collectors")
	collectors := []string{}
	for c := range collector.Collectors {
		collectors = append(collectors, c)
	}
	sort.Strings(collectors)
	for index := range collectors {
		setupLog.Info("registered collector", "collector", collectors[index])
	}
	if err := metrics.Registry.Register(collector); err != nil {
		return fmt.Errorf("failed to register collector: %w", err)
	}
	return nil
}

func main() {
	var onlyBPFMode bool
	var configFile string
	var interfacePrefix string
	var nodeConfigPath string
	var agentType string
	var agentPort int
	var agentAddr string
	flag.StringVar(&configFile, "config", "",
		"The controller will load its initial configuration from this file. "+
			"Omit this flag to use the default configuration values. "+
			"Command-line flags override configuration from this file.")
	flag.BoolVar(&onlyBPFMode, "only-attach-bpf", false,
		"Only attach BPF to specified interfaces in config. This will not start any reconciliation. Perfect for masters.")
	flag.StringVar(&interfacePrefix, "macvlan-interface-prefix", "",
		"Interface prefix for bridge devices for MACVlan sync")
	flag.StringVar(&nodeConfigPath, "nodeconfig-path", reconciler.DefaultNodeConfigPath,
		"Path to store working node configuration.")
	flag.StringVar(&agentType, "agent", "vrf-igbp", "Use selected agent type (default: vrf-igbp).")
	flag.StringVar(&agentAddr, "agentAddr", "", "Agent's address (default: '').")
	flag.IntVar(&agentPort, "agentPort", agent.DefaultPort, fmt.Sprintf("Agent's port (default: %d).", agent.DefaultPort))
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	var err error
	var options manager.Options
	if configFile != "" {
		options, err = managerconfig.Load(configFile, scheme)
		if err != nil {
			setupLog.Error(err, "unable to load the config file")
			os.Exit(1)
		}
	} else {
		options = ctrl.Options{Scheme: scheme}
	}
	if options.MetricsBindAddress != "0" && options.MetricsBindAddress != "" {
		err = initCollectors()
		if err != nil {
			setupLog.Error(err, "unable to initialize metrics collectors")
			os.Exit(1)
		}
	}

	var agentClient agent.Client
	switch agentType {
	case "vrf-igbp", "netns":
		agentClient, err = grpcclient.NewClient(fmt.Sprintf("%s:%d", agentAddr, agentPort))
		if err != nil {
			setupLog.Error(err, "error creating agent's client")
		}
	default:
		setupLog.Error(fmt.Errorf("agent %s is currently not supported", agentType), "unsupported error")
		os.Exit(1)
	}

	if err != nil {
		setupLog.Error(err, "error creating agent's client")
		os.Exit(1)
	}

	if err := start(&options, onlyBPFMode, nodeConfigPath, interfacePrefix, agentClient); err != nil {
		setupLog.Error(err, "error running manager")
		os.Exit(1)
	}
}

func start(options *manager.Options, onlyBPFMode bool, nodeConfigPath, interfacePrefix string, agentClient agent.Client) error {
	clientConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(clientConfig, *options)
	if err != nil {
		return fmt.Errorf("unable to create manager: %w", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("unable to load config: %w", err)
	}

	anycastTracker := anycast.NewTracker(&nl.Toolkit{})

	if err := (&v1alpha1.VRFRouteConfiguration{}).SetupWebhookWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create webhook VRFRouteConfiguration: %w", err)
	}

	if err := initComponents(mgr, anycastTracker, cfg, clientConfig, onlyBPFMode, nodeConfigPath, agentClient); err != nil {
		return fmt.Errorf("unable to initialize components: %w", err)
	}

	if interfacePrefix != "" {
		setupLog.Info("start macvlan sync")
		macvlan.RunMACSync(interfacePrefix)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("error running manager: %w", err)
	}

	return nil
}

func initComponents(mgr manager.Manager, anycastTracker *anycast.Tracker, cfg *config.Config, clientConfig *rest.Config, onlyBPFMode bool, nodeConfigPath string, agentClient agent.Client) error {
	// Start VRFRouteConfigurationReconciler when we are not running in only BPF mode.
	if !onlyBPFMode {
		if err := setupReconcilers(mgr, nodeConfigPath, agentClient); err != nil {
			return fmt.Errorf("unable to setup reconcilers: %w", err)
		}
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	if err := setupBPF(cfg); err != nil {
		return fmt.Errorf("uneable to set up BPF: %w", err)
	}

	setupLog.Info("start anycast sync")
	anycastTracker.RunAnycastSync()

	setupLog.Info("start notrack sync")
	if err := notrack.RunIPTablesSync(); err != nil {
		setupLog.Error(err, "error starting IPTables sync")
	}

	if onlyBPFMode {
		if err := runBPFOnlyMode(clientConfig); err != nil {
			return fmt.Errorf("error running BPF only mode: %w", err)
		}
	}

	return nil
}

func runBPFOnlyMode(clientConfig *rest.Config) error {
	clusterClient, err := client.New(clientConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("error creating controller-runtime client: %w", err)
	}

	nc, err := healthcheck.LoadConfig(healthcheck.NetHealthcheckFile)
	if err != nil {
		return fmt.Errorf("error loading network healthcheck config: %w", err)
	}

	tcpDialer := healthcheck.NewTCPDialer(nc.Timeout)
	hc, err := healthcheck.NewHealthChecker(clusterClient,
		healthcheck.NewDefaultHealthcheckToolkit(nil, tcpDialer),
		nc)
	if err != nil {
		return fmt.Errorf("error initializing healthchecker: %w", err)
	}
	if err = performNetworkingHealthcheck(hc); err != nil {
		return fmt.Errorf("error performing healthcheck: %w", err)
	}

	return nil
}

func setupBPF(cfg *config.Config) error {
	setupLog.Info("load bpf program into Kernel")
	if err := bpf.InitBPFRouter(); err != nil {
		return fmt.Errorf("unable to init BPF router: %w", err)
	}
	setupLog.Info("attach bpf to interfaces specified in config")
	if err := bpf.AttachInterfaces(cfg.BPFInterfaces); err != nil {
		return fmt.Errorf("unable to attach bpf to interfaces: %w", err)
	}

	setupLog.Info("start bpf interface check")
	bpf.RunInterfaceCheck()

	return nil
}

func setupReconcilers(mgr manager.Manager, nodeConfigPath string, agentClient agent.Client) error {
	r, err := reconciler.NewReconciler(mgr.GetClient(), mgr.GetLogger(), nodeConfigPath, agentClient)
	if err != nil {
		return fmt.Errorf("unable to create debounced reconciler: %w", err)
	}

	if err = (&controllers.NodeConfigReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		Reconciler: r,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("unable to create VRFRouteConfiguration controller: %w", err)
	}

	return nil
}

func performNetworkingHealthcheck(hc *healthcheck.HealthChecker) error {
	if err := hc.CheckInterfaces(); err != nil {
		return fmt.Errorf("error checking network interfaces: %w", err)
	}
	if err := hc.CheckReachability(); err != nil {
		return fmt.Errorf("error checking network reachability: %w", err)
	}
	if err := hc.RemoveTaints(context.Background()); err != nil {
		return fmt.Errorf("error removing taint: %w", err)
	}
	return nil
}
