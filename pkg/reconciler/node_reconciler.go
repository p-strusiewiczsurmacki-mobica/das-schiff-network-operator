package reconciler

import (
	"context"
	"fmt"
	"strings"
	"sync"

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
)

// NodeReconciler is responsible for watching node objects.
type NodeReconciler struct {
	client    client.Client
	logger    logr.Logger
	debouncer *debounce.Debouncer
	timeout   string
	nodes     map[string]corev1.Node
	Mutex     sync.Mutex
	Events    chan event.GenericEvent

	OnLeaderElectionDone chan bool
}

// Reconcile starts reconciliation.
func (nr *NodeReconciler) Reconcile(ctx context.Context) {
	nr.debouncer.Debounce(ctx)
}

// NewConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewNodeReconciler(clusterClient client.Client, logger logr.Logger, timeout string) (*NodeReconciler, error) {
	reconciler := &NodeReconciler{
		client:               clusterClient,
		logger:               logger,
		timeout:              timeout,
		nodes:                make(map[string]corev1.Node),
		Events:               make(chan event.GenericEvent),
		OnLeaderElectionDone: make(chan bool),
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (nr *NodeReconciler) reconcileDebounced(ctx context.Context) error {
	<-nr.OnLeaderElectionDone
	nr.Mutex.Lock()
	defer nr.Mutex.Unlock()

	currentNodes, err := nr.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	added, deleted := nr.checkNodeChanges(currentNodes)

	// remove NodeConfig obejcts if node was deleted
	for _, name := range deleted {
		nodeConfigs := &v1alpha1.NodeConfigList{}
		if err := nr.client.List(ctx, nodeConfigs); err != nil {
			return fmt.Errorf("error listing NodeConfigs: %w", err)
		}

		for i := range nodeConfigs.Items {
			if strings.Contains(nodeConfigs.Items[i].Name, name) {
				if err := nr.client.Delete(ctx, &nodeConfigs.Items[i]); err != nil {
					return fmt.Errorf("error while deleting config for deleted node: %w", err)
				}
			}
		}
	}

	// save list of current nodes
	nr.nodes = currentNodes

	// force reconciliation if new nodes were added to the cluster
	if len(added) > 0 {
		nr.Events <- event.GenericEvent{Object: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: added[0]}}}
	}

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
