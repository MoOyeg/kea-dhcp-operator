# Custom Resource Definitions

The operator defines five CRDs under the `kea.openshift.io/v1alpha1` API group. Four manage individual Kea daemons, and one is an umbrella resource that can deploy any combination of them.

## KeaDhcp4Server (kd4)

Manages a **Kea DHCPv4 server** (`kea-dhcp4` daemon).

### What It Does

- Renders a `kea-dhcp4.conf` JSON configuration from the CRD spec
- Creates a Deployment running the `kea-dhcp4` daemon
- Stores configuration in a ConfigMap mounted at `/etc/kea/`
- Optionally creates a Service for HTTP control socket access
- When `high-availability` is specified, automatically injects the HA and lease_cmds hook libraries

### Key Spec Fields

**Required:**
- `interfaces-config.interfaces` — network interfaces to listen on (e.g., `["eth0"]`)

**Network Configuration:**
- `subnet4[]` — IPv4 subnets with address pools, DHCP options, and host reservations
- `shared-networks[]` — groups of subnets sharing common settings
- `option-data[]` — global DHCP options (routers, DNS servers, domain name, etc.)
- `option-def[]` — custom option type definitions
- `client-classes[]` — classification rules with boolean expressions

**Lease Storage:**
- `lease-database` — backend: `memfile` (default, CSV file), `mysql`, or `postgresql`
- `hosts-database` / `hosts-databases` — reservation data sources

**Timers:**
- `valid-lifetime` — how long leases are valid (seconds, default: 4000)
- `renew-timer` — when clients should start renewal (T1)
- `rebind-timer` — when clients should start rebinding (T2)

**Advanced:**
- `hooks-libraries[]` — Kea hook libraries with arbitrary JSON parameters
- `control-socket` — management socket (unix or http)
- `loggers[]` — logging configuration
- `high-availability` — HA configuration (see below)
- `multi-threading` — thread pool and packet queue settings
- DDNS parameters (`ddns-send-updates`, `ddns-qualifying-suffix`, etc.)
- PXE boot parameters (`next-server`, `boot-file-name`)

**Deployment:**
- `container.image` — override the default per-component Kea container image (defaults: `kea-dhcp4`, `kea-dhcp6`, `kea-ctrl-agent`, `kea-dhcp-ddns` from `docker.cloudsmith.io/isc/docker/`)
- `replicas` — number of pods (set to 2 for HA)
- `hostNetwork` — enable host networking for direct L2 access
- `placement` — nodeSelector, tolerations, affinity

### Status

| Field | Description |
|-------|-------------|
| `phase` | `Pending`, `Progressing`, `Running`, or `Error` |
| `readyReplicas` | Number of ready pods |
| `configHash` | SHA-256 hash of the rendered configuration |
| `configMapRef` | Name of the ConfigMap containing the config |
| `conditions` | `Ready`, `ConfigurationValid`, `Progressing` |

---

## KeaDhcp6Server (kd6)

Manages a **Kea DHCPv6 server** (`kea-dhcp6` daemon).

### What It Does

Same as KeaDhcp4Server but for IPv6. Renders `kea-dhcp6.conf`.

### Key Differences from DHCPv4

- `subnet6[]` instead of `subnet4[]`
- Address pools use IPv6 ranges (e.g., `"2001:db8:1::1 - 2001:db8:1::ffff"`)
- **Prefix Delegation** support via `pd-pools[]`:
  - `prefix` — delegated prefix (e.g., `"2001:db8:8::"`)
  - `prefix-len` — length of the prefix block
  - `delegated-len` — length of each delegated prefix
  - `excluded-prefix` / `excluded-prefix-len` — exclusions
- `preferred-lifetime` — IPv6-specific T_pref timer
- `rapid-commit` — enable 2-message exchange (Solicit → Reply)
- `interface-id` — relay-supplied interface identification

All other fields (database, hooks, HA, logging, container settings) are identical to DHCPv4.

---

## KeaControlAgent (kca)

Manages the **Kea Control Agent** (`kea-ctrl-agent` daemon).

### What It Does

The Control Agent exposes a RESTful HTTP API for managing Kea daemons. It is required for:
- HA peer communication between DHCP servers
- Remote management and monitoring
- Sending configuration and lease commands

The operator:
- Renders `kea-ctrl-agent.conf`
- Creates a Deployment running `kea-ctrl-agent`
- Always creates a Service exposing the HTTP port

### Key Spec Fields

**HTTP Endpoint:**
- `http-host` — listen address (default: `"0.0.0.0"`)
- `http-port` — listen port (default: `8000`)

**TLS:**
- `tls.secretRef` — reference to a `kubernetes.io/tls` Secret
- `tls.certRequired` — whether client certificates are required

**Daemon Communication:**
- `control-sockets` — UNIX control sockets for each managed daemon:
  - `dhcp4` — DHCPv4 server socket (e.g., `/run/kea/kea-dhcp4-ctrl.sock`)
  - `dhcp6` — DHCPv6 server socket
  - `d2` — DDNS server socket

**Authentication:**
- `authentication.type` — `"basic"` (only supported type)
- `authentication.realm` — auth realm
- `authentication.clients[]` — inline credentials (user/password)
- `authentication.credentialsSecretRef` — reference to a Secret with credentials

### Relationship to Other Components

The Control Agent acts as a gateway to other Kea daemons. In a typical deployment:

```
External Client → KeaControlAgent (HTTP :8000)
                    → kea-dhcp4 (unix socket)
                    → kea-dhcp6 (unix socket)
                    → kea-dhcp-ddns (unix socket)
```

For HA, each DHCP server's partner communicates through the Control Agent:

```
Server1 kea-dhcp4 → Server2 KeaControlAgent → Server2 kea-dhcp4
```

---

## KeaDhcpDdns (kdd)

Manages the **Kea DHCP-DDNS** server (`kea-dhcp-ddns`, also known as D2).

### What It Does

D2 performs dynamic DNS updates on behalf of the DHCP servers. When a DHCP lease is granted or released, the DHCP server sends a Name Change Request (NCR) to D2, which then updates the appropriate DNS server using RFC 2136 (DNS UPDATE) and optionally RFC 2845 (TSIG authentication).

The operator:
- Renders `kea-dhcp-ddns.conf`
- Creates a Deployment running `kea-dhcp-ddns`
- Optionally creates a UDP Service

### Key Spec Fields

**Listener:**
- `ip-address` — address to listen for NCR messages from DHCP servers (default: `"127.0.0.1"`)
- `port` — NCR listener port (default: `53001`)
- `ncr-protocol` — `"UDP"` (default) or `"TCP"`
- `ncr-format` — `"JSON"` (only supported format)
- `dns-server-timeout` — timeout for DNS operations in ms (default: `500`)

**TSIG Keys:**
- `tsig-keys[]` — keys for authenticated DNS updates:
  - `name` — key name
  - `algorithm` — `"HMAC-MD5"`, `"HMAC-SHA256"`, `"HMAC-SHA512"`
  - `secret` or `secretRef` — base64-encoded key material

**DNS Zones:**
- `forward-ddns.ddns-domains[]` — forward DNS zones to update:
  - `name` — zone name (e.g., `"example.org."`)
  - `key-name` — TSIG key to use
  - `dns-servers[]` — target DNS servers with `ip-address` and `port`
- `reverse-ddns.ddns-domains[]` — reverse DNS zones (same structure)

### Integration with DHCP Servers

DHCP servers must be configured to send NCR messages to D2:
- Set `ddns-send-updates: true` on KeaDhcp4Server or KeaDhcp6Server
- Set `ddns-qualifying-suffix` to the DNS domain
- D2 listens on the configured address/port for NCR messages

---

## KeaServer (ks)

An **umbrella resource** that deploys any combination of Kea components through a single CR.

### What It Does

Instead of creating individual KeaDhcp4Server, KeaDhcp6Server, KeaControlAgent, and KeaDhcpDdns resources separately, you can create a single KeaServer that specifies all desired components. The KeaServer controller:

1. For each non-nil component in the spec, creates a child CRD with owner reference
2. For components removed from the spec, deletes the corresponding child CRD
3. Monitors all child CRDs and aggregates their status

### Spec

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaServer
metadata:
  name: my-kea
spec:
  dhcp4:          # Creates KeaDhcp4Server "my-kea-dhcp4"
    <KeaDhcp4ServerSpec>
  dhcp6:          # Creates KeaDhcp6Server "my-kea-dhcp6"
    <KeaDhcp6ServerSpec>
  controlAgent:   # Creates KeaControlAgent "my-kea-ctrl-agent"
    <KeaControlAgentSpec>
  dhcpDdns:       # Creates KeaDhcpDdns "my-kea-ddns"
    <KeaDhcpDdnsSpec>
```

Each section is optional. Only specified components are deployed.

### Child Naming

| Parent KeaServer | Component | Child CRD Name |
|------------------|-----------|----------------|
| `my-kea` | DHCPv4 | `my-kea-dhcp4` |
| `my-kea` | DHCPv6 | `my-kea-dhcp6` |
| `my-kea` | Control Agent | `my-kea-ctrl-agent` |
| `my-kea` | DDNS | `my-kea-ddns` |

### Status

| Field | Description |
|-------|-------------|
| `phase` | `Pending` (no components), `Running` (all ready), `Progressing` (some not ready) |
| `dhcp4Ready` | Whether the DHCPv4 child is running |
| `dhcp6Ready` | Whether the DHCPv6 child is running |
| `controlAgentReady` | Whether the Control Agent child is running |
| `dhcpDdnsReady` | Whether the DDNS child is running |

### Lifecycle

- Deleting the KeaServer automatically deletes all child CRDs (via owner references)
- Removing a component section from the spec deletes only that child
- Updating a component section updates the corresponding child CRD

---

## High Availability

HA is configured via the `high-availability` field on KeaDhcp4Server (DHCPv6 HA is not yet supported). When set, the operator automatically:

1. Injects `libdhcp_lease_cmds.so` (required for lease synchronization)
2. Injects `libdhcp_ha.so` with the HA configuration
3. Switches the workload from a Deployment to a **StatefulSet**
4. Creates a **headless Service** for stable per-pod DNS names
5. Creates **per-ordinal ConfigMaps** with each pod's `this-server-name` set correctly
6. Runs an **init container** that selects the right ConfigMap based on pod ordinal and resolves peer DNS hostnames to IP addresses (Kea requires numeric IPs in peer URLs)
7. When the control socket is `http`, auto-injects a **dedicated HTTP listener** (`http-dedicated-listener`) in the HA hook so that peer communication binds on `0.0.0.0` while the control socket stays on `127.0.0.1`
8. Injects **pod anti-affinity** so primary and secondary pods are scheduled on different nodes (preferred/soft constraint)
9. Supports **static peer addresses** — use the `address` field on each peer to assign fixed IPs on the NAD interface (must be in the subnet but outside the DHCP pool)

### Modes

| Mode | Peer Roles | Behavior |
|------|------------|----------|
| `load-balancing` | `primary` + `secondary` | Both servers actively serve leases, splitting the pool |
| `hot-standby` | `primary` + `standby` | Primary serves all leases; standby takes over on failure |

### How It Works

The operator creates the following resources for an HA deployment:

```
KeaDhcp4Server "dhcp4-ha" (replicas: 2)
  ├── ConfigMap "dhcp4-ha-dhcp4-0"     (this-server-name: server1)
  ├── ConfigMap "dhcp4-ha-dhcp4-1"     (this-server-name: server2)
  ├── Service "dhcp4-ha-dhcp4-hl"      (headless, ClusterIP: None)
  ├── StatefulSet "dhcp4-ha-dhcp4"
  │     ├── Pod "dhcp4-ha-dhcp4-0"     (init: copies ConfigMap-0, resolves DNS)
  │     └── Pod "dhcp4-ha-dhcp4-1"     (init: copies ConfigMap-1, resolves DNS)
  └── ServiceAccount "dhcp4-ha-kea"
```

Each pod's init container:
1. Determines its ordinal from the hostname (`${HOSTNAME##*-}`)
2. Copies the matching ConfigMap's config file to a shared emptyDir volume
3. Resolves all peer hostnames to IP addresses using `nslookup` and replaces them in the config (Kea's HA hook uses `inet_pton` and cannot resolve DNS names)

The headless Service has `publishNotReadyAddresses: true` so that DNS records are available before pods pass readiness checks (required for the init container DNS resolution). The StatefulSet uses `Parallel` pod management policy so both pods start simultaneously.

### Minimal HA Example

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
  control-socket:
    socket-type: http
    socket-port: 8000
  high-availability:
    this-server-name: server1    # Overridden per-pod by the operator
    mode: load-balancing
    heartbeat-delay: 10000
    max-response-delay: 60000
    max-unacked-clients: 10
    peers:
      - name: server1
        url: "http://dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/"
        role: primary
        auto-failover: true
      - name: server2
        url: "http://dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/"
        role: secondary
        auto-failover: true
```

### HA with Network Attachment Definition (NAD)

When running on OpenShift with a secondary network (e.g., a VLAN via OVN-Kubernetes `ClusterUserDefinedNetwork`), the operator can automatically assign per-pod IPs on the NAD interface. This is needed when the NAD has IPAM disabled.

The operator derives the NAD interface from `interfaces-config.interfaces` (skipping `eth0`, `lo`, and `*`) and the subnet from the first entry in `subnet4`. You can assign static IPs to each peer using the `address` field (recommended — choose IPs in the subnet but outside the DHCP pool). If `address` is omitted, the operator auto-assigns IPs as subnet base + ordinal + 2 (e.g., for `10.200.0.0/24`, pod-0 gets `10.200.0.2`, pod-1 gets `10.200.0.3`).

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4-ha
  namespace: kea-system
spec:
  replicas: 2
  interfaces-config:
    interfaces: ["net1"]           # NAD interface name
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
        url: "http://dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/"
        role: primary
        address: "10.200.0.5"     # Static IP on NAD (in subnet, outside pool)
      - name: server2
        url: "http://dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/"
        role: secondary
        address: "10.200.0.6"     # Static IP on NAD (in subnet, outside pool)
  placement:
    podAnnotations:
      k8s.v1.cni.cncf.io/networks: dhcp    # NAD name
  container:
    imagePullPolicy: Always
```

**Prerequisites for NAD-based HA:**

1. A `ClusterUserDefinedNetwork` or equivalent CNI network must exist (e.g., localnet topology, VLAN 200)
2. A `NetworkAttachmentDefinition` named in the pod annotation must be available in the namespace — label `kea-system` to match your CUDN's `namespaceSelector` (e.g., `kubectl label namespace kea-system dhcp_test=true`)
3. When deploying to `kea-system`, the operator automatically applies the `kea-dhcp` SCC and binds it to the service account — no manual steps needed
4. For custom namespaces, manually apply the SCC:
   ```bash
   oc apply -f config/scc/kea-dhcp-scc.yaml   # one-time, cluster-scoped
   oc adm policy add-scc-to-user kea-dhcp -z <cr-name>-kea -n <your-namespace>
   ```
5. DHCP client pods on a NAD do **not** need extra capabilities — they use standard UDP broadcast sockets for DHCP

### Peer URL Format

Peer URLs must follow the StatefulSet DNS pattern:

```
http://<cr-name>-<component>-<ordinal>.<cr-name>-<component>-hl.<namespace>.svc.cluster.local:<port>/
```

For a CR named `dhcp4-ha` in namespace `kea-system` with `socket-port: 8000`:

| Peer | URL |
|------|-----|
| Pod 0 | `http://dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/` |
| Pod 1 | `http://dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.kea-system.svc.cluster.local:8000/` |

The naming components are:
- `<cr-name>-<component>-<ordinal>` — StatefulSet pod hostname
- `<cr-name>-<component>-hl` — headless Service name (suffix `-hl`)

### HA Spec Fields

| Field | Default | Description |
|-------|---------|-------------|
| `this-server-name` | (required) | Server name in the HA cluster; overridden per-pod by the operator |
| `mode` | `load-balancing` | `load-balancing` or `hot-standby` |
| `heartbeat-delay` | `10000` | Heartbeat interval in milliseconds |
| `max-response-delay` | `60000` | Max wait time for partner response (ms) |
| `max-ack-delay` | `10000` | Max wait time for acknowledgment (ms) |
| `max-unacked-clients` | `10` | Unacked clients before failover (0 = immediate) |
| `send-lease-updates` | `true` | Send lease updates to partner |
| `sync-leases` | `true` | Sync leases on startup |
| `sync-timeout` | — | Sync timeout in milliseconds |
| `sync-page-limit` | — | Leases per sync page |
| `delayed-updates-limit` | — | Max queued lease updates |
| `peers[]` | (required) | At least 2 peers (see below) |
| `tls` | — | TLS configuration for peer communication |
| `multi-threading` | — | HA-specific multi-threading settings |

### Peer Fields

| Field | Description |
|-------|-------------|
| `name` | Unique peer name; must match `this-server-name` on the corresponding pod |
| `url` | Peer's HTTP endpoint URL (auto-generated if omitted; must use StatefulSet DNS format if set) |
| `role` | `primary`, `secondary` (load-balancing), `standby` (hot-standby), or `backup` |
| `address` | Static IP address on the NAD interface; must be in the subnet but outside the DHCP pool (optional — auto-assigned if omitted) |
| `auto-failover` | Enable automatic failover for this peer (default: `true`) |

### TLS for HA

Optionally secure peer communication with TLS:

```yaml
high-availability:
  tls:
    secretRef:
      name: kea-ha-tls
    certRequired: true
```

The referenced Secret must be of type `kubernetes.io/tls` with `tls.crt`, `tls.key`, and optionally `ca.crt`.

### Verifying HA Status

After deploying an HA CR, verify the setup:

```bash
# Check pods are running
kubectl get pods -n <namespace> -l app.kubernetes.io/instance=<cr-name>

# Verify HA state in logs (look for LOAD-BALANCING or HOT-STANDBY)
kubectl logs <pod-name> -c dhcp4 | grep HA_STATE_TRANSITION

# Check per-pod IPs on the DHCP interface
kubectl exec <pod-name> -c dhcp4 -- ip addr show <interface>

# Test DHCP lease from a client pod
kubectl exec <client-pod> -- udhcpc -i net1 -n -q -t 5
```

Both pods should transition through `WAITING → SYNCING → READY → LOAD-BALANCING` (or `HOT-STANDBY`). Heartbeat messages in the logs confirm ongoing peer communication.
