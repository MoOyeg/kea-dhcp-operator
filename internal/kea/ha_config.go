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
	"fmt"
	"net"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// DefaultHAListenerPort is the HA listener's port when nothing sets it, and
// the port Kea uses for an HTTP control socket without socket-port.
const DefaultHAListenerPort int32 = 8000

const socketTypeHTTP = "http"

// HAListenerPort returns the port of the HA hook's dedicated HTTP listener
// and whether the rendered configuration must set it. If it need not, Kea
// takes the port from this server's peer URL, which the operator fills in
// with the same port.
//
// high-availability.http-listener-port wins. Without it, control-socket's
// socket-port is used, as before that field existed, so existing servers keep
// their rendered configuration and their Services.
func HAListenerPort(ha *keav1alpha1.HAConfig, cs *keav1alpha1.ControlSocket) (int32, bool) {
	if ha != nil && ha.HTTPListenerPort != nil {
		return *ha.HTTPListenerPort, true
	}
	if cs != nil && cs.SocketPort != nil {
		return *cs.SocketPort, true
	}
	return DefaultHAListenerPort, false
}

// checkHAListenerPort rejects an HA server whose HTTP control socket would
// bind the HA listener's port on an address the listener also covers. The HA
// listener binds 0.0.0.0 (or the pod address), so an HTTP control socket on
// the same port only coexists with it on a loopback address, Kea's default
// (127.0.0.1). Kea 3.0.2 otherwise fails to start with "CmdHttpListener::run
// failed: unable to setup TCP acceptor ... Address in use".
func checkHAListenerPort(ha *keav1alpha1.HAConfig, cs *keav1alpha1.ControlSocket) error {
	if ha == nil || cs == nil || cs.SocketType != socketTypeHTTP || isLoopback(cs.SocketAddress) {
		return nil
	}
	csPort := DefaultHAListenerPort
	if cs.SocketPort != nil {
		csPort = *cs.SocketPort
	}
	if haPort, _ := HAListenerPort(ha, cs); haPort == csPort {
		return fmt.Errorf("the HTTP control-socket (socket-address %s) and the HA listener both use port %d; "+
			"set high-availability.http-listener-port to a different port", cs.SocketAddress, csPort)
	}
	return nil
}

// isLoopback reports whether a control-socket address is a loopback address.
// An empty address is Kea's default, 127.0.0.1.
func isLoopback(addr string) bool {
	if addr == "" {
		return true
	}
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}

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
				fieldName: p.Name,
				"url":     p.URL,
				"role":    p.Role,
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
		haConfig["http-listener-address"] = listenAnyIP4
		haConfig["http-listener-port"] = haListenerPort
	}

	// Return two hook entries: lease_cmds first, then HA
	return []map[string]interface{}{
		{
			fieldLibrary: leaseCmdsHookLib,
		},
		{
			fieldLibrary: haHookLib,
			"parameters": map[string]interface{}{
				"high-availability": []map[string]interface{}{haConfig},
			},
		},
	}
}
