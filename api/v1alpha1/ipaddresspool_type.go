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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// IPAddressPoolSpec defines the desired state of IPAddressPool.
type IPAddressPoolSpec struct {
	// IPv4 CIDR of the pool
	IPv4 string `json:"ipv4"`

	// IPv6 CIDR of the pool
	IPv6 string `json:"ipv6"`
}

// IPAddressPoolStatus defines the observed state of IPAddressPool.
type IPAddressPoolStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=ipaddrpool,scope=Cluster
//+kubebuilder:printcolumn:name="IPv4",type=string,JSONPath=`.spec.ipv4`
//+kubebuilder:printcolumn:name="IPv6",type=string,JSONPath=`.spec.ipv6`

// IPAddressPool is the Schema for the IPAddressPools API.
type IPAddressPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPAddressPoolSpec   `json:"spec,omitempty"`
	Status IPAddressPoolStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IPAddressPoolList contains a list of IPAddressPool.
type IPAddressPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPAddressPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPAddressPool{}, &IPAddressPoolList{})
}
