# ocp-kea-dhcp

A Kubernetes/OpenShift operator for managing [ISC Kea DHCP](https://www.isc.org/kea/) server deployments. It converts declarative Custom Resources into Kea JSON configuration, deploys them as ConfigMaps, and orchestrates Kea daemon pods.

## Features

- **DHCPv4** and **DHCPv6** servers with full subnet, pool, reservation, and option support
- **High Availability** with automatic StatefulSet orchestration and hook injection
- **Control Agent** and **DHCP-DDNS** for REST management and dynamic DNS
- **Umbrella resource** (`KeaServer`) to deploy any combination of components in a single CR
- **Network Attachment Definitions (NADs)** for multi-network DHCP on secondary interfaces
- **MySQL/PostgreSQL** lease database backends with secret-based credential management
- **Stork agent** sidecar for Prometheus metrics and centralized monitoring

## Quick Start

### Prerequisites

- OpenShift 4.14+ or Kubernetes 1.28+
- `oc` or `kubectl` CLI with cluster-admin access
- `operator-sdk` CLI

### 1. Install the Operator

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

### 2. Deploy a DHCPv4 Server

```yaml
apiVersion: kea.openshift.io/v1alpha1
kind: KeaDhcp4Server
metadata:
  name: dhcp4
  namespace: kea-system
spec:
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

### 3. Uninstall

```bash
# Delete CRs
kubectl delete keadhcp4servers --all -n kea-system

# Remove the operator
operator-sdk cleanup ocp-kea-dhcp -n kea-dhcp-operator
```

## Documentation

For detailed guides and reference material, see the [docs/](docs/) folder:

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | Design, data flow, package structure, and key decisions |
| [CRD Reference](docs/crds.md) | All Custom Resource fields and options |
| [Quick Start](docs/quickstart.md) | Extended quick start with HA, NAD, MySQL, and Stork examples |
| [HA Guide](docs/ha.md) | Step-by-step High Availability deployment walkthrough |
| [Stork Integration](docs/stork.md) | Prometheus metrics and Stork server monitoring |

## Development

```bash
make build          # Build the operator binary
make test           # Run unit tests (requires envtest)
make lint           # Run golangci-lint
make manifests      # Regenerate CRDs and RBAC from code markers
make generate       # Regenerate DeepCopy methods
make run            # Run the controller locally against the cluster
```

Run a single test:

```bash
go test ./internal/kea/... -run TestDhcp4MinimalConfig -v
```

## Contributing

Contributions are welcome. Run `make help` for a full list of available targets.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This operator deploys [ISC Kea DHCP](https://www.isc.org/kea/), which is licensed under the [Mozilla Public License 2.0](https://www.mozilla.org/en-US/MPL/2.0/). The operator itself does not contain Kea source code — it generates configuration files and orchestrates unmodified Kea container images.
