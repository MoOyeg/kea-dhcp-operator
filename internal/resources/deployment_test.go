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
	"testing"
)

func TestBuildDeploymentDefaultImages(t *testing.T) {
	tests := []struct {
		component     string
		expectedImage string
	}{
		{"dhcp4", DefaultKeaDhcp4Image},
		{"dhcp6", DefaultKeaDhcp6Image},
		{"ctrl-agent", DefaultKeaCtrlAgentImage},
		{"ddns", DefaultKeaDhcpDdnsImage},
	}
	for _, tc := range tests {
		t.Run(tc.component, func(t *testing.T) {
			dp := DeploymentParams{
				CRName:         "test",
				Namespace:      "default",
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
		CRName:         "test",
		Namespace:      "default",
		Component:      "dhcp4",
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
		{"dhcp4", DefaultKeaDhcp4Image},
		{"dhcp6", DefaultKeaDhcp6Image},
		{"ctrl-agent", DefaultKeaCtrlAgentImage},
		{"ddns", DefaultKeaDhcpDdnsImage},
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
