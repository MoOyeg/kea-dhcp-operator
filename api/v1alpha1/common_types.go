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
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ========== Database Configuration ==========

// DatabaseType defines the supported database backends.
// +kubebuilder:validation:Enum=memfile;mysql;postgresql
type DatabaseType string

const (
	DatabaseTypeMemfile    DatabaseType = "memfile"
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypePostgreSQL DatabaseType = "postgresql"
)

// LeaseStorageType selects where memfile leases are kept.
// +kubebuilder:validation:Enum=Ephemeral;ExistingClaim;ClaimTemplate
type LeaseStorageType string

const (
	// LeaseStorageEphemeral keeps memfile leases in an emptyDir, which is
	// removed with the pod. This is the default.
	LeaseStorageEphemeral LeaseStorageType = "Ephemeral"
	// LeaseStorageExistingClaim mounts a PersistentVolumeClaim the user
	// provides. Standalone servers only, with a single replica.
	LeaseStorageExistingClaim LeaseStorageType = "ExistingClaim"
	// LeaseStorageClaimTemplate gives each HA peer its own
	// PersistentVolumeClaim. HA servers only.
	LeaseStorageClaimTemplate LeaseStorageType = "ClaimTemplate"
)

// LeaseStorage selects the volume that holds the memfile lease file. It only
// applies to memfile leases; a MySQL or PostgreSQL lease database keeps
// leases outside the pods already.
type LeaseStorage struct {
	// Storage type. Ephemeral (default) loses the lease file when the pod is
	// replaced; ExistingClaim and ClaimTemplate keep it.
	// +optional
	// +kubebuilder:default=Ephemeral
	Type LeaseStorageType `json:"type,omitempty"`

	// Name of the PersistentVolumeClaim to mount (ExistingClaim). The operator
	// never creates, modifies or deletes it.
	// +optional
	ClaimName string `json:"claimName,omitempty"`

	// Requested size of each peer's claim (ClaimTemplate). Defaults to 1Gi.
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`

	// StorageClass of each peer's claim (ClaimTemplate). Defaults to the
	// cluster default StorageClass.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// DatabaseConfig defines database connection parameters used by lease-database and hosts-database.
type DatabaseConfig struct {
	// Type of the database backend.
	// +kubebuilder:validation:Required
	Type DatabaseType `json:"type"`

	// Persist leases to disk (memfile only).
	// +optional
	Persist *bool `json:"persist,omitempty"`

	// Database name or memfile path.
	// +optional
	Name string `json:"name,omitempty"`

	// Database host address.
	// +optional
	Host string `json:"host,omitempty"`

	// Database port number.
	// +optional
	Port *int32 `json:"port,omitempty"`

	// Reference to a Secret containing database credentials.
	// The Secret must have keys "username" and "password".
	// Required when type is "mysql" or "postgresql".
	// +optional
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`

	// Connection timeout in seconds.
	// +optional
	ConnectTimeout *int32 `json:"connect-timeout,omitempty"`

	// Maximum number of reconnect attempts.
	// +optional
	MaxReconnectTries *int32 `json:"max-reconnect-tries,omitempty"`

	// Wait time between reconnect attempts in milliseconds.
	// +optional
	ReconnectWaitTime *int32 `json:"reconnect-wait-time,omitempty"`

	// Action on database connection failure.
	// +optional
	// +kubebuilder:validation:Enum=stop-retry-exit;serve-retry-exit;serve-retry-continue
	OnFail string `json:"on-fail,omitempty"`

	// Retry database connection at startup.
	// +optional
	RetryOnStartup *bool `json:"retry-on-startup,omitempty"`

	// Lease File Cleanup interval in seconds (memfile only).
	// +optional
	LFCInterval *int32 `json:"lfc-interval,omitempty"`

	// Maximum acceptable row errors during load.
	// +optional
	MaxRowErrors *int32 `json:"max-row-errors,omitempty"`

	// Read-only mode (hosts-database only).
	// +optional
	ReadOnly *bool `json:"readonly,omitempty"`

	// Read timeout in seconds (MySQL only).
	// +optional
	ReadTimeout *int32 `json:"read-timeout,omitempty"`

	// Write timeout in seconds (MySQL only).
	// +optional
	WriteTimeout *int32 `json:"write-timeout,omitempty"`

	// TCP user timeout in seconds (PostgreSQL only).
	// +optional
	TCPUserTimeout *int32 `json:"tcp-user-timeout,omitempty"`
}

// ========== Interface Configuration ==========

// InterfacesConfig defines the network interfaces configuration for DHCP.
type InterfacesConfig struct {
	// List of interface names or interface/address pairs to listen on.
	// Interface names must contain only alphanumeric characters, hyphens, underscores, dots, slashes,
	// or colons (for IPv6 interface/address pairs such as "eth0/2001:db8::1").
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Pattern=`^[a-zA-Z0-9._/:-]+$`
	Interfaces []string `json:"interfaces"`

	// DHCP socket type: "raw" for raw sockets or "udp" for UDP sockets.
	// +optional
	// +kubebuilder:validation:Enum=raw;udp
	DHCPSocketType string `json:"dhcp-socket-type,omitempty"`

	// Outbound interface selection method.
	// +optional
	// +kubebuilder:validation:Enum=same-as-inbound;use-routing
	OutboundInterface string `json:"outbound-interface,omitempty"`

	// Re-detect interfaces on reconfiguration.
	// +optional
	ReDetect *bool `json:"re-detect,omitempty"`

	// Require all interfaces to bind successfully.
	// +optional
	ServiceSocketsRequireAll *bool `json:"service-sockets-require-all,omitempty"`

	// Maximum number of socket binding retries.
	// +optional
	ServiceSocketsMaxRetries *int32 `json:"service-sockets-max-retries,omitempty"`

	// Milliseconds to wait between socket binding retries.
	// +optional
	ServiceSocketsRetryWaitTime *int32 `json:"service-sockets-retry-wait-time,omitempty"`
}

// ========== DHCP-DDNS Configuration ==========

// DhcpDdnsConfig configures connectivity from a DHCP server to Kea D2.
type DhcpDdnsConfig struct {
	// Enable connectivity to the Kea DHCP-DDNS server.
	// +optional
	EnableUpdates *bool `json:"enable-updates,omitempty"`

	// IP address on which the Kea DHCP-DDNS server listens.
	// +optional
	ServerIP string `json:"server-ip,omitempty"`

	// Port on which the Kea DHCP-DDNS server listens.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ServerPort *int32 `json:"server-port,omitempty"`

	// Source IP address used to send NameChangeRequests.
	// +optional
	SenderIP string `json:"sender-ip,omitempty"`

	// Source port used to send NameChangeRequests. Zero selects a port automatically.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	SenderPort *int32 `json:"sender-port,omitempty"`

	// Maximum number of NameChangeRequests queued for delivery.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxQueueSize *int32 `json:"max-queue-size,omitempty"`

	// Protocol used to send NameChangeRequests.
	// +optional
	// +kubebuilder:validation:Enum=UDP
	NCRProtocol string `json:"ncr-protocol,omitempty"`

	// NameChangeRequest payload format.
	// +optional
	// +kubebuilder:validation:Enum=JSON
	NCRFormat string `json:"ncr-format,omitempty"`
}

// DDNSBehavior defines inheritable DDNS behavior at global, shared-network,
// subnet, and address-pool scope. A fixed ddns-ttl cannot be combined with
// ddns-ttl-percent, ddns-ttl-min or ddns-ttl-max at the same scope.
//
// The string parameters are pointers so that an explicit empty string at a
// narrower scope overrides an inherited value (Kea treats an empty
// hostname-char-set as "no sanitization", an empty hostname-char-replacement
// as "drop invalid characters", and an empty suffix or prefix as "none").
type DDNSBehavior struct {
	// Send DDNS updates for this scope.
	// +optional
	DDNSSendUpdates *bool `json:"ddns-send-updates,omitempty"`

	// Ignore a client's request not to perform DDNS updates.
	// +optional
	DDNSOverrideNoUpdate *bool `json:"ddns-override-no-update,omitempty"`

	// Override a client's request to perform its own DNS updates.
	// +optional
	DDNSOverrideClientUpdate *bool `json:"ddns-override-client-update,omitempty"`

	// Client hostname replacement policy.
	// +optional
	// +kubebuilder:validation:Enum=never;always;when-present;when-not-present
	DDNSReplaceClientName string `json:"ddns-replace-client-name,omitempty"`

	// Prefix used for generated hostnames. An empty string overrides an inherited prefix.
	// +optional
	DDNSGeneratedPrefix *string `json:"ddns-generated-prefix,omitempty"`

	// DNS suffix appended to qualified client hostnames. An empty string overrides an inherited suffix.
	// +optional
	DDNSQualifyingSuffix *string `json:"ddns-qualifying-suffix,omitempty"`

	// Send DDNS updates whenever a lease is renewed.
	// +optional
	DDNSUpdateOnRenew *bool `json:"ddns-update-on-renew,omitempty"`

	// DHCID-based DDNS conflict resolution mode.
	// +optional
	// +kubebuilder:validation:Enum=check-with-dhcid;no-check-with-dhcid;check-exists-with-dhcid;no-check-without-dhcid
	DDNSConflictResolutionMode string `json:"ddns-conflict-resolution-mode,omitempty"`

	// Fixed DNS record TTL in seconds.
	// +optional
	DDNSTTL *int32 `json:"ddns-ttl,omitempty"`

	// DNS record TTL as a fraction of the lease lifetime (a non-negative number,
	// e.g. 0.25; 0 suppresses an inherited value).
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	DDNSTTLPercent *apiextensionsv1.JSON `json:"ddns-ttl-percent,omitempty"`

	// Minimum DNS record TTL in seconds.
	// +optional
	DDNSTTLMin *int32 `json:"ddns-ttl-min,omitempty"`

	// Maximum DNS record TTL in seconds.
	// +optional
	DDNSTTLMax *int32 `json:"ddns-ttl-max,omitempty"`

	// Regular expression defining characters permitted in generated hostnames.
	// An empty string overrides an inherited set and disables sanitization.
	// +optional
	HostnameCharSet *string `json:"hostname-char-set,omitempty"`

	// Replacement for disallowed hostname characters. An empty string overrides
	// an inherited replacement and drops the characters instead.
	// +optional
	HostnameCharReplacement *string `json:"hostname-char-replacement,omitempty"`
}

// ========== DHCP Options ==========

// OptionData defines a DHCP option value.
type OptionData struct {
	// Option name (e.g., "domain-name-servers", "routers").
	// +optional
	Name string `json:"name,omitempty"`

	// Numeric option code.
	// +optional
	Code *int32 `json:"code,omitempty"`

	// Option space (default: "dhcp4" or "dhcp6").
	// +optional
	Space string `json:"space,omitempty"`

	// Whether data is in comma-separated format.
	// +optional
	CSVFormat *bool `json:"csv-format,omitempty"`

	// Option data value as text or hexadecimal.
	// +optional
	Data string `json:"data,omitempty"`

	// Always include this option in responses.
	// +optional
	AlwaysSend *bool `json:"always-send,omitempty"`

	// Never include this option in responses.
	// +optional
	NeverSend *bool `json:"never-send,omitempty"`
}

// OptionDef defines a custom DHCP option definition.
type OptionDef struct {
	// Custom option name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Numeric code assignment.
	// +kubebuilder:validation:Required
	Code int32 `json:"code"`

	// Data type (uint8, uint16, uint32, string, ipv4-address, boolean, binary, fqdn, record, etc.).
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// Whether option contains an array of values.
	// +optional
	Array *bool `json:"array,omitempty"`

	// Comma-separated field types for record-type options.
	// +optional
	RecordTypes string `json:"record-types,omitempty"`

	// Encapsulated option space name.
	// +optional
	Encapsulate string `json:"encapsulate,omitempty"`

	// Option space this definition belongs to.
	// +optional
	Space string `json:"space,omitempty"`
}

// ========== Client Classes ==========

// ClientClass defines a DHCP client classification rule.
type ClientClass struct {
	// Unique name of the client class.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Boolean expression for class membership testing.
	// +optional
	Test string `json:"test,omitempty"`

	// DHCP options specific to this class.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Only use this class when explicitly required by another class.
	// +optional
	OnlyIfRequired *bool `json:"only-if-required,omitempty"`

	// Valid lifetime for clients in this class.
	// +optional
	ValidLifetime *int32 `json:"valid-lifetime,omitempty"`

	// Minimum valid lifetime.
	// +optional
	MinValidLifetime *int32 `json:"min-valid-lifetime,omitempty"`

	// Maximum valid lifetime.
	// +optional
	MaxValidLifetime *int32 `json:"max-valid-lifetime,omitempty"`
}

// ========== Hooks Libraries ==========

// HookLibrary defines a Kea hook library to load.
type HookLibrary struct {
	// Full path to the hook library .so file inside the container.
	// +kubebuilder:validation:Required
	Library string `json:"library"`

	// Parameters passed to the hook library as arbitrary JSON.
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Parameters *apiextensionsv1.JSON `json:"parameters,omitempty"`
}

// ========== Control Socket ==========

// ControlSocket defines the Kea daemon control socket configuration.
type ControlSocket struct {
	// Socket type: "unix" for UNIX domain socket, "http" for HTTP.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=unix;http
	SocketType string `json:"socket-type"`

	// UNIX socket file path (for unix type).
	// +optional
	SocketName string `json:"socket-name,omitempty"`

	// HTTP socket port (for http type).
	// +optional
	SocketPort *int32 `json:"socket-port,omitempty"`

	// HTTP socket bind address (for http type). Defaults to "127.0.0.1".
	// Set to "0.0.0.0" for HA peer communication.
	// +optional
	SocketAddress string `json:"socket-address,omitempty"`
}

// ========== Loggers ==========

// LoggerConfig defines logging configuration for a Kea daemon.
type LoggerConfig struct {
	// Logger component name (e.g., "kea-dhcp4", "kea-dhcp6").
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Log severity level.
	// +optional
	// +kubebuilder:validation:Enum=FATAL;ERROR;WARN;INFO;DEBUG
	// +kubebuilder:default="INFO"
	Severity string `json:"severity,omitempty"`

	// Debug verbosity level (0-99, only used when severity=DEBUG).
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=99
	DebugLevel *int32 `json:"debuglevel,omitempty"`

	// Output destinations for log messages.
	// +optional
	OutputOptions []LogOutputOption `json:"output-options,omitempty"`
}

// LogOutputOption defines where log messages are written.
type LogOutputOption struct {
	// Output destination: "stdout", "stderr", "syslog", "syslog:name", or a file path.
	// +kubebuilder:validation:Required
	Output string `json:"output"`

	// Maximum log file size before rotation in bytes.
	// +optional
	Maxsize *int64 `json:"maxsize,omitempty"`

	// Maximum number of rotated log files to keep.
	// +optional
	Maxver *int32 `json:"maxver,omitempty"`

	// Flush output after each log message.
	// +optional
	Flush *bool `json:"flush,omitempty"`

	// Log message pattern.
	// +optional
	Pattern string `json:"pattern,omitempty"`
}

// ========== Reservations ==========

// Reservation defines a DHCP host reservation.
type Reservation struct {
	// Hardware (MAC) address identifier.
	// +optional
	HWAddress string `json:"hw-address,omitempty"`

	// DHCP Client Identifier.
	// +optional
	ClientID string `json:"client-id,omitempty"`

	// DHCP Unique Identifier.
	// +optional
	DUID string `json:"duid,omitempty"`

	// RAI circuit identifier (DHCPv4 only).
	// +optional
	CircuitID string `json:"circuit-id,omitempty"`

	// Flex identifier.
	// +optional
	FlexID string `json:"flex-id,omitempty"`

	// Hostname to assign to the client.
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// IPv4 address to reserve (DHCPv4 only).
	// +optional
	IPAddress string `json:"ip-address,omitempty"`

	// IPv6 addresses to reserve (DHCPv6 only).
	// +optional
	IPAddresses []string `json:"ip-addresses,omitempty"`

	// Prefix delegation prefixes (DHCPv6 only).
	// +optional
	Prefixes []string `json:"prefixes,omitempty"`

	// DHCP options for this reservation.
	// +optional
	OptionData []OptionData `json:"option-data,omitempty"`

	// Client classes to assign to this host.
	// +optional
	ClientClasses []string `json:"client-classes,omitempty"`

	// Next server address (DHCPv4 PXE boot).
	// +optional
	NextServer string `json:"next-server,omitempty"`

	// Boot file name (DHCPv4 PXE boot).
	// +optional
	BootFileName string `json:"boot-file-name,omitempty"`

	// Server hostname (DHCPv4).
	// +optional
	ServerHostname string `json:"server-hostname,omitempty"`
}

// ========== Relay Info ==========

// RelayInfo holds DHCP relay agent information.
type RelayInfo struct {
	// Relay agent IP addresses.
	// +optional
	IPAddresses []string `json:"ip-addresses,omitempty"`
}

// ========== Pod Placement ==========

// PodPlacement defines pod scheduling constraints and metadata.
type PodPlacement struct {
	// Node selector key-value pairs.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Pod tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Pod affinity rules.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Additional annotations to set on the pod template. Useful for
	// network attachment definitions (e.g., k8s.v1.cni.cncf.io/networks).
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`
}

// ========== Container Configuration ==========

// ContainerConfig defines container-level settings for Kea daemons.
type ContainerConfig struct {
	// Container image override. Defaults to the per-component ISC Kea image
	// (e.g., kea-dhcp4, kea-dhcp6, kea-ctrl-agent, kea-dhcp-ddns).
	// +optional
	Image string `json:"image,omitempty"`

	// Image pull policy.
	// +optional
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// CPU and memory resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Image pull secrets.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// ========== TLS Configuration ==========

// TLSConfig defines TLS certificate configuration for Kea components.
type TLSConfig struct {
	// Reference to a kubernetes.io/tls Secret containing TLS certificates.
	// The Secret must have keys "tls.crt", "tls.key", and optionally "ca.crt".
	// +kubebuilder:validation:Required
	SecretRef corev1.LocalObjectReference `json:"secretRef"`

	// Whether client certificates are required.
	// +optional
	CertRequired *bool `json:"certRequired,omitempty"`
}

// ========== Stork Agent Configuration ==========

// StorkAgentConfig defines the optional Stork agent sidecar that provides
// monitoring and Prometheus metrics for Kea DHCP servers.
// When enabled, a stork-agent container is injected as a sidecar alongside the
// Kea daemon. The agent discovers Kea by scanning processes in the shared PID
// namespace and reading the mounted config files.
type StorkAgentConfig struct {
	// Enable the Stork agent sidecar.
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// Stork agent container image.
	// Defaults to quay.io/mooyeg/stork-agent:v2.4.0 if not specified.
	// +optional
	Image string `json:"image,omitempty"`

	// Image pull policy for the stork-agent container.
	// +optional
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// CPU and memory resources for the stork-agent container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// URL of the Stork server to register with (e.g., "http://stork-server:8080").
	// If empty, the agent starts without server registration (Prometheus-only mode).
	// +optional
	ServerURL string `json:"serverURL,omitempty"`

	// Reference to a Secret containing the Stork server access token.
	// The Secret must have a key "token". Used for non-interactive agent registration.
	// +optional
	ServerTokenSecretRef *corev1.LocalObjectReference `json:"serverTokenSecretRef,omitempty"`

	// Reference to a KeaStorkServer CR in the same namespace. When set, the operator
	// auto-discovers the server URL and token, overriding serverURL and serverTokenSecretRef.
	// +optional
	StorkServerRef *corev1.LocalObjectReference `json:"storkServerRef,omitempty"`

	// Port for the Stork agent gRPC listener (stork-server connects here).
	// +optional
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port *int32 `json:"port,omitempty"`

	// Port for the Prometheus Kea metrics exporter.
	// +optional
	// +kubebuilder:default=9547
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	PrometheusPort *int32 `json:"prometheusPort,omitempty"`

	// Extra environment variables passed to the stork-agent container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// ========== Status Condition Constants ==========

const (
	// ConditionTypeReady indicates the component is running and healthy.
	ConditionTypeReady = "Ready"

	// ConditionTypeConfigValid indicates the configuration has been validated.
	ConditionTypeConfigValid = "ConfigurationValid"

	// ConditionTypeDegraded indicates the component is in a degraded state.
	ConditionTypeDegraded = "Degraded"

	// ConditionTypeProgressing indicates changes are being applied.
	ConditionTypeProgressing = "Progressing"

	// ConditionTypeServerTokenSynced indicates the agent token Secret of a
	// KeaStorkServer holds the token the Stork server actually accepts.
	ConditionTypeServerTokenSynced = "ServerTokenSynced"

	// ConditionTypeCredentialsRolloutPending indicates a referenced credential
	// Secret changed but the pods were not restarted, because restarting them
	// would discard memfile leases kept in pod-local storage.
	ConditionTypeCredentialsRolloutPending = "CredentialsRolloutPending"

	// ConditionTypeRestartPending indicates the operator renders the pods
	// differently than the release that created them, but did not restart
	// them, because that would discard memfile leases kept in pod-local
	// storage.
	ConditionTypeRestartPending = "RestartPending"

	// ConditionTypeLeasesDurable reports whether leases survive the
	// replacement of the pods: True with a database lease backend or a
	// persistent lease volume, False with memfile leases in an emptyDir.
	ConditionTypeLeasesDurable = "LeasesDurable"
)

const (
	// ConditionStatusTrue is the "True" status value for a condition.
	ConditionStatusTrue = "True"
	// ConditionStatusFalse is the "False" status value for a condition.
	ConditionStatusFalse = "False"
)

const (
	// PhaseRunning indicates the component is running and healthy.
	PhaseRunning = "Running"
	// PhaseProgressing indicates the component is still converging.
	PhaseProgressing = "Progressing"
	// PhaseError indicates the component encountered an error.
	PhaseError = "Error"
)

// ========== Common Status ==========

// ComponentStatus defines observed state common to all Kea components.
type ComponentStatus struct {
	// Current conditions of the component.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []ConditionStatus `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Current operational phase.
	// +optional
	Phase string `json:"phase,omitempty"`

	// The generation last observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Name of the ConfigMap holding the rendered Kea configuration.
	// +optional
	ConfigMapRef string `json:"configMapRef,omitempty"`

	// Number of ready replicas.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// SHA-256 hash of the current rendered configuration.
	// +optional
	ConfigHash string `json:"configHash,omitempty"`
}

// ConditionStatus mirrors metav1.Condition for embedding in status.
type ConditionStatus struct {
	// Type of condition.
	Type string `json:"type"`
	// Status of the condition (True, False, Unknown).
	Status string `json:"status"`
	// Last time the condition transitioned.
	// +optional
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	// Machine-readable reason for the condition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Human-readable message.
	// +optional
	Message string `json:"message,omitempty"`
}
