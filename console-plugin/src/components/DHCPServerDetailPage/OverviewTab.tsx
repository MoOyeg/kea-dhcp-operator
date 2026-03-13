import React from 'react';
import {
  DescriptionList,
  DescriptionListGroup,
  DescriptionListTerm,
  DescriptionListDescription,
  Title,
  Divider,
} from '@patternfly/react-core';
import {
  Table,
  Thead,
  Tr,
  Th,
  Tbody,
  Td,
} from '@patternfly/react-table';

import { KeaDhcp4Server } from '../../types/kea-dhcp4server';
import { KeaDhcp6Server } from '../../types/kea-dhcp6server';
import { ConditionStatus, HAConfig } from '../../types/kea-common';
import StatusIcon from '../shared/StatusIcon';

interface OverviewTabProps {
  resource: KeaDhcp4Server | KeaDhcp6Server;
  kind: string; // "keadhcp4servers" or "keadhcp6servers"
}

const ConditionsTable: React.FC<{ conditions?: ConditionStatus[] }> = ({
  conditions,
}) => {
  if (!conditions || conditions.length === 0) {
    return <p>No conditions reported.</p>;
  }

  return (
    <Table aria-label="Conditions" variant="compact">
      <Thead>
        <Tr>
          <Th>Type</Th>
          <Th>Status</Th>
          <Th>Reason</Th>
          <Th>Message</Th>
          <Th>Last Transition Time</Th>
        </Tr>
      </Thead>
      <Tbody>
        {conditions.map((c) => (
          <Tr key={c.type}>
            <Td dataLabel="Type">{c.type}</Td>
            <Td dataLabel="Status">{c.status}</Td>
            <Td dataLabel="Reason">{c.reason ?? '-'}</Td>
            <Td dataLabel="Message">{c.message ?? '-'}</Td>
            <Td dataLabel="Last Transition Time">{c.lastTransitionTime ?? '-'}</Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  );
};

const HASection: React.FC<{ ha: HAConfig }> = ({ ha }) => (
  <>
    <Divider style={{ margin: '1rem 0' }} />
    <Title headingLevel="h3" style={{ marginBottom: '0.5rem' }}>
      High Availability
    </Title>
    <DescriptionList isHorizontal>
      <DescriptionListGroup>
        <DescriptionListTerm>Mode</DescriptionListTerm>
        <DescriptionListDescription>{ha.mode ?? 'load-balancing'}</DescriptionListDescription>
      </DescriptionListGroup>
      <DescriptionListGroup>
        <DescriptionListTerm>This Server</DescriptionListTerm>
        <DescriptionListDescription>{ha['this-server-name']}</DescriptionListDescription>
      </DescriptionListGroup>
      <DescriptionListGroup>
        <DescriptionListTerm>Heartbeat Delay</DescriptionListTerm>
        <DescriptionListDescription>
          {ha['heartbeat-delay'] != null ? `${ha['heartbeat-delay']} ms` : '10000 ms'}
        </DescriptionListDescription>
      </DescriptionListGroup>
      <DescriptionListGroup>
        <DescriptionListTerm>Max Response Delay</DescriptionListTerm>
        <DescriptionListDescription>
          {ha['max-response-delay'] != null ? `${ha['max-response-delay']} ms` : '60000 ms'}
        </DescriptionListDescription>
      </DescriptionListGroup>
    </DescriptionList>

    <Title headingLevel="h4" style={{ marginTop: '1rem', marginBottom: '0.5rem' }}>
      Peers
    </Title>
    <Table aria-label="HA Peers" variant="compact">
      <Thead>
        <Tr>
          <Th>Name</Th>
          <Th>URL</Th>
          <Th>Role</Th>
          <Th>Auto-Failover</Th>
        </Tr>
      </Thead>
      <Tbody>
        {ha.peers.map((peer) => (
          <Tr key={peer.name}>
            <Td dataLabel="Name">{peer.name}</Td>
            <Td dataLabel="URL">{peer.url}</Td>
            <Td dataLabel="Role">{peer.role}</Td>
            <Td dataLabel="Auto-Failover">
              {peer['auto-failover'] !== false ? 'Yes' : 'No'}
            </Td>
          </Tr>
        ))}
      </Tbody>
    </Table>
  </>
);

const OverviewTab: React.FC<OverviewTabProps> = ({ resource, kind }) => {
  const status = resource.status;
  const spec = resource.spec;
  const interfaces = spec?.['interfaces-config']?.interfaces ?? [];
  const ha = spec?.['high-availability'];

  return (
    <div style={{ padding: '1rem' }}>
      <Title headingLevel="h2" style={{ marginBottom: '1rem' }}>
        {resource.metadata?.name ?? 'Server'} Overview
      </Title>

      <DescriptionList isHorizontal>
        <DescriptionListGroup>
          <DescriptionListTerm>Phase</DescriptionListTerm>
          <DescriptionListDescription>
            <StatusIcon phase={status?.phase} />{' '}
            {status?.phase ?? 'Unknown'}
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Ready Replicas</DescriptionListTerm>
          <DescriptionListDescription>
            {status?.readyReplicas ?? 0}
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Config Hash</DescriptionListTerm>
          <DescriptionListDescription>
            <code>{status?.configHash ?? '-'}</code>
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>ConfigMap Ref</DescriptionListTerm>
          <DescriptionListDescription>
            {status?.configMapRef ?? '-'}
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Interfaces</DescriptionListTerm>
          <DescriptionListDescription>
            {interfaces.length > 0 ? interfaces.join(', ') : '-'}
          </DescriptionListDescription>
        </DescriptionListGroup>
        <DescriptionListGroup>
          <DescriptionListTerm>Type</DescriptionListTerm>
          <DescriptionListDescription>
            {kind === 'keadhcp4servers' ? 'DHCPv4' : 'DHCPv6'}
          </DescriptionListDescription>
        </DescriptionListGroup>
      </DescriptionList>

      <Divider style={{ margin: '1rem 0' }} />
      <Title headingLevel="h3" style={{ marginBottom: '0.5rem' }}>
        Conditions
      </Title>
      <ConditionsTable conditions={status?.conditions} />

      {ha && <HASection ha={ha} />}
    </div>
  );
};

export default OverviewTab;
