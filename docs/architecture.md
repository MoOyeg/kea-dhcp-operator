# Architecture

## Overview

The Kea DHCP Operator is a Kubernetes/OpenShift operator built with Operator SDK (Go) that manages [ISC Kea DHCP](https://www.isc.org/kea/) server deployments. It converts declarative Kubernetes Custom Resources into Kea JSON configuration files, deploys them as ConfigMaps, and orchestrates Kea daemon pods that consume these configurations.

**API Group:** `kea.openshift.io`
**API Version:** `v1alpha1`

## Custom Resources

| CRD | Short Name | Description |
|-----|------------|-------------|
| `KeaDhcp4Server` | `kd4` | DHCPv4 server |
| `KeaDhcp6Server` | `kd6` | DHCPv6 server |
| `KeaControlAgent` | `kca` | REST API management agent |
| `KeaDhcpDdns` | `kdd` | Dynamic DNS update service |
| `KeaServer` | `ks` | Umbrella — creates child CRDs for any combination of the above |

See [CRD Reference](crds.md) for all fields and options.

## Component Architecture

```
                    ┌──────────────────────────────────┐
                    │          KeaServer (ks)           │
                    │     Umbrella / Convenience CRD    │
                    └──┬───────┬────────┬──────────┬───┘
                       │       │        │          │
              Creates  │       │        │          │  Creates
              child    │       │        │          │  child
              CRDs     │       │        │          │  CRDs
                       ▼       ▼        ▼          ▼
               ┌───────┐ ┌────────┐ ┌──────┐ ┌─────────┐
               │KeaDhcp│ │KeaDhcp │ │KeaCon│ │KeaDhcp  │
               │4Server│ │6Server │ │trolAg│ │Ddns     │
               │ (kd4) │ │ (kd6)  │ │(kca) │ │ (kdd)   │
               └───┬───┘ └───┬────┘ └──┬───┘ └────┬────┘
                   │         │         │           │
                   ▼         ▼         ▼           ▼
              ┌──────────────────────────────────────────┐
              │       Controller Reconciliation          │
              │                                          │
              │  CRD Spec                                │
              │    → Config Renderer (internal/kea)      │
              │      → Kea JSON                          │
              │        → ConfigMap                       │
              │          → Deployment + Service + SA     │
              │            → Status Update               │
              └──────────────────────────────────────────┘
```

## Data Flow: CRD to Running Pod

```
1. User creates/updates CRD          ┌─────────────┐
                                      │  API Server  │
2. Controller receives event          └──────┬───────┘
                                             │
3. Config Renderer builds JSON     ┌─────────▼──────────┐
   from CRD spec fields            │  Config Renderer    │
   (internal/kea/)                 │  Spec → JSON map    │
                                   │  → json.Marshal     │
                                   └─────────┬──────────┘
                                             │
4. SHA-256 hash computed           ┌─────────▼──────────┐
   (first 16 hex chars)           │  ComputeHash()      │
                                   └─────────┬──────────┘
                                             │
5. Resources reconciled            ┌─────────▼──────────┐
                                   │  ConfigMap          │
                                   │   key: kea-dhcp4.conf
                                   │   value: {JSON}     │
                                   │                     │
                                   │  Deployment         │
                                   │   volumes:          │
                                   │    /etc/kea (config)│
                                   │    /run/kea (socks) │
                                   │    /var/lib/kea     │
                                   │    /etc/kea/tls     │
                                   │   annotation:       │
                                   │    config-hash=abc..│
                                   │                     │
                                   │  Service (optional) │
                                   │  ServiceAccount     │
                                   └─────────┬──────────┘
                                             │
6. Pod starts kea daemon           ┌─────────▼──────────┐
   kea-dhcp4 -c /etc/kea/         │  Kea Daemon Pod     │
     kea-dhcp4.conf                │  Reads config from  │
                                   │  mounted ConfigMap  │
                                   └────────────────────┘
```

## Package Structure

| Package | Purpose |
|---------|---------|
| `api/v1alpha1/` | CRD type definitions — the operator's API surface |
| `internal/kea/` | Configuration rendering — converts Go structs to Kea JSON |
| `internal/resources/` | Kubernetes resource builders — Deployments, ConfigMaps, Services |
| `internal/controller/` | Reconciliation controllers — one per CRD kind |
| `internal/platform/` | Platform detection — identifies OpenShift vs. vanilla Kubernetes |
| `cmd/main.go` | Operator entrypoint — manager setup and controller registration |

## Key Design Decisions

### Config Hash Rolling Restarts

When a CRD spec changes, the rendered JSON changes, producing a new SHA-256 hash. This hash is stored as a pod template annotation (`kea.openshift.io/config-hash`). Kubernetes treats annotation changes as pod template changes, automatically triggering a rolling restart — even though the ConfigMap volume itself would eventually propagate.

### map[string]interface{} for JSON Rendering

Config renderers use Go maps rather than dedicated JSON struct types. This avoids maintaining two parallel type hierarchies (CRD types and JSON types) and simplifies omitting nil/unset fields — which is important because Kea uses its own defaults for absent fields.

### Umbrella CRD Pattern

The KeaServer CRD does not directly create Kubernetes resources. Instead, it creates child CRDs (KeaDhcp4Server, etc.) with owner references. Each component controller independently reconciles its own CRD. This separation means:

- Components can be deployed individually or as a group
- Each component's status is independently tracked
- Deletion of the umbrella cascades to all children via garbage collection

### Namespace Layout

The operator uses two namespaces:

- **`kea-dhcp-operator`** — where the operator controller manager runs (installed via OLM or `make deploy`)
- **`kea-system`** — the default namespace for DHCP server workloads (auto-created by the operator on startup)

On OpenShift, the operator also creates the `kea-dhcp` SecurityContextConstraints and auto-binds it to service accounts for any DHCP server deployed in `kea-system`. Users deploying to custom namespaces must manually apply the SCC.

### Security Context

Kea DHCP requires raw socket access for L2 DHCP operations. Pods are configured with:
- All capabilities dropped
- `NET_RAW` and `NET_BIND_SERVICE` added back
- Optional `hostNetwork: true` for environments where pod networking doesn't provide direct L2 access

DHCP *client* pods attached to a NAD do not need extra capabilities — they use standard UDP broadcast sockets.

### Secret Management

All sensitive values **must** be stored in Kubernetes Secrets and referenced from the CR. Inline plaintext secret fields are not supported.

| Component | Secret Field | CR Reference | Secret Keys |
|-----------|-------------|--------------|-------------|
| Database (lease/hosts) | password, username | `credentialsSecretRef` | `username`, `password` |
| Control Agent auth | client password | `passwordSecretKeyRef` | user-defined key |
| DHCP-DDNS TSIG | TSIG secret | `secretRef` | user-defined key |
| TLS certificates | cert, key, CA | `tls.secretRef` | `tls.crt`, `tls.key`, `ca.crt` |

Example — creating a database credentials Secret:

```bash
kubectl create secret generic kea-db-credentials \
  --from-literal=username=kea \
  --from-literal=password='your-secure-password'
```

Then reference it in the CR:

```yaml
lease-database:
  type: mysql
  host: "db.example.com"
  name: kea
  credentialsSecretRef:
    name: kea-db-credentials
```

TLS secrets are mounted as volumes at `/etc/kea/tls/`, and the rendered JSON references file paths within the pod. Database credentials are resolved by the controller and injected into the rendered JSON.

### Database Hook Auto-Injection

When `lease-database.type` is set to `mysql` or `postgresql`, the operator automatically injects the corresponding hook library (`libdhcp_mysql.so` or `libdhcp_pgsql.so`) into the rendered configuration. Kea 3.0+ requires these hook libraries to be loaded explicitly for database connectivity.

### High Availability

When the `high-availability` field is set on a DHCP server CR, the operator automatically:

1. Switches from a Deployment to a **StatefulSet** with per-ordinal ConfigMaps
2. Injects `libdhcp_lease_cmds.so` and `libdhcp_ha.so` hook libraries
3. Creates a **headless Service** for stable per-pod DNS names
4. Runs an **init container** that selects config by pod ordinal and resolves DNS to IPs
5. Injects **pod anti-affinity** to spread HA peers across nodes
6. Supports **static peer addresses** on NAD interfaces via the `address` field

See [HA Guide](ha.md) for a full deployment walkthrough.

### Stork Agent Sidecar

When `stork.enabled` is `true`, the operator adds a stork-agent sidecar container that:

- Shares the PID namespace to discover Kea processes
- Exposes Prometheus metrics on port 9547
- Optionally registers with a Stork server for centralized monitoring
- Auto-creates a `PodMonitor` resource when the `monitoring.coreos.com` CRD is available

See [Stork Integration](stork.md) for configuration details.

### Per-Component Container Images

The operator uses separate Kea container images for each daemon type rather than a single combined image:

| Component | Default Image |
|-----------|--------------|
| DHCPv4 | `docker.cloudsmith.io/isc/docker/kea-dhcp4:3.0.2` |
| DHCPv6 | `docker.cloudsmith.io/isc/docker/kea-dhcp6:3.0.2` |
| Control Agent | `docker.cloudsmith.io/isc/docker/kea-ctrl-agent:3.0.2` |
| DHCP-DDNS | `docker.cloudsmith.io/isc/docker/kea-dhcp-ddns:3.0.2` |

Each image can be overridden via the `container.image` field in the CR spec.

### Platform Detection

The operator detects OpenShift vs. vanilla Kubernetes at startup using `sync.Once` to cache the result. OpenShift-specific features (SCC creation, SCC binding, console plugin) are only activated on OpenShift clusters.
