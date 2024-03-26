package reconciler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
	"github.com/telekom/das-schiff-network-operator/pkg/config"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	"github.com/telekom/das-schiff-network-operator/pkg/frr"
	"github.com/telekom/das-schiff-network-operator/pkg/healthcheck"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultDebounceTime = 20 * time.Second

type Reconciler struct {
	client         client.Client
	netlinkManager *nl.NetlinkManager
	frrManager     *frr.Manager
	anycastTracker *anycast.Tracker
	config         *config.Config
	logger         logr.Logger
	healthChecker  *healthcheck.HealthChecker

	debouncer *debounce.Debouncer

	dirtyFRRConfig bool
}

type reconcile struct {
	*Reconciler
	logr.Logger
}

func NewReconciler(clusterClient client.Client, anycastTracker *anycast.Tracker, logger logr.Logger) (*Reconciler, error) {
	reconciler := &Reconciler{
		client:         clusterClient,
		netlinkManager: &nl.NetlinkManager{},
		frrManager:     frr.NewFRRManager(),
		anycastTracker: anycastTracker,
		logger:         logger,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	if val := os.Getenv("FRR_CONFIG_FILE"); val != "" {
		reconciler.frrManager.ConfigPath = val
	}
	if err := reconciler.frrManager.Init(); err != nil {
		return nil, fmt.Errorf("error trying to init FRR Manager: %w", err)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}
	reconciler.config = cfg

	nc, err := healthcheck.LoadConfig(healthcheck.NetHealthcheckFile)
	if err != nil {
		return nil, fmt.Errorf("error loading networking healthcheck config: %w", err)
	}

	tcpDialer := healthcheck.NewTCPDialer(nc.Timeout)
	reconciler.healthChecker, err = healthcheck.NewHealthChecker(reconciler.client,
		healthcheck.NewDefaultHealthcheckToolkit(reconciler.frrManager, tcpDialer),
		nc)
	if err != nil {
		return nil, fmt.Errorf("error creating networking healthchecker: %w", err)
	}

	return reconciler, nil
}

func (reconciler *Reconciler) Reconcile(ctx context.Context) {
	reconciler.debouncer.Debounce(ctx)
}

func (reconciler *Reconciler) reconcileDebounced(ctx context.Context) error {
	r := &reconcile{
		Reconciler: reconciler,
		Logger:     reconciler.logger,
	}

	r.Logger.Info("Reloading config")
	if err := r.config.ReloadConfig(); err != nil {
		return fmt.Errorf("error reloading network-operator config: %w", err)
	}

	l3vnis, err := r.fetchLayer3(ctx)
	if err != nil {
		return err
	}
	l2vnis, err := r.fetchLayer2(ctx)
	if err != nil {
		return err
	}
	taas, err := r.fetchTaas(ctx)
	if err != nil {
		return err
	}

	if err := r.reconcileLayer3(l3vnis, taas); err != nil {
		return err
	}
	if err := r.reconcileLayer2(l2vnis); err != nil {
		return err
	}

	if !reconciler.healthChecker.IsNetworkingHealthy() {
		_, err := reconciler.healthChecker.IsFRRActive()
		if err != nil {
			return fmt.Errorf("error checking FRR status: %w", err)
		}
		if err = reconciler.healthChecker.CheckInterfaces(); err != nil {
			return fmt.Errorf("error checking network interfaces: %w", err)
		}
		if err = reconciler.healthChecker.CheckReachability(); err != nil {
			return fmt.Errorf("error checking network reachability: %w", err)
		}
		if err = reconciler.healthChecker.RemoveTaints(ctx); err != nil {
			return fmt.Errorf("error removing taint from the node: %w", err)
		}
	}

	return nil
}

type ConfigReconciler struct {
	client    client.Client
	logger    logr.Logger
	debouncer *debounce.Debouncer
}

type reconcileConfig struct {
	*ConfigReconciler
	logr.Logger
}

func (cr *ConfigReconciler) Reconcile(ctx context.Context) {
	cr.debouncer.Debounce(ctx)
}

func NewConfigReconciler(clusterClient client.Client, logger logr.Logger) (*ConfigReconciler, error) {
	reconciler := &ConfigReconciler{
		client: clusterClient,
		logger: logger,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (cr *ConfigReconciler) reconcileDebounced(ctx context.Context) error {
	cr.logger.Info("ConfigReconciler - reconcileDebounced")
	r := &reconcileConfig{
		ConfigReconciler: cr,
		Logger:           cr.logger,
	}

	cr.logger.Info("ConfigReconciler - fetchLayer3")
	l3vnis, err := r.fetchLayer3(ctx)
	if err != nil {
		return err
	}

	cr.logger.Info("ConfigReconciler - fetchLayer2")
	l2vnis, err := r.fetchLayer2(ctx)
	if err != nil {
		return err
	}

	cr.logger.Info("ConfigReconciler - fetchTass")
	taas, err := r.fetchTaas(ctx)
	if err != nil {
		return err
	}

	l2Spec := []v1alpha1.Layer2NetworkConfigurationSpec{}
	for _, v := range l2vnis {
		l2Spec = append(l2Spec, v.Spec)
	}

	l3Spec := []v1alpha1.VRFRouteConfigurationSpec{}
	for _, v := range l3vnis {
		l3Spec = append(l3Spec, v.Spec)
	}

	taasSpec := []v1alpha1.RoutingTableSpec{}
	for _, v := range taas {
		taasSpec = append(taasSpec, v.Spec)
	}

	nodes, err := cr.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	existingConfigs, err := cr.ListConfigs(ctx)
	if err != nil {
		return fmt.Errorf("error listing configs: %w", err)
	}

	newConfigs := map[string]*v1alpha1.NodeConfig{}

	for name := range *nodes {
		var c *v1alpha1.NodeConfig
		if _, exists := (*existingConfigs)[name]; exists {
			x := (*existingConfigs)[name]
			c = &x
			// c.Spec.Layer2 = l2Spec
			c.Spec.Vrf = l3Spec
			c.Spec.RoutingTable = taasSpec

			// err = cr.client.Update(ctx, c)
			// if err != nil {
			// 	cr.logger.Error(err, "error updateing NodeConfig object")
			// 	return err
			// }
		} else {
			c = &v1alpha1.NodeConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: name,
				},
				Status: v1alpha1.NodeConfigStatus{
					Status: "new",
				},
				Spec: v1alpha1.NodeConfigSpec{
					// Layer2:       l2Spec,
					Vrf:          l3Spec,
					RoutingTable: taasSpec,
				}}
			// err = cr.client.Create(ctx, c)
			// if err != nil {
			// 	cr.logger.Error(err, "error creating NodeConfig object")
			// 	return err
			// }
		}
		cr.logger.Info("NodeConfig", name, *c)
		newConfigs[name] = c
	}

	newL2Config := make(map[string][]*v1alpha1.Layer2NetworkConfigurationSpec)

	for _, l2 := range l2Spec {
		if l2.NodeSelector != nil {
			selector := labels.NewSelector()
			var reqs labels.Requirements

			for key, value := range l2.NodeSelector.MatchLabels {
				requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
				if err != nil {
					cr.logger.Error(err, "error creating MatchLabel requirement")
					return fmt.Errorf("error creating MatchLabel requirement: %w", err)
				}
				reqs = append(reqs, *requirement)
			}

			for _, req := range l2.NodeSelector.MatchExpressions {
				lowercaseOperator := selection.Operator(strings.ToLower(string(req.Operator)))
				requirement, err := labels.NewRequirement(req.Key, lowercaseOperator, req.Values)
				if err != nil {
					cr.logger.Error(err, "error creating MatchExpression requirement")
					return fmt.Errorf("error creating MatchExpression requirement: %w", err)
				}
				reqs = append(reqs, *requirement)
			}
			selector = selector.Add(reqs...)

			for name := range *nodes {
				if selector.Matches(labels.Set((*nodes)[name].ObjectMeta.Labels)) {
					cr.logger.Info("node does match nodeSelector of layer2", "node", name)
					newL2Config[name] = append(newL2Config[name], &l2)
				}
			}
		} else {
			for name := range *nodes {
				newL2Config[name] = append(newL2Config[name], &l2)
			}
		}
	}

	for name := range *nodes {
		newConfigs[name].Spec.Layer2 = []v1alpha1.Layer2NetworkConfigurationSpec{}
		for _, v := range newL2Config {
			for i := range v {
				newConfigs[name].Spec.Layer2 = append(newConfigs[name].Spec.Layer2, *v[i])
			}
		}
	}

	// for _, l2 := range l2Spec {
	// 	l2.NodeSelector
	// }

	return nil
}

func (cr *ConfigReconciler) ListNodes(ctx context.Context) (*map[string]corev1.Node, error) {
	list := &corev1.NodeList{}

	if err := cr.client.List(ctx, list); err != nil {
		return nil, err
	}

	nodes := make(map[string]corev1.Node)

	for i := range list.Items {
		if _, exists := list.Items[i].Labels["node-role.kubernetes.io/control-plane"]; !exists {
			nodes[list.Items[i].Name] = list.Items[i]
		}
	}

	return &nodes, nil
}

func (cr *ConfigReconciler) ListConfigs(ctx context.Context) (*map[string]v1alpha1.NodeConfig, error) {
	list := &v1alpha1.NodeConfigList{}
	if err := cr.client.List(ctx, list); err != nil {
		return nil, err
	}

	configs := make(map[string]v1alpha1.NodeConfig)

	for i := range list.Items {
		configs[list.Items[i].Name] = list.Items[i]
	}

	return &configs, nil
}
