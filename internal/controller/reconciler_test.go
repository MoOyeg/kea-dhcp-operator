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

package controller

import (
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

func TestFillPeerURLs(t *testing.T) {
	t.Run("nil HA is a no-op", func(t *testing.T) {
		fillPeerURLs(nil, "ns", "myserver", "dhcp4", 8000)
	})

	t.Run("fills empty URLs using naming convention", func(t *testing.T) {
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Mode:           "load-balancing",
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", Role: "primary"},
				{Name: "server2", Role: "secondary"},
			},
		}
		fillPeerURLs(ha, "dhcptest1", "dhcp4-ha", "dhcp4", 8000)

		expected0 := "http://dhcp4-ha-dhcp4-0.dhcp4-ha-dhcp4-hl.dhcptest1.svc.cluster.local:8000/"
		expected1 := "http://dhcp4-ha-dhcp4-1.dhcp4-ha-dhcp4-hl.dhcptest1.svc.cluster.local:8000/"

		if ha.Peers[0].URL != expected0 {
			t.Errorf("peer 0 URL = %q, want %q", ha.Peers[0].URL, expected0)
		}
		if ha.Peers[1].URL != expected1 {
			t.Errorf("peer 1 URL = %q, want %q", ha.Peers[1].URL, expected1)
		}
	})

	t.Run("does not overwrite explicit URLs", func(t *testing.T) {
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Mode:           "load-balancing",
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", URL: "http://custom:9000/", Role: "primary"},
				{Name: "server2", Role: "secondary"},
			},
		}
		fillPeerURLs(ha, "ns", "myserver", "dhcp4", 8000)

		if ha.Peers[0].URL != "http://custom:9000/" {
			t.Errorf("peer 0 URL was overwritten: %q", ha.Peers[0].URL)
		}
		expected1 := "http://myserver-dhcp4-1.myserver-dhcp4-hl.ns.svc.cluster.local:8000/"
		if ha.Peers[1].URL != expected1 {
			t.Errorf("peer 1 URL = %q, want %q", ha.Peers[1].URL, expected1)
		}
	})

	t.Run("uses custom port", func(t *testing.T) {
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Peers: []keav1alpha1.HAPeer{
				{Name: "server1", Role: "primary"},
			},
		}
		fillPeerURLs(ha, "ns", "myserver", "dhcp4", 9090)

		expected := "http://myserver-dhcp4-0.myserver-dhcp4-hl.ns.svc.cluster.local:9090/"
		if ha.Peers[0].URL != expected {
			t.Errorf("peer 0 URL = %q, want %q", ha.Peers[0].URL, expected)
		}
	})
}
