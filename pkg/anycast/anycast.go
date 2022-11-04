package anycast

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var (
	ANYCAST_ROUTES_PROT       = netlink.RouteProtocol(125)
	DEFAULT_VRF_ANYCAST_TABLE = 130
)

type AnycastTracker struct {
	TrackedBridges []int
}

//TODO: Anycast Support is currently highly experimental

func (a *AnycastTracker) checkTrackedInterfaces() {
	for _, intfIdx := range a.TrackedBridges {
		intf, err := netlink.LinkByIndex(intfIdx)
		if err != nil {
			fmt.Printf("Couldn't load interface idx %d: %v\n", intfIdx, err)
			continue
		}

		syncInterface(intf.(*netlink.Bridge))
	}
}

func createNeighborEntry(mac net.HardwareAddr, intf, master int) *netlink.Neigh {
	return &netlink.Neigh{
		State:        netlink.NUD_NOARP,
		Family:       unix.AF_BRIDGE,
		HardwareAddr: mac,
		LinkIndex:    intf,
		MasterIndex:  master,
	}
}

func isUnicastMac(mac net.HardwareAddr) bool {
	return mac[0]&0x01 == 0
}

func containsIPNetwork(list []*net.IPNet, dst *net.IPNet) bool {
	for _, v := range list {
		if bytes.Equal(v.IP, dst.IP) && bytes.Equal(v.Mask, dst.Mask) {
			return true
		}
	}
	return false
}

func containsIPAddress(list []netlink.Neigh, dst *net.IPNet) bool {
	for _, v := range list {
		if bytes.Equal(v.IP, dst.IP) {
			return true
		}
	}
	return false
}

func buildRoute(family int, intf *netlink.Bridge, dst *net.IPNet, table uint32) *netlink.Route {
	return &netlink.Route{
		Family:    family,
		Protocol:  ANYCAST_ROUTES_PROT,
		LinkIndex: intf.Attrs().Index,
		Dst:       dst,
		Table:     int(table),
	}
}

func syncInterfaceByFamily(intf *netlink.Bridge, family int, routingTable uint32) {
	bridgeNeighbors, err := netlink.NeighList(intf.Attrs().Index, family)
	if err != nil {
		fmt.Printf("Error getting v4 neighbors of interface %s: %v\n", intf.Attrs().Name, err)
		return
	}

	routeFilterV4 := &netlink.Route{
		LinkIndex: intf.Attrs().Index,
		Table:     int(routingTable),
		Protocol:  ANYCAST_ROUTES_PROT,
	}
	routes, err := netlink.RouteListFiltered(family, routeFilterV4, netlink.RT_FILTER_OIF|netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		fmt.Printf("Error getting v4 routes of interface %s: %v\n", intf.Attrs().Name, err)
		return
	}

	alreadyV4Existing := []*net.IPNet{}
	for _, route := range routes {
		if !containsIPAddress(bridgeNeighbors, route.Dst) {
			netlink.RouteDel(&route)
		} else {
			alreadyV4Existing = append(alreadyV4Existing, route.Dst)
		}
	}

	for _, neighbor := range bridgeNeighbors {
		net := netlink.NewIPNet(neighbor.IP)
		if !containsIPNetwork(alreadyV4Existing, net) {
			netlink.RouteAdd(buildRoute(family, intf, net, routingTable))
		}
	}
}

func syncInterface(intf *netlink.Bridge) {
	routingTable := uint32(DEFAULT_VRF_ANYCAST_TABLE)
	if intf.Attrs().MasterIndex > 0 {
		nl, err := netlink.LinkByIndex(intf.Attrs().MasterIndex)
		if err != nil {
			fmt.Printf("Error getting VRF parent of interface %s: %v\n", intf.Attrs().Name, err)
			return
		}
		if nl.Type() != "vrf" {
			fmt.Printf("Parent interface of %s is not a VRF: %v\n", intf.Attrs().Name, err)
			return
		}
		routingTable = nl.(*netlink.Vrf).Table
	}

	syncInterfaceByFamily(intf, unix.AF_INET, routingTable)
	syncInterfaceByFamily(intf, unix.AF_INET6, routingTable)
}

func (a *AnycastTracker) RunAnycastSync() {
	go func() {
		for {
			a.checkTrackedInterfaces()
			time.Sleep(time.Second)
		}
	}()
}
