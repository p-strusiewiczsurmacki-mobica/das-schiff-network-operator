package reconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	controlPlaneLabel = "node-role.kubernetes.io/control-plane"
	nodeDebauncerTime = time.Second * 5
)

// NodeReconciler is responsible for watching node objects.
type NodeReconciler struct {
	client           client.Client
	logger           logr.Logger
	debouncer        *debounce.Debouncer
	timeout          string
	nodes            map[string]corev1.Node
	Mutex            sync.Mutex
	Events           chan event.GenericEvent
	firstRun         bool
	configReconciler *ConfigReconciler

	NodeReconcilerReady chan bool
}

// Reconcile starts reconciliation.
func (nr *NodeReconciler) Reconcile(ctx context.Context) {
	nr.debouncer.Debounce(ctx)
}

// NewConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewNodeReconciler(clusterClient client.Client, logger logr.Logger, timeout string, cr *ConfigReconciler) (*NodeReconciler, error) {
	reconciler := &NodeReconciler{
		client:              clusterClient,
		logger:              logger,
		timeout:             timeout,
		nodes:               make(map[string]corev1.Node),
		Events:              make(chan event.GenericEvent),
		NodeReconcilerReady: make(chan bool),
		firstRun:            true,
		configReconciler:    cr,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, nodeDebauncerTime, logger)

	return reconciler, nil
}

func (nr *NodeReconciler) reconcileDebounced(ctx context.Context) error {
	nr.logger.Info("node reconciler started")

	nr.logger.Info("on leader election finieshed")
	nr.Mutex.Lock()
	defer nr.Mutex.Unlock()

	currentNodes, err := nr.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	nr.logger.Info("listed nodes")

	added, deleted := nr.checkNodeChanges(currentNodes)

	nr.logger.Info("checked changes")

	if len(deleted) > 0 {
		nr.logger.Info("nodes deleted", "nodes", deleted)
	}

	// remove NodeConfig obejcts if node was deleted
	for _, name := range deleted {
		nr.logger.Info("node was deleted, will delete NodeConfig objects", "node", name)
		nodeConfigs := &v1alpha1.NodeConfigList{}
		if err := nr.client.List(ctx, nodeConfigs); err != nil {
			return fmt.Errorf("error listing NodeConfigs: %w", err)
		}

		for i := range nodeConfigs.Items {
			// TODO: should owner references be used to identify configs?
			if nodeConfigs.Items[i].Name == name ||
				nodeConfigs.Items[i].Name == name+invalidSuffix ||
				nodeConfigs.Items[i].Name == name+backupSuffix {
				if err := nr.client.Delete(ctx, &nodeConfigs.Items[i]); err != nil {
					return fmt.Errorf("error while deleting config for deleted node: %w", err)
				}
			}
		}
		if err := nr.configReconciler.DeleteConfig(name); err != nil {
			return fmt.Errorf("error deleting node objects: %w", err)
		}
		nr.logger.Info("NodeConfig objects associated with the node were deleted", "node", name)
	}

	// save list of current nodes
	nr.nodes = currentNodes

	nr.logger.Info("add nodes", "number", len(added))

	// force reconciliation if new nodes were added to the cluster
	if len(added) > 0 {
		if err := nr.configReconciler.AddConfigsForNodes(added); err != nil {
			return fmt.Errorf("error adding configs for new nodes: %w", err)
		}
		if !nr.firstRun {
			// it's not needed to trigger reconciler on first run
			nr.Events <- event.GenericEvent{Object: &corev1.Node{ObjectMeta: metav1.ObjectMeta{}}}
		}
	}

	if nr.firstRun {
		nr.NodeReconcilerReady <- true
		nr.firstRun = false
	}

	nr.logger.Info("node reconciler run", "nodes number", len(nr.nodes))

	return nil
}

func (nr *NodeReconciler) ListNodes(ctx context.Context) (map[string]corev1.Node, error) {
	// list all nodes
	list := &corev1.NodeList{}
	if err := nr.client.List(ctx, list); err != nil {
		return nil, fmt.Errorf("unable to list nodes: %w", err)
	}

	// discard control-plane nodes and create map of nodes
	nodes := make(map[string]corev1.Node)
	for i := range list.Items {
		_, isControlPlane := list.Items[i].Labels[controlPlaneLabel]
		if !isControlPlane {
			// discard nodes that are not in ready state
			for j := range list.Items[i].Status.Conditions {
				// TODO: Should taint node.kubernetes.io/not-ready be used instead of Conditions?
				if list.Items[i].Status.Conditions[j].Type == corev1.NodeReady &&
					list.Items[i].Status.Conditions[j].Status == corev1.ConditionTrue {
					nodes[list.Items[i].Name] = list.Items[i]
					break
				}
			}
		}
	}

	return nodes, nil
}

func (nr *NodeReconciler) checkNodeChanges(newState map[string]corev1.Node) (added, deleted []string) {
	added = getDifference(newState, nr.nodes)
	deleted = getDifference(nr.nodes, newState)
	return added, deleted
}

func getDifference(first, second map[string]corev1.Node) []string {
	diff := []string{}
	for name := range first {
		if _, exists := second[name]; !exists {
			diff = append(diff, name)
		}
	}
	return diff
}

// nolint: gocritic
func (nr *NodeReconciler) GetNodes() map[string]corev1.Node {
	nr.Mutex.Lock()
	defer nr.Mutex.Unlock()
	currentNodes := make(map[string]corev1.Node)
	for k, v := range nr.nodes {
		currentNodes[k] = v
	}
	return currentNodes
}

func (nr *NodeReconciler) CheckIfNodeExists(name string) bool {
	nr.Mutex.Lock()
	defer nr.Mutex.Unlock()
	_, exists := nr.nodes[name]
	return exists
}
