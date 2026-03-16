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

func TestDhcp6FullConfig(t *testing.T) {
	validLifetime := int32(4000)
	preferredLifetime := int32(3000)
	renewTimer := int32(1000)
	rebindTimer := int32(2000)
	rapidCommit := true
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0", "eth1"},
		},
		ValidLifetime:     &validLifetime,
		PreferredLifetime: &preferredLifetime,
		RenewTimer:        &renewTimer,
		RebindTimer:       &rebindTimer,
		RapidCommit:       &rapidCommit,
		ServerTag:         "server1",
		LeaseDatabase: &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMemfile,
		},
		Subnet6: []keav1alpha1.Subnet6{
			{
				ID:     1,
				Subnet: "2001:db8:1::/64",
				Pools: []keav1alpha1.Pool6{
					{Pool: "2001:db8:1::100 - 2001:db8:1::1ff"},
				},
			},
		},
		OptionData: []keav1alpha1.OptionData{
			{Name: "dns-recursive-name-server", Data: "2001:db8:1::dead:beef"},
		},
		Loggers: []keav1alpha1.LoggerConfig{
			{Name: "kea-dhcp6", Severity: "INFO"},
		},
		ControlSocket: &keav1alpha1.ControlSocket{
			SocketType: "unix",
			SocketName: "/run/kea/kea6-ctrl-socket",
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

	dhcp6, ok := root["Dhcp6"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'Dhcp6' root key")
	}

	for _, key := range []string{
		"interfaces-config", "valid-lifetime", "preferred-lifetime", "renew-timer",
		"rebind-timer", "rapid-commit", "server-tag", "lease-database", "subnet6",
		"option-data", "loggers", "control-socket",
	} {
		if _, ok := dhcp6[key]; !ok {
			t.Errorf("expected key %q to be present in full config", key)
		}
	}

	if vl, ok := dhcp6["valid-lifetime"].(float64); !ok || int32(vl) != 4000 {
		t.Errorf("expected valid-lifetime 4000, got %v", dhcp6["valid-lifetime"])
	}
	if dhcp6["server-tag"] != "server1" {
		t.Errorf("expected server-tag 'server1', got %v", dhcp6["server-tag"])
	}
	if dhcp6["rapid-commit"] != true {
		t.Errorf("expected rapid-commit true, got %v", dhcp6["rapid-commit"])
	}
}

func TestDhcp6SharedNetworks(t *testing.T) {
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		SharedNetworks: []keav1alpha1.SharedNetwork6{
			{
				Name:      "office-network",
				Interface: "eth0",
				Subnet6: []keav1alpha1.Subnet6{
					{
						ID:     10,
						Subnet: "2001:db8:10::/64",
						Pools: []keav1alpha1.Pool6{
							{Pool: "2001:db8:10::100 - 2001:db8:10::1ff"},
						},
					},
					{
						ID:     11,
						Subnet: "2001:db8:11::/64",
						Pools: []keav1alpha1.Pool6{
							{Pool: "2001:db8:11::100 - 2001:db8:11::1ff"},
						},
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
	sn, ok := dhcp6["shared-networks"].([]interface{})
	if !ok {
		t.Fatal("expected shared-networks to be an array")
	}
	if len(sn) != 1 {
		t.Fatalf("expected 1 shared network, got %d", len(sn))
	}

	net := sn[0].(map[string]interface{})
	if net["name"] != "office-network" {
		t.Errorf("expected name 'office-network', got %v", net["name"])
	}
	if net["interface"] != "eth0" {
		t.Errorf("expected interface 'eth0', got %v", net["interface"])
	}

	netSubnets, ok := net["subnet6"].([]interface{})
	if !ok {
		t.Fatal("expected subnet6 in shared network")
	}
	if len(netSubnets) != 2 {
		t.Fatalf("expected 2 subnets in shared network, got %d", len(netSubnets))
	}
}

func TestDhcp6WithHA(t *testing.T) {
	heartbeat := int32(10000)
	maxResponse := int32(60000)
	autoFailover := true
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		HighAvailability: &keav1alpha1.HAConfig{
			ThisServerName:   "server1",
			Mode:             "load-balancing",
			HeartbeatDelay:   &heartbeat,
			MaxResponseDelay: &maxResponse,
			Peers: []keav1alpha1.HAPeer{
				{
					Name:         "server1",
					URL:          "http://server1:8000/",
					Role:         "primary",
					AutoFailover: &autoFailover,
				},
				{
					Name:         "server2",
					URL:          "http://server2:8000/",
					Role:         "standby",
					AutoFailover: &autoFailover,
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
	hooks, ok := dhcp6["hooks-libraries"].([]interface{})
	if !ok {
		t.Fatal("expected hooks-libraries in HA config")
	}
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks (lease_cmds and ha), got %d", len(hooks))
	}

	hook0 := hooks[0].(map[string]interface{})
	if hook0["library"] != "/usr/lib/kea/hooks/libdhcp_lease_cmds.so" {
		t.Errorf("expected first hook to be lease_cmds, got %v", hook0["library"])
	}

	hook1 := hooks[1].(map[string]interface{})
	if hook1["library"] != "/usr/lib/kea/hooks/libdhcp_ha.so" {
		t.Errorf("expected second hook to be ha, got %v", hook1["library"])
	}

	params, ok := hook1["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters in ha hook")
	}
	haArray, ok := params["high-availability"].([]interface{})
	if !ok {
		t.Fatal("expected high-availability array in parameters")
	}
	if len(haArray) != 1 {
		t.Fatalf("expected 1 HA config entry, got %d", len(haArray))
	}
	haConfig := haArray[0].(map[string]interface{})
	if haConfig["this-server-name"] != "server1" {
		t.Errorf("expected this-server-name 'server1', got %v", haConfig["this-server-name"])
	}
	if haConfig["mode"] != "load-balancing" {
		t.Errorf("expected mode 'load-balancing', got %v", haConfig["mode"])
	}

	peers, ok := haConfig["peers"].([]interface{})
	if !ok {
		t.Fatal("expected peers array in HA config")
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
}

func TestDhcp6WithServerNameOverride(t *testing.T) {
	heartbeat := int32(10000)
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		HighAvailability: &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Mode:           "hot-standby",
			HeartbeatDelay: &heartbeat,
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", URL: "http://server1:8000/", Role: "primary"},
				{Name: "server2", URL: "http://server2:8000/", Role: "standby"},
			},
		},
	}

	renderer := NewDhcp6ConfigRenderer(spec)
	data, err := renderer.RenderJSONWithServerName("server2")
	if err != nil {
		t.Fatalf("RenderJSONWithServerName failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp6 := root["Dhcp6"].(map[string]interface{})
	hooks := dhcp6["hooks-libraries"].([]interface{})
	haHook := hooks[1].(map[string]interface{})
	params := haHook["parameters"].(map[string]interface{})
	haArray := params["high-availability"].([]interface{})
	haConfig := haArray[0].(map[string]interface{})

	if haConfig["this-server-name"] != "server2" {
		t.Errorf("expected this-server-name override 'server2', got %v", haConfig["this-server-name"])
	}
}

func TestDhcp6WithDatabase(t *testing.T) {
	port := int32(3306)
	spec := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		LeaseDatabase: &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMySQL,
			Name: "kea_leases6",
			Host: "db.example.com",
			Port: &port,
		},
	}

	renderer := NewDhcp6ConfigRenderer(spec)
	renderer.LeaseDBCreds = DBCredentials{User: "kea_user", Password: "kea_pass"}
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp6 := root["Dhcp6"].(map[string]interface{})
	leaseDb, ok := dhcp6["lease-database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected lease-database in config")
	}
	if leaseDb["type"] != "mysql" {
		t.Errorf("expected type 'mysql', got %v", leaseDb["type"])
	}
	if leaseDb["name"] != "kea_leases6" {
		t.Errorf("expected name 'kea_leases6', got %v", leaseDb["name"])
	}
	if leaseDb["host"] != "db.example.com" {
		t.Errorf("expected host 'db.example.com', got %v", leaseDb["host"])
	}
	if int32(leaseDb["port"].(float64)) != 3306 {
		t.Errorf("expected port 3306, got %v", leaseDb["port"])
	}
	if leaseDb["user"] != "kea_user" {
		t.Errorf("expected user 'kea_user', got %v", leaseDb["user"])
	}

	// Verify the MySQL hook library is automatically injected
	hooks, ok := dhcp6["hooks-libraries"].([]interface{})
	if !ok {
		t.Fatal("expected hooks-libraries when mysql lease-database is configured")
	}
	found := false
	for _, h := range hooks {
		hm := h.(map[string]interface{})
		if hm["library"] == "/usr/lib/kea/hooks/libdhcp_mysql.so" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected libdhcp_mysql.so in hooks-libraries for mysql lease database")
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
