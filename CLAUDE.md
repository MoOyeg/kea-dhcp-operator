# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build Commands

```bash
make build              # Build the operator binary (includes generate, fmt, vet)
make generate           # Generate DeepCopy methods from API types
make manifests          # Generate CRDs, RBAC, and webhook manifests
make test               # Run unit and integration tests (requires envtest)
make docker-build IMG=<tag>  # Build the container image
make lint               # Run golangci-lint
```

Run a single test:

```bash
go test ./internal/kea/... -run TestDhcp4MinimalConfig -v
```

## Architecture

Operator-SDK (Go) operator that manages ISC Kea DHCP servers on Kubernetes/OpenShift. The operator renders CRD specs into Kea JSON configuration, stores them in ConfigMaps, and mounts them into pods at `/etc/kea/`.

### CRD Hierarchy

API group: `kea.openshift.io`, version: `v1alpha1`

- **KeaDhcp4Server** (`kd4`) -- DHCPv4 server
- **KeaDhcp6Server** (`kd6`) -- DHCPv6 server
- **KeaControlAgent** (`kca`) -- REST API management agent
- **KeaDhcpDdns** (`kdd`) -- Dynamic DNS update service
- **KeaServer** (`ks`) -- Umbrella resource that creates any combination of the above four

### Key Packages

| Package | Purpose |
|---------|---------|
| `api/v1alpha1` | CRD type definitions and shared types |
| `internal/kea` | Config rendering: CRD spec to Kea JSON |
| `internal/resources` | Kubernetes resource builders (Deployment, ConfigMap, Service) |
| `internal/controller` | Reconciliation loops for each CRD |
| `internal/platform` | OpenShift vs. plain Kubernetes detection |

### Config Flow

`CRD Spec` --> Config Renderer (`internal/kea`) --> JSON --> `ConfigMap` --> Volume mount at `/etc/kea/`

A SHA-256 config hash annotation on the pod template triggers rolling restarts when configuration changes.
