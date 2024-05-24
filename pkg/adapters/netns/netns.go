package adapters

import (
	"errors"

	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/agent"
	"github.com/telekom/das-schiff-network-operator/pkg/config"
)

type NetNS struct{}

func New() (agent.Adapter, error) {
	return &NetNS{}, nil
}

func (*NetNS) CheckHealth() error {
	return errors.ErrUnsupported
}

func (*NetNS) GetConfig() *config.Config {
	return nil
}

func (*NetNS) ReconcileLayer3([]v1alpha1.VRFRouteConfigurationSpec, []v1alpha1.RoutingTableSpec) error {
	return errors.ErrUnsupported
}

func (*NetNS) ReconcileLayer2([]v1alpha1.Layer2NetworkConfigurationSpec) error {
	return errors.ErrUnsupported
}
