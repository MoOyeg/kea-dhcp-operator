/*
Copyright 2026.

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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

func TestHAListenerPort(t *testing.T) {
	p := func(v int32) *int32 { return &v }
	ha := func(port *int32) *keav1alpha1.HAConfig { return &keav1alpha1.HAConfig{HTTPListenerPort: port} }
	unix := func(port *int32) *keav1alpha1.ControlSocket {
		return &keav1alpha1.ControlSocket{SocketType: testSocketTypeUnix, SocketName: testCtrlSocket4, SocketPort: port}
	}
	http := func(port *int32) *keav1alpha1.ControlSocket {
		return &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketPort: port}
	}
	any4 := func(port *int32) *keav1alpha1.ControlSocket {
		return &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketAddress: listenAnyIP4, SocketPort: port}
	}
	cases := []struct {
		name     string
		ha       *keav1alpha1.HAConfig
		cs       *keav1alpha1.ControlSocket
		port     int32
		explicit bool
		conflict bool
	}{
		{"nothing set", ha(nil), nil, 8000, false, false},
		{"UNIX socket without port", ha(nil), unix(nil), 8000, false, false},
		{"socket-port keeps choosing it (UNIX)", ha(nil), unix(p(8080)), 8080, true, false},
		{"socket-port keeps choosing it (HTTP, loopback)", ha(nil), http(p(8000)), 8000, true, false},
		{"http-listener-port wins over socket-port", ha(p(8100)), unix(p(8080)), 8100, true, false},
		{"HTTP socket without port, loopback", ha(nil), http(nil), 8000, false, false},
		{"HTTP socket on 0.0.0.0 and the same port", ha(nil), any4(p(8001)), 8001, true, true},
		{"HTTP socket on 0.0.0.0 without port (Kea: 8000)", ha(nil), any4(nil), 8000, false, true},
		{"HTTP socket on the pod address and the same port", ha(nil), &keav1alpha1.ControlSocket{
			SocketType: testSocketTypeHTTP, SocketAddress: "10.128.0.5", SocketPort: p(8000)}, 8000, true, true},
		{"HTTP socket on 0.0.0.0 and http-listener-port apart", ha(p(8100)), any4(p(8001)), 8100, true, false},
		{"HTTP socket on 0.0.0.0 and http-listener-port equal", ha(p(8001)), any4(p(8001)), 8001, true, true},
		{"HTTP socket on ::1 and the same port", ha(nil), &keav1alpha1.ControlSocket{
			SocketType: testSocketTypeHTTP, SocketAddress: "::1", SocketPort: p(8000)}, 8000, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port, explicit := HAListenerPort(tc.ha, tc.cs)
			if port != tc.port || explicit != tc.explicit {
				t.Errorf("HAListenerPort = %d, %v; want %d, %v", port, explicit, tc.port, tc.explicit)
			}
			err := checkHAListenerPort(tc.ha, tc.cs)
			if (err != nil) != tc.conflict {
				t.Errorf("checkHAListenerPort = %v, want conflict %v", err, tc.conflict)
			}
			if err != nil && !strings.Contains(err.Error(), "http-listener-port") {
				t.Errorf("error %q does not name the field to set", err)
			}
		})
	}
}

// The rendered HA hook must not change for servers that existed before
// http-listener-port: an unchanged config hash means no rollout on upgrade.
func TestHAHookUnchangedWithoutListenerPort(t *testing.T) {
	port := int32(8080)
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}},
		ControlSocket:    &keav1alpha1.ControlSocket{SocketType: testSocketTypeUnix, SocketName: testCtrlSocket4, SocketPort: &port},
		HighAvailability: &keav1alpha1.HAConfig{ThisServerName: testServerName1, Peers: []keav1alpha1.HAPeer{
			{Name: testServerName1, URL: "http://a:8080/", Role: testRolePrimary},
			{Name: testServerName2, URL: "http://b:8080/", Role: testRoleSecondary},
		}},
	}
	haParams := func(spec *keav1alpha1.KeaDhcp4ServerSpec) map[string]interface{} {
		t.Helper()
		data, err := NewDhcp4ConfigRenderer(spec).RenderJSON()
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Dhcp4 struct {
				Hooks []struct {
					Parameters struct {
						HA []map[string]interface{} `json:"high-availability"`
					} `json:"parameters"`
				} `json:"hooks-libraries"`
			} `json:"Dhcp4"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}
		for _, h := range cfg.Dhcp4.Hooks {
			if len(h.Parameters.HA) == 1 {
				return h.Parameters.HA[0]
			}
		}
		t.Fatal("no HA hook rendered")
		return nil
	}
	got := haParams(spec)
	if got["http-listener-port"] != float64(8080) || got["http-dedicated-listener"] != true {
		t.Errorf("UNIX socket-port 8080: HA hook = %v, want the dedicated listener on 8080 as before", got)
	}
	spec.ControlSocket.SocketPort = nil
	if got := haParams(spec); got["http-listener-port"] != nil || got["http-dedicated-listener"] != nil {
		t.Errorf("no port set: HA hook = %v, want no listener keys (Kea uses the peer URL) as before", got)
	}
}

// TestHAPeersWithHTTPControlSocketsInKea runs two kea-dhcp4 HA peers with
// HTTP control sockets on a podman network and checks that both start, that
// they reach each other (load-balancing needs heartbeats both ways, after a
// lease sync), and that the control socket answers commands directly:
//   - v0.0.36's layout: the control socket on loopback (Kea's default) and
//     socket-port shared with the HA listener. Clients must be in the pod.
//   - the control socket on 0.0.0.0, the HA listener on http-listener-port.
//     A client in the other container reaches the control socket.
//
// It runs with KEA_CONFIG_CHECK=1 (see keaContainerCheck).
func TestHAPeersWithHTTPControlSocketsInKea(t *testing.T) {
	k := keaContainerCheck(t)
	image := k.image(t, resources.ComponentDhcp4)
	podman := func(args ...string) (string, error) {
		out, err := exec.Command(k.podman, args...).CombinedOutput()
		return string(out), err
	}
	p := func(v int32) *int32 { return &v }

	cases := []struct {
		name, net, address string
		csPort             int32
		listener           *int32
		fromPeer           bool // query server1's control socket from server2's container
	}{
		{"v0.0.36 loopback control socket sharing socket-port", "10.89.177", "", 8000, nil, false},
		{"control socket on 0.0.0.0, http-listener-port apart", "10.89.178", listenAnyIP4, 8001, p(8100), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			network := fmt.Sprintf("kea-ha-test-%d", time.Now().UnixNano()%1_000_000)
			subnet := tc.net + ".0/24"
			ips := map[string]string{testServerName1: tc.net + ".10", testServerName2: tc.net + ".11"}
			if out, err := podman("network", "create", "--subnet", subnet, network); err != nil {
				t.Fatalf("creating network: %v\n%s", err, out)
			}
			t.Cleanup(func() { _, _ = podman("network", "rm", "-f", network) })

			cs := &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketAddress: tc.address, SocketPort: p(tc.csPort)}
			haPort, _ := HAListenerPort(&keav1alpha1.HAConfig{HTTPListenerPort: tc.listener}, cs)
			hb, mrd, sync := int32(1000), int32(3000), int32(5000)
			for _, name := range []string{testServerName1, testServerName2} {
				spec := &keav1alpha1.KeaDhcp4ServerSpec{
					InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}, DHCPSocketType: "udp"},
					ControlSocket:    cs,
					Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: subnet,
						Pools: []keav1alpha1.Pool4{{Pool: tc.net + ".100 - " + tc.net + ".200"}}}},
					HighAvailability: &keav1alpha1.HAConfig{
						ThisServerName: name, Mode: testHAModeLB, HTTPListenerPort: tc.listener,
						HeartbeatDelay: &hb, MaxResponseDelay: &mrd, SyncTimeout: &sync,
						Peers: []keav1alpha1.HAPeer{
							{Name: testServerName1, URL: fmt.Sprintf("http://%s:%d/", ips[testServerName1], haPort), Role: testRolePrimary},
							{Name: testServerName2, URL: fmt.Sprintf("http://%s:%d/", ips[testServerName2], haPort), Role: testRoleSecondary},
						},
					},
				}
				data, err := NewDhcp4ConfigRenderer(spec).RenderJSON()
				if err != nil {
					t.Fatalf("RenderJSON: %v", err)
				}
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "kea-dhcp4.conf"), data, 0o644); err != nil {
					t.Fatal(err)
				}
				container := network + "-" + name
				if out, err := podman("run", "-d", "--name", container, "--network", network, "--ip", ips[name],
					"--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
					"-v", dir+":/cfg:ro,z", "--entrypoint", "/usr/sbin/kea-dhcp4",
					image, "-c", "/cfg/kea-dhcp4.conf"); err != nil {
					t.Fatalf("starting %s: %v\n%s", name, err, out)
				}
				t.Cleanup(func() {
					if t.Failed() {
						logs, _ := podman("logs", container)
						t.Logf("--- %s logs ---\n%s", name, logs)
					}
					_, _ = podman("rm", "-f", container)
				})
			}

			// command sends a Kea command to a daemon's HTTP control socket
			// with busybox wget, from the given container, as an API client would.
			command := func(from, host, cmd string) (map[string]interface{}, error) {
				out, err := podman("exec", network+"-"+from, "wget", "-qO-", "--header", "Content-Type: application/json",
					"--post-data", fmt.Sprintf(`{"command": %q}`, cmd), fmt.Sprintf("http://%s:%d/", host, tc.csPort))
				if err != nil {
					return nil, fmt.Errorf("%v: %s", err, out)
				}
				var resp []map[string]interface{}
				if err := json.Unmarshal([]byte(out), &resp); err != nil || len(resp) != 1 {
					return nil, fmt.Errorf("unexpected response %q: %v", out, err)
				}
				return resp[0], nil
			}

			deadline := time.Now().Add(90 * time.Second)
			states := map[string]string{}
			for time.Now().Before(deadline) {
				for _, name := range []string{testServerName1, testServerName2} {
					resp, err := command(name, testLoopbackIP, "ha-heartbeat")
					if err != nil {
						states[name] = err.Error()
						continue
					}
					args, _ := resp["arguments"].(map[string]interface{})
					states[name], _ = args["state"].(string)
				}
				if states[testServerName1] == testHAModeLB && states[testServerName2] == testHAModeLB {
					break
				}
				time.Sleep(2 * time.Second)
			}
			if states[testServerName1] != testHAModeLB || states[testServerName2] != testHAModeLB {
				t.Fatalf("HA states after 90s: %v, want both load-balancing", states)
			}

			from, host := testServerName1, testLoopbackIP
			if tc.fromPeer {
				from, host = testServerName2, ips[testServerName1]
			}
			resp, err := command(from, host, "version-get")
			if err != nil || resp["result"] != float64(0) {
				t.Fatalf("version-get from %s to %s:%d: %v, %v", from, host, tc.csPort, resp, err)
			}
			t.Logf("HA peers in load-balancing; control socket %s:%d (queried from %s), HA listener :%d; %v",
				host, tc.csPort, from, haPort, resp["text"])
		})
	}
}
