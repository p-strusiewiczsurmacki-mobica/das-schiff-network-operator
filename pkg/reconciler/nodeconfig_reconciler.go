package reconciler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	StatusInvalid      = "invalid"
	StatusProvisioning = "provisioning"
	StatusProvisioned  = "provisioned"

	controlPlaneLabel = "node-role.kubernetes.io/control-plane"
	numOfRefs         = 2
	configTimeout     = time.Minute * 2
)

// NodeConfigReconciler is responsible for creating NodeConfig objects.
type NodeConfigReconciler struct {
	logger      logr.Logger
	debouncer   *debounce.Debouncer
	client      client.Client
	timeout     time.Duration
	scheme      *runtime.Scheme
	maxUpdating int
}

// Reconcile starts reconciliation.
func (ncr *NodeConfigReconciler) Reconcile(ctx context.Context) {
	ncr.debouncer.Debounce(ctx)
}

// // NewNodeConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewNodeConfigReconciler(clusterClient client.Client, logger logr.Logger, timeout time.Duration, s *runtime.Scheme, maxUpdating int) (*NodeConfigReconciler, error) {
	reconciler := &NodeConfigReconciler{
		logger:      logger,
		timeout:     timeout,
		client:      clusterClient,
		scheme:      s,
		maxUpdating: maxUpdating,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (ncr *NodeConfigReconciler) reconcileDebounced(ctx context.Context) error {
	revisions, err := listRevisions(ctx, ncr.client)
	if err != nil {
		return fmt.Errorf("error listing revisions: %w", err)
	}

	revisionToDeploy := getFirstValidRevision(revisions.Items)

	// there is nothing to deploy - skip
	if revisionToDeploy == nil {
		return nil
	}

	nodes, err := listNodes(ctx, ncr.client)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	nodeConfigs, err := ncr.listConfigs(ctx)
	if err != nil {
		return fmt.Errorf("error listing configs: %w", err)
	}

	available := len(nodes)
	shouldExit, ready, ongoing, err := ncr.processConfigs(ctx, nodeConfigs.Items, revisionToDeploy)
	if err != nil {
		return fmt.Errorf("error processing configs: %w", err)
	}
	if shouldExit {
		return nil
	}

	nodesToDeploy := getNodesToDeploy(nodes, nodeConfigs.Items, revisionToDeploy)

	if err := ncr.updateRevisionCounters(ctx, revisionToDeploy, available, ready, ongoing); err != nil {
		return fmt.Errorf("error updating revision %s counters: %w", revisionToDeploy.Name, err)
	}

	if revisionToDeploy.Status.Ongoing < ncr.maxUpdating && len(nodesToDeploy) > 0 {
		if err := ncr.deployNodeConfig(ctx, nodesToDeploy[0], revisionToDeploy); err != nil {
			return fmt.Errorf("error deploying node configurations: %w", err)
		}
	}

	// remove all but last known valid revision
	if err := ncr.revisionCleanup(ctx); err != nil {
		return fmt.Errorf("error cleaning redundant revisions: %w", err)
	}

	return nil
}

func getFirstValidRevision(revisions []v1alpha1.NetworkConfigRevision) *v1alpha1.NetworkConfigRevision {
	for i := range revisions {
		if revisions[i].Status.IsInvalid {
			continue
		}
		return &revisions[i]
	}
	return nil
}

func (ncr *NodeConfigReconciler) processConfigs(ctx context.Context, configs []v1alpha1.NodeNetworkConfig, revision *v1alpha1.NetworkConfigRevision) (bool, int, int, error) {
	ready := 0
	ongoing := 0
	for i := range configs {
		// Every NodeNetworkConfig obejct should have 2 owner references - for NodeConfigRevision and for the Node. If there is only one owner reference,
		// it means that either node or revision were deleted, so the config itself can be deleted as well.
		if len(configs[i].ObjectMeta.OwnerReferences) < numOfRefs {
			if err := ncr.client.Delete(ctx, &configs[i]); err != nil && !apierrors.IsNotFound(err) {
				return true, ready, ongoing, fmt.Errorf("error deleting redundant node config - %s: %w", configs[i].Name, err)
			}
		}

		if configs[i].Spec.Revision == revision.Spec.Revision {
			switch configs[i].Status.ConfigStatus {
			case StatusInvalid:
				// One of the configs is invalid, tag revision as invalid.
				revision.Status.IsInvalid = true
				if err := ncr.client.Status().Update(ctx, revision); err != nil {
					return true, ready, ongoing, fmt.Errorf("error invalidating revision %s: %w", revision.Name, err)
				}
				return true, ready, ongoing, nil
			case StatusProvisioning:
				// Update ongoing counter
				ongoing++
				// If status is 'provisioning' check for how long this status is set already, and if time threshold is exceeded, tag config as invalid.
				if time.Now().After(configs[i].Status.LastUpdate.Add(configTimeout)) {
					configs[i].Status.ConfigStatus = StatusInvalid
					configs[i].Status.LastUpdate = metav1.Now()
					if err := ncr.client.Status().Update(ctx, &configs[i]); err != nil {
						return true, ready, ongoing, fmt.Errorf("error invalidating config: %w", err)
					}
					return true, ready, ongoing, nil
				}
			case StatusProvisioned:
				// Update ready counter
				ready++
			}
		}
	}
	return false, ready, ongoing, nil
}

func getNodesToDeploy(nodes map[string]*corev1.Node, configs []v1alpha1.NodeNetworkConfig, revision *v1alpha1.NetworkConfigRevision) []*corev1.Node {
	for nodeName := range nodes {
		for i := range configs {
			if configs[i].Name == nodeName && configs[i].Spec.Revision == revision.Spec.Revision {
				delete(nodes, nodeName)
				break
			}
		}
	}

	nodesToDeploy := []*corev1.Node{}
	for _, node := range nodes {
		nodesToDeploy = append(nodesToDeploy, node)
	}
	return nodesToDeploy
}

func (ncr *NodeConfigReconciler) updateRevisionCounters(ctx context.Context, revision *v1alpha1.NetworkConfigRevision, available, ready, ongoing int) error {
	revision.Status.Total = available
	revision.Status.Ready = ready
	revision.Status.Ongoing = ongoing
	revision.Status.Queued = available - ready - ongoing

	if err := ncr.client.Status().Update(ctx, revision); err != nil {
		return fmt.Errorf("error updating revision's status %s: %w", revision.Name, err)
	}
	return nil
}

func (ncr *NodeConfigReconciler) revisionCleanup(ctx context.Context) error {
	revisions, err := listRevisions(ctx, ncr.client)
	if err != nil {
		return fmt.Errorf("error listing revisions: %w", err)
	}

	if len(revisions.Items) > 1 {
		nodeConfigs, err := ncr.listConfigs(ctx)
		if err != nil {
			return fmt.Errorf("error listing configs: %w", err)
		}
		if !revisions.Items[0].Status.IsInvalid && revisions.Items[0].Status.Ready == revisions.Items[0].Status.Total {
			for i := 1; i < len(revisions.Items); i++ {
				if countReferences(&revisions.Items[i], nodeConfigs.Items) == 0 {
					if err := ncr.client.Delete(ctx, &revisions.Items[i]); err != nil {
						return fmt.Errorf("error deletring revision %s: %w", revisions.Items[i].Name, err)
					}
				}
			}
		}
	}

	return nil
}

func countReferences(revision *v1alpha1.NetworkConfigRevision, configs []v1alpha1.NodeNetworkConfig) int {
	refCnt := 0
	for j := range configs {
		if configs[j].Spec.Revision == revision.Spec.Revision {
			refCnt++
		}
	}
	return refCnt
}

func (ncr *NodeConfigReconciler) listConfigs(ctx context.Context) (*v1alpha1.NodeNetworkConfigList, error) {
	nodeConfigs := &v1alpha1.NodeNetworkConfigList{}
	if err := ncr.client.List(ctx, nodeConfigs); err != nil {
		return nil, fmt.Errorf("error listing nodeConfigs: %w", err)
	}
	return nodeConfigs, nil
}

func (ncr *NodeConfigReconciler) deployNodeConfig(ctx context.Context, node *corev1.Node, revision *v1alpha1.NetworkConfigRevision) error {
	newConfig, err := ncr.createConfigForNode(node, revision)
	if err != nil {
		return fmt.Errorf("error preparing config for node %s: %w", node.Name, err)
	}
	currentConfig := &v1alpha1.NodeNetworkConfig{}
	if err := ncr.client.Get(ctx, types.NamespacedName{Name: node.Name}, currentConfig); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("error getting NodeNetworkConfig object for node %s: %w", node.Name, err)
		}
		currentConfig = nil
	}
	if currentConfig != nil && currentConfig.Spec.Revision == revision.Spec.Revision {
		// current config is the same as current revision - skip
		return nil
	}
	if err := ncr.deployConfig(ctx, newConfig, currentConfig, node); err != nil {
		if errors.Is(err, InvalidConfigError) || errors.Is(err, context.DeadlineExceeded) {
			// revision results in invalid config or in context timeout - invalidate revision
			revision.Status.IsInvalid = true
			if err := ncr.client.Status().Update(ctx, revision); err != nil {
				return fmt.Errorf("error invalidating revision %s: %w", revision.Name, err)
			}
		}
		return fmt.Errorf("error deploying config for node %s: %w", node.Name, err)
	}
	return nil
}

func (ncr *NodeConfigReconciler) createConfigForNode(node *corev1.Node, revision *v1alpha1.NetworkConfigRevision) (*v1alpha1.NodeNetworkConfig, error) {
	// create new config
	c := &v1alpha1.NodeNetworkConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: node.Name,
		},
	}

	c.Spec = *revision.Spec.Config.DeepCopy()
	c.Spec.Revision = revision.Spec.Revision
	c.Name = node.Name

	if err := controllerutil.SetOwnerReference(node, c, scheme.Scheme); err != nil {
		return nil, fmt.Errorf("error setting owner references (node): %w", err)
	}

	if err := controllerutil.SetOwnerReference(revision, c, ncr.scheme); err != nil {
		return nil, fmt.Errorf("error setting owner references (revision): %w", err)
	}

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

func (ncr *NodeConfigReconciler) deployConfig(ctx context.Context, newConfig, currentConfig *v1alpha1.NodeNetworkConfig, node *corev1.Node) error {
	var cfg *v1alpha1.NodeNetworkConfig
	if currentConfig != nil {
		cfg = currentConfig
		// there already is config for node - update
		cfg.Spec = *newConfig.Spec.DeepCopy()
		cfg.Status = *newConfig.Status.DeepCopy()
		cfg.ObjectMeta.OwnerReferences = newConfig.ObjectMeta.OwnerReferences
		cfg.Name = node.Name
		if err := ncr.client.Update(ctx, cfg); err != nil {
			return fmt.Errorf("error updating config for node %s: %w", node.Name, err)
		}
	} else {
		cfg = newConfig
		// there is no config for node - create one
		if err := ncr.client.Create(ctx, cfg); err != nil {
			return fmt.Errorf("error creating config for node %s: %w", node.Name, err)
		}
	}

	if err := setStatus(ctx, ncr.client, cfg, ""); err != nil {
		return fmt.Errorf("error setting config '%s' status: %w", cfg.Name, err)
	}

	return nil
}

func listNodes(ctx context.Context, c client.Client) (map[string]*corev1.Node, error) {
	// list all nodes
	list := &corev1.NodeList{}
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("unable to list nodes: %w", err)
	}

	// discard control-plane and not-ready nodes
	nodes := map[string]*corev1.Node{}
	for i := range list.Items {
		_, isControlPlane := list.Items[i].Labels[controlPlaneLabel]
		if !isControlPlane {
			// discard nodes that are not in ready state
			for j := range list.Items[i].Status.Conditions {
				// TODO: Should taint node.kubernetes.io/not-ready be used instead of Conditions?
				if list.Items[i].Status.Conditions[j].Type == corev1.NodeReady &&
					list.Items[i].Status.Conditions[j].Status == corev1.ConditionTrue {
					nodes[list.Items[i].Name] = &list.Items[i]
					break
				}
			}
		}
	}

	return nodes, nil
}

type ConfigError struct {
	Message string
}

func (e *ConfigError) Error() string {
	return e.Message
}

func (*ConfigError) Is(target error) bool {
	_, ok := target.(*ConfigError)
	return ok
}

var InvalidConfigError = &ConfigError{Message: "invalid config"}
