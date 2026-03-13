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

func TestDdnsMinimalConfig(t *testing.T) {
	spec := &keav1alpha1.KeaDhcpDdnsSpec{
		IPAddress: "127.0.0.1",
	}
	renderer := NewDdnsConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Must have "DhcpDdns" root key
	ddns, ok := root["DhcpDdns"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'DhcpDdns' root key in JSON output")
	}

	if ddns["ip-address"] != "127.0.0.1" {
		t.Errorf("expected ip-address '127.0.0.1', got %v", ddns["ip-address"])
	}

	// Should not have optional sections
	for _, key := range []string{"forward-ddns", "reverse-ddns", "tsig-keys", "hooks-libraries"} {
		if _, ok := ddns[key]; ok {
			t.Errorf("expected key %q not to be set in minimal config", key)
		}
	}
}

func TestDdnsWithForwardDDNS(t *testing.T) {
	dnsPort := int32(53)
	spec := &keav1alpha1.KeaDhcpDdnsSpec{
		IPAddress: "127.0.0.1",
		ForwardDDNS: &keav1alpha1.DDNSConfig{
			DDNSDomains: []keav1alpha1.DDNSDomain{
				{
					Name:    "example.com.",
					KeyName: "my-tsig-key",
					DNSServers: []keav1alpha1.DNSServer{
						{
							IPAddress: "10.0.0.53",
							Port:      &dnsPort,
						},
						{
							IPAddress: "10.0.0.54",
						},
					},
				},
				{
					Name: "internal.example.com.",
					DNSServers: []keav1alpha1.DNSServer{
						{
							IPAddress: "10.0.0.55",
						},
					},
				},
			},
		},
	}

	renderer := NewDdnsConfigRenderer(spec)
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	ddns := root["DhcpDdns"].(map[string]interface{})
	fwd, ok := ddns["forward-ddns"].(map[string]interface{})
	if !ok {
		t.Fatal("expected forward-ddns in config")
	}

	domains, ok := fwd["ddns-domains"].([]interface{})
	if !ok {
		t.Fatal("expected ddns-domains array")
	}
	if len(domains) != 2 {
		t.Fatalf("expected 2 forward domains, got %d", len(domains))
	}

	// Check first domain
	dom0 := domains[0].(map[string]interface{})
	if dom0["name"] != "example.com." {
		t.Errorf("expected domain name 'example.com.', got %v", dom0["name"])
	}
	if dom0["key-name"] != "my-tsig-key" {
		t.Errorf("expected key-name 'my-tsig-key', got %v", dom0["key-name"])
	}

	servers, ok := dom0["dns-servers"].([]interface{})
	if !ok {
		t.Fatal("expected dns-servers array")
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 DNS servers, got %d", len(servers))
	}

	srv0 := servers[0].(map[string]interface{})
	if srv0["ip-address"] != "10.0.0.53" {
		t.Errorf("expected server ip-address '10.0.0.53', got %v", srv0["ip-address"])
	}
	if int32(srv0["port"].(float64)) != 53 {
		t.Errorf("expected server port 53, got %v", srv0["port"])
	}

	// Second server should not have port set
	srv1 := servers[1].(map[string]interface{})
	if _, ok := srv1["port"]; ok {
		t.Error("expected port not to be set for second server")
	}

	// Verify no reverse-ddns
	if _, ok := ddns["reverse-ddns"]; ok {
		t.Error("expected reverse-ddns not to be set")
	}
}

func TestDdnsWithTSIGKeys(t *testing.T) {
	digestBits := int32(128)
	port := int32(53001)
	dnsTimeout := int32(500)
	spec := &keav1alpha1.KeaDhcpDdnsSpec{
		IPAddress:        "127.0.0.1",
		Port:             &port,
		DNSServerTimeout: &dnsTimeout,
		NCRProtocol:      "UDP",
		NCRFormat:        "JSON",
		TSIGKeys: []keav1alpha1.TSIGKey{
			{
				Name:       "my-hmac-md5",
				Algorithm:  "HMAC-MD5",
				DigestBits: &digestBits,
			},
			{
				Name:      "my-hmac-sha256",
				Algorithm: "HMAC-SHA256",
			},
		},
	}

	renderer := NewDdnsConfigRenderer(spec)
	renderer.ResolvedTSIGSecrets = map[string]string{
		"my-hmac-md5":    "LSWXnfkKZjdPJI5QxlpnfQ==",
		"my-hmac-sha256": "YmluZDEwa2V5Cg==",
	}
	data, err := renderer.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	ddns := root["DhcpDdns"].(map[string]interface{})

	// Check port and timeout
	if int32(ddns["port"].(float64)) != 53001 {
		t.Errorf("expected port 53001, got %v", ddns["port"])
	}
	if int32(ddns["dns-server-timeout"].(float64)) != 500 {
		t.Errorf("expected dns-server-timeout 500, got %v", ddns["dns-server-timeout"])
	}
	if ddns["ncr-protocol"] != "UDP" {
		t.Errorf("expected ncr-protocol 'UDP', got %v", ddns["ncr-protocol"])
	}
	if ddns["ncr-format"] != "JSON" {
		t.Errorf("expected ncr-format 'JSON', got %v", ddns["ncr-format"])
	}

	// Check TSIG keys
	keys, ok := ddns["tsig-keys"].([]interface{})
	if !ok {
		t.Fatal("expected tsig-keys array")
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 TSIG keys, got %d", len(keys))
	}

	key0 := keys[0].(map[string]interface{})
	if key0["name"] != "my-hmac-md5" {
		t.Errorf("expected key name 'my-hmac-md5', got %v", key0["name"])
	}
	if key0["algorithm"] != "HMAC-MD5" {
		t.Errorf("expected algorithm 'HMAC-MD5', got %v", key0["algorithm"])
	}
	if int32(key0["digest-bits"].(float64)) != 128 {
		t.Errorf("expected digest-bits 128, got %v", key0["digest-bits"])
	}
	if key0["secret"] != "LSWXnfkKZjdPJI5QxlpnfQ==" {
		t.Errorf("expected secret 'LSWXnfkKZjdPJI5QxlpnfQ==', got %v", key0["secret"])
	}

	key1 := keys[1].(map[string]interface{})
	if key1["name"] != "my-hmac-sha256" {
		t.Errorf("expected key name 'my-hmac-sha256', got %v", key1["name"])
	}
	// digest-bits should not be set for second key
	if _, ok := key1["digest-bits"]; ok {
		t.Error("expected digest-bits not to be set for second key")
	}
}
