/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
)

// HBRConfigSpec defines the desired state of HBRConfig
type HBRConfigSpec struct {
	// Layer2s is a map of Layer2 configurations.
	Layer2s map[string]Layer2 `json:"layer2s,omitempty"`
	// DefaultVRF is the default VRF configuration used for the default route of HBR.
	DefaultVRF *FabricVRF `json:"defaultVRF,omitempty"`
	// FabricVRFs is a map of fabric VRF configurations.
	FabricVRFs map[string]FabricVRF `json:"fabricVRFs,omitempty"`
	// LocalVRFs is a map of local VRF configurations.
	LocalVRFs map[string]VRF `json:"localVRFs,omitempty"`
	// NodeSelector matches nodes to apply the configuration to.
	NodeSelector *metav1.LabelSelector `json:"nodeSelector,omitempty"`
}

// Layer2 represents a Layer 2 network configuration.
type Layer2 struct {
	// VNI is the Virtual Network Identifier.
	VNI uint32 `json:"vni"`
	// VLAN is the VLAN ID.
	VLAN uint16 `json:"vlan"`
	// RouteTarget is the route target for the Layer 2 network.
	RouteTarget string `json:"routeTarget"`
	// MTU is the Maximum Transmission Unit size.
	MTU uint16 `json:"mtu"`
	// IRB is the Integrated Routing and Bridging configuration.
	IRB *IRB `json:"irb,omitempty"`
}

// IRB represents the Integrated Routing and Bridging configuration.
type IRB struct {
	// VRF is the Virtual Routing and Forwarding instance.
	VRF string `json:"vrf"`
	// MACAddress is the MAC address for the IRB.
	MACAddress net.HardwareAddr `json:"macAddress"`
	// IPAddresses is a list of IP addresses for the IRB.
	// +kubebuilder:validation:MinItems=1
	IPAddresses []string `json:"ipAddresses"`
}

// VRF represents a Virtual Routing and Forwarding instance.
type VRF struct {
	// Loopbacks is a list of loopback interfaces.
	Loopbacks map[string]Loopback `json:"loopbacks,omitempty"`
	// BGPPeers is a list of BGP peers.
	BGPPeers []BGPPeer `json:"bgpPeers,omitempty"`
	// VRFImports is a list of VRF import configurations.
	VRFImports []VRFImport `json:"vrfImports,omitempty"`
	// StaticRoutes is a list of static routes.
	StaticRoutes []StaticRoute `json:"staticRoutes,omitempty"`
	// PolicyRoutes is a list of policy-based routes.
	PolicyRoutes []PolicyRoute `json:"policyRoutes,omitempty"`
	// MirrorACLs is a list of mirror ACLs.
	MirrorACLs []MirrorACL `json:"mirrorAcls,omitempty"`
}

// FabricVRF represents a fabric VRF configuration.
type FabricVRF struct {
	VRF `json:",inline"`
	// VNI is the Virtual Network Identifier.
	VNI uint32 `json:"vni"`
	// EVPNImportRouteTargets is a list of EVPN import route targets.
	EVPNImportRouteTargets []string `json:"evpnImportRouteTargets"`
	// EVPNExportRouteTargets is a list of EVPN export route targets.
	EVPNExportRouteTargets []string `json:"evpnExportRouteTargets"`
	// EVPNExportFilter is the export filter for EVPN.
	EVPNExportFilter *Filter `json:"evpnExportFilter"`
}

// Loopback represents a loopback interface.
type Loopback struct {
	// IPAddresses is a list of IP addresses for the loopback interface.
	// +kubebuilder:validation:MinItems=1
	IPAddresses []string `json:"ipAddresses"`
}

// VRFImport represents a VRF import configuration.
type VRFImport struct {
	// FromVRF is the source VRF for the import.
	FromVRF string `json:"fromVrf"`
	// Filter is the filter applied to the import.
	Filter Filter `json:"filter"`
}

// BGPPeer represents a BGP peer configuration.
type BGPPeer struct {
	// Address is the address of the BGP peer.
	Address *string `json:"address"`
	// ListenRange is the listen range for the BGP peer.
	ListenRange *string `json:"listenRange"`
	// RemoteASN is the remote Autonomous System Number.
	RemoteASN uint32 `json:"remoteAsn"`
	// IPv4 is the IPv4 address family configuration.
	IPv4 *AddressFamily `json:"ipv4,omitempty"`
	// IPv6 is the IPv6 address family configuration.
	IPv6 *AddressFamily `json:"ipv6,omitempty"`
	// BFDProfile is the BFD profile for the BGP peer.
	BFDProfile *BFDProfile `json:"bfdProfile,omitempty"`
	// HoldTime is the hold time for the BGP session, default is 90s.
	HoldTime *metav1.Duration `json:"holdTime,omitempty"`
	// KeepaliveTime is the keepalive time for the BGP session, default is 30s.
	KeepaliveTime *metav1.Duration `json:"keepaliveTime,omitempty"`
}

// BFDProfile represents a BFD profile configuration.
type BFDProfile struct {
	// MinInterval is the minimum interval for BFD.
	MinInterval uint32 `json:"minInterval"`
}

// AddressFamily represents an address family configuration.
type AddressFamily struct {
	// ExportFilter is the export filter for the address family.
	ExportFilter *Filter `json:"exportFilter,omitempty"`
	// ImportFilter is the import filter for the address family.
	ImportFilter *Filter `json:"importFilter,omitempty"`
	// MaxPrefixes is the maximum number of prefixes for the address family.
	MaxPrefixes *uint32 `json:"maxPrefixes,omitempty"`
}

// Filter represents a filter configuration.
type Filter struct {
	// Items is a list of filter items.
	Items []FilterItem `json:"items,omitempty"`
	// DefaultAction is the default action for the filter.
	DefaultAction Action `json:"defaultAction"`
}

// FilterItem represents a filter item.
type FilterItem struct {
	// Matcher is the matcher for the filter item.
	Matcher Matcher `json:"matcher"`
	// Action is the action for the filter item.
	Action Action `json:"action"`
}

// Matcher represents a matcher configuration.
type Matcher struct {
	// Prefix is the prefix matcher.
	Prefix *PrefixMatcher `json:"prefix,omitempty"`
	// BGPCommunity is the BGP community matcher.
	BGPCommunity *BGPCommunityMatcher `json:"bgpCommunity,omitempty"`
}

// PrefixMatcher represents a prefix matcher.
type PrefixMatcher struct {
	// Prefix is the prefix to match.
	Prefix string `json:"prefix"`
	// Ge is the minimum prefix length to match.
	Ge *int `json:"ge,omitempty"`
	// Le is the maximum prefix length to match.
	Le *int `json:"le,omitempty"`
}

// BGPCommunityMatcher represents a BGP community matcher.
type BGPCommunityMatcher struct {
	// Community is the BGP community to match.
	Community string `json:"community"`
}

// Action represents an action configuration.
type Action struct {
	// Type is the type of action.
	// +kubebuilder:validation:Enum=accept;reject;next
	Type ActionType `json:"type"`
	// ModifyRoute is the modify route action.
	ModifyRoute *ModifyRouteAction `json:"modifyRoute,omitempty"`
}

// ActionType represents the type of action.
type ActionType string

const (
	// Accept represents an accept action.
	Accept ActionType = "accept"
	// Reject represents a reject action.
	Reject ActionType = "reject"
	// Next represents a next action.
	Next ActionType = "next"
)

// ModifyRouteAction represents a modify route action.
type ModifyRouteAction struct {
	// AddCommunity is the community to add to the route.
	AddCommunity string `json:"addCommunity"`
}

// StaticRoute represents a static route configuration.
type StaticRoute struct {
	// Prefix is the prefix for the static route.
	Prefix string `json:"prefix"`
	// NextHop is the next hop for the static route.
	NextHop NextHop `json:"nextHop"`
	// BFDProfile is the BFD profile for the static route.
	BFDProfile *BFDProfile `json:"bfdProfile,omitempty"`
}

// TrafficMatch represents a traffic match configuration.
type TrafficMatch struct {
	// SrcPrefix is the source prefix to match.
	SrcPrefix *string `json:"srcPrefix,omitempty"`
	// DstPrefix is the destination prefix to match.
	DstPrefix *string `json:"dstPrefix,omitempty"`
	// SrcPort is the source port to match.
	SrcPort *uint16 `json:"srcPort,omitempty"`
	// DstPort is the destination port to match.
	DstPort *uint16 `json:"dstPort,omitempty"`
	// Protocol is the protocol to match.
	Protocol *string `json:"protocol,omitempty"`
	// Layer2 is the Layer2 to match.
	Layer2 *string `json:"layer2,omitempty"`
}

// PolicyRoute represents a policy-based route configuration.
type PolicyRoute struct {
	// TrafficMatch is the traffic match for the policy route.
	TrafficMatch TrafficMatch `json:"trafficMatch"`
	// NextHop is the next hop for the policy route.
	NextHop NextHop `json:"nextHop"`
}

type EncapsulationType string

const (
	EncapsulationTypeGRE EncapsulationType = "gre"
)

// MirrorACL represents a mirror ACL configuration.
type MirrorACL struct {
	// TrafficMatch is the traffic match for the mirror ACL.
	TrafficMatch TrafficMatch `json:"trafficMatch"`
	// DestinationAddress is the destination address for the mirrored traffic.
	DestinationAddress string `json:"destinationAddress"`
	// DestinationVrf is the destination VRF for the mirrored traffic.
	DestinationVrf string `json:"destinationVrf"`
	// EncapsulationType is the encapsulation type for the mirrored traffic.
	// +kubebuilder:validation:Enum=gre
	EncapsulationType EncapsulationType `json:"encapsulationType"`
}

// NextHop represents a next hop configuration.
type NextHop struct {
	// Address is the address of the next hop.
	Address *string `json:"address,omitempty"`
	// Vrf is the VRF of the next hop.
	Vrf *string `json:"vrf,omitempty"`
}

// HBRConfigStatus defines the observed state of HBRConfig
type HBRConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=hbrconfig,scope=Cluster

// HBRConfig is the Schema for the hbrconfigs API
type HBRConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HBRConfigSpec   `json:"spec,omitempty"`
	Status HBRConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HBRConfigList contains a list of HBRConfig
type HBRConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HBRConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HBRConfig{}, &HBRConfigList{})
}
