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

// TestDhcpSamplesAcceptedByKea renders the DHCPv4/DHCPv6 sample CRs in
// config/samples (which also become the CSV alm-examples the console offers)
// and runs them through the shipped kea-dhcp4/kea-dhcp6 parsers. `-t` checks
// that the configured interfaces exist, so the NAD interface is replaced with
// "*". The DHCP-DDNS and Control Agent samples are covered by
// TestDdnsConfigAcceptedByKea and TestCtrlAgentConfigAcceptedByKea.
// It runs with KEA_CONFIG_CHECK=1 (see keaContainerCheck).
func TestDhcpSamplesAcceptedByKea(t *testing.T) {
	k := keaContainerCheck(t)
	anyIface := []string{"*"}

	dhcp4 := map[string]*keav1alpha1.KeaDhcp4ServerSpec{}
	dhcp6 := map[string]*keav1alpha1.KeaDhcp6ServerSpec{}
	for _, f := range []string{"kea_v1alpha1_keadhcp4server.yaml", "kea_v1alpha1_keadhcp4server_ha.yaml"} {
		var o keav1alpha1.KeaDhcp4Server
		loadSample(t, f, &o)
		dhcp4[f] = &o.Spec
	}
	var o6 keav1alpha1.KeaDhcp6Server
	loadSample(t, "kea_v1alpha1_keadhcp6server.yaml", &o6)
	dhcp6["kea_v1alpha1_keadhcp6server.yaml"] = &o6.Spec
	var ks keav1alpha1.KeaServer
	loadSample(t, "kea_v1alpha1_keaserver.yaml", &ks)
	if ks.Spec.Dhcp4 != nil {
		dhcp4["kea_v1alpha1_keaserver.yaml dhcp4"] = ks.Spec.Dhcp4
	}
	if ks.Spec.Dhcp6 != nil {
		dhcp6["kea_v1alpha1_keaserver.yaml dhcp6"] = ks.Spec.Dhcp6
	}

	image4, image6 := k.image(t, resources.ComponentDhcp4), k.image(t, resources.ComponentDhcp6)
	for name, spec := range dhcp4 {
		t.Run(name, func(t *testing.T) {
			spec.InterfacesConfig.Interfaces = anyIface
			data, err := NewDhcp4ConfigRenderer(spec).RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			keaParserAccepts(t, k, image4, "kea-dhcp4", data)
		})
	}
	for name, spec := range dhcp6 {
		t.Run(name, func(t *testing.T) {
			spec.InterfacesConfig.Interfaces = anyIface
			data, err := NewDhcp6ConfigRenderer(spec).RenderJSON()
			if err != nil {
				t.Fatalf("RenderJSON failed: %v", err)
			}
			keaParserAccepts(t, k, image6, "kea-dhcp6", data)
		})
	}
}
