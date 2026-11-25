/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

// TemplateRevision identifies how this operator release renders the pod
// templates of Kea workloads. Bump it whenever BuildDeployment or
// BuildStatefulSet produce a different pod template for the same input;
// TestTemplateRevisionMatchesBuilderOutput fails until you do.
//
// The DHCP controllers record it on each workload. A workload rendered by an
// earlier revision whose pods keep memfile leases in pod-local storage is not
// restarted just because the operator was upgraded: a restart would discard
// the leases (two HA peers replaced seconds apart cannot resynchronize them).
// It picks up the new template with its next spec or configuration change,
// or when the user approves the restart.
//
// History:
//   - 1: v0.0.35 and earlier (workloads carry no revision annotation).
//   - 2: the HA init container no longer prints the configuration; a
//     standalone server with a NAD and credentials runs the NAD setup inline.
//   - 3: credentials are rendered into an operator-owned configuration Secret
//     that the pods mount; no credential env vars or startup substitution;
//     the HA init container stops on its first failing step.
const TemplateRevision = "3"

// LegacyTemplateRevision is the revision of workloads created before the
// operator recorded one.
const LegacyTemplateRevision = "1"
