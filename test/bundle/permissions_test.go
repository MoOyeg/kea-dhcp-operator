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

// Package bundle checks the committed OLM bundle against the generated
// manifests it is built from.
package bundle

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const repoRoot = "../.."

// grant is one verb on one resource (optionally one named object) in one API
// group: rules are compared at this granularity because operator-sdk and
// controller-gen group the same grants into rules differently.
type grant struct{ group, resource, name, verb string }

func (g grant) String() string {
	s := g.verb + " " + g.resource
	if g.group != "" {
		s += "." + g.group
	}
	if g.name != "" {
		s += "/" + g.name
	}
	return s
}

func grants(rules []rbacv1.PolicyRule) map[grant]bool {
	out := map[grant]bool{}
	for _, r := range rules {
		names := r.ResourceNames
		if len(names) == 0 {
			names = []string{""}
		}
		for _, g := range r.APIGroups {
			for _, res := range r.Resources {
				for _, n := range names {
					for _, v := range r.Verbs {
						out[grant{g, res, n, v}] = true
					}
				}
			}
		}
		for _, url := range r.NonResourceURLs {
			for _, v := range r.Verbs {
				out[grant{resource: url, verb: v}] = true
			}
		}
	}
	return out
}

func readYAML(t *testing.T, path string, into interface{}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func missing(want, have map[grant]bool) []string {
	var out []string
	for g := range want {
		if !have[g] {
			out = append(out, g.String())
		}
	}
	sort.Strings(out)
	return out
}

// The bundle is what OLM installs, so a permission added with an RBAC marker
// only reaches a cluster once the CSV carries it. `make verify-generated`
// checks the bundle CRDs but not the CSV permissions, and a missing verb only
// shows up on a cluster, as Forbidden errors.
func TestBundlePermissionsMatchGeneratedRBAC(t *testing.T) {
	var managerRole, metricsAuthRole rbacv1.ClusterRole
	readYAML(t, "config/rbac/role.yaml", &managerRole)
	readYAML(t, "config/rbac/metrics_auth_role.yaml", &metricsAuthRole)
	want := grants(append(managerRole.Rules, metricsAuthRole.Rules...))

	var csv struct {
		Spec struct {
			Install struct {
				Spec struct {
					ClusterPermissions []struct {
						ServiceAccountName string              `json:"serviceAccountName"`
						Rules              []rbacv1.PolicyRule `json:"rules"`
					} `json:"clusterPermissions"`
				} `json:"spec"`
			} `json:"install"`
		} `json:"spec"`
	}
	readYAML(t, "bundle/manifests/ocp-kea-dhcp.clusterserviceversion.yaml", &csv)
	var rules []rbacv1.PolicyRule
	for _, p := range csv.Spec.Install.Spec.ClusterPermissions {
		if p.ServiceAccountName == "ocp-kea-dhcp-controller-manager" {
			rules = append(rules, p.Rules...)
		}
	}
	have := grants(rules)

	const fix = "run 'make bundle' and commit the CSV"
	if m := missing(want, have); len(m) > 0 {
		t.Errorf("the bundle CSV lacks permissions the RBAC markers grant; %s:\n  %s", fix, strings.Join(m, "\n  "))
	}
	if m := missing(have, want); len(m) > 0 {
		t.Errorf("the bundle CSV grants permissions the RBAC markers no longer do; %s:\n  %s", fix, strings.Join(m, "\n  "))
	}
}
