package reconciler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	statusProvisioning = "provisioning"
	statusInvalid      = "invalid"
	statusProvisioned  = "provisioned"

	defaultTimeout      = 60 * time.Second
	defaultCooldownTime = 100 * time.Millisecond
)

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
	r := &reconcileConfig{
		ConfigReconciler: cr,
		Logger:           cr.logger,
	}

	cfg, err := r.fetchConfigData(ctx)
	if err != nil {
		return err
	}

	// list all nodes in the cluster
	nodes, err := cr.listNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	// list all NodeConfigs that were already deployed
	existingConfigs, err := cr.listConfigs(ctx)
	if err != nil {
		return fmt.Errorf("error listing configs: %w", err)
	}

	cr.logger.Info("existing configs", "configs", existingConfigs)

	// prepare map of NewConfigs (hostname is a map's key)
	// we simply add l3Spec and taasSpec to *all* configs, as
	// VRFRouteConfigurationSpec (l3Spec) and RoutingTableSpec (taasSpec)
	// are global respources (no node selctors are implemented) (Am I correct here?)
	newConfigs, err := preparePerNodeConfigs(nodes, existingConfigs, cfg)
	if err != nil {
		return fmt.Errorf("error preparing configs for nodes: %w", err)
	}

	cr.logger.Info("new configs", "configs", newConfigs)

	cr.logger.Info("ConfigReconciler - API query")
	// process new NodeConfigs one by one
	for name := range newConfigs {
		if err := cr.deployConfig(ctx, name, newConfigs, existingConfigs); err != nil {
			return fmt.Errorf("error deploying config: %w", err)
		}
	}

	cr.logger.Info("ConfigReconciler - success")
	return nil
}

func (cr *ConfigReconciler) listNodes(ctx context.Context) (map[string]corev1.Node, error) {
	// list all nodes
	list := &corev1.NodeList{}
	if err := cr.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("error listing nodes: %w", err)
	}

	// discard control-plane nodes and create map of nodes
	nodes := make(map[string]corev1.Node)
	for i := range list.Items {
		if _, exists := list.Items[i].Labels["node-role.kubernetes.io/control-plane"]; !exists {
			nodes[list.Items[i].Name] = list.Items[i]
		}
	}

	return nodes, nil
}

func (cr *ConfigReconciler) listConfigs(ctx context.Context) (map[string]v1alpha1.NodeConfig, error) {
	// list all node configs
	list := &v1alpha1.NodeConfigList{}
	if err := cr.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("error listing NodeConfigs: %w", err)
	}

	// create map of node configs
	configs := make(map[string]v1alpha1.NodeConfig)
	for i := range list.Items {
		configs[list.Items[i].Name] = list.Items[i]
	}

	return configs, nil
}

func (cr *ConfigReconciler) waitForConfigGet(ctx context.Context, instance *v1alpha1.NodeConfig, expectedStatus string, errCh chan error) {
	cr.logger.Info("wait for config", "name", instance.Name, "status", expectedStatus)
	for {
		select {
		case <-ctx.Done():
			cr.logger.Info("Context done")
			errCh <- fmt.Errorf("error while waiting for NodeConfig - context cancelled: %w", ctx.Err())
			return
		default:
			err := cr.client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance)
			if err == nil {
				// Accept any status
				cr.logger.Info("Get error is nil")
				if expectedStatus == "" {
					cr.logger.Info("Expecred statsu is empty - return nil error")
					errCh <- nil
					return
				}

				// accept only expected status
				if instance.Status.ConfigStatus == expectedStatus {
					cr.logger.Info("Expected statsus is set and fulfilled", "status", expectedStatus)
					errCh <- nil
					return
				}

				// return error if status is invalid
				if instance.Status.ConfigStatus == statusInvalid {
					errCh <- fmt.Errorf("error creating NodeConfig - node %s reported state as invalid", instance.Name)
					return
				}
			} else {
				cr.logger.Info("Get error", "err", err)
			}
			cr.logger.Info("waiting for config...")
			time.Sleep(defaultCooldownTime)
		}
	}
}

func (cr *ConfigReconciler) waitForConfig(ctx context.Context, timeout time.Duration, config *v1alpha1.NodeConfig, expectedStatus string) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	errCh := make(chan error)
	go cr.waitForConfigGet(ctxTimeout, config, expectedStatus, errCh)

	result := <-errCh
	if result != nil {
		return fmt.Errorf("error waiting for Node config: %w", result)
	}

	return nil
}

// removal function that preserves order (is it worth to preserve order?)
func remove[T any](slice []T, s int) []T {
	return append(slice[:s], slice[s+1:]...)
}

func (r *reconcileConfig) fetchConfigData(ctx context.Context) (*v1alpha1.NodeConfig, error) {
	// get VRFRouteConfiguration objects
	l3vnis, err := r.fetchLayer3(ctx)
	if err != nil {
		return nil, err
	}

	// get Layer2networkConfigurationObjects objects
	l2vnis, err := r.fetchLayer2(ctx)
	if err != nil {
		return nil, err
	}

	// get RoutingTable objects
	taas, err := r.fetchTaas(ctx)
	if err != nil {
		return nil, err
	}

	config := &v1alpha1.NodeConfig{}

	// discard metadata from previously fetched objects
	config.Spec.Layer2 = []v1alpha1.Layer2NetworkConfigurationSpec{}
	for i := range l2vnis {
		config.Spec.Layer2 = append(config.Spec.Layer2, l2vnis[i].Spec)
	}

	config.Spec.Vrf = []v1alpha1.VRFRouteConfigurationSpec{}
	for i := range l3vnis {
		config.Spec.Vrf = append(config.Spec.Vrf, l3vnis[i].Spec)
	}

	config.Spec.RoutingTable = []v1alpha1.RoutingTableSpec{}
	for i := range taas {
		config.Spec.RoutingTable = append(config.Spec.RoutingTable, taas[i].Spec)
	}

	return config, nil
}

func preparePerNodeConfigs(nodes map[string]corev1.Node, existingConfigs map[string]v1alpha1.NodeConfig, config *v1alpha1.NodeConfig) (map[string]*v1alpha1.NodeConfig, error) {
	newConfigs := map[string]*v1alpha1.NodeConfig{}
	for name := range nodes {
		var c *v1alpha1.NodeConfig
		if _, exists := existingConfigs[name]; exists {
			// config already exist - update (is this safe? Won't there be any leftovers in the config?)
			x := existingConfigs[name]
			c = &x
		} else {
			// config does not exist - create new
			c = &v1alpha1.NodeConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: name,
				},
			}
		}

		c.Spec.Vrf = config.Spec.Vrf
		c.Spec.RoutingTable = config.Spec.RoutingTable

		c.Spec.Layer2 = make([]v1alpha1.Layer2NetworkConfigurationSpec, len(config.Spec.Layer2))
		copy(c.Spec.Layer2, config.Spec.Layer2)

		// prepare Layer2NetworkConfigurationSpec (l2Spec) for each node.
		// Each Layer2NetworkConfigurationSpec from l2Spec has node selector,
		// which should be used to add config to proper nodes.
		// Each Layer2NetworkConfigurationSpec that don't match the node selector
		// is removed.
		for i := 0; i < len(c.Spec.Layer2); i++ {
			if c.Spec.Layer2[i].NodeSelector == nil {
				// node selector is not defined for the spec.
				// Layer2 is global - just continue
				continue
			}

			// node selector of type v1.labelSelector has to be converted
			// to labels.Selector type to be used with controller-runtime client
			selector, err := convertSelector(c.Spec.Layer2[i].NodeSelector.MatchLabels, c.Spec.Layer2[i].NodeSelector.MatchExpressions)
			if err != nil {
				return nil, fmt.Errorf("error converting selector: %w", err)
			}

			// remove currently processed Layer2NetworkConfigurationSpec if node does not match the selector
			if !selector.Matches(labels.Set(nodes[name].ObjectMeta.Labels)) {
				c.Spec.Layer2 = remove(c.Spec.Layer2, i)
				i--
			}
		}

		newConfigs[name] = c
	}

	return newConfigs, nil
}

func convertSelector(matchLabels map[string]string, matchExpressions []metav1.LabelSelectorRequirement) (labels.Selector, error) {
	selector := labels.NewSelector()
	var reqs labels.Requirements

	for key, value := range matchLabels {
		requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
		if err != nil {
			return nil, fmt.Errorf("error creating MatchLabel requirement: %w", err)
		}
		reqs = append(reqs, *requirement)
	}

	for _, req := range matchExpressions {
		lowercaseOperator := selection.Operator(strings.ToLower(string(req.Operator)))
		requirement, err := labels.NewRequirement(req.Key, lowercaseOperator, req.Values)
		if err != nil {
			return nil, fmt.Errorf("error creating MatchExpression requirement: %w", err)
		}
		reqs = append(reqs, *requirement)
	}
	selector = selector.Add(reqs...)

	return selector, nil
}

func (cr *ConfigReconciler) deployConfig(ctx context.Context, name string, newConfigs map[string]*v1alpha1.NodeConfig, existingConfigs map[string]v1alpha1.NodeConfig) error {
	cr.logger.Info("NodeConfig", name, *newConfigs[name])
	if _, exists := existingConfigs[name]; exists {
		// config already exists - update
		cr.logger.Info("ConfigReconciler - API query - update")

		// check if new config is equal to existing config
		// if so, skip the update as nothing has to be updated
		existing := existingConfigs[name]
		if newConfigs[name].IsEqual(&existing) {
			cr.logger.Info("ConfigReconciler - configs are equal")
			return nil
		}

		cr.logger.Info("ConfigReconciler - configs are not equal - provisoning will start")

		if err := cr.client.Update(ctx, newConfigs[name]); err != nil {
			cr.logger.Error(err, "error updating NodeConfig object")
			return fmt.Errorf("error updating NodeConfig object: %w", err)
		}
	} else {
		cr.logger.Info("ConfigReconciler - API query - create")
		// config does not exist - create
		if err := cr.client.Create(ctx, newConfigs[name]); err != nil {
			cr.logger.Error(err, "error creating NodeConfig object")
			return fmt.Errorf("error creating NodeConfig object: %w", err)
		}
	}

	// wait for config te be created
	// it also gets current instance from APIserver so we can update the status field
	// // TODO: check if this is required
	if err := cr.waitForConfig(ctx, defaultTimeout, newConfigs[name], ""); err != nil {
		return fmt.Errorf("error waiting for NodeConfig: %w", err)
	}

	// update the status with statusProvisioning
	newConfigs[name].Status.ConfigStatus = statusProvisioning
	err := cr.client.Status().Update(ctx, newConfigs[name])
	if err != nil {
		cr.logger.Error(err, "error creating NodeConfig status")
		return fmt.Errorf("error creating NodeConfig status: %w", err)
	}

	// wait for the node to update the status to 'provisioned' or 'invalid'
	// wait for config te be created
	if err := cr.waitForConfig(ctx, defaultTimeout, newConfigs[name], statusProvisioned); err != nil {
		return fmt.Errorf("error waiting for NodeConfig: %w", err)
	}

	return nil
}
