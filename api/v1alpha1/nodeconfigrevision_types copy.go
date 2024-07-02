/*
Copyright 2024.

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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeConfigSpec defines the desired state of NodeConfig.
type NodeConfigRevisionSpec struct {
	Revision string
	Config   NodeConfig
	Hash     string
}

type NodeConfigRevisionStatus struct {
	IsInvalid bool
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:shortName=ncr,scope=Cluster

// NodeConfig is the Schema for the node configuration.
type NodeConfigRevision struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeConfigRevisionSpec   `json:"spec,omitempty"`
	Status NodeConfigRevisionStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NodeConfigList contains a list of NodeConfig.
type NodeConfigRevisionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeConfigRevision `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeConfig{}, &NodeConfigList{})
}

func NewRevision(config *NodeConfig) (*NodeConfigRevision, error) {
	data, err := json.Marshal(config.Spec)
	if err != nil {
		return nil, fmt.Errorf("error marshalling data: %w", err)
	}

	h := md5.New()
	h.Write(data)
	hash := h.Sum(nil)
	hashHex := hex.EncodeToString(hash)

	return &NodeConfigRevision{
		ObjectMeta: metav1.ObjectMeta{Name: hashHex[:10]},
		Spec: NodeConfigRevisionSpec{
			Config: *config,
			Hash:   hashHex,
		},
		Status: NodeConfigRevisionStatus{
			IsInvalid: false,
		},
	}, nil
}
