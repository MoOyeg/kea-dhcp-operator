/**
 * TypeScript types for Kea Control Agent REST API lease responses.
 */

/** Lease state constants matching Kea's numeric values. */
export const LeaseState = {
  DEFAULT: 0,
  DECLINED: 1,
  EXPIRED_RECLAIMED: 2,
} as const;

/** Returns a human-readable label for a numeric lease state. */
export function leaseStateLabel(state: number): string {
  switch (state) {
    case LeaseState.DEFAULT:
      return 'Active';
    case LeaseState.DECLINED:
      return 'Declined';
    case LeaseState.EXPIRED_RECLAIMED:
      return 'Expired-Reclaimed';
    default:
      return `Unknown (${state})`;
  }
}

/** DHCPv4Lease matches the Kea Control Agent REST API response for a v4 lease. */
export interface DHCPv4Lease {
  'ip-address': string;
  'hw-address': string;
  'client-id'?: string;
  'valid-lft': number;
  cltt: number;
  'subnet-id': number;
  'fqdn-fwd': boolean;
  'fqdn-rev': boolean;
  hostname: string;
  state: number;
}

/** DHCPv6Lease matches the Kea Control Agent REST API response for a v6 lease. */
export interface DHCPv6Lease {
  'ip-address': string;
  duid: string;
  iaid: number;
  'subnet-id': number;
  'valid-lft': number;
  cltt: number;
  'preferred-lft': number;
  hostname: string;
  state: number;
  type: 'IA_NA' | 'IA_PD';
  'prefix-len'?: number;
}
