/*
Copyright 2026.

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

// KeaServerSpec is a convenience resource that deploys multiple Kea components
// via a single custom resource. Each non-nil component section causes the
// operator to create the corresponding component CRD.
type KeaServerSpec struct {
	// DHCPv4 server configuration. When set, creates a KeaDhcp4Server.
	// +optional
	Dhcp4 *KeaDhcp4ServerSpec `json:"dhcp4,omitempty"`

	// DHCPv6 server configuration. When set, creates a KeaDhcp6Server.
	// +optional
	Dhcp6 *KeaDhcp6ServerSpec `json:"dhcp6,omitempty"`

	// Control Agent configuration. When set, creates a KeaControlAgent.
	// +optional
	ControlAgent *KeaControlAgentSpec `json:"controlAgent,omitempty"`

	// DHCP-DDNS configuration. When set, creates a KeaDhcpDdns.
	// +optional
	DhcpDdns *KeaDhcpDdnsSpec `json:"dhcpDdns,omitempty"`

	// Stork server configuration. When set, creates a KeaStorkServer
	// that provides a web UI for monitoring and managing Kea DHCP servers.
	// +optional
	StorkServer *KeaStorkServerSpec `json:"storkServer,omitempty"`
}

// KeaServerStatus aggregates status from all deployed Kea components.
type KeaServerStatus struct {
	// Current conditions.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []ConditionStatus `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// The generation last observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Current operational phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// Whether the DHCPv4 component is ready.
	// +optional
	Dhcp4Ready bool `json:"dhcp4Ready,omitempty"`

	// Whether the DHCPv6 component is ready.
	// +optional
	Dhcp6Ready bool `json:"dhcp6Ready,omitempty"`

	// Whether the Control Agent component is ready.
	// +optional
	ControlAgentReady bool `json:"controlAgentReady,omitempty"`

	// Whether the DHCP-DDNS component is ready.
	// +optional
	DhcpDdnsReady bool `json:"dhcpDdnsReady,omitempty"`

	// Whether the Stork server component is ready.
	// +optional
	StorkServerReady bool `json:"storkServerReady,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="DHCPv4",type="boolean",JSONPath=".status.dhcp4Ready"
// +kubebuilder:printcolumn:name="DHCPv6",type="boolean",JSONPath=".status.dhcp6Ready"
// +kubebuilder:printcolumn:name="Agent",type="boolean",JSONPath=".status.controlAgentReady"
// +kubebuilder:printcolumn:name="DDNS",type="boolean",JSONPath=".status.dhcpDdnsReady"
// +kubebuilder:printcolumn:name="Stork",type="boolean",JSONPath=".status.storkServerReady"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=ks

// KeaServer is the Schema for the keaservers API.
// It is a convenience umbrella resource that deploys any combination
// of Kea DHCP components (DHCPv4, DHCPv6, Control Agent, DDNS).
type KeaServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeaServerSpec   `json:"spec,omitempty"`
	Status KeaServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KeaServerList contains a list of KeaServer.
type KeaServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeaServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeaServer{}, &KeaServerList{})
}
