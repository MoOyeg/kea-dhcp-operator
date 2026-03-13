/**
 * K8sModel definitions for Kea DHCP CRDs.
 * Used with useK8sWatchResource and other SDK functions.
 */
import { K8sModel } from '@openshift-console/dynamic-plugin-sdk';

export const KeaDhcp4ServerModel: K8sModel = {
  apiGroup: 'kea.openshift.io',
  apiVersion: 'v1alpha1',
  kind: 'KeaDhcp4Server',
  plural: 'keadhcp4servers',
  abbr: 'KD4',
  label: 'Kea DHCPv4 Server',
  labelPlural: 'Kea DHCPv4 Servers',
  namespaced: true,
};

export const KeaDhcp6ServerModel: K8sModel = {
  apiGroup: 'kea.openshift.io',
  apiVersion: 'v1alpha1',
  kind: 'KeaDhcp6Server',
  plural: 'keadhcp6servers',
  abbr: 'KD6',
  label: 'Kea DHCPv6 Server',
  labelPlural: 'Kea DHCPv6 Servers',
  namespaced: true,
};

export const KeaControlAgentModel: K8sModel = {
  apiGroup: 'kea.openshift.io',
  apiVersion: 'v1alpha1',
  kind: 'KeaControlAgent',
  plural: 'keacontrolagents',
  abbr: 'KCA',
  label: 'Kea Control Agent',
  labelPlural: 'Kea Control Agents',
  namespaced: true,
};

/** Lookup a model by its plural name. */
export function modelForPlural(plural: string): K8sModel | undefined {
  switch (plural) {
    case 'keadhcp4servers':
      return KeaDhcp4ServerModel;
    case 'keadhcp6servers':
      return KeaDhcp6ServerModel;
    case 'keacontrolagents':
      return KeaControlAgentModel;
    default:
      return undefined;
  }
}
