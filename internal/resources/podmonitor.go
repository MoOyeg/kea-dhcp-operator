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

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// PodMonitorName returns the conventional PodMonitor name for a given CR and component.
func PodMonitorName(crName, component string) string {
	return fmt.Sprintf("%s-%s", crName, component)
}

// BuildPodMonitor creates a PodMonitor that scrapes the stork-agent Prometheus
// metrics endpoint on each Kea pod.
func BuildPodMonitor(namespace, crName, component string) *monitoringv1.PodMonitor {
	labels := CommonLabels(crName, component)

	return &monitoringv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PodMonitorName(crName, component),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{
					Port:     ptr.To("stork-prom"),
					Path:     "/metrics",
					Interval: monitoringv1.Duration("30s"),
				},
			},
		},
	}
}
