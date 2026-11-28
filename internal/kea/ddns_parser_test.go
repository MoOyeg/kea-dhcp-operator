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
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

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
