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

// testHooksDBName is the database name used by the hook tests.
const testHooksDBName = "kea_hosts"

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
