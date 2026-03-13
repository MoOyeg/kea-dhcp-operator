import React from 'react';
import {
  Title,
  EmptyState,
  EmptyStateBody,
  EmptyStateHeader,
  EmptyStateIcon,
  ExpandableSection,
} from '@patternfly/react-core';
import { SearchIcon } from '@patternfly/react-icons';
import {
  Table,
  Thead,
  Tr,
  Th,
  Tbody,
  Td,
  ExpandableRowContent,
} from '@patternfly/react-table';

import { KeaDhcp4Server, Subnet4 } from '../../types/kea-dhcp4server';
import { KeaDhcp6Server, Subnet6 } from '../../types/kea-dhcp6server';
import { Reservation } from '../../types/kea-common';

interface SubnetsTabProps {
  resource: KeaDhcp4Server | KeaDhcp6Server;
  kind: string; // "keadhcp4servers" or "keadhcp6servers"
}

type AnySubnet = Subnet4 | Subnet6;

/** Render pool details for a subnet. */
const PoolDetails: React.FC<{ subnet: AnySubnet; kind: string }> = ({
  subnet,
  kind,
}) => {
  const pools =
    kind === 'keadhcp4servers'
      ? (subnet as Subnet4).pools
      : (subnet as Subnet6).pools;
  const pdPools =
    kind === 'keadhcp6servers' ? (subnet as Subnet6)['pd-pools'] : undefined;

  return (
    <div style={{ padding: '0.5rem 0' }}>
      {pools && pools.length > 0 ? (
        <>
          <strong>Address Pools:</strong>
          <ul style={{ margin: '0.25rem 0 0.5rem 1.5rem' }}>
            {pools.map((p, i) => (
              <li key={i}>{p.pool}</li>
            ))}
          </ul>
        </>
      ) : (
        <p>No address pools configured.</p>
      )}

      {pdPools && pdPools.length > 0 && (
        <>
          <strong>Prefix Delegation Pools:</strong>
          <ul style={{ margin: '0.25rem 0 0.5rem 1.5rem' }}>
            {pdPools.map((p, i) => (
              <li key={i}>
                {p.prefix}/{p['prefix-len']} (delegated: /{p['delegated-len']})
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
};

/** Render reservations for a subnet. */
const ReservationDetails: React.FC<{ reservations?: Reservation[] }> = ({
  reservations,
}) => {
  if (!reservations || reservations.length === 0) {
    return <p>No reservations configured.</p>;
  }

  return (
    <Table aria-label="Reservations" variant="compact">
      <Thead>
        <Tr>
          <Th>Identifier</Th>
          <Th>IP Address(es)</Th>
          <Th>Hostname</Th>
        </Tr>
      </Thead>
      <Tbody>
        {reservations.map((r, i) => {
          const identifier =
            r['hw-address'] ??
            r['client-id'] ??
            r.duid ??
            r['flex-id'] ??
            '-';
          const ips =
            r['ip-address'] ??
            r['ip-addresses']?.join(', ') ??
            '-';
          return (
            <Tr key={i}>
              <Td dataLabel="Identifier">{identifier}</Td>
              <Td dataLabel="IP Address(es)">{ips}</Td>
              <Td dataLabel="Hostname">{r.hostname ?? '-'}</Td>
            </Tr>
          );
        })}
      </Tbody>
    </Table>
  );
};

const SubnetsTab: React.FC<SubnetsTabProps> = ({ resource, kind }) => {
  const [expandedRows, setExpandedRows] = React.useState<Set<number>>(
    new Set(),
  );

  const subnets: AnySubnet[] =
    kind === 'keadhcp4servers'
      ? (resource as KeaDhcp4Server).spec?.subnet4 ?? []
      : (resource as KeaDhcp6Server).spec?.subnet6 ?? [];

  const toggleRow = (subnetId: number) => {
    setExpandedRows((prev) => {
      const next = new Set(prev);
      if (next.has(subnetId)) {
        next.delete(subnetId);
      } else {
        next.add(subnetId);
      }
      return next;
    });
  };

  if (subnets.length === 0) {
    return (
      <div style={{ padding: '1rem' }}>
        <EmptyState>
          <EmptyStateHeader
            titleText="No subnets configured"
            headingLevel="h3"
            icon={<EmptyStateIcon icon={SearchIcon} />}
          />
          <EmptyStateBody>
            This server does not have any subnet definitions.
          </EmptyStateBody>
        </EmptyState>
      </div>
    );
  }

  const columns = ['ID', 'Subnet CIDR', 'Pools Count', 'Reservations Count', 'Valid Lifetime'];

  return (
    <div style={{ padding: '1rem' }}>
      <Title headingLevel="h2" style={{ marginBottom: '1rem' }}>
        {kind === 'keadhcp4servers' ? 'IPv4' : 'IPv6'} Subnets
      </Title>
      <Table aria-label="Subnets" variant="compact">
        <Thead>
          <Tr>
            <Th screenReaderText="Expand" />
            {columns.map((col) => (
              <Th key={col}>{col}</Th>
            ))}
          </Tr>
        </Thead>
        {subnets.map((subnet) => {
          const isExpanded = expandedRows.has(subnet.id);
          const poolsCount =
            kind === 'keadhcp4servers'
              ? (subnet as Subnet4).pools?.length ?? 0
              : ((subnet as Subnet6).pools?.length ?? 0) +
                ((subnet as Subnet6)['pd-pools']?.length ?? 0);
          const reservationsCount = subnet.reservations?.length ?? 0;
          const validLifetime = subnet['valid-lifetime'];

          return (
            <Tbody key={subnet.id} isExpanded={isExpanded}>
              <Tr>
                <Td
                  expand={{
                    rowIndex: subnet.id,
                    isExpanded,
                    onToggle: () => toggleRow(subnet.id),
                  }}
                />
                <Td dataLabel="ID">{subnet.id}</Td>
                <Td dataLabel="Subnet CIDR">{subnet.subnet}</Td>
                <Td dataLabel="Pools Count">{poolsCount}</Td>
                <Td dataLabel="Reservations Count">{reservationsCount}</Td>
                <Td dataLabel="Valid Lifetime">
                  {validLifetime != null ? `${validLifetime}s` : 'Default'}
                </Td>
              </Tr>
              <Tr isExpanded={isExpanded}>
                <Td colSpan={columns.length + 1}>
                  <ExpandableRowContent>
                    <ExpandableSection
                      toggleText="Pool Details"
                      isExpanded
                    >
                      <PoolDetails subnet={subnet} kind={kind} />
                    </ExpandableSection>
                    <ExpandableSection
                      toggleText="Reservations"
                      isExpanded
                    >
                      <ReservationDetails reservations={subnet.reservations} />
                    </ExpandableSection>
                  </ExpandableRowContent>
                </Td>
              </Tr>
            </Tbody>
          );
        })}
      </Table>
    </div>
  );
};

export default SubnetsTab;
