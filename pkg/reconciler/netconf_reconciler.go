package reconciler

import (
	"context"
	"errors"

	networkv1alpha1 "github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/config"
)

type NetconfReconciler struct{}

func NewNetconfReconciler() (Adapter, error) {
	return &NetconfReconciler{}, nil
}

func (*NetconfReconciler) checkHealth(context.Context) error {
	return errors.ErrUnsupported
}

func (*NetconfReconciler) getConfig() *config.Config {
	return nil
}

func (*NetconfReconciler) reconcileLayer3([]networkv1alpha1.VRFRouteConfigurationSpec, []networkv1alpha1.RoutingTableSpec) error {
	return errors.ErrUnsupported
}

func (*NetconfReconciler) reconcileLayer2([]networkv1alpha1.Layer2NetworkConfigurationSpec) error {
	return errors.ErrUnsupported
}
