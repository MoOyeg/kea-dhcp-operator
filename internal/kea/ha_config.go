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
	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

const (
	hookLibPath      = "/usr/lib/kea/hooks/"
	haHookLib        = hookLibPath + "libdhcp_ha.so"
	leaseCmdsHookLib = hookLibPath + "libdhcp_lease_cmds.so"
	mysqlHookLib     = hookLibPath + "libdhcp_mysql.so"
	pgsqlHookLib     = hookLibPath + "libdhcp_pgsql.so"
)

// renderHAHooks returns the hook library entries for HA: libdhcp_lease_cmds.so
// and libdhcp_ha.so with the HA configuration nested under parameters.
// If serverNameOverride is non-empty, it replaces the this-server-name from the spec.
// If haListenerPort > 0, a dedicated HTTP listener is configured for HA peer communication.
func renderHAHooks(ha *keav1alpha1.HAConfig, serverNameOverride string, haListenerPort int32) []map[string]interface{} {
	if ha == nil {
		return nil
	}

	serverName := ha.ThisServerName
	if serverNameOverride != "" {
		serverName = serverNameOverride
	}

	// Build the HA configuration map
	haConfig := map[string]interface{}{
		"this-server-name": serverName,
	}
	setIfNotEmpty(haConfig, "mode", ha.Mode)
	setIfNotNil(haConfig, "heartbeat-delay", ha.HeartbeatDelay)
	setIfNotNil(haConfig, "max-response-delay", ha.MaxResponseDelay)
	setIfNotNil(haConfig, "max-ack-delay", ha.MaxAckDelay)
	setIfNotNil(haConfig, "max-unacked-clients", ha.MaxUnackedClients)
	setIfNotNil(haConfig, "send-lease-updates", ha.SendLeaseUpdates)
	setIfNotNil(haConfig, "sync-leases", ha.SyncLeases)
	setIfNotNil(haConfig, "sync-timeout", ha.SyncTimeout)
	setIfNotNil(haConfig, "sync-page-limit", ha.SyncPageLimit)
	setIfNotNil(haConfig, "delayed-updates-limit", ha.DelayedUpdatesLimit)

	// Peers
	if len(ha.Peers) > 0 {
		peers := make([]map[string]interface{}, 0, len(ha.Peers))
		for _, p := range ha.Peers {
			pm := map[string]interface{}{
				"name": p.Name,
				"url":  p.URL,
				"role": p.Role,
			}
			setIfNotNil(pm, "auto-failover", p.AutoFailover)
			peers = append(peers, pm)
		}
		haConfig["peers"] = peers
	}

	// Multi-threading
	if ha.MultiThreading != nil {
		mt := map[string]interface{}{}
		setIfNotNil(mt, "enable-multi-threading", ha.MultiThreading.EnableMultiThreading)
		setIfNotNil(mt, "http-client-threads", ha.MultiThreading.HTTPClientThreads)
		setIfNotNil(mt, "http-listener-threads", ha.MultiThreading.HTTPListenerThreads)
		if len(mt) > 0 {
			haConfig["multi-threading"] = mt
		}
	}

	// Configure dedicated HTTP listener for HA peer communication when using HTTP control socket.
	if haListenerPort > 0 {
		haConfig["http-dedicated-listener"] = true
		haConfig["http-listener-address"] = "0.0.0.0"
		haConfig["http-listener-port"] = haListenerPort
	}

	// Return two hook entries: lease_cmds first, then HA
	return []map[string]interface{}{
		{
			"library": leaseCmdsHookLib,
		},
		{
			"library": haHookLib,
			"parameters": map[string]interface{}{
				"high-availability": []map[string]interface{}{haConfig},
			},
		},
	}
}
