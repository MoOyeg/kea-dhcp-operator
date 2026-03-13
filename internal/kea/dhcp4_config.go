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

// DBCredentials holds resolved database credentials from a Secret.
type DBCredentials struct {
	User     string
	Password string
}

// Dhcp4ConfigRenderer renders a Kea DHCPv4 JSON configuration from a KeaDhcp4ServerSpec.
type Dhcp4ConfigRenderer struct {
	Spec *keav1alpha1.KeaDhcp4ServerSpec
	// LeaseDBCreds holds resolved credentials for the lease database.
	LeaseDBCreds DBCredentials
	// HostsDBCreds holds resolved credentials for the hosts database.
	HostsDBCreds DBCredentials
	// HostsDBsCreds holds resolved credentials for each hosts-databases entry (by index).
	HostsDBsCreds []DBCredentials
}

// NewDhcp4ConfigRenderer creates a new Dhcp4ConfigRenderer.
func NewDhcp4ConfigRenderer(spec *keav1alpha1.KeaDhcp4ServerSpec) *Dhcp4ConfigRenderer {
	return &Dhcp4ConfigRenderer{Spec: spec}
}

// RenderJSON builds the complete {"Dhcp4": {...}} JSON configuration.
func (r *Dhcp4ConfigRenderer) RenderJSON() ([]byte, error) {
	return r.renderJSONInternal("")
}

// RenderJSONWithServerName builds the config with a specific this-server-name
// override for the HA hooks section. Used to create per-pod configs in
// StatefulSet HA deployments.
func (r *Dhcp4ConfigRenderer) RenderJSONWithServerName(serverName string) ([]byte, error) {
	return r.renderJSONInternal(serverName)
}

func (r *Dhcp4ConfigRenderer) renderJSONInternal(serverNameOverride string) ([]byte, error) {
	s := r.Spec
	dhcp4 := map[string]interface{}{}

	// Interfaces config
	dhcp4["interfaces-config"] = r.renderInterfacesConfig()

	// Lease database
	if s.LeaseDatabase != nil {
		dhcp4["lease-database"] = renderDatabaseConfig(s.LeaseDatabase, r.LeaseDBCreds.User, r.LeaseDBCreds.Password)
	}

	// Hosts database (single)
	if s.HostsDatabase != nil {
		dhcp4["hosts-database"] = renderDatabaseConfig(s.HostsDatabase, r.HostsDBCreds.User, r.HostsDBCreds.Password)
	}

	// Hosts databases (multiple)
	if len(s.HostsDatabases) > 0 {
		dbs := make([]map[string]interface{}, 0, len(s.HostsDatabases))
		for i := range s.HostsDatabases {
			var creds DBCredentials
			if i < len(r.HostsDBsCreds) {
				creds = r.HostsDBsCreds[i]
			}
			dbs = append(dbs, renderDatabaseConfig(&s.HostsDatabases[i], creds.User, creds.Password))
		}
		dhcp4["hosts-databases"] = dbs
	}

	// Subnets
	if subnets := r.renderSubnets4(); subnets != nil {
		dhcp4["subnet4"] = subnets
	}

	// Shared networks
	if sn := r.renderSharedNetworks4(); sn != nil {
		dhcp4["shared-networks"] = sn
	}

	// Option data
	if opts := renderOptionData(s.OptionData); opts != nil {
		dhcp4["option-data"] = opts
	}

	// Option definitions
	if defs := renderOptionDef(s.OptionDef); defs != nil {
		dhcp4["option-def"] = defs
	}

	// Client classes
	if classes := renderClientClasses(s.ClientClasses); classes != nil {
		dhcp4["client-classes"] = classes
	}

	// Global reservations
	if res := renderReservations(s.Reservations); res != nil {
		dhcp4["reservations"] = res
	}

	// Host reservation identifiers
	if len(s.HostReservationIdentifiers) > 0 {
		dhcp4["host-reservation-identifiers"] = s.HostReservationIdentifiers
	}

	// Hooks libraries - start with user-specified, add DB hook if needed, then HA hooks
	hooks := renderHooksLibraries(s.HooksLibraries)
	if dbHook := renderDBHook(s.LeaseDatabase); dbHook != nil {
		hooks = append(hooks, dbHook)
	}
	if s.HighAvailability != nil {
		var haPort int32
		if s.ControlSocket != nil && s.ControlSocket.SocketPort != nil {
			haPort = *s.ControlSocket.SocketPort
		}
		haHooks := renderHAHooks(s.HighAvailability, serverNameOverride, haPort)
		hooks = append(hooks, haHooks...)
	}
	if len(hooks) > 0 {
		dhcp4["hooks-libraries"] = hooks
	}

	// Render the control socket. Unix sockets don't conflict with the HA
	// http-dedicated-listener and are needed for stork-agent monitoring.
	if cs := renderControlSocket(s.ControlSocket); cs != nil {
		dhcp4["control-socket"] = cs
	}

	// Loggers
	if loggers := renderLoggers(s.Loggers); loggers != nil {
		dhcp4["loggers"] = loggers
	}

	// Global parameters
	setIfNotNil(dhcp4, "valid-lifetime", s.ValidLifetime)
	setIfNotNil(dhcp4, "min-valid-lifetime", s.MinValidLifetime)
	setIfNotNil(dhcp4, "max-valid-lifetime", s.MaxValidLifetime)
	setIfNotNil(dhcp4, "renew-timer", s.RenewTimer)
	setIfNotNil(dhcp4, "rebind-timer", s.RebindTimer)
	setIfNotNil(dhcp4, "calculate-tee-times", s.CalculateTeeTimes)
	setIfNotNil(dhcp4, "t1-percent", s.T1Percent)
	setIfNotNil(dhcp4, "t2-percent", s.T2Percent)
	setIfNotNil(dhcp4, "authoritative", s.Authoritative)
	setIfNotNil(dhcp4, "match-client-id", s.MatchClientID)
	setIfNotEmpty(dhcp4, "server-tag", s.ServerTag)
	setIfNotEmpty(dhcp4, "next-server", s.NextServer)
	setIfNotEmpty(dhcp4, "boot-file-name", s.BootFileName)
	setIfNotEmpty(dhcp4, "server-hostname", s.ServerHostname)

	// DDNS parameters
	setIfNotNil(dhcp4, "ddns-send-updates", s.DDNSSendUpdates)
	setIfNotNil(dhcp4, "ddns-override-no-update", s.DDNSOverrideNoUpdate)
	setIfNotNil(dhcp4, "ddns-override-client-update", s.DDNSOverrideClientUpdate)
	setIfNotEmpty(dhcp4, "ddns-replace-client-name", s.DDNSReplaceClientName)
	setIfNotEmpty(dhcp4, "ddns-generated-prefix", s.DDNSGeneratedPrefix)
	setIfNotEmpty(dhcp4, "ddns-qualifying-suffix", s.DDNSQualifyingSuffix)
	setIfNotNil(dhcp4, "ddns-use-conflict-resolution", s.DDNSUseConflictResolution)

	// Multi-threading
	if s.MultiThreading != nil {
		mt := map[string]interface{}{}
		setIfNotNil(mt, "enable-multi-threading", s.MultiThreading.EnableMultiThreading)
		setIfNotNil(mt, "thread-pool-size", s.MultiThreading.ThreadPoolSize)
		setIfNotNil(mt, "packet-queue-size", s.MultiThreading.PacketQueueSize)
		if len(mt) > 0 {
			dhcp4["multi-threading"] = mt
		}
	}

	root := map[string]interface{}{
		"Dhcp4": dhcp4,
	}
	return json.MarshalIndent(root, "", "  ")
}

// renderInterfacesConfig builds the interfaces-config section.
func (r *Dhcp4ConfigRenderer) renderInterfacesConfig() map[string]interface{} {
	ic := r.Spec.InterfacesConfig
	m := map[string]interface{}{
		"interfaces": ic.Interfaces,
	}
	setIfNotEmpty(m, "dhcp-socket-type", ic.DHCPSocketType)
	setIfNotEmpty(m, "outbound-interface", ic.OutboundInterface)
	setIfNotNil(m, "re-detect", ic.ReDetect)
	setIfNotNil(m, "service-sockets-require-all", ic.ServiceSocketsRequireAll)
	setIfNotNil(m, "service-sockets-max-retries", ic.ServiceSocketsMaxRetries)
	setIfNotNil(m, "service-sockets-retry-wait-time", ic.ServiceSocketsRetryWaitTime)
	return m
}

// renderSubnets4 converts the Subnet4 slice to JSON-ready maps.
func (r *Dhcp4ConfigRenderer) renderSubnets4() []map[string]interface{} {
	if len(r.Spec.Subnet4) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(r.Spec.Subnet4))
	for _, sub := range r.Spec.Subnet4 {
		result = append(result, r.renderSubnet4(sub))
	}
	return result
}

// renderSubnet4 converts a single Subnet4 to a JSON-ready map.
func (r *Dhcp4ConfigRenderer) renderSubnet4(sub keav1alpha1.Subnet4) map[string]interface{} {
	sm := map[string]interface{}{
		"id":     sub.ID,
		"subnet": sub.Subnet,
	}

	if len(sub.Pools) > 0 {
		pools := make([]map[string]interface{}, 0, len(sub.Pools))
		for _, p := range sub.Pools {
			pools = append(pools, r.renderPool4(p))
		}
		sm["pools"] = pools
	}

	if opts := renderOptionData(sub.OptionData); opts != nil {
		sm["option-data"] = opts
	}
	if res := renderReservations(sub.Reservations); res != nil {
		sm["reservations"] = res
	}
	setIfNotEmpty(sm, "client-class", sub.ClientClass)
	if len(sub.RequireClientClasses) > 0 {
		sm["require-client-classes"] = sub.RequireClientClasses
	}
	setIfNotEmpty(sm, "interface", sub.Interface)
	if sub.Relay != nil && len(sub.Relay.IPAddresses) > 0 {
		sm["relay"] = map[string]interface{}{
			"ip-addresses": sub.Relay.IPAddresses,
		}
	}
	setIfNotNil(sm, "valid-lifetime", sub.ValidLifetime)
	setIfNotNil(sm, "min-valid-lifetime", sub.MinValidLifetime)
	setIfNotNil(sm, "max-valid-lifetime", sub.MaxValidLifetime)
	setIfNotNil(sm, "renew-timer", sub.RenewTimer)
	setIfNotNil(sm, "rebind-timer", sub.RebindTimer)
	setIfNotNil(sm, "authoritative", sub.Authoritative)
	setIfNotEmpty(sm, "reservation-mode", sub.ReservationMode)
	setIfNotNil(sm, "match-client-id", sub.MatchClientID)
	setIfNotEmpty(sm, "next-server", sub.NextServer)
	setIfNotEmpty(sm, "boot-file-name", sub.BootFileName)
	setIfNotEmpty(sm, "server-hostname", sub.ServerHostname)
	setIfNotEmpty(sm, "user-context", sub.UserContext)
	return sm
}

// renderPool4 converts a Pool4 to a JSON-ready map.
func (r *Dhcp4ConfigRenderer) renderPool4(p keav1alpha1.Pool4) map[string]interface{} {
	pm := map[string]interface{}{
		"pool": p.Pool,
	}
	if opts := renderOptionData(p.OptionData); opts != nil {
		pm["option-data"] = opts
	}
	setIfNotEmpty(pm, "client-class", p.ClientClass)
	if len(p.RequireClientClasses) > 0 {
		pm["require-client-classes"] = p.RequireClientClasses
	}
	return pm
}

// renderSharedNetworks4 converts SharedNetwork4 slice to JSON-ready maps.
func (r *Dhcp4ConfigRenderer) renderSharedNetworks4() []map[string]interface{} {
	if len(r.Spec.SharedNetworks) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(r.Spec.SharedNetworks))
	for _, sn := range r.Spec.SharedNetworks {
		snm := map[string]interface{}{
			"name": sn.Name,
		}
		if len(sn.Subnet4) > 0 {
			subs := make([]map[string]interface{}, 0, len(sn.Subnet4))
			for _, sub := range sn.Subnet4 {
				subs = append(subs, r.renderSubnet4(sub))
			}
			snm["subnet4"] = subs
		}
		setIfNotEmpty(snm, "interface", sn.Interface)
		if opts := renderOptionData(sn.OptionData); opts != nil {
			snm["option-data"] = opts
		}
		if sn.Relay != nil && len(sn.Relay.IPAddresses) > 0 {
			snm["relay"] = map[string]interface{}{
				"ip-addresses": sn.Relay.IPAddresses,
			}
		}
		setIfNotEmpty(snm, "client-class", sn.ClientClass)
		if len(sn.RequireClientClasses) > 0 {
			snm["require-client-classes"] = sn.RequireClientClasses
		}
		setIfNotNil(snm, "valid-lifetime", sn.ValidLifetime)
		setIfNotNil(snm, "min-valid-lifetime", sn.MinValidLifetime)
		setIfNotNil(snm, "max-valid-lifetime", sn.MaxValidLifetime)
		setIfNotNil(snm, "renew-timer", sn.RenewTimer)
		setIfNotNil(snm, "rebind-timer", sn.RebindTimer)
		setIfNotNil(snm, "authoritative", sn.Authoritative)
		result = append(result, snm)
	}
	return result
}
