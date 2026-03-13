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

package kea

import (
	"encoding/json"
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

func TestDhcp6MinimalConfig(t *testing.T) {
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
	}
	renderer := NewDhcp6ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Must have "Dhcp6" root key
	dhcp6, ok := root["Dhcp6"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'Dhcp6' root key in JSON output")
	}

	// Must have interfaces-config
	ic, ok := dhcp6["interfaces-config"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'interfaces-config' in Dhcp6")
	}
	ifaces, ok := ic["interfaces"].([]interface{})
	if !ok {
		t.Fatal("expected 'interfaces' to be an array")
	}
	if len(ifaces) != 1 || ifaces[0] != "eth0" {
		t.Fatalf("expected interfaces ['eth0'], got %v", ifaces)
	}

	// Should not have optional sections
	for _, key := range []string{"subnet6", "shared-networks", "lease-database", "hooks-libraries", "preferred-lifetime"} {
		if _, ok := dhcp6[key]; ok {
			t.Fatalf("expected key %q not to be set in minimal config", key)
		}
	}
}

func TestDhcp6WithPDPools(t *testing.T) {
	excludedPrefixLen := int32(64)
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		Subnet6: []keav1alpha1.Subnet6{
			{
				ID:     1,
				Subnet: "2001:db8:1::/48",
				Pools: []keav1alpha1.Pool6{
					{Pool: "2001:db8:1::1 - 2001:db8:1::ffff"},
				},
				PDPools: []keav1alpha1.PDPool{
					{
						Prefix:            "2001:db8:8::",
						PrefixLen:         48,
						DelegatedLen:      64,
						ExcludedPrefix:    "2001:db8:8:0:80::",
						ExcludedPrefixLen: &excludedPrefixLen,
					},
				},
			},
		},
	}

	renderer := NewDhcp6ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp6 := root["Dhcp6"].(map[string]interface{})
	subnets, ok := dhcp6["subnet6"].([]interface{})
	if !ok {
		t.Fatal("expected subnet6 to be an array")
	}
	if len(subnets) != 1 {
		t.Fatalf("expected 1 subnet, got %d", len(subnets))
	}

	sub := subnets[0].(map[string]interface{})

	// Check address pools
	pools, ok := sub["pools"].([]interface{})
	if !ok {
		t.Fatal("expected pools in subnet")
	}
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(pools))
	}

	// Check PD pools
	pdPools, ok := sub["pd-pools"].([]interface{})
	if !ok {
		t.Fatal("expected pd-pools in subnet")
	}
	if len(pdPools) != 1 {
		t.Fatalf("expected 1 pd-pool, got %d", len(pdPools))
	}

	pd := pdPools[0].(map[string]interface{})
	if pd["prefix"] != "2001:db8:8::" {
		t.Errorf("expected prefix '2001:db8:8::', got %v", pd["prefix"])
	}
	if int32(pd["prefix-len"].(float64)) != 48 {
		t.Errorf("expected prefix-len 48, got %v", pd["prefix-len"])
	}
	if int32(pd["delegated-len"].(float64)) != 64 {
		t.Errorf("expected delegated-len 64, got %v", pd["delegated-len"])
	}
	if pd["excluded-prefix"] != "2001:db8:8:0:80::" {
		t.Errorf("expected excluded-prefix '2001:db8:8:0:80::', got %v", pd["excluded-prefix"])
	}
	if int32(pd["excluded-prefix-len"].(float64)) != 64 {
		t.Errorf("expected excluded-prefix-len 64, got %v", pd["excluded-prefix-len"])
	}
}

func TestDhcp6WithPreferredLifetime(t *testing.T) {
	preferredLifetime := int32(3000)
	validLifetime := int32(4000)
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		PreferredLifetime: &preferredLifetime,
		ValidLifetime:     &validLifetime,
	}

	renderer := NewDhcp6ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp6 := root["Dhcp6"].(map[string]interface{})

	if pl, ok := dhcp6["preferred-lifetime"].(float64); !ok || int32(pl) != 3000 {
		t.Errorf("expected preferred-lifetime 3000, got %v", dhcp6["preferred-lifetime"])
	}
	if vl, ok := dhcp6["valid-lifetime"].(float64); !ok || int32(vl) != 4000 {
		t.Errorf("expected valid-lifetime 4000, got %v", dhcp6["valid-lifetime"])
	}
}
