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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

const (
	testStorkAgentImage  = "test-agent:v1"
	testStorkServerURL   = "http://stork-server:8080"
	testStorkServerToken = "secret-token"
)

const (
	testExternalStorkURL = "http://stork.example.com:8080"
	testDeployConfigMap  = "test-dhcp4"
	kindStatefulSet      = "statefulset"
	kindDeployment       = "deployment"
)

func testSTSConfigMaps() map[int]string { return map[int]string{0: "test-dhcp4-0", 1: "test-dhcp4-1"} }

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
		ServerURL:      testExternalStorkURL,
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
	if params.ServerURL != testExternalStorkURL {
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
		ServerURL:      testStorkServerURL,
		ServerToken:    testStorkServerToken,
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	envMap := make(map[string]corev1.EnvVar)
	for _, e := range c.Env {
		envMap[e.Name] = e
	}

	// Should have server URL
	if e, ok := envMap["STORK_AGENT_SERVER_URL"]; !ok || e.Value != testStorkServerURL {
		t.Errorf("expected STORK_AGENT_SERVER_URL=%s, got %+v", testStorkServerURL, e)
	}

	// Should have non-interactive mode
	if e, ok := envMap["STORK_AGENT_NON_INTERACTIVE"]; !ok || e.Value != valueTrue {
		t.Errorf("expected STORK_AGENT_NON_INTERACTIVE=true, got %+v", e)
	}

	// Should register with pod IP via downward API
	hostEnv, ok := envMap[envStorkAgentHost]
	if !ok {
		t.Fatal("expected STORK_AGENT_HOST env var")
	}
	if hostEnv.ValueFrom == nil || hostEnv.ValueFrom.FieldRef == nil {
		t.Fatal("expected STORK_AGENT_HOST to use fieldRef")
	}
	if hostEnv.ValueFrom.FieldRef.FieldPath != fieldPathPodIP {
		t.Errorf("expected fieldPath=status.podIP, got %q", hostEnv.ValueFrom.FieldRef.FieldPath)
	}

	// Should have server token
	if e, ok := envMap["STORK_AGENT_SERVER_TOKEN"]; !ok || e.Value != testStorkServerToken {
		t.Errorf("expected STORK_AGENT_SERVER_TOKEN=secret-token, got %+v", e)
	}

	// Should NOT have prometheus-only mode
	if _, ok := envMap["STORK_AGENT_LISTEN_PROMETHEUS_ONLY"]; ok {
		t.Error("should not have STORK_AGENT_LISTEN_PROMETHEUS_ONLY when server URL is set")
	}
}

// TestBuildStorkAgentContainer_StableAgentHost checks that, given a DNS suffix,
// the agent registers under "<pod name>.<suffix>" instead of the pod IP, and
// that POD_NAME comes before STORK_AGENT_HOST so the $(POD_NAME) reference expands.
func TestBuildStorkAgentContainer_StableAgentHost(t *testing.T) {
	const domain = "test-dhcp4-hl.kea-system.svc.cluster.local"
	sp := &StorkSidecarParams{
		Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547,
		ServerURL: testStorkServerURL, ServerToken: testStorkServerToken, AgentDomain: domain,
	}
	c := buildStorkAgentContainer(sp, "kea-config", nil)

	podNameIdx, hostIdx := -1, -1
	for i, e := range c.Env {
		switch e.Name {
		case "POD_NAME":
			podNameIdx = i
			if e.ValueFrom == nil || e.ValueFrom.FieldRef == nil || e.ValueFrom.FieldRef.FieldPath != "metadata.name" {
				t.Errorf("POD_NAME should come from metadata.name, got %+v", e)
			}
		case envStorkAgentHost:
			hostIdx = i
			if e.ValueFrom != nil {
				t.Errorf("STORK_AGENT_HOST should be a literal, not a fieldRef: %+v", e.ValueFrom)
			}
			if want := "$(POD_NAME)." + domain; e.Value != want {
				t.Errorf("STORK_AGENT_HOST = %q, want %q", e.Value, want)
			}
		}
	}
	if podNameIdx < 0 || hostIdx < 0 {
		t.Fatalf("expected POD_NAME and STORK_AGENT_HOST, env = %+v", c.Env)
	}
	if podNameIdx > hostIdx {
		t.Error("POD_NAME must be defined before STORK_AGENT_HOST for $(POD_NAME) to expand")
	}
}

// TestBuildStatefulSet_StorkAgentHost checks that StatefulSet pods register
// under their headless-Service DNS name, while Deployment pods (which have no
// stable per-pod name) keep using the pod IP.
func TestBuildStatefulSet_StorkAgentHost(t *testing.T) {
	stork := &StorkSidecarParams{Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547,
		ServerURL: testStorkServerURL, ServerToken: testStorkServerToken}
	agentEnv := func(spec corev1.PodSpec) map[string]corev1.EnvVar {
		for _, c := range spec.Containers {
			if c.Name == StorkAgentContainerName {
				m := map[string]corev1.EnvVar{}
				for _, e := range c.Env {
					m[e.Name] = e
				}
				return m
			}
		}
		t.Fatal("no stork-agent container")
		return nil
	}

	sts := BuildStatefulSet(StatefulSetParams{
		CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		ConfigMapNames: testSTSConfigMaps(), StorkAgent: stork,
	})
	want := "$(POD_NAME)." + HeadlessServiceName(testCRName, ComponentDhcp4) + "." + testNamespace + ".svc.cluster.local"
	if got := agentEnv(sts.Spec.Template.Spec)[envStorkAgentHost].Value; got != want {
		t.Errorf("statefulset STORK_AGENT_HOST = %q, want %q", got, want)
	}
	if sts.Spec.ServiceName != HeadlessServiceName(testCRName, ComponentDhcp4) {
		t.Errorf("serviceName %q must match the agent's DNS suffix", sts.Spec.ServiceName)
	}
	if stork.AgentDomain != "" {
		t.Error("BuildStatefulSet must not modify the caller's StorkSidecarParams")
	}

	dep := BuildDeployment(DeploymentParams{
		CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		ConfigMapName: testDeployConfigMap, StorkAgent: stork,
	})
	host := agentEnv(dep.Spec.Template.Spec)[envStorkAgentHost]
	if host.ValueFrom == nil || host.ValueFrom.FieldRef == nil || host.ValueFrom.FieldRef.FieldPath != fieldPathPodIP {
		t.Errorf("deployment STORK_AGENT_HOST should stay on status.podIP, got %+v", host)
	}
}

// TestStorkServerPodLabel checks that agents reporting to an in-cluster
// KeaStorkServer carry LabelStorkServer on the pod template only. The selector
// must not change, because a StatefulSet's selector is immutable.
func TestStorkServerPodLabel(t *testing.T) {
	withServer := &StorkSidecarParams{Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547,
		ServerURL: testStorkServerURL, ServerToken: testStorkServerToken, ServerName: "stork"}
	external := &StorkSidecarParams{Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547,
		ServerURL: testExternalStorkURL}

	for _, tc := range []struct {
		name      string
		sp        *StorkSidecarParams
		wantLabel string
	}{
		{"in-cluster server", withServer, "stork"},
		{"external server", external, ""},
		{"no stork", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sts := BuildStatefulSet(StatefulSetParams{
				CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
				Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
				ConfigMapNames: testSTSConfigMaps(), StorkAgent: tc.sp,
			})
			dep := BuildDeployment(DeploymentParams{
				CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
				Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
				ConfigMapName: testDeployConfigMap, StorkAgent: tc.sp,
			})
			for kind, tmpl := range map[string]map[string]string{kindStatefulSet: sts.Spec.Template.Labels, kindDeployment: dep.Spec.Template.Labels} {
				if got := tmpl[LabelStorkServer]; got != tc.wantLabel {
					t.Errorf("%s template label = %q, want %q", kind, got, tc.wantLabel)
				}
			}
			for kind, sel := range map[string]map[string]string{kindStatefulSet: sts.Spec.Selector.MatchLabels, kindDeployment: dep.Spec.Selector.MatchLabels} {
				if _, ok := sel[LabelStorkServer]; ok {
					t.Errorf("%s selector must not carry %s", kind, LabelStorkServer)
				}
			}
		})
	}
}

// TestBuildStorkAgentContainer_ServerTokenRegistration guards the auto-authorization
// fix: the agent daemon only does agent-token registration (manual approval), so a
// server token must go through "stork-agent register" before the daemon starts.
func TestBuildStorkAgentContainer_ServerTokenRegistration(t *testing.T) {
	withToken := buildStorkAgentContainer(&StorkSidecarParams{
		Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547,
		ServerURL: testStorkServerURL, ServerToken: testStorkServerToken,
	}, "kea-config", nil)
	if len(withToken.Command) != 3 || withToken.Command[0] != "/bin/sh" || withToken.Command[1] != "-c" {
		t.Fatalf("expected a /bin/sh -c command, got %q", withToken.Command)
	}
	script := withToken.Command[2]
	for _, want := range []string{
		"stork-agent register --non-interactive",
		`--server-token "$STORK_AGENT_SERVER_TOKEN"`,
		`--agent-host "$STORK_AGENT_HOST"`,
		`--agent-port "$STORK_AGENT_PORT"`,
		"exec /usr/bin/stork-agent",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("register script missing %q", want)
		}
	}
	if strings.Index(script, "register") > strings.Index(script, "exec /usr/bin/stork-agent") {
		t.Error("register must run before the agent daemon starts (it needs the agent port free)")
	}

	for name, sp := range map[string]*StorkSidecarParams{
		"server without token": {Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547, ServerURL: testStorkServerURL},
		"prometheus only":      {Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547},
	} {
		if c := buildStorkAgentContainer(sp, "kea-config", nil); c.Command != nil {
			t.Errorf("%s: expected the image entrypoint, got command %q", name, c.Command)
		}
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
			ConfigMapName: testDeployConfigMap, StorkAgent: stork,
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
			ConfigMapNames: testSTSConfigMaps(), StorkAgent: stork,
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
		{kindDeployment, deploy(false)},
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
