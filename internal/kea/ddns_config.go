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

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// DdnsConfigRenderer renders a Kea DHCP-DDNS (D2) JSON configuration
// from a KeaDhcpDdnsSpec.
type DdnsConfigRenderer struct {
	Spec *keav1alpha1.KeaDhcpDdnsSpec
	// ResolvedTSIGSecrets maps TSIG key name to the resolved secret value
	// from SecretRef.
	ResolvedTSIGSecrets map[string]string
}

// NewDdnsConfigRenderer creates a new DdnsConfigRenderer.
func NewDdnsConfigRenderer(spec *keav1alpha1.KeaDhcpDdnsSpec) *DdnsConfigRenderer {
	return &DdnsConfigRenderer{Spec: spec}
}

// RenderJSON builds the complete {"DhcpDdns": {...}} JSON configuration.
func (r *DdnsConfigRenderer) RenderJSON() ([]byte, error) {
	s := r.Spec
	ddns := map[string]interface{}{}

	// IP address and port
	setIfNotEmpty(ddns, "ip-address", s.IPAddress)
	setIfNotNil(ddns, "port", s.Port)

	// DNS server timeout
	setIfNotNil(ddns, "dns-server-timeout", s.DNSServerTimeout)

	// NCR protocol and format
	setIfNotEmpty(ddns, "ncr-protocol", s.NCRProtocol)
	setIfNotEmpty(ddns, "ncr-format", s.NCRFormat)

	// TSIG keys
	if len(s.TSIGKeys) > 0 {
		keys := make([]map[string]interface{}, 0, len(s.TSIGKeys))
		for _, k := range s.TSIGKeys {
			km := map[string]interface{}{
				"name":      k.Name,
				"algorithm": k.Algorithm,
			}
			setIfNotNil(km, "digest-bits", k.DigestBits)
			if r.ResolvedTSIGSecrets != nil {
				setIfNotEmpty(km, "secret", r.ResolvedTSIGSecrets[k.Name])
			}
			keys = append(keys, km)
		}
		ddns["tsig-keys"] = keys
	}

	// Forward DDNS
	if s.ForwardDDNS != nil {
		ddns["forward-ddns"] = r.renderDDNSConfig(s.ForwardDDNS)
	}

	// Reverse DDNS
	if s.ReverseDDNS != nil {
		ddns["reverse-ddns"] = r.renderDDNSConfig(s.ReverseDDNS)
	}

	// Control socket
	if cs := renderControlSocket(s.ControlSocket); cs != nil {
		ddns["control-socket"] = cs
	}

	// Hooks libraries
	if hooks := renderHooksLibraries(s.HooksLibraries); hooks != nil {
		ddns["hooks-libraries"] = hooks
	}

	// Loggers
	if loggers := renderLoggers(s.Loggers); loggers != nil {
		ddns["loggers"] = loggers
	}

	root := map[string]interface{}{
		"DhcpDdns": ddns,
	}
	return json.MarshalIndent(root, "", "  ")
}

// renderDDNSConfig converts a DDNSConfig to a JSON-ready map.
func (r *DdnsConfigRenderer) renderDDNSConfig(cfg *keav1alpha1.DDNSConfig) map[string]interface{} {
	if cfg == nil {
		return map[string]interface{}{}
	}
	m := map[string]interface{}{}
	if len(cfg.DDNSDomains) > 0 {
		domains := make([]map[string]interface{}, 0, len(cfg.DDNSDomains))
		for _, d := range cfg.DDNSDomains {
			dm := map[string]interface{}{
				"name": d.Name,
			}
			setIfNotEmpty(dm, "key-name", d.KeyName)
			if len(d.DNSServers) > 0 {
				servers := make([]map[string]interface{}, 0, len(d.DNSServers))
				for _, srv := range d.DNSServers {
					sm := map[string]interface{}{
						"ip-address": srv.IPAddress,
					}
					setIfNotNil(sm, "port", srv.Port)
					servers = append(servers, sm)
				}
				dm["dns-servers"] = servers
			}
			domains = append(domains, dm)
		}
		m["ddns-domains"] = domains
	}
	return m
}
