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

import "fmt"

// Standard Kubernetes recommended labels.
const (
	LabelApp       = "app.kubernetes.io/name"
	LabelInstance  = "app.kubernetes.io/instance"
	LabelComponent = "app.kubernetes.io/component"
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelPartOf    = "app.kubernetes.io/part-of"
	ManagedByValue = "kea-operator"
	PartOfValue    = "kea-dhcp"
)

// ConfigMapName returns the conventional ConfigMap name for a given CR and component.
func ConfigMapName(crName, component string) string {
	return fmt.Sprintf("%s-%s", crName, component)
}

// DeploymentName returns the conventional Deployment name for a given CR and component.
func DeploymentName(crName, component string) string {
	return fmt.Sprintf("%s-%s", crName, component)
}

// ServiceName returns the conventional Service name for a given CR and component.
func ServiceName(crName, component string) string {
	return fmt.Sprintf("%s-%s", crName, component)
}

// PerPodServiceName returns the Service name for a specific StatefulSet pod ordinal.
// These per-pod Services have stable ClusterIPs that survive pod restarts, unlike
// the headless Service which resolves to ephemeral pod IPs.
func PerPodServiceName(crName, component string, ordinal int) string {
	return fmt.Sprintf("%s-%s-%d", crName, component, ordinal)
}

// ServiceAccountName returns the conventional ServiceAccount name for a given CR.
func ServiceAccountName(crName string) string {
	return fmt.Sprintf("%s-kea", crName)
}

// StorkMetricsServiceName returns the Service name for Stork metrics/agent endpoints.
func StorkMetricsServiceName(crName, component string) string {
	return fmt.Sprintf("%s-%s-stork", crName, component)
}

// StorkAdminSecretName returns the Secret name for Stork admin credentials.
func StorkAdminSecretName(crName string) string {
	return fmt.Sprintf("%s-stork-admin", crName)
}

// StorkServerTokenSecretName returns the Secret name for the Stork server agent token.
func StorkServerTokenSecretName(crName string) string {
	return fmt.Sprintf("%s-stork-server-token", crName)
}

// CommonLabels returns the set of standard Kubernetes labels applied to all
// resources owned by a given CR and component.
func CommonLabels(crName, component string) map[string]string {
	return map[string]string{
		LabelApp:       "kea",
		LabelInstance:  crName,
		LabelComponent: component,
		LabelManagedBy: ManagedByValue,
		LabelPartOf:    PartOfValue,
	}
}
