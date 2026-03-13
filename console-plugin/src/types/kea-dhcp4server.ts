/**
 * TypeScript types for the KeaDhcp4Server CRD (kea.openshift.io/v1alpha1).
 * Mirrors api/v1alpha1/keadhcp4server_types.go from the operator.
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

/** Pool4 defines an IPv4 address pool for dynamic allocation. */
export interface Pool4 {
  pool: string;
  'option-data'?: OptionData[];
  'client-class'?: string;
  'require-client-classes'?: string[];
}

/** Subnet4 defines a DHCPv4 subnet configuration. */
export interface Subnet4 {
  id: number;
  subnet: string;
  pools?: Pool4[];
  'option-data'?: OptionData[];
  reservations?: Reservation[];
  'client-class'?: string;
  'require-client-classes'?: string[];
  interface?: string;
  relay?: RelayInfo;
  'valid-lifetime'?: number;
  'min-valid-lifetime'?: number;
  'max-valid-lifetime'?: number;
  'renew-timer'?: number;
  'rebind-timer'?: number;
  authoritative?: boolean;
  'reservation-mode'?: 'all' | 'out-of-pool' | 'global' | 'disabled';
  'match-client-id'?: boolean;
  'next-server'?: string;
  'boot-file-name'?: string;
  'server-hostname'?: string;
  'user-context'?: string;
}

/** SharedNetwork4 defines a shared network grouping multiple IPv4 subnets. */
export interface SharedNetwork4 {
  name: string;
  subnet4?: Subnet4[];
  interface?: string;
  'option-data'?: OptionData[];
  relay?: RelayInfo;
  'client-class'?: string;
  'require-client-classes'?: string[];
  'valid-lifetime'?: number;
  'min-valid-lifetime'?: number;
  'max-valid-lifetime'?: number;
  'renew-timer'?: number;
  'rebind-timer'?: number;
  authoritative?: boolean;
}

/** KeaDhcp4ServerSpec defines the desired state of a Kea DHCPv4 server deployment. */
export interface KeaDhcp4ServerSpec {
  container?: ContainerConfig;
  replicas?: number;
  hostNetwork?: boolean;
  'interfaces-config': InterfacesConfig;
  'lease-database'?: DatabaseConfig;
  'hosts-database'?: DatabaseConfig;
  'hosts-databases'?: DatabaseConfig[];
  subnet4?: Subnet4[];
  'shared-networks'?: SharedNetwork4[];
  'option-data'?: OptionData[];
  'option-def'?: OptionDef[];
  'client-classes'?: ClientClass[];
  reservations?: Reservation[];
  'host-reservation-identifiers'?: string[];
  'hooks-libraries'?: HookLibrary[];
  'control-socket'?: ControlSocket;
  loggers?: LoggerConfig[];
  'valid-lifetime'?: number;
  'min-valid-lifetime'?: number;
  'max-valid-lifetime'?: number;
  'renew-timer'?: number;
  'rebind-timer'?: number;
  'calculate-tee-times'?: boolean;
  't1-percent'?: string;
  't2-percent'?: string;
  authoritative?: boolean;
  'match-client-id'?: boolean;
  'server-tag'?: string;
  'next-server'?: string;
  'boot-file-name'?: string;
  'server-hostname'?: string;
  'ddns-send-updates'?: boolean;
  'ddns-override-no-update'?: boolean;
  'ddns-override-client-update'?: boolean;
  'ddns-replace-client-name'?: string;
  'ddns-generated-prefix'?: string;
  'ddns-qualifying-suffix'?: string;
  'ddns-use-conflict-resolution'?: boolean;
  'high-availability'?: HAConfig;
  'multi-threading'?: MultiThreadingConfig;
}

/** KeaDhcp4ServerStatus defines the observed state of KeaDhcp4Server. */
export type KeaDhcp4ServerStatus = ComponentStatus;

/** KeaDhcp4Server is the Schema for the keadhcp4servers API. */
export type KeaDhcp4Server = K8sResourceCommon & {
  spec?: KeaDhcp4ServerSpec;
  status?: KeaDhcp4ServerStatus;
};
