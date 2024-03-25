package adapters

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	nladapter "github.com/telekom/das-schiff-network-operator/pkg/adapters/netlink"
	"github.com/telekom/das-schiff-network-operator/pkg/agent"
	"github.com/telekom/das-schiff-network-operator/pkg/anycast"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
	"github.com/vishvananda/netlink"
)

type netNS struct {
	*nladapter.Common
}

func New(anycastTracker *anycast.Tracker, logger logr.Logger) (agent.Adapter, error) {
	n, err := nladapter.New(anycastTracker, logger)
	if err != nil {
		return nil, fmt.Errorf("error creating adapter: %w", err)
	}
	return &netNS{n}, nil
}

func (n *netNS) ReconcileLayer3(l3vnis []v1alpha1.VRFRouteConfigurationSpec, taas []v1alpha1.RoutingTableSpec) error {
	if err := n.Common.ReconcileLayer3(l3vnis, taas, false); err != nil {
		return fmt.Errorf("error reconciling Layer3: %w", err)
	}
	return nil
}

func (n *netNS) ReconcileLayer2(l2vnis []v1alpha1.Layer2NetworkConfigurationSpec) error {
	create, anycastTrackerInterfaces, err := n.ReconcileExistingAndDeleted(l2vnis)
	if err != nil {
		return fmt.Errorf("error reconciling: %w", err)
	}

	for i := range create {
		if err := n.createL2(&create[i], &anycastTrackerInterfaces); err != nil {
			return fmt.Errorf("error creating L2: %w", err)
		}
	}

	n.AnycastTracker.TrackedBridges = anycastTrackerInterfaces

	return nil
}

func (n *netNS) createL2(info *nl.Layer2Information, anycastTrackerInterfaces *[]int) error {
	n.Logger.Info("Creating Layer2", "vlan", info.VlanID, "vni", info.VNI)
	err := n.ProcessL2(info)
	if err != nil {
		return fmt.Errorf("error creating layer2 vlan %d vni %d: %w", info.VlanID, info.VNI, err)
	}
	if info.AdvertiseNeighbors {
		bridgeID, err := n.NetlinkManager.GetBridgeID(info)
		if err != nil {
			return fmt.Errorf("error getting bridge id for vlanId %d: %w", info.VlanID, err)
		}
		*anycastTrackerInterfaces = append(*anycastTrackerInterfaces, bridgeID)
	}
	return nil
}

func (n *netNS) ProcessL2(info *nl.Layer2Information) error {
	hbr, err := n.NetlinkManager.GetLinkByName("hbr")
	if err != nil {
		return fmt.Errorf("cannot get L3 by name: %w", err)
	}

	hbrVrf, ok := hbr.(*netlink.Vrf)
	if !ok {
		return fmt.Errorf("hbr is not a VRF")
	}

	if len(info.AnycastGateways) > 0 && info.AnycastMAC == nil {
		return fmt.Errorf("anycastGateways require anycastMAC to be set")
	}

	bridge, err := n.NetlinkManager.SetupBridge(info, hbrVrf.Index)
	if err != nil {
		return fmt.Errorf("cannot setup bridge: %w", err)
	}

	if err := n.NetlinkManager.SetupVXLAN(info, bridge); err != nil {
		return fmt.Errorf("cannot setup VxLAN: %w", err)
	}

	tr, err := n.NetlinkManager.GetLinkByName("tr")
	if err != nil {
		return fmt.Errorf("error getting link: %w", err)
	}

	if err := n.NetlinkManager.CreateVlan("tr.100", tr.Attrs().Index, bridge.Index); err != nil {
		return fmt.Errorf("error creating vlan: %w", err)
	}

	return nil
}
