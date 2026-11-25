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

package resources

import (
	"os/exec"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const (
	testSecretKey    = "db-password"
	testCM0          = "cm-0"
	testCM1          = "cm-1"
	testConfigSecret = "test-dhcp4-config"
	testCRName       = "test"
	testNamespace    = "default"
	testNADSubnet    = "10.200.0.0/24"
	testNADIface     = "net1"
)

func TestBuildDeploymentDefaultImages(t *testing.T) {
	tests := []struct {
		component     string
		expectedImage string
	}{
		{ComponentDhcp4, DefaultKeaDhcp4Image},
		{ComponentDhcp6, DefaultKeaDhcp6Image},
		{ComponentCtrlAgent, DefaultKeaCtrlAgentImage},
		{ComponentDdns, DefaultKeaDhcpDdnsImage},
	}
	for _, tc := range tests {
		t.Run(tc.component, func(t *testing.T) {
			dp := DeploymentParams{
				CRName:         testCRName,
				Namespace:      testNamespace,
				Component:      tc.component,
				Command:        "kea-" + tc.component,
				ConfigFileName: "kea-" + tc.component + ".conf",
				ConfigMapName:  "test-" + tc.component,
			}
			deploy := BuildDeployment(dp)
			got := deploy.Spec.Template.Spec.Containers[0].Image
			if got != tc.expectedImage {
				t.Errorf("expected %s, got %s", tc.expectedImage, got)
			}
		})
	}
}

func TestBuildDeploymentUserImageOverride(t *testing.T) {
	customImage := "my-registry.example.com/custom-kea:latest"
	dp := DeploymentParams{
		CRName:         testCRName,
		Namespace:      testNamespace,
		Component:      ComponentDhcp4,
		Command:        "/usr/sbin/kea-dhcp4",
		ConfigFileName: "kea-dhcp4.conf",
		ConfigMapName:  "test-dhcp4",
		Image:          customImage,
	}
	deploy := BuildDeployment(dp)
	got := deploy.Spec.Template.Spec.Containers[0].Image
	if got != customImage {
		t.Errorf("expected user image %s, got %s", customImage, got)
	}
}

func TestDefaultImageForComponent(t *testing.T) {
	tests := []struct {
		component string
		expected  string
	}{
		{ComponentDhcp4, DefaultKeaDhcp4Image},
		{ComponentDhcp6, DefaultKeaDhcp6Image},
		{ComponentCtrlAgent, DefaultKeaCtrlAgentImage},
		{ComponentDdns, DefaultKeaDhcpDdnsImage},
		{"unknown", ""},
	}
	for _, tc := range tests {
		t.Run(tc.component, func(t *testing.T) {
			got := DefaultImageForComponent(tc.component)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestBuildDeploymentRunDirPermissions(t *testing.T) {
	chmod := "chmod 750 " + RunMountPath + "; "
	dhcp4Exec := "exec /usr/sbin/kea-dhcp4 -c /etc/kea/kea-dhcp4.conf"
	tests := []struct {
		name         string
		component    string
		nadIface     string
		nadSubnet    string
		configSecret string
		wantSuffix   string
	}{
		{name: "dhcp4 plain", component: ComponentDhcp4,
			wantSuffix: dhcp4Exec},
		{name: "dhcp6 plain", component: ComponentDhcp6,
			wantSuffix: "exec /usr/sbin/kea-dhcp6 -c /etc/kea/kea-dhcp6.conf"},
		{name: "ctrl-agent plain", component: ComponentCtrlAgent,
			wantSuffix: "exec /usr/sbin/kea-ctrl-agent -c /etc/kea/kea-ctrl-agent.conf"},
		{name: "ddns plain", component: ComponentDdns,
			wantSuffix: "exec /usr/sbin/kea-dhcp-ddns -c /etc/kea/kea-ddns.conf"},
		{name: "dhcp4 with NAD", component: ComponentDhcp4, nadIface: testNADIface, nadSubnet: testNADSubnet,
			wantSuffix: dhcp4Exec},
		{name: "dhcp4 with a configuration Secret", component: ComponentDhcp4, configSecret: testConfigSecret,
			wantSuffix: dhcp4Exec},
		{name: "dhcp4 with NAD and a configuration Secret", component: ComponentDhcp4,
			nadIface: testNADIface, nadSubnet: testNADSubnet, configSecret: testConfigSecret,
			wantSuffix: dhcp4Exec},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deploy := BuildDeployment(DeploymentParams{
				CRName:           testCRName,
				Namespace:        testNamespace,
				Component:        tc.component,
				Command:          DefaultCommandForComponent(tc.component),
				ConfigFileName:   "kea-" + tc.component + ".conf",
				ConfigMapName:    "test-" + tc.component,
				NADInterface:     tc.nadIface,
				NADSubnet:        tc.nadSubnet,
				ConfigSecretName: tc.configSecret,
			})
			cmd := deploy.Spec.Template.Spec.Containers[0].Command
			if len(cmd) != 3 || cmd[0] != "sh" || cmd[1] != "-c" {
				t.Fatalf("expected sh -c wrapper, got %q", cmd)
			}
			if !strings.HasPrefix(cmd[2], chmod) {
				t.Errorf("expected script to start with %q, got %q", chmod, cmd[2])
			}
			if strings.Count(cmd[2], "chmod 750") != 1 {
				t.Errorf("expected exactly one chmod, got %q", cmd[2])
			}
			if !strings.HasSuffix(cmd[2], tc.wantSuffix) {
				t.Errorf("expected script to end with %q, got %q", tc.wantSuffix, cmd[2])
			}
		})
	}
}

// With credentials rendered into a configuration Secret, the Deployment
// mounts that Secret where the ConfigMap would go and does nothing at startup:
// no credential env vars, no template copy, no sed. The Stork sidecar reads
// the same volume.
func TestDeploymentMountsConfigSecret(t *testing.T) {
	stork := &StorkSidecarParams{Image: testStorkAgentImage, Port: 8080, PrometheusPort: 9547}
	deploy := BuildDeployment(DeploymentParams{
		CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		ConfigMapName: testDeployConfigMap, ConfigSecretName: testConfigSecret, StorkAgent: stork,
	})
	spec := deploy.Spec.Template.Spec

	var source *corev1.SecretVolumeSource
	for _, v := range spec.Volumes {
		if v.Name == ConfigVolumeName {
			source = v.Secret
			if v.ConfigMap != nil {
				t.Error("configuration volume still uses the ConfigMap")
			}
		}
	}
	if source == nil || source.SecretName != testConfigSecret {
		t.Fatalf("configuration volume is not the Secret %q: %+v", testConfigSecret, spec.Volumes)
	}
	if source.DefaultMode == nil || *source.DefaultMode != ConfigSecretMode {
		t.Errorf("configuration Secret mode = %v, want %o", source.DefaultMode, ConfigSecretMode)
	}

	for _, c := range append(spec.InitContainers, spec.Containers...) {
		for _, ev := range c.Env {
			if ev.ValueFrom != nil && ev.ValueFrom.SecretKeyRef != nil {
				t.Errorf("container %s still takes credential %s from an env var", c.Name, ev.Name)
			}
		}
		script := strings.Join(c.Command, " ")
		for _, forbidden := range []string{"sed ", "cp /etc/kea"} {
			if strings.Contains(script, forbidden) {
				t.Errorf("container %s still rewrites its configuration at startup: %q", c.Name, script)
			}
		}
		mounted := false
		for _, m := range c.VolumeMounts {
			if m.Name == ConfigVolumeName && m.MountPath == ConfigMountPath && m.ReadOnly {
				mounted = true
			}
		}
		if !mounted {
			t.Errorf("container %s does not mount the configuration read-only at %s", c.Name, ConfigMountPath)
		}
	}
}

// The NAD setup script must still run to the Kea exec when the configuration
// comes from a Secret (a NAD with credentials used to never start Kea).
func TestDeploymentNADScriptWithConfigSecretReachesKea(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	deploy := BuildDeployment(DeploymentParams{
		CRName: testCRName, Namespace: testNamespace, Component: ComponentDhcp4,
		Command: DefaultCommandForComponent(ComponentDhcp4), ConfigFileName: ConfigFileName(ComponentDhcp4),
		ConfigMapName: testDeployConfigMap, ConfigSecretName: testConfigSecret,
		NADInterface: testNADIface, NADSubnet: testNADSubnet,
	})
	script := deploy.Spec.Template.Spec.Containers[0].Command[2]
	run := t.TempDir()
	script = strings.ReplaceAll(script, RunMountPath, run)
	script = strings.Replace(script, "exec "+DefaultCommandForComponent(ComponentDhcp4), "exec echo KEA-STARTED", 1)
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "KEA-STARTED") {
		t.Fatalf("script did not reach the Kea exec (err %v):\n%s\n--- script ---\n%s", err, out, script)
	}
}
