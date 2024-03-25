package adapters

import (
	"fmt"
	"net"

	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/nl"
)

func (c *Common) ReconcileLayer2(l2vnis []v1alpha1.Layer2NetworkConfigurationSpec) error {
	create, anycastTrackerInterfaces, err := c.ReconcileExistingAndDeleted(l2vnis)
	if err != nil {
		return err
	}

	for i := range create {
		if err := c.createL2(&create[i], &anycastTrackerInterfaces); err != nil {
			return err
		}
	}

	c.AnycastTracker.TrackedBridges = anycastTrackerInterfaces

	return nil
}

func (c *Common) ReconcileExistingAndDeleted(l2vnis []v1alpha1.Layer2NetworkConfigurationSpec) ([]nl.Layer2Information, []int, error) {
	desired, err := c.GetDesired(l2vnis)
	if err != nil {
		return nil, nil, err
	}

	existing, err := c.NetlinkManager.ListL2()
	if err != nil {
		return nil, nil, fmt.Errorf("error listing L2: %w", err)
	}

	toDelete := DetermineToBeDeleted(existing, desired)

	create := []nl.Layer2Information{}
	anycastTrackerInterfaces := []int{}
	for i := range desired {
		alreadyExists := false
		var currentConfig nl.Layer2Information
		for j := range existing {
			if desired[i].VlanID == existing[j].VlanID {
				alreadyExists = true
				currentConfig = existing[j]
				break
			}
		}
		if !alreadyExists {
			create = append(create, desired[i])
		} else {
			if err := c.ReconcileExistingLayer(&desired[i], &currentConfig, &anycastTrackerInterfaces); err != nil {
				return nil, nil, err
			}
		}
	}

	for i := range toDelete {
		c.Logger.Info("Deleting Layer2 because it is no longer configured", "vlan", toDelete[i].VlanID, "vni", toDelete[i].VNI)
		errs := c.NetlinkManager.CleanupL2(&toDelete[i])
		for _, err := range errs {
			c.Logger.Error(err, "Error deleting Layer2", "vlan", toDelete[i].VlanID, "vni", toDelete[i].VNI)
		}
	}

	return create, anycastTrackerInterfaces, nil
}

func (c *Common) ProcessL2(info *nl.Layer2Information) error {
	masterIdx := -1
	if info.VRF != "" {
		l3Info, err := c.NetlinkManager.GetL3ByName(info.VRF)
		if err != nil {
			return fmt.Errorf("error getting L3 by name: %w", err)
		}
		masterIdx = l3Info.VrfID
	}

	if len(info.AnycastGateways) > 0 && info.AnycastMAC == nil {
		return fmt.Errorf("anycastGateways require anycastMAC to be set")
	}

	bridge, err := c.NetlinkManager.SetupBridge(info, masterIdx)
	if err != nil {
		return fmt.Errorf("cannot setup bridge: %w", err)
	}

	if err := c.NetlinkManager.SetupVXLAN(info, bridge); err != nil {
		return fmt.Errorf("cannot setup VxLAN: %w", err)
	}

	return nil
}

func (c *Common) createL2(info *nl.Layer2Information, anycastTrackerInterfaces *[]int) error {
	c.Logger.Info("Creating Layer2", "vlan", info.VlanID, "vni", info.VNI)
	err := c.ProcessL2(info)
	if err != nil {
		return fmt.Errorf("error creating layer2 vlan %d vni %d: %w", info.VlanID, info.VNI, err)
	}
	if info.AdvertiseNeighbors {
		bridgeID, err := c.NetlinkManager.GetBridgeID(info)
		if err != nil {
			return fmt.Errorf("error getting bridge id for vlanId %d: %w", info.VlanID, err)
		}
		*anycastTrackerInterfaces = append(*anycastTrackerInterfaces, bridgeID)
	}
	return nil
}

func (c *Common) GetDesired(l2vnis []v1alpha1.Layer2NetworkConfigurationSpec) ([]nl.Layer2Information, error) {
	availableVrfs, err := c.NetlinkManager.ListL3()
	if err != nil {
		return nil, fmt.Errorf("error loading available VRFs: %w", err)
	}

	desired := []nl.Layer2Information{}
	for i := range l2vnis {
		spec := l2vnis[i]

		var anycastMAC *net.HardwareAddr
		if mac, err := net.ParseMAC(spec.AnycastMac); err == nil {
			anycastMAC = &mac
		}

		anycastGateways, err := c.NetlinkManager.ParseIPAddresses(spec.AnycastGateways)
		if err != nil {
			c.Logger.Error(err, "error parsing anycast gateways", "gw", spec.AnycastGateways)
			return nil, fmt.Errorf("error parsing anycast gateways: %w", err)
		}

		if spec.VRF != "" {
			vrfAvailable := false
			for _, info := range availableVrfs {
				if info.Name == spec.VRF {
					vrfAvailable = true
					break
				}
			}
			if !vrfAvailable {
				c.Logger.Error(err, "VRF of Layer2 not found on node", "vrf", spec.VRF)
				continue
			}
		}

		desired = append(desired, nl.Layer2Information{
			VlanID:                 spec.ID,
			MTU:                    spec.MTU,
			VNI:                    spec.VNI,
			VRF:                    spec.VRF,
			AnycastMAC:             anycastMAC,
			AnycastGateways:        anycastGateways,
			AdvertiseNeighbors:     spec.AdvertiseNeighbors,
			NeighSuppression:       spec.NeighSuppression,
			CreateMACVLANInterface: spec.CreateMACVLANInterface,
		})
	}

	return desired, nil
}

func DetermineToBeDeleted(existing, desired []nl.Layer2Information) []nl.Layer2Information {
	toDelete := []nl.Layer2Information{}
	for i := range existing {
		stillExists := false
		for j := range desired {
			if desired[j].VlanID == existing[i].VlanID {
				stillExists = true
				break
			}
		}
		if !stillExists {
			toDelete = append(toDelete, existing[i])
		}
	}
	return toDelete
}

func (c *Common) ReconcileExistingLayer(desired, currentConfig *nl.Layer2Information, anycastTrackerInterfaces *[]int) error {
	c.Logger.Info("Reconciling existing Layer2", "vlan", desired.VlanID, "vni", desired.VNI)
	err := c.NetlinkManager.ReconcileL2(currentConfig, desired)
	if err != nil {
		return fmt.Errorf("error reconciling layer2 vlan %d vni %d: %w", desired.VlanID, desired.VNI, err)
	}
	if desired.AdvertiseNeighbors {
		bridgeID, err := c.NetlinkManager.GetBridgeID(desired)
		if err != nil {
			return fmt.Errorf("error getting bridge id for vlanId %d: %w", desired.VlanID, err)
		}
		*anycastTrackerInterfaces = append(*anycastTrackerInterfaces, bridgeID)
	}
	return nil
}
