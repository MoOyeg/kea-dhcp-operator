# Quick Start

Get a Kea DHCP server running on your cluster in minutes.

## Prerequisites

- OpenShift 4.14+ or Kubernetes 1.28+
- `oc` or `kubectl` CLI with cluster-admin access
- `operator-sdk` CLI

## 1. Install the Operator

```bash
oc new-project kea-dhcp-operator || oc project kea-dhcp-operator
operator-sdk run bundle quay.io/mooyeg/ocp-kea-dhcp-bundle:v0.0.14 \
  -n kea-dhcp-operator --timeout 5m
```

Verify:

```bash
oc get pods -n kea-dhcp-operator -l control-plane=controller-manager
```

The operator auto-creates the `kea-system` namespace and (on OpenShift) the `kea-dhcp` SCC.

## 2. Deploy a DHCPv4 Server

Create `dhcp4.yaml`:

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4
  namespace: kea-system
spec:
  interfaces-config:
    interfaces: ["net1"]
    dhcp-socket-type: raw

  placement:
    podAnnotations:
      k8s.v1.cni.cncf.io/networks: "dhcp-net"   # Your NAD name

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

  lease-database:
    type: memfile
    persist: true

  valid-lifetime: 43200

  loggers:
    - name: kea-dhcp4
      severity: INFO
      output-options:
        - output: stdout
```

```bash
oc apply -f dhcp4.yaml
oc get pods -n kea-system -w
```

## 3. Add High Availability

For an HA pair, set `replicas: 2`, add a control socket, and configure HA peers:

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4-ha
  namespace: kea-system
spec:
  replicas: 2

  interfaces-config:
    interfaces: ["net1"]
    dhcp-socket-type: raw

  placement:
    podAnnotations:
      k8s.v1.cni.cncf.io/networks: "dhcp-net"   # Your NAD name

  subnet4:
    - id: 1
      subnet: "192.168.1.0/24"
      pools:
        - pool: "192.168.1.100 - 192.168.1.200"
      option-data:
        - name: routers
          data: "192.168.1.1"

  lease-database:
    type: memfile
    persist: true

  control-socket:
    socket-type: http
    socket-port: 8000

  high-availability:
    this-server-name: server1    # Auto-set per pod by the operator
    mode: load-balancing
    heartbeat-delay: 10000
    max-response-delay: 60000
    max-unacked-clients: 10
    peers:
      - name: server1
        role: primary
        auto-failover: true
      - name: server2
        role: secondary
        auto-failover: true
    # Peer URLs are auto-generated from StatefulSet DNS

  valid-lifetime: 43200

  loggers:
    - name: kea-dhcp4
      severity: INFO
      output-options:
        - output: stdout
```

Verify HA is working:

```bash
oc get pods -n kea-system -l app.kubernetes.io/instance=dhcp4-ha
oc logs -n kea-system dhcp4-ha-dhcp4-0 -c dhcp4 | grep HA_STATE_TRANSITION
```

Both pods should reach `LOAD-BALANCING` state.

## 4. Enable Monitoring (Stork)

Add the stork sidecar for Prometheus metrics:

```yaml
spec:
  stork:
    enabled: true
```

This exposes Kea metrics on port 9547 and auto-creates a `PodMonitor`. To also register with a Stork server:

```yaml
spec:
  stork:
    enabled: true
    serverURL: "http://stork-server:8080"
    serverTokenSecretRef:
      name: stork-server-token
```

See [Stork Integration](stork.md) for details.

## 5. Use MySQL for Lease Storage

For durable leases across pod restarts, use a MySQL-compatible backend:

1. Create a credentials Secret:

   ```bash
   oc create secret generic kea-db-credentials \
     -n kea-system \
     --from-literal=username=kea \
     --from-literal=password='<password>'
   ```

2. Set the lease database:

   ```yaml
   spec:
     lease-database:
       type: mysql
       host: "kea-db-haproxy.pxc.svc.cluster.local"
       port: 3306
       name: kea
       credentialsSecretRef:
         name: kea-db-credentials
       connect-timeout: 10
       max-reconnect-tries: 5
       on-fail: serve-retry-continue
       retry-on-startup: true
   ```

See [HA Guide — Appendix A](ha.md#appendix-a-ha-with-mysql-backend-percona-xtradb) for the full Percona setup.

## What's Next

- [Architecture](architecture.md) — how the operator works
- [CRD Reference](crds.md) — all fields and options
- [HA Guide](ha.md) — detailed HA deployment walkthrough
- [Stork Integration](stork.md) — monitoring and Prometheus metrics
