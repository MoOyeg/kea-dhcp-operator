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
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// ctrlAgentFixture builds testdata/kea-ctrl-agent-fixture on top of the
// kea-dhcp4 image the tests check (pinned by digest) and returns its
// reference. This is fixture coverage of the renderer: ISC publishes no
// kea-ctrl-agent image, so there is no shipped image to check. Kea 3.2
// removed the Control Agent, so the test skips for KEA_IMAGE_TAG >= 3.2.
func ctrlAgentFixture(t *testing.T, k keaContainers) string {
	t.Helper()
	base := k.image(t, resources.ComponentDhcp4)
	if m := regexp.MustCompile(`:(\d+)\.(\d+)[^:/]*$`).FindStringSubmatch(base); m != nil {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		if major > 3 || (major == 3 && minor >= 2) {
			t.Skipf("%s: ISC removed the Kea Control Agent in Kea 3.2", base)
		}
	}
	from := pinned(base)
	tag := "localhost/kea-ctrl-agent-fixture:" + from[strings.LastIndex(from, ":")+1:][:12]
	if out, err := exec.Command(k.podman, "build", "-q", "--pull=missing", "--build-arg", "BASE="+from,
		"-t", tag, "testdata/kea-ctrl-agent-fixture").CombinedOutput(); err != nil {
		t.Fatalf("building the Control Agent fixture from %s: %v\n%s", from, err, out)
	}
	version, err := exec.Command(k.podman, "run", "--rm", "--entrypoint", "/usr/sbin/kea-ctrl-agent", tag, "-V").Output()
	if err != nil {
		t.Fatalf("running the Control Agent fixture: %v", err)
	}
	t.Logf("Kea Control Agent test fixture (not a shipped image): %s = %s + isc-kea-ctrl-agent %s",
		tag, from, strings.SplitN(strings.TrimSpace(string(version)), "\n", 2)[0])
	return tag
}

// TestCtrlAgentConfigAcceptedByKea feeds rendered Control Agent configurations
// (the shipped sample, and every option the renderer emits apart from TLS,
// whose files the parser would open) to `kea-ctrl-agent -t` from the test
// fixture. It runs with KEA_CONFIG_CHECK=1 (see keaContainerCheck).
func TestCtrlAgentConfigAcceptedByKea(t *testing.T) {
	k := keaContainerCheck(t)
	image := ctrlAgentFixture(t, k)

	var sample keav1alpha1.KeaControlAgent
	loadSample(t, "kea_v1alpha1_keacontrolagent.yaml", &sample)

	port, debug := int32(8000), int32(10)
	full := &keav1alpha1.KeaControlAgentSpec{
		HTTPHost: listenAnyIP4,
		HTTPPort: &port,
		ControlSockets: &keav1alpha1.AgentControlSockets{
			Dhcp4: &keav1alpha1.ControlSocket{SocketType: testSocketTypeUnix, SocketName: "/var/run/kea/kea4-ctrl-socket"},
			Dhcp6: &keav1alpha1.ControlSocket{SocketType: testSocketTypeUnix, SocketName: "/var/run/kea/kea6-ctrl-socket"},
			D2:    &keav1alpha1.ControlSocket{SocketType: testSocketTypeUnix, SocketName: "/var/run/kea/kea-ddns-ctrl-socket"},
		},
		Authentication: &keav1alpha1.AuthConfig{
			Type:    testAuthBasic,
			Realm:   testAuthRealm,
			Clients: []keav1alpha1.AuthClient{{User: testUserAdmin}, {User: testRoleReadonly}},
		},
		Loggers: []keav1alpha1.LoggerConfig{{
			Name: "kea-ctrl-agent", Severity: testSeverityDebug, DebugLevel: &debug,
			OutputOptions: []keav1alpha1.LogOutputOption{{Output: testOutputStdout}},
		}},
	}

	cases := []struct {
		name      string
		spec      *keav1alpha1.KeaControlAgentSpec
		passwords map[string]string
	}{
		{"sample", &sample.Spec, nil},
		{"unix sockets, basic auth and loggers", full, map[string]string{
			testUserAdmin: testAdminPassword, testRoleReadonly: `pass"word\with:punctuation`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewCtrlAgentConfigRenderer(tc.spec)
			r.ResolvedAuthPasswords = tc.passwords
			data, err := r.RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			keaParserAccepts(t, k, image, "kea-ctrl-agent", data)
		})
	}
}

// TestDaemonHTTPControlSocketsAcceptedByKea checks the replacement for the
// Control Agent: since Kea 3.0 each daemon can serve its own HTTP control
// socket. The rendered dhcp4, dhcp6 and DHCP-DDNS configurations with an HTTP
// control socket must pass the shipped parsers.
func TestDaemonHTTPControlSocketsAcceptedByKea(t *testing.T) {
	k := keaContainerCheck(t)
	http := func(port int32) *keav1alpha1.ControlSocket {
		return &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketAddress: listenAnyIP4, SocketPort: &port}
	}
	iface := keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}}
	cases := []struct {
		component, binary string
		renderer          ConfigRenderer
	}{
		{resources.ComponentDhcp4, "kea-dhcp4",
			NewDhcp4ConfigRenderer(&keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, ControlSocket: http(8001)})},
		{resources.ComponentDhcp6, testNameDhcp6,
			NewDhcp6ConfigRenderer(&keav1alpha1.KeaDhcp6ServerSpec{InterfacesConfig: iface, ControlSocket: http(8002)})},
		{resources.ComponentDdns, "kea-dhcp-ddns",
			NewDdnsConfigRenderer(&keav1alpha1.KeaDhcpDdnsSpec{IPAddress: testLoopbackIP, ControlSocket: http(8003)})},
	}
	for _, tc := range cases {
		t.Run(tc.binary, func(t *testing.T) {
			data, err := tc.renderer.RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			keaParserAccepts(t, k, k.image(t, tc.component), tc.binary, data)
		})
	}
}
