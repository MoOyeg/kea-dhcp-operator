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

package controller

import (
	"context"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

// An HA server with an HTTP control socket: the HA Services and the peer URLs
// the operator fills in use the HA listener's port, and the control Service
// uses the control socket's port. Without http-listener-port nothing changes
// from v0.0.36: socket-port is the HA port too, which works because the
// control socket stays on loopback.
func TestDhcp4HAListenerAndHTTPControlSocketPorts(t *testing.T) {
	csPort, explicitHA := int32(8001), int32(8100)
	cases := []struct {
		name       string
		address    string
		listener   *int32
		wantHAPort int32
	}{
		{"v0.0.36: loopback control socket, socket-port shared", "", nil, csPort},
		{"control socket beyond the pod, http-listener-port", "0.0.0.0", &explicitHA, explicitHA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := haDhcp4Server(2)
			server.Spec.ControlSocket = &keav1alpha1.ControlSocket{
				SocketType: testSocketTypeHTTP, SocketAddress: tc.address, SocketPort: &csPort}
			server.Spec.HighAvailability.HTTPListenerPort = tc.listener
			r, c := newDhcp4Reconciler(t, nil, server)
			if err := reconcileDhcp4(r); err != nil {
				t.Fatalf("reconcile: %v", err)
			}
			ctx := context.Background()
			port := func(name string) int32 {
				t.Helper()
				svc := &corev1.Service{}
				if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: testNamespaceDefault}, svc); err != nil {
					t.Fatalf("Service %s: %v", name, err)
				}
				return svc.Spec.Ports[0].Port
			}
			if p := port(resources.HeadlessServiceName(testDhcp4CR, resources.ComponentDhcp4)); p != tc.wantHAPort {
				t.Errorf("headless Service port = %d, want %d", p, tc.wantHAPort)
			}
			for i := 0; i < 2; i++ {
				if p := port(resources.PerPodServiceName(testDhcp4CR, resources.ComponentDhcp4, i)); p != tc.wantHAPort {
					t.Errorf("per-pod Service %d port = %d, want %d", i, p, tc.wantHAPort)
				}
			}
			if p := port(resources.ServiceName(testDhcp4CR, resources.ComponentDhcp4)); p != csPort {
				t.Errorf("control Service port = %d, want %d", p, csPort)
			}

			list := &corev1.ConfigMapList{}
			if err := c.List(ctx, list); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, cm := range list.Items {
				for _, data := range cm.Data {
					if !strings.Contains(data, "high-availability") {
						continue
					}
					found = true
					if !strings.Contains(data, ".svc.cluster.local:"+strconv.Itoa(int(tc.wantHAPort))+"/") {
						t.Errorf("ConfigMap %s: peer URLs do not use port %d", cm.Name, tc.wantHAPort)
					}
					if tc.wantHAPort != csPort && strings.Contains(data, ".svc.cluster.local:"+strconv.Itoa(int(csPort))+"/") {
						t.Errorf("ConfigMap %s: a peer URL uses the control socket's port %d", cm.Name, csPort)
					}
				}
			}
			if !found {
				t.Fatal("no rendered HA configuration")
			}
			if cv := conditionOf(getDhcp4(t, c), keav1alpha1.ConditionTypeConfigValid); cv == nil || cv.Status != condTrue {
				t.Errorf("ConfigurationValid = %+v, want True", cv)
			}
		})
	}
}
