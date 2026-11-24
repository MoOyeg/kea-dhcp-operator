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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

func boolPtr(v bool) *bool    { return &v }
func int32Ptr(v int32) *int32 { return &v }

func ttlPercent(raw string) *apiextensionsv1.JSON {
	return &apiextensionsv1.JSON{Raw: []byte(raw)}
}

// fullDDNSBehavior sets every inheritable DDNS parameter Kea accepts at
// shared-network, subnet and pool scope (a fixed ddns-ttl is left out because
// Kea refuses it next to ddns-ttl-percent/min/max).
func fullDDNSBehavior(suffix string) keav1alpha1.DDNSBehavior {
	return keav1alpha1.DDNSBehavior{
		DDNSSendUpdates:            boolPtr(true),
		DDNSOverrideNoUpdate:       boolPtr(true),
		DDNSOverrideClientUpdate:   boolPtr(true),
		DDNSReplaceClientName:      "when-not-present",
		DDNSGeneratedPrefix:        "host",
		DDNSQualifyingSuffix:       suffix,
		DDNSUpdateOnRenew:          boolPtr(true),
		DDNSConflictResolutionMode: "check-with-dhcid",
		DDNSTTLPercent:             ttlPercent("0.5"),
		DDNSTTLMin:                 int32Ptr(60),
		DDNSTTLMax:                 int32Ptr(3600),
		HostnameCharSet:            "[^A-Za-z0-9.-]",
		HostnameCharReplacement:    "x",
	}
}

func fullDhcp4DDNSSpec() *keav1alpha1.KeaDhcp4ServerSpec {
	g := fullDDNSBehavior("global.example")
	return &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}},
		DhcpDdns: &keav1alpha1.DhcpDdnsConfig{
			EnableUpdates: boolPtr(true), ServerIP: testLoopbackIP, ServerPort: int32Ptr(53001),
			SenderIP: "0.0.0.0", SenderPort: int32Ptr(0), MaxQueueSize: int32Ptr(1024),
			NCRProtocol: testNCRProtocolUDP, NCRFormat: testNCRFormatJSON,
		},
		DDNSSendUpdates: g.DDNSSendUpdates, DDNSOverrideNoUpdate: g.DDNSOverrideNoUpdate,
		DDNSOverrideClientUpdate: g.DDNSOverrideClientUpdate, DDNSReplaceClientName: g.DDNSReplaceClientName,
		DDNSGeneratedPrefix: g.DDNSGeneratedPrefix, DDNSQualifyingSuffix: g.DDNSQualifyingSuffix,
		DDNSUpdateOnRenew: g.DDNSUpdateOnRenew, DDNSConflictResolutionMode: g.DDNSConflictResolutionMode,
		DDNSTTLPercent: g.DDNSTTLPercent, DDNSTTLMin: g.DDNSTTLMin, DDNSTTLMax: g.DDNSTTLMax,
		HostnameCharSet: g.HostnameCharSet, HostnameCharReplacement: g.HostnameCharReplacement,
		DDNSUseConflictResolution: boolPtr(true),
		Subnet4: []keav1alpha1.Subnet4{{
			ID: 1, Subnet: testSubnet4CIDR, DDNSBehavior: fullDDNSBehavior("subnet.example"),
			Pools: []keav1alpha1.Pool4{{Pool: "192.0.2.10 - 192.0.2.100", DDNSBehavior: fullDDNSBehavior("pool.example")}},
		}},
		SharedNetworks: []keav1alpha1.SharedNetwork4{{
			Name: testSharedNetwork, DDNSBehavior: fullDDNSBehavior("sn.example"),
			Subnet4: []keav1alpha1.Subnet4{{ID: 2, Subnet: "198.51.100.0/24",
				Pools: []keav1alpha1.Pool4{{Pool: "198.51.100.10 - 198.51.100.20"}}}},
		}},
	}
}

func fullDhcp6DDNSSpec() *keav1alpha1.KeaDhcp6ServerSpec {
	g := fullDDNSBehavior("global.example")
	return &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}},
		DhcpDdns: &keav1alpha1.DhcpDdnsConfig{
			EnableUpdates: boolPtr(true), ServerIP: "::1", ServerPort: int32Ptr(53001),
			NCRProtocol: testNCRProtocolUDP, NCRFormat: testNCRFormatJSON,
		},
		DDNSSendUpdates: g.DDNSSendUpdates, DDNSOverrideNoUpdate: g.DDNSOverrideNoUpdate,
		DDNSOverrideClientUpdate: g.DDNSOverrideClientUpdate, DDNSReplaceClientName: g.DDNSReplaceClientName,
		DDNSGeneratedPrefix: g.DDNSGeneratedPrefix, DDNSQualifyingSuffix: g.DDNSQualifyingSuffix,
		DDNSUpdateOnRenew: g.DDNSUpdateOnRenew, DDNSConflictResolutionMode: g.DDNSConflictResolutionMode,
		DDNSTTLPercent: g.DDNSTTLPercent, DDNSTTLMin: g.DDNSTTLMin, DDNSTTLMax: g.DDNSTTLMax,
		HostnameCharSet: g.HostnameCharSet, HostnameCharReplacement: g.HostnameCharReplacement,
		DDNSUseConflictResolution: boolPtr(true),
		Subnet6: []keav1alpha1.Subnet6{{
			ID: 1, Subnet: testSubnet6Prefix, DDNSBehavior: fullDDNSBehavior("subnet.example"),
			Pools: []keav1alpha1.Pool6{{Pool: testPool6Range, DDNSBehavior: fullDDNSBehavior("pool.example")}},
		}},
		SharedNetworks: []keav1alpha1.SharedNetwork6{{
			Name: testSharedNetwork, DDNSBehavior: fullDDNSBehavior("sn.example"),
			MinValidLifetime: int32Ptr(100), MaxValidLifetime: int32Ptr(9000),
			Subnet6: []keav1alpha1.Subnet6{{ID: 2, Subnet: "2001:db8:3::/64",
				Pools: []keav1alpha1.Pool6{{Pool: "2001:db8:3::1 - 2001:db8:3::ff"}}}},
		}},
	}
}

func renderRoot(t *testing.T, r ConfigRenderer, top string) map[string]interface{} {
	t.Helper()
	data, err := r.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return root[top].(map[string]interface{})
}

// Keys Kea accepts at shared-network, subnet and pool scope.
var scopedDDNSKeys = []string{
	"ddns-send-updates", "ddns-override-no-update", "ddns-override-client-update",
	"ddns-replace-client-name", "ddns-generated-prefix", "ddns-qualifying-suffix",
	"ddns-update-on-renew", "ddns-conflict-resolution-mode", keyDDNSTTLPercent,
	"ddns-ttl-min", "ddns-ttl-max", "hostname-char-set", "hostname-char-replacement",
}

func TestDhcp4DDNSBehaviorRenderedAtEveryScope(t *testing.T) {
	dhcp4 := renderRoot(t, NewDhcp4ConfigRenderer(fullDhcp4DDNSSpec()), "Dhcp4")

	subnet := dhcp4["subnet4"].([]interface{})[0].(map[string]interface{})
	pool := subnet["pools"].([]interface{})[0].(map[string]interface{})
	network := dhcp4["shared-networks"].([]interface{})[0].(map[string]interface{})
	for name, scope := range map[string]map[string]interface{}{"global scope": dhcp4, "subnet scope": subnet, "pool scope": pool, "shared-network scope": network} {
		for _, key := range scopedDDNSKeys {
			if _, ok := scope[key]; !ok {
				t.Errorf("%s scope: expected key %q", name, key)
			}
		}
		if scope[keyDDNSTTLPercent] != 0.5 {
			t.Errorf("%s scope: ddns-ttl-percent should be the number 0.5, got %#v", name, scope[keyDDNSTTLPercent])
		}
	}
	if subnet["ddns-qualifying-suffix"] != "subnet.example" || pool["ddns-qualifying-suffix"] != "pool.example" ||
		network["ddns-qualifying-suffix"] != "sn.example" {
		t.Errorf("scoped values not kept apart: subnet=%v pool=%v network=%v",
			subnet["ddns-qualifying-suffix"], pool["ddns-qualifying-suffix"], network["ddns-qualifying-suffix"])
	}

	// The deprecated boolean is only valid globally: Kea 3.0 fails with
	// `got unexpected keyword "ddns-use-conflict-resolution" in pools map`.
	if dhcp4["ddns-use-conflict-resolution"] != true {
		t.Errorf("expected global ddns-use-conflict-resolution, got %v", dhcp4["ddns-use-conflict-resolution"])
	}
	for name, scope := range map[string]map[string]interface{}{"subnet scope": subnet, "pool scope": pool, "shared-network scope": network} {
		if _, ok := scope["ddns-use-conflict-resolution"]; ok {
			t.Errorf("%s scope must not carry ddns-use-conflict-resolution", name)
		}
	}
}

func TestDDNSTTLPercentWithoutValueIsOmitted(t *testing.T) {
	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
		DDNSTTLPercent:   &apiextensionsv1.JSON{},
	}
	dhcp4 := renderRoot(t, NewDhcp4ConfigRenderer(spec), "Dhcp4")
	if v, ok := dhcp4[keyDDNSTTLPercent]; ok {
		t.Errorf("empty ddns-ttl-percent must not be rendered (Kea rejects null), got %#v", v)
	}
}

func TestDDNSValidationRejectsWhatKeaRejects(t *testing.T) {
	tests := []struct {
		name    string
		spec    *keav1alpha1.KeaDhcp4ServerSpec
		wantErr string // empty: must render
	}{
		{
			name: "fixed ttl and percent at pool scope",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					Pools: []keav1alpha1.Pool4{{Pool: "192.0.2.10 - 192.0.2.100",
						DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTL: int32Ptr(600), DDNSTTLPercent: ttlPercent("0.5")}}}}},
			},
			wantErr: "subnet4[0].pools[0]: cannot specify both ddns-ttl-percent and ddns-ttl",
		},
		{
			name: "fixed ttl and min inside a shared network",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
				SharedNetworks: []keav1alpha1.SharedNetwork4{{Name: testSharedNetwork,
					Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
						DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTL: int32Ptr(600), DDNSTTLMin: int32Ptr(60)}}}}},
			},
			wantErr: "shared-networks[0].subnet4[0]: cannot specify both ddns-ttl-min and ddns-ttl",
		},
		{
			name: "fixed ttl and max globally",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
				DDNSTTL:          int32Ptr(600), DDNSTTLMax: int32Ptr(3600),
			},
			wantErr: "global: cannot specify both ddns-ttl-max and ddns-ttl",
		},
		{
			name: "percent given as a quoted string",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
				DDNSTTLPercent:   ttlPercent(`"0.5"`),
			},
			wantErr: `global: ddns-ttl-percent must be a positive number, got "0.5"`,
		},
		{
			name: "percent of zero",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTLPercent: ttlPercent("0")}}},
			},
			wantErr: "subnet4[0]: ddns-ttl-percent must be a positive number, got 0",
		},
		{
			name: "fixed ttl globally with percent in a subnet is fine (different scopes)",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{
				InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
				DDNSTTL:          int32Ptr(600),
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTLPercent: ttlPercent("0.25")}}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDhcp4ConfigRenderer(tc.spec).RenderJSON()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}

	// The same validation guards the DHCPv6 renderer.
	spec6 := &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
		Subnet6: []keav1alpha1.Subnet6{{ID: 1, Subnet: testSubnet6Prefix,
			Pools: []keav1alpha1.Pool6{{Pool: testPool6Range,
				DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTL: int32Ptr(600), DDNSTTLPercent: ttlPercent("0.5")}}}}},
	}
	if _, err := NewDhcp6ConfigRenderer(spec6).RenderJSON(); err == nil ||
		!strings.Contains(err.Error(), "subnet6[0].pools[0]: cannot specify both ddns-ttl-percent and ddns-ttl") {
		t.Fatalf("dhcp6: expected pool-scope ttl error, got %v", err)
	}
}

// TestDDNSConfigAcceptedByKea feeds the rendered configs to the real Kea
// parsers (`kea-dhcp4 -t` / `kea-dhcp6 -t`) in the Kea container images, so
// every DDNS keyword is checked at every scope against the Kea version the
// operator ships. It needs podman and network access, so it only runs when
// KEA_CONFIG_CHECK=1 is set:
//
//	KEA_CONFIG_CHECK=1 go test ./internal/kea/ -run TestDDNSConfigAcceptedByKea -v
//
// KEA_IMAGE_REPO (default docker.cloudsmith.io/isc/docker) and KEA_IMAGE_TAG
// (default 3.0.2) pick the images.
func TestDDNSConfigAcceptedByKea(t *testing.T) {
	if os.Getenv("KEA_CONFIG_CHECK") != "1" {
		t.Skip("set KEA_CONFIG_CHECK=1 to validate rendered configs with the Kea container images")
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Skip("podman not found")
	}
	repo := os.Getenv("KEA_IMAGE_REPO")
	if repo == "" {
		repo = "docker.cloudsmith.io/isc/docker"
	}
	tag := os.Getenv("KEA_IMAGE_TAG")
	if tag == "" {
		tag = "3.0.2"
	}

	dir := t.TempDir()
	cases := []struct {
		component string
		renderer  ConfigRenderer
	}{
		{"dhcp4", NewDhcp4ConfigRenderer(fullDhcp4DDNSSpec())},
		{"dhcp6", NewDhcp6ConfigRenderer(fullDhcp6DDNSSpec())},
	}
	for _, tc := range cases {
		t.Run(tc.component, func(t *testing.T) {
			data, err := tc.renderer.RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			cfg := "kea-" + tc.component + ".conf"
			if err := os.WriteFile(filepath.Join(dir, cfg), data, 0o644); err != nil {
				t.Fatal(err)
			}
			// The Kea binaries carry file capabilities, so the container needs them to exec.
			cmd := exec.Command(podman, "run", "--rm",
				"--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
				"-v", dir+":/cfg:ro,z",
				"--entrypoint", "/usr/sbin/kea-"+tc.component,
				repo+"/kea-"+tc.component+":"+tag,
				"-t", "/cfg/"+cfg)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("kea-%s -t rejected the rendered config: %v\n%s\n--- config ---\n%s", tc.component, err, out, data)
			}
			if strings.Contains(string(out), "ERROR") {
				t.Fatalf("kea-%s -t logged an error:\n%s", tc.component, out)
			}
		})
	}
}
