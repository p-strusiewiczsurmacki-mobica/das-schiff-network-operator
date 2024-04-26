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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultTimeout         = "60s"
	DefaultNodeUpdateLimit = 1
)

// ConfigReconciler is responsible for creating NodeConfig objects.
type ConfigReconciler struct {
	globalCfg *v1alpha1.NodeConfig
	logger    logr.Logger
	debouncer *debounce.Debouncer
	client    client.Client
	timeout   time.Duration

	configManagerInform chan bool
}

type reconcileConfig struct {
	*ConfigReconciler
	logr.Logger
}

// Reconcile starts reconciliation.
func (cr *ConfigReconciler) Reconcile(ctx context.Context) {
	cr.debouncer.Debounce(ctx)
}

// func (cr *ConfigReconciler) InjectNodeReconciler(nr *NodeReconciler) {
// 	cr.nodeReconciler = nr
// }

// // NewConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewConfigReconciler(clusterClient client.Client, logger logr.Logger, timeout time.Duration, cmInfo chan bool) (*ConfigReconciler, error) {
	reconciler := &ConfigReconciler{
		logger:              logger,
		timeout:             timeout,
		client:              clusterClient,
		configManagerInform: cmInfo,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (cr *ConfigReconciler) reconcileDebounced(ctx context.Context) error {
	r := &reconcileConfig{
		ConfigReconciler: cr,
		Logger:           cr.logger,
	}

	cr.logger.Info("fetching config data")

	timeoutCtx, cancel := context.WithTimeout(ctx, cr.timeout)
	defer cancel()

	// get all configuration objects
	var err error
	cr.globalCfg, err = r.fetchConfigData(timeoutCtx)
	if err != nil {
		return fmt.Errorf("error fetching configuration details: %w", err)
	}

	// inform config manager that it should update
	cr.configManagerInform <- true

	cr.logger.Info("global config updated", "config", *cr.globalCfg)
	return nil
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

func (cr *ConfigReconciler) CreateConfigForNode(name string, node *corev1.Node) (*v1alpha1.NodeConfig, error) {
	// create new config
	c := &v1alpha1.NodeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if cr.globalCfg == nil {
		cr.globalCfg = v1alpha1.NewEmptyConfig(name)
	}

	v1alpha1.CopyNodeConfig(cr.globalCfg, c, name)

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
		if !selector.Matches(labels.Set(node.ObjectMeta.Labels)) {
			// TODO: is it worth to preserve order?
			c.Spec.Layer2 = append(c.Spec.Layer2[:i], c.Spec.Layer2[i+1:]...)
			i--
		}
	}

	cr.logger.Info("config for node", "node", name, "config", *c)

	// set config as next config for the node
	return c, nil
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

// func (cr *ConfigReconciler) CreateConfigs(names []string) error {
// 	for _, name := range names {
// 		if err := cr.createConfig(name); err != nil {
// 			return fmt.Errorf("error adding config for node %s: %w", name, err)
// 		}

// 		// if deployment is ongoing and it's not revert - add config to the end of the queue
// 		if cr.processing.Load() && !cr.reverting.Load() {
// 			cr.configsRW.RLock()
// 			cr.toDeploy.PushBack(cr.configs[name])
// 			cr.configsRW.RUnlock()
// 		}
// 	}
// 	return nil
// }

// func (cr *ConfigReconciler) ValidateFormerLeader(ctx context.Context) error {
// 	if err := cr.getProcessState(ctx); err != nil {
// 		return fmt.Errorf("error while getting NodeConfig process object: %w", err)
// 	}

// 	if cr.process.Spec.State == statusProvisioning {
// 		cr.logger.Info("previous leader did not finish configuration - reverting changes")
// 		// get exisiting configs
// 		if err := cr.getConfigs(ctx); err != nil {
// 			return fmt.Errorf("error getting NodeConfigs from API server: %w", err)
// 		}
// 		nodes := []string{}
// 		cr.configsRW.RLock()
// 		for _, config := range cr.configs {
// 			if config.backup != nil {
// 				nodes = append(nodes, config.name)
// 			}
// 		}
// 		cr.configsRW.RUnlock()
// 		cr.logger.Info("nodes to revert", "nodes", nodes)
// 		if err := cr.revertChanges(ctx, nodes); err != nil {
// 			return fmt.Errorf("error restoring backup NodeConfigs: %w", err)
// 		}
// 		cr.logger.Info("reverted chenges after leader change")
// 	}

// 	cr.logger.Info("validation finished")

// 	return nil
// }

// func (cr *ConfigReconciler) createConfig(name string) error {
// 	cr.configsRW.Lock()
// 	defer cr.configsRW.Unlock()
// 	if _, exist := cr.configs[name]; !exist {
// 		config, err := cr.CreateConfigForNode(name)
// 		if err != nil {
// 			return fmt.Errorf("error adding config for node: %w", err)
// 		}
// 		cr.configs[name] = newEmptyNodeConfiguration(name)
// 		cr.configs[name].next = config
// 	}
// 	return nil
// }

// func getConfigsBySuffix(suffix string, configs map[string]v1alpha1.NodeConfig) map[string]v1alpha1.NodeConfig {
// 	cfg := make(map[string]v1alpha1.NodeConfig)
// 	for k := range configs {
// 		if strings.Contains(k, suffix) {
// 			newKey := strings.ReplaceAll(k, suffix, "")
// 			cfg[newKey] = configs[k]
// 			delete(configs, k)
// 		}
// 	}
// 	return cfg
// }

// // getConfigs gets currently deployed NodeConfigs and stores them in ConfigReconciler object.
// func (cr *ConfigReconciler) getConfigs(ctx context.Context) error {
// 	cr.logger.Info("getting configs")
// 	existingConfigs, err := cr.listConfigs(ctx)
// 	if err != nil {
// 		return fmt.Errorf("error listing configs: %w", err)
// 	}

// 	// separate invalid configs, current configs and backups
// 	// this also removes invalid and backup configs from the existing configs
// 	invalidConfigs := getConfigsBySuffix(invalidSuffix, existingConfigs)
// 	backupConfigs := getConfigsBySuffix(backupSuffix, existingConfigs)
// 	currentConfigs := existingConfigs

// 	for name := range currentConfigs {
// 		var current *v1alpha1.NodeConfig
// 		var backup *v1alpha1.NodeConfig
// 		var invalid *v1alpha1.NodeConfig

// 		cfg := currentConfigs[name]
// 		current = &cfg

// 		if cfg, exists := backupConfigs[name]; exists {
// 			backup = &cfg
// 		}

// 		if cfg, exists := invalidConfigs[name]; exists {
// 			invalid = &cfg
// 		}

// 		cr.configsRW.Lock()
// 		if config, exists := cr.configs[name]; exists {
// 			config.update(current, backup, invalid)
// 		} else {
// 			cr.configs[name] = newNodeConfiguration(name, current, backup, invalid)
// 		}
// 		cr.configsRW.Unlock()
// 	}

// 	cr.logger.Info("configs", "current", len(currentConfigs), "backup", len(backupConfigs), "invalid", len(invalidConfigs), "CONFIGS", len(cr.configs))

// 	return nil
// }

// // revertChanges restores backup NodeConfigs for selected nodes.
// func (cr *ConfigReconciler) revertChanges(ctx context.Context, nodes []string) error {
// 	cr.reverting.Store(true)
// 	// select what should be restored
// 	cr.prepareBackups(nodes)

// 	// restore configs from backup
// 	// we dont want to backup invalid configs, therefore we are disabling backup
// 	if _, restoreErr := cr.processConfigs(ctx, false, false); restoreErr != nil {
// 		return fmt.Errorf("error restoring configuration: %w", restoreErr)
// 	}

// 	cr.reverting.Store(false)

// 	return nil
// }

// func (cr *ConfigReconciler) checkInvalidConfigs() error {
// 	// check if no new configs result in known invalid config
// 	// if so, abort deployment
// 	cr.configsRW.RLock()
// 	defer cr.configsRW.RUnlock()
// 	for _, cfg := range cr.configs {
// 		if cfg.invalid != nil && cfg.next != nil {
// 			// if any of new configs equals to known invalid config, abort deployment
// 			if cfg.next.IsEqual(cfg.invalid) {
// 				return fmt.Errorf("values for node %s result in invalid config", cfg.name)
// 			}
// 		}
// 	}

// 	return nil
// }

// // For each config that should be restored set next config to backup value.
// func (cr *ConfigReconciler) prepareBackups(toRestore []string) {
// 	cr.toDeploy.Clear()
// 	cr.configsRW.RLock()
// 	defer cr.configsRW.RUnlock()

// 	for _, name := range toRestore {
// 		if cfg, exists := cr.configs[name]; exists {
// 			if cfg.next != nil {
// 				v1alpha1.CopyNodeConfig(cfg.backup, cfg.next, name)
// 			} else {
// 				cfg.next = &v1alpha1.NodeConfig{}
// 				v1alpha1.CopyNodeConfig(cfg.backup, cfg.next, name)
// 			}
// 		}
// 		cr.toDeploy.PushBack(cr.configs[name])
// 	}
// }

// func (cr *ConfigReconciler) listConfigs(ctx context.Context) (map[string]v1alpha1.NodeConfig, error) {
// 	// cfgList all node configs
// 	cfgList := &v1alpha1.NodeConfigList{}
// 	if err := cr.client.List(ctx, cfgList); err != nil {
// 		return nil, fmt.Errorf("error listing NodeConfigs: %w", err)
// 	}

// 	// create map of node configs
// 	configs := make(map[string]v1alpha1.NodeConfig)
// 	for i := range cfgList.Items {
// 		configs[cfgList.Items[i].Name] = cfgList.Items[i]
// 	}

// 	return configs, nil
// }

// func (cr *ConfigReconciler) getProcessState(ctx context.Context) error {
// 	if err := cr.client.Get(ctx, client.ObjectKeyFromObject(cr.process), cr.process); err != nil {
// 		if apierrors.IsNotFound(err) {
// 			cr.process.Spec.State = ""
// 			if createErr := cr.client.Create(ctx, cr.process); createErr != nil {
// 				return fmt.Errorf("error creating NodeConfigProcess: %w", err)
// 			}
// 			return nil
// 		}
// 		return fmt.Errorf("error getting NodeConfigProcess: %w", err)
// 	}
// 	return nil
// }

// func (cr *ConfigReconciler) updateProcessState(ctx context.Context, state string) error {
// 	cr.process.Spec.State = state
// 	if err := cr.client.Update(ctx, cr.process); err != nil {
// 		return fmt.Errorf("error updateing NodeCOnfigProcess object: %w", err)
// 	}
// 	return nil
// }

// func (cr *ConfigReconciler) waitForConfigGet(ctx context.Context, instance *v1alpha1.NodeConfig, expectedStatus string, failIfInvalid bool) error {
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			// return if context is done (e.g. cancelled)
// 			return fmt.Errorf("context error: %w", ctx.Err())
// 		default:
// 			err := cr.client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance)
// 			if err == nil {
// 				// Accept any status ("") or expected status
// 				if expectedStatus == "" || instance.Status.ConfigStatus == expectedStatus {
// 					return nil
// 				}

// 				// return error if status is invalid
// 				if failIfInvalid && instance.Status.ConfigStatus == statusInvalid {
// 					return fmt.Errorf("error creating NodeConfig - node %s reported state as %s", instance.Name, instance.Status.ConfigStatus)
// 				}
// 			}
// 			time.Sleep(defaultCooldownTime)
// 		}
// 	}
// }

// func (cr *ConfigReconciler) waitForConfig(ctx context.Context, config *v1alpha1.NodeConfig, expectedStatus string, failIfInvalid bool) error {
// 	ctxTimeout, cancel := context.WithTimeout(ctx, cr.timeout)
// 	defer cancel()

// 	if err := cr.waitForConfigGet(ctxTimeout, config, expectedStatus, failIfInvalid); err != nil {
// 		return fmt.Errorf("error getting config: %w", err)
// 	}

// 	return nil
// }

// func (cr *ConfigReconciler) preparePerNodeConfigs() error {
// 	cr.configsRW.Lock()
// 	defer cr.configsRW.Unlock()
// 	for name := range cr.configs {
// 		config, err := cr.CreateConfigForNode(name)
// 		if err != nil {
// 			return fmt.Errorf("error creating config: %w", err)
// 		}
// 		cr.configs[name].next = config
// 	}

// 	return nil
// }

// func (cr *ConfigReconciler) processConfig(ctx context.Context, parentCancel context.CancelFunc, config *nodeConfiguration, backup, createObject bool) {
// 	// acquire the semaphore lock with weight 1
// 	if err := cr.sem.Acquire(ctx, 1); err != nil {
// 		saveError(config, "error acquiring semaphore", err, parentCancel)
// 		return
// 	}
// 	defer cr.sem.Release(1)

// 	cr.logger.Info("semaphore acquired", "config", config.name)

// 	if err := ctx.Err(); err != nil {
// 		saveError(config, "context eror", err, parentCancel)
// 		return
// 	}

// 	cr.logger.Info("no context error yet", "config", config.name)

// 	cr.logger.Info("will deploy config", "config", config.name)
// 	// deploy config - return if error occurred or it was not required to deploy the config
// 	deployed, err := cr.deployConfig(ctx, config, backup)
// 	if err != nil || !deployed {
// 		if err != nil {
// 			cr.logger.Info("error deploying config", "config", config.name, "error", err)
// 		} else {
// 			cr.logger.Info("config not dpeloyed (err nil)", "config", config.name)
// 		}

// 		config.lastError = err
// 		cr.logger.Info("lastErr saved in cofnig", "config", config.name)
// 		return
// 	}

// 	cr.logger.Info("add to deployed queue", "config", config.name)
// 	// at his point CRD object was created/updated so we report config as deployed
// 	// this will be later used for reverting changes if any node reports an error
// 	cr.deployed.PushBack(config)

// 	cr.logger.Info("wait for status", "config", config.name, "status", statusEmpty)
// 	// wait for status to be updated
// 	if err := cr.waitForConfig(ctx, config.current, statusEmpty, false); err != nil {
// 		cr.logger.Info("error waiting for NodeConfig status (saved))", "config", config.name, "Status", statusEmpty, "error", err)
// 		saveError(config, fmt.Sprintf("error waiting for NodeConfig status %s", statusEmpty), err, parentCancel)
// 		return
// 	}

// 	cr.logger.Info("get current version", "config", config.name)
// 	if err := cr.client.Get(ctx, client.ObjectKeyFromObject(config.current), config.current); err != nil {
// 		cr.logger.Info("error getting NodeConfig(saved))", "config", config.name, "error", err)
// 		saveError(config, "error getting NodeConfig", err, parentCancel)
// 		return
// 	}

// 	cr.logger.Info("update status", "config", config.name, "status", statusProvisioning)
// 	if err := cr.updateStatusWithTimeout(ctx, config.current, statusProvisioning); err != nil {
// 		cr.logger.Info("error updating NodeConfig status (saved)", "config", config.name, "error", err)
// 		saveError(config, "error updating NodeConfig status", err, parentCancel)
// 		return
// 	}

// 	cr.logger.Info("wait for status", "config", config.name, "status", statusProvisioning)
// 	// wait for status to be updated
// 	if err := cr.waitForConfig(ctx, config.current, statusProvisioning, false); err != nil {
// 		cr.logger.Info("error waiting for NodeConfig status (saved))", "config", config.name, "Status", statusProvisioning, "error", err)
// 		saveError(config, fmt.Sprintf("error waiting for NodeConfig status %s", statusProvisioning), err, parentCancel)
// 		return
// 	}

// 	cr.logger.Info("wait for status", "config", config.name, "status", statusProvisioned)
// 	// wait for the node to update the status to 'provisioned' or 'invalid'
// 	if err := cr.waitForConfig(ctx, config.current, statusProvisioned, true); err != nil {
// 		cr.logger.Info("error waiting for status", "config", config.name, "status", statusProvisioned, "error", err)
// 		cr.logger.Info("check if node still exists in nodeReconciler", "config", config.name)
// 		nodeExists := cr.nodeReconciler.CheckIfNodeExists(config.name)
// 		cr.logger.Info("check if node still exists in nodeReconciler", "config", config.name)
// 		if !nodeExists {
// 			cr.logger.Info("seems that node was deleted during the configuration process, ignoring...", "nondename", config.name)
// 			return
// 		}
// 		cr.logger.Info("invalidate config", "config", config.name)
// 		if err := cr.invalidateConfig(ctx, config, createObject); err != nil {
// 			cr.logger.Info("error invalidating config (saved)", "config", config.name)
// 			saveError(config, "error invalidating config", err, parentCancel)
// 			return
// 		}
// 		cr.logger.Info("error waiting for NodeConfig status (saved)", "config", config.name, "Status", statusProvisioned, "error", err)
// 		saveError(config, fmt.Sprintf("error waiting for NodeConfig status %s", statusProvisioned), err, parentCancel)
// 		return
// 	}

// 	cr.logger.Info("config deployed", "name", config.name, "status", config.current.Status.ConfigStatus)
// }

// func (cr *ConfigReconciler) deployConfig(ctx context.Context, config *nodeConfiguration,
// 	backup bool) (bool, error) {
// 	nodeExists := cr.nodeReconciler.CheckIfNodeExists(config.name)
// 	if !nodeExists {
// 		// node was deleted - nothing to do
// 		cr.logger.Info("seems that node was deleted during the configuration process, ignoring...", "nondename", config.name)
// 		return false, nil
// 	}

// 	if backup {
// 		cr.logger.Info("create backup")
// 		if err := config.createBackup(ctx, cr.client); err != nil {
// 			return false, fmt.Errorf("error creating backup config: %w", err)
// 		}
// 	}
// 	cr.logger.Info("deploy", "node", config.name)
// 	return config.deploy(ctx, cr.client)
// }

// func (cr *ConfigReconciler) invalidateConfig(ctx context.Context, config *nodeConfiguration, createObject bool) error {
// 	// if cannot get config status or status is 'provisoning' and request timed out
// 	// let's assume that the node died and was unable to invalidate config
// 	// so we will do that here instead
// 	if config.current.Status.ConfigStatus == statusProvisioning {
// 		ctxTimeout, cancel := context.WithTimeout(ctx, cr.timeout)
// 		defer cancel()
// 		if err := cr.updateStatusWithTimeout(ctxTimeout, config.current, statusInvalid); err != nil {
// 			return fmt.Errorf("error updating config: %w", err)
// 		}
// 	}

// 	if createObject {
// 		// create invalid config object that will be later used to prevent configurator from redeploying invalid config
// 		if err := config.crateInvalid(ctx, cr.client); err != nil {
// 			return fmt.Errorf("error creating invalid config: %w", err)
// 		}
// 	}

// 	return nil
// }

// func (cr *ConfigReconciler) processConfigs(ctx context.Context,
// 	backup bool, createObject bool) ([]string, error) {
// 	// process new NodeConfigs one by one
// 	deploymentCtx, cancel := context.WithTimeout(ctx, cr.timeout)
// 	defer cancel()

// 	cr.deployed.Clear()

// 	var wg sync.WaitGroup

// 	for it := cr.toDeploy.Front(); it != nil; it = it.Next() {
// 		cfg, ok := it.Value.(*nodeConfiguration)
// 		if !ok {
// 			return nil, fmt.Errorf("error converting list element to NodeConfig")
// 		}
// 		cr.logger.Info("deploying", "config", cfg.name)
// 		wg.Add(1)
// 		go func(config *nodeConfiguration) {
// 			defer wg.Done()
// 			if !cr.nodeReconciler.CheckIfNodeExists(config.name) || !config.active.Load() {
// 				cr.logger.Info("node does not exist - skip", "node", config.name)
// 				return
// 			}
// 			configCtx, configCancel := context.WithTimeout(deploymentCtx, cr.timeout)
// 			config.ctxCancel = configCancel
// 			cr.logger.Info("created child context configCtx", "node", config.name)
// 			defer func() {
// 				if config != nil && config.ctxCancel != nil {
// 					config.ctxCancel()
// 					config.ctxCancel = nil
// 					cr.logger.Info("chld config cancelFunc = nil")
// 				}

// 			}()
// 			cr.logger.Info("starting dployment of", "config", cfg.name)
// 			cr.processConfig(configCtx, cancel, config, backup, createObject)
// 		}(cfg)
// 	}

// 	cr.logger.Info("wait for updates")

// 	wg.Wait()

// 	cr.logger.Info("all done")

// 	errorsOccurred := false
// 	deployed := []string{}
// 	for node := cr.deployed.Front(); node != nil; node = node.Next() {
// 		cr.logger.Info("looping through nodes... converting...")
// 		v, ok := node.Value.(*nodeConfiguration)
// 		if !ok {
// 			cr.logger.Info("error converting interface")
// 			return nil, fmt.Errorf("error converting interface to nodeConfiguration pointer")
// 		}
// 		cr.logger.Info("converted good")
// 		cr.logger.Info("porcessing", "config", v.name)
// 		if !v.active.Load() {
// 			cr.logger.Info("node is inactive", "node", v.name)
// 			continue
// 		}
// 		if v.lastError != nil && !errors.Is(v.lastError, context.Canceled) {
// 			cr.logger.Error(v.lastError, "error deploying config")
// 			errorsOccurred = true
// 		}
// 		cr.logger.Info("yeah append")
// 		deployed = append(deployed, v.name)
// 	}

// 	cr.logger.Info("deployed", "nodes", deployed)

// 	if errorsOccurred {
// 		cr.logger.Info("errors occurred")
// 		return deployed, fmt.Errorf("errors occurred while deploying configs")
// 	}

// 	cr.logger.Info("deployed", "nodes", deployed)

// 	return deployed, nil
// }

// func (cr *ConfigReconciler) updateStatusWithTimeout(ctx context.Context, config *v1alpha1.NodeConfig, status string) error {
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return fmt.Errorf("status update error: %w", ctx.Err())
// 		default:
// 			config.Status.ConfigStatus = status
// 			err := cr.client.Status().Update(ctx, config)
// 			if err != nil {
// 				if apierrors.IsConflict(err) {
// 					// if there is a conflict, update local copy of the config
// 					if getErr := cr.client.Get(ctx, client.ObjectKeyFromObject(config), config); getErr != nil {
// 						return fmt.Errorf("error updating status: %w", getErr)
// 					}
// 					time.Sleep(defaultCooldownTime)
// 					continue
// 				}
// 				return fmt.Errorf("status update error: %w", err)
// 			} else {
// 				return nil
// 			}
// 		}
// 	}
// }

// func saveError(config *nodeConfiguration, text string, err error, cancel context.CancelFunc) {
// 	config.saveError(text, err)
// 	cancel()
// }

// func (cr *ConfigReconciler) DeleteConfig(name string) error {
// 	cr.configsRW.Lock()
// 	defer cr.configsRW.Unlock()
// 	if config, exist := cr.configs[name]; exist {
// 		config.active.Store(false)
// 		delete(cr.configs, name)
// 		if cr.processing.Load() {
// 			if config.ctxCancel != nil {
// 				config.ctxCancel()
// 			}
// 			err := cr.toDeploy.Remove(name)
// 			if err != nil {
// 				return fmt.Errorf("error removing value from toDeploy queue: %w", err)
// 			}
// 			err = cr.deployed.Remove(name)
// 			if err != nil {
// 				return fmt.Errorf("error removing value from deployed queue: %w", err)
// 			}
// 		}
// 	}
// 	return nil
// }

// func (cr *ConfigReconciler) updateToDeployQueue() {
// 	for _, cfg := range cr.configs {
// 		if cfg.next != nil {
// 			cr.toDeploy.PushBack(cfg)
// 		}
// 	}
// }
