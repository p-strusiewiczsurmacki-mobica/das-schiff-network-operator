package reconciler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	"golang.org/x/sync/semaphore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	DefaultTimeout         = "60s"
	DefaultNodeUpdateLimit = 1
	defaultCooldownTime    = 100 * time.Millisecond

	invalidSuffix = "-invalid"
	backupSuffix  = "-backup"

	processName = "network-operator"
)

// ConfigReconciler is responsible for creating NodeConfig objects.
type ConfigReconciler struct {
	client    client.Client
	logger    logr.Logger
	debouncer *debounce.Debouncer
	timeout   time.Duration
	sem       *semaphore.Weighted

	process *v1alpha1.NodeConfigProcess

	OnLeaderElectionDone chan bool

	currentConfigs map[string]v1alpha1.NodeConfig
	invalidConfigs map[string]v1alpha1.NodeConfig
	backupConfigs  map[string]v1alpha1.NodeConfig
}

type reconcileConfig struct {
	*ConfigReconciler
	logr.Logger
}

// Reconcile starts reconciliation.
func (cr *ConfigReconciler) Reconcile(ctx context.Context) {
	cr.debouncer.Debounce(ctx)
}

// NewConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewConfigReconciler(clusterClient client.Client, logger logr.Logger, timeout string, limit int64) (*ConfigReconciler, error) {
	t, err := time.ParseDuration(timeout)
	if err != nil {
		return nil, fmt.Errorf("error parsing timeout %s: %w", timeout, err)
	}

	reconciler := &ConfigReconciler{
		client:         clusterClient,
		logger:         logger,
		timeout:        t,
		sem:            semaphore.NewWeighted(limit),
		currentConfigs: make(map[string]v1alpha1.NodeConfig),
		invalidConfigs: make(map[string]v1alpha1.NodeConfig),
		backupConfigs:  make(map[string]v1alpha1.NodeConfig),
		process: &v1alpha1.NodeConfigProcess{
			ObjectMeta: metav1.ObjectMeta{
				Name: processName,
			},
			Spec: v1alpha1.NodeConfigProcessSpec{
				State: "",
			},
		},
		OnLeaderElectionDone: make(chan bool),
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (cr *ConfigReconciler) reconcileDebounced(ctx context.Context) error {
	r := &reconcileConfig{
		ConfigReconciler: cr,
		Logger:           cr.logger,
	}

	// wait for OnLeaderElectionEvent runnable to finish
	<-cr.OnLeaderElectionDone

	// get all configuration objects
	cfg, err := r.fetchConfigData(ctx)
	if err != nil {
		return fmt.Errorf("error fetching configuration details: %w", err)
	}

	// list all nodes in the cluster
	nodes, err := cr.listNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	// get exisiting configs
	if err := cr.getConfigs(ctx); err != nil {
		return fmt.Errorf("error getting NodeConfigs from API server: %w", err)
	}

	// prepare map of NewConfigs (hostname is a map's key) - add l3Spec and taasSpec to *all* configs
	// as VRFRouteConfigurationSpec (l3Spec) and RoutingTableSpec (taasSpec)
	// are global resources (no node selectors are implemented) - TODO: Am I correct here?
	newConfigs, err := cr.preparePerNodeConfigs(nodes, cfg)
	if err != nil {
		return fmt.Errorf("error preparing configs for nodes: %w", err)
	}

	// check if no new configs result in known invalid config
	if err := cr.checkInvalidConfigs(newConfigs); err != nil {
		return fmt.Errorf("error checking for invalid configs: %w", err)
	}

	if err := cr.updateProcessState(ctx, statusProvisioning); err != nil {
		return fmt.Errorf("error updating provisioning global state with value '%s': %w", statusProvisioned, err)
	}

	// deploy new configs, revert changes if error occurred
	deployed, err := cr.processConfigs(ctx, newConfigs, true, true)
	if err != nil {
		if err := cr.revertChanges(ctx, deployed); err != nil {
			return fmt.Errorf("error reverting changes: %w", err)
		}
		return fmt.Errorf("error deploying config: %w", err)
	}

	if err := cr.updateProcessState(ctx, statusProvisioned); err != nil {
		return fmt.Errorf("error updating provisioning global state with value '%s': %w", statusProvisioned, err)
	}

	if len(deployed) > 0 {
		cr.logger.Info("successful deployment")
	}
	return nil
}

func getConfigsBySuffix(suffix string, configs map[string]v1alpha1.NodeConfig) map[string]v1alpha1.NodeConfig {
	cfg := make(map[string]v1alpha1.NodeConfig)
	for k := range configs {
		if strings.Contains(k, suffix) {
			newKey := strings.ReplaceAll(k, suffix, "")
			cfg[newKey] = configs[k]
			delete(configs, k)
		}
	}
	return cfg
}

// getConfigs gets currently depoyed NodeeConfigs and stores them in ConfigReconciler object.
func (cr *ConfigReconciler) getConfigs(ctx context.Context) error {
	existingConfigs, err := cr.listConfigs(ctx)
	if err != nil {
		return fmt.Errorf("error listing configs: %w", err)
	}

	// separate invalid configs, current configs and backups
	// this also removes invalid and backup configs from the existing configs
	cr.invalidConfigs = getConfigsBySuffix(invalidSuffix, existingConfigs)
	cr.backupConfigs = getConfigsBySuffix(backupSuffix, existingConfigs)
	cr.currentConfigs = existingConfigs

	return nil
}

// revertChanges restores backup NodeConfigs for selected nodes.
func (cr *ConfigReconciler) revertChanges(ctx context.Context, nodes []string) error {
	// refresh current configs
	existingConfigs, listingError := cr.listConfigs(ctx)
	if listingError != nil {
		return fmt.Errorf("error listing configs: %w", listingError)
	}

	cr.backupConfigs = getConfigsBySuffix(backupSuffix, existingConfigs)
	cr.invalidConfigs = getConfigsBySuffix(invalidSuffix, existingConfigs)
	cr.currentConfigs = existingConfigs

	// select what should be restored
	toRestore := cr.prepareBackups(nodes)

	// restore configs from backup
	// we dont want to backup invalid configs, therefore we are disabling backup
	if _, restoreErr := cr.processConfigs(ctx, toRestore, false, false); restoreErr != nil {
		return fmt.Errorf("error restoring configuration: %w", restoreErr)
	}

	return nil
}

func (cr *ConfigReconciler) checkInvalidConfigs(newConfigs map[string]*v1alpha1.NodeConfig) error {
	// check if no new configs result in known invalid config
	// if so, abort deployment
	for name := range newConfigs {
		if _, exists := cr.invalidConfigs[name]; exists {
			cfg := cr.invalidConfigs[name]
			// if any of new configs equals to known invalid config, abort deployment
			if newConfigs[name].IsEqual(&cfg) {
				return fmt.Errorf("values for node %s result in invalid config", name)
			}
		}
	}

	return nil
}

// For each config that should be restored find current config, and replace it's values with backup.
func (cr *ConfigReconciler) prepareBackups(toRestore []string) map[string]*v1alpha1.NodeConfig {
	filteredBackups := map[string]*v1alpha1.NodeConfig{}
	for _, name := range toRestore {
		existing := cr.currentConfigs[name]
		existing.Spec.Vrf = cr.backupConfigs[name].Spec.Vrf
		existing.Spec.RoutingTable = cr.backupConfigs[name].Spec.RoutingTable

		existing.Spec.Layer2 = make([]v1alpha1.Layer2NetworkConfigurationSpec, len(cr.backupConfigs[name].Spec.Layer2))
		copy(existing.Spec.Layer2, cr.backupConfigs[name].Spec.Layer2)

		filteredBackups[name] = &existing
	}
	return filteredBackups
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

func (cr *ConfigReconciler) GetProcessState(ctx context.Context) error {
	if err := cr.client.Get(ctx, client.ObjectKeyFromObject(cr.process), cr.process); err != nil {
		if apierrors.IsNotFound(err) {
			cr.process.Spec.State = ""
			if createErr := cr.client.Create(ctx, cr.process); createErr != nil {
				return fmt.Errorf("error creating NodeConfigProcess: %w", err)
			}
			return nil
		}
		return fmt.Errorf("error getting NodeConfigProcess: %w", err)
	}
	return nil
}

func (cr *ConfigReconciler) updateProcessState(ctx context.Context, state string) error {
	cr.process.Spec.State = state
	if err := cr.client.Update(ctx, cr.process); err != nil {
		return fmt.Errorf("error updateing NodeCOnfigProcess object: %w", err)
	}
	return nil
}

func (cr *ConfigReconciler) waitForConfigGet(ctx context.Context, instance *v1alpha1.NodeConfig, expectedStatus string, failIfInvalid bool) error {
	for {
		select {
		case <-ctx.Done():
			// return if context is done (e.g. cancelled)
			return fmt.Errorf("context error: %w", ctx.Err())
		default:
			err := cr.client.Get(ctx, types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}, instance)
			if err == nil {
				// Accept any status ("") or expected status
				if expectedStatus == "" || instance.Status.ConfigStatus == expectedStatus {
					return nil
				}

				// return error if status is invalid
				if failIfInvalid && instance.Status.ConfigStatus == statusInvalid {
					return fmt.Errorf("error creating NodeConfig - node %s reported state as %s", instance.Name, instance.Status.ConfigStatus)
				}
			}
			time.Sleep(defaultCooldownTime)
		}
	}
}

func (cr *ConfigReconciler) waitForConfig(ctx context.Context, config *v1alpha1.NodeConfig, expectedStatus string, failIfInvalid bool) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, cr.timeout)
	defer cancel()

	if err := cr.waitForConfigGet(ctxTimeout, config, expectedStatus, failIfInvalid); err != nil {
		return fmt.Errorf("error getting config: %w", err)
	}

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

func (cr *ConfigReconciler) preparePerNodeConfigs(nodes map[string]corev1.Node, config *v1alpha1.NodeConfig) (map[string]*v1alpha1.NodeConfig, error) {
	newConfigs := map[string]*v1alpha1.NodeConfig{}
	for name := range nodes {
		var c *v1alpha1.NodeConfig
		if _, exists := cr.currentConfigs[name]; exists {
			// config already exist - update (is this safe? Won't there be any leftovers in the config?)
			x := cr.currentConfigs[name]
			c = x.DeepCopy()
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
				// TODO: is it worth to preserve order?
				c.Spec.Layer2 = append(c.Spec.Layer2[:i], c.Spec.Layer2[i+1:]...)
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

// TODO: I don't like that newConfigs uses pointers and existingConfigs don't. Maybe this could be made consistent?
func (cr *ConfigReconciler) processConfig(ctx context.Context, cancel context.CancelFunc,
	wg *sync.WaitGroup, name string,
	newConfigs map[string]*v1alpha1.NodeConfig,
	processedNodes chan string, errCh chan error, backup bool, createObject bool) {
	defer wg.Done()

	// acquire the semaphore lock with weight 1
	if err := cr.sem.Acquire(ctx, 1); err != nil {
		sendError("error acquiring semaphore", err, errCh, cancel)
		return
	}
	defer cr.sem.Release(1)

	if err := ctx.Err(); err != nil {
		sendError("context eror", err, errCh, cancel)
		return
	}

	// deploy config
	// return if error occurred or it was not required to deokoy the config
	deployed, err := cr.deployConfig(ctx, newConfigs[name], backup)
	if err != nil || !deployed {
		errCh <- err
		return
	}

	// at his point CRD object was created/updated so we report config as deployed
	// this will be later used for reverting changes if any node reports an error
	processedNodes <- name

	// set the status to provisioning
	newConfigs[name].Status.ConfigStatus = statusProvisioning
	if err := cr.client.Status().Update(ctx, newConfigs[name]); err != nil {
		sendError("error creating NodeConfig status", err, errCh, cancel)
		return
	}

	// wait for status to be updated
	if err := cr.waitForConfig(ctx, newConfigs[name], statusProvisioning, false); err != nil {
		sendError(fmt.Sprintf("error waiting for NodeConfig status %s", statusProvisioning), err, errCh, cancel)
		return
	}

	// wait for the node to update the status to 'provisioned' or 'invalid'
	if err := cr.waitForConfig(ctx, newConfigs[name], statusProvisioned, true); err != nil {
		if err := cr.invalidateConfig(ctx, newConfigs[name], createObject); err != nil {
			sendError("error invalidating config", err, errCh, cancel)
			return
		}
		sendError(fmt.Sprintf("error waiting for NodeConfig status %s", statusProvisioned), err, errCh, cancel)
		return
	}

	// set new config as existing config
	cr.currentConfigs[name] = *newConfigs[name]

	cr.logger.Info("config deployed", "name", name, "status", newConfigs[name].Status.ConfigStatus)

	errCh <- nil
}

func (cr *ConfigReconciler) deployConfig(ctx context.Context, config *v1alpha1.NodeConfig,
	backup bool) (bool, error) {
	if backup {
		if err := cr.createBackup(ctx, config); err != nil {
			return false, fmt.Errorf("error creating backup config: %w", err)
		}
	}

	if _, exists := cr.currentConfigs[config.Name]; exists {
		// config already exists - update
		// check if new config is equal to existing config
		// if so, skip the update as nothing has to be updated
		existing := cr.currentConfigs[config.Name]
		if config.IsEqual(&existing) {
			return false, nil
		}
		if err := cr.client.Update(ctx, config); err != nil {
			return true, fmt.Errorf("error updating NodeConfig object: %w", err)
		}
	} else {
		// config does not exist - create
		if err := cr.client.Create(ctx, config); err != nil {
			return true, fmt.Errorf("error creating NodeConfig object: %w", err)
		}
	}

	return true, nil
}

func copyNodeConfig(src, dst *v1alpha1.NodeConfig, name string) {
	dst.Spec.Layer2 = make([]v1alpha1.Layer2NetworkConfigurationSpec, len(src.Spec.Layer2))
	dst.Spec.Vrf = make([]v1alpha1.VRFRouteConfigurationSpec, len(src.Spec.Vrf))
	dst.Spec.RoutingTable = make([]v1alpha1.RoutingTableSpec, len(src.Spec.RoutingTable))
	copy(dst.Spec.Layer2, src.Spec.Layer2)
	copy(dst.Spec.Vrf, src.Spec.Vrf)
	copy(dst.Spec.RoutingTable, src.Spec.RoutingTable)
	dst.Name = name
}

func (cr *ConfigReconciler) createBackup(ctx context.Context, config *v1alpha1.NodeConfig) error {
	backupName := config.Name + backupSuffix
	backup := &v1alpha1.NodeConfig{}

	createNew := false
	if err := cr.client.Get(ctx, types.NamespacedName{Name: backupName}, backup); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("error getting backup config %s: %w", backupName, err)
		}
		createNew = true
	}

	if exisitingCfg, exists := cr.currentConfigs[config.Name]; exists {
		copyNodeConfig(&exisitingCfg, backup, backupName)
	} else {
		copyNodeConfig(v1alpha1.NewEmptyConfig(backupName), backup, backupName)
	}

	if createNew {
		if err := cr.client.Create(ctx, backup); err != nil {
			return fmt.Errorf("error creating backup config: %w", err)
		}
	} else {
		if err := cr.client.Update(ctx, backup); err != nil {
			return fmt.Errorf("error updating backup config: %w", err)
		}
	}

	return nil
}

func sendError(text string, err error, errCh chan error, cancel context.CancelFunc) {
	errCh <- fmt.Errorf("%s: %w", text, err)
	cancel()
}

// Creates invalid config object named <nodename>-invalid.
func (cr *ConfigReconciler) createInvalidConfig(ctx context.Context, configToInvalidate *v1alpha1.NodeConfig) error {
	invalidName := fmt.Sprintf("%s%s", configToInvalidate.Name, invalidSuffix)
	invalidConfig := v1alpha1.NodeConfig{}

	if err := cr.client.Get(ctx, types.NamespacedName{Name: invalidName, Namespace: configToInvalidate.Namespace}, &invalidConfig); err != nil {
		if apierrors.IsNotFound(err) {
			// invalid config for the node does not exist - create new
			copyNodeConfig(configToInvalidate, &invalidConfig, invalidName)
			if err = cr.client.Create(ctx, &invalidConfig); err != nil {
				return fmt.Errorf("cannot store invalid config for node %s: %w", configToInvalidate.Name, err)
			}
			return nil
		}
		// other kind of error occurred - abort
		return fmt.Errorf("error getting invalid config for node %s: %w", configToInvalidate.Name, err)
	}

	// invalid config for the node exist - update
	copyNodeConfig(configToInvalidate, &invalidConfig, invalidName)
	if err := cr.client.Update(ctx, &invalidConfig); err != nil {
		return fmt.Errorf("error updating invalid config for node %s: %w", configToInvalidate.Name, err)
	}

	return nil
}

func (cr *ConfigReconciler) invalidateConfig(ctx context.Context, config *v1alpha1.NodeConfig, createObject bool) error {
	// if cannot get config status or status is 'provisoning' and request timed out
	// let's assume that the node died and was unable to invalidate config
	// so we will do that here instead
	if config.Status.ConfigStatus == statusProvisioning {
		config.Status.ConfigStatus = statusInvalid
		if err := cr.client.Status().Update(ctx, config); err != nil {
			return fmt.Errorf("error updating config: %w", err)
		}
	}

	if createObject {
		// create invalid config object that will be later used to prevent configurator from redeploying invalid config
		if err := cr.createInvalidConfig(ctx, config); err != nil {
			return fmt.Errorf("error creating invalid config: %w", err)
		}
	}

	return nil
}

func (cr *ConfigReconciler) processConfigs(ctx context.Context,
	newConfigs map[string]*v1alpha1.NodeConfig, backup bool, createObject bool) ([]string, error) {
	// process new NodeConfigs one by one
	deployCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	deploymentErr := make(chan error, len(newConfigs))
	deployedNodes := make(chan string, len(newConfigs))
	for name := range newConfigs {
		wg.Add(1)
		go cr.processConfig(deployCtx, cancel, &wg, name, newConfigs, deployedNodes, deploymentErr, backup, createObject)
	}

	wg.Wait()
	close(deploymentErr)
	close(deployedNodes)

	errorsOccurred := false
	for err := range deploymentErr {
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				cr.logger.Error(err, "error deploying config")
			}
			errorsOccurred = true
		}
	}

	deployed := []string{}
	for node := range deployedNodes {
		deployed = append(deployed, node)
	}

	if errorsOccurred {
		return deployed, fmt.Errorf("errors occurred while deploying config")
	}

	return deployed, nil
}

func (cr *ConfigReconciler) ValidateFormerLeader(ctx context.Context) error {
	if cr.process.Spec.State == statusProvisioning {
		cr.logger.Info("previous leader did not finish configuration - reverting changes")
		// get exisiting configs
		if err := cr.getConfigs(ctx); err != nil {
			return fmt.Errorf("error getting NodeConfigs from API server: %w", err)
		}
		nodes := []string{}
		for name := range cr.backupConfigs {
			nodes = append(nodes, name)
		}
		if err := cr.revertChanges(ctx, nodes); err != nil {
			return fmt.Errorf("error restoring backup NodeConfigs: %w", err)
		}
	}

	return nil
}
