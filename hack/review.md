# Code Review - OCP Kea DHCP Operator

Date: 2026-03-10

## Critical / High Priority

### 1. No Event Recording in Any Controller
All 6 controllers lack `record.EventRecorder`. Users can't see reconciliation history via `kubectl describe`.

**Files**: All controller files in `internal/controller/`

### 2. Masked Status Update Errors
When config rendering fails, the status update error is discarded with `_ = r.Status().Update(ctx, server)`.

**Files**: keadhcp4server_controller.go:133, keadhcp6server_controller.go:109, keacontrolagent_controller.go:90, keadhcpddns_controller.go:90

### 3. Missing `RenderJSONWithServerName()` on DHCP6 Renderer
dhcp4_config.go has this for HA StatefulSet per-ordinal configs; dhcp6_config.go lacks it entirely.

**Files**: internal/kea/dhcp6_config.go

### 4. Missing Watches for Reconciled Resources
Routes and PodMonitors are created/reconciled but not watched via `Owns()` in `SetupWithManager`.

**Files**: keadhcp4server_controller.go, keadhcp6server_controller.go, keastorkserver_controller.go

### 5. Missing SecurityContext on Init Container, Stork Agent, and Stork Server
Main Kea containers properly drop capabilities. These three containers have no security context.

**Files**: statefulset.go:274-280, stork.go:107-137, stork_server.go:123-156

### 6. Hardcoded `busybox:latest` Init Container Image
Non-deterministic, breaks air-gapped environments, no way to override.

**Files**: statefulset.go:36

### 7. Missing ServiceAccountName in Stork Server Deployment
Pods use default SA instead of the application SA, causing RBAC issues.

**Files**: stork_server.go:121-162

---

## Medium Priority

### 8. No Requeue Logic for Transient Failures
No controller uses `ctrl.Result{RequeueAfter: ...}`. All return `ctrl.Result{}, nil` on success.

### 9. Massive Controller Code Duplication
keadhcp4server_controller.go (501 lines) and keadhcp6server_controller.go (304 lines) share nearly identical logic.

### 10. Silent Hook Parameter Errors
config.go:174 — if `json.Unmarshal` of hook parameters fails, parameters are silently dropped.

### 11. Inconsistent API Type Validation
- Pointer fields marked `+kubebuilder:validation:Required`
- `ControlSocket.SocketType` has Enum but no Required marker
- `KeaServerStatus` doesn't embed `ComponentStatus`

### 12. HostNetwork Not Passed to StatefulSet in HA Mode
keadhcp4server_controller.go:267-294 — hostNetwork extracted but not passed to StatefulSet params.

### 13. Weak KeaServer Child Status Checking
keaserver_controller.go:293-313 — `isChildReady()` only checks `Phase == "Running"`, ignoring conditions.

### 14. Hardcoded Route Domain in KeaStorkServer
keastorkserver_controller.go:172-174 — uses `"cluster.local"` instead of actual cluster apps domain.

---

## Low Priority

### 15. Test Coverage Gaps
- Controller tests are all scaffolding with TODOs
- DHCP6 config has 3 tests vs DHCP4's 5
- No tests for StatefulSet creation, NAD IP logic, or security contexts

### 16. Makefile VERSION Mismatch
Makefile:6 has `VERSION ?= 0.0.3` but deployed version is v0.0.14.

### 17. Subnet4/Subnet6 Type Duplication
keadhcp4server_types.go and keadhcp6server_types.go share many identical fields.

### 18. NAD IP Assignment Logic Duplication
Same shell script pattern duplicated between deployment.go and statefulset.go.
