/**
 * Functions for fetching DHCP leases from the backend proxy.
 * The backend proxy is provided by the console plugin backend Go server.
 */
import { consoleFetchJSON } from '@openshift-console/dynamic-plugin-sdk';
import { DHCPv4Lease, DHCPv6Lease } from '../types/lease';

const BACKEND_BASE =
  '/api/proxy/plugin/kea-dhcp-console-plugin/backend/api/v1';

interface LeaseResponse<T> {
  leases: T[];
}

/**
 * Fetch DHCPv4 leases from the backend proxy.
 * Optionally filter by namespace (the backend routes to the correct Control Agent).
 */
export async function fetchLeases4(
  namespace?: string,
): Promise<DHCPv4Lease[]> {
  const params = namespace ? `?namespace=${encodeURIComponent(namespace)}` : '';
  const url = `${BACKEND_BASE}/leases4${params}`;
  const data: LeaseResponse<DHCPv4Lease> = await consoleFetchJSON(url);
  return data.leases ?? [];
}

/**
 * Fetch DHCPv6 leases from the backend proxy.
 * Optionally filter by namespace.
 */
export async function fetchLeases6(
  namespace?: string,
): Promise<DHCPv6Lease[]> {
  const params = namespace ? `?namespace=${encodeURIComponent(namespace)}` : '';
  const url = `${BACKEND_BASE}/leases6${params}`;
  const data: LeaseResponse<DHCPv6Lease> = await consoleFetchJSON(url);
  return data.leases ?? [];
}
