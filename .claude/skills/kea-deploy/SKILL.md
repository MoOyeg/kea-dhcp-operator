---
name: kea-deploy
description: >
  Deploy the OCP Kea DHCP operator and create HA DHCP instances on OpenShift/Kubernetes clusters.
  Use this skill whenever the user wants to: install or uninstall the Kea DHCP operator,
  create a DHCP HA instance with a NAD (Network Attachment Definition), set up Kea with Percona/MySQL
  lease database backend, deploy Kea DHCP servers, manage OLM bundle installations, or configure
  Kea HA (high availability) with stork monitoring. Trigger on mentions of "kea", "dhcp operator",
  "deploy dhcp", "ha instance", "percona lease database", "install kea operator", "NAD dhcp",
  or any combination of DHCP + OpenShift/Kubernetes deployment.
---

# Kea DHCP Operator Deployment Skill

This skill automates deploying the OCP Kea DHCP operator and creating HA DHCP instances. It supports
two primary deployment patterns: **NAD-based HA** (DHCP on a secondary network) and **Percona-backed HA**
(MySQL-compatible lease persistence).

## Prerequisites

Before starting, confirm:
1. **KUBECONFIG** is set or provided by the user (default: `/home/moyo/kubeconfig`)
2. `oc` or `kubectl` CLI is available
3. `operator-sdk` CLI is available (for OLM bundle installs)
4. For NAD mode: a NetworkAttachmentDefinition already exists on the cluster
5. For Percona mode: Percona XtraDB Cluster operator or a MySQL-compatible database is available

## Operator Source

The operator source code is at `/root/repos/ocp-kea-dhcp`. Key build commands:

```bash
make docker-build IMG=quay.io/mooyeg/ocp-kea-dhcp:v<VERSION>
make docker-push IMG=quay.io/mooyeg/ocp-kea-dhcp:v<VERSION>
make bundle IMG=quay.io/mooyeg/ocp-kea-dhcp:v<VERSION> VERSION=<VERSION>
make bundle-build bundle-push BUNDLE_IMG=quay.io/mooyeg/ocp-kea-dhcp-bundle:v<VERSION>
```

## Step 1: Install the Operator

### Uninstall existing (if any)

```bash
operator-sdk cleanup ocp-kea-dhcp -n kea-dhcp-operator
```

### Install via OLM bundle

```bash
operator-sdk run bundle quay.io/mooyeg/ocp-kea-dhcp-bundle:v<VERSION> \
  -n kea-dhcp-operator --timeout 5m
```

The operator runs in `kea-dhcp-operator` namespace. DHCP workloads run in `kea-system` (auto-created by the operator). The operator also auto-creates the `kea-dhcp` SCC and binds it to service accounts in `kea-system`.

### Verify installation

```bash
oc get csv -n kea-dhcp-operator
oc get pods -n kea-dhcp-operator
```

## Step 2: Create the HA DHCP Instance

Ask the user which mode they want if not specified:
- **NAD mode**: DHCP server listens on a secondary network interface via a NetworkAttachmentDefinition
- **Percona mode**: DHCP server uses Percona/MySQL for lease persistence instead of memfile

### Mode A: HA with NAD

The user must provide:
- **NAD name** (the `k8s.v1.cni.cncf.io/networks` annotation value)
- **Subnet** (CIDR, e.g. `192.168.50.0/24`)
- **Pool range** (e.g. `192.168.50.10-192.168.50.200`)
- **Gateway/router** (e.g. `192.168.50.1`)

Use `net1` as the interface name (standard for Multus secondary interfaces).

Template:

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: <NAME>
  namespace: kea-system
spec:
  replicas: 2
  container:
    image: docker.cloudsmith.io/isc/docker/kea-dhcp4:3.0.2
    imagePullPolicy: IfNotPresent
  interfaces-config:
    interfaces:
      - net1
  control-socket:
    socket-type: unix
    socket-name: /var/run/kea/kea-dhcp4-ctrl.sock
  lease-database:
    type: memfile
    persist: true
    lfc-interval: 3600
  high-availability:
    this-server-name: ""
    mode: load-balancing
    heartbeat-delay: 10000
    max-response-delay: 60000
    max-ack-delay: 5000
    max-unacked-clients: 5
    peers:
      - name: server1
        role: primary
        url: ""
      - name: server2
        role: secondary
        url: ""
  stork:
    enabled: true
    image: quay.io/mooyeg/stork-agent:v2.4.0-1
    imagePullPolicy: IfNotPresent
    prometheusExporterPort: 9547
  placement:
    podAnnotations:
      k8s.v1.cni.cncf.io/networks: "<NAD_NAME>"
  subnet4:
    - id: 1
      subnet: "<SUBNET_CIDR>"
      pools:
        - pool: "<POOL_RANGE>"
      option-data:
        - name: routers
          data: "<GATEWAY>"
        - name: domain-name-servers
          data: "8.8.8.8, 8.8.4.4"
  valid-lifetime: 43200
  renew-timer: 21600
  rebind-timer: 32400
  loggers:
    - name: kea-dhcp4
      severity: INFO
      output-options:
        - output: stdout
```

### Mode B: HA with Percona/MySQL

This mode adds a MySQL lease database for durable lease storage across pod restarts. The user must provide:
- All the NAD fields above (or `eth0` for host-network mode)
- **MySQL host** (e.g. `percona-xtradb-cluster-haproxy.percona.svc.cluster.local`)
- **MySQL port** (default: 3306)
- **Database name** (default: `kea`)
- **Credentials Secret name** (must contain `username` and `password` keys)

#### Step B.1: Create the database credentials Secret

```bash
oc create secret generic kea-db-credentials \
  -n kea-system \
  --from-literal=username=kea \
  --from-literal=password='<PASSWORD>'
```

#### Step B.2: Initialize the Kea database schema

Kea requires its schema to be pre-loaded. Run a one-shot job against the MySQL instance:

```bash
oc run kea-db-init --rm -it --restart=Never \
  -n kea-system \
  --image=docker.cloudsmith.io/isc/docker/kea-dhcp4:3.0.2 \
  --command -- kea-admin db-init mysql \
    -u <USERNAME> -p <PASSWORD> \
    -n <DATABASE> -h <MYSQL_HOST>
```

If the database already has the schema, this is safe to skip.

#### Step B.3: Apply the CR

The CR is identical to Mode A except for the `lease-database` section:

```yaml
  lease-database:
    type: mysql
    name: "<DATABASE_NAME>"
    host: "<MYSQL_HOST>"
    port: 3306
    credentialsSecretRef:
      name: kea-db-credentials
    connect-timeout: 5
    max-reconnect-tries: 3
    reconnect-wait-time: 1000
    on-fail: serve-retry-exit
    retry-on-startup: true
    read-timeout: 5
    write-timeout: 5
```

## Step 3: Verify Deployment

```bash
# Check pods are running (expect 2 pods, 2 containers each: kea + stork-agent)
oc get pods -n kea-system -o wide

# Check CR status
oc get kd4 -n kea-system

# Check PodMonitor was created (when stork is enabled)
oc get podmonitors -n kea-system

# Check HA heartbeat in logs
oc logs -n kea-system <POD_NAME> -c kea-dhcp4 | grep -i "heartbeat"

# Check stork-agent is detecting Kea
oc logs -n kea-system <POD_NAME> -c stork-agent | tail -20
```

## Important Notes

- **HA roles**: Use `primary`/`secondary` for load-balancing mode, `primary`/`standby` for hot-standby mode. Never use `standby` in load-balancing.
- **Control socket path**: Must use `/var/run/kea/` prefix (Kea 3.0+ validates this).
- **Peer URLs**: Leave empty (`""`) — the operator auto-generates them from the StatefulSet headless Service DNS.
- **this-server-name**: Leave empty (`""`) — the operator sets it per-ordinal via init container.
- **NAD interface**: Always `net1` for the first Multus secondary interface.
- **Image pull**: Use `IfNotPresent` for efficiency. Bump version tag if you rebuild.
- **SCC**: The operator auto-creates the `kea-dhcp` SCC and binds it in `kea-system`. Custom namespaces need manual SCC binding.
- **Stork sidecar**: When enabled, the operator also creates a PodMonitor for Prometheus scraping.
- **Percona vs MySQL**: Percona XtraDB is wire-compatible with MySQL. Use `type: mysql` for both.
