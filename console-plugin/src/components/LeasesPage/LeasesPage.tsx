import React from 'react';
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
  Toolbar,
  ToolbarContent,
  ToolbarItem,
  SearchInput,
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

import { DHCPv4Lease, DHCPv6Lease, leaseStateLabel } from '../../types/lease';
import { useLeases } from './useLeases';

/**
 * Compute expiry date from cltt (Client Last Transaction Time, epoch seconds)
 * and valid-lft (valid lifetime in seconds).
 */
function expiresAt(cltt: number, validLft: number): string {
  const expiryMs = (cltt + validLft) * 1000;
  return new Date(expiryMs).toLocaleString();
}

/** DHCPv4 leases table with filter. */
const Leases4Tab: React.FC = () => {
  const [leases, loaded, error] = useLeases('4');
  const [filter, setFilter] = React.useState('');

  if (error) {
    return (
      <Alert variant="warning" title="Unable to fetch DHCPv4 leases" isInline>
        The backend proxy may not be available, or no Kea Control Agent is deployed.
        {typeof error === 'object' && error !== null && 'message' in error
          ? ` Error: ${(error as { message: string }).message}`
          : ''}
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

  const filtered = filter
    ? (leases as DHCPv4Lease[]).filter(
        (l) =>
          l['ip-address'].toLowerCase().includes(filter.toLowerCase()) ||
          l.hostname.toLowerCase().includes(filter.toLowerCase()),
      )
    : (leases as DHCPv4Lease[]);

  if (filtered.length === 0) {
    return (
      <>
        <FilterToolbar filter={filter} onFilterChange={setFilter} />
        <EmptyState>
          <EmptyStateHeader
            titleText={filter ? 'No matching leases' : 'No DHCPv4 leases'}
            headingLevel="h3"
            icon={<EmptyStateIcon icon={SearchIcon} />}
          />
          <EmptyStateBody>
            {filter
              ? 'No leases match the current filter. Try a different search term.'
              : 'No active DHCPv4 leases were found. Ensure a Kea Control Agent is deployed and accessible.'}
          </EmptyStateBody>
        </EmptyState>
      </>
    );
  }

  const columns = [
    'IP Address',
    'MAC Address',
    'Hostname',
    'Subnet ID',
    'State',
    'Valid Lifetime',
    'Expires At',
  ];

  return (
    <>
      <FilterToolbar filter={filter} onFilterChange={setFilter} />
      <Table aria-label="DHCPv4 Leases" variant="compact">
        <Thead>
          <Tr>
            {columns.map((col) => (
              <Th key={col}>{col}</Th>
            ))}
          </Tr>
        </Thead>
        <Tbody>
          {filtered.map((lease, i) => (
            <Tr key={`${lease['ip-address']}-${i}`}>
              <Td dataLabel="IP Address">{lease['ip-address']}</Td>
              <Td dataLabel="MAC Address">{lease['hw-address']}</Td>
              <Td dataLabel="Hostname">{lease.hostname || '-'}</Td>
              <Td dataLabel="Subnet ID">{lease['subnet-id']}</Td>
              <Td dataLabel="State">{leaseStateLabel(lease.state)}</Td>
              <Td dataLabel="Valid Lifetime">{lease['valid-lft']}s</Td>
              <Td dataLabel="Expires At">
                {expiresAt(lease.cltt, lease['valid-lft'])}
              </Td>
            </Tr>
          ))}
        </Tbody>
      </Table>
    </>
  );
};

/** DHCPv6 leases table with filter. */
const Leases6Tab: React.FC = () => {
  const [leases, loaded, error] = useLeases('6');
  const [filter, setFilter] = React.useState('');

  if (error) {
    return (
      <Alert variant="warning" title="Unable to fetch DHCPv6 leases" isInline>
        The backend proxy may not be available, or no Kea Control Agent is deployed.
        {typeof error === 'object' && error !== null && 'message' in error
          ? ` Error: ${(error as { message: string }).message}`
          : ''}
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

  const filtered = filter
    ? (leases as DHCPv6Lease[]).filter(
        (l) =>
          l['ip-address'].toLowerCase().includes(filter.toLowerCase()) ||
          l.hostname.toLowerCase().includes(filter.toLowerCase()),
      )
    : (leases as DHCPv6Lease[]);

  if (filtered.length === 0) {
    return (
      <>
        <FilterToolbar filter={filter} onFilterChange={setFilter} />
        <EmptyState>
          <EmptyStateHeader
            titleText={filter ? 'No matching leases' : 'No DHCPv6 leases'}
            headingLevel="h3"
            icon={<EmptyStateIcon icon={SearchIcon} />}
          />
          <EmptyStateBody>
            {filter
              ? 'No leases match the current filter. Try a different search term.'
              : 'No active DHCPv6 leases were found. Ensure a Kea Control Agent is deployed and accessible.'}
          </EmptyStateBody>
        </EmptyState>
      </>
    );
  }

  const columns = [
    'IP Address',
    'DUID',
    'Hostname',
    'Subnet ID',
    'Type',
    'State',
    'Expires At',
  ];

  return (
    <>
      <FilterToolbar filter={filter} onFilterChange={setFilter} />
      <Table aria-label="DHCPv6 Leases" variant="compact">
        <Thead>
          <Tr>
            {columns.map((col) => (
              <Th key={col}>{col}</Th>
            ))}
          </Tr>
        </Thead>
        <Tbody>
          {filtered.map((lease, i) => (
            <Tr key={`${lease['ip-address']}-${i}`}>
              <Td dataLabel="IP Address">{lease['ip-address']}</Td>
              <Td dataLabel="DUID">{lease.duid}</Td>
              <Td dataLabel="Hostname">{lease.hostname || '-'}</Td>
              <Td dataLabel="Subnet ID">{lease['subnet-id']}</Td>
              <Td dataLabel="Type">{lease.type}</Td>
              <Td dataLabel="State">{leaseStateLabel(lease.state)}</Td>
              <Td dataLabel="Expires At">
                {expiresAt(lease.cltt, lease['valid-lft'])}
              </Td>
            </Tr>
          ))}
        </Tbody>
      </Table>
    </>
  );
};

/** Shared filter toolbar component. */
const FilterToolbar: React.FC<{
  filter: string;
  onFilterChange: (value: string) => void;
}> = ({ filter, onFilterChange }) => (
  <Toolbar>
    <ToolbarContent>
      <ToolbarItem>
        <SearchInput
          placeholder="Filter by IP address or hostname"
          value={filter}
          onChange={(_event, value) => onFilterChange(value)}
          onClear={() => onFilterChange('')}
          aria-label="Lease filter"
        />
      </ToolbarItem>
    </ToolbarContent>
  </Toolbar>
);

/** Main leases page component with tabs for DHCPv4 and DHCPv6 leases. */
const LeasesPage: React.FC = () => {
  const [activeTab, setActiveTab] = React.useState<string | number>('leases4');

  return (
    <Page>
      <PageSection variant="light">
        <Title headingLevel="h1">DHCP Leases</Title>
      </PageSection>
      <PageSection variant="light" type="tabs">
        <Tabs
          activeKey={activeTab}
          onSelect={(_event, tabKey) => setActiveTab(tabKey)}
          aria-label="Lease tabs"
        >
          <Tab
            eventKey="leases4"
            title={<TabTitleText>DHCPv4 Leases</TabTitleText>}
          >
            <PageSection variant="light">
              <Leases4Tab />
            </PageSection>
          </Tab>
          <Tab
            eventKey="leases6"
            title={<TabTitleText>DHCPv6 Leases</TabTitleText>}
          >
            <PageSection variant="light">
              <Leases6Tab />
            </PageSection>
          </Tab>
        </Tabs>
      </PageSection>
    </Page>
  );
};

export default LeasesPage;
