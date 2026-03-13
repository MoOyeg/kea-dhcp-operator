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

// HAConfig defines High Availability configuration for Kea DHCP servers.
// When specified, the operator automatically configures the HA hook library
// (libdhcp_ha.so) and the lease commands hook (libdhcp_lease_cmds.so).
type HAConfig struct {
	// Name of this server in the HA cluster. Must match one of the peer names.
	// +kubebuilder:validation:Required
	ThisServerName string `json:"this-server-name"`

	// HA operating mode.
	// +kubebuilder:validation:Enum=load-balancing;hot-standby
	// +kubebuilder:default="load-balancing"
	Mode string `json:"mode,omitempty"`

	// Heartbeat interval in milliseconds.
	// +optional
	// +kubebuilder:default=10000
	HeartbeatDelay *int32 `json:"heartbeat-delay,omitempty"`

	// Maximum time to wait for a response from a partner in milliseconds.
	// Should be greater than heartbeat-delay.
	// +optional
	// +kubebuilder:default=60000
	MaxResponseDelay *int32 `json:"max-response-delay,omitempty"`

	// Maximum time to wait for an acknowledgment in milliseconds.
	// +optional
	// +kubebuilder:default=10000
	MaxAckDelay *int32 `json:"max-ack-delay,omitempty"`

	// Maximum number of unacknowledged clients before failover.
	// Set to 0 for immediate failover on connection loss.
	// +optional
	// +kubebuilder:default=10
	MaxUnackedClients *int32 `json:"max-unacked-clients,omitempty"`

	// HA peers (at least 2: this server and partner).
	// +kubebuilder:validation:MinItems=2
	Peers []HAPeer `json:"peers"`

	// TLS configuration for HA peer communication.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`

	// Send lease updates to partner.
	// +optional
	SendLeaseUpdates *bool `json:"send-lease-updates,omitempty"`

	// Sync leases on startup.
	// +optional
	SyncLeases *bool `json:"sync-leases,omitempty"`

	// Sync timeout in milliseconds.
	// +optional
	SyncTimeout *int32 `json:"sync-timeout,omitempty"`

	// Sync page limit.
	// +optional
	SyncPageLimit *int32 `json:"sync-page-limit,omitempty"`

	// Delayed updates limit.
	// +optional
	DelayedUpdatesLimit *int32 `json:"delayed-updates-limit,omitempty"`

	// Multi-threading configuration.
	// +optional
	MultiThreading *HAMultiThreading `json:"multi-threading,omitempty"`
}

// HAPeer defines a peer in the Kea HA cluster.
type HAPeer struct {
	// Peer name. Must be unique in the HA cluster.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// URL of the peer's control agent (e.g., "http://peer:8000/").
	// If omitted, the operator auto-generates it from the StatefulSet headless
	// Service DNS name: http://<sts>-<ordinal>.<headless>.<ns>.svc.cluster.local:<port>/
	// +optional
	URL string `json:"url,omitempty"`

	// Peer role in the HA cluster.
	// +kubebuilder:validation:Enum=primary;secondary;standby;backup
	Role string `json:"role"`

	// Static IP address to assign to this peer's pod on the NAD interface.
	// Must be within the subnet but outside the DHCP pool range.
	// If omitted, the operator auto-assigns an IP based on the pod ordinal
	// (subnet base + ordinal + 2).
	// +optional
	Address string `json:"address,omitempty"`

	// Enable automatic failover for this peer.
	// +optional
	// +kubebuilder:default=true
	AutoFailover *bool `json:"auto-failover,omitempty"`
}

// HAMultiThreading defines multi-threading settings for HA.
type HAMultiThreading struct {
	// Enable multi-threading for HA.
	// +optional
	EnableMultiThreading *bool `json:"enable-multi-threading,omitempty"`

	// Number of HTTP client threads.
	// +optional
	HTTPClientThreads *int32 `json:"http-client-threads,omitempty"`

	// Number of HTTP listener threads.
	// +optional
	HTTPListenerThreads *int32 `json:"http-listener-threads,omitempty"`
}
