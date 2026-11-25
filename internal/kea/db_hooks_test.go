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
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// testHooksDBName is the database name used by the hook tests.
const (
	testHooksDBName    = "kea_hosts"
	testComponentDhcp4 = "dhcp4"
)

func hookLibraries(t *testing.T, r ConfigRenderer, top string) map[string]int {
	t.Helper()
	data, err := r.RenderJSON()
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var root map[string]map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	hooks, _ := root[top]["hooks-libraries"].([]interface{})
	for _, h := range hooks {
		counts[h.(map[string]interface{})["library"].(string)]++
	}
	return counts
}

// Kea 3.0 only knows the MySQL and PostgreSQL backends once their hook is
// loaded, for host reservations as much as for leases: without it a MySQL
// hosts database fails at startup with "not been compiled with support for
// host database type: mysql". Each library must be loaded exactly once.
func TestDatabaseHooksCoverEveryDatabase(t *testing.T) {
	mysql := &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMySQL, Name: testHooksDBName}
	pgsql := keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypePostgreSQL, Name: testHooksDBName}
	memfile := &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile}

	tests := []struct {
		name      string
		lease     *keav1alpha1.DatabaseConfig
		hosts     *keav1alpha1.DatabaseConfig
		hostsList []keav1alpha1.DatabaseConfig
		userHooks []keav1alpha1.HookLibrary
		wantMySQL int
		wantPgSQL int
	}{
		{name: "memfile leases, MySQL hosts", lease: memfile, hosts: mysql, wantMySQL: 1},
		{name: "no lease database, MySQL hosts list", hostsList: []keav1alpha1.DatabaseConfig{*mysql}, wantMySQL: 1},
		{name: "MySQL leases and hosts", lease: mysql, hosts: mysql, wantMySQL: 1},
		{name: "MySQL leases, PostgreSQL hosts", lease: mysql, hostsList: []keav1alpha1.DatabaseConfig{pgsql}, wantMySQL: 1, wantPgSQL: 1},
		{name: "memfile only", lease: memfile},
		{name: "hook already listed by the user", lease: memfile, hosts: mysql,
			userHooks: []keav1alpha1.HookLibrary{{Library: mysqlHookLib}}, wantMySQL: 1},
		{name: "MySQL leases with the hook listed by the user", lease: mysql,
			userHooks: []keav1alpha1.HookLibrary{{Library: mysqlHookLib}}, wantMySQL: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			iface := keav1alpha1.InterfacesConfig{Interfaces: []string{testInterfaceEth0}}
			v4 := hookLibraries(t, NewDhcp4ConfigRenderer(&keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface,
				LeaseDatabase: tc.lease, HostsDatabase: tc.hosts, HostsDatabases: tc.hostsList, HooksLibraries: tc.userHooks}), "Dhcp4")
			v6 := hookLibraries(t, NewDhcp6ConfigRenderer(&keav1alpha1.KeaDhcp6ServerSpec{InterfacesConfig: iface,
				LeaseDatabase: tc.lease, HostsDatabase: tc.hosts, HostsDatabases: tc.hostsList, HooksLibraries: tc.userHooks}), "Dhcp6")
			for proto, got := range map[string]map[string]int{"dhcp4": v4, "dhcp6": v6} {
				if got[mysqlHookLib] != tc.wantMySQL || got[pgsqlHookLib] != tc.wantPgSQL {
					t.Errorf("%s: mysql hook x%d, pgsql hook x%d; want x%d, x%d", proto, got[mysqlHookLib], got[pgsqlHookLib], tc.wantMySQL, tc.wantPgSQL)
				}
			}
		})
	}
}

// TestDatabaseBackendsLoadedByKea starts the real Kea daemons with rendered
// configurations whose hosts databases need the MySQL or PostgreSQL hook, and
// checks that Kea registered those backends. `kea-dhcp4 -t` cannot catch a
// missing hook (it never opens database backends), so the daemon is started
// against an unreachable database: with the hook it fails to connect, without
// it it fails with "not been compiled with support for host database type".
// It needs podman and network access, so it only runs with KEA_CONFIG_CHECK=1
// (KEA_IMAGE_REPO and KEA_IMAGE_TAG as for TestDDNSConfigAcceptedByKea).
func TestDatabaseBackendsLoadedByKea(t *testing.T) {
	if os.Getenv("KEA_CONFIG_CHECK") != "1" {
		t.Skip("set KEA_CONFIG_CHECK=1 to start the Kea container images")
	}
	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Fatal("KEA_CONFIG_CHECK=1 but podman is not installed")
	}
	repo, tag := os.Getenv("KEA_IMAGE_REPO"), os.Getenv("KEA_IMAGE_TAG")
	if repo == "" {
		repo = "docker.cloudsmith.io/isc/docker"
	}
	if tag == "" {
		tag = "3.0.2"
	}

	// Nothing listens on 127.0.0.1:3399 inside the container.
	unreachable := func(dbType keav1alpha1.DatabaseType) keav1alpha1.DatabaseConfig {
		port, timeout, retry := int32(3399), int32(2), false
		return keav1alpha1.DatabaseConfig{Type: dbType, Name: testHooksDBName, Host: testLoopbackIP, Port: &port,
			ConnectTimeout: &timeout, RetryOnStartup: &retry}
	}
	persist := false
	memfile := &keav1alpha1.DatabaseConfig{Type: keav1alpha1.DatabaseTypeMemfile, Persist: &persist}
	mysql, pgsql := unreachable(keav1alpha1.DatabaseTypeMySQL), unreachable(keav1alpha1.DatabaseTypePostgreSQL)
	iface := keav1alpha1.InterfacesConfig{Interfaces: []string{"*"}}

	cases := []struct {
		name      string
		component string
		renderer  ConfigRenderer
		backend   string
	}{
		{"dhcp4 memfile leases, MySQL hosts", testComponentDhcp4,
			NewDhcp4ConfigRenderer(&keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, LeaseDatabase: memfile, HostsDatabase: &mysql}), "mysql"},
		{"dhcp4 memfile leases, PostgreSQL hosts list", testComponentDhcp4,
			NewDhcp4ConfigRenderer(&keav1alpha1.KeaDhcp4ServerSpec{InterfacesConfig: iface, LeaseDatabase: memfile, HostsDatabases: []keav1alpha1.DatabaseConfig{pgsql}}), "postgresql"},
		{"dhcp6 memfile leases, MySQL hosts", "dhcp6",
			NewDhcp6ConfigRenderer(&keav1alpha1.KeaDhcp6ServerSpec{InterfacesConfig: iface, LeaseDatabase: memfile, HostsDatabase: &mysql}), "mysql"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.renderer.RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			dir := t.TempDir()
			cfg := "kea-" + tc.component + ".conf"
			if err := os.WriteFile(filepath.Join(dir, cfg), data, 0o644); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, podman, "run", "--rm",
				"--cap-add=NET_RAW", "--cap-add=NET_BIND_SERVICE", "--cap-add=NET_ADMIN",
				"-v", dir+":/cfg:ro,z", "--entrypoint", "/usr/sbin/kea-"+tc.component,
				repo+"/kea-"+tc.component+":"+tag, "-c", "/cfg/"+cfg)
			out, _ := cmd.CombinedOutput() // Kea exits with an error: the database is unreachable.
			log := string(out)
			if strings.Contains(log, "not been compiled with support for host database type") {
				t.Fatalf("Kea did not load the %s hook:\n%s\n--- config ---\n%s", tc.backend, log, data)
			}
			registered := regexp.MustCompile(`HOSTS_BACKENDS_REGISTERED the following host backend types are available: ([a-z ]*)`).FindStringSubmatch(log)
			if registered == nil || !strings.Contains(registered[1], tc.backend) {
				t.Fatalf("host backend %s not registered:\n%s", tc.backend, log)
			}
		})
	}
}
