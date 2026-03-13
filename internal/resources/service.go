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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServiceParams holds all parameters needed to build a Kea component Service.
type ServiceParams struct {
	Namespace string
	CRName    string
	Component string
	Port      int32
	Protocol  corev1.Protocol // TCP or UDP
}

// BuildHeadlessService constructs a headless Service (clusterIP: None) for
// StatefulSet DNS resolution in HA mode.
func BuildHeadlessService(p ServiceParams) *corev1.Service {
	labels := CommonLabels(p.CRName, p.Component)

	protocol := p.Protocol
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HeadlessServiceName(p.CRName, p.Component),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                "None",
			PublishNotReadyAddresses: true,
			Selector:                 labels,
			Ports: []corev1.ServicePort{
				{
					Name:       p.Component,
					Port:       p.Port,
					TargetPort: intstr.FromInt32(p.Port),
					Protocol:   protocol,
				},
			},
		},
	}
}

// BuildService constructs a Kubernetes Service for a Kea component based on
// the provided parameters.
func BuildService(p ServiceParams) *corev1.Service {
	labels := CommonLabels(p.CRName, p.Component)

	protocol := p.Protocol
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(p.CRName, p.Component),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       p.Component,
					Port:       p.Port,
					TargetPort: intstr.FromInt32(p.Port),
					Protocol:   protocol,
				},
			},
		},
	}
}

// StorkMetricsServiceParams holds the parameters for building a Stork metrics Service.
type StorkMetricsServiceParams struct {
	Namespace      string
	CRName         string
	Component      string
	AgentPort      int32
	PrometheusPort int32
}

// BuildStorkMetricsService constructs a ClusterIP Service that exposes the
// Stork agent gRPC port and Prometheus metrics exporter port.
func BuildStorkMetricsService(p StorkMetricsServiceParams) *corev1.Service {
	labels := CommonLabels(p.CRName, p.Component)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StorkMetricsServiceName(p.CRName, p.Component),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Name:       "stork-prom",
					Port:       p.PrometheusPort,
					TargetPort: intstr.FromString("stork-prom"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "stork-agent",
					Port:       p.AgentPort,
					TargetPort: intstr.FromString("stork-agent"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// BuildPerPodService constructs a ClusterIP Service that targets a specific
// StatefulSet pod by ordinal. Unlike the headless Service (which resolves to
// ephemeral pod IPs), per-pod Services have stable ClusterIPs that survive
// pod restarts. This is critical for Kea HA because peer URLs are resolved
// to IPs at init time and Kea cannot re-resolve DNS hostnames.
func BuildPerPodService(p ServiceParams, ordinal int) *corev1.Service {
	labels := CommonLabels(p.CRName, p.Component)
	stsName := StatefulSetName(p.CRName, p.Component)
	podName := fmt.Sprintf("%s-%d", stsName, ordinal)

	protocol := p.Protocol
	if protocol == "" {
		protocol = corev1.ProtocolTCP
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PerPodServiceName(p.CRName, p.Component, ordinal),
			Namespace: p.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"statefulset.kubernetes.io/pod-name": podName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       p.Component,
					Port:       p.Port,
					TargetPort: intstr.FromInt32(p.Port),
					Protocol:   protocol,
				},
			},
		},
	}
}
