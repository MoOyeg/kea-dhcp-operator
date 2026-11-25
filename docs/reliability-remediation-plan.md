# Kea operator reliability remediation plan

This plan fixes the credential handling, HA reconciliation, workload migration, lease persistence, Secret rotation, readiness, and test coverage problems identified in the repository review. Implement the changes as small, independently reviewable pull requests, beginning with credential handling and failure propagation. The final release must demonstrate DHCP service recovery and lease preservation, as well as correct generated Kubernetes resources.

Baseline: commit `7cd5953`, reviewed on November 24, 2026, and merged to `main` as `f1c377a`. The DDNS fixes in that commit are part of the baseline and must remain covered. New APIs, helpers, condition reasons, test names, and CI jobs described below are proposals, not features already available in this checkout, unless the [release plan](#release-plan) marks them as delivered.

## Implementation order and completion criteria

| Order | Change | Successful outcome | Main dependency |
| --- | --- | --- | --- |
| 1 | Safe credential rendering and removal of configuration logging | Supported credentials survive exactly; missing credentials stop the rollout; passwords never appear in logs | Configuration stored in an operator-owned Secret |
| 2 | Error propagation and staged HA configurations | Invalid or partially applied revisions cannot replace the active configuration | Controller failure tests |
| 3 | Guard workload mode changes | An unsupported Deployment to StatefulSet transition cannot start a second DHCP workload | Existing workload discovery |
| 4 | Complete DHCPv6 HA orchestration | Each managed peer has a stable identity, reachable endpoint, and distinct configuration | Steps 2 and 3 |
| 5 | Durable lease storage | Supported persistent deployments recover lease records after pod replacement | Storage API and lifecycle policy |
| 6 | Credential rotation | Referenced Secret changes cause bounded, repeatable rollouts | Steps 1, 2, and 5 |
| 7 | Startup and readiness checks | Pod and CR readiness reflect daemon health without causing HA bootstrap deadlock | Component health helper |
| 8 | CI and service recovery tests | Every release has reproducible evidence for the behaviors above | Add fast CI early; expand with each step |

All implementation pull requests should add their regression tests before changing the behavior, demonstrate the expected initial failure, and then pass those tests after the fix. Do not postpone testing until the last step.

The order above is the dependency order, not the shipping order. Several steps contain a small urgent fix and a larger redesign; the [release plan](#release-plan) ships the small fixes first so that a security-relevant change is never held back by an architectural one.

## Step 0 Establish a reproducible baseline

1. Use the Go toolchain declared in [go.mod](../go.mod) and [Dockerfile](../Dockerfile). The baseline requires Go 1.26.0 or later and selects toolchain 1.26.11. Do not lower the module requirement merely to accommodate an older workstation.
2. Record the commit, tool versions, Kea image digests, Kubernetes version, architecture, and command exit codes with the test results. The current default Kea image tag is 3.0.2; exercise the actual shipped images.
3. Run the renderer/resource tests, controller suite, lint, and the opt-in parser checks shown in the validation section. Record any prerequisite failures separately from test failures.
4. Preserve the DDNS regression cases added through `7cd5953`: explicit empty overrides, zero TTL percentage, mutual exclusion of fixed and calculated TTL settings, all supported scopes, and DHCPv6 extras.
5. Add regression cases for the failures below. Use synthetic credentials only. Never copy production Secret contents into fixtures or test artifacts.

The baseline is complete when another developer can run the same commands and distinguish a failing product assertion from missing Go, envtest assets, Podman, a cluster, or storage provisioning.

## Step 1 Prepare credentials as JSON and stop logging configurations

### Problem and affected code

[Deployment credential preparation](../internal/resources/deployment.go), in `buildSecretEnvSubstCommand`, and [StatefulSet initialization](../internal/resources/statefulset.go), in `BuildStatefulSet`, insert environment values into `sed` expressions. This applies two incompatible text formats to a password: `sed` replacement syntax and JSON string syntax. For example, `a&b` substitutes the matched placeholder into the password, `a|b` breaks the expression, a quote can break JSON, and a backslash can change the value.

The StatefulSet init script also prints the final configuration after substitution. Remove this immediately: the configuration can contain database passwords and other sensitive values. This removal ships on its own in Release 1, ahead of the redesign below.

Reproduction with the expression the builders emit today:

| Password | Result in the configuration |
| --- | --- |
| `a&b` | `a$KEA_Xb` (the matched placeholder is inserted) |
| `a\|b` | `sed` aborts with `unknown option to 's'`; the placeholder stays in place |
| `a"b` | Invalid JSON |
| `a\b` | `ab` |

### Chosen design: render credentials into an operator-owned Secret

The original proposal was a packaged preparation helper (a new binary, a multi-architecture image, and an init container for both workload types). That adds a second published image to every release and catalog submission, plus UID and SCC work, for a problem that disappears if substitution never happens. The accepted design removes runtime substitution instead:

1. When a component references credentials, the controller reads the referenced Secret keys at reconcile time and renders the complete Kea configuration with the real values through `encoding/json`. Every character is encoded correctly by construction; there is no second text format.
2. Store that configuration in an operator-owned Secret instead of a ConfigMap, owned by the CR, and mount it read-only at `/etc/kea` (or the template path the StatefulSet ordinal selection already uses). Components without credentials keep using a ConfigMap.
3. Remove `buildSecretEnvSubstCommand`, the StatefulSet `sed` loop, and the `SecretKeyRef` environment variables for credentials. The init container keeps only ordinal selection and peer-address resolution, each with explicit error handling.
4. Compute the configuration hash from the placeholder-rendered configuration plus the credential revision token from Step 6 (referenced Secret UIDs and resourceVersions). Never place a plain hash of a credential in an annotation; a low-entropy password hash is a dictionary-attack target.
5. A Secret change re-renders the configuration and changes the hash, so Step 6 reduces to the watch it already needs.
6. Accepted cost: the rendered configuration is readable by anyone who can read Secrets in the namespace. That is the same audience that can already read the referenced credential Secret, so no new disclosure is introduced. Document it.
7. Log only safe metadata such as the component, ordinal, configuration revision, and failed field path. Do not include input JSON, environment dumps, Secret values, or raw parser errors containing credential text.
8. Verify the Stork sidecar still has its intended access to the configuration directory, and that resolved credentials are absent from ConfigMaps, workload annotations, CR status, and Events.

Fall back to the preparation-helper design only if a future requirement forbids credentials in a Kubernetes Secret owned by the operator.

### Test updates

| Test location | Cases to add | Required assertion |
| --- | --- | --- |
| Renderer credential tests | Empty string, spaces, ampersand, pipe, single/double quotes, backslash, dollar sign, backticks, newline, and supported non-ASCII text | Decode the rendered JSON and compare the credential value exactly with the input |
| Renderer credential tests | Password equal to another placeholder; unrelated fields containing placeholder-like text | Only the credential field holds the value; nothing is substituted recursively |
| Controller tests | Missing Secret, missing key | Dependency condition is set; no configuration Secret or workload revision is created |
| Controller tests | Credential present | Configuration is stored in an owned Secret, not a ConfigMap; no ConfigMap, annotation, status field, or Event contains the value |
| `internal/resources/statefulset_test.go` | Execute the init script against temporary fixtures with two ordinals | Correct configuration selected; stdout/stderr contain no credential or configuration dump; a failing stage exits nonzero |
| Kea container tests | Rendered configuration with punctuation-heavy credentials | Shipped parser accepts it |

Capture output from the actual init path. A test that merely searches the generated command for a particular string is insufficient. Add a database integration case with punctuation in the password to establish successful authentication as well as correct JSON.

### Successful outcome

A supported credential reaches Kea unchanged. Logs and Kubernetes objects other than the operator-owned configuration Secret contain no resolved credentials. Both workload types use one tested rendering path, and no runtime substitution remains.

## Step 2 Propagate HA errors and stage configuration revisions

### Problem and affected code

In [the DHCPv4 controller](../internal/controller/keadhcp4server_controller.go), `reconcileHAConfigs` logs rendering and ConfigMap errors, then returns a hash as if configuration succeeded. The caller proceeds and sets `ConfigValid=True`. A missing or stale ordinal configuration can therefore be presented as a successful update.

### Implementation steps

1. Change the helper to return a structured result and an error. Separate pure rendering from Kubernetes writes. The result should identify the complete ordered set of ordinal configurations and its deterministic revision.
2. Validate the whole requested topology and render all peers before writing any resources. Reject duplicate names, unsupported role combinations, and ambiguous peer-to-ordinal mappings. Do not invent a fallback `serverN` identity for an undeclared managed peer.
3. Calculate the combined revision from a deterministic encoding of ordinal, peer identity, and rendered configuration. Map iteration order must not affect names or hashes.
4. Stage configuration in new, revision-specific ConfigMaps. Updating several Kubernetes objects is not an atomic transaction. Reusing active ConfigMap names would let a restarted old pod read a partial new revision even if the workload update is withheld.
5. Reconcile every required ConfigMap and prerequisite Service successfully before updating the StatefulSet template to reference the new revision. If any write fails, return the error and retain the old template and active revision.
6. Distinguish validation from application failures. Invalid specifications set `ConfigValid=False` with a useful field/ordinal path. A valid configuration whose ConfigMap cannot be written should report an application failure without claiming the new revision is applied or ready. Retry transient API errors through normal reconciliation.
7. Define status semantics explicitly: record the generation evaluated, the desired revision, and the applied revision separately where necessary. `ConfigValid` describes the evaluated specification; readiness must identify which revision is actually running. Avoid reporting success based on old ready replicas during a new rollout.
8. Garbage-collect unused staged revisions only after confirming no workload template or live pod still references them. Retain the active revision and the documented rollback revision. Cleanup failure must not erase the successful rollout result or delete a referenced configuration.
9. Use the same failure semantics for standalone DHCPv4, DHCPv6, and the umbrella resource. Keep known-good running configuration available when a newer specification is invalid.

### Test updates

Expand `internal/controller/keadhcp4server_controller_test.go` and `reconciler_test.go` with the following cases. Use a client wrapper or interceptors to fail a selected write deterministically.

| Scenario | Required assertion |
| --- | --- |
| Invalid global, subnet, or pool DDNS configuration in HA mode | Error/invalid condition; no new configuration or workload revision is applied |
| Failure while rendering a later ordinal | No earlier ordinal has been written; active revision remains unchanged |
| Forbidden or transient failure writing the second ConfigMap | Reconcile returns an error; template still references the old complete revision |
| Retry after partial staging | All desired maps eventually exist; template changes once; no duplicate resources |
| Operator restart after staging but before template update | Reconcile resumes from observed state without losing the active revision |
| Replacement old-revision pod during failed staging | Pod still reads the old complete configuration set |
| Same desired input reconciled repeatedly | Same revision and references; no unnecessary resource updates or repeated rollout |
| Garbage collection with old pods still present | Referenced maps are retained; only eligible unused revisions are removed |

Use envtest for API validation and status updates. Use a real cluster for pod replacement and reference-lifetime behavior; envtest does not run workload controllers.

### Successful outcome

Every pod starts from a complete revision. Invalid input and API write failures cannot advance the active workload, and status never falsely reports the failed revision as applied.

## Step 3 Prevent unsafe changes between standalone and HA modes

### Problem and affected code

`reconcileDhcp4Workload` selects a Deployment or StatefulSet based on the current HA field. It does not retire the opposite workload. An update that adds or removes HA can leave both running against the same pool.

### Immediate implementation

1. Discover existing owned Deployments and StatefulSets before creating or scaling a workload. Check owner UIDs, not just labels or names.
2. Initially prohibit in-place changes between standalone and managed HA after a workload has been established. Report `ModeChangeRequiresMigration` and preserve the existing workload. Add API transition validation if supported by the published Kubernetes support floor, with a controller guard for existing objects and upgrade scenarios.
3. Apply the rule to the umbrella `KeaServer` path as well. An umbrella update must not bypass the child controller's protection or report the requested topology as ready when the child rejected it.
4. If an upgrade discovers both workload kinds already active, report the conflict and stop creating/scaling additional workloads. Do not guess which workload or lease store can be discarded. Provide the recovery procedure below.
5. Protect against overlapping standalone instances during ordinary updates too. For a single memfile/PVC server, use a replacement policy that does not intentionally overlap old and new writers, and validate supported replica counts. A database shared by multiple servers is not by itself proof that those servers form a supported HA topology.

### Explicit migration procedure

1. Identify the currently serving workload, its CR UID, current applied configuration, lease database, and recoverable backup. If both kinds already exist, determine the authoritative lease state before stopping either one.
2. Validate the target configuration, topology, storage, and required network access before the maintenance window. Release 1 makes the operator do the part it can see: it resolves credentials and renders the target before evaluating the mode guard, so `ModeChangeRequiresMigration` only appears with `ConfigurationValid=True`; an invalid or credential-less target reports that problem instead.
3. Stop serving from the source workload through a supported operator-controlled maintenance mechanism. A manual scale-down alone is insufficient if reconciliation immediately restores replicas. If no maintenance mechanism exists, document and test how reconciliation is suspended during migration.
4. Wait for source pods to terminate and for the old servers to stop answering DHCP traffic. Readiness alone does not stop DHCP broadcast or host-network traffic.
5. Transfer or restore leases using a backend-appropriate procedure. Never attach one writable memfile lease directory to independent HA peers. For memfile, Release 1 documents export of the lease rows right before the source stops and import into each target pod followed by an in-process restart that keeps the `emptyDir` ([Preserving memfile leases](ha.md#preserving-memfile-leases)). Operator-assisted import before the target serves, which removes the remaining window, belongs with Step 5.
6. Create the target workload only after the source is stopped and state is ready. Confirm the target's identities and lease records before admitting normal traffic.
7. Verify allocation and renewal from a test client, then retire only obsolete resources owned by the source. Retain backups and PVCs according to the documented policy.
8. For rollback, stop the target before restarting the source. Reconcile lease changes made since cutover; restarting an old snapshot without that check can reintroduce conflicting allocations.

Automated migration can be a later feature. It requires a persisted, restart-safe state machine and explicit handling of maintenance, draining, state transfer, verification, and rollback. Do not implement it as “delete the old kind, then create the new kind.”

### Test updates and successful outcome

Add both mode-change directions, umbrella updates, operator restarts, conflicting owner UIDs, and the already-both-active upgrade case to controller tests. Assert that no second active workload is introduced and no unrelated resource is deleted. Add cluster tests for standalone rollout overlap and the documented maintenance procedure. Success means supported reconciliation never creates two independent servers for the same standalone pool and unsupported transitions fail visibly without destroying the serving workload.

## Step 4 Complete DHCPv6 HA orchestration

### Problem and affected code

[The DHCPv6 controller](../internal/controller/keadhcp6server_controller.go) calls `fillPeerURLs`, which generates StatefulSet-style names, but then creates a Deployment with one configuration. Operator-managed peer identities and the generated network endpoints are not created together.

### Implementation steps

1. Define two explicit supported cases: a managed local HA topology with one ordinal per managed peer, and a single local server with explicitly configured external peers. Do not infer that every declared peer belongs in this cluster.
2. Until managed DHCPv6 HA is implemented, reject requests that depend on generated peer URLs or multiple managed identities. Preserve any documented, validated external-peer case; otherwise report that HA topology as unsupported before creating a misleading workload.
3. Extract reusable orchestration from the corrected DHCPv4 path: desired peer/configuration planning, revision staging, workload selection, owned-resource reconciliation, and status reporting. Keep protocol-specific rendering and network address logic separate.
4. For managed DHCPv6 HA, create the StatefulSet, headless Service, required per-peer Services, and one rendered configuration per ordinal. Each configuration must identify its actual peer through `RenderJSONWithServerName`.
5. Use `net/url` and `net.JoinHostPort` for IPv6 endpoints. Test literal IPv6 addresses, generated names, explicit ports, DNS resolution, and preservation of external peer URLs. Do not copy DHCPv4 octet arithmetic or rewrite every external URL to a local Service.
6. Validate the selected HA mode and role combination against Kea 3.0.2 for DHCPv6. Test supported modes separately; reject unsupported combinations rather than assuming DHCPv4 and DHCPv6 modes are identical.
7. Define replica defaulting and peer-count rules once. Explicitly cover omitted replicas, zero replicas for maintenance if supported, and mismatches between managed peer count and replicas.
8. Verify cold start with both peers initially unavailable. Initialization, Service endpoint publication, and readiness must allow peers to discover each other. If parallel pod creation is required, retain controlled update behavior so a normal rollout does not intentionally stop all peers together. Kubernetes distinguishes StatefulSet pod management from update strategy; test the selected combination on supported cluster versions. [StatefulSet behavior](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
9. Update the HA guide and examples to state precisely which DHCPv4 and DHCPv6 topologies are supported. Remove broad support claims until those topologies pass cluster tests.

### Test updates

| Layer | Tests |
| --- | --- |
| Renderer | Every managed ordinal has its own `this-server-name`; same ordered peer list; no invented peers; supplied spec is not mutated |
| Resource builder | StatefulSet, configuration references, Services, peer selectors, IPv6 URLs, and ownership are correct |
| Controller | Managed HA creates no Deployment; standalone creates no StatefulSet; external peers retain their endpoints; invalid counts fail before writes |
| Kea container | Both ordinal configurations pass the appropriate shipped parser checks; required HA libraries load in an integration environment |
| Cluster | Both peers cold-start; a client gets and renews an IPv6 lease; a peer failure causes bounded recovery; the restored peer synchronizes |

Set the failover deadline from the fixture's HA timers plus a documented scheduling allowance. Assert lease/client identity and absence of conflicting assignments, not just that a pod becomes `Running`.

### Successful outcome

Every managed peer has a real endpoint, stable identity, distinct configuration, and demonstrated failover behavior. Unsupported topologies are rejected before workloads start.

## Step 5 Make lease durability explicit

### Problem and affected code

Both [workload builders](../internal/resources/) mount `/var/lib/kea` from `emptyDir`. Setting Kea's `persist: true` writes a lease file, but does not make that file survive pod replacement. An `emptyDir` is removed with its pod; it is not durable storage. [Kubernetes volume behavior](https://kubernetes.io/docs/concepts/storage/volumes/)

### Implementation steps

1. Add an explicit lease-storage API shared by DHCPv4 and DHCPv6, for example an ephemeral mode, a standalone existing-claim reference, and a managed claim template for HA. Finalize field names and validation before publishing examples.
2. Preserve existing storage behavior on upgrade unless the user explicitly migrates it. Surface ephemeral storage clearly in status and documentation when memfile persistence is enabled. Do not silently replace an active `emptyDir` with an empty PVC and imply that old leases were transferred.
3. For standalone durable memfile, mount a selected PVC at the effective lease directory and permit only the supported single-writer topology. Define a rollout policy that prevents concurrent file writers, including the case where old and new pods could run on the same node.
4. For managed HA, provision one claim per ordinal through `volumeClaimTemplates`, or another explicitly supported per-peer mapping. Each peer has its own lease file; Kea's HA protocol synchronizes state. Never make two peers share the same memfile database.
5. Define claim size, storage class selection, access modes, permissions, and retention. Retain user-owned claims. Prefer retaining managed lease claims on CR deletion and scale-down unless an explicit retention policy says otherwise. Verify behavior on the minimum supported Kubernetes version.
6. Check the effective `lease-database.name` path. A custom path outside the mounted storage must be rejected for durable memfile mode or mounted deliberately. Also check `persist: false`; a PVC cannot make an intentionally unpersisted database durable.
7. Extend resource reconciliation deliberately for any newly managed PVC kind. The default branch of `buildMutateFn` currently only sets an owner reference; it is not a general-purpose PVC lifecycle implementation. Respect immutable fields and storage expansion rules.
8. Reject unsupported in-place storage-class or claim-template changes with a migration explanation. StatefulSet storage changes can require recreation; do not create an endless retry loop by repeatedly submitting an immutable-field update.
9. Document backup, restore, node rescheduling, full-cluster restart, and migration from ephemeral storage. Define an external-database mode separately; a successful TCP connection is not proof of lease persistence or correct schema setup.
10. State the HA exposure accurately. With two memfile HA peers on `emptyDir`, Kea's lease-update protocol already preserves leases across the replacement of a single peer, because the surviving partner synchronizes the replacement. The real gap is a simultaneous restart of both peers. Documentation must say that, rather than implying memfile HA loses leases on any restart.
11. Define the upgrade defaulting rule explicitly: an existing CR with memfile `persist: true` and no storage field defaults to the ephemeral mode after an operator upgrade, keeps its `emptyDir`, and reports that its storage is ephemeral. OLM upgrades must never trigger an implicit storage migration.

### Test updates

| Scenario | Required assertion |
| --- | --- |
| Ephemeral configuration | Explicit ephemeral volume and truthful status/documentation |
| Standalone existing PVC | Correct claim and lease path; no unintended ownership takeover or deletion |
| Two managed HA peers | Two distinct claims; stable ordinal-to-claim mapping after replacement |
| Durable storage with `persist: false` or an unmounted custom path | Clear validation failure before rollout |
| Unsupported multi-writer or immutable storage update | Request rejected without replacing the working workload |
| Pod deletion after acquiring a lease | Replacement reads the existing lease record; the same client renews it |
| All HA pods stopped and restarted | Lease recovery succeeds without relying on a surviving peer's memory |
| Another client requests an address after recovery | Existing active lease is not allocated to that client |
| Scale-down and CR deletion | Claims follow the declared retention policy; user-owned claims survive |
| MySQL and PostgreSQL restart cases | Lease record persists across DHCP pod replacement and is usable after database reconnection |

Do not use “the client received the same address” as the only persistence assertion: a fresh allocator can coincidentally select it again. Query the lease record and verify client ID/DUID, expiry, and renewal behavior before and after replacement.

### Successful outcome

The API makes durability an explicit choice. A durable deployment recovers leases after pod replacement and a full peer restart; an ephemeral deployment does not claim that guarantee.

## Step 6 Roll workloads when referenced credentials change

### Problem and affected code

Database credentials come from `SecretKeyRef` environment variables, but the configuration hash contains placeholders. A change to the Secret does not change the template hash. Existing DHCP controller Secret watches handle Stork tokens, not all referenced database credentials.

Release 1 delivers the watch and the revision token while credentials still travel as environment variables. A rotated Secret reaches new pods immediately only where a restart loses nothing: with memfile leases in pod-local storage, the rollout is held and reported as `CredentialsRolloutPending` until a configuration change restarts the pods anyway or the user approves that exact revision, and an operator upgrade adopts the running revision without restarting anything. See [Credential Secrets and rotation](crds.md#credential-secrets-and-rotation). When Step 1's Secret-rendered configuration lands in Release 2, the token becomes an input of the configuration hash and the watch stays as is.

### Implementation steps

1. Add shared reference collection for lease, hosts, and every hosts-database entry. Follow the same rule for other credentials prepared only at startup. Keep Stork's existing watch behavior intact.
2. Index dependent CRs by Secret namespace and name. Register watches that enqueue only matching dependents. Include deletion and recreation of a referenced Secret, and update the index when a CR changes its references.
3. Use a deterministic, nonsecret revision token in the pod template. A straightforward first version derives it from the sorted referenced Secret names, UIDs, and resourceVersions. This deliberately treats any update to a referenced Secret, including metadata-only changes, as a possible rollout trigger. Document that conservative behavior rather than promising content-only restarts.
4. Do not expose plaintext or a plain hash of a low-entropy password in annotations. The revision token identifies Kubernetes object revisions, not credential values. Recreating a Secret with the same name must change the token because its UID changes.
5. Validate required keys before altering a running template. A missing Secret/key should set a clear dependency condition and retain the active workload when one exists. Initial creation should report the dependency failure instead of falsely reporting a valid running deployment.
6. Record the credential revision alongside the configuration revision as part of the applied workload state. Reconciliation after an operator restart must derive the same desired result and must not create repeated rollouts.
7. Use the rollout policy appropriate to the workload. Never restart pods whose leases live in pod-local storage without explicit approval; this applies until Step 5 provides durable storage. Rotate HA peers in a controlled sequence, waiting for health/synchronization as required by the chosen topology. Accept and document the standalone maintenance interruption for database-backed leases.
8. Document database-side rotation order: provision the new credential, update the Kubernetes Secret, wait for all intended pods to authenticate with the new value, then revoke the old credential where the database supports an overlap period. Do not promise uninterrupted rotation for a backend that invalidates the only credential immediately.

Secret environment references are configured on the container; startup preparation must be rerun to replace the active configuration. If switching to projected Secret files later, file refresh alone still does not prove that Kea has reloaded its credentials. [Kubernetes Secrets](https://kubernetes.io/docs/concepts/configuration/secret/)

### Test updates

Add `secret_rotation_test.go` for indexing, mapping, and revision computation, plus integration coverage in both DHCP controller suites. Assert that each database reference triggers a new template revision; unrelated Secrets do not; namespace isolation holds; array order is handled deterministically; deletion is reported; recreation changes identity; and repeated reconciliation with unchanged Secret objects produces no additional template change.

Start the controller manager in a watch integration test and update a Secret through the API. Calling `Reconcile` directly does not prove that the watch enqueues the right CR. In a real cluster, rotate a synthetic database password and prove that replacement pods authenticate with the new value and that the old value can then be revoked. Inspect ConfigMaps, annotations, Events, status, and captured logs for credential disclosure.

### Successful outcome

A referenced Secret revision produces one desired workload revision, converges without a loop, and is actually consumed by the new processes. The serving configuration remains intact when a dependency is missing.

## Step 7 Add startup and readiness checks with truthful status

### Problem and affected code

The main Kea containers in the Deployment and StatefulSet builders have no startup or readiness probes. Controller status uses ready replica counts without establishing that the desired Kea revision is initialized and responsive.

### Implementation steps

1. Define a component health contract. For DHCPv4, DHCPv6, and D2, use a supported local control command with a bounded timeout and validate the command result. For the Control Agent, check its own management endpoint rather than assuming a downstream DHCP server exists. Confirm available commands against each shipped image. [Kea management API](https://kea.readthedocs.io/en/kea-3.0.2/arm/ctrl-channel.html)
2. Use the tools the shipped images actually contain. The Kea 3.0.2 images provide `wget` and `nc` but not `curl`, `kea-shell`, `socat`, or Python. A Kea HTTP control socket accepts only POST, so a Kubernetes `httpGet` probe cannot be used; an `exec` probe with `wget --post-data` against the loopback control socket works without a new image. Package a probe client only if a component exposes no usable transport.
3. Resolve the probe transport from the actual configuration. Prefer a local UNIX socket or loopback endpoint and avoid exposing a new unauthenticated network listener. Support configured authentication/TLS without placing credentials in command arguments or diagnostics.
4. Define startup success as local initialization and responsiveness. Budget enough time for configured database reconnect and initial synchronization. Readiness should reject an unresponsive or administratively disabled daemon, but healthy HA standby behavior must be accepted as part of the supported topology.
5. Treat network-level DHCP service as a separate E2E assertion. A responsive control socket cannot establish that broadcasts reach the client, and a failed readiness probe does not itself prevent host-network DHCP replies.
6. Check the HA cold-start dependency cycle. If a peer must contact another peer before it becomes ready, ensure pod creation and peer Service discovery do not wait for that same readiness. Test both initial creation and rolling replacement. There is a concrete instance of this today: HA peers dial each other through the per-pod Services, which `BuildPerPodService` creates without `publishNotReadyAddresses`, while only the headless Service sets it. A readiness probe that depends on HA state would remove each unready peer from the Service its partner must reach, and neither peer would ever become ready. Either set `publishNotReadyAddresses` on the per-pod Services or keep readiness independent of HA synchronization.
7. Add liveness only with a well-defined recovery action. A transient database or peer outage should not automatically restart every server and destroy the opportunity to recover in place. Startup probes defer other probes until startup succeeds; readiness and liveness have different effects. [Kubernetes probe behavior](https://kubernetes.io/docs/concepts/workloads/pods/probes/)
8. Update status aggregation to compare observed workload generation and the intended revision, as well as ready replicas. Report progressing during a new rollout even when old pods are still ready. Make the umbrella resource reflect child failures and progress accurately.

### Test updates

| Layer | Required coverage |
| --- | --- |
| Probe helper | Success, nonzero Kea result, malformed response, EOF, timeout, inaccessible socket, TLS/auth errors; no credentials in output |
| Builders | Every component gets the appropriate probe; configured transport is respected; timeout and threshold values are explicit |
| Status unit tests | Old ready replicas do not satisfy a new revision; zero replicas and healthy standby semantics are defined; transition times remain stable when status does not change |
| Cluster | Slow startup, delayed database, healthy standby, blocked control endpoint, cold-start HA pair, and one-peer rollout |
| Service E2E | Healthy probe plus successful client allocation/renewal; readiness alone is never the lease-service assertion |

Use bounded polling rather than fixed sleeps. Success means normal startup converges, failures are reflected in status, and probes do not cause a bootstrap deadlock or a restart storm.

## Step 8 Replace smoke coverage with behavior coverage and CI

### Controller test structure

Expand the existing `keadhcp4server_controller_test.go`, `keadhcp6server_controller_test.go`, `keaserver_controller_test.go`, and related suites. The baseline happy-path tests that only assert a nil reconcile error should also inspect owned objects, effective configuration, Secret references, status, and subsequent reconciliation.

Use pure unit tests for planning and encoding, injected client failures for error paths, and envtest for admission, schema pruning, owner references, status subresources, and real watch behavior. Envtest does not prove garbage collection, scheduling, PVC binding, Deployment replacement, or StatefulSet rollout; put those assertions in cluster tests.

Ensure `KUBEBUILDER_ASSETS` and the chosen Kubernetes version are authoritative. The current suite embeds a `1.30.0` fallback path while the Makefile also chooses an envtest version; avoid maintaining two independent selections. Install/register optional APIs such as PodMonitor when a test actually exercises them.

### Required CI jobs

| Job | Required checks | Failure policy |
| --- | --- | --- |
| Go validation | Matching toolchain, formatting/vet, unit tests, controller envtest suite, lint | Any assertion, lint, or setup failure fails the job |
| Generated artifacts | Regenerate deepcopy and CRDs, synchronize bundle schemas using the repository's release conventions, check tracked diffs | Unexpected drift fails the job |
| Kea compatibility | All DDNS fixtures plus credential, HA, and storage-related configuration fixtures against shipped images | An explicitly requested check cannot silently skip |
| Container packaging | Build the manager image (and a probe client, only if Step 7 needs one); smoke-test supported architectures | Build or permission failure fails the job |
| Console | Frontend dependency install and type check/build; backend Go tests in its own module | Missing configuration/tooling is a setup failure, not a passing lint result |
| Cluster lifecycle | Storage, Secret rotation, workload replacement, HA cold start, and upgrade cases | Archive sanitized diagnostics; fail on unmet assertions |
| DHCP service | DHCPv4 and DHCPv6 allocation/renewal, HA failover, persistence, and D2 DNS updates | Require protocol-level evidence |

Add the fast jobs with the first implementation PR. Run longer cluster tests on a prepared runner and require them before a release. Add a network-isolated, disposable cluster fixture; never broadcast test DHCP traffic onto an existing production subnet.

The repository contains a nested Go module under `console-plugin/`, so root `go test ./...` does not cover its backend. The frontend declares a lint command, but its ESLint tooling/configuration must be made reproducible before treating that command as a required successful check.

### Validation commands

Run these existing commands from the repository root after provisioning their prerequisites:

```bash
go version
go test ./internal/kea/... ./internal/resources/...
make test
make lint
KEA_CONFIG_CHECK=1 go test ./internal/kea/ -run TestDDNSConfigAcceptedByKea -count=1 -v
```

Change `TestDDNSConfigAcceptedByKea` so missing Podman is a failure when `KEA_CONFIG_CHECK=1`. Skipping remains appropriate when the opt-in variable is absent. Log the actual image digest and ensure both protocol subtests execute. Parser checks cannot replace startup or service tests.

After intentionally updating generated files, check reproducibility:

```bash
make generate manifests
git diff --exit-code -- api/v1alpha1/zz_generated.deepcopy.go config/crd/bases
```

Add a narrow bundle-schema synchronization/check target as part of CI, or use the existing bundle generation workflow in a controlled release environment. `make manifests` alone does not prove that `bundle/manifests` contains the same schema. Avoid regenerating an entire versioned bundle with unintended release metadata changes just to copy CRDs.

Run console backend checks from `console-plugin/` with that module's declared toolchain. Use a lockfile-based frontend install when a tracked lockfile is available, then run `npm run type-check` and `npm run build`; enable `npm run lint` after installing and configuring its declared dependencies. Add any new Go packages to the normal test and build targets, and update the Dockerfile copy and build stages accordingly.

The existing `make test-e2e` target currently exercises operator startup. Extend it or add clearly named suites for the scenarios below before treating its success as evidence for DHCP functionality.

### End to end acceptance scenarios

1. **Standalone DHCPv4 and DHCPv6:** create a minimal supported CR; wait for the desired revision; acquire a lease using a real client; verify address, client identity, subnet, and options; renew successfully.
2. **Configuration update:** change one supported option; verify the intended rollout and client-visible result; submit an invalid configuration and prove the last working revision remains in service.
3. **Credential handling:** use synthetic punctuation-heavy database credentials; verify authentication and lease creation; check sanitized logs and Kubernetes objects for disclosure.
4. **Secret rotation:** enable the new database credential, update the referenced Secret, observe the new credential revision, verify reconnect/renewal, revoke the old credential, and verify again.
5. **Persistent memfile:** record lease identity and expiry, replace the pod, read the restored record, renew it, and verify a different client does not receive the active address. Repeat with all HA peers stopped so a survivor cannot mask missing disk persistence.
6. **Managed HA:** start both peers from zero, acquire leases, stop one peer, verify recovery within the fixture's configured deadline, restore it, and verify state synchronization. Repeat for every advertised protocol/mode combination.
7. **Mode protection:** attempt both in-place mode changes; verify a clear condition and no second active workload. Exercise the documented maintenance migration separately, including a failed target startup and rollback with state reconciliation.
8. **D2 integration:** run D2 and an authoritative test DNS server, obtain an address, and verify the expected A or AAAA record and PTR record. Renew/release or expire according to fixture timing and verify expected DNS changes.
9. **Upgrade:** start from the previous supported operator release with active leases and Secrets, upgrade, verify no unintended mode/storage migration, then perform one configuration update and one pod replacement.

For each scenario, save the CR conditions, desired/applied revisions, pod UIDs, relevant resource references, lease identities, and timing. Redact credentials and avoid dumping rendered configurations or Secret objects. Use a test network and DNS zone owned by the fixture; cleanup must only remove fixture-owned resources.

## Additional findings

These were found while reviewing the plan and belong to the steps named.

| Finding | Step | Note |
| --- | --- | --- |
| Deleting a CR leaves its `kea-dhcp-scc-<cr>-kea` RoleBinding in `kea-system`; `ensureSCCRoleBinding` creates it without an owner reference | 2 or 3 (owned-resource reconciliation) | Observed on cluster2. Set the CR as controller owner, and clean up bindings left by earlier releases on upgrade. |
| The KeaServer CRD is about 675 KB, past kubectl's 256 KiB client-side-apply annotation limit, so `oc apply -f config/crd/bases/` fails for it | 8 (generated artifacts) | OLM installs are unaffected. Document server-side apply (`oc apply --server-side`) in the quickstart, or shrink the schema. |
| `require-client-classes` is deprecated in Kea 3.0 in favour of `evaluate-additional-classes` and logs a warning at startup | 8 (Kea compatibility) | Not urgent; the parser CI job is where a future Kea minor turning it into an error would surface. |
| Upgrade defaulting for new storage and mode-guard behavior | 5 and 3 | Covered by Step 5 item 11 and the Step 3 already-both-active case. |

## Release plan

The release grouping ships each step's urgent, low-risk part first. Every PR keeps the test-first rule.

| Release | PR | Scope | Merge evidence |
| --- | --- | --- | --- |
| 1 | A1 | Remove the StatefulSet init configuration dump | Init script executed against fixtures; output contains neither the configuration nor the credential |
| 1 | B1 | Propagate HA render and ConfigMap write errors; reject undeclared or duplicate managed peers before any write | Invalid HA spec sets `ConfigurationValid=False` and creates no workload; injected write failure returns an error and leaves the workload template unchanged |
| 1 | C | Guard standalone/HA mode changes and document the maintenance migration | Both directions and the already-both-active case set `ModeChangeRequiresMigration` and keep the serving workload |
| 1 | F1 | Watch referenced credential Secrets and roll pods through a non-secret revision token | Token changes on update and recreation, stays stable otherwise; missing Secret or key sets a dependency condition; a real watch enqueues only referencing CRs |
| 1 | H1 | Fast CI: Go validation, generated-artifact and bundle-CRD drift, Kea parser check that fails instead of skipping when requested | Jobs pass on the release branch |
| 2 | A2 | Render credentials into an operator-owned Secret; remove runtime substitution | Step 1 test table. Implemented (TemplateRevision 3): `kea.SubstituteCredentials`, configuration Secrets for DHCPv4 (standalone and HA), DHCPv6, Control Agent and DDNS, no credential env vars or `sed`, `set -e` in the HA init container |
| 2 | B2 | Stage revision-specific ConfigMaps; status distinguishes desired and applied revisions | Step 2 test table |
| 2 | D | Durable storage API and lifecycle | Step 5 test table |
| 3 | E | Managed DHCPv6 HA using shared orchestration | Step 4 test table |
| 3 | G | Probes and revised status aggregation | Step 7 test table |
| 3 | H2 | Cluster lifecycle and DHCP service acceptance suites | End-to-end scenarios above |

Status: Release 1 (A1, B1, C, F1, H1) is implemented on branch `reliability/release-1` and was validated on an OpenShift cluster with `v0.0.36-rc1` on November 24, 2026: a Percona-backed DHCPv4 HA pair with a Stork server showed no credential in init container logs; a MySQL dual-password rotation rolled the pods exactly once and the new pods wrote leases after the old password was revoked; a deleted credential Secret set `CredentialsUnavailable` without touching the pods; the mode-change guard blocked both directions while the old workload kept serving, and an API watch stream showed the old pods deleted before any new pod was added during the documented foreground-deletion migration and rollback; the existing simple-HA, Stork-metrics and custom-namespace SCC end-to-end tests passed.

Upgrade blocker for Release 1 (measured on November 24, 2026 with an in-place OLM upgrade from v0.0.35 to `v0.0.36-rc3`): the init container script changes (A1), so the upgrade rolls every HA StatefulSet. With memfile leases both peers came back with **0 of 3 leases**: the replacement of ordinal 1 reported ready as soon as its containers started, ordinal 0 was replaced about 3 seconds later, and each new peer then synchronized from the other new, empty peer. Deployment-based servers were not restarted. This contradicts the Step 6 rule that pods keeping leases in pod-local storage are never restarted without approval, Resolved by holding operator-driven template changes for memfile workloads: each Deployment and StatefulSet records the `resources.TemplateRevision` that rendered it (a golden test fails when builder output changes without a bump), and a memfile workload on an older revision keeps its template until a spec or configuration change, a decided credential rollout, or `kea.openshift.io/approve-restart=<revision>`; the CR reports `RestartPending`. Database-backed workloads and changes within one revision, such as a Stork token update, are not held. Gating readiness on HA synchronization (Step 7) remains the long-term fix for rolling updates of memfile HA pairs. Verified with the same in-place OLM upgrade on November 24, 2026 using `v0.0.36-rc4`: no pod was restarted, both HA peers kept all 3 leases, and both CRs reported `RestartPending`; approving the restart with the documented lease export and import moved the pair to revision 2, and every client kept its address.

Review follow-ups on Release 1, re-validated on the same cluster with `v0.0.36-rc2`/`rc3` on November 24, 2026 (an in-place OLM upgrade from v0.0.35 left a MySQL-backed server's pod untouched; memfile HA with a MySQL hosts database held a credential rotation, then rolled once on approval and reconnected after the old password was revoked; an invalid HA target reported `ConfigurationValid=False` while the standalone server kept serving; a standalone-to-HA migration with exported and imported memfile leases kept both clients' addresses and gave new clients new ones; an optional DDNS Secret rolled the pods on update and deletion): credential rollouts no longer restart pods that keep memfile leases, and an upgrade adopts the running revision instead of restarting; optional Secret references are tracked; the mode guard runs after target validation, and the migration procedure preserves memfile leases; the MySQL/PostgreSQL hook is loaded for hosts databases, without which memfile leases with a MySQL hosts database could not start at all; and a standalone server with a network attachment and any credential now starts (the credential wrapper used to truncate the NAD setup script, so Kea exited immediately, in v0.0.35 as well).

Release 1 leaves one known limitation from Step 2: the per-ordinal ConfigMaps are still updated in place, so a failed write can leave ordinal 0 on the new configuration and ordinal 1 on the old one until the retry succeeds. The workload template is not advanced in that case, and B2 removes the window entirely.

Steps 4 and 5 can be developed in either order once failure propagation and mode guards exist, but HA durability must pass before the combined release. CI and tests should accompany each PR rather than arrive only in H2. Update [CRD documentation](crds.md), [architecture](architecture.md), [HA examples](ha.md), and [quickstart](quickstart.md) in the PR that changes their behavior.

## Release completion checklist

- [ ] All seven operational findings have implemented fixes and passing regression tests; configuration logging is removed.
- [ ] Existing DDNS fixes still pass both renderer assertions and shipped Kea parser checks.
- [ ] No valid supported credential is changed by preparation, and preparation errors prevent startup.
- [ ] Failed desired revisions cannot replace active configuration; status distinguishes validation, application, and readiness.
- [ ] Unsupported topology changes cannot start competing workloads; the maintenance migration and rollback are tested.
- [ ] Every advertised DHCPv6 HA topology has distinct identities, working endpoints, and measured failover.
- [ ] Persistent lease recovery is proven after replacement of every peer, with claim retention and ownership verified.
- [ ] Referenced credential updates reach the running processes without a reconciliation loop or credential disclosure.
- [ ] Probes and status converge during normal startup, cold-start HA, dependency delay, and rollout.
- [ ] Unit, envtest, parser, packaging, console, cluster, service, and upgrade checks pass in their intended environments.
- [ ] Generated source/CRDs/bundle schemas are reproducible and supported image/toolchain versions are recorded.
- [ ] Release notes describe new fields, defaults, rejected configurations, storage behavior, interruption windows, and recovery procedures.

These are acceptance criteria for future implementation. Creating this plan does not establish that the code has been fixed or that the acceptance tests have passed.
