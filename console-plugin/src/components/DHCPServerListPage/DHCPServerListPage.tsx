import React from 'react';
import { useHistory } from 'react-router-dom';
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk';
import {
  Page,
  PageSection,
  Title,
  Tabs,
  Tab,
  TabTitleText,
  EmptyState,
  EmptyStateBody,
  EmptyStateHeader,
  EmptyStateIcon,
  Spinner,
  Alert,
} from '@patternfly/react-core';
import { SearchIcon } from '@patternfly/react-icons';
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
import {
  KeaDhcp4ServerModel,
  KeaDhcp6ServerModel,
} from '../../types/resource-models';
import StatusIcon from '../shared/StatusIcon';

/** Compute a human-readable relative age from a creation timestamp. */
function formatAge(timestamp?: string): string {
  if (!timestamp) return '-';
  const created = new Date(timestamp).getTime();
  const now = Date.now();
  const diffSec = Math.floor((now - created) / 1000);
  if (diffSec < 60) return `${diffSec}s`;
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h`;
  const diffDay = Math.floor(diffHr / 24);
  return `${diffDay}d`;
}

/** DHCPv4 server table */
const Dhcp4Table: React.FC = () => {
  const history = useHistory();

  const [servers, loaded, error] = useK8sWatchResource<KeaDhcp4Server[]>({
    groupVersionKind: {
      group: KeaDhcp4ServerModel.apiGroup!,
      version: KeaDhcp4ServerModel.apiVersion,
      kind: KeaDhcp4ServerModel.kind,
    },
    isList: true,
    namespaced: true,
  });

  if (error) {
    return (
      <Alert variant="danger" title="Error loading DHCPv4 servers" isInline>
        {String(error)}
      </Alert>
    );
  }

  if (!loaded) {
    return (
      <div style={{ textAlign: 'center', padding: '2rem' }}>
        <Spinner size="lg" />
      </div>
    );
  }

  if (!servers || servers.length === 0) {
    return (
      <EmptyState>
        <EmptyStateHeader
          titleText="No DHCPv4 Servers found"
          headingLevel="h3"
          icon={<EmptyStateIcon icon={SearchIcon} />}
        />
        <EmptyStateBody>
          No KeaDhcp4Server resources exist in the cluster. Create one to get started.
        </EmptyStateBody>
      </EmptyState>
    );
  }

  const columns = ['Name', 'Namespace', 'Phase', 'Ready Replicas', 'Subnets', 'Age'];

  return (
    <Table aria-label="DHCPv4 Servers" variant="compact">
      <Thead>
        <Tr>
          {columns.map((col) => (
            <Th key={col}>{col}</Th>
          ))}
        </Tr>
      </Thead>
      <Tbody>
        {servers.map((server) => {
          const name = server.metadata?.name ?? '';
          const ns = server.metadata?.namespace ?? '';
          const phase = server.status?.phase ?? '-';
          const readyReplicas = server.status?.readyReplicas ?? 0;
          const subnetCount = server.spec?.subnet4?.length ?? 0;
          const age = formatAge(server.metadata?.creationTimestamp);

          return (
            <Tr
              key={`${ns}/${name}`}
              isClickable
              onRowClick={() =>
                history.push(`/kea-dhcp/servers/keadhcp4servers/${ns}/${name}`)
              }
            >
              <Td dataLabel="Name">
                <a
                  href={`/kea-dhcp/servers/keadhcp4servers/${ns}/${name}`}
                  onClick={(e) => {
                    e.preventDefault();
                    history.push(`/kea-dhcp/servers/keadhcp4servers/${ns}/${name}`);
                  }}
                >
                  {name}
                </a>
              </Td>
              <Td dataLabel="Namespace">{ns}</Td>
              <Td dataLabel="Phase">
                <StatusIcon phase={phase} /> {phase}
              </Td>
              <Td dataLabel="Ready Replicas">{readyReplicas}</Td>
              <Td dataLabel="Subnets">{subnetCount}</Td>
              <Td dataLabel="Age">{age}</Td>
            </Tr>
          );
        })}
      </Tbody>
    </Table>
  );
};

/** DHCPv6 server table */
const Dhcp6Table: React.FC = () => {
  const history = useHistory();

  const [servers, loaded, error] = useK8sWatchResource<KeaDhcp6Server[]>({
    groupVersionKind: {
      group: KeaDhcp6ServerModel.apiGroup!,
      version: KeaDhcp6ServerModel.apiVersion,
      kind: KeaDhcp6ServerModel.kind,
    },
    isList: true,
    namespaced: true,
  });

  if (error) {
    return (
      <Alert variant="danger" title="Error loading DHCPv6 servers" isInline>
        {String(error)}
      </Alert>
    );
  }

  if (!loaded) {
    return (
      <div style={{ textAlign: 'center', padding: '2rem' }}>
        <Spinner size="lg" />
      </div>
    );
  }

  if (!servers || servers.length === 0) {
    return (
      <EmptyState>
        <EmptyStateHeader
          titleText="No DHCPv6 Servers found"
          headingLevel="h3"
          icon={<EmptyStateIcon icon={SearchIcon} />}
        />
        <EmptyStateBody>
          No KeaDhcp6Server resources exist in the cluster. Create one to get started.
        </EmptyStateBody>
      </EmptyState>
    );
  }

  const columns = ['Name', 'Namespace', 'Phase', 'Ready Replicas', 'Subnets', 'Age'];

  return (
    <Table aria-label="DHCPv6 Servers" variant="compact">
      <Thead>
        <Tr>
          {columns.map((col) => (
            <Th key={col}>{col}</Th>
          ))}
        </Tr>
      </Thead>
      <Tbody>
        {servers.map((server) => {
          const name = server.metadata?.name ?? '';
          const ns = server.metadata?.namespace ?? '';
          const phase = server.status?.phase ?? '-';
          const readyReplicas = server.status?.readyReplicas ?? 0;
          const subnetCount = server.spec?.subnet6?.length ?? 0;
          const age = formatAge(server.metadata?.creationTimestamp);

          return (
            <Tr
              key={`${ns}/${name}`}
              isClickable
              onRowClick={() =>
                history.push(`/kea-dhcp/servers/keadhcp6servers/${ns}/${name}`)
              }
            >
              <Td dataLabel="Name">
                <a
                  href={`/kea-dhcp/servers/keadhcp6servers/${ns}/${name}`}
                  onClick={(e) => {
                    e.preventDefault();
                    history.push(`/kea-dhcp/servers/keadhcp6servers/${ns}/${name}`);
                  }}
                >
                  {name}
                </a>
              </Td>
              <Td dataLabel="Namespace">{ns}</Td>
              <Td dataLabel="Phase">
                <StatusIcon phase={phase} /> {phase}
              </Td>
              <Td dataLabel="Ready Replicas">{readyReplicas}</Td>
              <Td dataLabel="Subnets">{subnetCount}</Td>
              <Td dataLabel="Age">{age}</Td>
            </Tr>
          );
        })}
      </Tbody>
    </Table>
  );
};

/** Main list page component with tabs for DHCPv4 and DHCPv6 servers. */
const DHCPServerListPage: React.FC = () => {
  const [activeTab, setActiveTab] = React.useState<string | number>('dhcp4');

  return (
    <Page>
      <PageSection variant="light">
        <Title headingLevel="h1">DHCP Servers</Title>
      </PageSection>
      <PageSection variant="light" type="tabs">
        <Tabs
          activeKey={activeTab}
          onSelect={(_event, tabKey) => setActiveTab(tabKey)}
          aria-label="DHCP server tabs"
        >
          <Tab eventKey="dhcp4" title={<TabTitleText>DHCPv4 Servers</TabTitleText>}>
            <PageSection variant="light">
              <Dhcp4Table />
            </PageSection>
          </Tab>
          <Tab eventKey="dhcp6" title={<TabTitleText>DHCPv6 Servers</TabTitleText>}>
            <PageSection variant="light">
              <Dhcp6Table />
            </PageSection>
          </Tab>
        </Tabs>
      </PageSection>
    </Page>
  );
};

export default DHCPServerListPage;
