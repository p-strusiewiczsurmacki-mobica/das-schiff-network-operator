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
	routeProtocol          = 125
	anycastRoutesProt      = netlink.RouteProtocol(routeProtocol)
	defaultVrfAnycastTable = 130
)

type Tracker struct {
	TrackedBridges []int
}

//TODO: Anycast Support is currently highly experimental.

func (t *Tracker) checkTrackedInterfaces() {
	for _, intfIdx := range t.TrackedBridges {
		intf, err := netlink.LinkByIndex(intfIdx)
		if err != nil {
			_, _ = fmt.Printf("Couldn't load interface idx %d: %v\n", intfIdx, err)
			continue
		}

		syncInterface(intf.(*netlink.Bridge))
	}
}

func containsIPNetwork(list []*net.IPNet, dst *net.IPNet) bool {
	for _, v := range list {
		if v.IP.Equal(dst.IP) && bytes.Equal(v.Mask, dst.Mask) {
			return true
		}
	}
	return false
}

func containsIPAddress(list []netlink.Neigh, dst *net.IPNet) bool {
	for i := range list {
		if list[i].IP.Equal(dst.IP) {
			return true
		}
	}
	return false
}

func buildRoute(family int, intf *netlink.Bridge, dst *net.IPNet, table uint32) *netlink.Route {
	return &netlink.Route{
		Family:    family,
		Protocol:  anycastRoutesProt,
		LinkIndex: intf.Attrs().Index,
		Dst:       dst,
		Table:     int(table),
	}
}

func filterNeighbors(neighIn []netlink.Neigh) (neighOut []netlink.Neigh) {
	for i := range neighIn {
		if neighIn[i].Flags&netlink.NTF_EXT_LEARNED == netlink.NTF_EXT_LEARNED {
			continue
		}
		if neighIn[i].State != netlink.NUD_NONE &&
			neighIn[i].State&netlink.NUD_PERMANENT != netlink.NUD_PERMANENT &&
			neighIn[i].State&netlink.NUD_STALE != netlink.NUD_STALE &&
			neighIn[i].State&netlink.NUD_REACHABLE != netlink.NUD_REACHABLE &&
			neighIn[i].State&netlink.NUD_DELAY != netlink.NUD_DELAY {
			continue
		}
		neighOut = append(neighOut, neighIn[i])
	}
	return neighOut
}

func syncInterfaceByFamily(intf *netlink.Bridge, family int, routingTable uint32) {
	bridgeNeighbors, err := netlink.NeighList(intf.Attrs().Index, family)
	if err != nil {
		_, _ = fmt.Printf("Error getting v4 neighbors of interface %s: %v\n", intf.Attrs().Name, err)
		return
	}
	bridgeNeighbors = filterNeighbors(bridgeNeighbors)

	routeFilterV4 := &netlink.Route{
		LinkIndex: intf.Attrs().Index,
		Table:     int(routingTable),
		Protocol:  anycastRoutesProt,
	}
	routes, err := netlink.RouteListFiltered(family, routeFilterV4, netlink.RT_FILTER_OIF|netlink.RT_FILTER_TABLE|netlink.RT_FILTER_PROTOCOL)
	if err != nil {
		_, _ = fmt.Printf("Error getting v4 routes of interface %s: %v\n", intf.Attrs().Name, err)
		return
	}

	alreadyV4Existing := []*net.IPNet{}
	for i := range routes {
		if !containsIPAddress(bridgeNeighbors, routes[i].Dst) {
			if err := netlink.RouteDel(&routes[i]); err != nil {
				_, _ = fmt.Printf("Error deleting route %v: %v\n", routes[i], err)
			}
		} else {
			alreadyV4Existing = append(alreadyV4Existing, routes[i].Dst)
		}
	}

	for i := range bridgeNeighbors {
		ipnet := netlink.NewIPNet(bridgeNeighbors[i].IP)
		if !containsIPNetwork(alreadyV4Existing, ipnet) {
			route := buildRoute(family, intf, ipnet, routingTable)
			if err := netlink.RouteAdd(route); err != nil {
				_, _ = fmt.Printf("Error adding route %v: %v\n", route, err)
			}
		}
	}
}

func syncInterface(intf *netlink.Bridge) {
	routingTable := uint32(defaultVrfAnycastTable)
	if intf.Attrs().MasterIndex > 0 {
		nl, err := netlink.LinkByIndex(intf.Attrs().MasterIndex)
		if err != nil {
			_, _ = fmt.Printf("Error getting VRF parent of interface %s: %v\n", intf.Attrs().Name, err)
			return
		}
		if nl.Type() != "vrf" {
			_, _ = fmt.Printf("Parent interface of %s is not a VRF: %v\n", intf.Attrs().Name, err)
			return
		}
		routingTable = nl.(*netlink.Vrf).Table
	}

	syncInterfaceByFamily(intf, unix.AF_INET, routingTable)
	syncInterfaceByFamily(intf, unix.AF_INET6, routingTable)
}

func (t *Tracker) RunAnycastSync() {
	go func() {
		for {
			t.checkTrackedInterfaces()
			time.Sleep(time.Second)
		}
	}()
}
