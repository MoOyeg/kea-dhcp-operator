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
	"testing"

	corev1 "k8s.io/api/core/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

const testStorkAgentImage = "test-agent:v1"

func TestResolveStorkParams_NilConfig(t *testing.T) {
	params, err := ResolveStorkParams(nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params != nil {
		t.Errorf("expected nil params for nil config, got %+v", params)
	}
}

func TestResolveStorkParams_Disabled(t *testing.T) {
	cfg := &keav1alpha1.StorkAgentConfig{Enabled: false}
	params, err := ResolveStorkParams(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params != nil {
		t.Errorf("expected nil params for disabled config, got %+v", params)
	}
}

func TestResolveStorkParams_DefaultImage(t *testing.T) {
	cfg := &keav1alpha1.StorkAgentConfig{Enabled: true}
	params, err := ResolveStorkParams(cfg, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Image != DefaultStorkAgentImage {
		t.Errorf("expected default image %q, got %q", DefaultStorkAgentImage, params.Image)
	}
	if params.Port != DefaultStorkAgentPort {
		t.Errorf("expected default port %d, got %d", DefaultStorkAgentPort, params.Port)
	}
	if params.PrometheusPort != DefaultStorkPromPort {
		t.Errorf("expected default prom port %d, got %d", DefaultStorkPromPort, params.PrometheusPort)
	}
}

func TestResolveStorkParams_CustomValues(t *testing.T) {
	customPort := int32(9090)
	customPromPort := int32(9999)
	cfg := &keav1alpha1.StorkAgentConfig{
		Enabled:        true,
		Image:          "custom-agent:v1",
		Port:           &customPort,
		PrometheusPort: &customPromPort,
		ServerURL:      "http://stork.example.com:8080",
	}
	params, err := ResolveStorkParams(cfg, "my-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Image != "custom-agent:v1" {
		t.Errorf("expected custom image, got %q", params.Image)
	}
	if params.Port != 9090 {
		t.Errorf("expected port 9090, got %d", params.Port)
	}
	if params.PrometheusPort != 9999 {
		t.Errorf("expected prom port 9999, got %d", params.PrometheusPort)
	}
	if params.ServerURL != "http://stork.example.com:8080" {
		t.Errorf("expected server URL, got %q", params.ServerURL)
	}
	if params.ServerToken != "my-token" {
		t.Errorf("expected token 'my-token', got %q", params.ServerToken)
	}
}

func TestBuildStorkAgentContainer_PrometheusOnly(t *testing.T) {
	sp := &StorkSidecarParams{
		Image:          testStorkAgentImage,
		Port:           8080,
		PrometheusPort: 9547,
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	if c.Name != StorkAgentContainerName {
		t.Errorf("expected container name %q, got %q", StorkAgentContainerName, c.Name)
	}
	if c.Image != testStorkAgentImage {
		t.Errorf("expected image 'test-agent:v1', got %q", c.Image)
	}

	// Without ServerURL, should have STORK_AGENT_LISTEN_PROMETHEUS_ONLY
	found := false
	for _, e := range c.Env {
		if e.Name == "STORK_AGENT_LISTEN_PROMETHEUS_ONLY" && e.Value == valueTrue {
			found = true
		}
		if e.Name == "STORK_AGENT_SERVER_URL" {
			t.Error("should not have STORK_AGENT_SERVER_URL in prometheus-only mode")
		}
	}
	if !found {
		t.Error("expected STORK_AGENT_LISTEN_PROMETHEUS_ONLY=true in prometheus-only mode")
	}
}

func TestBuildStorkAgentContainer_WithServer(t *testing.T) {
	sp := &StorkSidecarParams{
		Image:          testStorkAgentImage,
		Port:           8080,
		PrometheusPort: 9547,
		ServerURL:      "http://stork-server:8080",
		ServerToken:    "secret-token",
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	envMap := make(map[string]corev1.EnvVar)
	for _, e := range c.Env {
		envMap[e.Name] = e
	}

	// Should have server URL
	if e, ok := envMap["STORK_AGENT_SERVER_URL"]; !ok || e.Value != "http://stork-server:8080" {
		t.Errorf("expected STORK_AGENT_SERVER_URL=http://stork-server:8080, got %+v", e)
	}

	// Should have non-interactive mode
	if e, ok := envMap["STORK_AGENT_NON_INTERACTIVE"]; !ok || e.Value != valueTrue {
		t.Errorf("expected STORK_AGENT_NON_INTERACTIVE=true, got %+v", e)
	}

	// Should register with pod IP via downward API
	hostEnv, ok := envMap["STORK_AGENT_HOST"]
	if !ok {
		t.Fatal("expected STORK_AGENT_HOST env var")
	}
	if hostEnv.ValueFrom == nil || hostEnv.ValueFrom.FieldRef == nil {
		t.Fatal("expected STORK_AGENT_HOST to use fieldRef")
	}
	if hostEnv.ValueFrom.FieldRef.FieldPath != "status.podIP" {
		t.Errorf("expected fieldPath=status.podIP, got %q", hostEnv.ValueFrom.FieldRef.FieldPath)
	}

	// Should have server token
	if e, ok := envMap["STORK_AGENT_SERVER_TOKEN"]; !ok || e.Value != "secret-token" {
		t.Errorf("expected STORK_AGENT_SERVER_TOKEN=secret-token, got %+v", e)
	}

	// Should NOT have prometheus-only mode
	if _, ok := envMap["STORK_AGENT_LISTEN_PROMETHEUS_ONLY"]; ok {
		t.Error("should not have STORK_AGENT_LISTEN_PROMETHEUS_ONLY when server URL is set")
	}
}

func TestBuildStorkAgentContainer_VolumeMounts(t *testing.T) {
	sp := &StorkSidecarParams{
		Image:          testStorkAgentImage,
		Port:           8080,
		PrometheusPort: 9547,
	}
	c := buildStorkAgentContainer(sp, "my-config-vol", nil)

	if len(c.VolumeMounts) != 2 {
		t.Fatalf("expected 2 volume mounts, got %d", len(c.VolumeMounts))
	}
	if c.VolumeMounts[0].Name != "my-config-vol" {
		t.Errorf("expected config volume name 'my-config-vol', got %q", c.VolumeMounts[0].Name)
	}
	if !c.VolumeMounts[0].ReadOnly {
		t.Error("config volume should be read-only")
	}
	if c.VolumeMounts[1].Name != RunVolumeName {
		t.Errorf("expected run volume name %q, got %q", RunVolumeName, c.VolumeMounts[1].Name)
	}
}

func TestBuildStorkAgentContainer_SecurityContext(t *testing.T) {
	sp := &StorkSidecarParams{
		Image:          testStorkAgentImage,
		Port:           8080,
		PrometheusPort: 9547,
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	if c.SecurityContext == nil || c.SecurityContext.Capabilities == nil {
		t.Fatal("expected security context with capabilities")
	}
	if len(c.SecurityContext.Capabilities.Drop) != 1 || c.SecurityContext.Capabilities.Drop[0] != capabilityAll {
		t.Errorf("expected drop ALL capabilities, got %v", c.SecurityContext.Capabilities.Drop)
	}
	if len(c.SecurityContext.Capabilities.Add) != 0 {
		t.Errorf("expected no added capabilities without Kea caps, got %v", c.SecurityContext.Capabilities.Add)
	}
}

// TestStorkSidecarCanMonitorKea checks the pod-level invariants the stork-agent
// needs to discover and monitor Kea, for every pod shape the operator builds:
// a shared PID namespace, the Kea config and control-socket directory mounted,
// and permitted capabilities that cover the Kea container's (otherwise the
// kernel denies /proc/<kea-pid>/cwd and detection logs a warning every cycle).
func TestStorkSidecarCanMonitorKea(t *testing.T) {
	stork := &StorkSidecarParams{Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547}

	deploy := func(nad bool) corev1.PodSpec {
		p := DeploymentParams{
			CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
			Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
			ConfigMapName: "test-dhcp4", StorkAgent: stork,
		}
		if nad {
			p.NADInterface, p.NADSubnet = testNADIface, testNADSubnet
		}
		return BuildDeployment(p).Spec.Template.Spec
	}
	sts := func(nad bool) corev1.PodSpec {
		p := StatefulSetParams{
			CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
			Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
			ConfigMapNames: map[int]string{0: "test-dhcp4-0", 1: "test-dhcp4-1"}, StorkAgent: stork,
		}
		if nad {
			p.NADInterface, p.NADSubnet = testNADIface, testNADSubnet
		}
		return BuildStatefulSet(p).Spec.Template.Spec
	}

	tests := []struct {
		name string
		spec corev1.PodSpec
	}{
		{"deployment", deploy(false)},
		{"deployment with NAD", deploy(true)},
		{"statefulset", sts(false)},
		{"statefulset with NAD", sts(true)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var kea, agent *corev1.Container
			for i := range tc.spec.Containers {
				if tc.spec.Containers[i].Name == StorkAgentContainerName {
					agent = &tc.spec.Containers[i]
				} else {
					kea = &tc.spec.Containers[i]
				}
			}
			if kea == nil || agent == nil {
				t.Fatalf("expected Kea and stork-agent containers, got %d containers", len(tc.spec.Containers))
			}

			if tc.spec.ShareProcessNamespace == nil || !*tc.spec.ShareProcessNamespace {
				t.Error("expected shareProcessNamespace so stork-agent can see the Kea process")
			}

			mounts := map[string]bool{}
			for _, m := range agent.VolumeMounts {
				mounts[m.MountPath] = true
			}
			for _, path := range []string{ConfigMountPath, RunMountPath} {
				if !mounts[path] {
					t.Errorf("expected stork-agent to mount %s, got %v", path, agent.VolumeMounts)
				}
			}

			agentCaps := map[corev1.Capability]bool{}
			for _, c := range agent.SecurityContext.Capabilities.Add {
				agentCaps[c] = true
			}
			for _, c := range kea.SecurityContext.Capabilities.Add {
				if !agentCaps[c] {
					t.Errorf("stork-agent capabilities %v do not cover Kea capability %s",
						agent.SecurityContext.Capabilities.Add, c)
				}
			}
		})
	}
}

func TestBuildStorkAgentContainer_ExtraEnv(t *testing.T) {
	sp := &StorkSidecarParams{
		Image:          testStorkAgentImage,
		Port:           8080,
		PrometheusPort: 9547,
		ExtraEnv: []corev1.EnvVar{
			{Name: "CUSTOM_VAR", Value: "custom-value"},
		},
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	found := false
	for _, e := range c.Env {
		if e.Name == "CUSTOM_VAR" && e.Value == "custom-value" {
			found = true
		}
	}
	if !found {
		t.Error("expected CUSTOM_VAR=custom-value in extra env")
	}
}

func TestBuildStorkAgentContainer_Ports(t *testing.T) {
	sp := &StorkSidecarParams{
		Image:          testStorkAgentImage,
		Port:           8888,
		PrometheusPort: 9999,
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	if len(c.Ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(c.Ports))
	}
	if c.Ports[0].ContainerPort != 8888 {
		t.Errorf("expected agent port 8888, got %d", c.Ports[0].ContainerPort)
	}
	if c.Ports[1].ContainerPort != 9999 {
		t.Errorf("expected prom port 9999, got %d", c.Ports[1].ContainerPort)
	}
}
