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
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

func TestRenderHAHooks(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		result := renderHAHooks(nil, "", 0)
		if result != nil {
			t.Fatal("expected nil for nil input")
		}
	})

	t.Run("produces 2 hooks", func(t *testing.T) {
		autoFailover := true
		ha := &keav1alpha1.HAConfig{
			ThisServerName: "server1",
			Mode:           "load-balancing",
			Peers: []keav1alpha1.HAPeer{
				{
					Name:         "server1",
					URL:          "http://server1:8000/",
					Role:         "primary",
					AutoFailover: &autoFailover,
				},
				{
					Name:         "server2",
					URL:          "http://server2:8000/",
					Role:         "standby",
					AutoFailover: &autoFailover,
				},
			},
		}
		result := renderHAHooks(ha, "", 0)
		if len(result) != 2 {
			t.Fatalf("expected 2 hooks, got %d", len(result))
		}

		// First hook is lease_cmds
		if result[0]["library"] != "/usr/lib/kea/hooks/libdhcp_lease_cmds.so" {
			t.Errorf("expected first hook library to be lease_cmds, got %v", result[0]["library"])
		}
		// lease_cmds should not have parameters
		if _, ok := result[0]["parameters"]; ok {
			t.Error("expected lease_cmds hook not to have parameters")
		}

		// Second hook is ha
		if result[1]["library"] != "/usr/lib/kea/hooks/libdhcp_ha.so" {
			t.Errorf("expected second hook library to be ha, got %v", result[1]["library"])
		}
		// ha hook must have parameters
		params, ok := result[1]["parameters"].(map[string]interface{})
		if !ok {
			t.Fatal("expected ha hook to have parameters map")
		}
		haArr, ok := params["high-availability"].([]map[string]interface{})
		if !ok {
			t.Fatal("expected high-availability array in parameters")
		}
		if len(haArr) != 1 {
			t.Fatalf("expected 1 HA config, got %d", len(haArr))
		}
		if haArr[0]["this-server-name"] != "server1" {
			t.Errorf("expected this-server-name 'server1', got %v", haArr[0]["this-server-name"])
		}
	})
}

func TestRenderHAHooksFullConfig(t *testing.T) {
	autoFailover := true
	heartbeat := int32(10000)
	maxResponse := int32(60000)
	maxAck := int32(10000)
	maxUnacked := int32(10)
	sendLeaseUpdates := true
	syncLeases := true
	syncTimeout := int32(60000)
	syncPageLimit := int32(10000)
	delayedUpdatesLimit := int32(100)
	enableMT := true
	httpClientThreads := int32(4)
	httpListenerThreads := int32(4)

	ha := &keav1alpha1.HAConfig{
		ThisServerName:      "primary-server",
		Mode:                "hot-standby",
		HeartbeatDelay:      &heartbeat,
		MaxResponseDelay:    &maxResponse,
		MaxAckDelay:         &maxAck,
		MaxUnackedClients:   &maxUnacked,
		SendLeaseUpdates:    &sendLeaseUpdates,
		SyncLeases:          &syncLeases,
		SyncTimeout:         &syncTimeout,
		SyncPageLimit:       &syncPageLimit,
		DelayedUpdatesLimit: &delayedUpdatesLimit,
		Peers: []keav1alpha1.HAPeer{
			{
				Name:         "primary-server",
				URL:          "http://primary:8000/",
				Role:         "primary",
				AutoFailover: &autoFailover,
			},
			{
				Name:         "standby-server",
				URL:          "http://standby:8000/",
				Role:         "standby",
				AutoFailover: &autoFailover,
			},
		},
		MultiThreading: &keav1alpha1.HAMultiThreading{
			EnableMultiThreading: &enableMT,
			HTTPClientThreads:    &httpClientThreads,
			HTTPListenerThreads:  &httpListenerThreads,
		},
	}

	result := renderHAHooks(ha, "", 0)
	if len(result) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(result))
	}

	// Extract HA config from the second hook
	params := result[1]["parameters"].(map[string]interface{})
	haArr := params["high-availability"].([]map[string]interface{})
	haConfig := haArr[0]

	// Verify all HA parameters are rendered
	if haConfig["this-server-name"] != "primary-server" {
		t.Errorf("expected this-server-name 'primary-server', got %v", haConfig["this-server-name"])
	}
	if haConfig["mode"] != "hot-standby" {
		t.Errorf("expected mode 'hot-standby', got %v", haConfig["mode"])
	}
	if haConfig["heartbeat-delay"] != int32(10000) {
		t.Errorf("expected heartbeat-delay 10000, got %v", haConfig["heartbeat-delay"])
	}
	if haConfig["max-response-delay"] != int32(60000) {
		t.Errorf("expected max-response-delay 60000, got %v", haConfig["max-response-delay"])
	}
	if haConfig["max-ack-delay"] != int32(10000) {
		t.Errorf("expected max-ack-delay 10000, got %v", haConfig["max-ack-delay"])
	}
	if haConfig["max-unacked-clients"] != int32(10) {
		t.Errorf("expected max-unacked-clients 10, got %v", haConfig["max-unacked-clients"])
	}
	if haConfig["send-lease-updates"] != true {
		t.Errorf("expected send-lease-updates true, got %v", haConfig["send-lease-updates"])
	}
	if haConfig["sync-leases"] != true {
		t.Errorf("expected sync-leases true, got %v", haConfig["sync-leases"])
	}
	if haConfig["sync-timeout"] != int32(60000) {
		t.Errorf("expected sync-timeout 60000, got %v", haConfig["sync-timeout"])
	}
	if haConfig["sync-page-limit"] != int32(10000) {
		t.Errorf("expected sync-page-limit 10000, got %v", haConfig["sync-page-limit"])
	}
	if haConfig["delayed-updates-limit"] != int32(100) {
		t.Errorf("expected delayed-updates-limit 100, got %v", haConfig["delayed-updates-limit"])
	}

	// Verify peers
	peers, ok := haConfig["peers"].([]map[string]interface{})
	if !ok {
		t.Fatal("expected peers to be []map[string]interface{}")
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2 peers, got %d", len(peers))
	}
	if peers[0]["name"] != "primary-server" {
		t.Errorf("expected first peer name 'primary-server', got %v", peers[0]["name"])
	}
	if peers[0]["url"] != "http://primary:8000/" {
		t.Errorf("expected first peer url 'http://primary:8000/', got %v", peers[0]["url"])
	}
	if peers[0]["role"] != "primary" {
		t.Errorf("expected first peer role 'primary', got %v", peers[0]["role"])
	}
	if peers[0]["auto-failover"] != true {
		t.Errorf("expected auto-failover true, got %v", peers[0]["auto-failover"])
	}
	if peers[1]["name"] != "standby-server" {
		t.Errorf("expected second peer name 'standby-server', got %v", peers[1]["name"])
	}
	if peers[1]["role"] != "standby" {
		t.Errorf("expected second peer role 'standby', got %v", peers[1]["role"])
	}

	// Verify multi-threading
	mt, ok := haConfig["multi-threading"].(map[string]interface{})
	if !ok {
		t.Fatal("expected multi-threading map in HA config")
	}
	if mt["enable-multi-threading"] != true {
		t.Errorf("expected enable-multi-threading true, got %v", mt["enable-multi-threading"])
	}
	if mt["http-client-threads"] != int32(4) {
		t.Errorf("expected http-client-threads 4, got %v", mt["http-client-threads"])
	}
	if mt["http-listener-threads"] != int32(4) {
		t.Errorf("expected http-listener-threads 4, got %v", mt["http-listener-threads"])
	}
}
