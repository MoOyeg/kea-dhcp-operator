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

// StorkServerDatabase defines the PostgreSQL connection for the Stork server.
type StorkServerDatabase struct {
	// PostgreSQL host address.
	// +kubebuilder:validation:Required
	Host string `json:"host"`

	// PostgreSQL port number.
	// +optional
	// +kubebuilder:default=5432
	Port *int32 `json:"port,omitempty"`

	// Database name.
	// +optional
	// +kubebuilder:default="stork"
	Name string `json:"name,omitempty"`

	// Reference to a Secret containing database credentials.
	// The Secret must have keys "username" and "password".
	// +kubebuilder:validation:Required
	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`

	// SSL mode for the PostgreSQL connection.
	// +optional
	// +kubebuilder:validation:Enum=disable;require;verify-ca;verify-full
	// +kubebuilder:default="disable"
	SSLMode string `json:"sslMode,omitempty"`
}

// KeaStorkServerSpec defines the desired state of KeaStorkServer.
type KeaStorkServerSpec struct {
	// Container image configuration for the Stork server.
	// +optional
	Container ContainerConfig `json:"container,omitempty"`

	// Number of Stork server replicas (typically 1).
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// PostgreSQL database connection configuration.
	// +kubebuilder:validation:Required
	Database StorkServerDatabase `json:"database"`

	// Port for the Stork server REST API and web UI.
	// +optional
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port *int32 `json:"port,omitempty"`

	// Enable the Prometheus metrics endpoint on the Stork server.
	// +optional
	// +kubebuilder:default=true
	EnableMetrics *bool `json:"enableMetrics,omitempty"`

	// Pod scheduling constraints and metadata.
	// +optional
	Placement *PodPlacement `json:"placement,omitempty"`
}

// KeaStorkServerStatus defines the observed state of KeaStorkServer.
type KeaStorkServerStatus struct {
	ComponentStatus `json:",inline"`

	// The URL of the Stork server web UI (set when Route is created on OpenShift).
	// +optional
	URL string `json:"url,omitempty"`

	// The name of the Secret containing admin credentials (username and password keys).
	// +optional
	AdminSecretName string `json:"adminSecretName,omitempty"`

	// The name of the Secret containing the server agent token (key "token").
	// Stork agents use this token to register with the server.
	// +optional
	ServerTokenSecretName string `json:"serverTokenSecretName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".status.url"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=kss

// KeaStorkServer is the Schema for the keastorkservers API.
// It deploys an ISC Stork server that provides a web UI for monitoring
// and managing Kea DHCP servers, including lease browsing, subnet utilization,
// HA status, and configuration management.
type KeaStorkServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeaStorkServerSpec   `json:"spec,omitempty"`
	Status KeaStorkServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KeaStorkServerList contains a list of KeaStorkServer.
type KeaStorkServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeaStorkServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeaStorkServer{}, &KeaStorkServerList{})
}
