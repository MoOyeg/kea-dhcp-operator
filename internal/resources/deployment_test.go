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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const (
	testCRName    = "test"
	testNamespace = "default"
	testNADSubnet = "10.200.0.0/24"
	testNADIface  = "net1"
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
	secretEnv := []corev1.EnvVar{{
		Name: "KEA_LEASE_DB_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "db"},
			Key:                  "db-password",
		}},
	}}

	tests := []struct {
		name       string
		component  string
		nadIface   string
		nadSubnet  string
		secretEnv  []corev1.EnvVar
		wantSuffix string
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
		{name: "dhcp4 with secret env vars", component: ComponentDhcp4, secretEnv: secretEnv,
			wantSuffix: dhcp4Exec},
		{name: "dhcp4 with NAD and secret env vars", component: ComponentDhcp4,
			nadIface: testNADIface, nadSubnet: testNADSubnet, secretEnv: secretEnv,
			wantSuffix: "/etc/kea/kea-dhcp4.conf"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deploy := BuildDeployment(DeploymentParams{
				CRName:         testCRName,
				Namespace:      testNamespace,
				Component:      tc.component,
				Command:        DefaultCommandForComponent(tc.component),
				ConfigFileName: "kea-" + tc.component + ".conf",
				ConfigMapName:  "test-" + tc.component,
				NADInterface:   tc.nadIface,
				NADSubnet:      tc.nadSubnet,
				SecretEnvVars:  tc.secretEnv,
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

// TestSecretEnvSubstScriptSubstitutesConfFile runs the generated startup
// script against a temp copy of the config layout and checks that the env var
// placeholder is really replaced in the "kea-*.conf" file. The config files
// are named "*.conf", so a "*.json" glob matched nothing and silently left the
// placeholder in place.
func TestSecretEnvSubstScriptSubstitutesConfFile(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	secretEnv := []corev1.EnvVar{{
		Name: "KEA_TSIG_SECRET_0",
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "tsig"},
			Key:                  "secret",
		}},
	}}
	deploy := BuildDeployment(DeploymentParams{
		CRName:         testCRName,
		Namespace:      testNamespace,
		Component:      ComponentDdns,
		Command:        DefaultCommandForComponent(ComponentDdns),
		ConfigFileName: ConfigFileName(ComponentDdns),
		ConfigMapName:  "test-ddns",
		SecretEnvVars:  secretEnv,
	})
	script := deploy.Spec.Template.Spec.Containers[0].Command[2]
	if strings.Contains(script, "*.json") {
		t.Fatalf("script still targets *.json (config files are *.conf): %q", script)
	}

	// Lay out template + writable dirs under a temp root and point the script at them.
	root := t.TempDir()
	tmpl := filepath.Join(root, "template")
	cfg := filepath.Join(root, "config")
	run := filepath.Join(root, "run")
	for _, d := range []string{tmpl, cfg, run} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	confName := ConfigFileName(ComponentDdns)
	if err := os.WriteFile(filepath.Join(tmpl, confName), []byte(`{"secret": "$KEA_TSIG_SECRET_0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	script = strings.ReplaceAll(script, ConfigTemplateMountPath, tmpl)
	script = strings.ReplaceAll(script, RunMountPath, run)
	script = strings.ReplaceAll(script, ConfigMountPath, cfg)
	// Don't exec the real Kea binary; the substitution is what is under test.
	script = strings.Replace(script, "exec "+DefaultCommandForComponent(ComponentDdns), "exec true", 1)

	cmd := exec.Command("sh", "-c", script)
	cmd.Env = append(os.Environ(), "KEA_TSIG_SECRET_0=c2VjcmV0Cg==")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(cfg, confName))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"secret": "c2VjcmV0Cg=="}`; string(got) != want {
		t.Errorf("placeholder not substituted: got %q, want %q", got, want)
	}
}
