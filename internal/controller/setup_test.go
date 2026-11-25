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

package controller

import (
	"context"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func sccVolumesOf(t *testing.T, c client.Client) []string {
	t.Helper()
	scc := &unstructured.Unstructured{}
	scc.SetGroupVersionKind(sccGVK)
	if err := c.Get(context.Background(), client.ObjectKey{Name: sccName}, scc); err != nil {
		t.Fatal(err)
	}
	volumes, _, err := unstructured.NestedStringSlice(scc.Object, "volumes")
	if err != nil {
		t.Fatal(err)
	}
	return volumes
}

func existingSCC(volumes ...string) *unstructured.Unstructured {
	scc := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": sccName},
		"volumes":  toInterfaces(volumes),
	}}
	scc.SetGroupVersionKind(sccGVK)
	return scc
}

// Lease claims are persistentVolumeClaim volumes, which pods can only mount
// when the SCC admitting them allows that type. A new SCC must allow it, and
// an SCC an earlier release created without it must gain it on startup,
// keeping whatever an administrator added.
func TestEnsureSCCAllowsLeaseClaims(t *testing.T) {
	ctx := context.Background()

	fresh := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	if err := EnsureSCC(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if got := sccVolumesOf(t, fresh); !slices.Contains(got, "persistentVolumeClaim") {
		t.Errorf("new SCC volumes %v do not allow persistentVolumeClaim", got)
	}

	old := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).
		WithObjects(existingSCC("configMap", "downwardAPI", "emptyDir", "projected", "secret", "csi")).Build()
	if err := EnsureSCC(ctx, old); err != nil {
		t.Fatal(err)
	}
	got := sccVolumesOf(t, old)
	for _, want := range append(slices.Clone(sccVolumes), "csi") {
		if !slices.Contains(got, want) {
			t.Errorf("upgraded SCC volumes %v are missing %q", got, want)
		}
	}
	if len(got) != len(sccVolumes)+1 {
		t.Errorf("upgraded SCC volumes %v, want %v and csi, each once", got, sccVolumes)
	}

	wildcard := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(existingSCC("*")).Build()
	if err := EnsureSCC(ctx, wildcard); err != nil {
		t.Fatal(err)
	}
	if got := sccVolumesOf(t, wildcard); !slices.Equal(got, []string{"*"}) {
		t.Errorf("an SCC allowing every volume type was changed to %v", got)
	}
}
