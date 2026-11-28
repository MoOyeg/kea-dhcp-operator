# Kea Control Agent: deprecation and migration

`KeaControlAgent` (and `KeaServer.spec.controlAgent`) is **deprecated**. ISC
deprecated the Kea Control Agent (`kea-ctrl-agent`) in Kea 3.0 and removed it
in Kea 3.2. Since Kea 3.0, each daemon (`kea-dhcp4`, `kea-dhcp6`,
`kea-dhcp-ddns`) can serve its own HTTP control socket, which replaces the
agent.

> **The operator-managed Control Agent cannot reach the daemons.** It runs in
> its own pod, and each daemon's UNIX control socket sits in a volume private
> to the daemon's pod (see [below](#what-the-operator-managed-control-agent-can-reach)).
> Setting `container.image` gets the agent deployed, but it does not make it
> work: commands it forwards to `kea-dhcp4`, `kea-dhcp6` or `kea-dhcp-ddns`
> fail. This was true before this release as well. Migrate to the daemons'
> own HTTP control sockets.

The operator still reconciles `KeaControlAgent` for existing users, but it
will drop the resource in a future release.

## Upgrade notes (this release)

- **No default image.** Before this release, the operator defaulted the
  Control Agent image to `docker.cloudsmith.io/isc/docker/kea-ctrl-agent:3.0.2`,
  but that image does not exist: ISC publishes no Control Agent image. A
  `KeaControlAgent` without `spec.container.image` therefore never started.
  Now the operator does not deploy such an object. It sets
  `ConfigurationValid=False` with reason `ImageRequired` and the phase to
  `Error`, emits one Warning event, and leaves any existing workload as it
  is. This applies to objects stored before the upgrade and to new ones.
- **Control sockets must be UNIX.** `control-sockets.{dhcp4,dhcp6,d2}` must
  use `socket-type: unix`. Kea's Control Agent only reaches daemons over UNIX
  sockets and rejects `socket-port`. An object with another socket type is
  not deployed either: `ConfigurationValid=False` with reason
  `UnsupportedControlSocket`, one Warning event.
- **Recovery.** Set `container.image` and use UNIX sockets, and the operator
  reconciles the object again. That restores reconciliation only; see the
  note above about what the deployed agent can reach.
- **Objects that already set `container.image`** with UNIX sockets are
  reconciled as before. That says nothing about whether they work; see the
  note above.
- **The API server does not reject these objects.** The checks live in the
  operator, not in the CRD. On Kubernetes before 1.33 (OpenShift 4.19 and
  earlier), a CRD rule is also applied to status updates, so the operator
  could not have reported `ImageRequired` on objects stored before the
  upgrade. The deprecation warning that `kubectl`/`oc` print says that
  `container.image` is required.
- **Deprecation is visible.** `kubectl`/`oc` print a deprecation warning for
  every request on `keacontrolagents`. Each `KeaControlAgent` carries a
  `Deprecated=True` condition, which stays after recovery. A `KeaServer` with
  `spec.controlAgent` carries the same condition and gets one
  `ControlAgentRemovedUpstream` Warning event when the condition appears.
- **Not offered in OperatorHub.** The Control Agent example is no longer in
  the OperatorHub examples, and the `KeaServer` example no longer includes
  `controlAgent`.

## What the operator-managed Control Agent can reach

The operator runs the Control Agent in its own Deployment. Each daemon's UNIX
control socket lives in `/var/run/kea`, an `emptyDir` private to the daemon's
pod. The Control Agent's pod cannot see it, so the agent cannot forward
commands to daemons in other pods. Kea's Control Agent accepts only UNIX
sockets, so there is no network path either.

Check whether anything actually depends on your Control Agent before you
spend effort keeping it.

## Migrating to daemon HTTP control sockets

> **On HA servers, only inside the pod, for now.** On a server with
> `high-availability` set, the operator uses `control-socket.socket-port` as
> the HA listener's port too. With Kea's default `socket-address`
> (`127.0.0.1`) the two coexist, so the daemon starts, but only processes in
> the pod (such as the Stork agent) can reach the control socket; the
> operator's Service on that port reaches the HA listener instead. With any
> other `socket-address`, such as `0.0.0.0`, the two bind the same port and
> `kea-dhcp4` fails to start. Migrating API clients that run outside the pod
> works on standalone servers only, until the operator gives the HA listener
> its own port.

Configure an HTTP control socket on the daemon itself:

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
spec:
  control-socket:
    socket-type: http
    socket-address: 127.0.0.1   # see "Authentication and TLS" below
    socket-port: 8001
```

`KeaDhcp6Server` and `KeaDhcpDdns` take the same `control-socket` field. The
repository's parser test `TestDaemonHTTPControlSocketsAcceptedByKea` checks
that `kea-dhcp4`, `kea-dhcp6` and `kea-dhcp-ddns` accept these configurations.

API clients change in two ways:
- **Endpoint:** send commands to the daemon's endpoint instead of the agent
  (`:8000` by default).
- **Request body:** drop the `"service": [...]` field. The daemon is
  addressed directly.

### Checklist before removing the Control Agent

| Area | Status in this operator |
|------|-------------------------|
| **HA peer traffic** | Doesn't use the Control Agent. The HA hook runs its own dedicated HTTP listener on each daemon, and peers connect to each other through the StatefulSet's headless Service. |
| **Stork** | Doesn't use the Control Agent. The `stork-agent` sidecar runs inside each daemon pod. The e2e Stork test passes with no Control Agent deployed. |
| **Probes** | The operator defines no probes on Kea daemon containers, so nothing to migrate. |
| **HTTP control socket on an HA server** | **Limited today.** The operator also uses `control-socket.socket-port` as the HA listener's port. On `127.0.0.1` (the default `socket-address`) the control socket coexists with the HA listener and serves clients in the pod only. On any other address the two collide and `kea-dhcp4` 3.0.2 fails to start (`CmdHttpListener::run failed: unable to setup TCP acceptor ... Address in use`); checked with two `kea-dhcp4` 3.0.2 peers. Clients outside the pod need the HA listener on its own port first. |
| **Service for the control port** | For `KeaDhcp4Server` and `KeaDhcp6Server` with an HTTP control socket, the operator creates a ClusterIP Service `<name>-dhcp4` / `<name>-dhcp6` on `socket-port`, reachable from every namespace unless a NetworkPolicy says otherwise. For `KeaDhcpDdns` it creates none; add your own if clients outside the pod need the endpoint. |
| **Authentication and TLS** | Kea 3.0 supports both on daemon HTTP control sockets, but the CRDs don't expose those fields yet. An HTTP control socket accepts every Kea command, including configuration changes, so until they do: bind to `127.0.0.1` (the Service then reaches nothing, and only processes in the pod, such as the Stork agent, can connect), or restrict the Service's port to your API clients with a NetworkPolicy. |
| **Your API clients** | Point them at the daemon endpoints (see above) and test every command they send. |

## Keeping the Control Agent for now

> **This keeps the object reconciling; it does not make the agent work.** An
> image built this way lets the operator deploy the Control Agent again, but
> the deployed agent still cannot reach the daemons' sockets (see
> [above](#what-the-operator-managed-control-agent-can-reach)). Use it only to
> keep an existing object out of the `Error` phase while you migrate.

To build an image on Kea 3.0, you have to build and maintain it yourself. It
won't exist for Kea 3.2 or later. ISC ships `kea-ctrl-agent` as an Alpine
package (`isc-kea-ctrl-agent`) in the same repository its images install
from. For example:

```Dockerfile
# Pin the base by digest; this example uses the operator's default kea-dhcp4 3.0.2.
FROM docker.cloudsmith.io/isc/docker/kea-dhcp4@sha256:384c936f23d064556d3a92e34d550a2e9b5cd3cf6e2e492a52b93ccb212d6508
USER 0
RUN set -eu; \
    version=$(apk list --installed isc-kea-common | cut -d' ' -f1 | sed 's/^isc-kea-common-//'); \
    series=$(echo "$version" | cut -d. -f1,2 | tr . -); \
    alpine=$(cut -d. -f1,2 /etc/alpine-release); \
    echo "https://dl.cloudsmith.io/public/isc/kea-${series}/alpine/v${alpine}/main" >> /etc/apk/repositories; \
    apk add --no-cache "isc-kea-ctrl-agent=${version}"
```

Then set `spec.container.image` to your image.

This is the recipe for the repository's parser-test fixture
(`internal/kea/testdata/kea-ctrl-agent-fixture`). The project does not
publish, scan or support an image built from it. The parser test only shows
that Kea accepts the operator's rendered configuration. It isn't evidence
that a deployed Control Agent works.
