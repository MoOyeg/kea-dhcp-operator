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

func TestCtrlAgentMinimalConfig(t *testing.T) {
	httpPort := int32(8000)
	spec := &keav1alpha1.KeaControlAgentSpec{
		HTTPHost: "0.0.0.0",
		HTTPPort: &httpPort,
	}
	renderer := NewCtrlAgentConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Must have "Control-agent" root key
	agent, ok := root["Control-agent"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'Control-agent' root key in JSON output")
	}

	if agent["http-host"] != "0.0.0.0" {
		t.Errorf("expected http-host '0.0.0.0', got %v", agent["http-host"])
	}
	if int32(agent["http-port"].(float64)) != 8000 {
		t.Errorf("expected http-port 8000, got %v", agent["http-port"])
	}

	// Should not have optional sections
	for _, key := range []string{"control-sockets", "authentication", "hooks-libraries"} {
		if _, ok := agent[key]; ok {
			t.Errorf("expected key %q not to be set in minimal config", key)
		}
	}
}

func TestCtrlAgentWithControlSockets(t *testing.T) {
	httpPort := int32(8000)
	d2Port := int32(8003)
	spec := &keav1alpha1.KeaControlAgentSpec{
		HTTPHost: "127.0.0.1",
		HTTPPort: &httpPort,
		ControlSockets: &keav1alpha1.AgentControlSockets{
			Dhcp4: &keav1alpha1.ControlSocket{
				SocketType: "unix",
				SocketName: "/run/kea/kea4-ctrl-socket",
			},
			Dhcp6: &keav1alpha1.ControlSocket{
				SocketType: "unix",
				SocketName: "/run/kea/kea6-ctrl-socket",
			},
			D2: &keav1alpha1.ControlSocket{
				SocketType: "http",
				SocketPort: &d2Port,
			},
		},
	}

	renderer := NewCtrlAgentConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	agent := root["Control-agent"].(map[string]interface{})
	sockets, ok := agent["control-sockets"].(map[string]interface{})
	if !ok {
		t.Fatal("expected control-sockets in config")
	}

	// Check dhcp4 socket
	dhcp4Sock, ok := sockets["dhcp4"].(map[string]interface{})
	if !ok {
		t.Fatal("expected dhcp4 control socket")
	}
	if dhcp4Sock["socket-type"] != "unix" {
		t.Errorf("expected dhcp4 socket-type 'unix', got %v", dhcp4Sock["socket-type"])
	}
	if dhcp4Sock["socket-name"] != "/run/kea/kea4-ctrl-socket" {
		t.Errorf("expected dhcp4 socket-name '/run/kea/kea4-ctrl-socket', got %v", dhcp4Sock["socket-name"])
	}

	// Check dhcp6 socket
	dhcp6Sock, ok := sockets["dhcp6"].(map[string]interface{})
	if !ok {
		t.Fatal("expected dhcp6 control socket")
	}
	if dhcp6Sock["socket-type"] != "unix" {
		t.Errorf("expected dhcp6 socket-type 'unix', got %v", dhcp6Sock["socket-type"])
	}

	// Check d2 socket
	d2Sock, ok := sockets["d2"].(map[string]interface{})
	if !ok {
		t.Fatal("expected d2 control socket")
	}
	if d2Sock["socket-type"] != "http" {
		t.Errorf("expected d2 socket-type 'http', got %v", d2Sock["socket-type"])
	}
	if int32(d2Sock["socket-port"].(float64)) != 8003 {
		t.Errorf("expected d2 socket-port 8003, got %v", d2Sock["socket-port"])
	}
}

func TestCtrlAgentWithAuth(t *testing.T) {
	httpPort := int32(8000)
	spec := &keav1alpha1.KeaControlAgentSpec{
		HTTPHost: "0.0.0.0",
		HTTPPort: &httpPort,
		Authentication: &keav1alpha1.AuthConfig{
			Type:  "basic",
			Realm: "kea-control-agent",
			Clients: []keav1alpha1.AuthClient{
				{
					User: "admin",
				},
				{
					User: "readonly",
				},
			},
		},
	}

	renderer := NewCtrlAgentConfigRenderer(spec)
	renderer.ResolvedAuthPasswords = map[string]string{
		"admin":    "admin-password",
		"readonly": "readonly-password",
	}
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	agent := root["Control-agent"].(map[string]interface{})
	auth, ok := agent["authentication"].(map[string]interface{})
	if !ok {
		t.Fatal("expected authentication in config")
	}

	if auth["type"] != "basic" {
		t.Errorf("expected auth type 'basic', got %v", auth["type"])
	}
	if auth["realm"] != "kea-control-agent" {
		t.Errorf("expected realm 'kea-control-agent', got %v", auth["realm"])
	}

	clients, ok := auth["clients"].([]interface{})
	if !ok {
		t.Fatal("expected clients array in authentication")
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}

	client0 := clients[0].(map[string]interface{})
	if client0["user"] != "admin" {
		t.Errorf("expected first client user 'admin', got %v", client0["user"])
	}
	if client0["password"] != "admin-password" {
		t.Errorf("expected first client password 'admin-password', got %v", client0["password"])
	}

	client1 := clients[1].(map[string]interface{})
	if client1["user"] != "readonly" {
		t.Errorf("expected second client user 'readonly', got %v", client1["user"])
	}
}
