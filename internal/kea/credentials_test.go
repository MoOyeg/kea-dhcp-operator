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

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

const (
	phUser = "$KEA_LEASE_DB_USER"
	phPass = "$KEA_LEASE_DB_PASSWORD"
)

// renderWithPlaceholders renders a DHCPv4 config whose lease database uses
// the credential placeholders, as the controller does.
func renderWithPlaceholders(t *testing.T) []byte {
	t.Helper()
	r := NewDhcp4ConfigRenderer(&keav1alpha1.KeaDhcp4ServerSpec{
		// "*": the Kea parser check runs in a container without a fixed interface name.
		InterfacesConfig: keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}},
		LeaseDatabase:    &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: "kea", Host: "db"},
		Subnet4: []keav1alpha1.Subnet4{{ID: 1, Subnet: testSubnet4CIDR,
			Pools: []keav1alpha1.Pool4{{Pool: testPool4Range}}}},
	})
	r.LeaseDBCreds = DBCredentials{User: phUser, Password: phPass}
	data, err := r.RenderJSON()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func leaseDB(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var root map[string]map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	return root["Dhcp4"]["lease-database"].(map[string]interface{})
}

// Every credential must reach Kea byte for byte. These are the values the
// old sed-based substitution corrupted or rejected.
func TestSubstituteCredentialsKeepsEveryCharacter(t *testing.T) {
	passwords := []string{
		"", "with spaces", "a&b", "a|b", `a"b`, "a'b", `a\b`, `a\\b`, "a$b", "a`b`c",
		"line1\nline2", "tab\there", "ünïcødé-密码", "%s %d {} []", `{"json":"inside"}`,
		phUser, // a password equal to another placeholder stays literal
	}
	base := renderWithPlaceholders(t)
	for _, pw := range passwords {
		out, err := SubstituteCredentials(base, map[string]string{phUser: "kea-user", phPass: pw})
		if err != nil {
			t.Fatalf("%q: %v", pw, err)
		}
		db := leaseDB(t, out)
		if db["password"] != pw {
			t.Errorf("password %q came out as %q", pw, db["password"])
		}
		if db["user"] != "kea-user" {
			t.Errorf("user = %q", db["user"])
		}
	}
}

// Only whole credential fields are replaced: text that merely contains a
// placeholder is left alone, and everything else is preserved exactly.
func TestSubstituteCredentialsOnlyReplacesWholeFields(t *testing.T) {
	in := []byte(`{"Dhcp4": {"lease-database": {"password": "` + phPass + `"},
		"option-data": [{"name": "domain-name", "data": "prefix-` + phPass + `"}],
		"valid-lifetime": 18446744073709551615, "ddns-ttl-percent": 0.0, "x": 1e-1}}`)
	out, err := SubstituteCredentials(in, map[string]string{phPass: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`"password": "secret"`, `"data": "prefix-` + phPass + `"`,
		`18446744073709551615`, `"ddns-ttl-percent": 0.0`, `"x": 1e-1`} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %s:\n%s", want, s)
		}
	}
}

func TestSubstituteCredentialsErrorsCarryNoText(t *testing.T) {
	_, err := SubstituteCredentials([]byte(`{"password": "hunter2"`), nil)
	if err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("err = %v, want an error without configuration text", err)
	}
}

// TestCredentialsWithPunctuationAcceptedByKea renders credentials full of
// JSON-significant characters into a configuration and runs the shipped
// parser on it (`kea-dhcp4 -t` checks syntax without opening the database),
// so the escaping encoding/json produces is the escaping Kea reads.
// It runs with KEA_CONFIG_CHECK=1 (see TestDDNSConfigAcceptedByKea).
func TestCredentialsWithPunctuationAcceptedByKea(t *testing.T) {
	k := keaContainerCheck(t)
	image := k.image(t, resources.ComponentDhcp4)
	password := `a&b|c"d'e\f$KEA_LEASE_DB_USER` + "`g`\nh\tü 密码 {}[]"
	data, err := SubstituteCredentials(renderWithPlaceholders(t), map[string]string{phUser: `us"er\`, phPass: password})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "kea-dhcp4.conf"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(k.podman, "run", "--rm", "--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
		"-v", dir+":/cfg:ro,z", "--entrypoint", "/usr/sbin/kea-dhcp4", image, "-t", "/cfg/kea-dhcp4.conf").CombinedOutput()
	if err != nil || strings.Contains(string(out), "ERROR") {
		t.Fatalf("kea-dhcp4 -t rejected a configuration with punctuation-heavy credentials: %v\n%s", err, out)
	}
}
