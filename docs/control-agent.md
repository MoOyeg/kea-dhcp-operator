# Kea Control Agent: deprecation and migration

`KeaControlAgent` (and `KeaServer.spec.controlAgent`) is **deprecated**. ISC
deprecated the Kea Control Agent (`kea-ctrl-agent`) in Kea 3.0 and removed it
in Kea 3.2. Since Kea 3.0, each daemon (`kea-dhcp4`, `kea-dhcp6`,
`kea-dhcp-ddns`) can serve its own HTTP control socket, which replaces the
agent.

The operator still reconciles `KeaControlAgent` for existing users, but it
will drop the resource in a future release. Plan your migration now.

## Upgrade notes (this release)

- **No default image.** Before this release, the operator defaulted the
  Control Agent image to `docker.cloudsmith.io/isc/docker/kea-ctrl-agent:3.0.2`,
  but that image does not exist: ISC publishes no Control Agent image. A
  `KeaControlAgent` without `spec.container.image` therefore never started.
  - **New objects:** the API server rejects a `KeaControlAgent`, or a
    `KeaServer` with `spec.controlAgent`, that has no `container.image`. The
    error says why.
  - **Objects stored before the upgrade:** the operator sets
    `ConfigurationValid=False` with reason `ImageRequired`, sets the phase to
    `Error`, and emits a Warning event. It leaves the existing workload
    alone. Set an image (see [Keeping the Control Agent](#keeping-the-control-agent-for-now))
    or delete the object.
  - **Updating such an object** works on Kubernetes 1.30 and later, where
    CRD validation ratcheting is on by default: unchanged invalid fields are
    not re-checked. On older clusters, an update is rejected until the image
    is set. Deletion always works.
- **Objects that already set `container.image`** keep working unchanged.
- **Control sockets must be UNIX.** `control-sockets.{dhcp4,dhcp6,d2}` must
  use `socket-type: unix`. Kea's Control Agent only reaches daemons over UNIX
  sockets and rejects `socket-port`. The API server now rejects other socket
  types; existing objects get `ConfigurationValid=False` with reason
  `UnsupportedControlSocket`.
- **Deprecation is visible.** `kubectl`/`oc` print a deprecation warning for
  every request on `keacontrolagents`. Each `KeaControlAgent` carries a
  `Deprecated=True` condition, and a `KeaServer` with `spec.controlAgent`
  gets a `ControlAgentRemovedUpstream` Warning event.
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
| **HTTP control socket on an HA server** | **Blocked today.** The operator also uses `control-socket.socket-port` as the HA listener port, so an HTTP control socket and the HA listener would bind the same port. `kea-dhcp4` 3.0.2 then fails to start with `CmdHttpListener::run failed: unable to setup TCP acceptor`. Use HTTP control sockets on standalone servers only until the operator gives the HA listener its own port. |
| **Service for the control port** | The operator does not create one. Add your own Service if clients outside the pod need the endpoint. |
| **Authentication and TLS** | Kea 3.0 supports both on daemon HTTP control sockets, but the CRDs don't expose those fields yet. Until they do, bind to `127.0.0.1`, or restrict access with a NetworkPolicy. An HTTP control socket accepts every Kea command, including configuration changes. |
| **Your API clients** | Point them at the daemon endpoints (see above) and test every command they send. |

## Keeping the Control Agent for now

To keep the Control Agent on Kea 3.0 until you migrate, you have to build and
maintain the image yourself. It won't exist for Kea 3.2 or later. ISC ships
`kea-ctrl-agent` as an Alpine package (`isc-kea-ctrl-agent`) in the same
repository its images install from. For example:

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
