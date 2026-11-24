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
	"strconv"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// Common Kea JSON field keys and literal values shared across renderers.
const (
	fieldName        = "name"
	fieldLibrary     = "library"
	fieldIPAddresses = "ip-addresses"

	dbTypeMySQL  = "mysql"
	listenAnyIP4 = "0.0.0.0"
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
	if v.Kind() == reflect.Pointer {
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

func renderDhcpDdnsConfig(config *keav1alpha1.DhcpDdnsConfig) map[string]interface{} {
	if config == nil {
		return nil
	}
	result := map[string]interface{}{}
	setIfNotNil(result, "enable-updates", config.EnableUpdates)
	setIfNotEmpty(result, "server-ip", config.ServerIP)
	setIfNotNil(result, "server-port", config.ServerPort)
	setIfNotEmpty(result, "sender-ip", config.SenderIP)
	setIfNotNil(result, "sender-port", config.SenderPort)
	setIfNotNil(result, "max-queue-size", config.MaxQueueSize)
	setIfNotEmpty(result, "ncr-protocol", config.NCRProtocol)
	setIfNotEmpty(result, "ncr-format", config.NCRFormat)
	return result
}

func renderDDNSBehavior(target map[string]interface{}, behavior keav1alpha1.DDNSBehavior) {
	setIfNotNil(target, "ddns-send-updates", behavior.DDNSSendUpdates)
	setIfNotNil(target, "ddns-override-no-update", behavior.DDNSOverrideNoUpdate)
	setIfNotNil(target, "ddns-override-client-update", behavior.DDNSOverrideClientUpdate)
	setIfNotEmpty(target, "ddns-replace-client-name", behavior.DDNSReplaceClientName)
	setIfNotNil(target, "ddns-generated-prefix", behavior.DDNSGeneratedPrefix)
	setIfNotNil(target, "ddns-qualifying-suffix", behavior.DDNSQualifyingSuffix)
	setIfNotNil(target, "ddns-update-on-renew", behavior.DDNSUpdateOnRenew)
	setIfNotEmpty(target, "ddns-conflict-resolution-mode", behavior.DDNSConflictResolutionMode)
	setIfNotNil(target, "ddns-ttl", behavior.DDNSTTL)
	if pct, ok, err := ddnsTTLPercentValue(behavior.DDNSTTLPercent); ok && err == nil {
		target[keyDDNSTTLPercent] = pct
	}
	setIfNotNil(target, "ddns-ttl-min", behavior.DDNSTTLMin)
	setIfNotNil(target, "ddns-ttl-max", behavior.DDNSTTLMax)
	setIfNotNil(target, "hostname-char-set", behavior.HostnameCharSet)
	setIfNotNil(target, "hostname-char-replacement", behavior.HostnameCharReplacement)
}

// stringPtrIfSet turns a plain (global-scope) string field into the pointer
// form used by DDNSBehavior; the global scope has nothing to override, so an
// empty string there means "unset".
func stringPtrIfSet(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// keyDDNSTTLPercent is the Kea parameter name for the lease-lifetime TTL percentage.
const keyDDNSTTLPercent = "ddns-ttl-percent"

// ddnsTTLPercentSet reports whether a schemaless ddns-ttl-percent carries a value.
func ddnsTTLPercentSet(v *apiextensionsv1.JSON) bool {
	return v != nil && len(v.Raw) > 0
}

// ddnsTTLPercentValue parses a schemaless ddns-ttl-percent and returns it as
// a JSON number that always carries a decimal point. Kea's parser only
// accepts a floating-point literal here: "0" is a syntax error while "0.0" is
// fine, and Go's encoder would print a whole-number float without the point.
// A negative value is accepted by Kea's parser but meaningless, so it is
// rejected; 0 is allowed because Kea uses it to suppress an inherited value.
func ddnsTTLPercentValue(v *apiextensionsv1.JSON) (json.Number, bool, error) {
	if !ddnsTTLPercentSet(v) {
		return "", false, nil
	}
	var pct float64
	if err := json.Unmarshal(v.Raw, &pct); err != nil || pct < 0 {
		return "", true, fmt.Errorf("%s must be a non-negative number, got %s", keyDDNSTTLPercent, string(v.Raw))
	}
	text := strconv.FormatFloat(pct, 'f', -1, 64)
	if !strings.ContainsAny(text, ".e") {
		text += ".0"
	}
	return json.Number(text), true, nil
}

// validateDDNSBehavior enforces the constraints Kea applies to the DDNS
// parameters of one scope, so a bad spec fails the CR's ConfigValid condition
// instead of crash-looping the server. Kea rejects a fixed ddns-ttl combined
// with ddns-ttl-percent, ddns-ttl-min or ddns-ttl-max ("cannot specify both
// ddns-ttl-percent and ddns-ttl"), and ddns-ttl-percent must be a non-negative
// floating-point number; the CRD field is schemaless so the API server cannot
// check that.
func validateDDNSBehavior(scope string, b keav1alpha1.DDNSBehavior) error {
	if b.DDNSTTL != nil {
		for _, c := range []struct {
			name string
			set  bool
		}{
			{keyDDNSTTLPercent, ddnsTTLPercentSet(b.DDNSTTLPercent)},
			{"ddns-ttl-min", b.DDNSTTLMin != nil},
			{"ddns-ttl-max", b.DDNSTTLMax != nil},
		} {
			if c.set {
				return fmt.Errorf("%s: cannot specify both %s and ddns-ttl", scope, c.name)
			}
		}
	}
	if _, _, err := ddnsTTLPercentValue(b.DDNSTTLPercent); err != nil {
		return fmt.Errorf("%s: %w", scope, err)
	}
	return nil
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
			fieldName: d.Name,
			"code":    d.Code,
			"type":    d.Type,
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
			fieldName: c.Name,
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
			fieldLibrary: h.Library,
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
	case dbTypeMySQL:
		return map[string]interface{}{fieldLibrary: mysqlHookLib}
	case "postgresql":
		return map[string]interface{}{fieldLibrary: pgsqlHookLib}
	default:
		return nil
	}
}

// appendDBHooks adds the backend hook of every database that needs one, the
// lease database and each hosts database alike: Kea 3.0 only registers the
// MySQL and PostgreSQL lease and host backends once their hook is loaded.
// Each library is added once, and not at all when the user's hooks list
// already loads it.
func appendDBHooks(hooks []map[string]interface{}, lease, hosts *keav1alpha1.DatabaseConfig, hostsList []keav1alpha1.DatabaseConfig) []map[string]interface{} {
	loaded := map[string]bool{}
	for _, h := range hooks {
		if lib, ok := h[fieldLibrary].(string); ok {
			loaded[lib] = true
		}
	}
	dbs := make([]*keav1alpha1.DatabaseConfig, 0, 2+len(hostsList))
	dbs = append(dbs, lease, hosts)
	for i := range hostsList {
		dbs = append(dbs, &hostsList[i])
	}
	for _, db := range dbs {
		hook := renderDBHook(db)
		if hook == nil {
			continue
		}
		lib := hook[fieldLibrary].(string)
		if loaded[lib] {
			continue
		}
		loaded[lib] = true
		hooks = append(hooks, hook)
	}
	return hooks
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
			fieldName: l.Name,
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
