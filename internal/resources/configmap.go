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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// componentConfigFile maps a component name to the corresponding Kea
// configuration file name.
var componentConfigFile = map[string]string{
	"dhcp4":      "kea-dhcp4.conf",
	"dhcp6":      "kea-dhcp6.conf",
	"ctrl-agent": "kea-ctrl-agent.conf",
	"ddns":       "kea-dhcp-ddns.conf",
}

// ConfigFileName returns the Kea configuration file name for the given
// component. If the component is not recognized, it falls back to
// "kea-<component>.conf".
func ConfigFileName(component string) string {
	if name, ok := componentConfigFile[component]; ok {
		return name
	}
	return "kea-" + component + ".conf"
}

// ConfigMapMeta returns an ObjectMeta for a ConfigMap with the given name,
// namespace, and labels.
func ConfigMapMeta(name, namespace string, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
		Labels:    labels,
	}
}

// BuildConfigMap constructs a ConfigMap containing the rendered Kea JSON
// configuration for a specific component.
func BuildConfigMap(namespace, crName, component string, configJSON []byte, labels map[string]string) *corev1.ConfigMap {
	filename := ConfigFileName(component)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(crName, component),
			Namespace: namespace,
			Labels:    labels,
		},
		Data: map[string]string{
			filename: string(configJSON),
		},
	}
}
