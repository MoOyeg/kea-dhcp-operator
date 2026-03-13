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

func TestDhcp4MinimalConfig(t *testing.T) {
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
	}
	renderer := NewDhcp4ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	// Verify valid JSON
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Must have "Dhcp4" root key
	dhcp4, ok := root["Dhcp4"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'Dhcp4' root key in JSON output")
	}

	// Must have interfaces-config
	ic, ok := dhcp4["interfaces-config"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'interfaces-config' in Dhcp4")
	}
	ifaces, ok := ic["interfaces"].([]interface{})
	if !ok {
		t.Fatal("expected 'interfaces' to be an array")
	}
	if len(ifaces) != 1 || ifaces[0] != "eth0" {
		t.Fatalf("expected interfaces ['eth0'], got %v", ifaces)
	}

	// Should not have optional sections
	for _, key := range []string{"subnet4", "shared-networks", "lease-database", "hooks-libraries"} {
		if _, ok := dhcp4[key]; ok {
			t.Fatalf("expected key %q not to be set in minimal config", key)
		}
	}
}

func TestDhcp4FullConfig(t *testing.T) {
	validLifetime := int32(4000)
	renewTimer := int32(1000)
	rebindTimer := int32(2000)
	authoritative := true
	redetect := false
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces:     []string{"eth0", "eth1"},
			DHCPSocketType: "raw",
			ReDetect:       &redetect,
		},
		ValidLifetime: &validLifetime,
		RenewTimer:    &renewTimer,
		RebindTimer:   &rebindTimer,
		Authoritative: &authoritative,
		ServerTag:     "server1",
		LeaseDatabase: &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMemfile,
		},
		Subnet4: []keav1alpha1.Subnet4{
			{
				ID:     1,
				Subnet: "192.168.1.0/24",
				Pools: []keav1alpha1.Pool4{
					{Pool: "192.168.1.10 - 192.168.1.200"},
				},
			},
		},
		OptionData: []keav1alpha1.OptionData{
			{Name: "routers", Data: "192.168.1.1"},
		},
		Loggers: []keav1alpha1.LoggerConfig{
			{Name: "kea-dhcp4", Severity: "INFO"},
		},
		ControlSocket: &keav1alpha1.ControlSocket{
			SocketType: "unix",
			SocketName: "/run/kea/kea4-ctrl-socket",
		},
	}

	renderer := NewDhcp4ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp4, ok := root["Dhcp4"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'Dhcp4' root key")
	}

	// Verify all expected sections are present
	for _, key := range []string{
		"interfaces-config", "valid-lifetime", "renew-timer", "rebind-timer",
		"authoritative", "server-tag", "lease-database", "subnet4",
		"option-data", "loggers", "control-socket",
	} {
		if _, ok := dhcp4[key]; !ok {
			t.Errorf("expected key %q to be present in full config", key)
		}
	}

	// Check valid-lifetime
	if vl, ok := dhcp4["valid-lifetime"].(float64); !ok || int32(vl) != 4000 {
		t.Errorf("expected valid-lifetime 4000, got %v", dhcp4["valid-lifetime"])
	}

	// Check server-tag
	if dhcp4["server-tag"] != "server1" {
		t.Errorf("expected server-tag 'server1', got %v", dhcp4["server-tag"])
	}

	// Check authoritative
	if dhcp4["authoritative"] != true {
		t.Errorf("expected authoritative true, got %v", dhcp4["authoritative"])
	}
}

func TestDhcp4SubnetsAndPools(t *testing.T) {
	code := int32(3)
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"*"},
		},
		Subnet4: []keav1alpha1.Subnet4{
			{
				ID:     1,
				Subnet: "192.168.1.0/24",
				Pools: []keav1alpha1.Pool4{
					{Pool: "192.168.1.10 - 192.168.1.100"},
					{Pool: "192.168.1.150 - 192.168.1.200"},
				},
				OptionData: []keav1alpha1.OptionData{
					{Name: "routers", Data: "192.168.1.1"},
				},
				Reservations: []keav1alpha1.Reservation{
					{
						HWAddress: "aa:bb:cc:dd:ee:01",
						IPAddress: "192.168.1.50",
						Hostname:  "reserved-host",
					},
				},
			},
			{
				ID:     2,
				Subnet: "10.0.0.0/8",
				Pools: []keav1alpha1.Pool4{
					{Pool: "10.0.0.10 - 10.0.0.200"},
				},
				OptionData: []keav1alpha1.OptionData{
					{Code: &code, Data: "10.0.0.1"},
				},
			},
		},
	}

	renderer := NewDhcp4ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp4 := root["Dhcp4"].(map[string]interface{})
	subnets, ok := dhcp4["subnet4"].([]interface{})
	if !ok {
		t.Fatal("expected subnet4 to be an array")
	}
	if len(subnets) != 2 {
		t.Fatalf("expected 2 subnets, got %d", len(subnets))
	}

	// Check first subnet
	sub1 := subnets[0].(map[string]interface{})
	if int32(sub1["id"].(float64)) != 1 {
		t.Errorf("expected subnet 1 id=1, got %v", sub1["id"])
	}
	if sub1["subnet"] != "192.168.1.0/24" {
		t.Errorf("expected subnet '192.168.1.0/24', got %v", sub1["subnet"])
	}
	pools := sub1["pools"].([]interface{})
	if len(pools) != 2 {
		t.Fatalf("expected 2 pools in subnet 1, got %d", len(pools))
	}

	// Check reservations in first subnet
	reservations, ok := sub1["reservations"].([]interface{})
	if !ok {
		t.Fatal("expected reservations in subnet 1")
	}
	if len(reservations) != 1 {
		t.Fatalf("expected 1 reservation, got %d", len(reservations))
	}
	res1 := reservations[0].(map[string]interface{})
	if res1["hw-address"] != "aa:bb:cc:dd:ee:01" {
		t.Errorf("expected hw-address 'aa:bb:cc:dd:ee:01', got %v", res1["hw-address"])
	}

	// Check second subnet has option with code
	sub2 := subnets[1].(map[string]interface{})
	opts := sub2["option-data"].([]interface{})
	opt := opts[0].(map[string]interface{})
	if int32(opt["code"].(float64)) != 3 {
		t.Errorf("expected option code 3, got %v", opt["code"])
	}
}

func TestDhcp4SharedNetworks(t *testing.T) {
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		SharedNetworks: []keav1alpha1.SharedNetwork4{
			{
				Name:      "office-network",
				Interface: "eth0",
				Subnet4: []keav1alpha1.Subnet4{
					{
						ID:     10,
						Subnet: "192.168.10.0/24",
						Pools: []keav1alpha1.Pool4{
							{Pool: "192.168.10.10 - 192.168.10.200"},
						},
					},
					{
						ID:     11,
						Subnet: "192.168.11.0/24",
						Pools: []keav1alpha1.Pool4{
							{Pool: "192.168.11.10 - 192.168.11.200"},
						},
					},
				},
			},
		},
	}

	renderer := NewDhcp4ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp4 := root["Dhcp4"].(map[string]interface{})
	sn, ok := dhcp4["shared-networks"].([]interface{})
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

	netSubnets, ok := net["subnet4"].([]interface{})
	if !ok {
		t.Fatal("expected subnet4 in shared network")
	}
	if len(netSubnets) != 2 {
		t.Fatalf("expected 2 subnets in shared network, got %d", len(netSubnets))
	}
}

func TestDhcp4WithHA(t *testing.T) {
	heartbeat := int32(10000)
	maxResponse := int32(60000)
	autoFailover := true
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
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

	renderer := NewDhcp4ConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp4 := root["Dhcp4"].(map[string]interface{})
	hooks, ok := dhcp4["hooks-libraries"].([]interface{})
	if !ok {
		t.Fatal("expected hooks-libraries in HA config")
	}
	if len(hooks) != 2 {
		t.Fatalf("expected 2 hooks (lease_cmds and ha), got %d", len(hooks))
	}

	// First hook should be lease_cmds
	hook0 := hooks[0].(map[string]interface{})
	if hook0["library"] != "/usr/lib/kea/hooks/libdhcp_lease_cmds.so" {
		t.Errorf("expected first hook to be lease_cmds, got %v", hook0["library"])
	}

	// Second hook should be ha
	hook1 := hooks[1].(map[string]interface{})
	if hook1["library"] != "/usr/lib/kea/hooks/libdhcp_ha.so" {
		t.Errorf("expected second hook to be ha, got %v", hook1["library"])
	}

	// Verify HA parameters nested in second hook
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

func TestDhcp4WithDatabase(t *testing.T) {
	port := int32(3306)
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{
			Interfaces: []string{"eth0"},
		},
		LeaseDatabase: &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMySQL,
			Name: "kea_leases",
			Host: "db.example.com",
			Port: &port,
		},
	}

	renderer := NewDhcp4ConfigRenderer(spec)
	renderer.LeaseDBCreds = DBCredentials{User: "kea_user", Password: "kea_pass"}
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	dhcp4 := root["Dhcp4"].(map[string]interface{})
	leaseDb, ok := dhcp4["lease-database"].(map[string]interface{})
	if !ok {
		t.Fatal("expected lease-database in config")
	}
	if leaseDb["type"] != "mysql" {
		t.Errorf("expected type 'mysql', got %v", leaseDb["type"])
	}
	if leaseDb["name"] != "kea_leases" {
		t.Errorf("expected name 'kea_leases', got %v", leaseDb["name"])
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
	hooks, ok := dhcp4["hooks-libraries"].([]interface{})
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
