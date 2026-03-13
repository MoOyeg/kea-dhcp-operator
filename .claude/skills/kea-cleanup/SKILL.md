---
name: kea-cleanup
description: >
  Uninstall the OCP Kea DHCP operator and remove all active operands (CRs) from the cluster.
  Use this skill whenever the user wants to: uninstall the Kea operator, remove Kea DHCP instances,
  clean up Kea resources, delete all Kea CRs, tear down Kea deployment, or do a full Kea cleanup
  before reinstalling. Trigger on mentions of "uninstall kea", "remove kea", "cleanup kea",
  "delete kea operator", "remove dhcp instances", "clean slate", or any request to tear down or
  remove the Kea DHCP operator and its managed resources.
---

# Kea DHCP Operator Cleanup Skill

This skill performs a clean removal of the OCP Kea DHCP operator and all its operands (managed
custom resources) from a Kubernetes/OpenShift cluster.

## Prerequisites

- **KUBECONFIG** is set or provided by the user (default: `/home/moyo/kubeconfig`)
- `oc` or `kubectl` CLI is available
- `operator-sdk` CLI is available (for OLM-installed operators)

## Cleanup Procedure

Follow these steps in order. Operands must be deleted before the operator, otherwise finalizers
can leave resources stuck in a terminating state.

### Step 1: Discover all Kea operands across all namespaces

Check every CRD for active instances. The five Kea CRDs and their short names are:

| CRD | Short Name | Kind |
|-----|-----------|------|
| keadhcp4servers.kea.openshift.io | kd4 | KeaDhcp4Server |
| keadhcp6servers.kea.openshift.io | kd6 | KeaDhcp6Server |
| keacontrolagents.kea.openshift.io | kca | KeaControlAgent |
| keadhcpddns.kea.openshift.io | kdd | KeaDhcpDdns |
| keaservers.kea.openshift.io | ks | KeaServer |

Run discovery:

```bash
for kind in kd4 kd6 kca kdd ks; do
  echo "=== $kind ==="
  oc get $kind --all-namespaces 2>/dev/null || echo "  (CRD not installed)"
done
```

Report what was found to the user before deleting anything.

### Step 2: Delete all operands

Delete KeaServer (umbrella) CRs first — they create child CRs, so deleting the parent
cleans up children automatically. Then delete any standalone CRs.

**Order matters**: `ks` first, then `kd4`, `kd6`, `kca`, `kdd`.

```bash
# Delete umbrella resources first
oc delete ks --all --all-namespaces --wait=true 2>/dev/null

# Then standalone CRs
oc delete kd4 --all --all-namespaces --wait=true 2>/dev/null
oc delete kd6 --all --all-namespaces --wait=true 2>/dev/null
oc delete kca --all --all-namespaces --wait=true 2>/dev/null
oc delete kdd --all --all-namespaces --wait=true 2>/dev/null
```

Wait for all managed resources (Deployments, StatefulSets, ConfigMaps, Services, PodMonitors)
to be garbage-collected via owner references. Verify:

```bash
oc get pods -n kea-system
oc get podmonitors -n kea-system 2>/dev/null
```

If any resources are stuck terminating, check for finalizer issues:

```bash
# Example: remove stuck finalizer (only if truly stuck)
oc patch kd4 <NAME> -n <NS> --type merge -p '{"metadata":{"finalizers":null}}'
```

### Step 3: Uninstall the operator

Determine how the operator was installed, then uninstall accordingly.

#### OLM bundle install (most common)

Check for a CSV in the operator namespace:

```bash
oc get csv -n kea-dhcp-operator 2>/dev/null
```

If a CSV exists, use operator-sdk to clean up:

```bash
operator-sdk cleanup ocp-kea-dhcp -n kea-dhcp-operator
```

This removes the Subscription, CSV, CatalogSource, OperatorGroup, and CRDs.

#### Direct `make deploy` install

```bash
make undeploy IMG=quay.io/mooyeg/ocp-kea-dhcp:v<VERSION>
```

#### Standalone installer

```bash
oc delete -f dist/install.yaml
```

### Step 4: Clean up residual resources

The operator auto-creates some resources that aren't owned by the CSV. Check and clean:

```bash
# Check if kea-system namespace still has resources
oc get all -n kea-system 2>/dev/null

# Remove the kea-dhcp SCC (OpenShift only)
oc get scc kea-dhcp 2>/dev/null && oc delete scc kea-dhcp

# Remove leftover RoleBindings for SCC
oc get rolebinding -n kea-system -l app.kubernetes.io/managed-by=kea-operator 2>/dev/null

# Optionally remove the kea-system namespace if empty
oc get all -n kea-system 2>/dev/null | grep -q 'No resources' && \
  oc delete namespace kea-system
```

### Step 5: Verify clean state

```bash
# No Kea CRDs should remain
oc get crd | grep kea

# No Kea pods
oc get pods --all-namespaces | grep kea

# No operator CSV
oc get csv --all-namespaces | grep kea

# No catalog source
oc get catalogsource -n kea-dhcp-operator 2>/dev/null
```

Report the final state to the user.

## Important Notes

- Always delete operands (CRs) before the operator. If the operator is removed first, its
  controllers can't process deletion and owned resources may become orphaned.
- KeaServer (umbrella) CRs should be deleted first since they own child CRs.
- The `kea-system` namespace and `kea-dhcp` SCC are created by the operator at startup,
  not by OLM, so `operator-sdk cleanup` does not remove them.
- PodMonitors (created when stork is enabled) are owned by the CR and will be garbage-collected
  when the CR is deleted.
- If the user only wants to remove operands but keep the operator running, stop after Step 2.
