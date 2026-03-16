# High Availability DHCP Deployment Guide

This guide walks through deploying a Kea DHCPv4 server in **High Availability (HA) load-balancing mode** on OpenShift, including an optional MySQL backend using the Percona XtraDB Cluster Operator.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Prerequisites](#prerequisites)
- [Step 1: Install the Kea DHCP Operator](#step-1-install-the-kea-dhcp-operator)
- [Step 2: Prepare the Namespace](#step-2-prepare-the-namespace)
- [Step 3: Deploy a Basic HA Server (memfile)](#step-3-deploy-a-basic-ha-server-memfile)
- [Step 4: Verify HA Status](#step-4-verify-ha-status)
- [Step 5: Test DHCP Lease Acquisition](#step-5-test-dhcp-lease-acquisition)
- [Appendix A: HA with MySQL Backend (Percona XtraDB)](#appendix-a-ha-with-mysql-backend-percona-xtradb)
- [Appendix B: HA on a Secondary Network (NAD)](#appendix-b-ha-on-a-secondary-network-nad)
- [Troubleshooting](#troubleshooting)

---

## Architecture Overview

In HA mode, the operator deploys a **StatefulSet** with 2 replicas instead of a Deployment. Each pod runs the same Kea configuration except for the `this-server-name` field, which identifies the pod's role in the HA cluster.

```
                    ┌──────────────────────────────┐
                    │      KeaDhcp4Server CR        │
                    │      (replicas: 2, HA)        │
                    └──────────┬───────────────────┘
                               │ creates
          ┌────────────────────┼────────────────────┐
          │                    │                     │
          ▼                    ▼                     ▼
   ┌─────────────┐   ┌────────────────┐   ┌────────────────┐
   │  Headless   │   │   ConfigMap-0  │   │   ConfigMap-1  │
   │  Service    │   │ (server1)      │   │ (server2)      │
   │  (-hl)      │   └────────┬───────┘   └────────┬───────┘
   └─────┬───────┘            │                     │
         │            ┌───────▼───────┐     ┌───────▼───────┐
         │            │    Pod-0      │     │    Pod-1      │
         └──DNS──────►│  kea-dhcp4    │◄───►│  kea-dhcp4    │
                      │  (primary)    │ HA  │  (secondary)  │
                      └───────────────┘     └───────────────┘
```

The operator handles:
- **Per-ordinal ConfigMaps** — each pod gets a config with its own `this-server-name`
- **Init container** — selects the correct config based on pod ordinal and resolves DNS hostnames to IPs in peer URLs
- **Headless Service** — provides stable DNS names (`pod-0.svc-hl.ns.svc.cluster.local`)
- **Dedicated HTTP listener** — when using HTTP control socket, the HA hook binds on `0.0.0.0` for peer communication while the control socket stays on `127.0.0.1`
- **Hook injection** — `libdhcp_lease_cmds.so` and `libdhcp_ha.so` are automatically added
- **Pod anti-affinity** — HA pods are automatically spread across different nodes using preferred pod anti-affinity on `kubernetes.io/hostname`, ensuring primary and secondary peers don't share the same node (soft constraint — won't block scheduling if only one node is available)
- **Static peer addresses** — optionally assign fixed IP addresses to each peer on the NAD interface via the `address` field on each peer

---

## Prerequisites

- OpenShift 4.14+ or Kubernetes 1.28+ cluster
- `oc` or `kubectl` CLI with cluster-admin access
- The Kea DHCP Operator installed (see Step 1)
- For NAD-based deployments: OVN-Kubernetes with secondary network support

---

## Step 1: Install the Kea DHCP Operator

The operator should be installed in the `kea-dhcp-operator` namespace. This is separate from the `kea-system` namespace where DHCP server workloads run.

Install via the OLM bundle:

```bash
oc new-project kea-dhcp-operator || oc project kea-dhcp-operator
operator-sdk run bundle quay.io/mooyeg/ocp-kea-dhcp-bundle:v0.0.6 \
  -n kea-dhcp-operator --timeout 5m
```

Verify the operator is running:

```bash
kubectl get pods -n kea-dhcp-operator -l control-plane=controller-manager
```

Expected output:

```
NAME                                               READY   STATUS    RESTARTS   AGE
ocp-kea-dhcp-controller-manager-xxxxx-xxxxx        1/1     Running   0          30s
```

> **Note:** The operator runs in `kea-dhcp-operator` but manages DHCP servers in `kea-system` (or custom namespaces). On startup, it automatically creates `kea-system` and, on OpenShift, the `kea-dhcp` SCC.

---

## Step 2: Prepare the Namespace

The operator automatically creates the `kea-system` namespace on startup and, on OpenShift, applies the `kea-dhcp` SCC. When you deploy DHCP servers to `kea-system`, the operator also auto-binds the SCC to each server's ServiceAccount — no manual `oc adm policy` commands are needed.

> **Custom namespaces:** You can deploy DHCP servers in any namespace, but outside `kea-system` you must manually apply the SCC and bind it to the service account:
>
> ```bash
> oc apply -f config/scc/kea-dhcp-scc.yaml   # one-time, cluster-scoped
> oc adm policy add-scc-to-user kea-dhcp -z <cr-name>-kea -n <your-namespace>
> ```
>
> The service account name follows the pattern `<cr-name>-kea`. If your CR is named `my-dhcp`, the SA will be `my-dhcp-kea`.

---

## Step 3: Deploy a Basic HA Server (memfile)

This is the simplest HA deployment using the default in-memory lease file backend. No external database is required.

### 3.1 Peer URLs

Peer URLs are **optional**. If you omit the `url` field, the operator auto-generates it from the StatefulSet DNS convention:

```
http://<cr-name>-<component>-<ordinal>.<cr-name>-<component>-hl.<namespace>.svc.cluster.local:<port>/
```

For a CR named `dhcp4-ha` in namespace `kea-system` with port 8000:

| Peer Index | Auto-generated URL |
|------------|-------------------|
| 0 | `http://dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/` |
| 1 | `http://dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/` |

You can still set `url` explicitly on any peer to override the auto-generated value.

### 3.2 Create the KeaDhcp4Server CR

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4-ha
  namespace: kea-system
spec:
  replicas: 2

  interfaces-config:
    interfaces: ["net1"]           # NAD interface (not eth0)
    dhcp-socket-type: raw

  placement:
    podAnnotations:
      k8s.v1.cni.cncf.io/networks: "dhcp-net"   # Your NAD name

  # Subnet configuration
  subnet4:
    - id: 1
      subnet: "192.168.1.0/24"
      pools:
        - pool: "192.168.1.100 - 192.168.1.200"
      option-data:
        - name: routers
          data: "192.168.1.1"
        - name: domain-name-servers
          data: "8.8.8.8, 8.8.4.4"

  # Lease storage (default: memfile)
  lease-database:
    type: memfile
    persist: true
    lfc-interval: 3600

  # HTTP control socket (required for HA peer communication)
  control-socket:
    socket-type: http
    socket-port: 8000

  # Global timers
  valid-lifetime: 43200
  renew-timer: 21600
  rebind-timer: 32400

  # High Availability
  high-availability:
    this-server-name: server1    # Overridden per-pod automatically
    mode: load-balancing
    heartbeat-delay: 10000
    max-response-delay: 60000
    max-ack-delay: 10000
    max-unacked-clients: 10
    peers:
      - name: server1
        role: primary
        auto-failover: true
        # address: "10.200.0.5"    # Optional: static IP on NAD (must be in subnet, outside pool)
      - name: server2
        role: secondary
        auto-failover: true
        # address: "10.200.0.6"    # Optional: static IP on NAD (must be in subnet, outside pool)
    # URLs are auto-generated by the operator. To override, add:
    #   url: "http://custom-host:8000/"

  # Logging
  loggers:
    - name: kea-dhcp4
      severity: INFO
      output-options:
        - output: stdout
```

Apply it:

```bash
kubectl apply -f dhcp4-ha.yaml
```

### 3.3 Wait for Pods

```bash
kubectl get pods -n kea-system -l app.kubernetes.io/instance=dhcp4-ha -w
```

Both pods should reach `Running 1/1` within 30-60 seconds:

```
NAME               READY   STATUS    RESTARTS   AGE
dhcp4-ha-dhcp4-0   1/1     Running   0          45s
dhcp4-ha-dhcp4-1   1/1     Running   0          45s
```

---

## Step 4: Verify HA Status

### 4.1 Check HA State Transitions

Both pods should transition through `WAITING → SYNCING → READY → LOAD-BALANCING`:

```bash
kubectl logs -n kea-system dhcp4-ha-dhcp4-0 -c dhcp4 | grep HA_STATE_TRANSITION
```

Expected output:

```
HA_STATE_TRANSITION server1: server transitions from WAITING to SYNCING state, partner state is WAITING
HA_STATE_TRANSITION server1: server transitions from SYNCING to READY state, partner state is WAITING
HA_STATE_TRANSITION server1: server transitions from READY to LOAD-BALANCING state, partner state is READY
```

### 4.2 Verify Heartbeats

Ongoing heartbeats confirm HA peering is healthy:

```bash
kubectl logs -n kea-system dhcp4-ha-dhcp4-0 -c dhcp4 | grep -c "ha-heartbeat"
```

### 4.3 Check CR Status

```bash
kubectl get keadhcp4servers -n kea-system
```

```
NAME       READY     REPLICAS   AGE
dhcp4-ha   Running   2          2m
```

### 4.4 Check Init Container DNS Resolution

The init container resolves peer hostnames to IPs. Verify it worked:

```bash
kubectl logs -n kea-system dhcp4-ha-dhcp4-0 -c config-selector
```

```
Selected config for ordinal 0
Resolved dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local -> 10.x.x.x
Resolved dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local -> 10.x.x.x
```

---

## Step 5: Test DHCP Lease Acquisition

### 5.1 Create a Test Client Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: dhcp-test-client
  namespace: kea-system
spec:
  containers:
    - name: client
      image: busybox:1.37
      command: ["sleep", "infinity"]
```

```bash
kubectl apply -f dhcp-test-client.yaml
```

### 5.2 Request a DHCP Lease

```bash
# Create a udhcpc script (busybox doesn't ship one by default)
kubectl exec -n kea-system dhcp-test-client -- sh -c '
cat > /tmp/udhcpc.sh << "SCRIPT"
#!/bin/sh
case "$1" in
  bound|renew)
    ip addr flush dev $interface
    ip addr add $ip/$mask dev $interface
    [ -n "$router" ] && ip route add default via $router dev $interface
    ;;
esac
SCRIPT
chmod +x /tmp/udhcpc.sh
udhcpc -i eth0 -n -q -t 5 -s /tmp/udhcpc.sh'
```

Expected output:

```
udhcpc: started, v1.37.0
udhcpc: broadcasting discover
udhcpc: broadcasting select for 192.168.1.100, server 192.168.1.x
udhcpc: lease of 192.168.1.100 obtained from 192.168.1.x, lease time 43200
```

### 5.3 Verify Lease in Server Logs

```bash
kubectl logs -n kea-system dhcp4-ha-dhcp4-0 -c dhcp4 | grep DHCP4_LEASE_ALLOC
```

---

## Appendix A: HA with MySQL Backend (Percona XtraDB)

For production deployments, a MySQL-backed lease database provides persistent, replicated lease storage. This section uses the **Percona XtraDB Cluster Operator** to deploy a 3-node MySQL cluster.

### A.1 Install the Percona XtraDB Cluster Operator

```bash
# Create a namespace for the database
kubectl create namespace pxc

# Install the Percona operator (check https://docs.percona.com for the latest version)
kubectl apply -f https://raw.githubusercontent.com/percona/percona-xtradb-cluster-operator/v1.16.1/deploy/bundle.yaml -n pxc
```

Wait for the operator pod:

```bash
kubectl get pods -n pxc -l app.kubernetes.io/name=percona-xtradb-cluster-operator
```

### A.2 Create the Kea Database Secret

Create a Secret with the database credentials that both Percona and Kea will use:

```bash
kubectl create secret generic kea-db-credentials \
  -n kea-system \
  --from-literal=username=kea \
  --from-literal=password='<your-secure-password>'
```

### A.3 Deploy the Percona XtraDB Cluster

Create a PerconaXtraDBCluster CR. This is a minimal configuration — adjust sizing for production:

```yaml
apiVersion: pxc.percona.com/v1
kind: PerconaXtraDBCluster
metadata:
  name: kea-db
  namespace: pxc
spec:
  crVersion: "1.16.1"
  secretsName: kea-db-secrets

  pxc:
    size: 3
    image: percona/percona-xtradb-cluster:8.0.36-28.1
    resources:
      requests:
        memory: 512Mi
        cpu: 300m
    volumeSpec:
      persistentVolumeClaim:
        resources:
          requests:
            storage: 5Gi

  haproxy:
    enabled: true
    size: 2
    image: percona/percona-xtradb-cluster-operator:1.16.1-haproxy
    resources:
      requests:
        memory: 256Mi
        cpu: 200m
```

> **Note:** The Percona operator creates its own Secret named `<cluster>-secrets` with auto-generated passwords. You'll need to extract the `root` password to create the Kea database and user.

```bash
kubectl apply -f percona-xtradb-cluster.yaml -n pxc
```

Wait for all pods to be ready (this can take several minutes):

```bash
kubectl get pods -n pxc -l app.kubernetes.io/instance=kea-db -w
```

Expected (3 PXC nodes + 2 HAProxy):

```
kea-db-haproxy-0   2/2     Running   0          3m
kea-db-haproxy-1   2/2     Running   0          3m
kea-db-pxc-0       3/3     Running   0          5m
kea-db-pxc-1       3/3     Running   0          4m
kea-db-pxc-2       3/3     Running   0          3m
```

### A.4 Create the Kea Database

Connect to the MySQL cluster and create the `kea` database and user:

```bash
# Get the root password
ROOT_PWD=$(kubectl get secret kea-db-secrets -n pxc -o jsonpath='{.data.root}' | base64 -d)

# Connect via HAProxy
kubectl exec -n pxc -it kea-db-pxc-0 -c pxc -- \
  mysql -u root -p"${ROOT_PWD}" -e "
    CREATE DATABASE IF NOT EXISTS kea;
    CREATE USER IF NOT EXISTS 'kea'@'%' IDENTIFIED BY '<your-secure-password>';
    GRANT ALL PRIVILEGES ON kea.* TO 'kea'@'%';
    FLUSH PRIVILEGES;
  "
```

> **Important:** Kea automatically initializes the lease database schema on first connection. You do not need to manually create tables.

### A.5 Deploy the HA Server with MySQL

The MySQL endpoint is the HAProxy service: `kea-db-haproxy.pxc.svc.cluster.local:3306`.

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4-ha
  namespace: kea-system
spec:
  replicas: 2

  interfaces-config:
    interfaces: ["eth0"]

  subnet4:
    - id: 1
      subnet: "192.168.1.0/24"
      pools:
        - pool: "192.168.1.100 - 192.168.1.200"
      option-data:
        - name: routers
          data: "192.168.1.1"
        - name: domain-name-servers
          data: "8.8.8.8, 8.8.4.4"

  # MySQL lease database via Percona XtraDB HAProxy
  lease-database:
    type: mysql
    host: "kea-db-haproxy.pxc.svc.cluster.local"
    port: 3306
    name: kea
    credentialsSecretRef:
      name: kea-db-credentials    # Secret with keys: username, password
    connect-timeout: 10
    max-reconnect-tries: 5
    reconnect-wait-time: 2000
    on-fail: serve-retry-continue
    retry-on-startup: true

  control-socket:
    socket-type: http
    socket-port: 8000

  valid-lifetime: 43200
  renew-timer: 21600
  rebind-timer: 32400

  high-availability:
    this-server-name: server1
    mode: load-balancing
    heartbeat-delay: 10000
    max-response-delay: 60000
    max-ack-delay: 10000
    max-unacked-clients: 10
    peers:
      - name: server1
        role: primary
        auto-failover: true
      - name: server2
        role: secondary
        auto-failover: true

  loggers:
    - name: kea-dhcp4
      severity: INFO
      output-options:
        - output: stdout
```

#### Verify MySQL Connectivity

After the pods start, check the logs for database initialization:

```bash
kubectl logs -n kea-system dhcp4-ha-dhcp4-0 -c dhcp4 | grep -i "mysql\|database"
```

You should see Kea initializing the schema and connecting successfully.

### A.6 Cleaning Up Percona

To remove the Percona cluster:

```bash
kubectl delete perconaxtradbcluster kea-db -n pxc
kubectl delete pvc -l app.kubernetes.io/instance=kea-db -n pxc
kubectl delete -f https://raw.githubusercontent.com/percona/percona-xtradb-cluster-operator/v1.16.1/deploy/bundle.yaml -n pxc
kubectl delete namespace pxc
```

---

## Appendix B: HA on a Secondary Network (NAD)

On OpenShift with OVN-Kubernetes, you can run DHCP on a secondary VLAN network using a `ClusterUserDefinedNetwork` and `NetworkAttachmentDefinition`. This is typical for bare-metal or edge deployments where DHCP must serve a specific L2 segment.

### B.1 Create the Cluster User-Defined Network

```yaml
apiVersion: k8s.ovn.org/v1
kind: ClusterUserDefinedNetwork
metadata:
  name: dhcp
spec:
  namespaceSelector:
    matchExpressions:
      - key: dhcp_test
        operator: Exists
  network:
    localnet:
      ipam:
        mode: Disabled
      mtu: 1500
      physicalNetworkName: vmtrunk
      role: Secondary
      vlan:
        access:
          id: 500
        mode: Access
    topology: Localnet
    ipam:
      mode: Disabled
```

```bash
kubectl apply -f cudn-dhcp.yaml
```

### B.2 Label the Namespace

The CUDN's `namespaceSelector` requires a label. Label `kea-system` so OVN-Kubernetes creates the NAD there:

```bash
kubectl label namespace kea-system dhcp_test=true
```

This causes OVN-Kubernetes to create a `NetworkAttachmentDefinition` named `dhcp` in the namespace automatically.

> **Note:** The operator auto-creates `kea-system` and applies the SCC on startup. No manual SCC setup is needed when deploying to `kea-system`.

### B.3 Deploy the HA Server on NAD

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4-ha
  namespace: kea-system
spec:
  replicas: 2

  interfaces-config:
    interfaces: ["net1"]           # NAD interface (not eth0)
    dhcp-socket-type: raw          # Required for L2 DHCP on secondary networks

  subnet4:
    - id: 1
      subnet: "10.200.0.0/24"
      pools:
        - pool: "10.200.0.10 - 10.200.0.200"

  control-socket:
    socket-type: http
    socket-port: 8000

  high-availability:
    this-server-name: server1
    mode: load-balancing
    peers:
      - name: server1
        role: primary
        address: "10.200.0.5"     # Static IP for primary (in subnet, outside pool)
      - name: server2
        role: secondary
        address: "10.200.0.6"     # Static IP for secondary (in subnet, outside pool)

  placement:
    podAnnotations:
      k8s.v1.cni.cncf.io/networks: dhcp    # NAD name

  container:
    imagePullPolicy: Always
```

The operator detects `net1` as a NAD interface (anything that isn't `eth0`, `lo`, or `*`) and assigns IPs to each peer pod. You can specify static addresses using the `address` field on each peer (recommended — choose IPs in the subnet but outside the DHCP pool). If omitted, the operator auto-assigns IPs based on the pod ordinal (subnet base + ordinal + 2).

> **Note:** The operator automatically injects **pod anti-affinity** so the primary and secondary pods land on different nodes (soft constraint).

In the example above:
- Pod 0 (server1/primary): `10.200.0.5/24`
- Pod 1 (server2/secondary): `10.200.0.6/24`

### B.5 Verify NAD IPs

```bash
kubectl exec -n kea-system dhcp4-ha-dhcp4-0 -c dhcp4 -- ip addr show net1
```

```
3: net1@if1013: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500
    inet 10.200.0.5/24 scope global net1
```

You can also verify that the pods are scheduled on different nodes:

```bash
kubectl get pods -n kea-system -l app.kubernetes.io/instance=dhcp4-ha -o wide
```

### B.6 Test DHCP from a Client Pod

Create a client pod attached to the same NAD. The client can be in `kea-system` or any other namespace with the `dhcp_test` label:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: dhcp-client
  namespace: kea-system
  annotations:
    k8s.v1.cni.cncf.io/networks: dhcp
spec:
  containers:
    - name: client
      image: busybox:1.37
      command: ["sleep", "infinity"]
```

Run `udhcpc` (no special capabilities needed — DHCP client pods on a NAD use standard UDP broadcast sockets):

```bash
kubectl exec -n kea-system dhcp-client -- sh -c '
cat > /tmp/udhcpc.sh << "SCRIPT"
#!/bin/sh
case "$1" in
  bound|renew)
    ip addr flush dev $interface
    ip addr add $ip/$mask dev $interface
    ;;
esac
SCRIPT
chmod +x /tmp/udhcpc.sh
udhcpc -i net1 -n -q -t 5 -s /tmp/udhcpc.sh
ip addr show net1'
```

Expected output:

```
udhcpc: broadcasting select for 10.200.0.10, server 10.200.0.2
udhcpc: lease of 10.200.0.10 obtained from 10.200.0.2, lease time 4000
3: net1: <BROADCAST,MULTICAST,UP,LOWER_UP>
    inet 10.200.0.10/24 scope global net1
```

---

## Troubleshooting

### Pods stuck in `Init:0/1`

The init container is waiting for DNS resolution of peer hostnames. Check its logs:

```bash
kubectl logs -n <namespace> <pod-name> -c config-selector
```

Common causes:
- The headless Service doesn't exist yet (operator didn't reconcile it)
- DNS is slow in the cluster — the init container retries 15 times with 2-second intervals

### `Failed to convert string to address`

Kea's HA hook uses `inet_pton()` which requires numeric IP addresses, not DNS hostnames. This means the init container's DNS resolution failed. Check:

```bash
kubectl logs -n <namespace> <pod-name> -c config-selector | grep "WARNING"
```

If you see `WARNING: Could not resolve`, the peer hostname couldn't be resolved. Verify the headless Service exists:

```bash
kubectl get svc -n <namespace> -l app.kubernetes.io/instance=<cr-name>
```

### `standby servers not allowed in the load balancing configuration`

Load-balancing mode requires `primary` and `secondary` roles. The `standby` role is only valid for `hot-standby` mode.

| Mode | Valid Roles |
|------|-------------|
| `load-balancing` | `primary`, `secondary`, `backup` |
| `hot-standby` | `primary`, `standby`, `backup` |

### `bind: Address in use` on port 8000

Both the control socket and the HA hook are trying to bind on the same address/port. The operator auto-injects `http-dedicated-listener` to avoid this, but only when `control-socket.socket-type` is `http`. Verify your CR has:

```yaml
control-socket:
  socket-type: http
  socket-port: 8000
```

### Kea pod has no IPv4 on the NAD interface

If the NAD has IPAM disabled, the operator assigns IPs automatically only when it detects a NAD interface (not `eth0`/`lo`/`*`) in `interfaces-config.interfaces`. Verify:

```bash
kubectl exec <pod> -c dhcp4 -- ip addr show net1
```

If no `inet` address appears, check:
1. The `interfaces-config.interfaces` contains the NAD interface name (e.g., `net1`)
2. At least one entry exists in `subnet4`

### `udhcpc: no lease, failing`

The DHCP client can't reach the server. Check:
1. Client pod is on the same L2 network (same NAD annotation)
2. Kea pod has an IP on the DHCP interface
3. Kea is listening on the correct interface (check `interfaces-config`)

### HA peers stuck in `WAITING` state

Both peers are started but can't reach each other. Verify:
1. HTTP control socket port is reachable between pods
2. Peer URLs in the config point to the correct addresses (check init container DNS resolution logs)
3. No network policies blocking inter-pod traffic on the control socket port
