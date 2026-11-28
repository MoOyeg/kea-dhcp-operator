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
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
)

// testCtrlAgentImage stands for an image the user maintains: ISC publishes
// no Kea Control Agent image.
const testCtrlAgentImage = "quay.io/example/kea-ctrl-agent:3.0.2"

const testSocketTypeHTTP = "http"

func findCondition(conds []keav1alpha1.ConditionStatus, t string) *keav1alpha1.ConditionStatus {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

// A KeaControlAgent stored before the CRD required an image (or UNIX sockets)
// must not be deployed: the operator reports why and leaves the workload
// alone. Every KeaControlAgent carries the Deprecated condition.
func TestCtrlAgentThatCannotRunIsReported(t *testing.T) {
	port := int32(8001)
	cases := []struct {
		name, reason string
		spec         keav1alpha1.KeaControlAgentSpec
	}{
		{"no image", "ImageRequired", keav1alpha1.KeaControlAgentSpec{}},
		{"HTTP socket", "UnsupportedControlSocket", keav1alpha1.KeaControlAgentSpec{
			Container: keav1alpha1.ContainerConfig{Image: testCtrlAgentImage},
			ControlSockets: &keav1alpha1.AgentControlSockets{
				D2: &keav1alpha1.ControlSocket{SocketType: testSocketTypeHTTP, SocketPort: &port},
			},
		}},
		{"runnable", "", keav1alpha1.KeaControlAgentSpec{
			Container: keav1alpha1.ContainerConfig{Image: testCtrlAgentImage},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := &keav1alpha1.KeaControlAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: testNamespaceDefault},
				Spec:       tc.spec,
			}
			c := fake.NewClientBuilder().WithScheme(controllerTestScheme(t)).
				WithStatusSubresource(&keav1alpha1.KeaControlAgent{}).WithObjects(agent).Build()
			recorder := record.NewFakeRecorder(10)
			if err := reconcileWith(&KeaControlAgentReconciler{Client: c, Scheme: c.Scheme(), Recorder: recorder}, "legacy"); err != nil {
				t.Fatalf("reconcile: %v", err)
			}

			got := &keav1alpha1.KeaControlAgent{}
			if err := c.Get(context.Background(), types.NamespacedName{Name: "legacy", Namespace: testNamespaceDefault}, got); err != nil {
				t.Fatal(err)
			}
			if d := findCondition(got.Status.Conditions, keav1alpha1.ConditionTypeDeprecated); d == nil || d.Status != string(metav1.ConditionTrue) ||
				!strings.Contains(d.Message, "docs/control-agent.md") {
				t.Errorf("Deprecated condition = %+v, want True pointing at docs/control-agent.md", d)
			}
			err := c.Get(context.Background(), types.NamespacedName{Name: "legacy-ctrl-agent", Namespace: testNamespaceDefault}, &appsv1.Deployment{})
			if tc.reason == "" {
				if err != nil {
					t.Errorf("runnable Control Agent: Deployment not created: %v", err)
				}
				return
			}
			if !errors.IsNotFound(err) {
				t.Errorf("Deployment exists (err = %v); a Control Agent that cannot run must not be deployed", err)
			}
			cv := findCondition(got.Status.Conditions, keav1alpha1.ConditionTypeConfigValid)
			if cv == nil || cv.Status != string(metav1.ConditionFalse) || cv.Reason != tc.reason {
				t.Errorf("ConfigurationValid = %+v, want False/%s", cv, tc.reason)
			}
			if got.Status.Phase != keav1alpha1.PhaseError {
				t.Errorf("phase = %q, want %q", got.Status.Phase, keav1alpha1.PhaseError)
			}
			select {
			case e := <-recorder.Events:
				if !strings.Contains(e, tc.reason) {
					t.Errorf("event %q does not name %s", e, tc.reason)
				}
			default:
				t.Errorf("no Warning event for %s", tc.reason)
			}
		})
	}
}
