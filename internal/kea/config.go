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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// ConfigRenderer defines the interface for rendering Kea JSON configuration.
type ConfigRenderer interface {
	RenderJSON() ([]byte, error)
}

// ComputeHash returns the first 16 characters of the SHA-256 hex digest of data.
func ComputeHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)[:16]
}

// setIfNotNil sets the key in the map if val is a non-nil pointer.
// If val is a pointer, it is dereferenced before being stored.
func setIfNotNil(m map[string]interface{}, key string, val interface{}) {
	if val == nil {
		return
	}
	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		m[key] = v.Elem().Interface()
		return
	}
	m[key] = val
}

// setIfNotEmpty sets the key in the map if val is a non-empty string.
func setIfNotEmpty(m map[string]interface{}, key string, val string) {
	if val != "" {
		m[key] = val
	}
}

// renderOptionData converts a slice of OptionData to JSON-ready maps.
func renderOptionData(opts []keav1alpha1.OptionData) []map[string]interface{} {
	if len(opts) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(opts))
	for _, o := range opts {
		om := map[string]interface{}{}
		setIfNotEmpty(om, "name", o.Name)
		setIfNotNil(om, "code", o.Code)
		setIfNotEmpty(om, "space", o.Space)
		setIfNotNil(om, "csv-format", o.CSVFormat)
		setIfNotEmpty(om, "data", o.Data)
		setIfNotNil(om, "always-send", o.AlwaysSend)
		setIfNotNil(om, "never-send", o.NeverSend)
		result = append(result, om)
	}
	return result
}

// renderOptionDef converts a slice of OptionDef to JSON-ready maps.
func renderOptionDef(defs []keav1alpha1.OptionDef) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		dm := map[string]interface{}{
			"name": d.Name,
			"code": d.Code,
			"type": d.Type,
		}
		setIfNotNil(dm, "array", d.Array)
		setIfNotEmpty(dm, "record-types", d.RecordTypes)
		setIfNotEmpty(dm, "encapsulate", d.Encapsulate)
		setIfNotEmpty(dm, "space", d.Space)
		result = append(result, dm)
	}
	return result
}

// renderClientClasses converts a slice of ClientClass to JSON-ready maps.
func renderClientClasses(classes []keav1alpha1.ClientClass) []map[string]interface{} {
	if len(classes) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(classes))
	for _, c := range classes {
		cm := map[string]interface{}{
			"name": c.Name,
		}
		setIfNotEmpty(cm, "test", c.Test)
		if opts := renderOptionData(c.OptionData); opts != nil {
			cm["option-data"] = opts
		}
		setIfNotNil(cm, "only-if-required", c.OnlyIfRequired)
		setIfNotNil(cm, "valid-lifetime", c.ValidLifetime)
		setIfNotNil(cm, "min-valid-lifetime", c.MinValidLifetime)
		setIfNotNil(cm, "max-valid-lifetime", c.MaxValidLifetime)
		result = append(result, cm)
	}
	return result
}

// renderReservations converts a slice of Reservation to JSON-ready maps.
func renderReservations(reservations []keav1alpha1.Reservation) []map[string]interface{} {
	if len(reservations) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(reservations))
	for _, r := range reservations {
		rm := map[string]interface{}{}
		setIfNotEmpty(rm, "hw-address", r.HWAddress)
		setIfNotEmpty(rm, "client-id", r.ClientID)
		setIfNotEmpty(rm, "duid", r.DUID)
		setIfNotEmpty(rm, "circuit-id", r.CircuitID)
		setIfNotEmpty(rm, "flex-id", r.FlexID)
		setIfNotEmpty(rm, "hostname", r.Hostname)
		setIfNotEmpty(rm, "ip-address", r.IPAddress)
		if len(r.IPAddresses) > 0 {
			rm["ip-addresses"] = r.IPAddresses
		}
		if len(r.Prefixes) > 0 {
			rm["prefixes"] = r.Prefixes
		}
		if opts := renderOptionData(r.OptionData); opts != nil {
			rm["option-data"] = opts
		}
		if len(r.ClientClasses) > 0 {
			rm["client-classes"] = r.ClientClasses
		}
		setIfNotEmpty(rm, "next-server", r.NextServer)
		setIfNotEmpty(rm, "boot-file-name", r.BootFileName)
		setIfNotEmpty(rm, "server-hostname", r.ServerHostname)
		result = append(result, rm)
	}
	return result
}

// renderHooksLibraries converts a slice of HookLibrary to JSON-ready maps.
func renderHooksLibraries(hooks []keav1alpha1.HookLibrary) []map[string]interface{} {
	if len(hooks) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(hooks))
	for _, h := range hooks {
		hm := map[string]interface{}{
			"library": h.Library,
		}
		if h.Parameters != nil && h.Parameters.Raw != nil {
			var params interface{}
			if err := json.Unmarshal(h.Parameters.Raw, &params); err == nil {
				hm["parameters"] = params
			}
		}
		result = append(result, hm)
	}
	return result
}

// renderDBHook returns a hook library entry for the database backend if a
// non-memfile lease database type is configured. Kea 3.0+ requires loading
// the appropriate hook library (libdhcp_mysql.so or libdhcp_pgsql.so) to
// enable MySQL or PostgreSQL lease database support.
func renderDBHook(db *keav1alpha1.DatabaseConfig) map[string]interface{} {
	if db == nil {
		return nil
	}
	switch db.Type {
	case "mysql":
		return map[string]interface{}{"library": mysqlHookLib}
	case "postgresql":
		return map[string]interface{}{"library": pgsqlHookLib}
	default:
		return nil
	}
}

// renderControlSocket converts a ControlSocket to a JSON-ready map.
func renderControlSocket(cs *keav1alpha1.ControlSocket) map[string]interface{} {
	if cs == nil {
		return nil
	}
	csm := map[string]interface{}{
		"socket-type": cs.SocketType,
	}
	setIfNotEmpty(csm, "socket-name", cs.SocketName)
	setIfNotNil(csm, "socket-port", cs.SocketPort)
	setIfNotEmpty(csm, "socket-address", cs.SocketAddress)
	return csm
}

// renderLoggers converts a slice of LoggerConfig to JSON-ready maps.
func renderLoggers(loggers []keav1alpha1.LoggerConfig) []map[string]interface{} {
	if len(loggers) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(loggers))
	for _, l := range loggers {
		lm := map[string]interface{}{
			"name": l.Name,
		}
		setIfNotEmpty(lm, "severity", l.Severity)
		setIfNotNil(lm, "debuglevel", l.DebugLevel)
		if len(l.OutputOptions) > 0 {
			outs := make([]map[string]interface{}, 0, len(l.OutputOptions))
			for _, o := range l.OutputOptions {
				om := map[string]interface{}{
					"output": o.Output,
				}
				setIfNotNil(om, "maxsize", o.Maxsize)
				setIfNotNil(om, "maxver", o.Maxver)
				setIfNotNil(om, "flush", o.Flush)
				setIfNotEmpty(om, "pattern", o.Pattern)
				outs = append(outs, om)
			}
			lm["output-options"] = outs
		}
		result = append(result, lm)
	}
	return result
}

// renderDatabaseConfig converts a DatabaseConfig to a JSON-ready map.
// resolvedUser and resolvedPassword are the values resolved from
// CredentialsSecretRef by the controller before rendering.
func renderDatabaseConfig(db *keav1alpha1.DatabaseConfig, resolvedUser, resolvedPassword string) map[string]interface{} {
	if db == nil {
		return nil
	}
	dm := map[string]interface{}{
		"type": string(db.Type),
	}
	setIfNotNil(dm, "persist", db.Persist)
	setIfNotEmpty(dm, "name", db.Name)
	setIfNotEmpty(dm, "host", db.Host)
	setIfNotNil(dm, "port", db.Port)
	setIfNotEmpty(dm, "user", resolvedUser)
	setIfNotEmpty(dm, "password", resolvedPassword)
	setIfNotNil(dm, "connect-timeout", db.ConnectTimeout)
	setIfNotNil(dm, "max-reconnect-tries", db.MaxReconnectTries)
	setIfNotNil(dm, "reconnect-wait-time", db.ReconnectWaitTime)
	setIfNotEmpty(dm, "on-fail", db.OnFail)
	setIfNotNil(dm, "retry-on-startup", db.RetryOnStartup)
	setIfNotNil(dm, "lfc-interval", db.LFCInterval)
	setIfNotNil(dm, "max-row-errors", db.MaxRowErrors)
	setIfNotNil(dm, "readonly", db.ReadOnly)
	setIfNotNil(dm, "read-timeout", db.ReadTimeout)
	setIfNotNil(dm, "write-timeout", db.WriteTimeout)
	setIfNotNil(dm, "tcp-user-timeout", db.TCPUserTimeout)
	return dm
}
