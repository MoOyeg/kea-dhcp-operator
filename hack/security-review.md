# Security Review: ocp-kea-dhcp Operator

**Date:** 2026-03-07
**Last Updated:** 2026-03-09
**Scope:** Full codebase security review of the OCP Kea DHCP operator

---

## Remediation Summary

| Finding | Status | Fixed In |
|---------|--------|----------|
| 1. Shell injection via NADInterface/NADSubnet | **FIXED** | `internal/resources/validate.go` |
| 2. Plaintext secrets in CRD specs | **FIXED** | Plaintext fields removed; all secrets use `SecretKeySelector`/`LocalObjectReference` |
| 3. Plaintext secrets rendered into ConfigMaps | **FIXED** | Credentials injected via `ValueFrom.SecretKeyRef` env vars; only placeholders in configs |
| 4. Shell injection via HA peer hostnames in `sed` | **FIXED** | `internal/resources/validate.go` |
| 5. Hook library path traversal | **PARTIALLY FIXED** | Auto-injected hooks use hardcoded paths; user-supplied hooks still unvalidated |
| 6. Stork token as plain env var `Value` | **FIXED** | `internal/controller/reconciler.go` (dbCredentialEnvVars) |
| 8. `CreateOrUpdate` doesn't reconcile desired spec | **FIXED** | `internal/controller/reconciler.go` (buildMutateFn) |
| 10. Secret resolution errors silently swallowed | **ACCEPTED** | Token is optional; Stork runs in Prometheus-only mode on failure |
| All others | **OPEN** | See individual findings below |

---

## CRITICAL / HIGH Findings

### 1. ~~Shell Injection via NADInterface/NADSubnet in `sh -c`~~ FIXED
**Severity: HIGH** | `internal/resources/deployment.go`, `internal/resources/statefulset.go`

~~User-controlled CRD fields (`NADInterface`, `NADSubnet`) are interpolated directly into shell commands via `fmt.Sprintf` into `sh -c`.~~

**Fix**: `ValidateNADShellInputs()` in `internal/resources/validate.go` now validates all shell-interpolated fields before use:
- Interface names: `[a-zA-Z0-9._-]+`
- Subnet: IPv4 CIDR format `[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/[0-9]{1,2}`
- Command: `[a-zA-Z0-9._/-]+`

Validation is called in both `deployment.go` and `statefulset.go` before building the `sh -c` command.

### 2. ~~Plaintext Secrets in CRD Specs~~ FIXED
**Severity: HIGH** | `api/v1alpha1/common_types.go`, `api/v1alpha1/keacontrolagent_types.go`, `api/v1alpha1/keadhcpddns_types.go`

~~Multiple fields accepted plaintext secrets directly in the CRD spec.~~

**Fix**: All plaintext secret fields have been removed from the API types. Secrets are now exclusively referenced via Kubernetes Secret objects:

- `DatabaseConfig.CredentialsSecretRef` (`*corev1.LocalObjectReference`) — references a Secret with `username` and `password` keys
- `AuthClient.PasswordSecretKeyRef` (`*corev1.SecretKeySelector`) — references a specific Secret key for the auth password
- `TSIGKey.SecretRef` (`*corev1.SecretKeySelector`) — references a specific Secret key for the TSIG secret

No plaintext `Password string` or `Secret string` fields exist in the CRD types. CRs never contain secret material.

### 3. ~~Plaintext Secrets Rendered into ConfigMaps~~ FIXED
**Severity: HIGH** | `internal/kea/config.go`, `internal/kea/ctrl_agent_config.go`, `internal/kea/ddns_config.go`

~~When inline passwords were used, they were rendered into the Kea JSON config stored in a ConfigMap.~~

**Fix**: Credentials are never rendered into ConfigMaps. The operator uses environment variable placeholders in the Kea JSON config and injects actual secrets via `ValueFrom.SecretKeyRef` on the container spec:

- **Database credentials**: `dbCredentialEnvVars()` in `reconciler.go` creates `SecretKeyRef` env vars (e.g., `KEA_LEASE_DB_USER`, `KEA_LEASE_DB_PASSWORD`). The config renderer receives placeholder strings like `$KEA_LEASE_DB_USER` which Kea expands at runtime.
- **Auth client passwords**: `secretKeyRefEnvVar()` creates per-client `SecretKeyRef` env vars. Placeholders stored in `ResolvedAuthPasswords` map.
- **TSIG secrets**: `secretKeyRefEnvVar()` creates per-key `SecretKeyRef` env vars. Placeholders stored in `ResolvedTSIGSecrets` map.

ConfigMaps contain only placeholder references (e.g., `"password": "$KEA_LEASE_DB_PASSWORD"`), never actual secret values. Kubernetes expands the environment variables at container startup.

### 4. ~~Shell Injection in HA Init Container `sed` Commands~~ FIXED
**Severity: HIGH** | `internal/resources/statefulset.go`

~~The HA init container interpolates peer hostnames directly into `sed` commands.~~

**Fix**: `ValidateHostname()` in `internal/resources/validate.go` now restricts hostnames to `[a-zA-Z0-9._-]+`, preventing sed metacharacter injection. Validation is called in the controller before building StatefulSet parameters.

### 5. Hook Library Path Traversal — PARTIALLY FIXED
**Severity: MEDIUM-HIGH** | `internal/kea/config.go`

Auto-injected hooks (HA, lease_cmds, MySQL, PostgreSQL) now use hardcoded paths via constants in `internal/kea/ha_config.go`:
```go
const (
    hookLibPath      = "/usr/lib/kea/hooks/"
    haHookLib        = hookLibPath + "libdhcp_ha.so"
    leaseCmdsHookLib = hookLibPath + "libdhcp_lease_cmds.so"
    mysqlHookLib     = hookLibPath + "libdhcp_mysql.so"
    pgsqlHookLib     = hookLibPath + "libdhcp_pgsql.so"
)
```

However, the `renderHooksLibraries()` function in `config.go` does **not** validate user-supplied `HookLibrary.Library` paths from the CRD spec. A user could still specify path traversal values like `../../tmp/malicious.so`.

**Recommendation**: Add a kubebuilder validation pattern restricting user-supplied hook paths to `/usr/lib/kea/hooks/libdhcp_*.so`.

---

## MEDIUM Findings

### 6. ~~Stork Agent Token Exposed as Environment Variable~~ FIXED
**Severity: MEDIUM** | `internal/resources/stork.go`, `internal/controller/reconciler.go`

~~The Stork server token was set as a plain `Value` in an environment variable.~~

**Fix**: Database credentials and Stork tokens are now injected via `ValueFrom.SecretKeyRef` using the `dbCredentialEnvVars()` function in `internal/controller/reconciler.go`. The token is sourced from a Kubernetes Secret reference (`ServerTokenSecretRef`), never embedded in the pod spec as a literal value.

### 7. No Admission Webhooks for CRD Validation — OPEN
**Severity: MEDIUM**

The webhook server is configured in `cmd/main.go` but **no validating or mutating webhooks are registered** for any CRD. This means:
- No server-side validation of subnet CIDR formats, IP addresses, interface names
- No enforcement of mutual exclusivity (e.g., inline password vs. secretRef)
- No rejection of obviously dangerous input values
- Relies entirely on kubebuilder markers and runtime validation in `validate.go`

**Recommendation**: Implement validating webhooks for at least `KeaDhcp4Server` and `KeaDhcp6Server` to validate subnets, interfaces, and reject inline secrets.

### 8. ~~`CreateOrUpdate` Mutate Function Does Not Set Desired Spec~~ FIXED
**Severity: MEDIUM** | `internal/controller/reconciler.go`

~~The `reconcileResource` mutate function only set the controller reference, not the desired spec.~~

**Fix**: `buildMutateFn()` in `internal/controller/reconciler.go` now captures desired state before `CreateOrUpdate` and re-applies it in the mutate function. Handles all resource types:
- **ConfigMap**: Data and labels
- **Deployment**: Full spec deep copy
- **StatefulSet**: Full spec deep copy
- **Service**: Spec deep copy with ClusterIP preservation
- **ServiceAccount**: Labels
- **RoleBinding**: Subjects and RoleRef
- **PodMonitor**: Full spec deep copy

### 9. SCC Allows `allowPrivilegeEscalation: true` and `runAsUser: RunAsAny` — OPEN
**Severity: MEDIUM** | `config/scc/kea-dhcp-scc.yaml`

The custom SCC:
- Sets `allowPrivilegeEscalation: true` — allows processes to gain more privileges than their parent
- Sets `runAsUser: RunAsAny` — allows running as root (UID 0)
- Grants `NET_ADMIN` — powerful capability that allows ARP spoofing, traffic sniffing, and network reconfiguration
- Sets `readOnlyRootFilesystem: false`

While some of these are needed for DHCP, the combination is overly permissive.

**Recommendation**: Set `allowPrivilegeEscalation: false`, use `MustRunAsRange` for `runAsUser` with a non-root range, set `readOnlyRootFilesystem: true`.

### 10. ~~Silently Swallowed Errors on Secret Resolution~~ ACCEPTED
**Severity: MEDIUM** | `internal/controller/keadhcp4server_controller.go`, `internal/controller/keadhcp6server_controller.go`

```go
serverToken, _ = resolveSecretValue(ctx, r.Client, server.Namespace, ...)
```

Errors from `resolveSecretValue` are intentionally ignored. The Stork server token is optional — when resolution fails, the Stork agent runs in Prometheus-only mode without server registration. This is acceptable degraded behavior: reconciliation continues and metrics are still exported.

**Accepted risk**: An attacker who deletes the secret would cause unauthenticated Stork operation, but Stork server registration is an optional monitoring feature, not a security boundary.

### 11. Init Container Uses Unpinned `busybox:latest` — OPEN
**Severity: MEDIUM** | `internal/resources/statefulset.go:36`

```go
InitContainerImage = "docker.io/busybox:latest"
```

The `:latest` tag is mutable — a supply chain attack could replace this image. Pin to a specific digest.

**Recommendation**: Pin to `docker.io/busybox:1.36.1` or a specific digest.

### 12. `ClientClass.Test` — Expression Injection — OPEN
**Severity: MEDIUM** | `api/v1alpha1/common_types.go`

This field accepts arbitrary Kea Expression Language strings with no validation. While Kea's EL is not a general-purpose language, unvalidated expressions could cause unexpected classification behavior or denial of service through complex/malicious expressions.

### 13. No NetworkPolicies Deployed — OPEN
**Severity: MEDIUM** | `internal/resources/service.go`

The operator creates Services but deploys no NetworkPolicies. The Kea control socket (port 8000), Stork metrics (port 9547), and DHCP services are accessible from any pod in the cluster.

---

## LOW Findings

### 14. `hostNetwork: true` Exposed via CRD — OPEN
**Severity: LOW** | `api/v1alpha1/keadhcp4server_types.go`

Users can enable `hostNetwork: true` via the CRD spec. When combined with `NET_RAW` + `NET_ADMIN`, this grants the pod full access to the host's network stack, enabling sniffing, ARP spoofing, and port binding on the host.

### 15. Invalid Hook Parameters Silently Dropped — OPEN
**Severity: LOW** | `internal/kea/config.go`

```go
if err := json.Unmarshal(h.Parameters.Raw, &params); err == nil {
    hm["parameters"] = params
}
```

Invalid JSON in hook parameters is silently dropped — no error logged, no status condition set.

### 16. `T1Percent` / `T2Percent` are `*string` instead of numeric — OPEN
**Severity: LOW** | `api/v1alpha1/keadhcp4server_types.go`

These percentage fields are typed as `*string` rather than a constrained numeric type. No range enforcement (should be 0.0-1.0).

### 17. No `MaxLength` on any string fields — OPEN
**Severity: LOW**

No string fields in the API types have length limits. A malicious user could submit extremely large strings leading to oversized ConfigMaps or memory pressure.

### 18. Init Container Missing Resource Limits — OPEN
**Severity: LOW** | `internal/resources/statefulset.go`

The `config-selector` init container has no `Resources` set — it can consume unlimited CPU/memory.

### 19. Operator Builder Image Not Pinned by Digest — OPEN
**Severity: LOW** | `Dockerfile:2`

The `golang:1.24` builder image is not pinned to a digest, which could allow supply chain attacks via tag mutability.

---

## Operational Finding

### 20. HA Failover Delay in Load-Balancing Mode — RESOLVED
**Severity: MEDIUM** | CRD `spec.high-availability` defaults

With the default `max-response-delay: 60000` (60s) and `max-unacked-clients: 5`, the HA failover in load-balancing mode is too slow for Kubernetes environments where StatefulSet pods recover in ~15-20s. During the `COMMUNICATION-RECOVERY` state, the surviving server only serves its own scope (hash-based subset of clients), not all clients. The server must reach `PARTNER-DOWN` to serve all clients.

**Resolution**: The CR was patched with `max-response-delay: 10000` and `max-unacked-clients: 0` for faster PARTNER-DOWN transitions. Consider updating the skill template and documentation defaults.

**Tested**: With the updated values, pod-1 entered PARTNER-DOWN in ~3s after pod-0 failure and successfully served all clients. Full recovery cycle (LOAD-BALANCING -> PARTNER-DOWN -> LOAD-BALANCING) verified.

---

## Positive Findings

- **Operator Dockerfile** uses distroless base, non-root (UID 65532), CGO disabled — good practices
- **RBAC is well-scoped** — no wildcards, secrets read-only (`get`, `list`, `watch`)
- **Proper owner references** — controllers set controller references for garbage collection
- **Capabilities dropped by default** — `Drop: ALL` with only necessary capabilities re-added
- **Config/TLS volumes mounted read-only**
- **Enum validation present** for `DatabaseType`, `DHCPSocketType`, `ControlSocket.SocketType`, `Severity`, `HAPeer.Role`, `HAConfig.Mode`, and reservation modes
- **Numeric range validation** on debug level (0-99), ports (1-65535), prefix lengths (1-128)
- **MinItems validation** on interfaces and HA peers
- **No hardcoded credentials** in Go source code
- **Console plugin** has no XSS vectors (no `dangerouslySetInnerHTML`, `eval`, or `innerHTML`)
- **Runtime input validation** via `internal/resources/validate.go` for shell-interpolated values
- **Database credentials via SecretKeyRef** — `credentialsSecretRef` path correctly uses `ValueFrom.SecretKeyRef` env vars
- **PodMonitor implementation is secure** — no user input embedded, proper label selectors, scrapes only `/metrics`
- **buildMutateFn reconciles desired state** — prevents config drift and unauthorized modifications to managed resources

---

## Consolidated Priority Table

| Priority | # | Finding | Severity | Status |
|----------|---|---------|----------|--------|
| ~~P0~~ | 1 | ~~Shell injection via NADInterface/NADSubnet~~ | ~~HIGH~~ | **FIXED** |
| ~~P0~~ | 4 | ~~Shell injection via HA peer hostnames~~ | ~~HIGH~~ | **FIXED** |
| ~~P0~~ | 2-3 | ~~Plaintext secrets in CRD specs and rendered into ConfigMaps~~ | ~~HIGH~~ | **FIXED** |
| **P1** | 5 | Hook library path traversal (user-supplied hooks only) | **MEDIUM-HIGH** | PARTIAL |
| **P1** | 7 | No validating webhooks for any CRD | **MEDIUM** | OPEN |
| ~~P1~~ | 8 | ~~`CreateOrUpdate` doesn't reconcile desired spec~~ | ~~MEDIUM~~ | **FIXED** |
| **P1** | 9 | SCC: privilege escalation + RunAsAny + writable rootfs | **MEDIUM** | OPEN |
| ~~P2~~ | 6 | ~~Stork token as plain env var `Value`~~ | ~~MEDIUM~~ | **FIXED** |
| ~~P2~~ | 10 | ~~Secret resolution errors silently swallowed~~ | ~~MEDIUM~~ | **ACCEPTED** |
| **P2** | 11 | Init container `busybox:latest` unpinned | **MEDIUM** | OPEN |
| **P2** | 12 | ClientClass.Test expression injection | **MEDIUM** | OPEN |
| **P2** | 13 | No NetworkPolicies for Kea services | **MEDIUM** | OPEN |
| **P2** | 20 | HA failover delay defaults too slow | **MEDIUM** | RESOLVED |
| **P3** | 14 | `hostNetwork: true` exposed with no guardrails | **LOW** | OPEN |
| **P3** | 15 | Invalid hook params silently dropped | **LOW** | OPEN |
| **P3** | 16 | `T1Percent`/`T2Percent` typed as string | **LOW** | OPEN |
| **P3** | 17 | No `MaxLength` on string fields | **LOW** | OPEN |
| **P3** | 18 | Init container missing resource limits | **LOW** | OPEN |
| **P3** | 19 | Operator builder image not pinned by digest | **LOW** | OPEN |

---

## Files Reviewed

### API Types
- `api/v1alpha1/common_types.go`
- `api/v1alpha1/keadhcp4server_types.go`
- `api/v1alpha1/keadhcp6server_types.go`
- `api/v1alpha1/keacontrolagent_types.go`
- `api/v1alpha1/keadhcpddns_types.go`
- `api/v1alpha1/keaserver_types.go`
- `api/v1alpha1/ha_types.go`

### Config Rendering
- `internal/kea/config.go`
- `internal/kea/dhcp4_config.go`
- `internal/kea/dhcp6_config.go`
- `internal/kea/ctrl_agent_config.go`
- `internal/kea/ddns_config.go`
- `internal/kea/ha_config.go`

### Resource Builders
- `internal/resources/deployment.go`
- `internal/resources/statefulset.go`
- `internal/resources/stork.go`
- `internal/resources/configmap.go`
- `internal/resources/service.go`
- `internal/resources/validate.go` *(new — input validation)*
- `internal/resources/podmonitor.go` *(new — PodMonitor builder)*

### Controllers
- `internal/controller/reconciler.go`
- `internal/controller/keadhcp4server_controller.go`
- `internal/controller/keadhcp6server_controller.go`
- `internal/controller/keacontrolagent_controller.go`
- `internal/controller/keadhcpddns_controller.go`
- `internal/controller/keaserver_controller.go`

### Infrastructure
- `Dockerfile`
- `config/scc/kea-dhcp-scc.yaml`
- `config/rbac/role.yaml`
- `config/manager/manager.yaml`
- `cmd/main.go`
