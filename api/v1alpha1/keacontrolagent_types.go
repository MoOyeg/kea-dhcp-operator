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

// ========== Control Agent Types ==========

// AgentControlSockets defines the UNIX control sockets for each managed Kea daemon.
type AgentControlSockets struct {
	// Control socket for the DHCPv4 server.
	// +optional
	Dhcp4 *ControlSocket `json:"dhcp4,omitempty"`

	// Control socket for the DHCPv6 server.
	// +optional
	Dhcp6 *ControlSocket `json:"dhcp6,omitempty"`

	// Control socket for the DHCP-DDNS (D2) server.
	// +optional
	D2 *ControlSocket `json:"d2,omitempty"`
}

// AuthConfig defines authentication settings for the Control Agent REST API.
type AuthConfig struct {
	// Authentication type. Currently only "basic" is supported.
	// +optional
	// +kubebuilder:validation:Enum=basic
	Type string `json:"type,omitempty"`

	// Authentication realm.
	// +optional
	Realm string `json:"realm,omitempty"`

	// Inline client credentials. Prefer credentialsSecretRef for production.
	// +optional
	Clients []AuthClient `json:"clients,omitempty"`

	// Reference to a Secret containing authentication credentials.
	// The Secret should contain key-value pairs where keys are usernames and values are passwords.
	// +optional
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`
}

// AuthClient defines a single authentication client credential.
type AuthClient struct {
	// Username for authentication.
	// +kubebuilder:validation:Required
	User string `json:"user"`

	// Reference to a Secret key containing the password.
	PasswordSecretKeyRef *corev1.SecretKeySelector `json:"passwordSecretKeyRef,omitempty"`
}

// ========== KeaControlAgent Spec ==========

// KeaControlAgentSpec defines the desired state of a Kea Control Agent deployment.
type KeaControlAgentSpec struct {
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

	// HTTP host address to listen on.
	// +optional
	// +kubebuilder:default="0.0.0.0"
	HTTPHost string `json:"http-host,omitempty"`

	// HTTP port to listen on.
	// +optional
	// +kubebuilder:default=8000
	HTTPPort *int32 `json:"http-port,omitempty"`

	// TLS configuration for the REST API endpoint.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`

	// Control sockets for communicating with managed Kea daemons.
	// +optional
	ControlSockets *AgentControlSockets `json:"control-sockets,omitempty"`

	// Authentication configuration for the REST API.
	// +optional
	Authentication *AuthConfig `json:"authentication,omitempty"`

	// Hook libraries to load.
	// +optional
	HooksLibraries []HookLibrary `json:"hooks-libraries,omitempty"`

	// Logging configuration.
	// +optional
	Loggers []LoggerConfig `json:"loggers,omitempty"`
}

// KeaControlAgentStatus defines the observed state of KeaControlAgent.
type KeaControlAgentStatus struct {
	ComponentStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Host",type="string",JSONPath=".spec.http-host"
// +kubebuilder:printcolumn:name="Port",type="integer",JSONPath=".spec.http-port"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=kca

// KeaControlAgent is the Schema for the keacontrolagents API.
// It manages a Kea Control Agent deployment that provides a REST API
// for managing Kea DHCP servers.
type KeaControlAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeaControlAgentSpec   `json:"spec,omitempty"`
	Status KeaControlAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KeaControlAgentList contains a list of KeaControlAgent.
type KeaControlAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeaControlAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeaControlAgent{}, &KeaControlAgentList{})
}
