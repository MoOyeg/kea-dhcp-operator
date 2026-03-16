# Code Review - OCP Kea DHCP Operator

Date: 2026-03-10
Last updated: 2026-03-16

## Critical / High Priority

### 1. ~~No Event Recording in Any Controller~~ — FIXED
All 6 controllers now have `record.EventRecorder` and emit events via `r.Recorder.Event()`.

### 2. ~~Masked Status Update Errors~~ — FIXED
All final status updates return errors properly. Early error-path updates are logged (acceptable).

### 3. ~~Missing `RenderJSONWithServerName()` on DHCP6 Renderer~~ — FIXED
Method exists at `internal/kea/dhcp6_config.go:46-50`, matching DHCP4 implementation.

### 4. ~~Missing Watches for Reconciled Resources~~ — FIXED
PodMonitors and StatefulSets now watched via `Owns()` in both DHCP4 and DHCP6 controllers.
Routes are reconciled via unstructured objects (no typed `Owns()` possible without Route CRD import).

### 5. ~~Missing SecurityContext on Init Container, Stork Agent, and Stork Server~~ — FIXED
All containers now drop ALL capabilities via `SecurityContext`.

### 6. ~~Hardcoded `busybox:latest` Init Container Image~~ — FIXED
Pinned to `busybox:1.37` and configurable via `StatefulSetParams.InitContainerImage`.

### 7. ~~Missing ServiceAccountName in Stork Server Deployment~~ — FIXED
`ServiceAccountName` is set in the PodSpec at `stork_server.go:287`.

---

## Medium Priority

### 8. No Requeue Logic for Transient Failures — OPEN
No controller uses `ctrl.Result{RequeueAfter: ...}`. All return `ctrl.Result{}, nil` on success.
Note: `ctrl.Result{}, err` does trigger workqueue retry with backoff, so this is not critical.

### 9. ~~Massive Controller Code Duplication~~ — FIXED
Common helpers extracted to `reconciler.go` (reconcileResource, setCondition, dbCredentialEnvVars, fillPeerURLs, enqueueStorkDependents, listStorkEnabledDhcp4/6). Remaining structural differences between DHCP4/DHCP6 are intentional (DHCP4 has HA StatefulSet branching).

### 10. Silent Hook Parameter Errors — OPEN
`internal/kea/config.go:172-176` — if `json.Unmarshal` of hook parameters fails, parameters are silently dropped. Should at minimum log a warning.

### 11. ~~Inconsistent API Type Validation~~ — FIXED
- `AuthClient.PasswordSecretKeyRef` and `TSIGKey.SecretRef` changed from Required pointer to optional
- `ControlSocket.SocketType` now has `+kubebuilder:validation:Required` marker
- `KeaServerStatus` defines its own fields instead of embedding `ComponentStatus` (by design)

### 12. ~~HostNetwork Not Passed to StatefulSet in HA Mode~~ — FIXED
`HostNetwork` properly passed via `StatefulSetParams` at `keadhcp4server_controller.go:299`.

### 13. ~~Weak KeaServer Child Status Checking~~ — FIXED
`isChildReady()` now checks both `Phase == "Running"` and the `Ready` condition status.

### 14. Hardcoded Route Domain in KeaStorkServer — OPEN
`keastorkserver_controller.go:220` uses `"cluster.local"` instead of actual cluster apps domain.
Note: The code later fetches the actual Route hostname, so this only affects the initial status URL.

---

## Low Priority

### 15. ~~Test Coverage Gaps~~ — FIXED
- Controller scaffolding tests replaced with real unit tests using fake client
- Tests cover: `setCondition`, `dbCredentialEnvVars`, `fillPeerURLs`, `computeNADAddresses`, `isChildReady`
- DHCP6 config tests expanded (shared networks, HA, server name override, database) to match DHCP4
- Total: 20 controller unit tests + 13 config renderer tests

### 16. Makefile VERSION Mismatch — OPEN
Makefile:6 has `VERSION ?= 0.0.3` but deployed version is v0.0.19.

### 17. Subnet4/Subnet6 Type Duplication — OPEN
keadhcp4server_types.go and keadhcp6server_types.go share many identical fields.

### 18. NAD IP Assignment Logic Duplication — OPEN
Same shell script pattern duplicated between deployment.go and statefulset.go.

---

## Summary

| Status | Count | Issues |
|--------|-------|--------|
| FIXED | 12 | #1, #2, #3, #4, #5, #6, #7, #9, #11, #12, #13, #15 |
| OPEN | 6 | #8, #10, #14, #16, #17, #18 |
