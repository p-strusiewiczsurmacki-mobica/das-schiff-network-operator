package adapters

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	nladapter "github.com/telekom/das-schiff-network-operator/pkg/adapters/netlink"
	"github.com/telekom/das-schiff-network-operator/pkg/agent"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
)

type VrfIgbp struct {
	*nladapter.Common
}

func New(anycastTracker *anycast.Tracker, logger logr.Logger) (agent.Adapter, error) {
	n, err := nladapter.New(anycastTracker, logger)
	if err != nil {
		return nil, fmt.Errorf("error creating adapter: %w", err)
	}
	return &VrfIgbp{n}, nil
}

func (n *VrfIgbp) ReconcileLayer3(l3vnis []v1alpha1.VRFRouteConfigurationSpec, taas []v1alpha1.RoutingTableSpec) error {
	if err := n.Common.ReconcileLayer3(l3vnis, taas, true); err != nil {
		return fmt.Errorf("error reconciling Layer3: %w", err)
	}
	return nil
}
