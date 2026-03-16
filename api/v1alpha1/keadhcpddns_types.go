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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ========== DDNS Types ==========

// TSIGKey defines a TSIG key for authenticated DNS updates (RFC 2845).
type TSIGKey struct {
	// TSIG key name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// TSIG algorithm (e.g., "HMAC-MD5", "HMAC-SHA256", "HMAC-SHA512").
	// +kubebuilder:validation:Required
	Algorithm string `json:"algorithm"`

	// Number of bits for digest truncation. 0 means no truncation.
	// +optional
	DigestBits *int32 `json:"digest-bits,omitempty"`

	// Reference to a Secret key containing the base64-encoded TSIG secret.
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty"`
}

// DDNSConfig holds forward or reverse DDNS domain configuration.
type DDNSConfig struct {
	// List of DDNS domains.
	// +optional
	DDNSDomains []DDNSDomain `json:"ddns-domains,omitempty"`
}

// DDNSDomain defines a DNS domain for dynamic updates.
type DDNSDomain struct {
	// DNS domain name (e.g., "example.com." — note trailing dot).
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Name of the TSIG key to use for this domain.
	// +optional
	KeyName string `json:"key-name,omitempty"`

	// DNS servers to send updates to.
	// +optional
	DNSServers []DNSServer `json:"dns-servers,omitempty"`
}

// DNSServer defines a DNS server endpoint for DDNS updates.
type DNSServer struct {
	// IP address of the DNS server.
	// +kubebuilder:validation:Required
	IPAddress string `json:"ip-address"`

	// Port of the DNS server. Defaults to 53.
	// +optional
	Port *int32 `json:"port,omitempty"`
}

// ========== KeaDhcpDdns Spec ==========

// KeaDhcpDdnsSpec defines the desired state of a Kea DHCP-DDNS (D2) server deployment.
type KeaDhcpDdnsSpec struct {
	// Container configuration (image, resources, pull policy).
	// +optional
	Container ContainerConfig `json:"container,omitempty"`

	// Number of replicas.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Pod scheduling constraints.
	// +optional
	Placement *PodPlacement `json:"placement,omitempty"`

	// IP address to listen on for NCR messages from DHCP servers.
	// +optional
	// +kubebuilder:default="127.0.0.1"
	IPAddress string `json:"ip-address,omitempty"`

	// Port to listen on for NCR messages.
	// +optional
	// +kubebuilder:default=53001
	Port *int32 `json:"port,omitempty"`

	// DNS server timeout in milliseconds.
	// +optional
	// +kubebuilder:default=500
	DNSServerTimeout *int32 `json:"dns-server-timeout,omitempty"`

	// NCR protocol. Only "UDP" is currently supported by Kea.
	// +optional
	// +kubebuilder:default="UDP"
	// +kubebuilder:validation:Enum=UDP;TCP
	NCRProtocol string `json:"ncr-protocol,omitempty"`

	// NCR format. Only "JSON" is currently supported by Kea.
	// +optional
	// +kubebuilder:default="JSON"
	// +kubebuilder:validation:Enum=JSON
	NCRFormat string `json:"ncr-format,omitempty"`

	// TSIG keys for authenticated DNS updates.
	// +optional
	TSIGKeys []TSIGKey `json:"tsig-keys,omitempty"`

	// Forward DDNS domain configuration.
	// +optional
	ForwardDDNS *DDNSConfig `json:"forward-ddns,omitempty"`

	// Reverse DDNS domain configuration.
	// +optional
	ReverseDDNS *DDNSConfig `json:"reverse-ddns,omitempty"`

	// Control socket configuration for management commands.
	// +optional
	ControlSocket *ControlSocket `json:"control-socket,omitempty"`

	// Hook libraries to load.
	// +optional
	HooksLibraries []HookLibrary `json:"hooks-libraries,omitempty"`

	// Logging configuration.
	// +optional
	Loggers []LoggerConfig `json:"loggers,omitempty"`
}

// KeaDhcpDdnsStatus defines the observed state of KeaDhcpDdns.
type KeaDhcpDdnsStatus struct {
	ComponentStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=kdd

// KeaDhcpDdns is the Schema for the keadhcpddns API.
// It manages a Kea DHCP-DDNS (D2) server that performs dynamic DNS updates
// on behalf of the DHCP servers.
type KeaDhcpDdns struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeaDhcpDdnsSpec   `json:"spec,omitempty"`
	Status KeaDhcpDdnsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KeaDhcpDdnsList contains a list of KeaDhcpDdns.
type KeaDhcpDdnsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeaDhcpDdns `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeaDhcpDdns{}, &KeaDhcpDdnsList{})
}
