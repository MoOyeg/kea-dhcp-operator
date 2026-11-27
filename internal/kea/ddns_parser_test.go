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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// loadSample reads a CR from config/samples into obj.
func loadSample(t *testing.T, file string, obj interface{}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "samples", file))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, obj); err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
}

// keaParserAccepts runs `<binary> -t` from image on data and fails the test
// if Kea rejects the configuration or logs an error while checking it.
func keaParserAccepts(t *testing.T, k keaContainers, image, binary string, data []byte) {
	t.Helper()
	dir := t.TempDir()
	cfg := binary + ".conf"
	if err := os.WriteFile(filepath.Join(dir, cfg), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The Kea binaries may carry file capabilities, so the container needs them to exec.
	out, err := exec.Command(k.podman, "run", "--rm",
		"--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
		"-v", dir+":/cfg:ro,z", "--entrypoint", "/usr/sbin/"+binary,
		image, "-t", "/cfg/"+cfg).CombinedOutput()
	if err != nil {
		t.Fatalf("%s -t rejected the rendered config: %v\n%s\n--- config ---\n%s", binary, err, out, data)
	}
	if strings.Contains(string(out), "ERROR") {
		t.Fatalf("%s -t logged an error:\n%s\n--- config ---\n%s", binary, out, data)
	}
}

// TestDdnsConfigAcceptedByKea feeds rendered DHCP-DDNS configurations (the
// shipped sample, and TSIG keys with every algorithm form, NCR settings and
// several DNS servers) to `kea-dhcp-ddns -t` in the image the operator
// deploys. It runs with KEA_CONFIG_CHECK=1 (see keaContainerCheck).
func TestDdnsConfigAcceptedByKea(t *testing.T) {
	k := keaContainerCheck(t)
	image := k.image(t, resources.ComponentDdns)

	var sample keav1alpha1.KeaDhcpDdns
	loadSample(t, "kea_v1alpha1_keadhcpddns.yaml", &sample)

	port, timeout, bits, dnsPort := int32(53001), int32(500), int32(128), int32(53)
	full := &keav1alpha1.KeaDhcpDdnsSpec{
		IPAddress:        testLoopbackIP,
		Port:             &port,
		DNSServerTimeout: &timeout,
		NCRProtocol:      testNCRProtocolUDP,
		NCRFormat:        testNCRFormatJSON,
		TSIGKeys: []keav1alpha1.TSIGKey{
			{Name: testTSIGKeyMD5, Algorithm: testTSIGAlgMD5, DigestBits: &bits},
			{Name: testTSIGKeySHA256, Algorithm: "HMAC-SHA256"},
		},
		ForwardDDNS: &keav1alpha1.DDNSConfig{DDNSDomains: []keav1alpha1.DDNSDomain{
			{Name: testForwardZone, KeyName: testTSIGKeySHA256, DNSServers: []keav1alpha1.DNSServer{
				{IPAddress: testDNSServerIP, Port: &dnsPort}, {IPAddress: "10.0.0.54"},
			}},
			{Name: "internal.example.com.", DNSServers: []keav1alpha1.DNSServer{{IPAddress: "10.0.0.55"}}},
		}},
		ReverseDDNS: &keav1alpha1.DDNSConfig{DDNSDomains: []keav1alpha1.DDNSDomain{
			{Name: "0.0.10.in-addr.arpa.", KeyName: testTSIGKeyMD5, DNSServers: []keav1alpha1.DNSServer{{IPAddress: testDNSServerIP}}},
		}},
	}

	cases := []struct {
		name    string
		spec    *keav1alpha1.KeaDhcpDdnsSpec
		secrets map[string]string
	}{
		{"sample", &sample.Spec, map[string]string{"ddns-key": testTSIGSecretSHA256}},
		{"TSIG algorithms, NCR settings and several servers", full, map[string]string{
			testTSIGKeyMD5: testTSIGSecretMD5, testTSIGKeySHA256: testTSIGSecretSHA256,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewDdnsConfigRenderer(tc.spec)
			r.ResolvedTSIGSecrets = tc.secrets
			data, err := r.RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			keaParserAccepts(t, k, image, "kea-dhcp-ddns", data)
		})
	}
}
