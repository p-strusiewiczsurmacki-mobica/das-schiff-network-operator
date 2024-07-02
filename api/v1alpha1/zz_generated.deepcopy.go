//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Layer2NetworkConfiguration) DeepCopyInto(out *Layer2NetworkConfiguration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Layer2NetworkConfiguration.
func (in *Layer2NetworkConfiguration) DeepCopy() *Layer2NetworkConfiguration {
	if in == nil {
		return nil
	}
	out := new(Layer2NetworkConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Layer2NetworkConfiguration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Layer2NetworkConfigurationList) DeepCopyInto(out *Layer2NetworkConfigurationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Layer2NetworkConfiguration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Layer2NetworkConfigurationList.
func (in *Layer2NetworkConfigurationList) DeepCopy() *Layer2NetworkConfigurationList {
	if in == nil {
		return nil
	}
	out := new(Layer2NetworkConfigurationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Layer2NetworkConfigurationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Layer2NetworkConfigurationSpec) DeepCopyInto(out *Layer2NetworkConfigurationSpec) {
	*out = *in
	if in.AnycastGateways != nil {
		in, out := &in.AnycastGateways, &out.AnycastGateways
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.NeighSuppression != nil {
		in, out := &in.NeighSuppression, &out.NeighSuppression
		*out = new(bool)
		**out = **in
	}
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Layer2NetworkConfigurationSpec.
func (in *Layer2NetworkConfigurationSpec) DeepCopy() *Layer2NetworkConfigurationSpec {
	if in == nil {
		return nil
	}
	out := new(Layer2NetworkConfigurationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Layer2NetworkConfigurationStatus) DeepCopyInto(out *Layer2NetworkConfigurationStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Layer2NetworkConfigurationStatus.
func (in *Layer2NetworkConfigurationStatus) DeepCopy() *Layer2NetworkConfigurationStatus {
	if in == nil {
		return nil
	}
	out := new(Layer2NetworkConfigurationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfig) DeepCopyInto(out *NodeConfig) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfig.
func (in *NodeConfig) DeepCopy() *NodeConfig {
	if in == nil {
		return nil
	}
	out := new(NodeConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NodeConfig) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigList) DeepCopyInto(out *NodeConfigList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NodeConfig, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigList.
func (in *NodeConfigList) DeepCopy() *NodeConfigList {
	if in == nil {
		return nil
	}
	out := new(NodeConfigList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NodeConfigList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigRevision) DeepCopyInto(out *NodeConfigRevision) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigRevision.
func (in *NodeConfigRevision) DeepCopy() *NodeConfigRevision {
	if in == nil {
		return nil
	}
	out := new(NodeConfigRevision)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NodeConfigRevision) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigRevisionList) DeepCopyInto(out *NodeConfigRevisionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NodeConfigRevision, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigRevisionList.
func (in *NodeConfigRevisionList) DeepCopy() *NodeConfigRevisionList {
	if in == nil {
		return nil
	}
	out := new(NodeConfigRevisionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NodeConfigRevisionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigRevisionSpec) DeepCopyInto(out *NodeConfigRevisionSpec) {
	*out = *in
	in.Config.DeepCopyInto(&out.Config)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigRevisionSpec.
func (in *NodeConfigRevisionSpec) DeepCopy() *NodeConfigRevisionSpec {
	if in == nil {
		return nil
	}
	out := new(NodeConfigRevisionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigRevisionStatus) DeepCopyInto(out *NodeConfigRevisionStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigRevisionStatus.
func (in *NodeConfigRevisionStatus) DeepCopy() *NodeConfigRevisionStatus {
	if in == nil {
		return nil
	}
	out := new(NodeConfigRevisionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigSpec) DeepCopyInto(out *NodeConfigSpec) {
	*out = *in
	if in.Layer2 != nil {
		in, out := &in.Layer2, &out.Layer2
		*out = make([]Layer2NetworkConfigurationSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Vrf != nil {
		in, out := &in.Vrf, &out.Vrf
		*out = make([]VRFRouteConfigurationSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.RoutingTable != nil {
		in, out := &in.RoutingTable, &out.RoutingTable
		*out = make([]RoutingTableSpec, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigSpec.
func (in *NodeConfigSpec) DeepCopy() *NodeConfigSpec {
	if in == nil {
		return nil
	}
	out := new(NodeConfigSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeConfigStatus) DeepCopyInto(out *NodeConfigStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeConfigStatus.
func (in *NodeConfigStatus) DeepCopy() *NodeConfigStatus {
	if in == nil {
		return nil
	}
	out := new(NodeConfigStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RoutingTable) DeepCopyInto(out *RoutingTable) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RoutingTable.
func (in *RoutingTable) DeepCopy() *RoutingTable {
	if in == nil {
		return nil
	}
	out := new(RoutingTable)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *RoutingTable) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RoutingTableList) DeepCopyInto(out *RoutingTableList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]RoutingTable, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RoutingTableList.
func (in *RoutingTableList) DeepCopy() *RoutingTableList {
	if in == nil {
		return nil
	}
	out := new(RoutingTableList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *RoutingTableList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RoutingTableSpec) DeepCopyInto(out *RoutingTableSpec) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RoutingTableSpec.
func (in *RoutingTableSpec) DeepCopy() *RoutingTableSpec {
	if in == nil {
		return nil
	}
	out := new(RoutingTableSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RoutingTableStatus) DeepCopyInto(out *RoutingTableStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RoutingTableStatus.
func (in *RoutingTableStatus) DeepCopy() *RoutingTableStatus {
	if in == nil {
		return nil
	}
	out := new(RoutingTableStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VRFRouteConfiguration) DeepCopyInto(out *VRFRouteConfiguration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VRFRouteConfiguration.
func (in *VRFRouteConfiguration) DeepCopy() *VRFRouteConfiguration {
	if in == nil {
		return nil
	}
	out := new(VRFRouteConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *VRFRouteConfiguration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VRFRouteConfigurationList) DeepCopyInto(out *VRFRouteConfigurationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]VRFRouteConfiguration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VRFRouteConfigurationList.
func (in *VRFRouteConfigurationList) DeepCopy() *VRFRouteConfigurationList {
	if in == nil {
		return nil
	}
	out := new(VRFRouteConfigurationList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *VRFRouteConfigurationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VRFRouteConfigurationSpec) DeepCopyInto(out *VRFRouteConfigurationSpec) {
	*out = *in
	if in.Import != nil {
		in, out := &in.Import, &out.Import
		*out = make([]VrfRouteConfigurationPrefixItem, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Export != nil {
		in, out := &in.Export, &out.Export
		*out = make([]VrfRouteConfigurationPrefixItem, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Aggregate != nil {
		in, out := &in.Aggregate, &out.Aggregate
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Community != nil {
		in, out := &in.Community, &out.Community
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VRFRouteConfigurationSpec.
func (in *VRFRouteConfigurationSpec) DeepCopy() *VRFRouteConfigurationSpec {
	if in == nil {
		return nil
	}
	out := new(VRFRouteConfigurationSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VRFRouteConfigurationStatus) DeepCopyInto(out *VRFRouteConfigurationStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VRFRouteConfigurationStatus.
func (in *VRFRouteConfigurationStatus) DeepCopy() *VRFRouteConfigurationStatus {
	if in == nil {
		return nil
	}
	out := new(VRFRouteConfigurationStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *VrfRouteConfigurationPrefixItem) DeepCopyInto(out *VrfRouteConfigurationPrefixItem) {
	*out = *in
	if in.GE != nil {
		in, out := &in.GE, &out.GE
		*out = new(int)
		**out = **in
	}
	if in.LE != nil {
		in, out := &in.LE, &out.LE
		*out = new(int)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new VrfRouteConfigurationPrefixItem.
func (in *VrfRouteConfigurationPrefixItem) DeepCopy() *VrfRouteConfigurationPrefixItem {
	if in == nil {
		return nil
	}
	out := new(VrfRouteConfigurationPrefixItem)
	in.DeepCopyInto(out)
	return out
}
