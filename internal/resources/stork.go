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

// buildStorkAgentContainer creates the stork-agent sidecar container.
// It shares the Kea config volume (read-only) so the agent can discover Kea
// by reading the config file path from the process command line.
func buildStorkAgentContainer(sp *StorkSidecarParams, configVolumeName string) corev1.Container {
	env := []corev1.EnvVar{
		{Name: "STORK_AGENT_PORT", Value: fmt.Sprintf("%d", sp.Port)},
		{Name: "STORK_AGENT_PROMETHEUS_KEA_EXPORTER_PORT", Value: fmt.Sprintf("%d", sp.PrometheusPort)},
		{Name: "STORK_AGENT_PROMETHEUS_KEA_EXPORTER_ADDRESS", Value: "0.0.0.0"},
	}

	if sp.ServerURL != "" {
		env = append(env,
			corev1.EnvVar{Name: "STORK_AGENT_SERVER_URL", Value: sp.ServerURL},
			corev1.EnvVar{Name: "STORK_AGENT_NON_INTERACTIVE", Value: "true"},
		)
		// Always register with the pod IP so the Stork server can reach the agent
		// over the cluster network.
		env = append(env, corev1.EnvVar{
			Name: "STORK_AGENT_HOST",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		})
		if sp.ServerToken != "" {
			env = append(env, corev1.EnvVar{Name: "STORK_AGENT_SERVER_TOKEN", Value: sp.ServerToken})
		}
	} else {
		// Without a Stork server, run in Prometheus-only mode to avoid
		// requiring gRPC TLS certificates.
		env = append(env, corev1.EnvVar{Name: "STORK_AGENT_LISTEN_PROMETHEUS_ONLY", Value: "true"})
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
				Name:          "stork-prom",
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
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	return c
}
