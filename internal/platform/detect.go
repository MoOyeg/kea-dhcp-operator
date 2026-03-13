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

package platform

import (
	"sync"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// Detector provides platform detection capabilities.
type Detector interface {
	// IsOpenShift returns true when the cluster exposes the
	// route.openshift.io API group, indicating an OpenShift environment.
	IsOpenShift() bool
}

// NewDetector creates a Detector that uses the Kubernetes discovery API to
// determine the target platform. The result is cached after the first check.
func NewDetector(config *rest.Config) Detector {
	return &platformDetector{
		config: config,
	}
}

// platformDetector implements Detector using the Kubernetes discovery client.
type platformDetector struct {
	config *rest.Config

	once        sync.Once
	isOpenShift bool
}

// IsOpenShift checks whether the route.openshift.io API group is available
// on the cluster. The result is cached after the first successful probe.
func (d *platformDetector) IsOpenShift() bool {
	d.once.Do(func() {
		d.isOpenShift = d.detect()
	})
	return d.isOpenShift
}

// detect queries the API server for the route.openshift.io group.
func (d *platformDetector) detect() bool {
	client, err := discovery.NewDiscoveryClientForConfig(d.config)
	if err != nil {
		return false
	}

	groups, err := client.ServerGroups()
	if err != nil {
		return false
	}

	for _, g := range groups.Groups {
		if g.Name == "route.openshift.io" {
			return true
		}
	}
	return false
}
