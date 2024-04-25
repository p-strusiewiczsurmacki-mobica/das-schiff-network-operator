package reconciler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

const (
	controlPlaneLabel = "node-role.kubernetes.io/control-plane"
	nodeDebauncerTime = time.Second * 5
)

// NodeReconciler is responsible for watching node objects.
type NodeReconciler struct {
	client    client.Client
	logger    logr.Logger
	debouncer *debounce.Debouncer
	nodes     map[string]corev1.Node
	Mutex     sync.Mutex
	Events    chan event.GenericEvent
	firstRun  bool
	timeout   time.Duration

	NodeReconcilerReady chan bool
	configManagerInform chan bool
	deleteNodeInform    chan []string
}

// Reconcile starts reconciliation.
func (nr *NodeReconciler) Reconcile(ctx context.Context) {
	nr.debouncer.Debounce(ctx)
}

// NewConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewNodeReconciler(clusterClient client.Client, logger logr.Logger, timeout time.Duration, cmInfo chan bool, nodeDelInfo chan []string) (*NodeReconciler, error) {
	reconciler := &NodeReconciler{
		client:              clusterClient,
		logger:              logger,
		nodes:               make(map[string]corev1.Node),
		Events:              make(chan event.GenericEvent),
		NodeReconcilerReady: make(chan bool),
		firstRun:            true,
		timeout:             timeout,
		configManagerInform: cmInfo,
		deleteNodeInform:    nodeDelInfo,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, nodeDebauncerTime, logger)

	return reconciler, nil
}

func (nr *NodeReconciler) reconcileDebounced(ctx context.Context) error {
	nr.logger.Info("node reconciler started")

	nr.Mutex.Lock()

	timeoutCtx, cancel := context.WithTimeout(ctx, nr.timeout)
	defer cancel()

	currentNodes, err := nr.listNodes(timeoutCtx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	nr.logger.Info("listed nodes")

	added, deleted := nr.checkNodeChanges(currentNodes)
	// save list of current nodes
	nr.nodes = currentNodes
	nr.Mutex.Unlock()

	nr.logger.Info("checked changes")

	// inform config manager that nodes were deleted
	if len(deleted) > 0 {
		nr.deleteNodeInform <- deleted
		nr.logger.Info("nodes deleted", "nodes", deleted)
	}

	// inform config manager that new nodes were added
	if len(added) > 0 {
		nr.configManagerInform <- true
		nr.logger.Info("add nodes", "number", len(added))
	}

	if nr.firstRun {
		nr.NodeReconcilerReady <- true
		nr.firstRun = false
	}

	nr.logger.Info("node reconciler run", "nodes number", len(nr.nodes))

	return nil
}

func (nr *NodeReconciler) listNodes(ctx context.Context) (map[string]corev1.Node, error) {
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
	nr.logger.Info("getting nodes")
	nr.Mutex.Lock()
	defer nr.Mutex.Unlock()
	currentNodes := make(map[string]corev1.Node)
	for k, v := range nr.nodes {
		currentNodes[k] = v
	}
	return currentNodes
}

// func (nr *NodeReconciler) CheckIfNodeExists(name string) bool {
// 	nr.logger.Info("checking if node exists", "node", name)
// 	nr.Mutex.Lock()
// 	defer nr.Mutex.Unlock()
// 	_, exists := nr.nodes[name]
// 	nr.logger.Info("status", "node", name, "exists", exists)
// 	return exists
// }
