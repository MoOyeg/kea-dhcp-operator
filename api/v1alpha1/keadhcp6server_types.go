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

// ========== DHCPv6-specific Types ==========

// Pool6 defines an IPv6 address pool for dynamic allocation.
type Pool6 struct {
	// Address range (e.g., "2001:db8:1::1 - 2001:db8:1::ffff").
	// +kubebuilder:validation:Required
	Pool string `json:"pool"`

	// Pool-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Client class required to use this pool.
	// +optional
	ClientClass string `json:"client-class,omitempty"`

	// Client classes that must be evaluated.
	// +optional
	RequireClientClasses []string `json:"require-client-classes,omitempty"`
}

// PDPool defines a prefix delegation pool (IPv6-specific).
type PDPool struct {
	// Delegated prefix (e.g., "2001:db8:8::").
	// +kubebuilder:validation:Required
	Prefix string `json:"prefix"`

	// Length of the prefix in bits.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=128
	PrefixLen int32 `json:"prefix-len"`

	// Length of the delegated prefix in bits.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=128
	DelegatedLen int32 `json:"delegated-len"`

	// Excluded prefix.
	// +optional
	ExcludedPrefix string `json:"excluded-prefix,omitempty"`

	// Excluded prefix length.
	// +optional
	ExcludedPrefixLen *int32 `json:"excluded-prefix-len,omitempty"`

	// Pool-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Client class required to use this pool.
	// +optional
	ClientClass string `json:"client-class,omitempty"`
}

// Subnet6 defines a DHCPv6 subnet configuration.
type Subnet6 struct {
	// Unique subnet identifier.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	ID int32 `json:"id"`

	// Subnet prefix in CIDR notation (e.g., "2001:db8:1::/64").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[a-fA-F0-9:]+/[0-9]{1,3}$`
	Subnet string `json:"subnet"`

	// Address pools for dynamic allocation.
	// +optional
	Pools []Pool6 `json:"pools,omitempty"`

	// Prefix delegation pools.
	// +optional
	PDPools []PDPool `json:"pd-pools,omitempty"`

	// Subnet-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Host reservations within this subnet.
	// +optional
	Reservations []Reservation `json:"reservations,omitempty"`

	// Client class required for this subnet.
	// +optional
	ClientClass string `json:"client-class,omitempty"`

	// Client classes to evaluate.
	// +optional
	RequireClientClasses []string `json:"require-client-classes,omitempty"`

	// Network interface to listen on.
	// +optional
	Interface string `json:"interface,omitempty"`

	// Interface ID for relay-supplied interface identification.
	// +optional
	InterfaceID string `json:"interface-id,omitempty"`

	// Relay agent configuration.
	// +optional
	Relay *RelayInfo `json:"relay,omitempty"`

	// Preferred lifetime in seconds (T_pref).
	// +optional
	PreferredLifetime *int32 `json:"preferred-lifetime,omitempty"`

	// Valid lease lifetime in seconds.
	// +optional
	ValidLifetime *int32 `json:"valid-lifetime,omitempty"`

	// Minimum valid lifetime.
	// +optional
	MinValidLifetime *int32 `json:"min-valid-lifetime,omitempty"`

	// Maximum valid lifetime.
	// +optional
	MaxValidLifetime *int32 `json:"max-valid-lifetime,omitempty"`

	// Renew timer (T1) in seconds.
	// +optional
	RenewTimer *int32 `json:"renew-timer,omitempty"`

	// Rebind timer (T2) in seconds.
	// +optional
	RebindTimer *int32 `json:"rebind-timer,omitempty"`

	// Enable rapid commit (2-message exchange).
	// +optional
	RapidCommit *bool `json:"rapid-commit,omitempty"`

	// Host reservation mode.
	// +optional
	// +kubebuilder:validation:Enum=all;out-of-pool;global;disabled
	ReservationMode string `json:"reservation-mode,omitempty"`
}

// SharedNetwork6 defines a shared network grouping multiple IPv6 subnets.
type SharedNetwork6 struct {
	// Shared network name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Subnets belonging to this shared network.
	// +optional
	Subnet6 []Subnet6 `json:"subnet6,omitempty"`

	// Network interface.
	// +optional
	Interface string `json:"interface,omitempty"`

	// Interface ID.
	// +optional
	InterfaceID string `json:"interface-id,omitempty"`

	// Network-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Relay agent configuration.
	// +optional
	Relay *RelayInfo `json:"relay,omitempty"`

	// Client class required for this network.
	// +optional
	ClientClass string `json:"client-class,omitempty"`

	// Preferred lifetime in seconds.
	// +optional
	PreferredLifetime *int32 `json:"preferred-lifetime,omitempty"`

	// Valid lease lifetime in seconds.
	// +optional
	ValidLifetime *int32 `json:"valid-lifetime,omitempty"`

	// Renew timer in seconds.
	// +optional
	RenewTimer *int32 `json:"renew-timer,omitempty"`

	// Rebind timer in seconds.
	// +optional
	RebindTimer *int32 `json:"rebind-timer,omitempty"`

	// Enable rapid commit.
	// +optional
	RapidCommit *bool `json:"rapid-commit,omitempty"`
}

// ========== KeaDhcp6Server Spec ==========

// KeaDhcp6ServerSpec defines the desired state of a Kea DHCPv6 server deployment.
type KeaDhcp6ServerSpec struct {
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

	// Enable host networking.
	// +optional
	HostNetwork *bool `json:"hostNetwork,omitempty"`

	// Network interface configuration.
	// +kubebuilder:validation:Required
	InterfacesConfig InterfacesConfig `json:"interfaces-config"`

	// Lease database configuration.
	// +optional
	LeaseDatabase *DatabaseConfig `json:"lease-database,omitempty"`

	// Host reservations database (single source).
	// +optional
	HostsDatabase *DatabaseConfig `json:"hosts-database,omitempty"`

	// Host reservations databases (multiple sources).
	// +optional
	HostsDatabases []DatabaseConfig `json:"hosts-databases,omitempty"`

	// DHCPv6 subnet definitions.
	// +optional
	Subnet6 []Subnet6 `json:"subnet6,omitempty"`

	// Shared networks.
	// +optional
	SharedNetworks []SharedNetwork6 `json:"shared-networks,omitempty"`

	// Global DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Custom option definitions.
	// +optional
	OptionDef []OptionDef `json:"option-def,omitempty"`

	// Client classification rules.
	// +optional
	ClientClasses []ClientClass `json:"client-classes,omitempty"`

	// Global host reservations.
	// +optional
	Reservations []Reservation `json:"reservations,omitempty"`

	// Host reservation identifier order.
	// +optional
	HostReservationIdentifiers []string `json:"host-reservation-identifiers,omitempty"`

	// Hook libraries to load.
	// +optional
	HooksLibraries []HookLibrary `json:"hooks-libraries,omitempty"`

	// Control socket configuration.
	// +optional
	ControlSocket *ControlSocket `json:"control-socket,omitempty"`

	// Logging configuration.
	// +optional
	Loggers []LoggerConfig `json:"loggers,omitempty"`

	// Default preferred lifetime in seconds.
	// +optional
	PreferredLifetime *int32 `json:"preferred-lifetime,omitempty"`

	// Default valid lease lifetime in seconds.
	// +optional
	// +kubebuilder:default=4000
	ValidLifetime *int32 `json:"valid-lifetime,omitempty"`

	// Minimum valid lifetime.
	// +optional
	MinValidLifetime *int32 `json:"min-valid-lifetime,omitempty"`

	// Maximum valid lifetime.
	// +optional
	MaxValidLifetime *int32 `json:"max-valid-lifetime,omitempty"`

	// Renew timer (T1) in seconds.
	// +optional
	RenewTimer *int32 `json:"renew-timer,omitempty"`

	// Rebind timer (T2) in seconds.
	// +optional
	RebindTimer *int32 `json:"rebind-timer,omitempty"`

	// Automatically calculate T1 and T2.
	// +optional
	CalculateTeeTimes *bool `json:"calculate-tee-times,omitempty"`

	// T1 percentage (0.0-1.0).
	// +optional
	T1Percent *string `json:"t1-percent,omitempty"`

	// T2 percentage (0.0-1.0).
	// +optional
	T2Percent *string `json:"t2-percent,omitempty"`

	// Server tag for config backend.
	// +optional
	ServerTag string `json:"server-tag,omitempty"`

	// Enable rapid commit (2-message exchange).
	// +optional
	RapidCommit *bool `json:"rapid-commit,omitempty"`

	// DDNS send updates.
	// +optional
	DDNSSendUpdates *bool `json:"ddns-send-updates,omitempty"`

	// DDNS qualifying suffix.
	// +optional
	DDNSQualifyingSuffix string `json:"ddns-qualifying-suffix,omitempty"`

	// High Availability configuration.
	// +optional
	HighAvailability *HAConfig `json:"high-availability,omitempty"`

	// Multi-threading configuration.
	// +optional
	MultiThreading *MultiThreadingConfig `json:"multi-threading,omitempty"`

	// Stork agent sidecar for monitoring and Prometheus metrics.
	// When enabled, a stork-agent container is injected alongside the Kea daemon.
	// +optional
	Stork *StorkAgentConfig `json:"stork,omitempty"`
}

// KeaDhcp6ServerStatus defines the observed state of KeaDhcp6Server.
type KeaDhcp6ServerStatus struct {
	ComponentStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=kd6

// KeaDhcp6Server is the Schema for the keadhcp6servers API.
type KeaDhcp6Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeaDhcp6ServerSpec   `json:"spec,omitempty"`
	Status KeaDhcp6ServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KeaDhcp6ServerList contains a list of KeaDhcp6Server.
type KeaDhcp6ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeaDhcp6Server `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeaDhcp6Server{}, &KeaDhcp6ServerList{})
}
