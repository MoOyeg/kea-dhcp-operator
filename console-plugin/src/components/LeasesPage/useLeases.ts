import { useState, useEffect, useCallback, useRef } from 'react';
import { fetchLeases4, fetchLeases6 } from '../../utils/lease-api';
import { DHCPv4Lease, DHCPv6Lease } from '../../types/lease';

/** Polling interval in milliseconds. */
const POLL_INTERVAL = 30_000;

type Leases4Result = [DHCPv4Lease[], boolean, unknown];
type Leases6Result = [DHCPv6Lease[], boolean, unknown];

/**
 * Custom hook for polling DHCP leases.
 * Calls the appropriate fetch function and refreshes every 30 seconds.
 */
export function useLeases(
  version: '4',
  namespace?: string,
): Leases4Result;
export function useLeases(
  version: '6',
  namespace?: string,
): Leases6Result;
export function useLeases(
  version: '4' | '6',
  namespace?: string,
): Leases4Result | Leases6Result {
  const [leases, setLeases] = useState<DHCPv4Lease[] | DHCPv6Lease[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mountedRef = useRef(true);

  const fetchData = useCallback(async () => {
    try {
      const data =
        version === '4'
          ? await fetchLeases4(namespace)
          : await fetchLeases6(namespace);
      if (mountedRef.current) {
        setLeases(data);
        setLoaded(true);
        setError(null);
      }
    } catch (err) {
      if (mountedRef.current) {
        setError(err);
        setLoaded(true);
      }
    }
  }, [version, namespace]);

  useEffect(() => {
    mountedRef.current = true;
    fetchData();

    const interval = setInterval(fetchData, POLL_INTERVAL);

    return () => {
      mountedRef.current = false;
      clearInterval(interval);
    };
  }, [fetchData]);

  return [leases, loaded, error] as Leases4Result | Leases6Result;
}

export default useLeases;
