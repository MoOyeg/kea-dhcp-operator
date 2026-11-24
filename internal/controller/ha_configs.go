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

package controller

import (
	"fmt"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/kea"
)

// haConfigRenderer renders the Kea configuration seen by one named HA peer.
type haConfigRenderer interface {
	RenderJSONWithServerName(serverName string) ([]byte, error)
}

// haConfigPlan is the complete set of per-ordinal configurations of one
// revision, rendered and validated before anything is written.
type haConfigPlan struct {
	// configs holds the rendered configuration of each managed ordinal.
	configs [][]byte
	// hash identifies the whole set; it changes when any ordinal changes.
	hash string
}

// validateHAPeers rejects topologies the operator cannot map onto ordinals:
// every managed ordinal needs its own declared peer (an invented "serverN"
// identity is not in the peers list, so Kea refuses to start), and peer names
// must be unique.
func validateHAPeers(ha *keav1alpha1.HAConfig, replicas int32) error {
	seen := make(map[string]int, len(ha.Peers))
	for i, p := range ha.Peers {
		if j, dup := seen[p.Name]; dup {
			return fmt.Errorf("high-availability.peers[%d]: duplicate peer name %q (also peers[%d])", i, p.Name, j)
		}
		seen[p.Name] = i
	}
	if int(replicas) > len(ha.Peers) {
		return fmt.Errorf("replicas is %d but %d high-availability peers are declared: ordinal %d has no declared high-availability peer",
			replicas, len(ha.Peers), len(ha.Peers))
	}
	return nil
}

// planHAConfigs validates the HA topology and renders the configuration of
// every managed ordinal. It performs no writes, so a failure for any ordinal
// leaves every existing ConfigMap and the workload untouched.
func planHAConfigs(r haConfigRenderer, ha *keav1alpha1.HAConfig, replicas int32) (*haConfigPlan, error) {
	if err := validateHAPeers(ha, replicas); err != nil {
		return nil, err
	}
	plan := &haConfigPlan{configs: make([][]byte, replicas)}
	var combined string
	for i := int32(0); i < replicas; i++ {
		name := ha.Peers[i].Name
		cfg, err := r.RenderJSONWithServerName(name)
		if err != nil {
			return nil, fmt.Errorf("rendering config for ordinal %d (peer %q): %w", i, name, err)
		}
		plan.configs[i] = cfg
		combined += kea.ComputeHash(cfg)
	}
	// Same construction as before, so an upgrade does not roll HA pods.
	plan.hash = kea.ComputeHash([]byte(combined))
	return plan, nil
}

// resolveCondition sets condType to False when an earlier failure raised it,
// without adding the condition to resources that never failed.
func resolveCondition(conditions *[]keav1alpha1.ConditionStatus, condType, reason, message string) {
	for _, c := range *conditions {
		if c.Type == condType {
			setCondition(conditions, condType, "False", reason, message)
			return
		}
	}
}
