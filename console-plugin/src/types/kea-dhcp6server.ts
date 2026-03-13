/**
 * TypeScript types for the KeaDhcp6Server CRD (kea.openshift.io/v1alpha1).
 * Mirrors api/v1alpha1/keadhcp6server_types.go from the operator.
 */
import { K8sResourceCommon } from '@openshift-console/dynamic-plugin-sdk';
import {
  ComponentStatus,
  ContainerConfig,
  ControlSocket,
  ClientClass,
  DatabaseConfig,
  HAConfig,
  HookLibrary,
  InterfacesConfig,
  LoggerConfig,
  MultiThreadingConfig,
  OptionData,
  OptionDef,
  RelayInfo,
  Reservation,
} from './kea-common';

/** Pool6 defines an IPv6 address pool for dynamic allocation. */
export interface Pool6 {
  pool: string;
  'option-data'?: OptionData[];
  'client-class'?: string;
  'require-client-classes'?: string[];
}

/** PDPool defines a prefix delegation pool (IPv6-specific). */
export interface PDPool {
  prefix: string;
  'prefix-len': number;
  'delegated-len': number;
  'excluded-prefix'?: string;
  'excluded-prefix-len'?: number;
  'option-data'?: OptionData[];
  'client-class'?: string;
}

/** Subnet6 defines a DHCPv6 subnet configuration. */
export interface Subnet6 {
  id: number;
  subnet: string;
  pools?: Pool6[];
  'pd-pools'?: PDPool[];
  'option-data'?: OptionData[];
  reservations?: Reservation[];
  'client-class'?: string;
  'require-client-classes'?: string[];
  interface?: string;
  'interface-id'?: string;
  relay?: RelayInfo;
  'preferred-lifetime'?: number;
  'valid-lifetime'?: number;
  'min-valid-lifetime'?: number;
  'max-valid-lifetime'?: number;
  'renew-timer'?: number;
  'rebind-timer'?: number;
  'rapid-commit'?: boolean;
  'reservation-mode'?: 'all' | 'out-of-pool' | 'global' | 'disabled';
}

/** SharedNetwork6 defines a shared network grouping multiple IPv6 subnets. */
export interface SharedNetwork6 {
  name: string;
  subnet6?: Subnet6[];
  interface?: string;
  'interface-id'?: string;
  'option-data'?: OptionData[];
  relay?: RelayInfo;
  'client-class'?: string;
  'preferred-lifetime'?: number;
  'valid-lifetime'?: number;
  'renew-timer'?: number;
  'rebind-timer'?: number;
  'rapid-commit'?: boolean;
}

/** KeaDhcp6ServerSpec defines the desired state of a Kea DHCPv6 server deployment. */
export interface KeaDhcp6ServerSpec {
  container?: ContainerConfig;
  replicas?: number;
  hostNetwork?: boolean;
  'interfaces-config': InterfacesConfig;
  'lease-database'?: DatabaseConfig;
  'hosts-database'?: DatabaseConfig;
  'hosts-databases'?: DatabaseConfig[];
  subnet6?: Subnet6[];
  'shared-networks'?: SharedNetwork6[];
  'option-data'?: OptionData[];
  'option-def'?: OptionDef[];
  'client-classes'?: ClientClass[];
  reservations?: Reservation[];
  'host-reservation-identifiers'?: string[];
  'hooks-libraries'?: HookLibrary[];
  'control-socket'?: ControlSocket;
  loggers?: LoggerConfig[];
  'preferred-lifetime'?: number;
  'valid-lifetime'?: number;
  'min-valid-lifetime'?: number;
  'max-valid-lifetime'?: number;
  'renew-timer'?: number;
  'rebind-timer'?: number;
  'calculate-tee-times'?: boolean;
  't1-percent'?: string;
  't2-percent'?: string;
  'server-tag'?: string;
  'rapid-commit'?: boolean;
  'ddns-send-updates'?: boolean;
  'ddns-qualifying-suffix'?: string;
  'high-availability'?: HAConfig;
  'multi-threading'?: MultiThreadingConfig;
}

/** KeaDhcp6ServerStatus defines the observed state of KeaDhcp6Server. */
export type KeaDhcp6ServerStatus = ComponentStatus;

/** KeaDhcp6Server is the Schema for the keadhcp6servers API. */
export type KeaDhcp6Server = K8sResourceCommon & {
  spec?: KeaDhcp6ServerSpec;
  status?: KeaDhcp6ServerStatus;
};
