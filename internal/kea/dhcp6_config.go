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

// Dhcp6ConfigRenderer renders a Kea DHCPv6 JSON configuration from a KeaDhcp6ServerSpec.
type Dhcp6ConfigRenderer struct {
	Spec *keav1alpha1.KeaDhcp6ServerSpec
	// LeaseDBCreds holds resolved credentials for the lease database.
	LeaseDBCreds DBCredentials
	// HostsDBCreds holds resolved credentials for the hosts database.
	HostsDBCreds DBCredentials
	// HostsDBsCreds holds resolved credentials for each hosts-databases entry (by index).
	HostsDBsCreds []DBCredentials
}

// NewDhcp6ConfigRenderer creates a new Dhcp6ConfigRenderer.
func NewDhcp6ConfigRenderer(spec *keav1alpha1.KeaDhcp6ServerSpec) *Dhcp6ConfigRenderer {
	return &Dhcp6ConfigRenderer{Spec: spec}
}

// RenderJSON builds the complete {"Dhcp6": {...}} JSON configuration.
func (r *Dhcp6ConfigRenderer) RenderJSON() ([]byte, error) {
	return r.renderJSONInternal("")
}

// RenderJSONWithServerName builds the config with a specific this-server-name
// override for the HA hooks section. Used to create per-pod configs in
// StatefulSet HA deployments.
func (r *Dhcp6ConfigRenderer) RenderJSONWithServerName(serverName string) ([]byte, error) {
	return r.renderJSONInternal(serverName)
}

func (r *Dhcp6ConfigRenderer) renderJSONInternal(serverNameOverride string) ([]byte, error) {
	s := r.Spec
	dhcp6 := map[string]interface{}{}

	// Interfaces config
	ic := s.InterfacesConfig
	icm := map[string]interface{}{
		"interfaces": ic.Interfaces,
	}
	setIfNotEmpty(icm, "dhcp-socket-type", ic.DHCPSocketType)
	setIfNotEmpty(icm, "outbound-interface", ic.OutboundInterface)
	setIfNotNil(icm, "re-detect", ic.ReDetect)
	setIfNotNil(icm, "service-sockets-require-all", ic.ServiceSocketsRequireAll)
	setIfNotNil(icm, "service-sockets-max-retries", ic.ServiceSocketsMaxRetries)
	setIfNotNil(icm, "service-sockets-retry-wait-time", ic.ServiceSocketsRetryWaitTime)
	dhcp6["interfaces-config"] = icm

	// Lease database
	if s.LeaseDatabase != nil {
		dhcp6["lease-database"] = renderDatabaseConfig(s.LeaseDatabase, r.LeaseDBCreds.User, r.LeaseDBCreds.Password)
	}

	// Hosts database (single)
	if s.HostsDatabase != nil {
		dhcp6["hosts-database"] = renderDatabaseConfig(s.HostsDatabase, r.HostsDBCreds.User, r.HostsDBCreds.Password)
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
		dhcp6["hosts-databases"] = dbs
	}

	// Subnets
	if subnets := r.renderSubnets6(); subnets != nil {
		dhcp6["subnet6"] = subnets
	}

	// Shared networks
	if sn := r.renderSharedNetworks6(); sn != nil {
		dhcp6["shared-networks"] = sn
	}

	// Option data
	if opts := renderOptionData(s.OptionData); opts != nil {
		dhcp6["option-data"] = opts
	}

	// Option definitions
	if defs := renderOptionDef(s.OptionDef); defs != nil {
		dhcp6["option-def"] = defs
	}

	// Client classes
	if classes := renderClientClasses(s.ClientClasses); classes != nil {
		dhcp6["client-classes"] = classes
	}

	// Global reservations
	if res := renderReservations(s.Reservations); res != nil {
		dhcp6["reservations"] = res
	}

	// Host reservation identifiers
	if len(s.HostReservationIdentifiers) > 0 {
		dhcp6["host-reservation-identifiers"] = s.HostReservationIdentifiers
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
		dhcp6["hooks-libraries"] = hooks
	}

	// Render the control socket. Unix sockets don't conflict with the HA
	// http-dedicated-listener and are needed for stork-agent monitoring.
	if cs := renderControlSocket(s.ControlSocket); cs != nil {
		dhcp6["control-socket"] = cs
	}

	// Loggers
	if loggers := renderLoggers(s.Loggers); loggers != nil {
		dhcp6["loggers"] = loggers
	}

	// Global parameters
	setIfNotNil(dhcp6, "preferred-lifetime", s.PreferredLifetime)
	setIfNotNil(dhcp6, "valid-lifetime", s.ValidLifetime)
	setIfNotNil(dhcp6, "min-valid-lifetime", s.MinValidLifetime)
	setIfNotNil(dhcp6, "max-valid-lifetime", s.MaxValidLifetime)
	setIfNotNil(dhcp6, "renew-timer", s.RenewTimer)
	setIfNotNil(dhcp6, "rebind-timer", s.RebindTimer)
	setIfNotNil(dhcp6, "calculate-tee-times", s.CalculateTeeTimes)
	setIfNotNil(dhcp6, "t1-percent", s.T1Percent)
	setIfNotNil(dhcp6, "t2-percent", s.T2Percent)
	setIfNotEmpty(dhcp6, "server-tag", s.ServerTag)
	setIfNotNil(dhcp6, "rapid-commit", s.RapidCommit)

	// DDNS parameters
	setIfNotNil(dhcp6, "ddns-send-updates", s.DDNSSendUpdates)
	setIfNotEmpty(dhcp6, "ddns-qualifying-suffix", s.DDNSQualifyingSuffix)

	// Multi-threading
	if s.MultiThreading != nil {
		mt := map[string]interface{}{}
		setIfNotNil(mt, "enable-multi-threading", s.MultiThreading.EnableMultiThreading)
		setIfNotNil(mt, "thread-pool-size", s.MultiThreading.ThreadPoolSize)
		setIfNotNil(mt, "packet-queue-size", s.MultiThreading.PacketQueueSize)
		if len(mt) > 0 {
			dhcp6["multi-threading"] = mt
		}
	}

	root := map[string]interface{}{
		"Dhcp6": dhcp6,
	}
	return json.MarshalIndent(root, "", "  ")
}

// renderSubnets6 converts the Subnet6 slice to JSON-ready maps.
func (r *Dhcp6ConfigRenderer) renderSubnets6() []map[string]interface{} {
	if len(r.Spec.Subnet6) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(r.Spec.Subnet6))
	for _, sub := range r.Spec.Subnet6 {
		result = append(result, r.renderSubnet6(sub))
	}
	return result
}

// renderSubnet6 converts a single Subnet6 to a JSON-ready map.
func (r *Dhcp6ConfigRenderer) renderSubnet6(sub keav1alpha1.Subnet6) map[string]interface{} {
	sm := map[string]interface{}{
		"id":     sub.ID,
		"subnet": sub.Subnet,
	}

	if len(sub.Pools) > 0 {
		pools := make([]map[string]interface{}, 0, len(sub.Pools))
		for _, p := range sub.Pools {
			pools = append(pools, r.renderPool6(p))
		}
		sm["pools"] = pools
	}

	if len(sub.PDPools) > 0 {
		pdPools := make([]map[string]interface{}, 0, len(sub.PDPools))
		for _, pd := range sub.PDPools {
			pdPools = append(pdPools, r.renderPDPool(pd))
		}
		sm["pd-pools"] = pdPools
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
	setIfNotEmpty(sm, "interface-id", sub.InterfaceID)
	if sub.Relay != nil && len(sub.Relay.IPAddresses) > 0 {
		sm["relay"] = map[string]interface{}{
			"ip-addresses": sub.Relay.IPAddresses,
		}
	}
	setIfNotNil(sm, "preferred-lifetime", sub.PreferredLifetime)
	setIfNotNil(sm, "valid-lifetime", sub.ValidLifetime)
	setIfNotNil(sm, "min-valid-lifetime", sub.MinValidLifetime)
	setIfNotNil(sm, "max-valid-lifetime", sub.MaxValidLifetime)
	setIfNotNil(sm, "renew-timer", sub.RenewTimer)
	setIfNotNil(sm, "rebind-timer", sub.RebindTimer)
	setIfNotNil(sm, "rapid-commit", sub.RapidCommit)
	setIfNotEmpty(sm, "reservation-mode", sub.ReservationMode)
	return sm
}

// renderPool6 converts a Pool6 to a JSON-ready map.
func (r *Dhcp6ConfigRenderer) renderPool6(p keav1alpha1.Pool6) map[string]interface{} {
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

// renderPDPool converts a PDPool to a JSON-ready map.
func (r *Dhcp6ConfigRenderer) renderPDPool(pd keav1alpha1.PDPool) map[string]interface{} {
	pm := map[string]interface{}{
		"prefix":        pd.Prefix,
		"prefix-len":    pd.PrefixLen,
		"delegated-len": pd.DelegatedLen,
	}
	setIfNotEmpty(pm, "excluded-prefix", pd.ExcludedPrefix)
	setIfNotNil(pm, "excluded-prefix-len", pd.ExcludedPrefixLen)
	if opts := renderOptionData(pd.OptionData); opts != nil {
		pm["option-data"] = opts
	}
	setIfNotEmpty(pm, "client-class", pd.ClientClass)
	return pm
}

// renderSharedNetworks6 converts SharedNetwork6 slice to JSON-ready maps.
func (r *Dhcp6ConfigRenderer) renderSharedNetworks6() []map[string]interface{} {
	if len(r.Spec.SharedNetworks) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(r.Spec.SharedNetworks))
	for _, sn := range r.Spec.SharedNetworks {
		snm := map[string]interface{}{
			"name": sn.Name,
		}
		if len(sn.Subnet6) > 0 {
			subs := make([]map[string]interface{}, 0, len(sn.Subnet6))
			for _, sub := range sn.Subnet6 {
				subs = append(subs, r.renderSubnet6(sub))
			}
			snm["subnet6"] = subs
		}
		setIfNotEmpty(snm, "interface", sn.Interface)
		setIfNotEmpty(snm, "interface-id", sn.InterfaceID)
		if opts := renderOptionData(sn.OptionData); opts != nil {
			snm["option-data"] = opts
		}
		if sn.Relay != nil && len(sn.Relay.IPAddresses) > 0 {
			snm["relay"] = map[string]interface{}{
				"ip-addresses": sn.Relay.IPAddresses,
			}
		}
		setIfNotEmpty(snm, "client-class", sn.ClientClass)
		setIfNotNil(snm, "preferred-lifetime", sn.PreferredLifetime)
		setIfNotNil(snm, "valid-lifetime", sn.ValidLifetime)
		setIfNotNil(snm, "renew-timer", sn.RenewTimer)
		setIfNotNil(snm, "rebind-timer", sn.RebindTimer)
		setIfNotNil(snm, "rapid-commit", sn.RapidCommit)
		result = append(result, snm)
	}
	return result
}
