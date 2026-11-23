/*
Copyright 2026.

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

package resources

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

const (
	StorkAgentContainerName = "stork-agent"
	DefaultStorkAgentImage  = "quay.io/mooyeg/stork-agent:v2.4.0-1"
	DefaultStorkAgentPort   = int32(8080)
	DefaultStorkPromPort    = int32(9547)
)

// storkAgentRegisterScript is the sidecar command when a server token is set.
// The agent daemon only does agent-token registration, which leaves the machine
// waiting for manual authorization in the Stork UI. Server-token registration,
// which authorizes the machine automatically, is only available through the
// separate "register" command. It needs the agent port free (it answers the
// server's ping itself), so it has to run before the daemon starts. The daemon
// then needs neither the server URL nor the token: it serves the certificates
// "register" stored. If registration keeps failing (server not up yet), fall
// back to the daemon's agent-token registration so monitoring still starts.
const storkAgentRegisterScript = `for attempt in 1 2 3 4 5 6; do
  if /usr/bin/stork-agent register --non-interactive \
      --server-url "$STORK_AGENT_SERVER_URL" --server-token "$STORK_AGENT_SERVER_TOKEN" \
      --agent-host "$STORK_AGENT_HOST" --agent-port "$STORK_AGENT_PORT"; then
    unset STORK_AGENT_SERVER_URL STORK_AGENT_SERVER_TOKEN
    exec /usr/bin/stork-agent
  fi
  sleep 10
done
echo "server-token registration failed; falling back to agent-token registration" >&2
unset STORK_AGENT_SERVER_TOKEN
exec /usr/bin/stork-agent`

// StorkSidecarParams holds the resolved parameters for the stork-agent sidecar.
type StorkSidecarParams struct {
	Image           string
	ImagePullPolicy corev1.PullPolicy
	Resources       corev1.ResourceRequirements
	ServerURL       string
	ServerToken     string
	Port            int32
	PrometheusPort  int32
	ExtraEnv        []corev1.EnvVar
	// AgentDomain is the DNS suffix that follows the pod name in the address
	// the agent registers with, e.g. "<cr>-dhcp4-hl.<ns>.svc.cluster.local"
	// for StatefulSet pods. Set it only when every pod has a stable per-pod
	// DNS name. Empty registers with the pod IP instead.
	AgentDomain string
	// ServerName is the in-cluster KeaStorkServer the agent registers with.
	// When set, the pod template carries LabelStorkServer so that server's
	// reconciler can find the agent pods. Empty for external Stork servers.
	ServerName string
}

const (
	envStorkAgentHost = "STORK_AGENT_HOST"
	fieldPathPodIP    = "status.podIP"
)

// LabelStorkServer marks pods whose stork-agent reports to the named
// KeaStorkServer in the same namespace.
const LabelStorkServer = "kea.openshift.io/stork-server"

// podTemplateLabels returns the workload labels plus the Stork server label
// when the agent reports to an in-cluster KeaStorkServer. It always returns a
// new map: the selector labels must stay as they are, because a StatefulSet's
// selector is immutable.
func podTemplateLabels(labels map[string]string, sp *StorkSidecarParams) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	if sp != nil && sp.ServerName != "" {
		out[LabelStorkServer] = sp.ServerName
	}
	return out
}

// ResolveStorkParams converts the CRD StorkAgentConfig into resolved params.
// The serverToken is resolved externally by the controller (from Secret).
// If no image is specified, DefaultStorkAgentImage is used.
func ResolveStorkParams(cfg *keav1alpha1.StorkAgentConfig, serverToken string) (*StorkSidecarParams, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	image := cfg.Image
	if image == "" {
		image = DefaultStorkAgentImage
	}

	port := DefaultStorkAgentPort
	if cfg.Port != nil {
		port = *cfg.Port
	}

	promPort := DefaultStorkPromPort
	if cfg.PrometheusPort != nil {
		promPort = *cfg.PrometheusPort
	}

	return &StorkSidecarParams{
		Image:           image,
		ImagePullPolicy: cfg.ImagePullPolicy,
		Resources:       cfg.Resources,
		ServerURL:       cfg.ServerURL,
		ServerToken:     serverToken,
		Port:            port,
		PrometheusPort:  promPort,
		ExtraEnv:        cfg.Env,
	}, nil
}

// storkAgentHostEnv sets STORK_AGENT_HOST, the address the Stork server
// records for the agent and dials for state.
//
// Stork keys machines on address and agent port, and re-registering an
// existing address updates that machine in place. With a stable per-pod DNS
// name, a recreated pod therefore takes over its old machine. With the pod IP,
// every recreation adds a new machine and leaves the old one behind. The state
// puller then keeps dialling those dead addresses one after another, which
// delays the state pulls for the live agents.
func storkAgentHostEnv(agentDomain string) []corev1.EnvVar {
	if agentDomain == "" {
		return []corev1.EnvVar{{
			Name: envStorkAgentHost,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: fieldPathPodIP},
			},
		}}
	}
	// POD_NAME must come first: $(POD_NAME) only expands from variables
	// defined earlier in the list.
	return []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{Name: envStorkAgentHost, Value: "$(POD_NAME)." + agentDomain},
	}
}

// buildStorkAgentContainer creates the stork-agent sidecar container.
// It shares the Kea config volume (read-only) so the agent can discover Kea
// by reading the config file path from the process command line.
//
// keaCaps must be the capabilities added to the Kea container in the same pod.
// The agent inspects the Kea process via /proc (e.g. /proc/<pid>/cwd), and the
// kernel only allows that without CAP_SYS_PTRACE when the observer's permitted
// capabilities are a superset of the target's. Without them every detection
// cycle logs "readlink /proc/<pid>/cwd: permission denied".
func buildStorkAgentContainer(sp *StorkSidecarParams, configVolumeName string, keaCaps []corev1.Capability) corev1.Container {
	env := []corev1.EnvVar{
		{Name: "STORK_AGENT_PORT", Value: fmt.Sprintf("%d", sp.Port)},
		{Name: "STORK_AGENT_PROMETHEUS_KEA_EXPORTER_PORT", Value: fmt.Sprintf("%d", sp.PrometheusPort)},
		{Name: "STORK_AGENT_PROMETHEUS_KEA_EXPORTER_ADDRESS", Value: "0.0.0.0"},
	}

	if sp.ServerURL != "" {
		env = append(env,
			corev1.EnvVar{Name: "STORK_AGENT_SERVER_URL", Value: sp.ServerURL},
			corev1.EnvVar{Name: "STORK_AGENT_NON_INTERACTIVE", Value: valueTrue},
		)
		env = append(env, storkAgentHostEnv(sp.AgentDomain)...)
		if sp.ServerToken != "" {
			env = append(env, corev1.EnvVar{Name: "STORK_AGENT_SERVER_TOKEN", Value: sp.ServerToken})
		}
	} else {
		// Without a Stork server, run in Prometheus-only mode to avoid
		// requiring gRPC TLS certificates.
		env = append(env, corev1.EnvVar{Name: "STORK_AGENT_LISTEN_PROMETHEUS_ONLY", Value: valueTrue})
	}

	env = append(env, sp.ExtraEnv...)

	c := corev1.Container{
		Name:            StorkAgentContainerName,
		Image:           sp.Image,
		ImagePullPolicy: sp.ImagePullPolicy,
		Resources:       sp.Resources,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "stork-agent",
				ContainerPort: sp.Port,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          portNameStorkProm,
				ContainerPort: sp.PrometheusPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      configVolumeName,
				MountPath: ConfigMountPath,
				ReadOnly:  true,
			},
			{
				Name:      RunVolumeName,
				MountPath: RunMountPath,
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{capabilityAll},
				Add:  append([]corev1.Capability(nil), keaCaps...),
			},
		},
	}

	if sp.ServerURL != "" && sp.ServerToken != "" {
		c.Command = []string{"/bin/sh", "-c", storkAgentRegisterScript}
	}

	return c
}
