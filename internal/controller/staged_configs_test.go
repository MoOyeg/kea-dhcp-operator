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
	"sort"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	keav1alpha1 "github.com/openshift/ocp-kea-dhcp/api/v1alpha1"
	"github.com/openshift/ocp-kea-dhcp/internal/resources"
)

func haConfigMapNames(t *testing.T, c client.Client) []string {
	t.Helper()
	list := &corev1.ConfigMapList{}
	if err := c.List(context.Background(), list, client.InNamespace(testNamespaceDefault),
		client.MatchingLabels(resources.CommonLabels(testDhcp4CR, testComponent4))); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(list.Items))
	for _, cm := range list.Items {
		names = append(names, cm.Name)
	}
	sort.Strings(names)
	return names
}

func stsRevisions(t *testing.T, c client.Client) (current, previous string) {
	t.Helper()
	sts := &appsv1.StatefulSet{}
	if err := c.Get(context.Background(), workloadKey(), sts); err != nil {
		t.Fatal(err)
	}
	return sts.Annotations[resources.ConfigRevisionAnnotation], sts.Annotations[resources.PreviousConfigRevisionAnnotation]
}

func changeValidLifetime(t *testing.T, c client.Client, life int32) {
	t.Helper()
	updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
		s.Spec.ValidLifetime = &life
		s.Generation++
	})
}

func exists(t *testing.T, c client.Client, obj client.Object, name string) bool {
	t.Helper()
	err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespaceDefault}, obj)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	return err == nil
}

func stagedCM(rev string, ordinal int) string {
	return resources.HAConfigMapRevisionName(testDhcp4CR, testComponent4, ordinal, rev)
}

// Each revision is written under its own names; the StatefulSet records the
// current and previous revision; the previous one is kept for rollback and
// anything older is removed once nothing uses it. Reconciling the same spec
// again creates nothing new.
func TestStagedHARevisionsAndCleanup(t *testing.T) {
	r, c := newDhcp4Reconciler(t, nil, haDhcp4Server(2))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	rev1, prev := stsRevisions(t, c)
	if rev1 == "" || prev != "" {
		t.Fatalf("first revision %q previous %q", rev1, prev)
	}
	first := haConfigMapNames(t, c)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if again := haConfigMapNames(t, c); len(again) != len(first) {
		t.Errorf("an unchanged spec created objects: %v -> %v", first, again)
	}

	changeValidLifetime(t, c, 7200)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	rev2, prev := stsRevisions(t, c)
	if rev2 == rev1 || prev != rev1 {
		t.Fatalf("after one change: current %q previous %q, want new / %q", rev2, prev, rev1)
	}
	for _, name := range []string{stagedCM(rev1, 0), stagedCM(rev1, 1), stagedCM(rev2, 0), stagedCM(rev2, 1)} {
		if !exists(t, c, &corev1.ConfigMap{}, name) {
			t.Errorf("%s missing: current and previous revisions must be kept", name)
		}
	}

	changeValidLifetime(t, c, 9000)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	rev3, prev := stsRevisions(t, c)
	if prev != rev2 {
		t.Fatalf("previous revision %q, want %q", prev, rev2)
	}
	if exists(t, c, &corev1.ConfigMap{}, stagedCM(rev1, 0)) || exists(t, c, &corev1.ConfigMap{}, stagedCM(rev1, 1)) {
		t.Error("the revision before the previous one was not removed")
	}
	want := []string{stagedCM(rev2, 0), stagedCM(rev2, 1), stagedCM(rev3, 0), stagedCM(rev3, 1)}
	sort.Strings(want)
	if got := haConfigMapNames(t, c); len(got) != len(want) {
		t.Errorf("ConfigMaps %v, want %v", got, want)
	}
}

// A pod still running an older revision keeps exactly the objects it mounts.
func TestStagedHARevisionsKeptWhilePodsUseThem(t *testing.T) {
	r, c := newDhcp4Reconciler(t, nil, haDhcp4Server(2))
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	rev1, _ := stsRevisions(t, c)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: testDhcp4CR + "-dhcp4-0", Namespace: testNamespaceDefault,
			Labels: resources.CommonLabels(testDhcp4CR, testComponent4)},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "dhcp4", Image: "kea"}},
			Volumes: []corev1.Volume{{Name: "kea-config-0", VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: stagedCM(rev1, 0)}}}}},
		},
	}
	if err := c.Create(context.Background(), pod); err != nil {
		t.Fatal(err)
	}
	changeValidLifetime(t, c, 7200)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	changeValidLifetime(t, c, 9000)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if !exists(t, c, &corev1.ConfigMap{}, stagedCM(rev1, 0)) {
		t.Error("a ConfigMap mounted by a live pod was removed")
	}
	if exists(t, c, &corev1.ConfigMap{}, stagedCM(rev1, 1)) {
		t.Error("an unused ConfigMap of an old revision was kept")
	}
}

// Objects written in place by releases before staging (<cr>-<component>-<n>)
// are kept while a held template still mounts them, and removed once the
// workload moved on. Standalone objects and objects controlled by someone
// else are never touched.
func TestLegacyHAConfigObjectsCleanup(t *testing.T) {
	ctx := context.Background()
	server := haDhcp4Server(2)
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(server, keav1alpha1.GroupVersion.WithKind("KeaDhcp4Server"))}
	legacy := func(name string, refs []metav1.OwnerReference) *corev1.ConfigMap {
		return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespaceDefault,
			Labels: resources.CommonLabels(testDhcp4CR, testComponent4), OwnerReferences: refs}}
	}
	standalone := legacy(resources.ConfigMapName(testDhcp4CR, testComponent4), owner)
	foreign := legacy(testDhcp4CR+"-dhcp4-7", nil)
	r, c := newDhcp4Reconciler(t, nil, server,
		legacy(testDhcp4CR+"-dhcp4-0", owner), legacy(testDhcp4CR+"-dhcp4-1", owner), standalone, foreign)

	// Memfile workload left by an earlier release: its held template mounts
	// the legacy objects, which must survive. (The setup reconcile creates a
	// fresh StatefulSet that does not mount them, so they are recreated to
	// model a StatefulSet that already existed.)
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{testDhcp4CR + "-dhcp4-0", testDhcp4CR + "-dhcp4-1"} {
		if err := c.Create(ctx, legacy(name, owner)); err != nil && !apierrors.IsAlreadyExists(err) {
			t.Fatal(err)
		}
	}
	sts := &appsv1.StatefulSet{}
	if err := c.Get(ctx, workloadKey(), sts); err != nil {
		t.Fatal(err)
	}
	for i, v := range sts.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && v.Name == "kea-config-0" {
			sts.Spec.Template.Spec.Volumes[i].ConfigMap.Name = testDhcp4CR + "-dhcp4-0"
		}
		if v.ConfigMap != nil && v.Name == "kea-config-1" {
			sts.Spec.Template.Spec.Volumes[i].ConfigMap.Name = testDhcp4CR + "-dhcp4-1"
		}
	}
	sts.Annotations = nil
	if err := c.Update(ctx, sts); err != nil {
		t.Fatal(err)
	}
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if !exists(t, c, &corev1.ConfigMap{}, testDhcp4CR+"-dhcp4-0") || !exists(t, c, &corev1.ConfigMap{}, testDhcp4CR+"-dhcp4-1") {
		t.Fatal("legacy ConfigMaps mounted by a held template were removed")
	}

	// Approving the restart moves the workload to staged objects; the legacy
	// ones go, the standalone and the foreign one stay.
	updateDhcp4Spec(t, c, func(s *keav1alpha1.KeaDhcp4Server) {
		s.Annotations = map[string]string{resources.ApproveRestartAnnotation: resources.TemplateRevision}
	})
	if err := reconcileDhcp4(r); err != nil {
		t.Fatal(err)
	}
	if exists(t, c, &corev1.ConfigMap{}, testDhcp4CR+"-dhcp4-0") || exists(t, c, &corev1.ConfigMap{}, testDhcp4CR+"-dhcp4-1") {
		t.Error("legacy ConfigMaps were not removed after the workload moved on")
	}
	if !exists(t, c, &corev1.ConfigMap{}, standalone.Name) {
		t.Error("the standalone ConfigMap was removed")
	}
	if !exists(t, c, &corev1.ConfigMap{}, foreign.Name) {
		t.Error("a ConfigMap this CR does not control was removed")
	}
}
