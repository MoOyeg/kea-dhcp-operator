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
	"testing"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIsChildReady(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := keav1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	t.Run("returns false when child does not exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &KeaServerReconciler{Client: c, Scheme: scheme}

		ready := r.isChildReady(context.Background(), "default", "nonexistent", &keav1alpha1.KeaDhcp4Server{})
		if ready {
			t.Error("expected false for non-existent child")
		}
	})

	t.Run("returns false when phase is Progressing", func(t *testing.T) {
		child := &keav1alpha1.KeaDhcp4Server{
			ObjectMeta: metav1.ObjectMeta{Name: "dhcp4-child", Namespace: "default"},
			Status: keav1alpha1.KeaDhcp4ServerStatus{
				ComponentStatus: keav1alpha1.ComponentStatus{Phase: "Progressing"},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(child).WithObjects(child).Build()
		r := &KeaServerReconciler{Client: c, Scheme: scheme}

		ready := r.isChildReady(context.Background(), "default", "dhcp4-child", &keav1alpha1.KeaDhcp4Server{})
		if ready {
			t.Error("expected false for Progressing phase")
		}
	})

	t.Run("returns true when phase is Running and Ready condition True", func(t *testing.T) {
		child := &keav1alpha1.KeaDhcp4Server{
			ObjectMeta: metav1.ObjectMeta{Name: "dhcp4-child", Namespace: "default"},
			Status: keav1alpha1.KeaDhcp4ServerStatus{
				ComponentStatus: keav1alpha1.ComponentStatus{
					Phase: "Running",
					Conditions: []keav1alpha1.ConditionStatus{
						{Type: keav1alpha1.ConditionTypeReady, Status: "True"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(child).WithObjects(child).Build()
		r := &KeaServerReconciler{Client: c, Scheme: scheme}

		ready := r.isChildReady(context.Background(), "default", "dhcp4-child", &keav1alpha1.KeaDhcp4Server{})
		if !ready {
			t.Error("expected true for Running phase with Ready=True condition")
		}
	})

	t.Run("returns false when phase is Running but Ready condition False", func(t *testing.T) {
		child := &keav1alpha1.KeaDhcp6Server{
			ObjectMeta: metav1.ObjectMeta{Name: "dhcp6-child", Namespace: "default"},
			Status: keav1alpha1.KeaDhcp6ServerStatus{
				ComponentStatus: keav1alpha1.ComponentStatus{
					Phase: "Running",
					Conditions: []keav1alpha1.ConditionStatus{
						{Type: keav1alpha1.ConditionTypeReady, Status: "False"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(child).WithObjects(child).Build()
		r := &KeaServerReconciler{Client: c, Scheme: scheme}

		ready := r.isChildReady(context.Background(), "default", "dhcp6-child", &keav1alpha1.KeaDhcp6Server{})
		if ready {
			t.Error("expected false for Running phase with Ready=False condition")
		}
	})

	t.Run("returns true when phase is Running with no conditions (backward compat)", func(t *testing.T) {
		child := &keav1alpha1.KeaControlAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-child", Namespace: "default"},
			Status: keav1alpha1.KeaControlAgentStatus{
				ComponentStatus: keav1alpha1.ComponentStatus{Phase: "Running"},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(child).WithObjects(child).Build()
		r := &KeaServerReconciler{Client: c, Scheme: scheme}

		ready := r.isChildReady(context.Background(), "default", "ca-child", &keav1alpha1.KeaControlAgent{})
		if !ready {
			t.Error("expected true for Running phase with no conditions (backward compat)")
		}
	})
}
