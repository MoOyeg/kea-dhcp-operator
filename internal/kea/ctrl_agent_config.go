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

// CtrlAgentConfigRenderer renders a Kea Control Agent JSON configuration
// from a KeaControlAgentSpec.
type CtrlAgentConfigRenderer struct {
	Spec *keav1alpha1.KeaControlAgentSpec
	// ResolvedAuthPasswords maps client username to the resolved password
	// from PasswordSecretKeyRef.
	ResolvedAuthPasswords map[string]string
}

// NewCtrlAgentConfigRenderer creates a new CtrlAgentConfigRenderer.
func NewCtrlAgentConfigRenderer(spec *keav1alpha1.KeaControlAgentSpec) *CtrlAgentConfigRenderer {
	return &CtrlAgentConfigRenderer{Spec: spec}
}

// RenderJSON builds the complete {"Control-agent": {...}} JSON configuration.
func (r *CtrlAgentConfigRenderer) RenderJSON() ([]byte, error) {
	s := r.Spec
	agent := map[string]interface{}{}

	// HTTP host and port
	setIfNotEmpty(agent, "http-host", s.HTTPHost)
	setIfNotNil(agent, "http-port", s.HTTPPort)

	// TLS configuration
	if s.TLS != nil {
		agent["trust-anchor"] = "/etc/kea/tls/ca.crt"
		agent["cert-file"] = "/etc/kea/tls/tls.crt"
		agent["key-file"] = "/etc/kea/tls/tls.key"
		setIfNotNil(agent, "cert-required", s.TLS.CertRequired)
	}

	// Control sockets
	if s.ControlSockets != nil {
		sockets := map[string]interface{}{}
		if s.ControlSockets.Dhcp4 != nil {
			sockets["dhcp4"] = renderControlSocket(s.ControlSockets.Dhcp4)
		}
		if s.ControlSockets.Dhcp6 != nil {
			sockets["dhcp6"] = renderControlSocket(s.ControlSockets.Dhcp6)
		}
		if s.ControlSockets.D2 != nil {
			sockets["d2"] = renderControlSocket(s.ControlSockets.D2)
		}
		if len(sockets) > 0 {
			agent["control-sockets"] = sockets
		}
	}

	// Authentication
	if s.Authentication != nil {
		auth := map[string]interface{}{}
		setIfNotEmpty(auth, "type", s.Authentication.Type)
		setIfNotEmpty(auth, "realm", s.Authentication.Realm)
		if len(s.Authentication.Clients) > 0 {
			clients := make([]map[string]interface{}, 0, len(s.Authentication.Clients))
			for _, c := range s.Authentication.Clients {
				cm := map[string]interface{}{
					"user": c.User,
				}
				if r.ResolvedAuthPasswords != nil {
					setIfNotEmpty(cm, "password", r.ResolvedAuthPasswords[c.User])
				}
				clients = append(clients, cm)
			}
			auth["clients"] = clients
		}
		if len(auth) > 0 {
			agent["authentication"] = auth
		}
	}

	// Hooks libraries
	if hooks := renderHooksLibraries(s.HooksLibraries); hooks != nil {
		agent["hooks-libraries"] = hooks
	}

	// Loggers
	if loggers := renderLoggers(s.Loggers); loggers != nil {
		agent["loggers"] = loggers
	}

	root := map[string]interface{}{
		"Control-agent": agent,
	}
	return json.MarshalIndent(root, "", "  ")
}
