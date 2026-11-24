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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// Kea parameter names, enum values and scope labels used by the DDNS tests.
const (
	keyDDNSSendUpdates            = "ddns-send-updates"
	keyDDNSOverrideNoUpdate       = "ddns-override-no-update"
	keyDDNSOverrideClientUpdate   = "ddns-override-client-update"
	keyDDNSReplaceClientName      = "ddns-replace-client-name"
	keyDDNSGeneratedPrefix        = "ddns-generated-prefix"
	keyDDNSQualifyingSuffix       = "ddns-qualifying-suffix"
	keyDDNSUpdateOnRenew          = "ddns-update-on-renew"
	keyDDNSConflictResolutionMode = "ddns-conflict-resolution-mode"
	keyDDNSTTL                    = "ddns-ttl"
	keyDDNSTTLMin                 = "ddns-ttl-min"
	keyDDNSTTLMax                 = "ddns-ttl-max"
	keyHostnameCharSet            = "hostname-char-set"
	keyHostnameCharReplacement    = "hostname-char-replacement"
	keyDDNSUseConflictResolution  = "ddns-use-conflict-resolution"
	testReplaceWhenNotPresent     = "when-not-present"
	testConflictCheckWithDHCID    = "check-with-dhcid"
	scopeGlobal                   = "global"
	scopeSharedNetwork            = "shared-network"
	scopeSubnet                   = "subnet"
	scopePool                     = "pool"
)

func boolPtr(v bool) *bool       { return &v }
func int32Ptr(v int32) *int32    { return &v }
func stringPtr(v string) *string { return &v }

func ttlPercent(raw string) *apiextensionsv1.JSON {
	return &apiextensionsv1.JSON{Raw: []byte(raw)}
}

// ddnsFixture is one shape of DDNS behavior, together with the exact Kea JSON
// it must render to at a scope, so the parser test can assert that nothing
// was dropped or mangled before Kea sees it.
type ddnsFixture struct {
	name     string
	behavior func(suffix string) keav1alpha1.DDNSBehavior
	expected func(suffix string) map[string]interface{}
}

var ddnsFixtures = []ddnsFixture{
	{
		// Every inheritable parameter, with the TTL expressed as percent/min/max.
		name: "percent-min-max",
		behavior: func(suffix string) keav1alpha1.DDNSBehavior {
			return keav1alpha1.DDNSBehavior{
				DDNSSendUpdates:            boolPtr(true),
				DDNSOverrideNoUpdate:       boolPtr(true),
				DDNSOverrideClientUpdate:   boolPtr(true),
				DDNSReplaceClientName:      testReplaceWhenNotPresent,
				DDNSGeneratedPrefix:        stringPtr("host"),
				DDNSQualifyingSuffix:       stringPtr(suffix),
				DDNSUpdateOnRenew:          boolPtr(true),
				DDNSConflictResolutionMode: testConflictCheckWithDHCID,
				DDNSTTLPercent:             ttlPercent("0.5"),
				DDNSTTLMin:                 int32Ptr(60),
				DDNSTTLMax:                 int32Ptr(3600),
				HostnameCharSet:            stringPtr("[^A-Za-z0-9.-]"),
				HostnameCharReplacement:    stringPtr("x"),
			}
		},
		expected: func(suffix string) map[string]interface{} {
			return map[string]interface{}{
				keyDDNSSendUpdates: true, keyDDNSOverrideNoUpdate: true, keyDDNSOverrideClientUpdate: true,
				keyDDNSReplaceClientName: testReplaceWhenNotPresent, keyDDNSGeneratedPrefix: "host",
				keyDDNSQualifyingSuffix: suffix, keyDDNSUpdateOnRenew: true,
				keyDDNSConflictResolutionMode: testConflictCheckWithDHCID, keyDDNSTTLPercent: 0.5,
				keyDDNSTTLMin: float64(60), keyDDNSTTLMax: float64(3600),
				keyHostnameCharSet: "[^A-Za-z0-9.-]", keyHostnameCharReplacement: "x",
			}
		},
	},
	{
		// A fixed TTL, which Kea refuses next to percent/min/max.
		name: "fixed-ttl",
		behavior: func(suffix string) keav1alpha1.DDNSBehavior {
			return keav1alpha1.DDNSBehavior{
				DDNSSendUpdates:            boolPtr(true),
				DDNSReplaceClientName:      "always",
				DDNSQualifyingSuffix:       stringPtr(suffix),
				DDNSConflictResolutionMode: "no-check-with-dhcid",
				DDNSTTL:                    int32Ptr(600),
			}
		},
		expected: func(suffix string) map[string]interface{} {
			return map[string]interface{}{
				keyDDNSSendUpdates: true, keyDDNSReplaceClientName: "always",
				keyDDNSQualifyingSuffix: suffix, keyDDNSConflictResolutionMode: "no-check-with-dhcid",
				keyDDNSTTL: float64(600),
			}
		},
	},
	{
		// Explicit empty strings override inherited values, and a whole-number
		// percent must still reach Kea as a floating-point literal.
		name: "empty-overrides",
		behavior: func(string) keav1alpha1.DDNSBehavior {
			return keav1alpha1.DDNSBehavior{
				DDNSSendUpdates:         boolPtr(false),
				DDNSGeneratedPrefix:     stringPtr(""),
				DDNSQualifyingSuffix:    stringPtr(""),
				DDNSTTLPercent:          ttlPercent("0"),
				HostnameCharSet:         stringPtr(""),
				HostnameCharReplacement: stringPtr(""),
			}
		},
		expected: func(string) map[string]interface{} {
			return map[string]interface{}{
				keyDDNSSendUpdates: false, keyDDNSGeneratedPrefix: "", keyDDNSQualifyingSuffix: "",
				keyDDNSTTLPercent: float64(0), keyHostnameCharSet: "", keyHostnameCharReplacement: "",
			}
		},
	},
}

// Scope suffixes, so the assertions can tell the scopes apart.
const (
	suffixGlobal = "global.example"
	suffixShared = "sn.example"
	suffixSubnet = "subnet.example"
	suffixPool   = "pool.example"
)

// ddnsConnectivity builds the D2 connectivity block; Kea requires server-ip
// and sender-ip to share an address family.
func ddnsConnectivity(serverIP, senderIP string) *keav1alpha1.DhcpDdnsConfig {
	return &keav1alpha1.DhcpDdnsConfig{
		EnableUpdates: boolPtr(true), ServerIP: serverIP, ServerPort: int32Ptr(53001),
		SenderIP: senderIP, SenderPort: int32Ptr(0), MaxQueueSize: int32Ptr(1024),
		NCRProtocol: testNCRProtocolUDP, NCRFormat: testNCRFormatJSON,
	}
}

// dhcp4Spec places the fixture's behavior at every scope. The global scope
// uses the spec's plain fields (where "" means unset), so it always gets the
// percent-min-max shape.
func dhcp4Spec(f ddnsFixture) *keav1alpha1.KeaDhcp4ServerSpec {
	g := ddnsFixtures[0].behavior(suffixGlobal)
	return &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}},
		DhcpDdns:         ddnsConnectivity(testLoopbackIP, "0.0.0.0"),
		DDNSSendUpdates:  g.DDNSSendUpdates, DDNSOverrideNoUpdate: g.DDNSOverrideNoUpdate,
		DDNSOverrideClientUpdate: g.DDNSOverrideClientUpdate, DDNSReplaceClientName: g.DDNSReplaceClientName,
		DDNSGeneratedPrefix: *g.DDNSGeneratedPrefix, DDNSQualifyingSuffix: *g.DDNSQualifyingSuffix,
		DDNSUpdateOnRenew: g.DDNSUpdateOnRenew, DDNSConflictResolutionMode: g.DDNSConflictResolutionMode,
		DDNSTTLPercent: g.DDNSTTLPercent, DDNSTTLMin: g.DDNSTTLMin, DDNSTTLMax: g.DDNSTTLMax,
		HostnameCharSet: *g.HostnameCharSet, HostnameCharReplacement: *g.HostnameCharReplacement,
		DDNSUseConflictResolution: boolPtr(true),
		Subnet4: []keav1alpha1.Subnet4{{
			ID: 1, Subnet: testSubnet4CIDR, DDNSBehavior: f.behavior(suffixSubnet),
			Pools: []keav1alpha1.Pool4{{Pool: "192.0.2.10 - 192.0.2.100", DDNSBehavior: f.behavior(suffixPool),
				RequireClientClasses: []string{"c1"}}},
		}},
		SharedNetworks: []keav1alpha1.SharedNetwork4{{
			Name: testSharedNetwork, DDNSBehavior: f.behavior(suffixShared),
			Subnet4: []keav1alpha1.Subnet4{{ID: 2, Subnet: "198.51.100.0/24",
				Pools: []keav1alpha1.Pool4{{Pool: "198.51.100.10 - 198.51.100.20"}}}},
		}},
	}
}

func dhcp6Spec(f ddnsFixture) *keav1alpha1.KeaDhcp6ServerSpec {
	g := ddnsFixtures[0].behavior(suffixGlobal)
	return &keav1alpha1.KeaDhcp6ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}},
		DhcpDdns:         ddnsConnectivity("::1", "::"),
		DDNSSendUpdates:  g.DDNSSendUpdates, DDNSOverrideNoUpdate: g.DDNSOverrideNoUpdate,
		DDNSOverrideClientUpdate: g.DDNSOverrideClientUpdate, DDNSReplaceClientName: g.DDNSReplaceClientName,
		DDNSGeneratedPrefix: *g.DDNSGeneratedPrefix, DDNSQualifyingSuffix: *g.DDNSQualifyingSuffix,
		DDNSUpdateOnRenew: g.DDNSUpdateOnRenew, DDNSConflictResolutionMode: g.DDNSConflictResolutionMode,
		DDNSTTLPercent: g.DDNSTTLPercent, DDNSTTLMin: g.DDNSTTLMin, DDNSTTLMax: g.DDNSTTLMax,
		HostnameCharSet: *g.HostnameCharSet, HostnameCharReplacement: *g.HostnameCharReplacement,
		DDNSUseConflictResolution: boolPtr(true),
		Subnet6: []keav1alpha1.Subnet6{{
			ID: 1, Subnet: testSubnet6Prefix, DDNSBehavior: f.behavior(suffixSubnet),
			Pools: []keav1alpha1.Pool6{{Pool: testPool6Range, DDNSBehavior: f.behavior(suffixPool),
				RequireClientClasses: []string{"c1"}}},
			PDPools: []keav1alpha1.PDPool{{Prefix: "2001:db8:2::", PrefixLen: 48, DelegatedLen: 64,
				RequireClientClasses: []string{"c2"}}},
		}},
		SharedNetworks: []keav1alpha1.SharedNetwork6{{
			Name: testSharedNetwork, DDNSBehavior: f.behavior(suffixShared),
			RequireClientClasses: []string{"c3"}, MinValidLifetime: int32Ptr(100), MaxValidLifetime: int32Ptr(9000),
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

// ddnsScopes returns the rendered global, shared-network, subnet and pool
// maps of a DHCPv4 or DHCPv6 config.
func ddnsScopes(top map[string]interface{}, v string) map[string]map[string]interface{} {
	subnet := top[scopeSubnet+v].([]interface{})[0].(map[string]interface{})
	return map[string]map[string]interface{}{
		scopeGlobal:        top,
		scopeSharedNetwork: top["shared-networks"].([]interface{})[0].(map[string]interface{}),
		scopeSubnet:        subnet,
		scopePool:          subnet["pools"].([]interface{})[0].(map[string]interface{}),
	}
}

// assertDDNSValues checks that every expected key is rendered with exactly
// the expected value at the scope, so a key the renderer silently drops (or
// an empty string it swallows) fails here rather than being parsed as "fine".
func assertDDNSValues(t *testing.T, scope string, got, want map[string]interface{}) {
	t.Helper()
	for key, exp := range want {
		val, ok := got[key]
		if !ok {
			t.Errorf("%s: key %q missing", scope, key)
			continue
		}
		if !reflect.DeepEqual(val, exp) {
			t.Errorf("%s: %s = %#v, want %#v", scope, key, val, exp)
		}
	}
}

func expectedByScope(f ddnsFixture) map[string]map[string]interface{} {
	return map[string]map[string]interface{}{
		scopeGlobal:        ddnsFixtures[0].expected(suffixGlobal),
		scopeSharedNetwork: f.expected(suffixShared),
		scopeSubnet:        f.expected(suffixSubnet),
		scopePool:          f.expected(suffixPool),
	}
}

func TestDDNSBehaviorRenderedAtEveryScope(t *testing.T) {
	for _, f := range ddnsFixtures {
		for _, v := range []string{"4", "6"} {
			t.Run(f.name+"/dhcp"+v, func(t *testing.T) {
				var top map[string]interface{}
				if v == "4" {
					top = renderRoot(t, NewDhcp4ConfigRenderer(dhcp4Spec(f)), "Dhcp4")
				} else {
					top = renderRoot(t, NewDhcp6ConfigRenderer(dhcp6Spec(f)), "Dhcp6")
				}
				scopes := ddnsScopes(top, v)
				for name, want := range expectedByScope(f) {
					assertDDNSValues(t, name, scopes[name], want)
				}
				// The deprecated boolean is only valid globally: Kea 3.0 fails with
				// `got unexpected keyword keyDDNSUseConflictResolution in pools map`.
				if top[keyDDNSUseConflictResolution] != true {
					t.Errorf("expected global ddns-use-conflict-resolution, got %v", top[keyDDNSUseConflictResolution])
				}
				for name, scope := range scopes {
					if name == scopeGlobal {
						continue
					}
					if _, ok := scope[keyDDNSUseConflictResolution]; ok {
						t.Errorf("%s must not carry ddns-use-conflict-resolution", name)
					}
				}
			})
		}
	}
}

func TestDhcp6RequireClientClassesRendered(t *testing.T) {
	top := renderRoot(t, NewDhcp6ConfigRenderer(dhcp6Spec(ddnsFixtures[0])), "Dhcp6")
	subnet := top["subnet6"].([]interface{})[0].(map[string]interface{})
	pool := subnet["pools"].([]interface{})[0].(map[string]interface{})
	pdPool := subnet["pd-pools"].([]interface{})[0].(map[string]interface{})
	network := top["shared-networks"].([]interface{})[0].(map[string]interface{})
	for name, got := range map[string]interface{}{scopePool: pool["require-client-classes"],
		"pd-pool": pdPool["require-client-classes"], scopeSharedNetwork: network["require-client-classes"]} {
		if list, ok := got.([]interface{}); !ok || len(list) != 1 {
			t.Errorf("%s: expected one require-client-classes entry, got %#v", name, got)
		}
	}
	if network["min-valid-lifetime"] != float64(100) || network["max-valid-lifetime"] != float64(9000) {
		t.Errorf("shared-network lifetimes not rendered: %v / %v", network["min-valid-lifetime"], network["max-valid-lifetime"])
	}
}

// Kea's parser only accepts a floating-point literal for ddns-ttl-percent
// ("unexpected integer, expecting floating point"), so whole numbers must be
// rendered with a decimal point, and an empty value must not become null.
func TestDDNSTTLPercentRendering(t *testing.T) {
	for raw, want := range map[string]string{"0": "0.0", "1": "1.0", "0.5": "0.5", "0.25": "0.25", "1e-1": "0.1"} {
		spec := &keav1alpha1.KeaDhcp4ServerSpec{
			InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
			DDNSTTLPercent:   ttlPercent(raw),
		}
		data, err := NewDhcp4ConfigRenderer(spec).RenderJSON()
		if err != nil {
			t.Fatalf("%s: RenderJSON failed: %v", raw, err)
		}
		if !strings.Contains(string(data), fmt.Sprintf(`"%s": %s,`, keyDDNSTTLPercent, want)) &&
			!strings.Contains(string(data), fmt.Sprintf(`"%s": %s`+"\n", keyDDNSTTLPercent, want)) {
			t.Errorf("%s: expected literal %s in output, got:\n%s", raw, want, data)
		}
	}

	spec := &keav1alpha1.KeaDhcp4ServerSpec{
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}},
		DDNSTTLPercent:   &apiextensionsv1.JSON{},
	}
	if v, ok := renderRoot(t, NewDhcp4ConfigRenderer(spec), "Dhcp4")[keyDDNSTTLPercent]; ok {
		t.Errorf("empty ddns-ttl-percent must not be rendered (Kea rejects null), got %#v", v)
	}
}

func TestDDNSValidationRejectsWhatKeaRejects(t *testing.T) {
	iface := keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}}
	tests := []struct {
		name    string
		spec    *keav1alpha1.KeaDhcp4ServerSpec
		wantErr string // empty: must render
	}{
		{
			name: "fixed ttl and percent at pool scope",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface,
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					Pools: []keav1alpha1.Pool4{{Pool: "192.0.2.10 - 192.0.2.100",
						DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTL: int32Ptr(600), DDNSTTLPercent: ttlPercent("0.5")}}}}}},
			wantErr: "subnet4[0].pools[0]: cannot specify both ddns-ttl-percent and ddns-ttl",
		},
		{
			name: "fixed ttl of zero still conflicts with percent (Kea does not treat 0 as unset)",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface,
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTL: int32Ptr(0), DDNSTTLPercent: ttlPercent("0.5")}}}},
			wantErr: "subnet4[0]: cannot specify both ddns-ttl-percent and ddns-ttl",
		},
		{
			name: "fixed ttl and min inside a shared network",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface,
				SharedNetworks: []keav1alpha1.SharedNetwork4{{Name: testSharedNetwork,
					Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
						DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTL: int32Ptr(600), DDNSTTLMin: int32Ptr(60)}}}}}},
			wantErr: "shared-networks[0].subnet4[0]: cannot specify both ddns-ttl-min and ddns-ttl",
		},
		{
			name:    "fixed ttl and max globally",
			spec:    &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, DDNSTTL: int32Ptr(600), DDNSTTLMax: int32Ptr(3600)},
			wantErr: "global: cannot specify both ddns-ttl-max and ddns-ttl",
		},
		{
			name:    "percent given as a quoted string",
			spec:    &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, DDNSTTLPercent: ttlPercent(`"0.5"`)},
			wantErr: `global: ddns-ttl-percent must be a non-negative number, got "0.5"`,
		},
		{
			name: "negative percent",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface,
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTLPercent: ttlPercent("-0.5")}}}},
			wantErr: "subnet4[0]: ddns-ttl-percent must be a non-negative number, got -0.5",
		},
		{
			name: "percent of zero suppresses an inherited value and is accepted",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, DDNSTTLPercent: ttlPercent("0.5"),
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTLPercent: ttlPercent("0")}}}},
		},
		{
			name: "fixed ttl globally with percent in a subnet is fine (different scopes)",
			spec: &keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, DDNSTTL: int32Ptr(600),
				Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
					DDNSBehavior: keav1alpha1.DDNSBehavior{DDNSTTLPercent: ttlPercent("0.25")}}}},
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

// TestDDNSConfigAcceptedByKea feeds the rendered configs of every fixture to
// the real Kea parsers (`kea-dhcp4 -t` / `kea-dhcp6 -t`) in the Kea container
// images, after asserting the expected values are present at every scope, so
// each DDNS keyword (percent/min/max TTL, fixed TTL, explicit empty overrides,
// the deprecated global boolean, require-client-classes on pools, pd-pools and
// shared networks) is checked against the Kea version the operator ships.
// It needs podman and network access, so it only runs when KEA_CONFIG_CHECK=1:
//
//	KEA_CONFIG_CHECK=1 go test ./internal/kea/ -run TestDDNSConfigAcceptedByKea -v
//
// KEA_IMAGE_REPO (default docker.cloudsmith.io/isc/docker) and KEA_IMAGE_TAG
// (default 3.0.2) pick the images.
func TestDDNSConfigAcceptedByKea(t *testing.T) {
	if os.Getenv("KEA_CONFIG_CHECK") != "1" {
		t.Skip("set KEA_CONFIG_CHECK=1 to validate rendered configs with the Kea container images")
	}
	// The check was explicitly requested, so a missing tool is a failure,
	// not a silent skip that would let CI report success without parsing.
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Fatal("KEA_CONFIG_CHECK=1 but podman is not installed")
	}
	repo := os.Getenv("KEA_IMAGE_REPO")
	if repo == "" {
		repo = "docker.cloudsmith.io/isc/docker"
	}
	tag := os.Getenv("KEA_IMAGE_TAG")
	if tag == "" {
		tag = "3.0.2"
	}
	// Record exactly which images were checked.
	for _, v := range []string{"4", "6"} {
		image := repo + "/kea-dhcp" + v + ":" + tag
		if out, err := exec.Command(podman, "pull", "-q", image).CombinedOutput(); err != nil {
			t.Fatalf("pulling %s: %v\n%s", image, err, out)
		}
		digest, err := exec.Command(podman, "image", "inspect", "--format", "{{.Digest}}", image).Output()
		if err != nil {
			t.Fatalf("inspecting %s: %v", image, err)
		}
		t.Logf("checking against %s@%s", image, strings.TrimSpace(string(digest)))
	}

	for _, f := range ddnsFixtures {
		for _, v := range []string{"4", "6"} {
			t.Run(f.name+"/dhcp"+v, func(t *testing.T) {
				var r ConfigRenderer
				if v == "4" {
					r = NewDhcp4ConfigRenderer(dhcp4Spec(f))
				} else {
					r = NewDhcp6ConfigRenderer(dhcp6Spec(f))
				}
				data, err := r.RenderJSON()
				if err != nil {
					t.Fatalf("RenderJSON failed: %v", err)
				}
				var root map[string]interface{}
				if err := json.Unmarshal(data, &root); err != nil {
					t.Fatalf("invalid JSON: %v", err)
				}
				scopes := ddnsScopes(root["Dhcp"+v].(map[string]interface{}), v)
				for name, want := range expectedByScope(f) {
					assertDDNSValues(t, name, scopes[name], want)
				}

				dir := t.TempDir()
				cfg := "kea-dhcp" + v + ".conf"
				if err := os.WriteFile(filepath.Join(dir, cfg), data, 0o644); err != nil {
					t.Fatal(err)
				}
				// The Kea binaries carry file capabilities, so the container needs them to exec.
				cmd := exec.Command(podman, "run", "--rm",
					"--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
					"-v", dir+":/cfg:ro,z",
					"--entrypoint", "/usr/sbin/kea-dhcp"+v,
					repo+"/kea-dhcp"+v+":"+tag,
					"-t", "/cfg/"+cfg)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("kea-dhcp%s -t rejected the rendered config: %v\n%s\n--- config ---\n%s", v, err, out, data)
				}
				if strings.Contains(string(out), "ERROR") {
					t.Fatalf("kea-dhcp%s -t logged an error:\n%s", v, out)
				}
			})
		}
	}
}
