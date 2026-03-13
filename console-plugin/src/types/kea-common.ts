/**
 * Common TypeScript types shared across Kea DHCP CRDs.
 * Mirrors api/v1alpha1/common_types.go from the operator.
 */

/** ConditionStatus mirrors metav1.Condition for embedding in status. */
export interface ConditionStatus {
  type: string;
  status: string; // "True" | "False" | "Unknown"
  lastTransitionTime?: string;
  reason?: string;
  message?: string;
}

/** ComponentStatus defines observed state common to all Kea components. */
export interface ComponentStatus {
  conditions?: ConditionStatus[];
  phase?: string;
  observedGeneration?: number;
  configMapRef?: string;
  readyReplicas?: number;
  configHash?: string;
}

/** DatabaseConfig defines database connection parameters. */
export interface DatabaseConfig {
  type: 'memfile' | 'mysql' | 'postgresql';
  persist?: boolean;
  name?: string;
  host?: string;
  port?: number;
  user?: string;
  password?: string;
  'connect-timeout'?: number;
  'max-reconnect-tries'?: number;
  'reconnect-wait-time'?: number;
  'on-fail'?: string;
  'retry-on-startup'?: boolean;
  'lfc-interval'?: number;
  'max-row-errors'?: number;
  readonly?: boolean;
  'read-timeout'?: number;
  'write-timeout'?: number;
  'tcp-user-timeout'?: number;
}

/** OptionData defines a DHCP option value. */
export interface OptionData {
  name?: string;
  code?: number;
  space?: string;
  'csv-format'?: boolean;
  data?: string;
  'always-send'?: boolean;
  'never-send'?: boolean;
}

/** OptionDef defines a custom DHCP option definition. */
export interface OptionDef {
  name: string;
  code: number;
  type: string;
  array?: boolean;
  'record-types'?: string;
  encapsulate?: string;
  space?: string;
}

/** ClientClass defines a DHCP client classification rule. */
export interface ClientClass {
  name: string;
  test?: string;
  'option-data'?: OptionData[];
  'only-if-required'?: boolean;
  'valid-lifetime'?: number;
  'min-valid-lifetime'?: number;
  'max-valid-lifetime'?: number;
}

/** HookLibrary defines a Kea hook library to load. */
export interface HookLibrary {
  library: string;
  parameters?: Record<string, unknown>;
}

/** ControlSocket defines the Kea daemon control socket configuration. */
export interface ControlSocket {
  'socket-type': 'unix' | 'http';
  'socket-name'?: string;
  'socket-port'?: number;
}

/** LogOutputOption defines where log messages are written. */
export interface LogOutputOption {
  output: string;
  maxsize?: number;
  maxver?: number;
  flush?: boolean;
  pattern?: string;
}

/** LoggerConfig defines logging configuration for a Kea daemon. */
export interface LoggerConfig {
  name: string;
  severity?: 'FATAL' | 'ERROR' | 'WARN' | 'INFO' | 'DEBUG';
  debuglevel?: number;
  'output-options'?: LogOutputOption[];
}

/** Reservation defines a DHCP host reservation. */
export interface Reservation {
  'hw-address'?: string;
  'client-id'?: string;
  duid?: string;
  'circuit-id'?: string;
  'flex-id'?: string;
  hostname?: string;
  'ip-address'?: string;
  'ip-addresses'?: string[];
  prefixes?: string[];
  'option-data'?: OptionData[];
  'client-classes'?: string[];
  'next-server'?: string;
  'boot-file-name'?: string;
  'server-hostname'?: string;
}

/** InterfacesConfig defines the network interfaces configuration for DHCP. */
export interface InterfacesConfig {
  interfaces: string[];
  'dhcp-socket-type'?: 'raw' | 'udp';
  'outbound-interface'?: 'same-as-inbound' | 'use-routing';
  're-detect'?: boolean;
  'service-sockets-require-all'?: boolean;
  'service-sockets-max-retries'?: number;
  'service-sockets-retry-wait-time'?: number;
}

/** HAPeer defines a peer in the Kea HA cluster. */
export interface HAPeer {
  name: string;
  url: string;
  role: 'primary' | 'standby' | 'backup';
  'auto-failover'?: boolean;
}

/** HAConfig defines High Availability configuration for Kea DHCP servers. */
export interface HAConfig {
  'this-server-name': string;
  mode?: 'load-balancing' | 'hot-standby';
  'heartbeat-delay'?: number;
  'max-response-delay'?: number;
  'max-ack-delay'?: number;
  'max-unacked-clients'?: number;
  peers: HAPeer[];
  'send-lease-updates'?: boolean;
  'sync-leases'?: boolean;
  'sync-timeout'?: number;
  'sync-page-limit'?: number;
  'delayed-updates-limit'?: number;
}

/** ContainerConfig defines container-level settings for Kea daemons. */
export interface ContainerConfig {
  image?: string;
  imagePullPolicy?: 'Always' | 'IfNotPresent' | 'Never';
  resources?: {
    limits?: Record<string, string>;
    requests?: Record<string, string>;
  };
}

/** TLSConfig defines TLS certificate configuration for Kea components. */
export interface TLSConfig {
  secretRef: { name: string };
  certRequired?: boolean;
}

/** RelayInfo holds DHCP relay agent information. */
export interface RelayInfo {
  'ip-addresses'?: string[];
}

/** MultiThreadingConfig defines multi-threading settings. */
export interface MultiThreadingConfig {
  'enable-multi-threading'?: boolean;
  'thread-pool-size'?: number;
  'packet-queue-size'?: number;
}
