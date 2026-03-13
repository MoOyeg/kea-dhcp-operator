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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RouteGVK is the GroupVersionKind for OpenShift Routes.
var RouteGVK = schema.GroupVersionKind{
	Group:   "route.openshift.io",
	Version: "v1",
	Kind:    "Route",
}

// StorkRouteParams holds parameters for building a Stork metrics Route.
type StorkRouteParams struct {
	Namespace      string
	CRName         string
	Component      string
	PrometheusPort int32
}

// BuildStorkMetricsRoute constructs an OpenShift Route (as unstructured) that
// exposes the Stork Prometheus metrics endpoint externally. Uses edge TLS
// termination so the Route is accessible over HTTPS.
func BuildStorkMetricsRoute(p StorkRouteParams) *unstructured.Unstructured {
	labels := CommonLabels(p.CRName, p.Component)
	svcName := StorkMetricsServiceName(p.CRName, p.Component)
	routeName := StorkRouteName(p.CRName, p.Component)

	// Convert labels to map[string]interface{} for unstructured.
	labelsIface := make(map[string]interface{}, len(labels))
	for k, v := range labels {
		labelsIface[k] = v
	}

	route := &unstructured.Unstructured{}
	route.SetGroupVersionKind(RouteGVK)
	route.Object = map[string]interface{}{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata": map[string]interface{}{
			"name":      routeName,
			"namespace": p.Namespace,
			"labels":    labelsIface,
		},
		"spec": map[string]interface{}{
			"to": map[string]interface{}{
				"kind": "Service",
				"name": svcName,
			},
			"port": map[string]interface{}{
				"targetPort": "stork-prom",
			},
			"tls": map[string]interface{}{
				"termination": "edge",
			},
		},
	}

	return route
}

// StorkRouteName returns the Route name for Stork metrics.
func StorkRouteName(crName, component string) string {
	return StorkMetricsServiceName(crName, component)
}
