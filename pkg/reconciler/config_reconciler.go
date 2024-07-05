package reconciler

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	DefaultTimeout         = "60s"
	DefaultNodeUpdateLimit = 1
)

//go:generate mockgen -destination ./mock/mock_config_reconciler.go . ConfigReconcilerInterface
type ConfigReconcilerInterface interface {
	CreateConfigForNode(string, *corev1.Node) (*v1alpha1.NodeNetworkConfig, error)
}

// ConfigReconciler is responsible for creating NodeConfig objects.
type ConfigReconciler struct {
	globalCfg *v1alpha1.NodeNetworkConfig
	logger    logr.Logger
	debouncer *debounce.Debouncer
	client    client.Client
	timeout   time.Duration
}

type reconcileConfig struct {
	*ConfigReconciler
	logr.Logger
}

// Reconcile starts reconciliation.
func (cr *ConfigReconciler) Reconcile(ctx context.Context) {
	cr.debouncer.Debounce(ctx)
}

// // NewConfigReconciler creates new reconciler that creates NodeConfig objects.
func NewConfigReconciler(clusterClient client.Client, logger logr.Logger, timeout time.Duration) (*ConfigReconciler, error) {
	reconciler := &ConfigReconciler{
		logger:  logger,
		timeout: timeout,
		client:  clusterClient,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (cr *ConfigReconciler) reconcileDebounced(ctx context.Context) error {
	r := &reconcileConfig{
		ConfigReconciler: cr,
		Logger:           cr.logger,
	}

	cr.logger.Info("fetching config data...")

	timeoutCtx, cancel := context.WithTimeout(ctx, cr.timeout)
	defer cancel()

	// get VRFRouteConfiguration, Layer2networkConfiguration and RoutingTable objects
	globalCfg, err := r.fetchConfigData(timeoutCtx)
	if err != nil {
		return fmt.Errorf("error fetching configuration details: %w", err)
	}

	// prepare new revision
	revision, err := v1alpha1.NewRevision(globalCfg)

	if err != nil {
		return fmt.Errorf("error preparing new config revision: %w", err)
	}

	cr.logger.Info("new revision", "data", revision)

	revisions, err := listRevisions(timeoutCtx, cr.client)
	if err != nil {
		return fmt.Errorf("error listing revisions: %w", err)
	}

	if revision.Spec.Revision == revisions.Items[0].Spec.Revision {
		// new revision equals to the last one - skip
		return nil
	}

	for i := range revisions.Items {
		cr.logger.Info("revision", "data", r)
		if revision.Spec.Revision == revisions.Items[i].Spec.Revision && revisions.Items[i].Status.IsInvalid {
			// new revision is equal to known invalid revision
			return nil
		}
	}

	// create revision object
	if err := cr.client.Create(timeoutCtx, revision); err != nil {
		return fmt.Errorf("error creating new NodeConfigRevision: %w", err)
	}

	cr.logger.Info("global config updated", "config", *cr.globalCfg)
	return nil
}

func (r *reconcileConfig) fetchConfigData(ctx context.Context) (*v1alpha1.NodeNetworkConfig, error) {
	// get VRFRouteConfiguration objects
	l3vnis, err := r.fetchLayer3(ctx)
	if err != nil {
		return nil, err
	}

	// get Layer2networkConfiguration objects
	l2vnis, err := r.fetchLayer2(ctx)
	if err != nil {
		return nil, err
	}

	// get RoutingTable objects
	taas, err := r.fetchTaas(ctx)
	if err != nil {
		return nil, err
	}

	config := &v1alpha1.NodeNetworkConfig{}

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

func (cr *ConfigReconciler) createConfigForNode(name string, node *corev1.Node) (*v1alpha1.NodeNetworkConfig, error) {
	// create new config
	c := &v1alpha1.NodeNetworkConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if cr.globalCfg == nil {
		cr.globalCfg = v1alpha1.NewEmptyConfig(name)
	}

	v1alpha1.CopyNodeNetworkConfig(cr.globalCfg, c, name)

	err := controllerutil.SetOwnerReference(node, c, scheme.Scheme)
	if err != nil {
		return nil, fmt.Errorf("error setting owner references: %w", err)
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

func listRevisions(ctx context.Context, c client.Client) (*v1alpha1.NetworkConfigRevisionList, error) {
	revisions := &v1alpha1.NetworkConfigRevisionList{}
	if err := c.List(ctx, revisions); err != nil {
		return nil, fmt.Errorf("error listing revisions: %w", err)
	}

	// sort revisions by creation date ascending (newest first)
	slices.SortFunc(revisions.Items, func(a, b v1alpha1.NetworkConfigRevision) int {
		return a.CreationTimestamp.Compare(b.CreationTimestamp.Time) * -1 // reverse result
	})

	return revisions, nil
}
