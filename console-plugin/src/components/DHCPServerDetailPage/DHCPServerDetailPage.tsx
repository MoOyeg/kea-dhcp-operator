import React from 'react';
import { useRouteMatch } from 'react-router-dom';
import { useK8sWatchResource } from '@openshift-console/dynamic-plugin-sdk';
import {
  Page,
  PageSection,
  Title,
  Tabs,
  Tab,
  TabTitleText,
  Spinner,
  Alert,
  Breadcrumb,
  BreadcrumbItem,
} from '@patternfly/react-core';

import { KeaDhcp4Server } from '../../types/kea-dhcp4server';
import { KeaDhcp6Server } from '../../types/kea-dhcp6server';
import { modelForPlural } from '../../types/resource-models';
import OverviewTab from './OverviewTab';
import SubnetsTab from './SubnetsTab';

/** Route params from console-extensions.json: /kea-dhcp/servers/:kind/:ns/:name */
interface RouteParams {
  kind: string; // "keadhcp4servers" or "keadhcp6servers"
  ns: string;
  name: string;
}

const DHCPServerDetailPage: React.FC = () => {
  const match = useRouteMatch<RouteParams>();
  const { kind, ns, name } = match.params;

  const model = modelForPlural(kind);
  const [activeTab, setActiveTab] = React.useState<string | number>('overview');

  const [resource, loaded, error] = useK8sWatchResource<
    KeaDhcp4Server | KeaDhcp6Server
  >(
    model
      ? {
          groupVersionKind: {
            group: model.apiGroup!,
            version: model.apiVersion,
            kind: model.kind,
          },
          name,
          namespace: ns,
        }
      : null,
  );

  if (!model) {
    return (
      <Page>
        <PageSection variant="light">
          <Alert variant="danger" title="Unknown resource type" isInline>
            Resource type &quot;{kind}&quot; is not recognized. Expected
            &quot;keadhcp4servers&quot; or &quot;keadhcp6servers&quot;.
          </Alert>
        </PageSection>
      </Page>
    );
  }

  if (error) {
    return (
      <Page>
        <PageSection variant="light">
          <Alert variant="danger" title="Error loading resource" isInline>
            {String(error)}
          </Alert>
        </PageSection>
      </Page>
    );
  }

  if (!loaded || !resource) {
    return (
      <Page>
        <PageSection variant="light">
          <div style={{ textAlign: 'center', padding: '2rem' }}>
            <Spinner size="lg" />
          </div>
        </PageSection>
      </Page>
    );
  }

  const displayKind = kind === 'keadhcp4servers' ? 'DHCPv4' : 'DHCPv6';

  return (
    <Page>
      <PageSection variant="light">
        <Breadcrumb>
          <BreadcrumbItem to="/kea-dhcp/servers">DHCP Servers</BreadcrumbItem>
          <BreadcrumbItem isActive>
            {name}
          </BreadcrumbItem>
        </Breadcrumb>
        <Title headingLevel="h1" style={{ marginTop: '0.5rem' }}>
          {displayKind} Server: {name}
        </Title>
        <p style={{ color: 'var(--pf-v5-global--Color--200)', marginTop: '0.25rem' }}>
          Namespace: {ns}
        </p>
      </PageSection>
      <PageSection variant="light" type="tabs">
        <Tabs
          activeKey={activeTab}
          onSelect={(_event, tabKey) => setActiveTab(tabKey)}
          aria-label="Server detail tabs"
        >
          <Tab eventKey="overview" title={<TabTitleText>Overview</TabTitleText>}>
            <PageSection variant="light">
              <OverviewTab resource={resource} kind={kind} />
            </PageSection>
          </Tab>
          <Tab eventKey="subnets" title={<TabTitleText>Subnets</TabTitleText>}>
            <PageSection variant="light">
              <SubnetsTab resource={resource} kind={kind} />
            </PageSection>
          </Tab>
        </Tabs>
      </PageSection>
    </Page>
  );
};

export default DHCPServerDetailPage;
