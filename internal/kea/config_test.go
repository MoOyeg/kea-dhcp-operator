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
	"encoding/hex"
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// --- pointer helpers ---

func int32Ptr(v int32) *int32    { return &v }
func int64Ptr(v int64) *int64    { return &v }
func boolPtr(v bool) *bool       { return &v }
func stringPtr(v string) *string { return &v }

// --- ComputeHash tests ---

func TestComputeHash(t *testing.T) {
	t.Run("returns 16 hex characters", func(t *testing.T) {
		hash := ComputeHash([]byte("hello world"))
		if len(hash) != 16 {
			t.Fatalf("expected hash length 16, got %d: %q", len(hash), hash)
		}
		// verify it is valid hex
		if _, err := hex.DecodeString(hash); err != nil {
			t.Fatalf("hash is not valid hex: %v", err)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		h1 := ComputeHash([]byte("deterministic input"))
		h2 := ComputeHash([]byte("deterministic input"))
		if h1 != h2 {
			t.Fatalf("expected same hash for same input, got %q vs %q", h1, h2)
		}
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		h1 := ComputeHash([]byte("input-a"))
		h2 := ComputeHash([]byte("input-b"))
		if h1 == h2 {
			t.Fatalf("expected different hashes for different inputs, both are %q", h1)
		}
	})
}

// --- setIfNotNil tests ---

func TestSetIfNotNil(t *testing.T) {
	t.Run("nil pointer does not set key", func(t *testing.T) {
		m := map[string]interface{}{}
		var p *int32
		setIfNotNil(m, "key", p)
		if _, ok := m["key"]; ok {
			t.Fatal("expected key not to be set for nil pointer")
		}
	})

	t.Run("nil interface does not set key", func(t *testing.T) {
		m := map[string]interface{}{}
		setIfNotNil(m, "key", nil)
		if _, ok := m["key"]; ok {
			t.Fatal("expected key not to be set for nil interface")
		}
	})

	t.Run("non-nil int32 pointer dereferences and sets", func(t *testing.T) {
		m := map[string]interface{}{}
		v := int32(42)
		setIfNotNil(m, "port", &v)
		got, ok := m["port"]
		if !ok {
			t.Fatal("expected key to be set")
		}
		if got != int32(42) {
			t.Fatalf("expected 42, got %v", got)
		}
	})

	t.Run("non-nil bool pointer dereferences and sets", func(t *testing.T) {
		m := map[string]interface{}{}
		v := true
		setIfNotNil(m, "enabled", &v)
		got, ok := m["enabled"]
		if !ok {
			t.Fatal("expected key to be set")
		}
		if got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})

	t.Run("non-pointer value is stored directly", func(t *testing.T) {
		m := map[string]interface{}{}
		setIfNotNil(m, "count", 99)
		got, ok := m["count"]
		if !ok {
			t.Fatal("expected key to be set")
		}
		if got != 99 {
			t.Fatalf("expected 99, got %v", got)
		}
	})
}

// --- setIfNotEmpty tests ---

func TestSetIfNotEmpty(t *testing.T) {
	t.Run("empty string does not set key", func(t *testing.T) {
		m := map[string]interface{}{}
		setIfNotEmpty(m, "name", "")
		if _, ok := m["name"]; ok {
			t.Fatal("expected key not to be set for empty string")
		}
	})

	t.Run("non-empty string sets key", func(t *testing.T) {
		m := map[string]interface{}{}
		setIfNotEmpty(m, "name", "kea-dhcp4")
		got, ok := m["name"]
		if !ok {
			t.Fatal("expected key to be set")
		}
		if got != "kea-dhcp4" {
			t.Fatalf("expected 'kea-dhcp4', got %v", got)
		}
	})
}

// --- renderDatabaseConfig tests ---

func TestRenderDatabaseConfig(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		result := renderDatabaseConfig(nil, "", "")
		if result != nil {
			t.Fatal("expected nil for nil input")
		}
	})

	t.Run("minimal memfile config", func(t *testing.T) {
		db := &keav1alpha1.DatabaseConfig{
			Type: keav1alpha1.DatabaseTypeMemfile,
		}
		result := renderDatabaseConfig(db, "", "")
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["type"] != "memfile" {
			t.Fatalf("expected type 'memfile', got %v", result["type"])
		}
		// no optional fields should be present
		for _, key := range []string{"name", "host", "port", "user", "password", "persist"} {
			if _, ok := result[key]; ok {
				t.Fatalf("expected key %q not to be set for minimal config", key)
			}
		}
	})

	t.Run("full mysql config", func(t *testing.T) {
		port := int32(3306)
		connectTimeout := int32(30)
		maxReconnect := int32(5)
		reconnectWait := int32(2000)
		retryOnStartup := true
		db := &keav1alpha1.DatabaseConfig{
			Type:              keav1alpha1.DatabaseTypeMySQL,
			Name:              "kea_leases",
			Host:              "mysql.example.com",
			Port:              &port,
			ConnectTimeout:    &connectTimeout,
			MaxReconnectTries: &maxReconnect,
			ReconnectWaitTime: &reconnectWait,
			OnFail:            "serve-retry-continue",
			RetryOnStartup:    &retryOnStartup,
		}
		result := renderDatabaseConfig(db, "kea", "secret")
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["type"] != "mysql" {
			t.Fatalf("expected type 'mysql', got %v", result["type"])
		}
		if result["name"] != "kea_leases" {
			t.Fatalf("expected name 'kea_leases', got %v", result["name"])
		}
		if result["host"] != "mysql.example.com" {
			t.Fatalf("expected host 'mysql.example.com', got %v", result["host"])
		}
		if result["port"] != int32(3306) {
			t.Fatalf("expected port 3306, got %v", result["port"])
		}
		if result["user"] != "kea" {
			t.Fatalf("expected user 'kea', got %v", result["user"])
		}
		if result["password"] != "secret" {
			t.Fatalf("expected password 'secret', got %v", result["password"])
		}
		if result["connect-timeout"] != int32(30) {
			t.Fatalf("expected connect-timeout 30, got %v", result["connect-timeout"])
		}
		if result["max-reconnect-tries"] != int32(5) {
			t.Fatalf("expected max-reconnect-tries 5, got %v", result["max-reconnect-tries"])
		}
		if result["reconnect-wait-time"] != int32(2000) {
			t.Fatalf("expected reconnect-wait-time 2000, got %v", result["reconnect-wait-time"])
		}
		if result["on-fail"] != "serve-retry-continue" {
			t.Fatalf("expected on-fail 'serve-retry-continue', got %v", result["on-fail"])
		}
		if result["retry-on-startup"] != true {
			t.Fatalf("expected retry-on-startup true, got %v", result["retry-on-startup"])
		}
	})
}

// --- renderOptionData tests ---

func TestRenderOptionData(t *testing.T) {
	t.Run("nil/empty returns nil", func(t *testing.T) {
		result := renderOptionData(nil)
		if result != nil {
			t.Fatal("expected nil for nil input")
		}
		result = renderOptionData([]keav1alpha1.OptionData{})
		if result != nil {
			t.Fatal("expected nil for empty input")
		}
	})

	t.Run("option with name and data", func(t *testing.T) {
		opts := []keav1alpha1.OptionData{
			{
				Name: "domain-name-servers",
				Data: "8.8.8.8, 8.8.4.4",
			},
		}
		result := renderOptionData(opts)
		if len(result) != 1 {
			t.Fatalf("expected 1 option, got %d", len(result))
		}
		if result[0]["name"] != "domain-name-servers" {
			t.Fatalf("expected name 'domain-name-servers', got %v", result[0]["name"])
		}
		if result[0]["data"] != "8.8.8.8, 8.8.4.4" {
			t.Fatalf("expected data '8.8.8.8, 8.8.4.4', got %v", result[0]["data"])
		}
	})

	t.Run("option with code and always-send", func(t *testing.T) {
		code := int32(6)
		alwaysSend := true
		opts := []keav1alpha1.OptionData{
			{
				Code:       &code,
				Data:       "10.0.0.1",
				AlwaysSend: &alwaysSend,
			},
		}
		result := renderOptionData(opts)
		if len(result) != 1 {
			t.Fatalf("expected 1 option, got %d", len(result))
		}
		if result[0]["code"] != int32(6) {
			t.Fatalf("expected code 6, got %v", result[0]["code"])
		}
		if result[0]["always-send"] != true {
			t.Fatalf("expected always-send true, got %v", result[0]["always-send"])
		}
		// name should not be set since it was empty
		if _, ok := result[0]["name"]; ok {
			t.Fatal("expected name not to be set")
		}
	})
}

// --- renderReservations tests ---

func TestRenderReservations(t *testing.T) {
	t.Run("nil/empty returns nil", func(t *testing.T) {
		result := renderReservations(nil)
		if result != nil {
			t.Fatal("expected nil for nil input")
		}
		result = renderReservations([]keav1alpha1.Reservation{})
		if result != nil {
			t.Fatal("expected nil for empty input")
		}
	})

	t.Run("reservation with hw-address and ip-address", func(t *testing.T) {
		res := []keav1alpha1.Reservation{
			{
				HWAddress: "aa:bb:cc:dd:ee:ff",
				IPAddress: "192.168.1.100",
				Hostname:  "host1",
			},
		}
		result := renderReservations(res)
		if len(result) != 1 {
			t.Fatalf("expected 1 reservation, got %d", len(result))
		}
		if result[0]["hw-address"] != "aa:bb:cc:dd:ee:ff" {
			t.Fatalf("expected hw-address 'aa:bb:cc:dd:ee:ff', got %v", result[0]["hw-address"])
		}
		if result[0]["ip-address"] != "192.168.1.100" {
			t.Fatalf("expected ip-address '192.168.1.100', got %v", result[0]["ip-address"])
		}
		if result[0]["hostname"] != "host1" {
			t.Fatalf("expected hostname 'host1', got %v", result[0]["hostname"])
		}
	})

	t.Run("reservation with ipv6 addresses and prefixes", func(t *testing.T) {
		res := []keav1alpha1.Reservation{
			{
				DUID:        "01:02:03:04:05",
				IPAddresses: []string{"2001:db8::1", "2001:db8::2"},
				Prefixes:    []string{"2001:db8:1::/48"},
			},
		}
		result := renderReservations(res)
		if len(result) != 1 {
			t.Fatalf("expected 1 reservation, got %d", len(result))
		}
		if result[0]["duid"] != "01:02:03:04:05" {
			t.Fatalf("expected duid '01:02:03:04:05', got %v", result[0]["duid"])
		}
		addrs, ok := result[0]["ip-addresses"].([]string)
		if !ok {
			t.Fatal("expected ip-addresses to be []string")
		}
		if len(addrs) != 2 {
			t.Fatalf("expected 2 ip-addresses, got %d", len(addrs))
		}
		prefixes, ok := result[0]["prefixes"].([]string)
		if !ok {
			t.Fatal("expected prefixes to be []string")
		}
		if len(prefixes) != 1 {
			t.Fatalf("expected 1 prefix, got %d", len(prefixes))
		}
	})
}

// --- renderLoggers tests ---

func TestRenderLoggers(t *testing.T) {
	t.Run("nil/empty returns nil", func(t *testing.T) {
		result := renderLoggers(nil)
		if result != nil {
			t.Fatal("expected nil for nil input")
		}
		result = renderLoggers([]keav1alpha1.LoggerConfig{})
		if result != nil {
			t.Fatal("expected nil for empty input")
		}
	})

	t.Run("logger with stdout output", func(t *testing.T) {
		debugLevel := int32(50)
		flush := true
		loggers := []keav1alpha1.LoggerConfig{
			{
				Name:       "kea-dhcp4",
				Severity:   "DEBUG",
				DebugLevel: &debugLevel,
				OutputOptions: []keav1alpha1.LogOutputOption{
					{
						Output: "stdout",
						Flush:  &flush,
					},
				},
			},
		}
		result := renderLoggers(loggers)
		if len(result) != 1 {
			t.Fatalf("expected 1 logger, got %d", len(result))
		}
		if result[0]["name"] != "kea-dhcp4" {
			t.Fatalf("expected name 'kea-dhcp4', got %v", result[0]["name"])
		}
		if result[0]["severity"] != "DEBUG" {
			t.Fatalf("expected severity 'DEBUG', got %v", result[0]["severity"])
		}
		if result[0]["debuglevel"] != int32(50) {
			t.Fatalf("expected debuglevel 50, got %v", result[0]["debuglevel"])
		}
		outs, ok := result[0]["output-options"].([]map[string]interface{})
		if !ok {
			t.Fatal("expected output-options to be []map[string]interface{}")
		}
		if len(outs) != 1 {
			t.Fatalf("expected 1 output option, got %d", len(outs))
		}
		if outs[0]["output"] != "stdout" {
			t.Fatalf("expected output 'stdout', got %v", outs[0]["output"])
		}
		if outs[0]["flush"] != true {
			t.Fatalf("expected flush true, got %v", outs[0]["flush"])
		}
	})

	t.Run("logger with file output and rotation", func(t *testing.T) {
		maxsize := int64(10485760)
		maxver := int32(8)
		loggers := []keav1alpha1.LoggerConfig{
			{
				Name:     "kea-dhcp6",
				Severity: "INFO",
				OutputOptions: []keav1alpha1.LogOutputOption{
					{
						Output:  "/var/log/kea/kea-dhcp6.log",
						Maxsize: &maxsize,
						Maxver:  &maxver,
					},
				},
			},
		}
		result := renderLoggers(loggers)
		if len(result) != 1 {
			t.Fatalf("expected 1 logger, got %d", len(result))
		}
		outs, ok := result[0]["output-options"].([]map[string]interface{})
		if !ok {
			t.Fatal("expected output-options to be []map[string]interface{}")
		}
		if outs[0]["maxsize"] != int64(10485760) {
			t.Fatalf("expected maxsize 10485760, got %v", outs[0]["maxsize"])
		}
		if outs[0]["maxver"] != int32(8) {
			t.Fatalf("expected maxver 8, got %v", outs[0]["maxver"])
		}
	})
}
