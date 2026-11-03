# Stork Agent Integration

The operator supports an optional [ISC Stork](https://www.isc.org/stork/) agent sidecar for monitoring Kea DHCP servers. When enabled, a `stork-agent` container runs alongside the Kea daemon in the same pod, providing:

- **Prometheus metrics** for Kea (subnets, leases, packets) on a configurable port (default 9547)
- **Stork server integration** for centralized monitoring via the Stork web UI

## How It Works

The stork-agent sidecar:
1. Shares the PID namespace with the Kea container (`shareProcessNamespace: true`)
2. Mounts the Kea config volume read-only
3. Discovers Kea by scanning processes and reading their `-c` config file paths
4. Exposes gRPC (port 8080) for Stork server and HTTP (port 9547) for Prometheus

## Default Image

The operator ships with a default stork-agent image (`quay.io/mooyeg/stork-agent:v2.4.0`) built from the [ISC Stork source](https://gitlab.isc.org/isc-projects/stork). When `stork.enabled` is `true` and no `image` is specified, this default is used automatically.

To use a custom image instead, set the `image` field explicitly. You can build your own from the ISC Stork source using the provided `Dockerfile.stork-agent`:

```bash
docker build -f Dockerfile.stork-agent -t <your-registry>/stork-agent:latest .
docker push <your-registry>/stork-agent:latest
```

## Basic Usage (Prometheus Only)

The simplest setup enables the agent for Prometheus metrics without a Stork server:

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

  stork:
    enabled: true
    # image defaults to quay.io/mooyeg/stork-agent:v2.4.0 if omitted
```

Metrics are available at `http://<pod-ip>:9547/metrics`.

Create a Prometheus `ServiceMonitor` or `PodMonitor` to scrape them:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: kea-dhcp4-metrics
  namespace: kea-system
spec:
  selector:
    matchLabels:
      app.kubernetes.io/component: dhcp4
  podMetricsEndpoints:
    - port: stork-prom
      path: /metrics
```

## With Stork Server

To register the agent with a Stork server for centralized monitoring:

### 1. Create a Server Token Secret

Generate a server access token in the Stork server UI, then create a Secret:

```bash
kubectl create secret generic stork-server-token \
  -n kea-system \
  --from-literal=token='<your-server-token>'
```

### 2. Enable Stork with Server Registration

```yaml
spec:
  stork:
    enabled: true
    serverURL: "http://stork-server.stork-system.svc.cluster.local:8080"
    serverTokenSecretRef:
      name: stork-server-token
```

The agent registers non-interactively using the token, avoiding manual approval in the Stork UI.

With a token, the sidecar runs `stork-agent register --server-token` before starting the agent, because only that command authorizes the machine automatically. The agent image therefore needs `/bin/sh`; the default image has it.

### Using a KeaStorkServer instead

If the server is a `KeaStorkServer` in the same namespace, point at it and leave out `serverURL` and `serverTokenSecretRef`:

```yaml
spec:
  stork:
    enabled: true
    storkServerRef:
      name: stork
```

Stork generates its agent token itself, so the operator reads it from the running server, logging in with the generated `<name>-stork-admin` credentials. It keeps the token in `<name>-stork-server-token` and reports progress in the `ServerTokenSynced` condition on the `KeaStorkServer`. Agents that start before the token is available are rolled onto it once it syncs, and the token is re-read every 5 minutes, so regenerating it in the Stork UI reaches the agents.

If you change the admin password in the Stork UI, update `<name>-stork-admin` to match, or the sync stops and `ServerTokenSynced` turns `False`.

#### How agents appear in Stork

HA (StatefulSet) agents register under their stable per-pod DNS name, `<pod>.<cr>-dhcp4-hl.<namespace>.svc.cluster.local`, rather than the pod IP. A recreated pod takes over its existing machine in Stork, so the machine list doesn't grow with every restart. Non-HA (Deployment) agents have no stable per-pod name and register with the pod IP.

Agent pods carry the label `kea.openshift.io/stork-server: <name>`, and the `KeaStorkServer` reconciler uses that label to find them. It then keeps the server's machine list in step with them:

- **It refreshes machines that have no state.** With server-token registration, Stork's first state pull always fails with `GetState not implemented`, because the pull runs while `stork-agent register` still holds the agent port with a server that only answers Ping. Once the real agent is running, the operator asks Stork to pull the state again. This is the same as the Refresh button in the UI.
- **It removes machines whose pod is gone for good.** These are machines registered under an operator agent name whose StatefulSet ordinal was scaled away or whose CR was deleted. A pod that is only being recreated keeps its machine. Machines registered by IP or under any other name are never touched.

The operator rechecks every 30 seconds while an agent is still registering or has no state, and every 5 minutes after that. After upgrading from a version that registered agents by pod IP, delete the old IP-addressed machines in the Stork UI once.

## Configuration Reference

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable the stork-agent sidecar |
| `image` | `quay.io/mooyeg/stork-agent:v2.4.0` | Stork agent container image |
| `imagePullPolicy` | (unset) | Image pull policy |
| `resources` | (unset) | CPU/memory resource requirements |
| `serverURL` | (empty) | Stork server URL for registration |
| `serverTokenSecretRef` | (empty) | Secret with key `token` for non-interactive registration |
| `port` | `8080` | gRPC listener port (Stork server connects here) |
| `prometheusPort` | `9547` | Prometheus Kea metrics exporter port |
| `env` | (empty) | Extra environment variables (e.g., `STORK_LOG_LEVEL`) |

## Example with Custom Resources and Logging

```yaml
spec:
  stork:
    enabled: true
    image: quay.io/mooyeg/stork-agent:v2.4.0   # optional, this is the default
    imagePullPolicy: IfNotPresent
    resources:
      requests:
        cpu: 50m
        memory: 64Mi
      limits:
        cpu: 200m
        memory: 128Mi
    serverURL: "http://stork-server:8080"
    serverTokenSecretRef:
      name: stork-token
    env:
      - name: STORK_LOG_LEVEL
        value: DEBUG
```

## Works with HA

Stork is fully compatible with HA deployments. Each pod in the StatefulSet gets its own stork-agent sidecar:

```yaml
spec:
  replicas: 2
  high-availability:
    mode: load-balancing
    peers:
      - name: server1
        role: primary
      - name: server2
        role: secondary
  stork:
    enabled: true
```

Both pods appear as separate machines in the Stork server UI.

## Troubleshooting

### Agent can't discover Kea

The stork-agent discovers Kea by scanning `/proc` for running processes. Verify:
1. The pod has `shareProcessNamespace: true` (automatic when stork is enabled)
2. The Kea config is mounted and readable

```bash
kubectl logs <pod> -c stork-agent | grep -i "kea"
```

### Prometheus metrics not available

Check the agent is listening:

```bash
kubectl exec <pod> -c stork-agent -- curl -s http://localhost:9547/metrics | head -5
```

### Registration fails

Check the server URL is reachable and the token is correct:

```bash
kubectl logs <pod> -c stork-agent | grep -i "register"
```
