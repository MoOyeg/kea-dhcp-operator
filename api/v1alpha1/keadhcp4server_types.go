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

// ========== DHCPv4-specific Types ==========

// Pool4 defines an IPv4 address pool for dynamic allocation.
type Pool4 struct {
	// Address range (e.g., "192.0.2.1 - 192.0.2.200" or "192.0.2.0/26").
	// +kubebuilder:validation:Required
	Pool string `json:"pool"`

	// Pool-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Client class required to use this pool.
	// +optional
	ClientClass string `json:"client-class,omitempty"`

	// Client classes that must be evaluated for this pool.
	// +optional
	RequireClientClasses []string `json:"require-client-classes,omitempty"`
}

// Subnet4 defines a DHCPv4 subnet configuration.
type Subnet4 struct {
	// Unique subnet identifier. Must be positive.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	ID int32 `json:"id"`

	// Subnet prefix in CIDR notation (e.g., "192.168.1.0/24").
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/[0-9]{1,2}$`
	Subnet string `json:"subnet"`

	// Address pools for dynamic allocation.
	// +optional
	Pools []Pool4 `json:"pools,omitempty"`

	// Subnet-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Host reservations within this subnet.
	// +optional
	Reservations []Reservation `json:"reservations,omitempty"`

	// Client class required to use this subnet.
	// +optional
	ClientClass string `json:"client-class,omitempty"`

	// Client classes to evaluate for this subnet.
	// +optional
	RequireClientClasses []string `json:"require-client-classes,omitempty"`

	// Network interface to listen on for this subnet.
	// +optional
	Interface string `json:"interface,omitempty"`

	// Relay agent configuration.
	// +optional
	Relay *RelayInfo `json:"relay,omitempty"`

	// Valid lease lifetime in seconds for this subnet.
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

	// Whether this server is authoritative for the subnet.
	// +optional
	Authoritative *bool `json:"authoritative,omitempty"`

	// Host reservation mode.
	// +optional
	// +kubebuilder:validation:Enum=all;out-of-pool;global;disabled
	ReservationMode string `json:"reservation-mode,omitempty"`

	// Match client by client-id.
	// +optional
	MatchClientID *bool `json:"match-client-id,omitempty"`

	// Next server address for PXE boot.
	// +optional
	NextServer string `json:"next-server,omitempty"`

	// Boot file name for PXE boot.
	// +optional
	BootFileName string `json:"boot-file-name,omitempty"`

	// Server hostname for PXE boot.
	// +optional
	ServerHostname string `json:"server-hostname,omitempty"`

	// Arbitrary user context data.
	// +optional
	UserContext string `json:"user-context,omitempty"`
}

// SharedNetwork4 defines a shared network grouping multiple IPv4 subnets.
type SharedNetwork4 struct {
	// Shared network name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Subnets belonging to this shared network.
	// +optional
	Subnet4 []Subnet4 `json:"subnet4,omitempty"`

	// Shared network interface.
	// +optional
	Interface string `json:"interface,omitempty"`

	// Network-level DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Relay agent configuration.
	// +optional
	Relay *RelayInfo `json:"relay,omitempty"`

	// Client class required for this network.
	// +optional
	ClientClass string `json:"client-class,omitempty"`

	// Client classes to evaluate.
	// +optional
	RequireClientClasses []string `json:"require-client-classes,omitempty"`

	// Valid lease lifetime in seconds.
	// +optional
	ValidLifetime *int32 `json:"valid-lifetime,omitempty"`

	// Minimum valid lifetime.
	// +optional
	MinValidLifetime *int32 `json:"min-valid-lifetime,omitempty"`

	// Maximum valid lifetime.
	// +optional
	MaxValidLifetime *int32 `json:"max-valid-lifetime,omitempty"`

	// Renew timer in seconds.
	// +optional
	RenewTimer *int32 `json:"renew-timer,omitempty"`

	// Rebind timer in seconds.
	// +optional
	RebindTimer *int32 `json:"rebind-timer,omitempty"`

	// Authoritative flag.
	// +optional
	Authoritative *bool `json:"authoritative,omitempty"`
}

// ========== KeaDhcp4Server Spec ==========

// KeaDhcp4ServerSpec defines the desired state of a Kea DHCPv4 server deployment.
type KeaDhcp4ServerSpec struct {
	// ===== Container/Deployment Settings =====

	// Container configuration (image, resources, pull policy).
	// +optional
	Container ContainerConfig `json:"container,omitempty"`

	// Number of replicas. For HA, typically set to 2.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Pod scheduling constraints.
	// +optional
	Placement *PodPlacement `json:"placement,omitempty"`

	// Enable host networking. Required for DHCP in some network topologies.
	// +optional
	HostNetwork *bool `json:"hostNetwork,omitempty"`

	// ===== Kea DHCPv4 Configuration =====

	// Network interface configuration. Required.
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

	// DHCPv4 subnet definitions.
	// +optional
	Subnet4 []Subnet4 `json:"subnet4,omitempty"`

	// Shared networks grouping related subnets.
	// +optional
	SharedNetworks []SharedNetwork4 `json:"shared-networks,omitempty"`

	// Global DHCP option data.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Custom option definitions.
	// +optional
	OptionDef []OptionDef `json:"option-def,omitempty"`

	// Client classification rules.
	// +optional
	ClientClasses []ClientClass `json:"client-classes,omitempty"`

	// Global host reservations (outside of subnets).
	// +optional
	Reservations []Reservation `json:"reservations,omitempty"`

	// Host reservation identifier order.
	// +optional
	HostReservationIdentifiers []string `json:"host-reservation-identifiers,omitempty"`

	// Hook libraries to load.
	// +optional
	HooksLibraries []HookLibrary `json:"hooks-libraries,omitempty"`

	// Control socket configuration for management commands.
	// +optional
	ControlSocket *ControlSocket `json:"control-socket,omitempty"`

	// Logging configuration.
	// +optional
	Loggers []LoggerConfig `json:"loggers,omitempty"`

	// ===== Global Parameters =====

	// Default valid lease lifetime in seconds.
	// +optional
	// +kubebuilder:default=4000
	ValidLifetime *int32 `json:"valid-lifetime,omitempty"`

	// Minimum valid lease lifetime in seconds.
	// +optional
	MinValidLifetime *int32 `json:"min-valid-lifetime,omitempty"`

	// Maximum valid lease lifetime in seconds.
	// +optional
	MaxValidLifetime *int32 `json:"max-valid-lifetime,omitempty"`

	// Renew timer (T1) in seconds.
	// +optional
	RenewTimer *int32 `json:"renew-timer,omitempty"`

	// Rebind timer (T2) in seconds.
	// +optional
	RebindTimer *int32 `json:"rebind-timer,omitempty"`

	// Automatically calculate T1 and T2 as percentages of valid-lifetime.
	// +optional
	CalculateTeeTimes *bool `json:"calculate-tee-times,omitempty"`

	// T1 percentage of valid-lifetime (0.0-1.0). Used when calculate-tee-times is true.
	// +optional
	T1Percent *string `json:"t1-percent,omitempty"`

	// T2 percentage of valid-lifetime (0.0-1.0). Used when calculate-tee-times is true.
	// +optional
	T2Percent *string `json:"t2-percent,omitempty"`

	// Whether this server is authoritative for its subnets.
	// +optional
	Authoritative *bool `json:"authoritative,omitempty"`

	// Match client using client-id option.
	// +optional
	MatchClientID *bool `json:"match-client-id,omitempty"`

	// Server tag for multi-server configuration backend.
	// +optional
	ServerTag string `json:"server-tag,omitempty"`

	// Next server address for PXE boot (global).
	// +optional
	NextServer string `json:"next-server,omitempty"`

	// Boot file name for PXE boot (global).
	// +optional
	BootFileName string `json:"boot-file-name,omitempty"`

	// Server hostname (global).
	// +optional
	ServerHostname string `json:"server-hostname,omitempty"`

	// DDNS-related parameters.
	// +optional
	DDNSSendUpdates *bool `json:"ddns-send-updates,omitempty"`

	// DDNS override no update.
	// +optional
	DDNSOverrideNoUpdate *bool `json:"ddns-override-no-update,omitempty"`

	// DDNS override client update.
	// +optional
	DDNSOverrideClientUpdate *bool `json:"ddns-override-client-update,omitempty"`

	// DDNS replace client name mode.
	// +optional
	// +kubebuilder:validation:Enum=never;always;when-present;when-not-present
	DDNSReplaceClientName string `json:"ddns-replace-client-name,omitempty"`

	// DDNS generated prefix.
	// +optional
	DDNSGeneratedPrefix string `json:"ddns-generated-prefix,omitempty"`

	// DDNS qualifying suffix.
	// +optional
	DDNSQualifyingSuffix string `json:"ddns-qualifying-suffix,omitempty"`

	// Enable DDNS conflict resolution.
	// +optional
	DDNSUseConflictResolution *bool `json:"ddns-use-conflict-resolution,omitempty"`

	// ===== High Availability =====

	// High Availability configuration. When set, the operator automatically
	// configures the HA and lease_cmds hook libraries.
	// +optional
	HighAvailability *HAConfig `json:"high-availability,omitempty"`

	// ===== Multi-Threading =====

	// Enable multi-threading.
	// +optional
	MultiThreading *MultiThreadingConfig `json:"multi-threading,omitempty"`

	// ===== Monitoring =====

	// Stork agent sidecar for monitoring and Prometheus metrics.
	// When enabled, a stork-agent container is injected alongside the Kea daemon.
	// +optional
	Stork *StorkAgentConfig `json:"stork,omitempty"`
}

// MultiThreadingConfig defines multi-threading settings for Kea DHCP servers.
type MultiThreadingConfig struct {
	// Enable multi-threading.
	// +optional
	EnableMultiThreading *bool `json:"enable-multi-threading,omitempty"`

	// Thread pool size.
	// +optional
	ThreadPoolSize *int32 `json:"thread-pool-size,omitempty"`

	// Packet queue size.
	// +optional
	PacketQueueSize *int32 `json:"packet-queue-size,omitempty"`
}

// KeaDhcp4ServerStatus defines the observed state of KeaDhcp4Server.
type KeaDhcp4ServerStatus struct {
	ComponentStatus `json:",inline"`

	// NAD IP addresses assigned to each pod on the secondary network interface.
	// Indexed by pod ordinal (e.g., ["192.168.50.2", "192.168.50.3"]).
	// Only populated when a NAD interface and subnet are configured.
	// +optional
	NADAddresses []string `json:"nadAddresses,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=kd4

// KeaDhcp4Server is the Schema for the keadhcp4servers API.
// It manages a Kea DHCPv4 server deployment on Kubernetes or OpenShift.
type KeaDhcp4Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KeaDhcp4ServerSpec   `json:"spec,omitempty"`
	Status KeaDhcp4ServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KeaDhcp4ServerList contains a list of KeaDhcp4Server.
type KeaDhcp4ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KeaDhcp4Server `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeaDhcp4Server{}, &KeaDhcp4ServerList{})
}
