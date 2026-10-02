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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

const (
	testCRName    = "test"
	testNamespace = "default"
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
		{name: "dhcp4 with NAD", component: ComponentDhcp4, nadIface: "net1", nadSubnet: "10.200.0.0/24",
			wantSuffix: dhcp4Exec},
		{name: "dhcp4 with secret env vars", component: ComponentDhcp4, secretEnv: secretEnv,
			wantSuffix: dhcp4Exec},
		{name: "dhcp4 with NAD and secret env vars", component: ComponentDhcp4,
			nadIface: "net1", nadSubnet: "10.200.0.0/24", secretEnv: secretEnv,
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
