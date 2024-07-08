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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultNodeConfigDebounceTime, logger)

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

	revisions, err := listRevisions(timeoutCtx, cr.client, cr.logger)
	if err != nil {
		return fmt.Errorf("error listing revisions: %w", err)
	}
	cr.logger.Info("number of revisions:", "len", len(revisions.Items))

	if len(revisions.Items) > 0 && revision.Spec.Revision == revisions.Items[0].Spec.Revision {
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
		if apierrors.IsAlreadyExists(err) {
			if err := cr.client.Delete(timeoutCtx, revision); err != nil {
				return fmt.Errorf("error creating deleting already exisitng NodeConfigRevision: %w", err)
			}
			if err := cr.client.Create(timeoutCtx, revision); err != nil {
				return fmt.Errorf("error creating NodeConfigRevision: %w", err)
			}
		} else {
			return fmt.Errorf("error creating NodeConfigRevision: %w", err)
		}
	}

	cr.logger.Info("global config updated", "config", *globalCfg)
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

func listRevisions(ctx context.Context, c client.Client, l logr.Logger) (*v1alpha1.NetworkConfigRevisionList, error) {
	revisions := &v1alpha1.NetworkConfigRevisionList{}
	if err := c.List(ctx, revisions); err != nil {
		return nil, fmt.Errorf("error listing revisions: %w", err)
	}

	l.Info("revisions found", "items", revisions.Items)

	// sort revisions by creation date ascending (newest first)
	if len(revisions.Items) > 0 {
		slices.SortFunc(revisions.Items, func(a, b v1alpha1.NetworkConfigRevision) int {
			return b.ObjectMeta.CreationTimestamp.Compare(a.ObjectMeta.CreationTimestamp.Time) // newest first
		})
	}

	return revisions, nil
}
